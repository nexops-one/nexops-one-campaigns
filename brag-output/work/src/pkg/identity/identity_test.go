// SPDX-License-Identifier: Apache-2.0

package identity_test

import (
	"context"
	"errors"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/nexops-one/compliance-engine/pkg/access"
	"github.com/nexops-one/compliance-engine/pkg/audit"
	"github.com/nexops-one/compliance-engine/pkg/extension"
	"github.com/nexops-one/compliance-engine/pkg/identity"
	"github.com/nexops-one/compliance-engine/pkg/store"
	"github.com/nexops-one/compliance-engine/pkg/store/memory"
	"github.com/nexops-one/compliance-engine/sdk/adapter"
)

var (
	ctx   = context.Background()
	scope = adapter.Scope{TenantID: "acme", WorkspaceID: "ws-1"}
	other = adapter.Scope{TenantID: "acme", WorkspaceID: "ws-2"}
	t0    = time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC)
)

type clock struct{ now time.Time }

func (c *clock) Now() time.Time { return c.now }

func newService(t *testing.T) (*identity.Service, *memory.Store, *clock) {
	t.Helper()
	st := memory.New()
	c := &clock{now: t0}
	return identity.New(st, identity.Options{Clock: c.Now}), st, c
}

// admin is the bootstrap static-token principal of scope.
var admin = extension.Principal{Scope: scope, Actor: "token:bootstrap", Kind: extension.ActorToken, Roles: []access.Role{access.RoleOwner, access.RoleAdmin}}

func roles(rs ...access.Role) []access.Role { return rs }

// authenticate resolves a token value through the service.
func authenticate(s *identity.Service, value string) (extension.Principal, error) {
	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("Authorization", "Bearer "+value)
	return s.Authenticate(req)
}

// userPrincipal creates a member and returns a principal acting as that user.
func userPrincipal(t *testing.T, s *identity.Service, email string, rs ...access.Role) extension.Principal {
	t.Helper()
	m, err := s.SetMember(ctx, admin, email, rs)
	if err != nil {
		t.Fatal(err)
	}
	return extension.Principal{Scope: scope, Actor: "user:" + m.UserID, Kind: extension.ActorUser, Roles: m.Roles}
}

func TestPasswordRoundTrip(t *testing.T) {
	h1, err := identity.HashPassword("correct horse")
	if err != nil {
		t.Fatal(err)
	}
	h2, _ := identity.HashPassword("correct horse")
	if h1 == h2 || !strings.HasPrefix(h1, "$argon2id$v=19$m=65536,t=3,p=2$") {
		t.Fatalf("hashes %q %q", h1, h2)
	}
	if !identity.VerifyPassword(h1, "correct horse") || identity.VerifyPassword(h1, "wrong") {
		t.Fatal("verification")
	}
	for _, bad := range []string{"", "$argon2id$v=19$m=0,t=3,p=2$AAAA$AAAA", "$argon2i$v=19$m=65536,t=3,p=2$AAAA$AAAA", "plain"} {
		if identity.VerifyPassword(bad, "correct horse") {
			t.Errorf("malformed hash %q must not verify", bad)
		}
	}
	if _, err := identity.HashPassword(""); err == nil {
		t.Fatal("an empty password must be refused")
	}
	pw, _ := identity.GeneratePassword()
	if len(pw) != 32 {
		t.Fatalf("generated password %q", pw)
	}
}

