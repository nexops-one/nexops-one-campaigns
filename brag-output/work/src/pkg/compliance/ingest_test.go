// SPDX-License-Identifier: Apache-2.0

package compliance_test

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/nexops-one/compliance-engine/pkg/canonical"
	"github.com/nexops-one/compliance-engine/pkg/catalog"
	"github.com/nexops-one/compliance-engine/pkg/compliance"
	"github.com/nexops-one/compliance-engine/pkg/extension"
	"github.com/nexops-one/compliance-engine/pkg/store/memory"
	"github.com/nexops-one/compliance-engine/sdk/adapter"
	"github.com/nexops-one/compliance-engine/sdk/schema"
)

func TestNewLoadsEmbeddedCatalogs(t *testing.T) {
	want := []catalog.Ref{{Catalog: "aiact", Version: "1.0.0"}, {Catalog: "dora", Version: "1.0.0"}, {Catalog: "gdpr", Version: "1.0.0"}}
	if got := newEngine(t).Catalogs(); !reflect.DeepEqual(got, want) {
		t.Fatalf("catalogs = %v", got)
	}
}

func TestNewValidatesConfig(t *testing.T) {
	if _, err := compliance.New(ctx, compliance.Config{}); err == nil {
		t.Fatal("a store is required")
	}
	if _, err := compliance.New(ctx, compliance.Config{Store: memory.New(), IdentifierPolicy: "sometimes"}); err == nil {
		t.Fatal("an unknown identifier policy must be rejected")
	}
}

func TestIngestRequiresRegisteredManifest(t *testing.T) {
	_, err := newEngine(t).Ingest(ctx, scopeA, sampleBatch(t))
	if !errors.Is(err, compliance.ErrUnknownAdapter) {
		t.Fatalf("err = %v", err)
	}
}

func TestIngestRejectsUnsupportedSchemaVersion(t *testing.T) {
	eng := newEngine(t)
	b := sampleBatch(t)
	if err := eng.RegisterManifest(ctx, scopeA, manifestFor(b)); err != nil {
		t.Fatal(err)
	}
	b.SchemaVersion = "0.9.0"
	if _, err := eng.Ingest(ctx, scopeA, b); !errors.Is(err, schema.ErrUnsupportedVersion) {
		t.Fatalf("err = %v", err)
	}
}

func TestIngestRejectsInvalidEnvelope(t *testing.T) {
	eng := newEngine(t)
	b := sampleBatch(t)
	b.Entities["nope"] = []adapter.Record{{}}
	_, err := eng.Ingest(ctx, scopeA, b)
	var be *compliance.BatchError
	if !errors.As(err, &be) || be.Errors[0].Code != "unknown_entity" {
		t.Fatalf("err = %v", err)
	}
}

func TestIngestEnforcesRecordLimit(t *testing.T) {
	eng := newEngine(t, func(c *compliance.Config) { c.Limits.MaxRecordsPerBatch = 5 })
	if _, err := eng.Ingest(ctx, scopeA, sampleBatch(t)); !errors.Is(err, compliance.ErrTooLarge) {
		t.Fatalf("err = %v", err)
	}
}

func TestIngestRejectsModeNotInManifest(t *testing.T) {
	eng := newEngine(t)
	b := sampleBatch(t) // mode full
	if err := eng.RegisterManifest(ctx, scopeA, manifestFor(b, adapter.ModeIncremental)); err != nil {
		t.Fatal(err)
	}
	if _, err := eng.Ingest(ctx, scopeA, b); !errors.Is(err, compliance.ErrModeNotSupported) {
		t.Fatalf("err = %v", err)
	}
}

func TestIngestSampleBatch(t *testing.T) {
	eng := newEngine(t)
	res := loadSample(t, eng)
	if res.Accepted != 9 || res.Created != 9 || res.RejectedRecords != 0 || res.SnapshotID != "rev-1" || res.BatchID != "demo-001" {
		t.Fatalf("res = %+v", res)
	}
	if len(res.Warnings) != 4 {
		t.Fatalf("warnings = %+v", res.Warnings)
	}
	for _, w := range res.Warnings {
		if w.Code != "lei_check_digits" {
			t.Fatalf("unexpected warning %+v", w)
		}
	}
}

