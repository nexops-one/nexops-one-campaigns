// SPDX-License-Identifier: Apache-2.0

// Package identity manages local users, workspace members and stored API
// tokens, and authenticates requests carrying stored tokens. It enforces the
// rules around roles (a token never carries a role its creator does not
// hold; service tokens never review or approve) and writes an audit event for
// every change through the store.
package identity

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/mail"
	"strings"
	"time"

	"github.com/nexops-one/compliance-engine/pkg/access"
	"github.com/nexops-one/compliance-engine/pkg/auth"
	"github.com/nexops-one/compliance-engine/pkg/extension"
	"github.com/nexops-one/compliance-engine/pkg/store"
	"github.com/nexops-one/compliance-engine/sdk/adapter"
)

var (
	ErrForbidden      = errors.New("not allowed")
	ErrRoleNotHeld    = errors.New("a token cannot carry a role its creator does not hold")
	ErrServiceRole    = errors.New("service tokens may only hold the owner and auditor roles")
	ErrLastAdmin      = errors.New("the workspace must keep at least one admin")
	ErrInvalidEmail   = errors.New("invalid email address")
	ErrInvalidRequest = errors.New("invalid request")
)

// ServiceRoles are the only roles a service token may hold.
var ServiceRoles = []access.Role{access.RoleOwner, access.RoleAuditor}

// OperatorActor is recorded for CLI actions when the context has no principal.
const OperatorActor = "cli:operator"

// Store is what the service needs from the engine store.
type Store interface {
	store.IdentityStore
	store.AuditLog
}

// Options configures a Service.
type Options struct {
	Clock func() time.Time
	NewID func(prefix string) string
	// SessionIdle and SessionMax bound console sessions (defaults
	// DefaultSessionIdle and DefaultSessionMax).
	SessionIdle time.Duration
	SessionMax  time.Duration
}

// Service implements the identity rules over a Store.
type Service struct {
	st          Store
	now         func() time.Time
	newID       func(string) string
	sessionIdle time.Duration
	sessionMax  time.Duration
	gate        func(context.Context, adapter.Scope) error
}

// SetWorkspaceGate installs the check run before a member is added or a
// token issued in a workspace, typically compliance.Engine.WorkspaceWritable
// (registration on first use, refusal of suspended workspaces). Call it
// before serving requests.
func (s *Service) SetWorkspaceGate(gate func(context.Context, adapter.Scope) error) { s.gate = gate }

func (s *Service) checkGate(ctx context.Context, scope adapter.Scope) error {
	if s.gate == nil {
		return nil
	}
	return s.gate(ctx, scope)
}

// New returns a Service.
func New(st Store, opts Options) *Service {
	s := &Service{st: st, now: opts.Clock, newID: opts.NewID, sessionIdle: opts.SessionIdle, sessionMax: opts.SessionMax}
	if s.now == nil {
		s.now = func() time.Time { return time.Now().UTC() }
	}
	if s.newID == nil {
		s.newID = func(prefix string) string {
			var b [8]byte
			_, _ = rand.Read(b[:])
			return prefix + "-" + hex.EncodeToString(b[:])
		}
	}
	return s
}

// event builds an audit event attributed to the principal in ctx, or to the
// CLI operator when there is none.
func (s *Service) event(ctx context.Context, scope adapter.Scope, action, targetType, targetID string, details any) store.AuditEvent {
	actor, kind := OperatorActor, string(extension.ActorSystem)
	if p, ok := extension.PrincipalFrom(ctx); ok {
		actor, kind = p.Actor, string(p.Kind)
	}
	data, _ := json.Marshal(details)
	return store.AuditEvent{Scope: scope, At: s.now(), Actor: actor, ActorKind: kind, Action: action,
		TargetType: targetType, TargetID: targetID, Details: data}
}

func withCaller(ctx context.Context, p extension.Principal) context.Context {
	return extension.WithPrincipal(ctx, p)
}

func normalizeEmail(email string) (string, error) {
	e := store.NormalizeEmail(email)
	addr, err := mail.ParseAddress(e)
	if err != nil || addr.Address != e || strings.ContainsAny(e, " ,:") {
		return "", fmt.Errorf("%w: %q", ErrInvalidEmail, email)
	}
	return e, nil
}

func require(p extension.Principal, perm access.Permission) error {
	if !access.Allows(p.Roles, perm) {
		return fmt.Errorf("%w: requires %s", ErrForbidden, perm)
	}
	return nil
}