func TestUsersAndPasswords(t *testing.T) {
	s, st, _ := newService(t)
	u, pw, err := s.CreateUser(ctx, "acme", " Anna@Example.com ")
	if err != nil || u.Email != "anna@example.com" || pw == "" {
		t.Fatalf("create = %+v %q %v", u, pw, err)
	}
	if _, _, err := s.CreateUser(ctx, "acme", "anna@example.com"); !errors.Is(err, store.ErrExists) {
		t.Fatalf("duplicate = %v", err)
	}
	if _, _, err := s.CreateUser(ctx, "acme", "not an email"); !errors.Is(err, identity.ErrInvalidEmail) {
		t.Fatalf("invalid email = %v", err)
	}
	if _, ok := s.CheckPassword(ctx, "acme", "ANNA@example.com", pw); !ok {
		t.Fatal("the generated password must sign in")
	}
	pw2, err := s.ResetPassword(ctx, "acme", "anna@example.com")
	if err != nil || pw2 == pw {
		t.Fatalf("reset = %q %v", pw2, err)
	}
	if _, ok := s.CheckPassword(ctx, "acme", "anna@example.com", pw); ok {
		t.Fatal("the old password must stop working")
	}
	if _, ok := s.CheckPassword(ctx, "acme", "anna@example.com", pw2); !ok {
		t.Fatal("the new password must work")
	}
	evs, _ := st.AuditEvents(ctx, adapter.Scope{TenantID: "acme"}, store.AuditQuery{})
	if len(evs) != 2 || evs[0].Action != "user.create" || evs[1].Action != "user.password_reset" || evs[0].Actor != identity.OperatorActor {
		t.Fatalf("tenant events = %+v", evs)
	}
	for _, e := range evs {
		if strings.Contains(string(e.Details), pw) || strings.Contains(string(e.Details), "argon2") {
			t.Fatal("audit details must never contain passwords or hashes")
		}
	}
}

func TestMembers(t *testing.T) {
	s, _, _ := newService(t)
	m, err := s.SetMember(ctx, admin, "zoe@example.com", roles(access.RoleApprover, access.RoleAdmin))
	if err != nil || !reflect.DeepEqual(m.Roles, roles(access.RoleApprover, access.RoleAdmin)) || m.Email != "zoe@example.com" {
		t.Fatalf("set = %+v %v", m, err)
	}
	if _, ok := s.CheckPassword(ctx, "acme", "zoe@example.com", ""); ok {
		t.Fatal("a user created by membership has no password")
	}
	if _, err := s.SetMember(ctx, admin, "zoe@example.com", roles("root")); !errors.Is(err, identity.ErrInvalidRequest) {
		t.Fatalf("bad role = %v", err)
	}
	owner := userPrincipal(t, s, "olga@example.com", access.RoleOwner)
	if _, err := s.SetMember(ctx, owner, "x@example.com", roles(access.RoleOwner)); !errors.Is(err, identity.ErrForbidden) {
		t.Fatalf("non-admin = %v", err)
	}
	if _, err := s.SetMember(ctx, admin, "zoe@example.com", roles(access.RoleApprover)); !errors.Is(err, identity.ErrLastAdmin) {
		t.Fatalf("demoting the last admin = %v", err)
	}
	if err := s.RemoveMember(ctx, admin, "zoe@example.com"); !errors.Is(err, identity.ErrLastAdmin) {
		t.Fatalf("removing the last admin = %v", err)
	}
	if _, err := s.SetMember(ctx, admin, "olga@example.com", roles(access.RoleOwner, access.RoleAdmin)); err != nil {
		t.Fatal(err)
	}
	if err := s.RemoveMember(ctx, admin, "zoe@example.com"); err != nil {
		t.Fatalf("removing an admin with another admin left: %v", err)
	}
	ms, _ := s.Members(ctx, admin)
	if len(ms) != 1 || ms[0].Email != "olga@example.com" {
		t.Fatalf("members = %+v", ms)
	}
}

