// SPDX-License-Identifier: Apache-2.0

package compliance_test

import (
	"errors"
	"reflect"
	"testing"

	"github.com/nexops-one/compliance-engine/pkg/compliance"
	"github.com/nexops-one/compliance-engine/pkg/store"
	"github.com/nexops-one/compliance-engine/sdk/adapter"
)

func hashes(t *testing.T, eng *compliance.Engine, id string) []string {
	t.Helper()
	snap, err := eng.Snapshot(ctx, scopeA, id)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, r := range snap.Records {
		out = append(out, r.Entity+"|"+r.Key+"|"+r.Hash)
	}
	return out
}

func TestRollbackRestoresPriorSnapshot(t *testing.T) {
	eng := newEngine(t)
	loadSample(t, eng)
	b := sampleBatch(t)
	b.Batch.Mode = adapter.ModeIncremental
	b.Entities["ict_provider"][1]["legal_name"] = "Renamed Group Inc"
	second, err := eng.Ingest(ctx, scopeA, b)
	if err != nil || second.Updated != 1 || second.SnapshotID != "rev-2" {
		t.Fatalf("second = %+v, %v", second, err)
	}
	rb, err := eng.Rollback(ctx, scopeA, second.IngestionID)
	if err != nil || rb.SnapshotID != "rev-3" || rb.Updated != 1 || rb.RolledBack != second.IngestionID {
		t.Fatalf("rollback = %+v, %v", rb, err)
	}
	if !reflect.DeepEqual(hashes(t, eng, "rev-3"), hashes(t, eng, "rev-1")) {
		t.Fatal("rollback must restore the content of the revision before the ingestion")
	}
	if reflect.DeepEqual(hashes(t, eng, "rev-2"), hashes(t, eng, "rev-1")) || len(hashes(t, eng, "rev-2")) != 9 {
		t.Fatal("history must be kept: rev-2 still holds the renamed provider")
	}
	if _, err := eng.Rollback(ctx, scopeA, second.IngestionID); !errors.Is(err, compliance.ErrAlreadyRolledBack) {
		t.Fatalf("second rollback err = %v", err)
	}
	p, err := eng.Provenance(ctx, scopeA, "ict_provider", `["SAMPLETP000000000003"]`)
	if err != nil || len(p) != 3 || p[2].Source != compliance.RollbackSource || p[2].Op != store.OpUpdate {
		t.Fatalf("provenance = %+v, %v", p, err)
	}
}

func TestRollbackRefusesNoOpAndUnknownIngestions(t *testing.T) {
	eng := newEngine(t)
	loadSample(t, eng)
	noop, _ := eng.Ingest(ctx, scopeA, sampleBatch(t))
	if _, err := eng.Rollback(ctx, scopeA, noop.IngestionID); !errors.Is(err, compliance.ErrNothingToRollBack) {
		t.Fatalf("no-op rollback err = %v", err)
	}
	if _, err := eng.Rollback(ctx, scopeA, "ing-missing"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("unknown rollback err = %v", err)
	}
}

func TestFullSyncKeepsOtherSourcesRecords(t *testing.T) {
	eng := newEngine(t)
	loadSample(t, eng)
	other := adapter.Manifest{Name: "nexops", Version: "1", SchemaVersion: "0.1.0", Modes: []adapter.Mode{adapter.ModeIncremental},
		Supplies: map[string][]string{"cloud_resource": {"resource_ref", "region"}}}
	if err := eng.RegisterManifest(ctx, scopeA, other); err != nil {
		t.Fatal(err)
	}
	if _, err := eng.Ingest(ctx, scopeA, adapter.NewBuilder(other, "nexops-one").Add("cloud_resource", adapter.Record{"resource_ref": "res-99", "region": "eu-central-1"}).Build()); err != nil {
		t.Fatal(err)
	}
	full := sampleBatch(t)
	full.Entities = map[string][]adapter.Record{"reporting_entity": full.Entities["reporting_entity"]}
	res, err := eng.Ingest(ctx, scopeA, full)
	if err != nil || res.Deleted != 8 || res.Unchanged != 1 {
		t.Fatalf("full sync = %+v, %v", res, err)
	}
	snap, _ := eng.Snapshot(ctx, scopeA, "")
	var keys []string
	for _, r := range snap.Records {
		keys = append(keys, r.Entity+"|"+r.Key)
	}
	want := []string{`cloud_resource|["res-99"]`, `reporting_entity|["SAMPLEFE000000000001"]`}
	if !reflect.DeepEqual(keys, want) {
		t.Fatalf("records = %v", keys)
	}
}

func TestSnapshotLookup(t *testing.T) {
	eng := newEngine(t)
	loadSample(t, eng)
	for _, id := range []string{"rev-9", "bogus"} {
		if _, err := eng.Snapshot(ctx, scopeA, id); !errors.Is(err, store.ErrNotFound) {
			t.Errorf("%s err = %v", id, err)
		}
	}
	empty, err := eng.Snapshot(ctx, scopeA, "rev-0")
	if err != nil || len(empty.Records) != 0 {
		t.Fatalf("rev-0 = %+v, %v", empty, err)
	}
	if _, err := eng.Snapshot(ctx, compliance.Scope{}, ""); err == nil {
		t.Fatal("an empty scope must be rejected")
	}
}