// CreateUser creates a tenant user with a generated password, returned once.
func (s *Service) CreateUser(ctx context.Context, tenantID, email string) (store.User, string, error) {
	e, err := normalizeEmail(email)
	if err != nil {
		return store.User{}, "", err
	}
	if strings.TrimSpace(tenantID) == "" {
		return store.User{}, "", fmt.Errorf("%w: tenant is required", ErrInvalidRequest)
	}
	pw, err := GeneratePassword()
	if err != nil {
		return store.User{}, "", err
	}
	hash, err := HashPassword(pw)
	if err != nil {
		return store.User{}, "", err
	}
	u := store.User{TenantID: tenantID, ID: s.newID("usr"), Email: e, PasswordHash: hash, CreatedAt: s.now()}
	ev := s.event(ctx, adapter.Scope{TenantID: tenantID}, "user.create", "user", u.ID, map[string]any{"email": e})
	if err := s.st.CreateUser(ctx, u, ev); err != nil {
		return store.User{}, "", err
	}
	return u, pw, nil
}

// ResetPassword sets a new generated password for a user and returns it once.
func (s *Service) ResetPassword(ctx context.Context, tenantID, email string) (string, error) {
	e, err := normalizeEmail(email)
	if err != nil {
		return "", err
	}
	u, err := s.st.UserByEmail(ctx, tenantID, e)
	if err != nil {
		return "", err
	}
	pw, err := GeneratePassword()
	if err != nil {
		return "", err
	}
	hash, err := HashPassword(pw)
	if err != nil {
		return "", err
	}
	ev := s.event(ctx, adapter.Scope{TenantID: tenantID}, "user.password_reset", "user", u.ID, map[string]any{"email": e})
	if err := s.st.SetPassword(ctx, tenantID, u.ID, hash, ev); err != nil {
		return "", err
	}
	return pw, nil
}

// Users lists a tenant's users.
func (s *Service) Users(ctx context.Context, tenantID string) ([]store.User, error) {
	return s.st.Users(ctx, tenantID)
}

// CheckPassword authenticates a user by email and password (console sign-in).
func (s *Service) CheckPassword(ctx context.Context, tenantID, email, pw string) (store.User, bool) {
	e, err := normalizeEmail(email)
	if err != nil {
		return store.User{}, false
	}
	u, err := s.st.UserByEmail(ctx, tenantID, e)
	if err != nil || u.Disabled || !VerifyPassword(u.PasswordHash, pw) {
		return store.User{}, false
	}
	return u, true
}

// Members lists the caller's workspace members.
func (s *Service) Members(ctx context.Context, p extension.Principal) ([]store.Member, error) {
	if err := require(p, access.PermMembersManage); err != nil {
		return nil, err
	}
	return s.st.Members(ctx, p.Scope)
}

func (s *Service) otherAdmins(ctx context.Context, scope adapter.Scope, userID string) (int, error) {
	ms, err := s.st.Members(ctx, scope)
	if err != nil {
		return 0, err
	}
	n := 0
	for _, m := range ms {
		if m.UserID != userID && access.Contains(m.Roles, access.RoleAdmin) {
			n++
		}
	}
	return n, nil
}

// SetMember grants roles in the caller's workspace to the user with email,
// creating a user without a password when none exists. It refuses to remove
// the admin role from the workspace's last admin.
func (s *Service) SetMember(ctx context.Context, p extension.Principal, email string, roles []access.Role) (store.Member, error) {
	if err := require(p, access.PermMembersManage); err != nil {
		return store.Member{}, err
	}
	return s.setMember(withCaller(ctx, p), p.Scope, email, roles)
}

// MembersOf lists a workspace's members as the operator (CLI, demo bootstrap).
func (s *Service) MembersOf(ctx context.Context, scope adapter.Scope) ([]store.Member, error) {
	return s.st.Members(ctx, scope)
}

// AddMember grants roles as the operator (CLI), without a caller check.
func (s *Service) AddMember(ctx context.Context, scope adapter.Scope, email string, roles []access.Role) (store.Member, error) {
	return s.setMember(ctx, scope, email, roles)
}

