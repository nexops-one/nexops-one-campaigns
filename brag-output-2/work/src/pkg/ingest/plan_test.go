// SPDX-License-Identifier: Apache-2.0

package ingest_test

import (
	"testing"
	"time"

	"github.com/nexops-one/compliance-engine/pkg/canonical"
	"github.com/nexops-one/compliance-engine/pkg/ingest"
	"github.com/nexops-one/compliance-engine/pkg/store"
	"github.com/nexops-one/compliance-engine/sdk/adapter"
	"github.com/nexops-one/compliance-engine/sdk/schema"
)

var manifest = adapter.Manifest{
	Name: "csv-import", Version: "0.1.0", SchemaVersion: "0.1.0",
	Supplies: map[string][]string{"ict_provider": {"provider_id_code", "legal_name", "hq_country"}},
	Modes:    []adapter.Mode{adapter.ModeIncremental, adapter.ModeFull},
}

func options(t *testing.T, current ...store.RecordVersion) ingest.Options {
	t.Helper()
	reg, err := schema.Default()
	if err != nil {
		t.Fatal(err)
	}
	return ingest.Options{Schema: reg.Latest(), Manifest: manifest, Policy: canonical.PolicyWarn, Current: current, IngestionID: "ing-1", Now: time.Date(2026, 1, 15, 9, 0, 0, 0, time.UTC)}
}

func batch(mode adapter.Mode, providers ...adapter.Record) adapter.Batch {
	b := adapter.NewBuilder(manifest, "sheet").Mode(mode)
	for _, p := range providers {
		b.Add("ict_provider", p)
	}
	return b.Build()
}

// stored plans a batch against nothing and returns its versions, as if committed.
func stored(t *testing.T, providers ...adapter.Record) []store.RecordVersion {
	t.Helper()
	_, changes := ingest.Plan(batch(adapter.ModeIncremental, providers...), options(t))
	var out []store.RecordVersion
	for _, c := range changes {
		out = append(out, *c.Version)
	}
	return out
}

func TestPlanCreatesValidRecords(t *testing.T) {
	res, changes := ingest.Plan(batch(adapter.ModeIncremental, adapter.Record{"provider_id_code": "P1", "legal_name": "One"}), options(t))
	if res.Accepted != 1 || res.Created != 1 || res.NoChanges || len(changes) != 1 {
		t.Fatalf("res = %+v", res)
	}
	v := changes[0].Version
	if changes[0].Op != store.OpCreate || v.Key != `["P1"]` || v.IngestionID != "ing-1" || v.Source.Adapter != "csv-import" || v.SchemaVersion != "0.1.0" || v.Hash == "" {
		t.Fatalf("change = %+v / %+v", changes[0], v)
	}
	if res.IngestionID != "ing-1" || res.Mode != adapter.ModeIncremental || res.SchemaVersion != "0.1.0" {
		t.Fatalf("result header = %+v", res)
	}
}

func TestPlanUnchangedAndUpdated(t *testing.T) {
	cur := stored(t, adapter.Record{"provider_id_code": "P1", "legal_name": "One"}, adapter.Record{"provider_id_code": "P2", "legal_name": "Two"})
	res, changes := ingest.Plan(batch(adapter.ModeIncremental,
		adapter.Record{"provider_id_code": "P1", "legal_name": "One"},
		adapter.Record{"provider_id_code": "P2", "legal_name": "Deux"}), options(t, cur...))
	if res.Unchanged != 1 || res.Updated != 1 || len(changes) != 1 || changes[0].Op != store.OpUpdate || changes[0].PreviousHash != cur[1].Hash {
		t.Fatalf("res = %+v changes = %+v", res, changes)
	}
	res, changes = ingest.Plan(batch(adapter.ModeIncremental, adapter.Record{"provider_id_code": "P1", "legal_name": "One"}), options(t, cur...))
	if !res.NoChanges || len(changes) != 0 || res.Deleted != 0 {
		t.Fatalf("incremental batch must not delete: %+v", res)
	}
}

