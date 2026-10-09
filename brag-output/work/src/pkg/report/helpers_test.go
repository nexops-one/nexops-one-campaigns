// SPDX-License-Identifier: Apache-2.0

package report_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/nexops-one/compliance-engine/pkg/access"
	"github.com/nexops-one/compliance-engine/pkg/catalog"
	"github.com/nexops-one/compliance-engine/pkg/compliance"
	"github.com/nexops-one/compliance-engine/pkg/crypt"
	"github.com/nexops-one/compliance-engine/pkg/extension"
	"github.com/nexops-one/compliance-engine/pkg/identity"
	"github.com/nexops-one/compliance-engine/pkg/report"
	"github.com/nexops-one/compliance-engine/pkg/store"
	"github.com/nexops-one/compliance-engine/pkg/store/memory"
	"github.com/nexops-one/compliance-engine/pkg/workflow"
	"github.com/nexops-one/compliance-engine/sdk/adapter"
)

var (
	update = flag.Bool("update", false, "rewrite golden files")
	ctx    = context.Background()
	scope  = adapter.Scope{TenantID: "acme", WorkspaceID: "ws-1"}
	dora   = catalog.Ref{Catalog: "dora", Version: "1.0.0"}
)

// Markers placed in places a report must never show.
const (
	secretPath   = "SECRET-PATH"
	secretQuery  = "SECRET-QUERY"
	secretNote   = "SECRET-NOTE"
	secretReason = "SECRET-REASON"
)

func principal(userID string, roles ...access.Role) context.Context {
	return extension.WithPrincipal(ctx, extension.Principal{Scope: scope, Actor: "user:" + userID, Kind: extension.ActorUser, Roles: roles})
}

func sampleBatch(t *testing.T) adapter.Batch {
	t.Helper()
	f, err := os.Open("../compliance/testdata/sample-batch.json")
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
				if f != "_meta" && !seen[f] {
					seen[f] = true
					supplies[entity] = append(supplies[entity], f)
				}
			}
		}
	}
	return adapter.Manifest{Name: b.Source.Adapter, Version: "1", SchemaVersion: b.SchemaVersion, Supplies: supplies,
		Modes: []adapter.Mode{adapter.ModeFull, adapter.ModeIncremental}}
}

// sensitiveNumber replaces numeric sensitive values in the fixture.
const sensitiveNumber = 9876543

// markSensitive gives every sensitive field of b a value found nowhere else:
// "SENSITIVE-<entity>-<field>" for text, sensitiveNumber for numbers.
func markSensitive(t *testing.T, b adapter.Batch) {
	t.Helper()
	sens, err := crypt.LoadSensitivity(b.SchemaVersion)
	if err != nil {
		t.Fatal(err)
	}
	for entity, recs := range b.Entities {
		for _, r := range recs {
			for field := range sens[entity] {
				switch r[field].(type) {
				case string:
					r[field] = "SENSITIVE-" + entity + "-" + field
				case float64, json.Number:
					r[field] = sensitiveNumber
				}
			}
		}
	}
}

