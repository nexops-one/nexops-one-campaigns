// SPDX-License-Identifier: Apache-2.0

package compliance_test

import (
	"errors"
	"testing"

	"github.com/nexops-one/compliance-engine/pkg/catalog"
	"github.com/nexops-one/compliance-engine/pkg/compliance"
	"github.com/nexops-one/compliance-engine/sdk/adapter"
)

func TestRollbackMarksLaterIngestionsItReverts(t *testing.T) {
	eng := newEngine(t)
	x := loadSample(t, eng)
	b := sampleBatch(t)
	b.Batch.Mode = adapter.ModeIncremental
	b.Entities["ict_provider"][1]["legal_name"] = "Renamed Group Inc"
	y, err := eng.Ingest(ctx, scopeA, b)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := eng.Rollback(ctx, scopeA, x.IngestionID); err != nil {
		t.Fatal(err)
	}
	if _, err := eng.Rollback(ctx, scopeA, y.IngestionID); !errors.Is(err, compliance.ErrAlreadyRolledBack) {
		t.Fatalf("rolling back an ingestion already reverted by an earlier rollback must fail, got %v", err)
	}
	snap, _ := eng.Snapshot(ctx, scopeA, "")
	if len(snap.Records) != 0 {
		t.Fatalf("rolled-back data came back: %d records", len(snap.Records))
	}
}

func TestReturnedCatalogIsACopy(t *testing.T) {
	eng := newEngine(t)
	c, err := eng.Catalog(dora)
	if err != nil {
		t.Fatal(err)
	}
	c.Controls[0].Requires[0] = "mutated.field"
	c.Controls = nil
	again, _ := eng.Catalog(dora)
	if len(again.Controls) != 11 || again.Controls[0].Requires[0] != "reporting_entity.lei" {
		t.Fatal("mutating a returned catalog changed the engine's catalog")
	}
	loadSample(t, eng)
	ev, err := eng.Evaluate(ctx, scopeA, "", []catalog.Ref{dora})
	if err != nil {
		t.Fatal(err)
	}
	ev.Result.Frameworks[0].Controls[0].Explanation.Inputs[0] = "mutated.field"
	second, _ := eng.Evaluate(ctx, scopeA, "", []catalog.Ref{dora})
	if second.Result.Frameworks[0].Controls[0].Explanation.Inputs[0] != "reporting_entity.lei" {
		t.Fatal("mutating an evaluation result changed later evaluations")
	}
}
