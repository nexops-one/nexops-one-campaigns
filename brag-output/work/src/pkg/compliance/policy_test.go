// SPDX-License-Identifier: Apache-2.0

package compliance_test

import (
	"context"
	"errors"
	"testing"

	"github.com/nexops-one/compliance-engine/pkg/access"
	"github.com/nexops-one/compliance-engine/pkg/compliance"
	"github.com/nexops-one/compliance-engine/pkg/engine"
	"github.com/nexops-one/compliance-engine/pkg/extension"
	"github.com/nexops-one/compliance-engine/pkg/identity"
	"github.com/nexops-one/compliance-engine/pkg/store"
	"github.com/nexops-one/compliance-engine/pkg/store/memory"
	"github.com/nexops-one/compliance-engine/pkg/workflow"
	"github.com/nexops-one/compliance-engine/sdk/adapter"
)

// twoApprovers requires two approvers and a recommendation for every control.
type twoApprovers struct{ asked *int }

func (p twoApprovers) Requirements(_ context.Context, _ adapter.Scope, catalogName, _ string) extension.ApprovalRequirements {
	*p.asked++
	return extension.ApprovalRequirements{MinApprovers: 2, RequireReviewerRecommendation: catalogName == "dora"}
}

func TestWorkflowPolicyApplied(t *testing.T) {
	asked := 0
	w := newWorkflowEnv(t, func(c *compliance.Config) { c.Extensions.WorkflowPolicy = twoApprovers{&asked} })
	ids := identity.New(w.st, identity.Options{})
	second, err := ids.AddMember(ctx, scopeA, "bea@example.com", []access.Role{access.RoleApprover})
	if err != nil {
		t.Fatal(err)
	}
	approver2 := principalCtx(scopeA, second.UserID, access.RoleApprover)
	w.addEvidence(t, "register.pdf", []byte("register"), monitored)
	if _, err := w.eng.Act(w.owner, scopeA, "dora", monitored, workflow.ActionSubmit, compliance.ActInput{}); err != nil {
		t.Fatal(err)
	}
	ev := w.evaluate(t)
	in := compliance.ActInput{EvaluationID: ev.ID}
	if _, err := w.eng.Act(w.approver, scopeA, "dora", monitored, workflow.ActionApprove, in); !errors.Is(err, workflow.ErrRecommendationRequired) {
		t.Fatalf("approve before recommendation = %v", err)
	}
	if _, err := w.eng.Act(w.approver, scopeA, "dora", monitored, workflow.ActionRecommend, compliance.ActInput{Note: "looks right"}); err != nil {
		t.Fatal(err)
	}
	a, err := w.eng.Act(w.approver, scopeA, "dora", monitored, workflow.ActionApprove, in)
	if err != nil || a.Stage != store.StageSubmitted || len(a.Approvals) != 1 {
		t.Fatalf("first approval = %+v, %v", a, err)
	}
	if c := w.status(t, monitored); c.Status != engine.StatusInReview || c.Approvals == nil || c.Approvals.Given != 1 || c.Approvals.Required != 2 {
		t.Fatalf("status during the quorum = %+v", c)
	}
	a, err = w.eng.Act(approver2, scopeA, "dora", monitored, workflow.ActionApprove, in)
	if err != nil || a.Stage != store.StageApproved || len(a.Approval.Approvers) != 2 {
		t.Fatalf("second approval = %+v, %v", a, err)
	}
	if c := w.status(t, monitored); c.Status != engine.StatusReady {
		t.Fatalf("status after the quorum = %+v", c)
	}
	if asked != 3 {
		t.Fatalf("the policy was asked %d times, want once per approval", asked)
	}
}

func TestSettingsExtension(t *testing.T) {
	st := memory.New()
	eng := newEngine(t, func(c *compliance.Config) { c.Store = st })
	if v, err := eng.SettingsExtension(ctx, scopeA, "workflow.advanced"); err != nil || v != nil {
		t.Fatalf("unset = %s, %v", v, err)
	}
	if _, err := eng.PutSettingsExtension(ctx, scopeA, "workflow.advanced", []byte(`{"rules":[]}`)); err != nil {
		t.Fatal(err)
	}
	if v, _ := eng.SettingsExtension(ctx, scopeA, "workflow.advanced"); string(v) != `{"rules":[]}` {
		t.Fatalf("stored = %s", v)
	}
	if _, err := eng.PutSettingsExtension(ctx, scopeA, "workflow.advanced", []byte(`{`)); !errors.Is(err, compliance.ErrInvalidSettings) {
		t.Fatalf("invalid JSON = %v", err)
	}
	if _, err := eng.PutSettingsExtension(ctx, scopeA, "workflow.advanced", nil); err != nil {
		t.Fatal(err)
	}
	if v, _ := eng.SettingsExtension(ctx, scopeA, "workflow.advanced"); v != nil {
		t.Fatalf("removed = %s", v)
	}
	evs, _ := st.AuditEvents(ctx, scopeA, store.AuditQuery{})
	if len(evs) != 2 || evs[0].Action != "settings.update" {
		t.Fatalf("audit = %+v", evs)
	}
}
