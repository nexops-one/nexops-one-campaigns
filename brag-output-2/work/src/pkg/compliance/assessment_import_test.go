// SPDX-License-Identifier: Apache-2.0

package compliance_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/nexops-one/compliance-engine/pkg/compliance"
	"github.com/nexops-one/compliance-engine/pkg/engine"
	"github.com/nexops-one/compliance-engine/pkg/extension"
	"github.com/nexops-one/compliance-engine/pkg/store"
	"github.com/nexops-one/compliance-engine/pkg/workflow"
)

// migration is the operator context of an import.
func migration() context.Context {
	return extension.WithPrincipal(ctx, extension.Principal{Scope: scopeA, Actor: "migration:m8", Kind: extension.ActorSystem})
}

func (w *wfEnv) importApproved(t *testing.T, control string, at, expires time.Time) {
	t.Helper()
	_, err := w.eng.ImportAssessment(migration(), scopeA, compliance.ImportInput{
		Catalog: "dora", ControlID: control, Owner: "user:" + w.ownerID, Notes: "from M8",
		Approval: &compliance.ImportedApproval{Actor: "migration:m8", At: at, ExpiresAt: expires},
		Reason:   "migrated from M8 manual status", Note: "M8 status: ready",
	})
	if err != nil {
		t.Fatalf("import %s: %v", control, err)
	}
}

func TestImportAssessment(t *testing.T) {
	w := newWorkflowEnv(t)
	w.addEvidence(t, "p.pdf", []byte("p"), monitored)
	w.importApproved(t, monitored, w.now.AddDate(0, -1, 0), w.now.AddDate(0, 6, 0))

	view, err := w.eng.Assessment(ctx, scopeA, "dora", monitored)
	if err != nil {
		t.Fatal(err)
	}
	a := view.Assessment
	if a.Stage != store.StageApproved || a.Owner != "user:"+w.ownerID || a.Notes != "from M8" || a.Approval == nil || !a.Approval.Imported ||
		a.Approval.Actor != "migration:m8" || a.Approval.EvaluationID != "" {
		t.Fatalf("assessment = %+v (approval %+v)", a, a.Approval)
	}
	if len(view.History) != 1 || view.History[0].Action != "import" || view.History[0].Actor != "migration:m8" || view.History[0].ActorKind != "system" ||
		view.History[0].Reason != "migrated from M8 manual status" || view.History[0].Note != "M8 status: ready" {
		t.Fatalf("history = %+v", view.History)
	}
	if c := w.status(t, monitored); c.Status != engine.StatusReady {
		t.Fatalf("imported approval with checksummed evidence = %+v", c)
	}
	evs, _ := w.st.AuditEvents(ctx, scopeA, store.AuditQuery{})
	if last := evs[len(evs)-1]; last.Action != "assessment.import" || last.Actor != "migration:m8" || last.TargetID != "dora/"+monitored {
		t.Fatalf("audit = %+v", last)
	}

	// The owner can work with it like any approval.
	if _, err := w.eng.Act(w.owner, scopeA, "dora", monitored, workflow.ActionReopen, compliance.ActInput{Reason: "re-check"}); err != nil {
		t.Fatalf("reopen an imported approval: %v", err)
	}
}

func TestImportAssessmentRefusals(t *testing.T) {
	w := newWorkflowEnv(t)
	plain := compliance.ImportInput{Catalog: "dora", ControlID: monitored, Reason: "migrated"}
	if _, err := w.eng.ImportAssessment(migration(), scopeA, plain); err != nil {
		t.Fatal(err)
	}
	if _, err := w.eng.ImportAssessment(migration(), scopeA, plain); !errors.Is(err, workflow.ErrInvalidTransition) {
		t.Fatalf("second import = %v", err)
	}
	other := plain
	other.ControlID = manualCtl
	if _, err := w.eng.ImportAssessment(w.owner, scopeA, other); !errors.Is(err, workflow.ErrInvalidTransition) {
		t.Fatalf("import by a user = %v", err)
	}
	unknown := plain
	unknown.ControlID = "no-such-control"
	if _, err := w.eng.ImportAssessment(migration(), scopeA, unknown); !errors.Is(err, compliance.ErrUnknownControl) {
		t.Fatalf("unknown control = %v", err)
	}
	ghost := other
	ghost.Owner = "user:nobody"
	if _, err := w.eng.ImportAssessment(migration(), scopeA, ghost); !errors.Is(err, compliance.ErrInvalidAssignee) {
		t.Fatalf("unknown owner = %v", err)
	}
	noReason := other
	noReason.Reason = ""
	if _, err := w.eng.ImportAssessment(migration(), scopeA, noReason); !errors.Is(err, workflow.ErrReasonRequired) {
		t.Fatalf("no reason = %v", err)
	}
	// Owners are not role-checked: the import carries over who owned the control.
	approverOwned := other
	approverOwned.Owner = "user:" + w.apprID
	if _, err := w.eng.ImportAssessment(migration(), scopeA, approverOwned); err != nil {
		t.Fatalf("owner without the owner role = %v", err)
	}
}

