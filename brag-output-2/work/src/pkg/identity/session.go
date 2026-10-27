// SPDX-License-Identifier: Apache-2.0

package identity

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"time"

	"github.com/nexops-one/compliance-engine/pkg/access"
	"github.com/nexops-one/compliance-engine/pkg/auth"
	"github.com/nexops-one/compliance-engine/pkg/extension"
	"github.com/nexops-one/compliance-engine/pkg/store"
	"github.com/nexops-one/compliance-engine/sdk/adapter"
)

// Default console session timeouts.
const (
	DefaultSessionIdle = 30 * time.Minute
	DefaultSessionMax  = 12 * time.Hour
)

var (
	// ErrSignInFailed hides why a sign-in failed from the caller.
	ErrSignInFailed = errors.New("unknown email or wrong password")
	// ErrNoWorkspace refuses a sign-in by a user who is a member of no workspace.
	ErrNoWorkspace = errors.New("your account is not a member of any workspace")
	// ErrSessionEnded is returned for an unknown, expired or no longer valid session.
	ErrSessionEnded = errors.New("session ended; sign in again")
)

// SignedIn is a new console session. Value goes into the cookie and is
// never stored or shown again.
type SignedIn struct {
	Session    store.Session
	Value      string
	Workspaces []store.Member
}

func (s *Service) idle() time.Duration {
	if s.sessionIdle > 0 {
		return s.sessionIdle
	}
	return DefaultSessionIdle
}

func (s *Service) maxAge() time.Duration {
	if s.sessionMax > 0 {
		return s.sessionMax
	}
	return DefaultSessionMax
}

// SessionMaxAge is the absolute lifetime of a session (the cookie Max-Age).
func (s *Service) SessionMaxAge() time.Duration { return s.maxAge() }

// userEvent is an event of the tenant chain attributed to the user.
func (s *Service) userEvent(tenantID, userID, action string, details any) store.AuditEvent {
	ev := s.event(context.Background(), adapter.Scope{TenantID: tenantID}, action, "user", userID, details)
	ev.Actor, ev.ActorKind = "user:"+userID, string(extension.ActorUser)
	return ev
}

// SignIn checks a password and opens a session bound to the user's first
// workspace (in workspace order). Failures for a known user are audited as
// login.failure in the tenant chain; failures for an unknown tenant or email
// are not, so that unauthenticated traffic cannot grow an audit chain.
func (s *Service) SignIn(ctx context.Context, tenantID, email, pw string) (SignedIn, error) {
	e, err := normalizeEmail(email)
	if err != nil || tenantID == "" {
		return SignedIn{}, ErrSignInFailed
	}
	u, err := s.st.UserByEmail(ctx, tenantID, e)
	if errors.Is(err, store.ErrNotFound) {
		return SignedIn{}, ErrSignInFailed
	} else if err != nil {
		return SignedIn{}, err
	}
	failure := func(reason string, refusal error) (SignedIn, error) {
		if err := s.st.AppendAudit(ctx, s.userEvent(tenantID, u.ID, "login.failure", map[string]any{"email": e, "reason": reason})); err != nil {
			return SignedIn{}, err
		}
		return SignedIn{}, refusal
	}
	switch {
	case u.PasswordHash == "":
		return failure("no_password", ErrSignInFailed)
	case u.Disabled:
		return failure("disabled", ErrSignInFailed)
	case !VerifyPassword(u.PasswordHash, pw):
		return failure("wrong_password", ErrSignInFailed)
	}
	ms, err := s.st.UserMembers(ctx, tenantID, u.ID)
	if err != nil {
		return SignedIn{}, err
	}
	if len(ms) == 0 {
		return failure("no_membership", ErrNoWorkspace)
	}
	return s.openSession(ctx, u, ms, "", map[string]any{"email": e})
}

// SignInExternal opens a console session for a user an edition's sign-in
// provider has authenticated (for example single sign-on), preferring
// workspace when the user is a member of it. method is recorded in the
// login.success event; details add to it and must hold no secret.
// Disabled users and users without a workspace are refused and audited as
// login.failure.
func (s *Service) SignInExternal(ctx context.Context, tenantID, userID, workspace, method string, details map[string]any) (SignedIn, error) {
	u, err := s.st.User(ctx, tenantID, userID)
	if errors.Is(err, store.ErrNotFound) {
		return SignedIn{}, ErrSignInFailed
	} else if err != nil {
		return SignedIn{}, err
	}
	ev := map[string]any{"email": u.Email, "method": method}
	for k, v := range details {
		if _, fixed := ev[k]; !fixed {
			ev[k] = v
		}
	}
	failure := func(reason string, refusal error) (SignedIn, error) {
		ev["reason"] = reason
		if err := s.st.AppendAudit(ctx, s.userEvent(tenantID, u.ID, "login.failure", ev)); err != nil {
			return SignedIn{}, err
		}
		return SignedIn{}, refusal
	}
	if u.Disabled {
		return failure("disabled", ErrSignInFailed)
	}
	ms, err := s.st.UserMembers(ctx, tenantID, u.ID)
	if err != nil {
		return SignedIn{}, err
	}
	if len(ms) == 0 {
		return failure("no_membership", ErrNoWorkspace)
	}
	return s.openSession(ctx, u, ms, workspace, ev)
}