func (s *Service) setMember(ctx context.Context, scope adapter.Scope, email string, roles []access.Role) (store.Member, error) {
	if err := scope.Validate(); err != nil {
		return store.Member{}, fmt.Errorf("%w: %v", ErrInvalidRequest, err)
	}
	e, err := normalizeEmail(email)
	if err != nil {
		return store.Member{}, err
	}
	if len(roles) == 0 {
		return store.Member{}, fmt.Errorf("%w: at least one role is required", ErrInvalidRequest)
	}
	if err := access.Validate(roles); err != nil {
		return store.Member{}, fmt.Errorf("%w: %v", ErrInvalidRequest, err)
	}
	if err := s.checkGate(ctx, scope); err != nil {
		return store.Member{}, err
	}
	roles = access.Normalize(roles)
	u, err := s.st.UserByEmail(ctx, scope.TenantID, e)
	if errors.Is(err, store.ErrNotFound) {
		u = store.User{TenantID: scope.TenantID, ID: s.newID("usr"), Email: e, CreatedAt: s.now()}
		ev := s.event(ctx, adapter.Scope{TenantID: scope.TenantID}, "user.create", "user", u.ID, map[string]any{"email": e, "password": false})
		if err := s.st.CreateUser(ctx, u, ev); err != nil {
			return store.Member{}, err
		}
	} else if err != nil {
		return store.Member{}, err
	}
	if cur, err := s.st.Member(ctx, scope, u.ID); err == nil && access.Contains(cur.Roles, access.RoleAdmin) && !access.Contains(roles, access.RoleAdmin) {
		if n, err := s.otherAdmins(ctx, scope, u.ID); err != nil {
			return store.Member{}, err
		} else if n == 0 {
			return store.Member{}, ErrLastAdmin
		}
	}
	m := store.Member{Scope: scope, UserID: u.ID, Email: e, Roles: roles, UpdatedAt: s.now()}
	ev := s.event(ctx, scope, "member.set", "user", u.ID, map[string]any{"email": e, "roles": roles})
	if err := s.st.PutMember(ctx, m, ev); err != nil {
		return store.Member{}, err
	}
	return s.st.Member(ctx, scope, u.ID)
}

// RemoveMember removes the user with email from the caller's workspace and
// revokes their tokens there.
func (s *Service) RemoveMember(ctx context.Context, p extension.Principal, email string) error {
	if err := require(p, access.PermMembersManage); err != nil {
		return err
	}
	ctx = withCaller(ctx, p)
	e, err := normalizeEmail(email)
	if err != nil {
		return err
	}
	u, err := s.st.UserByEmail(ctx, p.Scope.TenantID, e)
	if err != nil {
		return err
	}
	m, err := s.st.Member(ctx, p.Scope, u.ID)
	if err != nil {
		return err
	}
	if access.Contains(m.Roles, access.RoleAdmin) {
		if n, err := s.otherAdmins(ctx, p.Scope, u.ID); err != nil {
			return err
		} else if n == 0 {
			return ErrLastAdmin
		}
	}
	ev := s.event(ctx, p.Scope, "member.remove", "user", u.ID, map[string]any{"email": e})
	return s.st.DeleteMember(ctx, p.Scope, u.ID, s.now(), ev)
}

// TokenRequest describes a token to create.
type TokenRequest struct {
	Name      string        `json:"name"`
	Roles     []access.Role `json:"roles"`
	ExpiresAt *time.Time    `json:"expires_at,omitempty"`
	Service   bool          `json:"service,omitempty"`
}

// CreatedToken is a new token and its value, which is never shown again.
type CreatedToken struct {
	Token store.Token `json:"token"`
	Value string      `json:"value"`
}

// UserID returns the user ID of a user principal ("user:<id>").
func UserID(p extension.Principal) (string, bool) {
	if p.Kind != extension.ActorUser {
		return "", false
	}
	id, ok := strings.CutPrefix(p.Actor, "user:")
	return id, ok && id != ""
}

func (s *Service) checkRequest(req TokenRequest) (TokenRequest, error) {
	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" || len(req.Name) > 100 {
		return req, fmt.Errorf("%w: a token name of 1 to 100 characters is required", ErrInvalidRequest)
	}
	if len(req.Roles) == 0 {
		return req, fmt.Errorf("%w: at least one role is required", ErrInvalidRequest)
	}
	if err := access.Validate(req.Roles); err != nil {
		return req, fmt.Errorf("%w: %v", ErrInvalidRequest, err)
	}
	req.Roles = access.Normalize(req.Roles)
	if req.ExpiresAt != nil {
		exp := req.ExpiresAt.UTC()
		if !exp.After(s.now()) {
			return req, fmt.Errorf("%w: expires_at must be in the future", ErrInvalidRequest)
		}
		req.ExpiresAt = &exp
	}
	if req.Service && !access.Subset(req.Roles, ServiceRoles) {
		return req, ErrServiceRole
	}
	return req, nil
}

// CreateToken creates a token for the caller (a user token with a subset of
// the caller's roles) or, for admins, a service token.
func (s *Service) CreateToken(ctx context.Context, p extension.Principal, req TokenRequest) (CreatedToken, error) {
	if err := require(p, access.PermTokensOwn); err != nil {
		return CreatedToken{}, err
	}
	req, err := s.checkRequest(req)
	if err != nil {
		return CreatedToken{}, err
	}
	userID := ""
	if req.Service {
		if err := require(p, access.PermTokensManage); err != nil {
			return CreatedToken{}, err
		}
	} else {
		id, ok := UserID(p)
		if !ok {
			return CreatedToken{}, fmt.Errorf("%w: only users can create user tokens; admins can create service tokens", ErrForbidden)
		}
		if !access.Subset(req.Roles, p.Roles) {
			return CreatedToken{}, ErrRoleNotHeld
		}
		userID = id
	}
	return s.issue(withCaller(ctx, p), p.Scope, userID, req)
}

