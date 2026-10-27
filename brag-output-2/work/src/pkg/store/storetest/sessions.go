// SPDX-License-Identifier: Apache-2.0

package storetest

import (
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/nexops-one/compliance-engine/pkg/access"
	"github.com/nexops-one/compliance-engine/pkg/store"
	"github.com/nexops-one/compliance-engine/sdk/adapter"
)

func session(hash, userID, workspace string) store.Session {
	return store.Session{Hash: hash, TenantID: scopeA.TenantID, UserID: userID, WorkspaceID: workspace,
		CreatedAt: at, LastSeenAt: at, ExpiresAt: at.Add(12 * time.Hour)}
}

func runSessions(t *testing.T, newStore func(t *testing.T) store.Store) {
	t.Run("user memberships", func(t *testing.T) {
		s := newStore(t)
		mustUser(t, s, user("u1", "anna@example.com"))
		mustUser(t, s, user("u2", "zoe@example.com"))
		mustMember(t, s, scopeB, "u1", access.RoleAuditor)
		mustMember(t, s, scopeA, "u1", access.RoleOwner, access.RoleAdmin)
		mustMember(t, s, scopeA, "u2", access.RoleOwner)
		ms, err := s.UserMembers(ctx, scopeA.TenantID, "u1")
		if err != nil || len(ms) != 2 || ms[0].Scope != scopeA || ms[1].Scope != scopeB || ms[0].Email != "anna@example.com" ||
			!reflect.DeepEqual(ms[0].Roles, []access.Role{access.RoleOwner, access.RoleAdmin}) {
			t.Fatalf("memberships = %+v, %v", ms, err)
		}
		if ms, err := s.UserMembers(ctx, "tenant-b", "u1"); err != nil || len(ms) != 0 {
			t.Fatalf("other tenant = %+v, %v", ms, err)
		}
	})

	t.Run("session lifecycle", func(t *testing.T) {
		s := newStore(t)
		mustUser(t, s, user("u1", "anna@example.com"))
		tenant := tenantScope(scopeA.TenantID)
		if err := s.CreateSession(ctx, session("h1", "u1", scopeA.WorkspaceID), Event(tenant, "login.success")); err != nil {
			t.Fatal(err)
		}
		if err := s.CreateSession(ctx, session("h1", "u1", scopeA.WorkspaceID), Event(tenant, "login.success")); !errors.Is(err, store.ErrExists) {
			t.Fatalf("duplicate hash: %v", err)
		}
		got, err := s.SessionByHash(ctx, "h1")
		if err != nil || got.UserID != "u1" || got.WorkspaceID != scopeA.WorkspaceID || !got.ExpiresAt.Equal(at.Add(12*time.Hour)) || got.Hash != "h1" {
			t.Fatalf("session = %+v, %v", got, err)
		}
		later := at.Add(5 * time.Minute)
		if err := s.TouchSession(ctx, "h1", later); err != nil {
			t.Fatal(err)
		}
		if err := s.SetSessionWorkspace(ctx, "h1", scopeB.WorkspaceID); err != nil {
			t.Fatal(err)
		}
		if got, _ := s.SessionByHash(ctx, "h1"); !got.LastSeenAt.Equal(later) || got.WorkspaceID != scopeB.WorkspaceID {
			t.Fatalf("after touch = %+v", got)
		}
		for _, err := range []error{s.TouchSession(ctx, "nope", later), s.SetSessionWorkspace(ctx, "nope", "x")} {
			if !errors.Is(err, store.ErrNotFound) {
				t.Fatalf("unknown session: %v", err)
			}
		}
		if err := s.DeleteSession(ctx, "h1", Event(tenant, "logout")); err != nil {
			t.Fatal(err)
		}
		if err := s.DeleteSession(ctx, "h1", Event(tenant, "logout")); err != nil {
			t.Fatal(err)
		}
		if _, err := s.SessionByHash(ctx, "h1"); !errors.Is(err, store.ErrNotFound) {
			t.Fatalf("deleted session: %v", err)
		}
		if got := ChainOf(t, s, tenant); !reflect.DeepEqual(got, []string{"user.create", "login.success", "logout"}) {
			t.Fatalf("chain = %v (a failed create and a second delete append nothing)", got)
		}
	})

	t.Run("session create is atomic with its audit event", func(t *testing.T) {
		s := newStore(t)
		mustUser(t, s, user("u1", "anna@example.com"))
		if err := s.CreateSession(ctx, session("h1", "u1", scopeA.WorkspaceID), Invalid(tenantScope(scopeA.TenantID))); err == nil {
			t.Fatal("invalid event accepted")
		}
		if _, err := s.SessionByHash(ctx, "h1"); !errors.Is(err, store.ErrNotFound) {
			t.Fatalf("session stored without its event: %v", err)
		}
	})

	t.Run("expired sessions", func(t *testing.T) {
		s := newStore(t)
		mustUser(t, s, user("u1", "anna@example.com"))
		ev := Event(tenantScope(scopeA.TenantID), "login.success")
		fresh := session("fresh", "u1", scopeA.WorkspaceID)
		fresh.LastSeenAt = at.Add(time.Hour)
		idle := session("idle", "u1", scopeA.WorkspaceID)
		old := session("old", "u1", scopeA.WorkspaceID)
		old.LastSeenAt, old.ExpiresAt = at.Add(time.Hour), at.Add(time.Hour)
		for _, se := range []store.Session{fresh, idle, old} {
			if err := s.CreateSession(ctx, se, ev); err != nil {
				t.Fatal(err)
			}
		}
		n, err := s.DeleteExpiredSessions(ctx, at.Add(time.Hour), at.Add(30*time.Minute))
		if err != nil || n != 2 {
			t.Fatalf("deleted = %d, %v", n, err)
		}
		if _, err := s.SessionByHash(ctx, "fresh"); err != nil {
			t.Fatalf("fresh session deleted: %v", err)
		}
		for _, h := range []string{"idle", "old"} {
			if _, err := s.SessionByHash(ctx, h); !errors.Is(err, store.ErrNotFound) {
				t.Fatalf("%s kept: %v", h, err)
			}
		}
	})
}

// sessionFor creates a session of user u1 in scope (tenant deletion test).
func sessionFor(t *testing.T, s store.Store, sc adapter.Scope) {
	t.Helper()
	se := session("sess-"+sc.TenantID, "u1", sc.WorkspaceID)
	se.TenantID = sc.TenantID
	if err := s.CreateSession(ctx, se, Event(sc, "login.success")); err != nil {
		t.Fatal(err)
	}
}
