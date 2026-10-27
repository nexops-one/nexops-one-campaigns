// SPDX-License-Identifier: Apache-2.0

// Package storetest is the contract test suite every store implementation must pass.
package storetest

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/nexops-one/compliance-engine/pkg/store"
	"github.com/nexops-one/compliance-engine/sdk/adapter"
)

var (
	scopeA = adapter.Scope{TenantID: "tenant-a", WorkspaceID: "ws-1"}
	scopeB = adapter.Scope{TenantID: "tenant-a", WorkspaceID: "ws-2"}
	source = adapter.Source{System: "sheet", Adapter: "csv-import", AdapterVersion: "0.1.0"}
	at     = time.Date(2026, 1, 15, 9, 0, 0, 0, time.UTC)
)

func version(entity, key, name string) *store.RecordVersion {
	return &store.RecordVersion{
		Entity: entity, Key: key, Data: adapter.Record{"provider_id_code": key, "legal_name": name},
		Hash: "h-" + name, SchemaVersion: "0.1.0", Source: source, WrittenAt: at,
	}
}

func commit(scope adapter.Scope, expected int64, id string, changes ...store.Change) store.Commit {
	return store.Commit{
		Scope: scope, ExpectedRevision: expected, Kind: store.KindIngestion, At: at, Changes: changes,
		Ingestion: store.Ingestion{ID: id, Scope: scope, Source: source, Mode: adapter.ModeIncremental, Result: json.RawMessage(`{}`), CreatedAt: at},
	}
}

func create(v *store.RecordVersion) store.Change {
	return store.Change{Op: store.OpCreate, Entity: v.Entity, Key: v.Key, Version: v}
}

func keys(recs []store.RecordVersion) []string {
	out := []string{}
	for _, r := range recs {
		out = append(out, r.Entity+"/"+r.Key+"/"+r.Hash)
	}
	return out
}