func TestPlanRejectsInvalidRecordsWithLocation(t *testing.T) {
	bad := adapter.Record{"provider_id_code": "P1", "hq_country": "Ireland"}
	adapter.SetSourceRef(bad, "row-7")
	res, changes := ingest.Plan(batch(adapter.ModeIncremental, adapter.Record{"provider_id_code": "P0"}, bad), options(t))
	if res.Accepted != 1 || res.RejectedRecords != 1 || len(changes) != 1 || len(res.Errors) != 1 {
		t.Fatalf("res = %+v", res)
	}
	e := res.Errors[0]
	if e.Entity != "ict_provider" || e.Index != 1 || e.SourceRecordRef != "row-7" || e.Field != "hq_country" || e.Code != "pattern" {
		t.Fatalf("error = %+v", e)
	}
}

func TestPlanTreatsNullAsMissing(t *testing.T) {
	res, changes := ingest.Plan(batch(adapter.ModeIncremental, adapter.Record{"provider_id_code": "P1", "legal_name": nil}), options(t))
	if res.Accepted != 1 || len(res.Errors) != 0 {
		t.Fatalf("null must be accepted as missing: %+v", res)
	}
	if _, ok := changes[0].Version.Data["legal_name"]; ok {
		t.Fatal("null field must be dropped")
	}
}

func TestPlanRejectsDuplicateIdentity(t *testing.T) {
	res, changes := ingest.Plan(batch(adapter.ModeIncremental,
		adapter.Record{"provider_id_code": "P1", "legal_name": "First"},
		adapter.Record{"provider_id_code": "P1", "legal_name": "Second"}), options(t))
	if res.Accepted != 1 || res.RejectedRecords != 1 || res.Errors[0].Code != "duplicate_identity" || res.Errors[0].Index != 1 {
		t.Fatalf("res = %+v", res)
	}
	if changes[0].Version.Data["legal_name"] != "First" {
		t.Fatal("the first record must win")
	}
}

func TestPlanIdentifierPolicy(t *testing.T) {
	rec := adapter.Record{"provider_id_code": "P1", "hq_country": "UK"}
	res, _ := ingest.Plan(batch(adapter.ModeIncremental, rec), options(t))
	if res.Accepted != 1 || len(res.Warnings) != 1 || res.Warnings[0].Code != "iso_country" || res.Warnings[0].Index != 0 {
		t.Fatalf("warn policy = %+v", res)
	}
	opt := options(t)
	opt.Policy = canonical.PolicyReject
	res, changes := ingest.Plan(batch(adapter.ModeIncremental, rec), opt)
	if res.RejectedRecords != 1 || len(changes) != 0 || res.Errors[0].Code != "iso_country" {
		t.Fatalf("reject policy = %+v", res)
	}
}

func TestPlanFullModeDeletesOnlyOwnRecordsInScope(t *testing.T) {
	cur := stored(t, adapter.Record{"provider_id_code": "P1"}, adapter.Record{"provider_id_code": "P2"}, adapter.Record{"provider_id_code": "P3"})
	cur[2].Source.Adapter = "other-adapter" // P3 was written by another source
	res, changes := ingest.Plan(batch(adapter.ModeFull, adapter.Record{"provider_id_code": "P1"}), options(t, cur...))
	if res.Deleted != 1 || len(changes) != 1 || changes[0].Op != store.OpDelete || changes[0].Key != `["P2"]` || changes[0].Version != nil {
		t.Fatalf("res = %+v changes = %+v", res, changes)
	}
}

func TestPlanFullModeSkipsDeletionsForEntityWithRejects(t *testing.T) {
	cur := stored(t, adapter.Record{"provider_id_code": "P1"}, adapter.Record{"provider_id_code": "P2"})
	res, changes := ingest.Plan(batch(adapter.ModeFull,
		adapter.Record{"provider_id_code": "P1"},
		adapter.Record{"provider_id_code": "P2", "hq_country": "not-a-country"}), options(t, cur...))
	if res.Deleted != 0 || len(changes) != 0 {
		t.Fatalf("a rejected record must not cause its stored version to be deleted: %+v %+v", res, changes)
	}
	if len(res.Warnings) != 1 || res.Warnings[0].Code != "full_sync_deletions_skipped" || res.Warnings[0].Entity != "ict_provider" || res.Warnings[0].Index != -1 {
		t.Fatalf("warnings = %+v", res.Warnings)
	}
}
