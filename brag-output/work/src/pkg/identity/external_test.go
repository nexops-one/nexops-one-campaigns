// SPDX-License-Identifier: Apache-2.0

package identity_test

import (
	"errors"
	"reflect"
	"testing"

	"github.com/nexops-one/compliance-engine/pkg/access"
	"github.com/nexops-one/compliance-engine/pkg/extension"
	"github.com/nexops-one/compliance-engine/pkg/identity"
	"github.com/nexops-one/compliance-engine/pkg/store"
	"github.com/nexops-one/compliance-engine/pkg/store/memory"
	"github.com/nexops-one/compliance-engine/sdk/adapter"
)

func workspaceChain(t *testing.T, st *memory.Store, sc adapter.Scope) []string {
	t.Helper()
	evs, err := st.AuditEvents(ctx, sc, store.AuditQuery{})
	if err != nil {
		t.Fatal(err)
	}
	out := []string{}
	for _, e := range evs {
		out = append(out, e.Action)
	}
	return out
}

func TestSyncExternalUser(t *testing.T) {
	s, st, _ := newService(t)
	anna := identity.ExternalUser{ID: "nexops:42", Email: "Anna@Example.com"}
	p, err := s.SyncExternalUser(ctx, scope, anna, roles(access.RoleOwner))
	if err != nil {
		t.Fatal(err)
	}
	u, err := st.UserByExternalID(ctx, scope.TenantID, "nexops:42")
	if err != nil || u.Email != "anna@example.com" || u.PasswordHash != "" {
		t.Fatalf("user = %+v, %v", u, err)
	}
	want := extension.Principal{Scope: scope, Actor: "user:" + u.ID, Kind: extension.ActorUser, Roles: roles(access.RoleOwner)}
	if !reflect.DeepEqual(p, want) {
		t.Fatalf("principal = %+v, want %+v", p, want)
	}
	if got := tenantChain(t, st); !reflect.DeepEqual(got, []string{"user.create"}) {
		t.Fatalf("tenant chain = %v", got)
	}
	evs, _ := st.AuditEvents(ctx, scope, store.AuditQuery{})
	if len(evs) != 1 || evs[0].Action != "member.sync" || evs[0].Actor != identity.HostActor || evs[0].ActorKind != string(extension.ActorSystem) {
		t.Fatalf("workspace events = %+v", evs)
	}

	// The same sync again writes nothing.
	if p2, err := s.SyncExternalUser(ctx, scope, anna, roles(access.RoleOwner)); err != nil || !reflect.DeepEqual(p2, want) {
		t.Fatalf("second sync = %+v, %v", p2, err)
	}
	if len(tenantChain(t, st)) != 1 || len(workspaceChain(t, st, scope)) != 1 {
		t.Fatal("an unchanged sync must not write")
	}

	// Email and role changes are written and audited once each.
	anna.Email = "anna@new.example"
	p3, err := s.SyncExternalUser(ctx, scope, anna, roles(access.RoleApprover, access.RoleOwner))
	if err != nil || p3.Actor != want.Actor || !reflect.DeepEqual(p3.Roles, roles(access.RoleOwner, access.RoleApprover)) {
		t.Fatalf("changed sync = %+v, %v", p3, err)
	}
	if got := tenantChain(t, st); !reflect.DeepEqual(got, []string{"user.create", "user.email"}) {
		t.Fatalf("tenant chain = %v", got)
	}
	if got := workspaceChain(t, st, scope); !reflect.DeepEqual(got, []string{"member.sync", "member.sync"}) {
		t.Fatalf("workspace chain = %v", got)
	}
	if _, err := s.SyncExternalUser(ctx, scope, anna, roles(access.RoleOwner, access.RoleApprover)); err != nil || len(workspaceChain(t, st, scope)) != 2 {
		t.Fatalf("role order must not count as a change: %v", err)
	}

	// Memberships are per workspace; the user is the same.
	po, err := s.SyncExternalUser(ctx, other, anna, roles(access.RoleAuditor))
	if err != nil || po.Actor != want.Actor || !reflect.DeepEqual(po.Roles, roles(access.RoleAuditor)) {
		t.Fatalf("other workspace = %+v, %v", po, err)
	}
	if m, _ := st.Member(ctx, scope, u.ID); !reflect.DeepEqual(m.Roles, roles(access.RoleOwner, access.RoleApprover)) {
		t.Fatalf("first workspace roles changed: %v", m.Roles)
	}

	// External users have no password: console sign-in fails.
	if _, err := s.SignIn(ctx, scope.TenantID, "anna@new.example", ""); !errors.Is(err, identity.ErrSignInFailed) {
		t.Fatalf("sign-in without password = %v", err)
	}
}

func TestSyncExternalUserAuditsTheCaller(t *testing.T) {
	s, st, _ := newService(t)
	caller := extension.Principal{Scope: scope, Actor: "user:admin", Kind: extension.ActorUser, Roles: roles(access.RoleAdmin)}
	if _, err := s.SyncExternalUser(extension.WithPrincipal(ctx, caller), scope, identity.ExternalUser{ID: "nexops:1", Email: "a@example.com"}, roles(access.RoleOwner)); err != nil {
		t.Fatal(err)
	}
	evs, _ := st.AuditEvents(ctx, scope, store.AuditQuery{})
	if len(evs) != 1 || evs[0].Actor != "user:admin" || evs[0].ActorKind != string(extension.ActorUser) {
		t.Fatalf("events = %+v", evs)
	}
}

