// SPDX-License-Identifier: Apache-2.0

package compliance_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nexops-one/compliance-engine/pkg/access"
	"github.com/nexops-one/compliance-engine/pkg/catalog"
	"github.com/nexops-one/compliance-engine/pkg/compliance"
	"github.com/nexops-one/compliance-engine/pkg/engine"
	"github.com/nexops-one/compliance-engine/pkg/evidence"
	"github.com/nexops-one/compliance-engine/pkg/extension"
	"github.com/nexops-one/compliance-engine/pkg/identity"
	"github.com/nexops-one/compliance-engine/pkg/store"
	"github.com/nexops-one/compliance-engine/pkg/store/memory"
	"github.com/nexops-one/compliance-engine/pkg/workflow"
)

const (
	monitored  = "dora-roi-provider-identification"
	manualCtl  = "dora-incident-readiness"
	ruleFailed = "dora-exit-plans"
)

type wfEnv struct {
	eng             *compliance.Engine
	st              *memory.Store
	now             *time.Time
	owner, approver context.Context // contexts carrying the two users' principals
	ownerID, apprID string
	evidenceRoot    string
}

func principalCtx(scope compliance.Scope, userID string, roles ...access.Role) context.Context {
	return extension.WithPrincipal(ctx, extension.Principal{Scope: scope, Actor: "user:" + userID, Kind: extension.ActorUser, Roles: roles})
}