func TestTokenRules(t *testing.T) {
	s, st, _ := newService(t)
	approver := userPrincipal(t, s, "ann@example.com", access.RoleApprover)
	owner := userPrincipal(t, s, "olga@example.com", access.RoleOwner)

	if _, err := s.CreateToken(ctx, owner, identity.TokenRequest{Name: "ci", Roles: roles(access.RoleApprover)}); !errors.Is(err, identity.ErrRoleNotHeld) {
		t.Fatalf("minting a role not held = %v", err)
	}
	if _, err := s.CreateToken(ctx, admin, identity.TokenRequest{Name: "svc", Roles: roles(access.RoleApprover), Service: true}); !errors.Is(err, identity.ErrServiceRole) {
		t.Fatalf("service approver = %v", err)
	}
	if _, err := s.CreateToken(ctx, admin, identity.TokenRequest{Name: "svc", Roles: roles(access.RoleReviewer), Service: true}); !errors.Is(err, identity.ErrServiceRole) {
		t.Fatalf("service reviewer = %v", err)
	}
	if _, err := s.CreateToken(ctx, owner, identity.TokenRequest{Name: "svc", Roles: roles(access.RoleOwner), Service: true}); !errors.Is(err, identity.ErrForbidden) {
		t.Fatalf("non-admin service token = %v", err)
	}
	if _, err := s.CreateToken(ctx, admin, identity.TokenRequest{Name: "mine", Roles: roles(access.RoleOwner)}); !errors.Is(err, identity.ErrForbidden) {
		t.Fatalf("a static token has no user to own a user token: %v", err)
	}
	past := t0.Add(-time.Hour)
	if _, err := s.CreateToken(ctx, owner, identity.TokenRequest{Name: "old", Roles: roles(access.RoleOwner), ExpiresAt: &past}); !errors.Is(err, identity.ErrInvalidRequest) {
		t.Fatalf("expiry in the past = %v", err)
	}
	if _, err := s.CreateToken(ctx, owner, identity.TokenRequest{Name: " ", Roles: roles(access.RoleOwner)}); !errors.Is(err, identity.ErrInvalidRequest) {
		t.Fatalf("empty name = %v", err)
	}

	own, err := s.CreateToken(ctx, owner, identity.TokenRequest{Name: "laptop", Roles: roles(access.RoleOwner)})
	if err != nil || own.Value == "" || own.Token.UserID == "" || own.Token.CreatedBy != owner.Actor {
		t.Fatalf("user token = %+v %v", own, err)
	}
	svc, err := s.CreateToken(ctx, admin, identity.TokenRequest{Name: "pipeline", Roles: roles(access.RoleOwner, access.RoleAuditor), Service: true})
	if err != nil || svc.Token.UserID != "" {
		t.Fatalf("service token = %+v %v", svc, err)
	}
	appr, err := s.CreateToken(ctx, approver, identity.TokenRequest{Name: "approvals", Roles: roles(access.RoleApprover)})
	if err != nil {
		t.Fatal(err)
	}

	if list, _ := s.Tokens(ctx, owner); len(list) != 1 || list[0].ID != own.Token.ID {
		t.Fatalf("an owner lists only their own tokens: %+v", list)
	}
	if list, _ := s.Tokens(ctx, admin); len(list) != 3 {
		t.Fatalf("an admin lists all tokens: %+v", list)
	}
	if err := s.RevokeToken(ctx, owner, appr.Token.ID); !errors.Is(err, identity.ErrForbidden) {
		t.Fatalf("revoking another user's token = %v", err)
	}
	if err := s.RevokeToken(ctx, owner, own.Token.ID); err != nil {
		t.Fatalf("revoking one's own token: %v", err)
	}
	if _, err := authenticate(s, own.Value); err == nil {
		t.Fatal("a revoked token must not authenticate")
	}

	if err := s.RemoveMember(ctx, admin, "ann@example.com"); err != nil {
		t.Fatal(err)
	}
	if _, err := authenticate(s, appr.Value); err == nil {
		t.Fatal("removing a member must revoke their tokens")
	}
	evs, _ := st.AuditEvents(ctx, scope, store.AuditQuery{})
	if r := audit.Verify(evs); !r.OK {
		t.Fatalf("chain: %+v", r)
	}
	for _, e := range evs {
		for _, v := range []string{own.Value, svc.Value, appr.Value} {
			if strings.Contains(string(e.Details), v) {
				t.Fatal("audit details must never contain token values")
			}
		}
	}
}