func TestSyncExternalUserRefusals(t *testing.T) {
	s, st, _ := newService(t)
	if _, err := s.AddMember(ctx, scope, "local@example.com", roles(access.RoleAdmin)); err != nil {
		t.Fatal(err)
	}
	ok := identity.ExternalUser{ID: "nexops:1", Email: "a@example.com"}
	cases := []struct {
		name  string
		scope adapter.Scope
		user  identity.ExternalUser
		roles []access.Role
		want  error
	}{
		{"no external ID", scope, identity.ExternalUser{Email: "a@example.com"}, roles(access.RoleOwner), identity.ErrInvalidRequest},
		{"bad email", scope, identity.ExternalUser{ID: "nexops:1", Email: "not an email"}, roles(access.RoleOwner), identity.ErrInvalidEmail},
		{"no roles", scope, ok, nil, identity.ErrInvalidRequest},
		{"unknown role", scope, ok, roles("superuser"), identity.ErrInvalidRequest},
		{"no workspace", adapter.Scope{TenantID: "acme"}, ok, roles(access.RoleOwner), identity.ErrInvalidRequest},
		{"email of a local user", scope, identity.ExternalUser{ID: "nexops:2", Email: "local@example.com"}, roles(access.RoleOwner), identity.ErrEmailInUse},
	}
	for _, c := range cases {
		if _, err := s.SyncExternalUser(ctx, c.scope, c.user, c.roles); !errors.Is(err, c.want) {
			t.Errorf("%s: err = %v, want %v", c.name, err, c.want)
		}
	}
	if _, err := st.UserByExternalID(ctx, scope.TenantID, "nexops:2"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("a refused sync must not create the user: %v", err)
	}

	// Moving to an email another user has is refused too.
	if _, err := s.SyncExternalUser(ctx, scope, ok, roles(access.RoleOwner)); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SyncExternalUser(ctx, scope, identity.ExternalUser{ID: "nexops:1", Email: "local@example.com"}, roles(access.RoleOwner)); !errors.Is(err, identity.ErrEmailInUse) {
		t.Fatalf("email change onto a local user = %v", err)
	}
}

func TestSyncExternalUserKeepsTheLastAdmin(t *testing.T) {
	s, st, _ := newService(t)
	boss := identity.ExternalUser{ID: "nexops:1", Email: "boss@example.com"}
	if _, err := s.SyncExternalUser(ctx, scope, boss, roles(access.RoleAdmin, access.RoleApprover)); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SyncExternalUser(ctx, scope, boss, roles(access.RoleOwner)); !errors.Is(err, identity.ErrLastAdmin) {
		t.Fatalf("demoting the last admin = %v", err)
	}
	u, _ := st.UserByExternalID(ctx, scope.TenantID, "nexops:1")
	if m, _ := st.Member(ctx, scope, u.ID); !access.Contains(m.Roles, access.RoleAdmin) {
		t.Fatalf("roles after refusal = %v", m.Roles)
	}
	if _, err := s.SyncExternalUser(ctx, scope, identity.ExternalUser{ID: "nexops:2", Email: "second@example.com"}, roles(access.RoleAdmin)); err != nil {
		t.Fatal(err)
	}
	if p, err := s.SyncExternalUser(ctx, scope, boss, roles(access.RoleOwner)); err != nil || !reflect.DeepEqual(p.Roles, roles(access.RoleOwner)) {
		t.Fatalf("demotion with another admin = %+v, %v", p, err)
	}
}

func TestSyncExternalUserDisabled(t *testing.T) {
	s, st, _ := newService(t)
	if err := st.CreateUser(ctx, store.User{TenantID: "acme", ID: "usr-off", Email: "off@example.com", ExternalID: "nexops:9", Disabled: true, CreatedAt: t0},
		store.AuditEvent{Scope: adapter.Scope{TenantID: "acme"}, At: t0, Actor: "test", ActorKind: "system", Action: "user.create"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SyncExternalUser(ctx, scope, identity.ExternalUser{ID: "nexops:9", Email: "off@example.com"}, roles(access.RoleOwner)); !errors.Is(err, identity.ErrForbidden) {
		t.Fatalf("disabled user = %v", err)
	}
}

func TestSyncExternalUserConcurrentFirstSight(t *testing.T) {
	s, st, _ := newService(t)
	ext := identity.ExternalUser{ID: "nexops:5", Email: "five@example.com"}
	errs := make(chan error, 8)
	actors := make(chan string, 8)
	for i := 0; i < 8; i++ {
		go func() {
			p, err := s.SyncExternalUser(ctx, scope, ext, roles(access.RoleOwner))
			errs <- err
			actors <- p.Actor
		}()
	}
	first := ""
	for i := 0; i < 8; i++ {
		if err := <-errs; err != nil {
			t.Fatalf("concurrent sync: %v", err)
		}
		if a := <-actors; first == "" {
			first = a
		} else if a != first {
			t.Fatalf("actors differ: %s, %s", first, a)
		}
	}
	if users, _ := st.Users(ctx, scope.TenantID); len(users) != 1 {
		t.Fatalf("users = %+v", users)
	}
}