func newWorkflowEnv(t *testing.T, mutate ...func(*compliance.Config)) *wfEnv {
	t.Helper()
	st := memory.New()
	now := time.Date(2026, 9, 30, 9, 0, 0, 0, time.UTC)
	env := &wfEnv{st: st, now: &now, evidenceRoot: t.TempDir()}
	n := 0
	cfg := compliance.Config{
		Store: st, Clock: func() time.Time { return *env.now },
		NewID:    func(prefix string) string { n++; return fmt.Sprintf("%s-%d", prefix, n) },
		Workflow: compliance.WorkflowOptions{Verifier: evidence.Verifier{Root: env.evidenceRoot}},
	}
	for _, m := range mutate {
		m(&cfg)
	}
	eng, err := compliance.New(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	env.eng = eng
	ids := identity.New(st, identity.Options{Clock: cfg.Clock})
	o, err := ids.AddMember(ctx, scopeA, "olga@example.com", []access.Role{access.RoleOwner})
	if err != nil {
		t.Fatal(err)
	}
	a, err := ids.AddMember(ctx, scopeA, "arno@example.com", []access.Role{access.RoleApprover})
	if err != nil {
		t.Fatal(err)
	}
	env.ownerID, env.apprID = o.UserID, a.UserID
	env.owner = principalCtx(scopeA, o.UserID, access.RoleOwner)
	env.approver = principalCtx(scopeA, a.UserID, access.RoleApprover)
	loadSample(t, eng)
	return env
}

func checksumOf(b []byte) string {
	s := sha256.Sum256(b)
	return "sha256:" + hex.EncodeToString(s[:])
}

// addEvidence writes a file under the evidence root and references it.
func (w *wfEnv) addEvidence(t *testing.T, name string, content []byte, links ...string) store.Evidence {
	t.Helper()
	p := filepath.Join(w.evidenceRoot, name)
	if err := os.WriteFile(p, content, 0o644); err != nil {
		t.Fatal(err)
	}
	refs := []store.ControlRef{}
	for _, l := range links {
		refs = append(refs, store.ControlRef{Catalog: "dora", ControlID: l})
	}
	ev, err := w.eng.AddEvidence(w.owner, scopeA, compliance.EvidenceInput{
		Title: name, Kind: "document", Source: "dms", URI: "file:///" + strings.TrimPrefix(filepath.ToSlash(p), "/"),
		Checksum: checksumOf(content), Retention: store.Retention{MinDays: 3650}, Links: refs,
	})
	if err != nil {
		t.Fatal(err)
	}
	return ev
}

func (w *wfEnv) evaluate(t *testing.T) compliance.Evaluation {
	t.Helper()
	ev, err := w.eng.Evaluate(w.owner, scopeA, "", []catalog.Ref{{Catalog: "dora", Version: "1.0.0"}})
	if err != nil {
		t.Fatal(err)
	}
	return ev
}

func effectiveOf(t *testing.T, r *workflow.Result, control string) workflow.Control {
	t.Helper()
	for _, fw := range r.Frameworks {
		for _, c := range fw.Controls {
			if c.ControlID == control {
				return c
			}
		}
	}
	t.Fatalf("control %s not in result", control)
	return workflow.Control{}
}

func (w *wfEnv) status(t *testing.T, control string) workflow.Control {
	t.Helper()
	r, err := w.eng.Status(w.owner, scopeA, []catalog.Ref{{Catalog: "dora", Version: "1.0.0"}})
	if err != nil {
		t.Fatal(err)
	}
	return effectiveOf(t, &r, control)
}

func (w *wfEnv) approve(t *testing.T, control string) {
	t.Helper()
	if _, err := w.eng.Act(w.owner, scopeA, "dora", control, workflow.ActionSubmit, compliance.ActInput{}); err != nil {
		t.Fatalf("submit %s: %v", control, err)
	}
	ev := w.evaluate(t)
	if _, err := w.eng.Act(w.approver, scopeA, "dora", control, workflow.ActionApprove, compliance.ActInput{EvaluationID: ev.ID}); err != nil {
		t.Fatalf("approve %s: %v", control, err)
	}
}

func TestWorkflowToReady(t *testing.T) {
	w := newWorkflowEnv(t)
	owner, reviewer := "olga@example.com", "arno@example.com"
	if _, err := w.eng.Assign(w.owner, scopeA, "dora", monitored, compliance.AssignInput{Owner: &owner, Reviewer: &reviewer}); err != nil {
		t.Fatal(err)
	}
	if _, err := w.eng.Assign(w.owner, scopeA, "dora", monitored, compliance.AssignInput{Owner: &reviewer}); !errors.Is(err, compliance.ErrInvalidAssignee) {
		t.Fatalf("an approver without the owner role cannot own a control: %v", err)
	}
	if c := w.status(t, monitored); c.Status != engine.StatusMonitoring || c.Owner != "user:"+w.ownerID {
		t.Fatalf("before submit = %+v", c)
	}
	if _, err := w.eng.Act(w.owner, scopeA, "dora", monitored, workflow.ActionSubmit, compliance.ActInput{}); err != nil {
		t.Fatal(err)
	}
	if c := w.status(t, monitored); c.Status != engine.StatusInReview || c.Attention != workflow.AttentionAwaitingApproval {
		t.Fatalf("submitted = %+v", c)
	}
	if _, err := w.eng.Act(w.approver, scopeA, "dora", monitored, workflow.ActionReject, compliance.ActInput{Reason: "attach the provider register extract"}); err != nil {
		t.Fatal(err)
	}
	if c := w.status(t, monitored); c.Status != engine.StatusRejected {
		t.Fatalf("rejected = %+v", c)
	}
	ev := w.evaluate(t)
	if _, err := w.eng.Act(w.owner, scopeA, "dora", monitored, workflow.ActionSubmit, compliance.ActInput{Note: "resubmitted"}); err != nil {
		t.Fatal(err)
	}
	if _, err := w.eng.Act(w.approver, scopeA, "dora", monitored, workflow.ActionApprove, compliance.ActInput{EvaluationID: ev.ID}); !errors.Is(err, workflow.ErrEvidenceRequired) {
		t.Fatalf("approve without evidence = %v", err)
	}
	w.addEvidence(t, "providers.pdf", []byte("provider register extract"), monitored)
	if _, err := w.eng.Act(w.approver, scopeA, "dora", monitored, workflow.ActionApprove, compliance.ActInput{}); !errors.Is(err, compliance.ErrStaleEvaluation) {
		t.Fatalf("approve without evaluation = %v", err)
	}
	a, err := w.eng.Act(w.approver, scopeA, "dora", monitored, workflow.ActionApprove, compliance.ActInput{EvaluationID: ev.ID})
	if err != nil || a.Stage != store.StageApproved || a.Approval.Actor != "user:"+w.apprID || a.Approval.SnapshotID != "rev-1" {
		t.Fatalf("approve = %+v %v", a, err)
	}
	stored := w.evaluate(t)
	c := effectiveOf(t, stored.Effective, monitored)
	if c.Status != engine.StatusReady || stored.Effective.Frameworks[0].Tally.Ready != 1 {
		t.Fatalf("ready = %+v tally %+v", c, stored.Effective.Frameworks[0].Tally)
	}
	if stored.Result.Frameworks[0].Tally.Ready != 0 {
		t.Fatal("the computed result never contains ready")
	}
	view, err := w.eng.Assessment(ctx, scopeA, "dora", monitored)
	if err != nil || len(view.History) != 5 || view.History[4].Action != "approve" || view.History[2].Reason == "" {
		t.Fatalf("history = %+v %v", view.History, err)
	}
}

func TestManualControlNeedsEvidenceAndApproval(t *testing.T) {
	w := newWorkflowEnv(t)
	if c := w.status(t, manualCtl); c.Status != engine.StatusNotAssessed || !c.RequiresEvidence {
		t.Fatalf("manual before = %+v", c)
	}
	w.addEvidence(t, "incident-test.pdf", []byte("incident exercise report"), manualCtl)
	w.approve(t, manualCtl)
	if c := w.status(t, manualCtl); c.Status != engine.StatusReady || c.ComputedStatus != engine.StatusNotAssessed {
		t.Fatalf("manual after approval = %+v", c)
	}
	if _, err := w.eng.Act(w.owner, scopeA, "dora", ruleFailed, workflow.ActionSubmit, compliance.ActInput{}); !errors.Is(err, workflow.ErrInvalidTransition) {
		t.Fatalf("a failing rule cannot be submitted: %v", err)
	}
}

func TestApproveSeparationOfDutiesInEngine(t *testing.T) {
	w := newWorkflowEnv(t)
	w.addEvidence(t, "p.pdf", []byte("p"), monitored)
	if _, err := w.eng.Act(w.owner, scopeA, "dora", monitored, workflow.ActionSubmit, compliance.ActInput{}); err != nil {
		t.Fatal(err)
	}
	ev := w.evaluate(t)
	ownerAsApprover := principalCtx(scopeA, w.ownerID, access.RoleOwner, access.RoleApprover)
	if _, err := w.eng.Act(ownerAsApprover, scopeA, "dora", monitored, workflow.ActionApprove, compliance.ActInput{EvaluationID: ev.ID}); !errors.Is(err, workflow.ErrSelfApproval) {
		t.Fatalf("self approval = %v", err)
	}

	allowed := newWorkflowEnv(t, func(c *compliance.Config) { c.Workflow.AllowSelfApproval = true })
	allowed.addEvidence(t, "p.pdf", []byte("p"), monitored)
	if _, err := allowed.eng.Act(allowed.owner, scopeA, "dora", monitored, workflow.ActionSubmit, compliance.ActInput{}); err != nil {
		t.Fatal(err)
	}
	ev = allowed.evaluate(t)
	self := principalCtx(scopeA, allowed.ownerID, access.RoleOwner, access.RoleApprover)
	a, err := allowed.eng.Act(self, scopeA, "dora", monitored, workflow.ActionApprove, compliance.ActInput{EvaluationID: ev.ID})
	if err != nil || !a.Approval.SelfApproved {
		t.Fatalf("allowed self approval = %+v %v", a.Approval, err)
	}
	evs, _ := allowed.st.AuditEvents(ctx, scopeA, store.AuditQuery{})
	last := evs[len(evs)-1]
	if last.Action != "assessment.approve" || !strings.Contains(string(last.Details), `"self_approved":true`) {
		t.Fatalf("self approval must be flagged in the audit log: %+v", last)
	}
}

func TestStaleEvaluation(t *testing.T) {
	w := newWorkflowEnv(t)
	w.addEvidence(t, "p.pdf", []byte("p"), monitored)
	if _, err := w.eng.Act(w.owner, scopeA, "dora", monitored, workflow.ActionSubmit, compliance.ActInput{}); err != nil {
		t.Fatal(err)
	}
	old := w.evaluate(t)
	b := sampleBatch(t)
	b.Batch.Mode = "incremental"
	b.Entities["ict_provider"][0]["legal_name"] = "Renamed Provider SA"
	if _, err := w.eng.Ingest(ctx, scopeA, b); err != nil {
		t.Fatal(err)
	}
	if _, err := w.eng.Act(w.approver, scopeA, "dora", monitored, workflow.ActionApprove, compliance.ActInput{EvaluationID: old.ID}); !errors.Is(err, compliance.ErrStaleEvaluation) {
		t.Fatalf("an evaluation of an older snapshot = %v", err)
	}
	fresh := w.evaluate(t)
	if _, err := w.eng.Act(w.approver, scopeA, "dora", monitored, workflow.ActionApprove, compliance.ActInput{EvaluationID: fresh.ID}); err != nil {
		t.Fatalf("a fresh evaluation = %v", err)
	}
}

func TestApprovalIsVoidedWhenInputsBreak(t *testing.T) {
	t.Run("revoked evidence", func(t *testing.T) {
		w := newWorkflowEnv(t)
		ev := w.addEvidence(t, "p.pdf", []byte("p"), monitored)
		w.approve(t, monitored)
		if _, err := w.eng.RevokeEvidence(w.owner, scopeA, ev.ID, "superseded"); err != nil {
			t.Fatal(err)
		}
		if c := w.status(t, monitored); c.Status != engine.StatusInReview || c.Attention != workflow.AttentionEvidenceInvalid {
			t.Fatalf("live status after revocation = %+v", c)
		}
		stored := w.evaluate(t)
		if c := effectiveOf(t, stored.Effective, monitored); c.VoidReason != workflow.VoidEvidenceInvalid {
			t.Fatalf("stored = %+v", c)
		}
		view, _ := w.eng.Assessment(ctx, scopeA, "dora", monitored)
		last := view.History[len(view.History)-1]
		if view.Assessment.Stage != store.StageNone || last.Action != "void" || last.Actor != "system" || last.Reason != workflow.VoidEvidenceInvalid {
			t.Fatalf("void = %+v %+v", view.Assessment, last)
		}
		if c := w.status(t, monitored); c.Status != engine.StatusMonitoring {
			t.Fatalf("after the void the control is back to its computed status: %+v", c)
		}
	})
	t.Run("failed integrity check, then recovery", func(t *testing.T) {
		w := newWorkflowEnv(t)
		ev := w.addEvidence(t, "p.pdf", []byte("p"), monitored)
		w.approve(t, monitored)
		if err := os.WriteFile(filepath.Join(w.evidenceRoot, "p.pdf"), []byte("tampered"), 0o644); err != nil {
			t.Fatal(err)
		}
		got, err := w.eng.VerifyEvidence(w.owner, scopeA, ev.ID)
		if err != nil || got.Integrity != store.IntegrityFailed || len(got.Checks) != 1 || got.Checks[0].Match || got.Checks[0].Method != "engine_file" {
			t.Fatalf("verify = %+v %v", got, err)
		}
		if c := w.status(t, monitored); c.Status != engine.StatusInReview || c.Attention != workflow.AttentionEvidenceInvalid {
			t.Fatalf("after a failed check = %+v", c)
		}
		got, err = w.eng.AttestEvidenceCheck(w.owner, scopeA, ev.ID, checksumOf([]byte("p")))
		if err != nil || got.Integrity != store.IntegrityVerified || len(got.Checks) != 2 {
			t.Fatalf("recovery = %+v %v", got, err)
		}
		if c := w.status(t, monitored); c.Status != engine.StatusReady {
			t.Fatalf("a later successful check restores the evidence: %+v", c)
		}
	})
	t.Run("data regression", func(t *testing.T) {
		w := newWorkflowEnv(t)
		w.addEvidence(t, "p.pdf", []byte("p"), monitored)
		w.approve(t, monitored)
		b := sampleBatch(t)
		b.Batch.Mode = "incremental"
		for _, rec := range b.Entities["ict_provider"] {
			delete(rec, "legal_name")
		}
		if _, err := w.eng.Ingest(ctx, scopeA, b); err != nil {
			t.Fatal(err)
		}
		stored := w.evaluate(t)
		c := effectiveOf(t, stored.Effective, monitored)
		if c.Status != engine.StatusNotAssessed || c.VoidReason != workflow.VoidInputsIncomplete {
			t.Fatalf("after the data broke = %+v", c)
		}
	})
	t.Run("review interval", func(t *testing.T) {
		w := newWorkflowEnv(t)
		w.addEvidence(t, "p.pdf", []byte("p"), monitored)
		w.approve(t, monitored)
		*w.now = w.now.AddDate(1, 0, 1)
		if c := w.status(t, monitored); c.Status != engine.StatusExpired || c.Attention != workflow.AttentionApprovalExpired {
			t.Fatalf("after the review interval = %+v", c)
		}
		if _, err := w.eng.Act(w.owner, scopeA, "dora", monitored, workflow.ActionSubmit, compliance.ActInput{Note: "annual review"}); err != nil {
			t.Fatalf("re-review = %v", err)
		}
	})
}

func TestEvidenceAccessIsAudited(t *testing.T) {
	w := newWorkflowEnv(t)
	ev := w.addEvidence(t, "p.pdf", []byte("p"), monitored)
	if _, err := w.eng.Evidence(w.approver, scopeA, ev.ID); err != nil {
		t.Fatal(err)
	}
	if list, err := w.eng.ListEvidence(w.approver, scopeA, store.EvidenceQuery{Catalog: "dora", ControlID: monitored}); err != nil || len(list) != 1 {
		t.Fatalf("list = %v %v", list, err)
	}
	evs, _ := w.st.AuditEvents(ctx, scopeA, store.AuditQuery{})
	accesses := 0
	for _, e := range evs {
		if e.Action == "evidence.access" && e.Actor == "user:"+w.apprID && strings.Contains(string(e.Details), ev.ID) {
			accesses++
		}
		if e.Action == "evidence.create" && strings.Contains(string(e.Details), w.evidenceRoot[3:]) {
			t.Fatal("audit details must not carry the full evidence location")
		}
	}
	if accesses != 2 {
		t.Fatalf("evidence accesses logged = %d", accesses)
	}
	_, err := w.eng.AddEvidence(w.owner, scopeA, compliance.EvidenceInput{Title: "x", Kind: "document", Source: "s3",
		URI: "https://files.example/a?X-Amz-Signature=abc", Checksum: checksumOf([]byte("x"))})
	if !errors.Is(err, compliance.ErrInvalidEvidence) {
		t.Fatalf("credentials in the URI = %v", err)
	}
	if _, err := w.eng.LinkEvidence(w.owner, scopeA, ev.ID, store.ControlRef{Catalog: "dora", ControlID: "nope"}); !errors.Is(err, compliance.ErrUnknownControl) {
		t.Fatalf("unknown control = %v", err)
	}
	if _, err := w.eng.VerifyEvidence(w.owner, scopeA, mustAddURN(t, w).ID); !errors.Is(err, evidence.ErrCannotVerifyHere) {
		t.Fatalf("urn verification = %v", err)
	}
}

func mustAddURN(t *testing.T, w *wfEnv) store.Evidence {
	t.Helper()
	ev, err := w.eng.AddEvidence(w.owner, scopeA, compliance.EvidenceInput{Title: "GRC test", Kind: "attestation", Source: "grc",
		URI: "urn:grc:test:1", Checksum: checksumOf([]byte("grc"))})
	if err != nil {
		t.Fatal(err)
	}
	return ev
}

func TestStoredEvaluationIsReproducible(t *testing.T) {
	w := newWorkflowEnv(t)
	w.addEvidence(t, "p.pdf", []byte("p"), monitored, manualCtl)
	w.approve(t, monitored)
	stored := w.evaluate(t)
	*w.now = w.now.Add(48 * time.Hour)
	if _, err := w.eng.Act(w.owner, scopeA, "dora", manualCtl, workflow.ActionSubmit, compliance.ActInput{}); err != nil {
		t.Fatal(err)
	}
	again, err := w.eng.Evaluation(ctx, scopeA, stored.ID)
	if err != nil {
		t.Fatal(err)
	}
	cat, _ := w.eng.Catalog(catalog.Ref{Catalog: "dora", Version: "1.0.0"})
	recomputed := workflow.Recompute(again.Result, []*catalog.Catalog{cat}, *again.Effective)
	a, _ := json.Marshal(recomputed)
	b, _ := json.Marshal(again.Effective)
	if string(a) != string(b) {
		t.Fatalf("recomputing a stored effective result must give the same result\n%s\n%s", a, b)
	}
	if c := effectiveOf(t, again.Effective, manualCtl); c.Stage != store.StageNone {
		t.Fatal("a stored result must not change when the workflow moves on")
	}
}

func TestReviewOverrides(t *testing.T) {
	w := newWorkflowEnv(t)
	no := false
	if _, err := w.eng.UpdateSettings(w.owner, scopeA, compliance.SettingsInput{ReviewOverrides: map[string]store.ReviewOverride{
		"dora/" + monitored: {ReviewInterval: "P1M", ApprovalRequiresEvidence: &no},
	}}); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []map[string]store.ReviewOverride{{"dora/nope": {}}, {"no-slash": {}}, {"dora/" + monitored: {ReviewInterval: "PT1H"}}} {
		if _, err := w.eng.UpdateSettings(w.owner, scopeA, compliance.SettingsInput{ReviewOverrides: bad}); !errors.Is(err, compliance.ErrInvalidSettings) {
			t.Errorf("override %v = %v", bad, err)
		}
	}
	if _, err := w.eng.UpdateSettings(w.owner, scopeA, compliance.SettingsInput{Retention: &store.RetentionPolicy{KeepRevisions: 0}}); !errors.Is(err, compliance.ErrInvalidSettings) {
		t.Fatalf("keep_revisions 0 = %v", err)
	}
	w.approve(t, monitored) // no evidence needed with the override
	view, _ := w.eng.Assessment(ctx, scopeA, "dora", monitored)
	if !view.Assessment.Approval.ExpiresAt.Equal(w.now.AddDate(0, 1, 0)) {
		t.Fatalf("the overridden interval must set the expiry: %v", view.Assessment.Approval.ExpiresAt)
	}
	stored := w.evaluate(t)
	c := effectiveOf(t, stored.Effective, monitored)
	if c.Status != engine.StatusReady || c.RequiresEvidence || stored.Effective.Inputs.Overrides["dora/"+monitored].ReviewInterval != "P1M" {
		t.Fatalf("effective with override = %+v inputs %+v", c, stored.Effective.Inputs.Overrides)
	}
	cat, _ := w.eng.Catalog(catalog.Ref{Catalog: "dora", Version: "1.0.0"})
	a, _ := json.Marshal(workflow.Recompute(stored.Result, []*catalog.Catalog{cat}, *stored.Effective))
	b, _ := json.Marshal(stored.Effective)
	if string(a) != string(b) {
		t.Fatal("results with overrides must be reproducible")
	}
	*w.now = w.now.AddDate(0, 1, 1)
	if c := w.status(t, monitored); c.Status != engine.StatusExpired {
		t.Fatalf("after the overridden interval = %+v", c)
	}
	evs, _ := w.st.AuditEvents(ctx, scopeA, store.AuditQuery{})
	found := false
	for _, e := range evs {
		found = found || e.Action == "settings.update"
	}
	if !found {
		t.Fatal("settings changes must be audited")
	}
}