func TestReingestSameBatchIsNoOp(t *testing.T) {
	eng := newEngine(t)
	loadSample(t, eng)
	res, err := eng.Ingest(ctx, scopeA, sampleBatch(t))
	if err != nil {
		t.Fatal(err)
	}
	if !res.NoChanges || res.Unchanged != 9 || res.SnapshotID != "rev-1" {
		t.Fatalf("res = %+v", res)
	}
	revs, _ := eng.Snapshots(ctx, scopeA)
	if len(revs) != 1 {
		t.Fatalf("re-ingesting identical content must not create a revision: %d", len(revs))
	}
	ing, err := eng.Ingestion(ctx, scopeA, res.IngestionID)
	if err != nil || ing.RevisionAfter != 0 || ing.RevisionBefore != 1 {
		t.Fatalf("no-change ingestion must still be recorded: %+v, %v", ing, err)
	}
}

func TestIdentifierPolicyReject(t *testing.T) {
	eng := newEngine(t, func(c *compliance.Config) { c.IdentifierPolicy = canonical.PolicyReject })
	res := loadSample(t, eng)
	if res.Accepted != 5 || res.RejectedRecords != 4 {
		t.Fatalf("res = %+v", res)
	}
}

func TestDryRunDoesNotCommit(t *testing.T) {
	eng := newEngine(t)
	b := sampleBatch(t)
	if err := eng.RegisterManifest(ctx, scopeA, manifestFor(b)); err != nil {
		t.Fatal(err)
	}
	dr, err := eng.DryRun(ctx, scopeA, b)
	if err != nil {
		t.Fatal(err)
	}
	if !dr.Result.DryRun || dr.Result.Created != 9 || dr.Result.SnapshotID != "" || len(dr.Completeness.Gaps) == 0 {
		t.Fatalf("dry run = %+v", dr.Result)
	}
	if revs, _ := eng.Snapshots(ctx, scopeA); len(revs) != 0 {
		t.Fatal("a dry run must not commit")
	}
}

type denyIngest struct{}

func (denyIngest) Allowed(_ context.Context, _ adapter.Scope, f extension.Feature) extension.Decision {
	if f == extension.FeatureRegisterIngest {
		return extension.Decision{Reason: extension.ReasonAddonDisabled}
	}
	return extension.Decision{Allowed: true}
}

func TestEntitlementsGateWritesButNeverReads(t *testing.T) {
	eng := newEngine(t, func(c *compliance.Config) { c.Entitlements = denyIngest{} })
	err := eng.RegisterManifest(ctx, scopeA, manifestFor(sampleBatch(t)))
	var ne *extension.NotEntitledError
	if !errors.As(err, &ne) || ne.Reason != extension.ReasonAddonDisabled {
		t.Fatalf("err = %v", err)
	}
	if _, err := eng.Ingest(ctx, scopeA, sampleBatch(t)); !errors.As(err, &ne) {
		t.Fatalf("ingest err = %v", err)
	}
	if _, err := eng.Completeness(ctx, scopeA, ""); err != nil {
		t.Fatalf("reads must not be gated: %v", err)
	}
	if _, err := eng.Snapshot(ctx, scopeA, ""); err != nil {
		t.Fatalf("reads must not be gated: %v", err)
	}
}

type fakeAdapter struct {
	manifest adapter.Manifest
	batch    adapter.Batch
	gotScope adapter.Scope
}

func (f *fakeAdapter) Manifest() adapter.Manifest { return f.manifest }

func (f *fakeAdapter) Pull(_ context.Context, req adapter.SyncRequest) (adapter.Batch, error) {
	f.gotScope = req.Scope
	return f.batch, nil
}

func TestSyncBindsScopeAndChecksSource(t *testing.T) {
	eng := newEngine(t)
	b := sampleBatch(t)
	a := &fakeAdapter{manifest: manifestFor(b), batch: b}
	res, err := eng.Sync(ctx, scopeA, a, adapter.ModeFull)
	if err != nil || res.Created != 9 || a.gotScope != scopeA {
		t.Fatalf("sync = %+v, %v (scope %+v)", res, err, a.gotScope)
	}
	other, err := eng.Snapshot(ctx, scopeB, "")
	if err != nil || other.ID != "rev-0" || len(other.Records) != 0 {
		t.Fatalf("another workspace must see nothing: %+v, %v", other, err)
	}
	if _, err := eng.Sync(ctx, scopeA, a, adapter.ModeIncremental); err == nil {
		t.Fatal("a batch whose mode differs from the requested sync must be refused")
	}
	spoof := sampleBatch(t)
	spoof.Source.Adapter = "someone-else"
	if _, err := eng.Sync(ctx, scopeA, &fakeAdapter{manifest: manifestFor(b), batch: spoof}, adapter.ModeFull); err == nil {
		t.Fatal("a batch claiming another adapter must be refused")
	}
}