// Run executes the contract against fresh stores from newStore.
func Run(t *testing.T, newStore func(t *testing.T) store.Store) {
	ctx := context.Background()

	t.Run("empty workspace", func(t *testing.T) {
		s := newStore(t)
		if _, err := s.CurrentRevision(ctx, scopeA); !errors.Is(err, store.ErrNotFound) {
			t.Fatalf("CurrentRevision err = %v", err)
		}
		recs, err := s.Records(ctx, scopeA, 0)
		if err != nil || len(recs) != 0 {
			t.Fatalf("Records(0) = %v, %v", recs, err)
		}
		if revs, err := s.Revisions(ctx, scopeA); err != nil || len(revs) != 0 {
			t.Fatalf("Revisions = %v, %v", revs, err)
		}
	})

	t.Run("commit and copy-on-write revisions", func(t *testing.T) {
		s := newStore(t)
		rev, err := s.Commit(ctx, commit(scopeA, 0, "ing-1",
			create(version("ict_provider", `["P2"]`, "two")),
			create(version("ict_provider", `["P1"]`, "one"))))
		if err != nil || rev.Number != 1 || rev.SnapshotID != "rev-1" || rev.IngestionID != "ing-1" || rev.Kind != store.KindIngestion {
			t.Fatalf("rev = %+v, %v", rev, err)
		}
		_, err = s.Commit(ctx, commit(scopeA, 1, "ing-2",
			store.Change{Op: store.OpUpdate, Entity: "ict_provider", Key: `["P1"]`, Version: version("ict_provider", `["P1"]`, "uno"), PreviousHash: "h-one"},
			store.Change{Op: store.OpDelete, Entity: "ict_provider", Key: `["P2"]`, PreviousHash: "h-two"}))
		if err != nil {
			t.Fatal(err)
		}
		r1, _ := s.Records(ctx, scopeA, 1)
		r2, _ := s.Records(ctx, scopeA, 2)
		if !reflect.DeepEqual(keys(r1), []string{`ict_provider/["P1"]/h-one`, `ict_provider/["P2"]/h-two`}) {
			t.Fatalf("revision 1 changed: %v", keys(r1))
		}
		if !reflect.DeepEqual(keys(r2), []string{`ict_provider/["P1"]/h-uno`}) {
			t.Fatalf("revision 2 = %v", keys(r2))
		}
		cur, _ := s.CurrentRevision(ctx, scopeA)
		revs, _ := s.Revisions(ctx, scopeA)
		if cur.Number != 2 || len(revs) != 2 || revs[0].Number != 1 {
			t.Fatalf("current %+v revisions %+v", cur, revs)
		}
		if _, err := s.Revision(ctx, scopeA, 3); !errors.Is(err, store.ErrNotFound) {
			t.Fatalf("Revision(3) err = %v", err)
		}
		if _, err := s.Records(ctx, scopeA, 3); !errors.Is(err, store.ErrNotFound) {
			t.Fatalf("Records(3) err = %v", err)
		}
	})

	t.Run("conflict", func(t *testing.T) {
		s := newStore(t)
		if _, err := s.Commit(ctx, commit(scopeA, 0, "ing-1", create(version("ict_provider", `["P1"]`, "one")))); err != nil {
			t.Fatal(err)
		}
		_, err := s.Commit(ctx, commit(scopeA, 0, "ing-2", create(version("ict_provider", `["P9"]`, "nine"))))
		if !errors.Is(err, store.ErrConflict) {
			t.Fatalf("stale commit err = %v", err)
		}
		recs, _ := s.Records(ctx, scopeA, 1)
		if len(recs) != 1 {
			t.Fatal("a conflicting commit must write nothing")
		}
		if _, err := s.Ingestion(ctx, scopeA, "ing-2"); !errors.Is(err, store.ErrNotFound) {
			t.Fatal("a conflicting commit must not store its ingestion")
		}
	})

	t.Run("scope isolation", func(t *testing.T) {
		s := newStore(t)
		if _, err := s.Commit(ctx, commit(scopeA, 0, "ing-1", create(version("ict_provider", `["P1"]`, "one")))); err != nil {
			t.Fatal(err)
		}
		if _, err := s.CurrentRevision(ctx, scopeB); !errors.Is(err, store.ErrNotFound) {
			t.Fatal("other workspace must see no revision")
		}
		if _, err := s.Ingestion(ctx, scopeB, "ing-1"); !errors.Is(err, store.ErrNotFound) {
			t.Fatal("other workspace must not see the ingestion")
		}
		if p, _ := s.Provenance(ctx, scopeB, "ict_provider", `["P1"]`); len(p) != 0 {
			t.Fatal("other workspace must not see provenance")
		}
	})

	t.Run("ingestions and provenance", func(t *testing.T) {
		s := newStore(t)
		if _, err := s.Commit(ctx, commit(scopeA, 0, "ing-1", create(version("ict_provider", `["P1"]`, "one")))); err != nil {
			t.Fatal(err)
		}
		upd := version("ict_provider", `["P1"]`, "uno")
		upd.SourceRecordRef = "row-3"
		if _, err := s.Commit(ctx, commit(scopeA, 1, "ing-2", store.Change{Op: store.OpUpdate, Entity: "ict_provider", Key: `["P1"]`, Version: upd, PreviousHash: "h-one"})); err != nil {
			t.Fatal(err)
		}
		ing, err := s.Ingestion(ctx, scopeA, "ing-2")
		if err != nil || ing.RevisionBefore != 1 || ing.RevisionAfter != 2 || string(ing.Result) != `{}` {
			t.Fatalf("ingestion = %+v, %v", ing, err)
		}
		noop := store.Ingestion{ID: "ing-3", Scope: scopeA, Source: source, RevisionBefore: 2, Result: json.RawMessage(`{"no_changes":true}`), CreatedAt: at}
		if err := s.SaveIngestion(ctx, noop); err != nil {
			t.Fatal(err)
		}
		if got, _ := s.Ingestion(ctx, scopeA, "ing-3"); got.RevisionAfter != 0 || got.RevisionBefore != 2 {
			t.Fatalf("saved ingestion = %+v", got)
		}
		p, err := s.Provenance(ctx, scopeA, "ict_provider", `["P1"]`)
		if err != nil || len(p) != 2 {
			t.Fatalf("provenance = %+v, %v", p, err)
		}
		if p[0].Op != store.OpCreate || p[1].Op != store.OpUpdate || p[1].PreviousHash != "h-one" || p[1].ContentHash != "h-uno" ||
			p[1].SourceRecordRef != "row-3" || p[1].Revision != 2 || p[1].IngestionID != "ing-2" || p[0].Seq >= p[1].Seq {
			t.Fatalf("provenance entries = %+v", p)
		}
	})

	t.Run("rollback marks ingestion", func(t *testing.T) {
		s := newStore(t)
		if _, err := s.Commit(ctx, commit(scopeA, 0, "ing-1", create(version("ict_provider", `["P1"]`, "one")))); err != nil {
			t.Fatal(err)
		}
		rb := commit(scopeA, 1, "rbk-1", store.Change{Op: store.OpDelete, Entity: "ict_provider", Key: `["P1"]`, PreviousHash: "h-one"})
		rb.Kind, rb.RollsBack = store.KindRollback, []string{"ing-1"}
		rev, err := s.Commit(ctx, rb)
		if err != nil || rev.Kind != store.KindRollback {
			t.Fatalf("rollback commit = %+v, %v", rev, err)
		}
		if ing, _ := s.Ingestion(ctx, scopeA, "ing-1"); ing.RolledBackBy != "rbk-1" {
			t.Fatalf("RolledBackBy = %q", ing.RolledBackBy)
		}
		bad := commit(scopeA, 2, "rbk-2")
		bad.RollsBack = []string{"ing-missing"}
		if _, err := s.Commit(ctx, bad); !errors.Is(err, store.ErrNotFound) {
			t.Fatalf("rolling back an unknown ingestion err = %v", err)
		}
	})

	t.Run("returned records are copies", func(t *testing.T) {
		s := newStore(t)
		v := version("ict_provider", `["P1"]`, "one")
		if _, err := s.Commit(ctx, commit(scopeA, 0, "ing-1", create(v))); err != nil {
			t.Fatal(err)
		}
		v.Data["legal_name"] = "mutated after commit"
		recs, _ := s.Records(ctx, scopeA, 1)
		recs[0].Data["legal_name"] = "mutated after read"
		again, _ := s.Records(ctx, scopeA, 1)
		if again[0].Data["legal_name"] != "one" {
			t.Fatalf("store data was mutated: %v", again[0].Data)
		}
	})

	t.Run("manifests", func(t *testing.T) {
		s := newStore(t)
		m := adapter.Manifest{Name: "csv-import", Version: "0.1.0", SchemaVersion: "0.1.0", Modes: []adapter.Mode{adapter.ModeFull}}
		if err := s.PutManifest(ctx, scopeA, m); err != nil {
			t.Fatal(err)
		}
		m.Version = "0.2.0"
		if err := s.PutManifest(ctx, scopeA, m); err != nil {
			t.Fatal(err)
		}
		if err := s.PutManifest(ctx, scopeA, adapter.Manifest{Name: "a-first", Version: "1"}); err != nil {
			t.Fatal(err)
		}
		got, err := s.Manifests(ctx, scopeA)
		if err != nil || len(got) != 2 || got[0].Name != "a-first" || got[1].Version != "0.2.0" {
			t.Fatalf("manifests = %+v, %v", got, err)
		}
		if other, _ := s.Manifests(ctx, scopeB); len(other) != 0 {
			t.Fatal("manifests must be scoped")
		}
	})

	t.Run("evaluations", func(t *testing.T) {
		s := newStore(t)
		e := store.StoredEvaluation{ID: "eval-1", Scope: scopeA, SnapshotID: "rev-1", Catalogs: []string{"dora@1.0.0"}, Result: json.RawMessage(`{"ok":true}`), CreatedAt: at}
		if err := s.SaveEvaluation(ctx, e); err != nil {
			t.Fatal(err)
		}
		got, err := s.Evaluation(ctx, scopeA, "eval-1")
		if err != nil || !reflect.DeepEqual(got, e) {
			t.Fatalf("evaluation = %+v, %v", got, err)
		}
		if _, err := s.Evaluation(ctx, scopeB, "eval-1"); !errors.Is(err, store.ErrNotFound) {
			t.Fatal("evaluations must be scoped")
		}
	})

	runAudit(t, newStore)
	runIdentity(t, newStore)
	runSessions(t, newStore)
	runWorkflow(t, newStore)
	runKeys(t, newStore)
	runRetention(t, newStore)
	runTenant(t, newStore)
	runWorkspaces(t, newStore)
	runReports(t, newStore)
}