func TestAuthenticateStoredTokens(t *testing.T) {
	s, st, c := newService(t)
	multi := userPrincipal(t, s, "mia@example.com", access.RoleOwner, access.RoleApprover)
	tok, err := s.CreateToken(ctx, multi, identity.TokenRequest{Name: "all", Roles: roles(access.RoleOwner, access.RoleApprover)})
	if err != nil {
		t.Fatal(err)
	}
	p, err := authenticate(s, tok.Value)
	if err != nil || p.Actor != multi.Actor || p.Kind != extension.ActorUser || !reflect.DeepEqual(p.Roles, roles(access.RoleOwner, access.RoleApprover)) || p.Scope != scope {
		t.Fatalf("principal = %+v %v", p, err)
	}
	if got, _ := st.TokenByHash(ctx, tok.Token.Hash); got.LastUsedAt == nil || !got.LastUsedAt.Equal(t0) {
		t.Fatalf("last use = %+v", got.LastUsedAt)
	}

	// Shrinking the membership shrinks the token at once.
	if _, err := s.SetMember(ctx, admin, "mia@example.com", roles(access.RoleOwner)); err != nil {
		t.Fatal(err)
	}
	if p, _ := authenticate(s, tok.Value); !reflect.DeepEqual(p.Roles, roles(access.RoleOwner)) {
		t.Fatalf("roles after demotion = %v", p.Roles)
	}
	// No overlap left: refused.
	if _, err := s.SetMember(ctx, admin, "mia@example.com", roles(access.RoleAuditor)); err != nil {
		t.Fatal(err)
	}
	if _, err := authenticate(s, tok.Value); err == nil {
		t.Fatal("a token whose roles the user no longer holds must be refused")
	}

	svc, _ := s.CreateToken(ctx, admin, identity.TokenRequest{Name: "svc", Roles: roles(access.RoleOwner), Service: true})
	if p, err := authenticate(s, svc.Value); err != nil || p.Actor != "token:"+svc.Token.ID || p.Kind != extension.ActorToken {
		t.Fatalf("service principal = %+v %v", p, err)
	}

	exp := t0.Add(time.Hour)
	short, _ := s.CreateToken(ctx, admin, identity.TokenRequest{Name: "short", Roles: roles(access.RoleOwner), Service: true, ExpiresAt: &exp})
	c.now = exp
	if _, err := authenticate(s, short.Value); err == nil {
		t.Fatal("an expired token must be refused")
	}
	for _, v := range []string{"", "nope", "ce_unknown"} {
		if _, err := authenticate(s, v); err == nil {
			t.Errorf("%q must be refused", v)
		}
	}
	// Tokens are bound to their workspace.
	if p, _ := authenticate(s, svc.Value); p.Scope == other {
		t.Fatal("scope")
	}
}

func TestIssueToken(t *testing.T) {
	s, _, _ := newService(t)
	if _, err := s.AddMember(ctx, scope, "op@example.com", roles(access.RoleOwner)); err != nil {
		t.Fatal(err)
	}
	if _, err := s.IssueToken(ctx, scope, "op@example.com", identity.TokenRequest{Name: "x", Roles: roles(access.RoleApprover)}); !errors.Is(err, identity.ErrRoleNotHeld) {
		t.Fatalf("issue beyond membership = %v", err)
	}
	if _, err := s.IssueToken(ctx, other, "op@example.com", identity.TokenRequest{Name: "x", Roles: roles(access.RoleOwner)}); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("issue outside membership = %v", err)
	}
	tok, err := s.IssueToken(ctx, scope, "op@example.com", identity.TokenRequest{Name: "x", Roles: roles(access.RoleOwner)})
	if err != nil || tok.Token.CreatedBy != identity.OperatorActor {
		t.Fatalf("issue = %+v %v", tok, err)
	}
	if p, err := authenticate(s, tok.Value); err != nil || p.Kind != extension.ActorUser {
		t.Fatalf("issued token = %+v %v", p, err)
	}
}
