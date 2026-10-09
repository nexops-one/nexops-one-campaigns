// SPDX-License-Identifier: Apache-2.0

package storetest

import (
	"encoding/json"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/nexops-one/compliance-engine/pkg/audit"
	"github.com/nexops-one/compliance-engine/pkg/store"
)

func runRetention(t *testing.T, newStore func(t *testing.T) store.Store) {
	t.Run("retention deletes a revision prefix and what only it used", func(t *testing.T) {
		s := newStore(t)
		keep := version("ict_provider", `["P1"]`, "kept")      // created in rev 1, still current
		old := version("ict_provider", `["P2"]`, "old")        // replaced in rev 2
		newer := version("ict_provider", `["P2"]`, "newer")    // current from rev 2
		gone := version("ict_provider", `["P3"]`, "temporary") // created in rev 2, deleted in rev 3
		mustCommit := func(expected int64, id string, changes ...store.Change) {
			t.Helper()
			if _, err := s.Commit(ctx, commit(scopeA, expected, id, changes...)); err != nil {
				t.Fatal(err)
			}
		}
		mustCommit(0, "ing-1", create(keep), create(old))
		mustCommit(1, "ing-2", store.Change{Op: store.OpUpdate, Entity: newer.Entity, Key: newer.Key, Version: newer, PreviousHash: old.Hash}, create(gone))
		mustCommit(2, "ing-3", store.Change{Op: store.OpDelete, Entity: gone.Entity, Key: gone.Key, PreviousHash: gone.Hash})
		for i, snap := range []string{"rev-1", "rev-3"} {
			if err := s.SaveEvaluation(ctx, store.StoredEvaluation{ID: []string{"eval-old", "eval-new"}[i], Scope: scopeA, SnapshotID: snap,
				Catalogs: []string{}, Result: json.RawMessage(`{}`), CreatedAt: at}); err != nil {
				t.Fatal(err)
			}
		}
		refs, err := s.EvaluationRefs(ctx, scopeA)
		if err != nil || len(refs) != 2 || refs[1].SnapshotID != "rev-1" {
			t.Fatalf("evaluation refs = %+v %v", refs, err)
		}
		if _, err := s.PutEvidence(ctx, evidenceDoc("ev-old", store.ControlRef{Catalog: "dora", ControlID: "c1"}), 0); err != nil {
			t.Fatal(err)
		}
		c, err := s.ApplyRetention(ctx, scopeA, store.RetentionDeletion{KeepFrom: 3, Evaluations: []string{"eval-old"}, Evidence: []string{"ev-old"}}, Event(scopeA, "retention.run"))
		if err != nil {
			t.Fatal(err)
		}
		if c.Revisions != 2 || c.Evaluations != 1 || c.Evidence != 1 || c.Ingestions != 2 || c.RecordVersions != 2 || c.Provenance != 4 {
			t.Fatalf("counts = %+v", c)
		}
		revs, _ := s.Revisions(ctx, scopeA)
		if len(revs) != 1 || revs[0].Number != 3 {
			t.Fatalf("revisions = %+v", revs)
		}
		if _, err := s.Records(ctx, scopeA, 1); !errors.Is(err, store.ErrNotFound) {
			t.Fatalf("records of a deleted revision = %v", err)
		}
		if _, err := s.Revision(ctx, scopeA, 2); !errors.Is(err, store.ErrNotFound) {
			t.Fatalf("a deleted revision = %v", err)
		}
		recs, err := s.Records(ctx, scopeA, 3)
		if err != nil || !reflect.DeepEqual(keys(recs), []string{`ict_provider/["P1"]/h-kept`, `ict_provider/["P2"]/h-newer`}) {
			t.Fatalf("the kept revision must be complete: %v %v", keys(recs), err)
		}
		if p, _ := s.Provenance(ctx, scopeA, "ict_provider", `["P3"]`); len(p) != 1 || p[0].Op != store.OpDelete {
			t.Fatalf("provenance of kept revisions stays: %+v", p)
		}
		if _, err := s.Ingestion(ctx, scopeA, "ing-1"); !errors.Is(err, store.ErrNotFound) {
			t.Fatal("ingestions of deleted revisions are deleted")
		}
		if _, err := s.Ingestion(ctx, scopeA, "ing-3"); err != nil {
			t.Fatal("the ingestion of the kept revision stays")
		}
		if _, err := s.Evaluation(ctx, scopeA, "eval-new"); err != nil {
			t.Fatal("kept evaluations stay")
		}
		if _, err := s.Evidence(ctx, scopeA, "ev-old"); !errors.Is(err, store.ErrNotFound) {
			t.Fatal("deleted evidence is gone")
		}
		if l, _ := s.ListEvidence(ctx, scopeA, store.EvidenceQuery{Catalog: "dora", ControlID: "c1"}); len(l) != 0 {
			t.Fatal("links of deleted evidence are gone")
		}
		if got := ChainOf(t, s, scopeA); !reflect.DeepEqual(got, []string{"retention.run"}) {
			t.Fatalf("chain = %v", got)
		}
		if _, err := s.Commit(ctx, commit(scopeA, 3, "ing-4", create(version("ict_provider", `["P4"]`, "four")))); err != nil {
			t.Fatalf("commits continue after retention: %v", err)
		}
		scopes, err := s.WorkspaceScopes(ctx)
		if err != nil || len(scopes) != 1 || scopes[0] != scopeA {
			t.Fatalf("scopes = %v %v", scopes, err)
		}
	})

	t.Run("audit prune keeps a verifiable suffix", func(t *testing.T) {
		s := newStore(t)
		for i := 0; i < 5; i++ {
			ev := Event(scopeA, "e")
			ev.At = at.Add(time.Duration(i) * time.Hour)
			ev.TargetID = string(rune('a' + i))
			if err := s.AppendAudit(ctx, ev); err != nil {
				t.Fatal(err)
			}
		}
		prune := Event(scopeA, "audit.pruned")
		prune.At = at.Add(10 * time.Hour)
		n, err := s.PruneAudit(ctx, scopeA, at.Add(2*time.Hour), prune)
		if err != nil || n != 2 {
			t.Fatalf("prune = %d %v", n, err)
		}
		evs, _ := s.AuditEvents(ctx, scopeA, store.AuditQuery{})
		if len(evs) != 4 || evs[0].Seq != 3 || evs[3].Action != "audit.pruned" || evs[3].Seq != 6 {
			t.Fatalf("remaining = %+v", evs)
		}
		if r := audit.Verify(evs); !r.OK {
			t.Fatalf("the remaining suffix must verify: %+v", r)
		}
		n, err = s.PruneAudit(ctx, scopeA, at.Add(100*time.Hour), prune)
		if err != nil || n != 3 {
			t.Fatalf("pruning everything keeps the latest event: %d %v", n, err)
		}
		evs, _ = s.AuditEvents(ctx, scopeA, store.AuditQuery{})
		if len(evs) != 2 || audit.Verify(evs).OK != true {
			t.Fatalf("after a full prune = %+v", evs)
		}
		if n, err := s.PruneAudit(ctx, scopeB, at, prune); err != nil || n != 0 {
			t.Fatalf("an empty chain = %d %v", n, err)
		}
	})
}