// openSession creates a session in workspace (when u is a member of it,
// otherwise in u's first workspace) and audits login.success with details.
func (s *Service) openSession(ctx context.Context, u store.User, ms []store.Member, workspace string, details map[string]any) (SignedIn, error) {
	value, err := newSessionValue()
	if err != nil {
		return SignedIn{}, err
	}
	ws := ms[0].Scope.WorkspaceID
	for _, m := range ms {
		if m.Scope.WorkspaceID == workspace {
			ws = workspace
		}
	}
	now := s.now()
	se := store.Session{Hash: auth.HashToken(value), TenantID: u.TenantID, UserID: u.ID, WorkspaceID: ws,
		CreatedAt: now, LastSeenAt: now, ExpiresAt: now.Add(s.maxAge())}
	details["workspace"] = ws
	ev := s.userEvent(u.TenantID, u.ID, "login.success", details)
	if err := s.st.CreateSession(ctx, se, ev); err != nil {
		return SignedIn{}, err
	}
	return SignedIn{Session: se, Value: value, Workspaces: ms}, nil
}

func newSessionValue() (string, error) {
	var b [32]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b[:]), nil
}

// SessionPrincipal resolves a session value into the principal acting in
// the session's workspace. Roles are read from the membership at each call,
// so a removed member or role takes effect at once. An expired session (idle
// or absolute) is deleted.
func (s *Service) SessionPrincipal(ctx context.Context, value string) (extension.Principal, store.Session, error) {
	if value == "" {
		return extension.Principal{}, store.Session{}, ErrSessionEnded
	}
	hash := auth.HashToken(value)
	se, err := s.st.SessionByHash(ctx, hash)
	if errors.Is(err, store.ErrNotFound) {
		return extension.Principal{}, store.Session{}, ErrSessionEnded
	} else if err != nil {
		return extension.Principal{}, store.Session{}, err
	}
	now := s.now()
	if !now.Before(se.ExpiresAt) || now.Sub(se.LastSeenAt) >= s.idle() {
		_ = s.st.DeleteSession(ctx, hash) // best effort: the session is refused either way
		return extension.Principal{}, store.Session{}, ErrSessionEnded
	}
	u, err := s.st.User(ctx, se.TenantID, se.UserID)
	if err != nil || u.Disabled {
		return extension.Principal{}, store.Session{}, ErrSessionEnded
	}
	scope := adapter.Scope{TenantID: se.TenantID, WorkspaceID: se.WorkspaceID}
	m, err := s.st.Member(ctx, scope, se.UserID)
	if err != nil || len(m.Roles) == 0 {
		return extension.Principal{}, store.Session{}, ErrSessionEnded
	}
	if now.Sub(se.LastSeenAt) >= touchInterval {
		if err := s.st.TouchSession(ctx, hash, now); err == nil {
			se.LastSeenAt = now
		}
	}
	p := extension.Principal{Scope: scope, Actor: "user:" + se.UserID, Kind: extension.ActorUser, Roles: access.Normalize(m.Roles)}
	return p, se, nil
}

// Workspaces lists the memberships of a session's user.
func (s *Service) Workspaces(ctx context.Context, se store.Session) ([]store.Member, error) {
	return s.st.UserMembers(ctx, se.TenantID, se.UserID)
}

// SwitchWorkspace binds the session to another workspace the user is a
// member of.
func (s *Service) SwitchWorkspace(ctx context.Context, value, workspaceID string) error {
	_, se, err := s.SessionPrincipal(ctx, value)
	if err != nil {
		return err
	}
	if _, err := s.st.Member(ctx, adapter.Scope{TenantID: se.TenantID, WorkspaceID: workspaceID}, se.UserID); err != nil {
		return fmt.Errorf("%w: not a member of workspace %s", ErrForbidden, workspaceID)
	}
	return s.st.SetSessionWorkspace(ctx, se.Hash, workspaceID)
}

// SignOut deletes the session and audits it as logout.
func (s *Service) SignOut(ctx context.Context, value string) error {
	hash := auth.HashToken(value)
	se, err := s.st.SessionByHash(ctx, hash)
	if errors.Is(err, store.ErrNotFound) {
		return nil
	} else if err != nil {
		return err
	}
	return s.st.DeleteSession(ctx, hash, s.userEvent(se.TenantID, se.UserID, "logout", map[string]any{}))
}

// PurgeSessions deletes expired and idle sessions (retention job).
func (s *Service) PurgeSessions(ctx context.Context) (int, error) {
	now := s.now()
	return s.st.DeleteExpiredSessions(ctx, now, now.Add(-s.idle()))
}
