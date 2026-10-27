// SPDX-License-Identifier: Apache-2.0

package postgres_test

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/nexops-one/compliance-engine/pkg/catalog"
	"github.com/nexops-one/compliance-engine/pkg/compliance"
	"github.com/nexops-one/compliance-engine/pkg/store"
	"github.com/nexops-one/compliance-engine/pkg/store/postgres"
	"github.com/nexops-one/compliance-engine/pkg/store/storetest"
	"github.com/nexops-one/compliance-engine/sdk/adapter"
	"github.com/nexops-one/compliance-engine/sdk/schema"
)

func TestPostgresStoreContract(t *testing.T) {
	storetest.Run(t, func(t *testing.T) store.Store {
		s, _ := migrated(t)
		return s
	})
}

func TestConcurrentCommitsConflict(t *testing.T) {
	s, _ := migrated(t)
	scope := adapter.Scope{TenantID: "t", WorkspaceID: "w"}
	at := time.Date(2026, 1, 15, 9, 0, 0, 0, time.UTC)
	errs := make([]error, 8)
	var wg sync.WaitGroup
	for i := range errs {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			v := &store.RecordVersion{Entity: "ict_provider", Key: fmt.Sprintf(`["P%d"]`, i), Data: adapter.Record{"provider_id_code": fmt.Sprintf("P%d", i)}, Hash: "h", SchemaVersion: "0.1.0", WrittenAt: at}
			_, errs[i] = s.Commit(ctx, store.Commit{
				Scope: scope, ExpectedRevision: 0, Kind: store.KindIngestion, At: at,
				Changes:   []store.Change{{Op: store.OpCreate, Entity: v.Entity, Key: v.Key, Version: v}},
				Ingestion: store.Ingestion{ID: fmt.Sprintf("ing-%d", i), Scope: scope, Result: json.RawMessage(`{}`), CreatedAt: at},
			})
		}(i)
	}
	wg.Wait()
	ok, conflicts := 0, 0
	for _, err := range errs {
		switch {
		case err == nil:
			ok++
		case errors.Is(err, store.ErrConflict):
			conflicts++
		default:
			t.Fatalf("unexpected error: %v", err)
		}
	}
	revs, _ := s.Revisions(ctx, scope)
	recs, _ := s.Records(ctx, scope, 1)
	if ok != 1 || conflicts != 7 || len(revs) != 1 || len(recs) != 1 {
		t.Fatalf("ok=%d conflicts=%d revisions=%d records=%d", ok, conflicts, len(revs), len(recs))
	}
}

func sampleBatch(t *testing.T) adapter.Batch {
	t.Helper()
	f, err := os.Open("../../compliance/testdata/sample-batch.json")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	b, err := adapter.DecodeBatch(f)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func manifestFor(b adapter.Batch) adapter.Manifest {
	supplies := map[string][]string{}
	for entity, recs := range b.Entities {
		seen := map[string]bool{}
		for _, r := range recs {
			for f := range r {
				if f != schema.MetaField && !seen[f] {
					seen[f] = true
					supplies[entity] = append(supplies[entity], f)
				}
			}
		}
		sort.Strings(supplies[entity])
	}
	return adapter.Manifest{Name: b.Source.Adapter, Version: b.Source.AdapterVersion, SchemaVersion: b.SchemaVersion, Supplies: supplies, Modes: []adapter.Mode{adapter.ModeIncremental, adapter.ModeFull}}
}

func TestEngineOverPostgresSurvivesReconnect(t *testing.T) {
	s, url := migrated(t)
	scope := compliance.Scope{TenantID: "acme", WorkspaceID: "ws-1"}
	eng, err := compliance.New(ctx, compliance.Config{Store: s})
	if err != nil {
		t.Fatal(err)
	}
	b := sampleBatch(t)
	if err := eng.RegisterManifest(ctx, scope, manifestFor(b)); err != nil {
		t.Fatal(err)
	}
	first, err := eng.Ingest(ctx, scope, b)
	if err != nil || first.Created != 9 {
		t.Fatalf("first = %+v, %v", first, err)
	}

	s2, err := postgres.Open(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer s2.Close()
	eng2, err := compliance.New(ctx, compliance.Config{Store: s2})
	if err != nil {
		t.Fatal(err)
	}
	again, err := eng2.Ingest(ctx, scope, sampleBatch(t))
	if err != nil || !again.NoChanges || again.Unchanged != 9 {
		t.Fatalf("re-ingesting after reconnect must be a no-op: %+v, %v", again, err)
	}
	ev, err := eng2.Evaluate(ctx, scope, "", []catalog.Ref{{Catalog: "dora", Version: "1.0.0"}})
	if err != nil {
		t.Fatal(err)
	}
	if tally := ev.Result.Frameworks[0].Tally; tally.ScorePct != 80 || tally.CoveragePct != 45 {
		t.Fatalf("tally = %+v", tally)
	}
	rb, err := eng2.Rollback(ctx, scope, first.IngestionID)
	if err != nil || rb.Deleted != 9 {
		t.Fatalf("rollback = %+v, %v", rb, err)
	}
	snap, _ := eng2.Snapshot(ctx, scope, "")
	if snap.ID != "rev-2" || len(snap.Records) != 0 {
		t.Fatalf("snapshot after rollback = %s with %d records", snap.ID, len(snap.Records))
	}
	old, _ := eng2.Snapshot(ctx, scope, "rev-1")
	if len(old.Records) != 9 {
		t.Fatalf("history must be kept: rev-1 has %d records", len(old.Records))
	}
}