// fixture is a workspace with the sample register, an approved control with
// evidence, a rejected control, revoked evidence and texts that must stay
// out of reports.
type fixture struct {
	eng   *compliance.Engine
	st    *memory.Store
	owner context.Context
	ids   map[string]string // actor -> email
	eval  compliance.Evaluation
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	st := memory.New()
	now := time.Date(2026, 9, 30, 9, 0, 0, 0, time.UTC)
	n := 0
	eng, err := compliance.New(ctx, compliance.Config{Store: st, Clock: func() time.Time { return now },
		NewID: func(p string) string { n++; return fmt.Sprintf("%s-%d", p, n) }})
	if err != nil {
		t.Fatal(err)
	}
	ids := identity.New(st, identity.Options{Clock: func() time.Time { return now }})
	o, err := ids.AddMember(ctx, scope, "olga@example.com", []access.Role{access.RoleOwner})
	if err != nil {
		t.Fatal(err)
	}
	a, err := ids.AddMember(ctx, scope, "arno@example.com", []access.Role{access.RoleApprover})
	if err != nil {
		t.Fatal(err)
	}
	f := &fixture{eng: eng, st: st, owner: principal(o.UserID, access.RoleOwner),
		ids: map[string]string{"user:" + o.UserID: "olga@example.com", "user:" + a.UserID: "arno@example.com"}}
	approver := principal(a.UserID, access.RoleApprover)
	b := sampleBatch(t)
	markSensitive(t, b)
	if err := eng.RegisterManifest(ctx, scope, manifestFor(b)); err != nil {
		t.Fatal(err)
	}
	if _, err := eng.Ingest(ctx, scope, b); err != nil {
		t.Fatal(err)
	}
	const approved, rejected = "dora-roi-provider-identification", "dora-roi-reporting-entity"
	owner, notes := "olga@example.com", secretNote+"-assessment"
	due := "2026-06-30T00:00:00Z"
	for _, ctl := range []string{approved, rejected} {
		if _, err := eng.Assign(f.owner, scope, "dora", ctl, compliance.AssignInput{Owner: &owner, Notes: &notes, DueAt: &due}); err != nil {
			t.Fatal(err)
		}
	}
	sum := sha256.Sum256([]byte("exit plan"))
	if _, err := eng.AddEvidence(f.owner, scope, compliance.EvidenceInput{Title: "Provider register extract", Kind: "document", Source: "GRC",
		URI:      "https://dms.example.com:8443/" + secretPath + "/register.pdf?version=" + secretQuery + "#" + secretPath,
		Checksum: "sha256:" + hex.EncodeToString(sum[:]), Links: []store.ControlRef{{Catalog: "dora", ControlID: approved}}}); err != nil {
		t.Fatal(err)
	}
	revoked, err := eng.AddEvidence(f.owner, scope, compliance.EvidenceInput{Title: "Old attestation", Kind: "attestation", Source: "manual",
		URI: "urn:example:" + secretPath, Checksum: "sha256:" + hex.EncodeToString(sum[:]),
		Links: []store.ControlRef{{Catalog: "dora", ControlID: rejected}}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := eng.RevokeEvidence(f.owner, scope, revoked.ID, secretReason+"-revoke"); err != nil {
		t.Fatal(err)
	}
	for _, ctl := range []string{approved, rejected} {
		if _, err := eng.Act(f.owner, scope, "dora", ctl, workflow.ActionSubmit, compliance.ActInput{Note: secretNote + "-submit"}); err != nil {
			t.Fatal(err)
		}
	}
	ev, err := eng.Evaluate(f.owner, scope, "", []catalog.Ref{dora})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := eng.Act(approver, scope, "dora", approved, workflow.ActionApprove, compliance.ActInput{EvaluationID: ev.ID, Note: secretNote + "-approve"}); err != nil {
		t.Fatal(err)
	}
	if _, err := eng.Act(approver, scope, "dora", rejected, workflow.ActionReject, compliance.ActInput{Reason: secretReason + "-reject"}); err != nil {
		t.Fatal(err)
	}
	if f.eval, err = eng.Evaluate(f.owner, scope, "", []catalog.Ref{dora}); err != nil {
		t.Fatal(err)
	}
	return f
}

// input gathers a profile input from the engine's public API, as the facade does.
func (f *fixture) input(t *testing.T, sample bool) extension.ReportInput {
	t.Helper()
	snap, err := f.eng.Snapshot(ctx, scope, f.eval.SnapshotID)
	if err != nil {
		t.Fatal(err)
	}
	comp, err := f.eng.Completeness(ctx, scope, f.eval.SnapshotID)
	if err != nil {
		t.Fatal(err)
	}
	cat, err := f.eng.Catalog(dora)
	if err != nil {
		t.Fatal(err)
	}
	evs, err := f.st.ListEvidence(ctx, scope, store.EvidenceQuery{})
	if err != nil {
		t.Fatal(err)
	}
	asOf := f.eval.Effective.AsOf
	var rev []extension.ReportEvidence
	for _, e := range evs {
		rev = append(rev, extension.ReportEvidence{ID: e.ID, Title: e.Title, Kind: e.Kind, Source: e.Source, Location: report.Redact(e.URI),
			Checksum: e.Checksum, Integrity: e.Integrity, CollectedAt: e.CollectedAt, ValidUntil: e.ValidUntil, RevokedAt: e.RevokedAt,
			Links: e.Links, State: e.StateAt(asOf)})
	}
	return extension.ReportInput{
		Meta: extension.ReportMeta{ReportID: "rpt-1", Profile: report.ProfileBID, Scope: scope, EngineVersion: "test", SchemaVersion: "0.1.0",
			SnapshotID: f.eval.SnapshotID, EvaluationID: f.eval.ID, Catalogs: []catalog.Ref{dora}, AsOf: asOf,
			GeneratedAt: asOf.Add(time.Minute), GeneratedBy: "user:x", GeneratorKind: extension.ActorUser, Sample: sample},
		Schema: f.eng.Schema(), Catalogs: []*catalog.Catalog{cat}, Records: snap.Records, Computed: f.eval.Result,
		Effective: *f.eval.Effective, Completeness: comp, Evidence: rev, People: f.ids,
	}
}

func generate(t *testing.T, in extension.ReportInput) extension.ReportOutput {
	t.Helper()
	out, err := report.ProfileB().Generate(ctx, in)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func file(t *testing.T, out extension.ReportOutput, name string) []byte {
	t.Helper()
	for _, f := range out.Files {
		if f.Name == name {
			return f.Data
		}
	}
	t.Fatalf("no file %s in %d files", name, len(out.Files))
	return nil
}
