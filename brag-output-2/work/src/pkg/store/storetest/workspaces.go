// SPDX-License-Identifier: Apache-2.0

package storetest

import (
	"errors"
	"fmt"
	"reflect"
	"sync"
	"testing"

	"github.com/nexops-one/compliance-engine/pkg/access"
	"github.com/nexops-one/compliance-engine/pkg/store"
	"github.com/nexops-one/compliance-engine/sdk/adapter"
)

var systemChain = adapter.Scope{TenantID: "_system"}

func workspace(sc adapter.Scope) store.Workspace {
	return store.Workspace{Scope: sc, CreatedAt: at, CreatedBy: "user:u1"}
}

func runWorkspaces(t *testing.T, newStore func(t *testing.T) store.Store) {
	t.Run("register, read and list workspaces", func(t *testing.T) {
		s := newStore(t)
		if _, err := s.Workspace(ctx, scopeA); !errors.Is(err, store.ErrNotFound) {
			t.Fatalf("unregistered = %v", err)
		}
		w, created, err := s.RegisterWorkspace(ctx, workspace(scopeA), 0, Event(systemChain, "workspace.register"))
		if err != nil || !created || w.Status != store.WorkspaceActive || !w.StatusAt.Equal(at) || w.StatusBy != "user:u1" {
			t.Fatalf("register = %+v %v %v", w, created, err)
		}
		// A second registration keeps the first and appends nothing.
		again := workspace(scopeA)
		again.CreatedBy = "user:other"
		w2, created, err := s.RegisterWorkspace(ctx, again, 0, Event(systemChain, "workspace.register"))
		if err != nil || created || w2.CreatedBy != "user:u1" {
			t.Fatalf("second register = %+v %v %v", w2, created, err)
		}
		if got := ChainOf(t, s, systemChain); !reflect.DeepEqual(got, []string{"workspace.register"}) {
			t.Fatalf("system chain = %v", got)
		}
		other := adapter.Scope{TenantID: "tenant-b", WorkspaceID: "ws-1"}
		for _, sc := range []adapter.Scope{other, scopeB} {
			if _, _, err := s.RegisterWorkspace(ctx, workspace(sc), 0); err != nil {
				t.Fatal(err)
			}
		}
		got, err := s.Workspace(ctx, scopeA)
		if err != nil || got.Scope != scopeA || !got.CreatedAt.Equal(at) {
			t.Fatalf("workspace = %+v %v", got, err)
		}
		all, _ := s.Workspaces(ctx, "")
		tenantA, _ := s.Workspaces(ctx, "tenant-a")
		if len(all) != 3 || all[0].Scope != scopeA || all[1].Scope != scopeB || all[2].Scope != other || len(tenantA) != 2 {
			t.Fatalf("all = %+v, tenant-a = %+v", all, tenantA)
		}
		if none, _ := s.Workspaces(ctx, "nobody"); len(none) != 0 {
			t.Fatalf("unknown tenant = %+v", none)
		}
	})

	t.Run("workspace limit, suspension and resumption", func(t *testing.T) {
		s := newStore(t)
		for _, sc := range []adapter.Scope{scopeA, scopeB} {
			if _, _, err := s.RegisterWorkspace(ctx, workspace(sc), 2); err != nil {
				t.Fatal(err)
			}
		}
		third := adapter.Scope{TenantID: "tenant-b", WorkspaceID: "ws-9"}
		if _, _, err := s.RegisterWorkspace(ctx, workspace(third), 2, Event(systemChain, "workspace.register")); !errors.Is(err, store.ErrWorkspaceLimit) {
			t.Fatalf("over the limit = %v", err)
		}
		if len(ChainOf(t, s, systemChain)) != 0 {
			t.Fatal("a refused registration must not be audited")
		}
		// An already registered workspace is returned even at the limit.
		if _, created, err := s.RegisterWorkspace(ctx, workspace(scopeA), 2); err != nil || created {
			t.Fatalf("existing at the limit = %v %v", created, err)
		}
		later := at.Add(1)
		w, err := s.SetWorkspaceStatus(ctx, scopeB, store.WorkspaceSuspended, later, "admin:ops", "unpaid", 2, Event(systemChain, "workspace.suspend"))
		if err != nil || w.Status != store.WorkspaceSuspended || w.Reason != "unpaid" || w.StatusBy != "admin:ops" || !w.StatusAt.Equal(later) {
			t.Fatalf("suspend = %+v %v", w, err)
		}
		// Suspension frees a slot; resuming then needs one.
		if _, _, err := s.RegisterWorkspace(ctx, workspace(third), 2); err != nil {
			t.Fatalf("register after suspension = %v", err)
		}
		if _, err := s.SetWorkspaceStatus(ctx, scopeB, store.WorkspaceActive, later, "admin:ops", "", 2); !errors.Is(err, store.ErrWorkspaceLimit) {
			t.Fatalf("resume over the limit = %v", err)
		}
		if _, err := s.SetWorkspaceStatus(ctx, scopeB, store.WorkspaceActive, later, "admin:ops", "", 0, Event(systemChain, "workspace.resume")); err != nil {
			t.Fatalf("resume without a limit = %v", err)
		}
		if got, _ := s.Workspace(ctx, scopeB); got.Status != store.WorkspaceActive || got.Reason != "" {
			t.Fatalf("resumed = %+v", got)
		}
		if _, err := s.SetWorkspaceStatus(ctx, adapter.Scope{TenantID: "x", WorkspaceID: "y"}, store.WorkspaceSuspended, later, "a", "", 0); !errors.Is(err, store.ErrNotFound) {
			t.Fatalf("unknown workspace = %v", err)
		}
		if got := ChainOf(t, s, systemChain); !reflect.DeepEqual(got, []string{"workspace.suspend", "workspace.resume"}) {
			t.Fatalf("system chain = %v", got)
		}
	})

	t.Run("concurrent registrations respect the limit", func(t *testing.T) {
		s := newStore(t)
		const n, limit = 12, 4
		var wg sync.WaitGroup
		errs := make([]error, n)
		for i := 0; i < n; i++ {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				sc := adapter.Scope{TenantID: "tenant-c", WorkspaceID: fmt.Sprintf("ws-%02d", i)}
				_, _, errs[i] = s.RegisterWorkspace(ctx, workspace(sc), limit)
			}(i)
		}
		wg.Wait()
		ok := 0
		for _, err := range errs {
			switch {
			case err == nil:
				ok++
			case !errors.Is(err, store.ErrWorkspaceLimit):
				t.Fatalf("unexpected error: %v", err)
			}
		}
		if all, _ := s.Workspaces(ctx, ""); ok != limit || len(all) != limit {
			t.Fatalf("%d registrations succeeded, %d stored; want %d", ok, len(all), limit)
		}
	})

	t.Run("delete workspace data", func(t *testing.T) {
		s := newStore(t)
		u := user("u1", "a@example.com")
		if err := s.CreateUser(ctx, u, Event(adapter.Scope{TenantID: scopeA.TenantID}, "user.create")); err != nil {
			t.Fatal(err)
		}
		for _, sc := range []adapter.Scope{scopeA, scopeB} {
			if _, _, err := s.RegisterWorkspace(ctx, workspace(sc), 0); err != nil {
				t.Fatal(err)
			}
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
			if err := s.PutMember(ctx, store.Member{Scope: sc, UserID: "u1", Roles: []access.Role{access.RoleOwner}, UpdatedAt: at}, Event(sc, "member.set")); err != nil {
				t.Fatal(err)
			}
			tok := token("tok-"+sc.WorkspaceID, sc, "u1", access.RoleOwner)
			if err := s.CreateToken(ctx, tok, Event(sc, "token.create")); err != nil {
				t.Fatal(err)
			}
		}
		n, err := s.DeleteWorkspaceData(ctx, scopeA, Event(systemChain, "workspace.delete"))
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
		if _, err := s.TokenByHash(ctx, "hash-tok-ws-1"); !errors.Is(err, store.ErrNotFound) {
			t.Fatal("tokens remain")
		}
		if ms, _ := s.UserMembers(ctx, scopeA.TenantID, "u1"); len(ms) != 1 || ms[0].Scope != scopeB {
			t.Fatalf("memberships = %+v", ms)
		}
		if _, err := s.Workspace(ctx, scopeA); !errors.Is(err, store.ErrNotFound) {
			t.Fatal("the registration remains")
		}
		for _, check := range []func() error{
			func() error { _, err := s.Evidence(ctx, scopeB, "ev-1"); return err },
			func() error { _, err := s.TokenByHash(ctx, "hash-tok-ws-2"); return err },
			func() error { _, err := s.Records(ctx, scopeB, 1); return err },
			func() error { _, err := s.Workspace(ctx, scopeB); return err },
			func() error { _, err := s.UserByEmail(ctx, scopeA.TenantID, "a@example.com"); return err },
		} {
			if err := check(); err != nil {
				t.Fatalf("another workspace or the tenant's users were touched: %v", err)
			}
		}
		if got := ChainOf(t, s, scopeA); len(got) == 0 {
			t.Fatal("the workspace audit chain must be kept")
		}
		if got := ChainOf(t, s, systemChain); !reflect.DeepEqual(got, []string{"workspace.delete"}) {
			t.Fatalf("system chain = %v", got)
		}
		// Tenant deletion removes the remaining registrations.
		if _, err := s.DeleteTenantData(ctx, scopeA.TenantID, Event(adapter.Scope{TenantID: scopeA.TenantID}, "tenant.delete")); err != nil {
			t.Fatal(err)
		}
		if ws, _ := s.Workspaces(ctx, scopeA.TenantID); len(ws) != 0 {
			t.Fatalf("registrations remain after tenant deletion: %+v", ws)
		}
	})
}