func TestImportedApprovalVoided(t *testing.T) {
	t.Run("rule failed", func(t *testing.T) {
		w := newWorkflowEnv(t)
		w.importApproved(t, ruleFailed, w.now.AddDate(0, -1, 0), w.now.AddDate(0, 6, 0))
		if c := w.status(t, ruleFailed); c.Status != engine.StatusInReview || c.Attention != workflow.AttentionRuleFailed || c.VoidReason != workflow.VoidRuleFailed {
			t.Fatalf("live = %+v", c)
		}
		w.evaluate(t)
		view, _ := w.eng.Assessment(ctx, scopeA, "dora", ruleFailed)
		if view.Assessment.Stage != store.StageNone || len(view.History) != 2 || view.History[1].Action != "void" {
			t.Fatalf("after a stored evaluation = %+v %+v", view.Assessment, view.History)
		}
	})
	t.Run("no checksummed evidence", func(t *testing.T) {
		w := newWorkflowEnv(t, func(c *compliance.Config) { c.Workflow.AllowEvidenceWithoutChecksum = true })
		if _, err := w.eng.AddEvidence(w.owner, scopeA, compliance.EvidenceInput{Title: "old", Kind: "document", Source: "M8", URI: "urn:m8:evidence:old",
			Links: []store.ControlRef{{Catalog: "dora", ControlID: manualCtl}}}); err != nil {
			t.Fatal(err)
		}
		w.importApproved(t, manualCtl, w.now.AddDate(0, -1, 0), w.now.AddDate(0, 6, 0))
		if c := w.status(t, manualCtl); c.Status != engine.StatusInReview || c.Attention != workflow.AttentionEvidenceInvalid {
			t.Fatalf("imported approval resting on checksum-less evidence = %+v", c)
		}
	})
}

func TestImportedApprovalExpires(t *testing.T) {
	w := newWorkflowEnv(t)
	w.addEvidence(t, "p.pdf", []byte("p"), monitored)
	w.importApproved(t, monitored, w.now.AddDate(-2, 0, 0), w.now.AddDate(0, 0, -1))
	if c := w.status(t, monitored); c.Status != engine.StatusExpired || c.Attention != workflow.AttentionApprovalExpired {
		t.Fatalf("past review date = %+v", c)
	}
}

func TestEvidenceWithoutChecksum(t *testing.T) {
	in := compliance.EvidenceInput{Title: "policy", Kind: "document", Source: "M8", URI: "urn:m8:evidence:1",
		Links: []store.ControlRef{{Catalog: "dora", ControlID: manualCtl}}}

	off := newWorkflowEnv(t)
	if _, err := off.eng.AddEvidence(off.owner, scopeA, in); !errors.Is(err, compliance.ErrInvalidEvidence) {
		t.Fatalf("without the option a checksum is required: %v", err)
	}

	w := newWorkflowEnv(t, func(c *compliance.Config) { c.Workflow.AllowEvidenceWithoutChecksum = true })
	ev, err := w.eng.AddEvidence(w.owner, scopeA, in)
	if err != nil {
		t.Fatal(err)
	}
	if ev.Integrity != store.IntegrityNoChecksum || ev.Checksum != "" || ev.StateAt(*w.now) != store.EvidenceActive {
		t.Fatalf("evidence = %+v", ev)
	}
	if _, err := w.eng.Act(w.owner, scopeA, "dora", manualCtl, workflow.ActionSubmit, compliance.ActInput{}); err != nil {
		t.Fatal(err)
	}
	if _, err := w.eng.Act(w.approver, scopeA, "dora", manualCtl, workflow.ActionApprove, compliance.ActInput{EvaluationID: w.evaluate(t).ID}); !errors.Is(err, workflow.ErrEvidenceRequired) {
		t.Fatalf("approve with only checksum-less evidence = %v", err)
	}
	if _, err := w.eng.VerifyEvidence(w.owner, scopeA, ev.ID); !errors.Is(err, compliance.ErrNoChecksum) {
		t.Fatalf("engine-side check without a checksum = %v", err)
	}

	// The first attested check supplies the checksum.
	sum := checksumOf([]byte("the policy"))
	adopted, err := w.eng.AttestEvidenceCheck(w.owner, scopeA, ev.ID, sum)
	if err != nil {
		t.Fatal(err)
	}
	if adopted.Checksum != sum || adopted.Integrity != store.IntegrityVerified || len(adopted.Checks) != 1 ||
		adopted.Checks[0].Method != "attested_adopt" || !adopted.Checks[0].Match {
		t.Fatalf("adopted = %+v", adopted)
	}
	if _, err := w.eng.Act(w.approver, scopeA, "dora", manualCtl, workflow.ActionApprove, compliance.ActInput{EvaluationID: w.evaluate(t).ID}); err != nil {
		t.Fatalf("approve after adoption = %v", err)
	}
	// Later checks compare against the adopted checksum.
	again, err := w.eng.AttestEvidenceCheck(w.owner, scopeA, ev.ID, checksumOf([]byte("another file")))
	if err != nil || again.Integrity != store.IntegrityFailed || again.Checksum != sum {
		t.Fatalf("mismatch after adoption = %+v, %v", again, err)
	}
}