// IssueToken creates a token as the operator (CLI): for the user with email,
// or a service token when email is empty and req.Service is set. User tokens
// are limited to the user's current roles.
func (s *Service) IssueToken(ctx context.Context, scope adapter.Scope, email string, req TokenRequest) (CreatedToken, error) {
	if err := scope.Validate(); err != nil {
		return CreatedToken{}, fmt.Errorf("%w: %v", ErrInvalidRequest, err)
	}
	req, err := s.checkRequest(req)
	if err != nil {
		return CreatedToken{}, err
	}
	userID := ""
	if !req.Service {
		e, err := normalizeEmail(email)
		if err != nil {
			return CreatedToken{}, err
		}
		u, err := s.st.UserByEmail(ctx, scope.TenantID, e)
		if err != nil {
			return CreatedToken{}, err
		}
		m, err := s.st.Member(ctx, scope, u.ID)
		if err != nil {
			return CreatedToken{}, fmt.Errorf("%s is not a member of workspace %s: %w", e, scope.WorkspaceID, err)
		}
		if !access.Subset(req.Roles, m.Roles) {
			return CreatedToken{}, ErrRoleNotHeld
		}
		userID = u.ID
	}
	return s.issue(ctx, scope, userID, req)
}

func (s *Service) issue(ctx context.Context, scope adapter.Scope, userID string, req TokenRequest) (CreatedToken, error) {
	if err := s.checkGate(ctx, scope); err != nil {
		return CreatedToken{}, err
	}
	value, err := auth.GenerateToken()
	if err != nil {
		return CreatedToken{}, err
	}
	createdBy := OperatorActor
	if p, ok := extension.PrincipalFrom(ctx); ok {
		createdBy = p.Actor
	}
	t := store.Token{ID: s.newID("tok"), Scope: scope, Hash: auth.HashToken(value), Name: req.Name, UserID: userID,
		Roles: req.Roles, CreatedBy: createdBy, CreatedAt: s.now(), ExpiresAt: req.ExpiresAt}
	ev := s.event(ctx, scope, "token.create", "token", t.ID, map[string]any{
		"name": t.Name, "roles": t.Roles, "service": userID == "", "user_id": userID, "expires_at": t.ExpiresAt,
	})
	if err := s.st.CreateToken(ctx, t, ev); err != nil {
		return CreatedToken{}, err
	}
	return CreatedToken{Token: t, Value: value}, nil
}

// Tokens lists the workspace's tokens for admins, and the caller's own user
// tokens for everyone else.
func (s *Service) Tokens(ctx context.Context, p extension.Principal) ([]store.Token, error) {
	if err := require(p, access.PermTokensOwn); err != nil {
		return nil, err
	}
	all, err := s.st.Tokens(ctx, p.Scope)
	if err != nil || access.Allows(p.Roles, access.PermTokensManage) {
		return all, err
	}
	id, _ := UserID(p)
	own := []store.Token{}
	for _, t := range all {
		if id != "" && t.UserID == id {
			own = append(own, t)
		}
	}
	return own, nil
}

// AllTokens lists a workspace's tokens as the operator (CLI).
func (s *Service) AllTokens(ctx context.Context, scope adapter.Scope) ([]store.Token, error) {
	return s.st.Tokens(ctx, scope)
}

// RevokeToken revokes a token of the caller's workspace: any token for
// admins, the caller's own user tokens otherwise.
func (s *Service) RevokeToken(ctx context.Context, p extension.Principal, id string) error {
	if err := require(p, access.PermTokensOwn); err != nil {
		return err
	}
	if !access.Allows(p.Roles, access.PermTokensManage) {
		own, err := s.Tokens(ctx, p)
		if err != nil {
			return err
		}
		found := false
		for _, t := range own {
			found = found || t.ID == id
		}
		if !found {
			return fmt.Errorf("%w: only admins can revoke other callers' tokens", ErrForbidden)
		}
	}
	return s.Revoke(withCaller(ctx, p), p.Scope, id)
}

// Revoke revokes a token as the operator (CLI) or on behalf of a checked caller.
func (s *Service) Revoke(ctx context.Context, scope adapter.Scope, id string) error {
	ev := s.event(ctx, scope, "token.revoke", "token", id, map[string]any{})
	return s.st.RevokeToken(ctx, scope, id, s.now(), ev)
}
