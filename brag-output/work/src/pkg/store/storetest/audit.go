// SPDX-License-Identifier: Apache-2.0

package storetest

import (
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"testing"

	"github.com/nexops-one/compliance-engine/pkg/audit"
	"github.com/nexops-one/compliance-engine/pkg/store"
	"github.com/nexops-one/compliance-engine/sdk/adapter"
)

// Event returns a valid, unsealed audit event of scope.
func Event(scope adapter.Scope, action string) store.AuditEvent {
	return store.AuditEvent{
		Scope: scope, At: at, Actor: "user:u1", ActorKind: "user", Action: action,
		TargetType: "test", TargetID: action, Details: json.RawMessage(`{"n": 1}`),
	}
}

// Invalid is an event every store must refuse to append (no action).
func Invalid(scope adapter.Scope) store.AuditEvent {
	ev := Event(scope, "")
	return ev
}

func actions(evs []store.AuditEvent) []string {
	out := []string{}
	for _, e := range evs {
		out = append(out, e.Action)
	}
	return out
}

// ChainOf returns the actions of scope's chain, failing the test when the
// chain does not verify.
func ChainOf(t *testing.T, s store.Store, scope adapter.Scope) []string {
	t.Helper()
	evs, err := s.AuditEvents(ctx, scope, store.AuditQuery{})
	if err != nil {
		t.Fatal(err)
	}
	if r := audit.Verify(evs); !r.OK {
		t.Fatalf("chain of %v does not verify: %+v", scope, r)
	}
	return actions(evs)
}

func runAudit(t *testing.T, newStore func(t *testing.T) store.Store) {
	t.Run("audit append and read back", func(t *testing.T) {
		s := newStore(t)
		if err := s.AppendAudit(ctx, Event(scopeA, "a1"), Event(scopeA, "a2")); err != nil {
			t.Fatal(err)
		}
		if err := s.AppendAudit(ctx, Event(scopeA, "a3")); err != nil {
			t.Fatal(err)
		}
		evs, err := s.AuditEvents(ctx, scopeA, store.AuditQuery{})
		if err != nil || len(evs) != 3 {
			t.Fatalf("events = %+v, %v", evs, err)
		}
		if evs[2].Seq != 3 || evs[2].PrevHash != evs[1].Hash || string(evs[0].Details) != `{"n":1}` || !evs[0].At.Equal(at) {
			t.Fatalf("sealed events: %+v", evs)
		}
		if r := audit.Verify(evs); !r.OK {
			t.Fatalf("verify: %+v", r)
		}
		want := evs[1]
		got, err := s.AuditEvents(ctx, scopeA, store.AuditQuery{AfterSeq: 1, Limit: 1})
		if err != nil || len(got) != 1 || !reflect.DeepEqual(got[0], want) {
			t.Fatalf("page = %+v, %v; want %+v", got, err, want)
		}
		if err := s.AppendAudit(ctx); err != nil {
			t.Fatalf("appending nothing: %v", err)
		}
	})

	t.Run("audit chains are per scope", func(t *testing.T) {
		s := newStore(t)
		tenant := adapter.Scope{TenantID: scopeA.TenantID}
		for _, sc := range []adapter.Scope{scopeA, scopeB, tenant} {
			if err := s.AppendAudit(ctx, Event(sc, "x")); err != nil {
				t.Fatal(err)
			}
		}
		for _, sc := range []adapter.Scope{scopeA, scopeB, tenant} {
			evs, err := s.AuditEvents(ctx, sc, store.AuditQuery{})
			if err != nil || len(evs) != 1 || evs[0].Seq != 1 || evs[0].PrevHash != "" || evs[0].Scope != sc {
				t.Fatalf("%v: %+v, %v", sc, evs, err)
			}
		}
		scopes, err := s.AuditScopes(ctx, scopeA.TenantID)
		if err != nil || !reflect.DeepEqual(scopes, []adapter.Scope{tenant, scopeA, scopeB}) {
			t.Fatalf("scopes = %v, %v", scopes, err)
		}
		if other, _ := s.AuditScopes(ctx, "other"); len(other) != 0 {
			t.Fatalf("other tenant: %v", other)
		}
	})

	t.Run("audit append refuses invalid events", func(t *testing.T) {
		s := newStore(t)
		if err := s.AppendAudit(ctx, Event(scopeA, "ok"), Invalid(scopeA)); err == nil {
			t.Fatal("an invalid event must fail the append")
		}
		if got := ChainOf(t, s, scopeA); len(got) != 0 {
			t.Fatalf("a failed append must write nothing: %v", got)
		}
		if err := s.AppendAudit(ctx, Event(scopeA, "a"), Event(scopeB, "b")); err == nil {
			t.Fatal("mixed scopes must fail")
		}
	})

	t.Run("commit appends its audit events atomically", func(t *testing.T) {
		s := newStore(t)
		c := commit(scopeA, 0, "ing-1", create(version("ict_provider", `["P1"]`, "one")))
		c.Audit = []store.AuditEvent{Event(scopeA, "ingestion.commit")}
		if _, err := s.Commit(ctx, c); err != nil {
			t.Fatal(err)
		}
		if got := ChainOf(t, s, scopeA); !reflect.DeepEqual(got, []string{"ingestion.commit"}) {
			t.Fatalf("chain = %v", got)
		}
		bad := commit(scopeA, 1, "ing-2", create(version("ict_provider", `["P2"]`, "two")))
		bad.Audit = []store.AuditEvent{Invalid(scopeA)}
		if _, err := s.Commit(ctx, bad); err == nil {
			t.Fatal("a commit whose audit append fails must fail")
		}
		if rev, err := s.CurrentRevision(ctx, scopeA); err != nil || rev.Number != 1 {
			t.Fatalf("the failed commit must not create a revision: %+v, %v", rev, err)
		}
		if _, err := s.Ingestion(ctx, scopeA, "ing-2"); !errors.Is(err, store.ErrNotFound) {
			t.Fatalf("the failed commit must not save its ingestion: %v", err)
		}
		if got := ChainOf(t, s, scopeA); len(got) != 1 {
			t.Fatalf("chain = %v", got)
		}
	})

	t.Run("audit appends many events", func(t *testing.T) {
		s := newStore(t)
		var evs []store.AuditEvent
		for i := 0; i < 25; i++ {
			evs = append(evs, Event(scopeA, fmt.Sprintf("e%02d", i)))
		}
		if err := s.AppendAudit(ctx, evs...); err != nil {
			t.Fatal(err)
		}
		if got := ChainOf(t, s, scopeA); len(got) != 25 || got[24] != "e24" {
			t.Fatalf("chain = %v", got)
		}
	})
}
