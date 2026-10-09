// SPDX-License-Identifier: Apache-2.0

package compliance_test

import (
	"encoding/json"
	"testing"

	"github.com/nexops-one/compliance-engine/pkg/access"
	"github.com/nexops-one/compliance-engine/pkg/compliance"
	"github.com/nexops-one/compliance-engine/pkg/extension"
	"github.com/nexops-one/compliance-engine/pkg/store"
	"github.com/nexops-one/compliance-engine/sdk/adapter"
)

func auditOf(t *testing.T, eng *compliance.Engine) []store.AuditEvent {
	t.Helper()
	evs, err := eng.AuditEvents(ctx, scopeA, store.AuditQuery{})
	if err != nil {
		t.Fatal(err)
	}
	if r, err := eng.VerifyAudit(ctx, scopeA); err != nil || !r.OK {
		t.Fatalf("verify = %+v, %v", r, err)
	}
	return evs
}

func TestIngestionIsAudited(t *testing.T) {
	eng := newEngine(t)
	b := sampleBatch(t)
	if err := eng.RegisterManifest(ctx, scopeA, manifestFor(b, adapter.ModeFull, adapter.ModeIncremental)); err != nil {
		t.Fatal(err)
	}
	p := extension.Principal{Scope: scopeA, Actor: "user:u1", Kind: extension.ActorUser, Roles: []access.Role{access.RoleOwner}}
	res, err := eng.Ingest(extension.WithPrincipal(ctx, p), scopeA, b)
	if err != nil {
		t.Fatal(err)
	}
	evs := auditOf(t, eng)
	if len(evs) != 1 || evs[0].Action != "ingestion.commit" || evs[0].Actor != "user:u1" || evs[0].ActorKind != "user" ||
		evs[0].TargetID != res.IngestionID {
		t.Fatalf("events = %+v", evs)
	}
	var d map[string]any
	if err := json.Unmarshal(evs[0].Details, &d); err != nil || d["adapter"] != b.Source.Adapter || d["snapshot_id"] != "rev-1" || d["created"] != float64(res.Created) {
		t.Fatalf("details = %s", evs[0].Details)
	}
}

func TestNoChangeIngestionIsNotAudited(t *testing.T) {
	eng := newEngine(t)
	loadSample(t, eng)
	if res, err := eng.Ingest(ctx, scopeA, sampleBatch(t)); err != nil || !res.NoChanges {
		t.Fatalf("second ingestion = %+v, %v", res, err)
	}
	if evs := auditOf(t, eng); len(evs) != 1 {
		t.Fatalf("an ingestion without changes must not be audited: %d events", len(evs))
	}
	if _, err := eng.DryRun(ctx, scopeA, sampleBatch(t)); err != nil {
		t.Fatal(err)
	}
	if evs := auditOf(t, eng); len(evs) != 1 {
		t.Fatal("a dry run must not be audited")
	}
}

func TestRollbackIsAudited(t *testing.T) {
	eng := newEngine(t)
	first := loadSample(t, eng)
	rb, err := eng.Rollback(ctx, scopeA, first.IngestionID)
	if err != nil {
		t.Fatal(err)
	}
	evs := auditOf(t, eng)
	if len(evs) != 2 || evs[1].Action != "ingestion.rollback" || evs[1].TargetID != first.IngestionID {
		t.Fatalf("events = %+v", evs)
	}
	var d map[string]any
	_ = json.Unmarshal(evs[1].Details, &d)
	if d["rollback_ingestion_id"] != rb.IngestionID || d["snapshot_id"] != rb.SnapshotID {
		t.Fatalf("details = %s", evs[1].Details)
	}
}

func TestLibraryActor(t *testing.T) {
	eng := newEngine(t)
	loadSample(t, eng)
	evs := auditOf(t, eng)
	if len(evs) != 1 || evs[0].Actor != compliance.LibraryActor || evs[0].ActorKind != "system" {
		t.Fatalf("events = %+v", evs)
	}
	if _, err := eng.AuditEvents(ctx, compliance.Scope{}, store.AuditQuery{}); err == nil {
		t.Fatal("an empty scope must be refused")
	}
}
