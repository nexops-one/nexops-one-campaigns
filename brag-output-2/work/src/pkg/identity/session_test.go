// SPDX-License-Identifier: Apache-2.0

package identity_test

import (
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/nexops-one/compliance-engine/pkg/access"
	"github.com/nexops-one/compliance-engine/pkg/extension"
	"github.com/nexops-one/compliance-engine/pkg/identity"
	"github.com/nexops-one/compliance-engine/pkg/store"
	"github.com/nexops-one/compliance-engine/pkg/store/memory"
	"github.com/nexops-one/compliance-engine/sdk/adapter"
)

// signInUser creates a user with a password and the given roles in scope.
func signInUser(t *testing.T, s *identity.Service, email string, rs ...access.Role) string {
	t.Helper()
	if _, err := s.AddMember(ctx, scope, email, rs); err != nil {
		t.Fatal(err)
	}
	pw, err := s.ResetPassword(ctx, scope.TenantID, email)
	if err != nil {
		t.Fatal(err)
	}
	return pw
}

func tenantChain(t *testing.T, st *memory.Store) []string {
	t.Helper()
	evs, err := st.AuditEvents(ctx, adapter.Scope{TenantID: scope.TenantID}, store.AuditQuery{})
	if err != nil {
		t.Fatal(err)
	}
	out := []string{}
	for _, e := range evs {
		out = append(out, e.Action)
	}
	return out
}

func TestSignInAndSessionPrincipal(t *testing.T) {
	s, st, _ := newService(t)
	pw := signInUser(t, s, "anna@example.com", access.RoleOwner, access.RoleApprover)
	in, err := s.SignIn(ctx, "acme", " Anna@Example.com ", pw)
	if err != nil {
		t.Fatal(err)
	}
	if in.Value == "" || in.Session.WorkspaceID != scope.WorkspaceID || len(in.Workspaces) != 1 || !in.Session.ExpiresAt.Equal(t0.Add(identity.DefaultSessionMax)) {
		t.Fatalf("signed in = %+v", in)
	}
	if strings.Contains(in.Session.Hash, in.Value) || in.Session.Hash == in.Value {
		t.Fatal("the session value must not be stored")
	}
	p, se, err := s.SessionPrincipal(ctx, in.Value)
	want := extension.Principal{Scope: scope, Actor: "user:" + in.Session.UserID, Kind: extension.ActorUser, Roles: []access.Role{access.RoleOwner, access.RoleApprover}}
	if err != nil || !reflect.DeepEqual(p, want) || se.UserID != in.Session.UserID {
		t.Fatalf("principal = %+v, %v", p, err)
	}
	if got := tenantChain(t, st); got[len(got)-1] != "login.success" {
		t.Fatalf("chain = %v", got)
	}
	if err := s.SignOut(ctx, in.Value); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.SessionPrincipal(ctx, in.Value); !errors.Is(err, identity.ErrSessionEnded) {
		t.Fatalf("after sign-out: %v", err)
	}
	if got := tenantChain(t, st); got[len(got)-1] != "logout" {
		t.Fatalf("chain = %v", got)
	}
}

func TestSignInFailures(t *testing.T) {
	s, st, _ := newService(t)
	pw := signInUser(t, s, "anna@example.com", access.RoleOwner)
	if _, err := s.AddMember(ctx, scope, "nopass@example.com", roles(access.RoleOwner)); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.CreateUser(ctx, "acme", "lonely@example.com"); err != nil {
		t.Fatal(err)
	}
	lonelyPW, _ := s.ResetPassword(ctx, "acme", "lonely@example.com")
	before := len(tenantChain(t, st))
	cases := []struct {
		tenant, email, pw string
		want              error
		audited           bool
	}{
		{"acme", "anna@example.com", "wrong", identity.ErrSignInFailed, true},
		{"acme", "nopass@example.com", "", identity.ErrSignInFailed, true},
		{"acme", "lonely@example.com", lonelyPW, identity.ErrNoWorkspace, true},
		{"acme", "nobody@example.com", pw, identity.ErrSignInFailed, false},
		{"other", "anna@example.com", pw, identity.ErrSignInFailed, false},
		{"acme", "not an email", pw, identity.ErrSignInFailed, false},
	}
	audited := 0
	for _, c := range cases {
		if _, err := s.SignIn(ctx, c.tenant, c.email, c.pw); !errors.Is(err, c.want) {
			t.Errorf("%s/%s: %v, want %v", c.tenant, c.email, err, c.want)
		}
		if c.audited {
			audited++
		}
	}
	got := tenantChain(t, st)[before:]
	if len(got) != audited {
		t.Fatalf("audited %v, want %d login.failure events", got, audited)
	}
	for _, a := range got {
		if a != "login.failure" {
			t.Fatalf("chain = %v", got)
		}
	}
	if evs, _ := st.AuditEvents(ctx, adapter.Scope{TenantID: "other"}, store.AuditQuery{}); len(evs) != 0 {
		t.Fatal("an unknown tenant must not get an audit chain")
	}
}

