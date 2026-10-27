// SPDX-License-Identifier: Apache-2.0

package storetest

import (
	"errors"
	"testing"

	"github.com/nexops-one/compliance-engine/pkg/access"
	"github.com/nexops-one/compliance-engine/pkg/store"
	"github.com/nexops-one/compliance-engine/sdk/adapter"
)

func runTenant(t *testing.T, newStore func(t *testing.T) store.Store) {
	t.Run("delete tenant data", func(t *testing.T) {
		s := newStore(t)
		other := adapter.Scope{TenantID: "tenant-b", WorkspaceID: "ws-1"}
		for _, sc := range []adapter.Scope{scopeA, other} {
			if _, err := s.Commit(ctx, commit(sc, 0, "ing-1", create(version("ict_provider", `["P1"]`, "one")))); err != nil {
				t.Fatal(err)
			}
			ev := evidenceDoc("ev-1", store.ControlRef{Catalog: "dora", ControlID: "c1"})
			ev.Scope = sc
			if _, err := s.PutEvidence(ctx, ev, 0); err != nil {
				t.Fatal(err)
			}
			a := assessment("c1")
			a.Scope = sc
			if _, err := s.SaveAssessment(ctx, a, 0, &store.Transition{Action: "assign", Actor: "u"}); err != nil {
				t.Fatal(err)
			}
			u := user("u1", "a@example.com")
			u.TenantID = sc.TenantID
			if err := s.CreateUser(ctx, u, Event(adapter.Scope{TenantID: sc.TenantID}, "user.create")); err != nil {
				t.Fatal(err)
			}
			if err := s.PutMember(ctx, store.Member{Scope: sc, UserID: "u1", Roles: []access.Role{access.RoleOwner}, UpdatedAt: at}, Event(sc, "member.set")); err != nil {
				t.Fatal(err)
			}
			tok := token("tok-"+sc.TenantID, sc, "u1", access.RoleOwner)
			if err := s.CreateToken(ctx, tok, Event(sc, "token.create")); err != nil {
				t.Fatal(err)
			}
			sessionFor(t, s, sc)
		}
		n, err := s.DeleteTenantData(ctx, "tenant-a", Event(adapter.Scope{TenantID: "tenant-a"}, "tenant.delete"))
		if err != nil || n == 0 {
			t.Fatalf("delete = %d %v", n, err)
		}
		if revs, _ := s.Revisions(ctx, scopeA); len(revs) != 0 {
			t.Fatal("revisions remain")
		}
		if _, err := s.Evidence(ctx, scopeA, "ev-1"); !errors.Is(err, store.ErrNotFound) {
			t.Fatal("evidence remains")
		}
		if h, _ := s.History(ctx, scopeA, "dora", "c1"); len(h) != 0 {
			t.Fatal("history remains")
		}
		if _, err := s.UserByEmail(ctx, "tenant-a", "a@example.com"); !errors.Is(err, store.ErrNotFound) {
			t.Fatal("users remain")
		}
		if _, err := s.TokenByHash(ctx, "hash-tok-tenant-a"); !errors.Is(err, store.ErrNotFound) {
			t.Fatal("tokens remain")
		}
		if _, err := s.SessionByHash(ctx, "sess-tenant-a"); !errors.Is(err, store.ErrNotFound) {
			t.Fatal("sessions remain")
		}
		for _, check := range []func() error{
			func() error { _, err := s.Evidence(ctx, other, "ev-1"); return err },
			func() error { _, err := s.UserByEmail(ctx, "tenant-b", "a@example.com"); return err },
			func() error { _, err := s.TokenByHash(ctx, "hash-tok-tenant-b"); return err },
			func() error { _, err := s.SessionByHash(ctx, "sess-tenant-b"); return err },
			func() error { _, err := s.Records(ctx, other, 1); return err },
		} {
			if err := check(); err != nil {
				t.Fatalf("another tenant was touched: %v", err)
			}
		}
		if got := ChainOf(t, s, adapter.Scope{TenantID: "tenant-a"}); len(got) < 2 || got[len(got)-1] != "tenant.delete" || got[len(got)-2] != "user.create" {
			t.Fatalf("the tenant audit chain is kept and records the deletion: %v", got)
		}
	})
}