func TestSessionIdleAndAbsoluteTimeout(t *testing.T) {
	st := memory.New()
	c := &clock{now: t0}
	s := identity.New(st, identity.Options{Clock: c.Now, SessionIdle: 30 * time.Minute, SessionMax: 2 * time.Hour})
	pw := signInUser(t, s, "anna@example.com", access.RoleOwner)

	in, _ := s.SignIn(ctx, "acme", "anna@example.com", pw)
	for i := 0; i < 3; i++ { // activity every 25 minutes keeps the session alive
		c.now = c.now.Add(25 * time.Minute)
		if _, _, err := s.SessionPrincipal(ctx, in.Value); err != nil {
			t.Fatalf("active session refused at %v: %v", c.now, err)
		}
	}
	c.now = c.now.Add(31 * time.Minute)
	if _, _, err := s.SessionPrincipal(ctx, in.Value); !errors.Is(err, identity.ErrSessionEnded) {
		t.Fatalf("idle session: %v", err)
	}
	if _, err := st.SessionByHash(ctx, in.Session.Hash); !errors.Is(err, store.ErrNotFound) {
		t.Fatal("an idle session is deleted")
	}

	c.now = t0
	in, _ = s.SignIn(ctx, "acme", "anna@example.com", pw)
	for c.now.Before(t0.Add(2*time.Hour - 20*time.Minute)) {
		c.now = c.now.Add(20 * time.Minute)
		if _, _, err := s.SessionPrincipal(ctx, in.Value); err != nil {
			t.Fatal(err)
		}
	}
	c.now = t0.Add(2 * time.Hour)
	if _, _, err := s.SessionPrincipal(ctx, in.Value); !errors.Is(err, identity.ErrSessionEnded) {
		t.Fatalf("absolute expiry: %v", err)
	}
}

func TestRemovedMemberLosesSession(t *testing.T) {
	s, _, _ := newService(t)
	signInUser(t, s, "root@example.com", access.RoleAdmin)
	pw := signInUser(t, s, "anna@example.com", access.RoleOwner, access.RoleApprover)
	in, _ := s.SignIn(ctx, "acme", "anna@example.com", pw)
	if _, err := s.AddMember(ctx, scope, "anna@example.com", roles(access.RoleAuditor)); err != nil {
		t.Fatal(err)
	}
	if p, _, err := s.SessionPrincipal(ctx, in.Value); err != nil || !reflect.DeepEqual(p.Roles, roles(access.RoleAuditor)) {
		t.Fatalf("role change takes effect at once: %+v, %v", p, err)
	}
	if err := s.RemoveMember(ctx, admin, "anna@example.com"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.SessionPrincipal(ctx, in.Value); !errors.Is(err, identity.ErrSessionEnded) {
		t.Fatalf("removed member: %v", err)
	}
}

func TestSwitchWorkspace(t *testing.T) {
	s, _, _ := newService(t)
	pw := signInUser(t, s, "anna@example.com", access.RoleOwner)
	if _, err := s.AddMember(ctx, other, "anna@example.com", roles(access.RoleAuditor)); err != nil {
		t.Fatal(err)
	}
	in, _ := s.SignIn(ctx, "acme", "anna@example.com", pw)
	if len(in.Workspaces) != 2 || in.Session.WorkspaceID != "ws-1" {
		t.Fatalf("workspaces = %+v", in.Workspaces)
	}
	if err := s.SwitchWorkspace(ctx, in.Value, "ws-2"); err != nil {
		t.Fatal(err)
	}
	if p, _, err := s.SessionPrincipal(ctx, in.Value); err != nil || p.Scope != other || !reflect.DeepEqual(p.Roles, roles(access.RoleAuditor)) {
		t.Fatalf("after switch = %+v, %v", p, err)
	}
	if err := s.SwitchWorkspace(ctx, in.Value, "ws-3"); !errors.Is(err, identity.ErrForbidden) {
		t.Fatalf("switch to a foreign workspace: %v", err)
	}
}

func TestPurgeSessions(t *testing.T) {
	s, st, c := newService(t)
	pw := signInUser(t, s, "anna@example.com", access.RoleOwner)
	old, _ := s.SignIn(ctx, "acme", "anna@example.com", pw)
	c.now = c.now.Add(time.Hour)
	fresh, _ := s.SignIn(ctx, "acme", "anna@example.com", pw)
	if n, err := s.PurgeSessions(ctx); err != nil || n != 1 {
		t.Fatalf("purged %d, %v", n, err)
	}
	if _, err := st.SessionByHash(ctx, old.Session.Hash); !errors.Is(err, store.ErrNotFound) {
		t.Fatal("idle session kept")
	}
	if _, err := st.SessionByHash(ctx, fresh.Session.Hash); err != nil {
		t.Fatal("fresh session purged")
	}
}
