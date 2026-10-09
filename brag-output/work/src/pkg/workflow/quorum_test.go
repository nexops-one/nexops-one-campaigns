// SPDX-License-Identifier: Apache-2.0

package workflow_test

import (
	"errors"
	"reflect"
	"testing"

	"github.com/nexops-one/compliance-engine/pkg/catalog"
	"github.com/nexops-one/compliance-engine/pkg/engine"
	"github.com/nexops-one/compliance-engine/pkg/store"
	"github.com/nexops-one/compliance-engine/pkg/workflow"
)

const (
	approver2 = "user:approver-2"
	reviewer  = "user:reviewer"
)

// step applies one action and fails the test on an unexpected outcome.
func step(t *testing.T, a store.Assessment, r workflow.ActionRequest, wantErr error) (store.Assessment, store.Transition) {
	t.Helper()
	next, tr, err := workflow.Apply(workflow.Config{}, a, r)
	if !errors.Is(err, wantErr) {
		t.Fatalf("%s by %s: err = %v, want %v", r.Action, r.Actor, err, wantErr)
	}
	if err != nil {
		return a, tr
	}
	return next, tr
}

func approve(actor string, rq workflow.Requirements) workflow.ActionRequest {
	r := req(workflow.ActionApprove, actor)
	r.Requirements = rq
	return r
}

func TestQuorumApproval(t *testing.T) {
	two := workflow.Requirements{MinApprovers: 2}
	a, _ := step(t, base(store.StageNone), req(workflow.ActionSubmit, owner), nil)
	if a.SubmittedBy != owner || a.Stage != store.StageSubmitted {
		t.Fatalf("submitted = %+v", a)
	}
	a, tr := step(t, a, approve(approver, two), nil)
	if a.Stage != store.StageSubmitted || a.Approval != nil || len(a.Approvals) != 1 || a.RequiredApprovals != 2 {
		t.Fatalf("after one approval = %+v", a)
	}
	if tr.Action != "approve" || tr.ToStage != store.StageSubmitted || tr.Approvals != 1 || tr.RequiredApprovals != 2 ||
		tr.ToStatus != string(engine.StatusInReview) {
		t.Fatalf("transition = %+v", tr)
	}
	// The same approver cannot count twice.
	step(t, a, approve(approver, two), workflow.ErrAlreadyApproved)
	// The owner still cannot approve.
	step(t, a, approve(owner, two), workflow.ErrSelfApproval)

	// The effective status shows the quorum's progress.
	res := workflow.Effective(workflow.Input{Computed: engine.Result{Frameworks: []engine.FrameworkResult{{
		Catalog: catalog.Ref{Catalog: "dora", Version: "1.0.0"}, Controls: []engine.ControlResult{monitoring}}}},
		Assessments: []store.Assessment{a}, AsOf: t0})
	c := res.Frameworks[0].Controls[0]
	if c.Status != engine.StatusInReview || c.Attention != workflow.AttentionAwaitingApproval || !reflect.DeepEqual(c.Approvals, &workflow.Quorum{Given: 1, Required: 2}) {
		t.Fatalf("effective = %+v", c)
	}

	a, tr = step(t, a, approve(approver2, two), nil)
	if a.Stage != store.StageApproved || a.Approval == nil || a.Approval.Actor != approver2 ||
		!reflect.DeepEqual(a.Approval.Approvers, []string{approver, approver2}) || len(a.Approvals) != 0 || a.RequiredApprovals != 0 {
		t.Fatalf("after the quorum = %+v %+v", a, a.Approval)
	}
	if tr.ToStage != store.StageApproved || tr.Approvals != 2 || tr.RequiredApprovals != 2 {
		t.Fatalf("final transition = %+v", tr)
	}
	// A reopen and a new submission start a new quorum.
	r := req(workflow.ActionReopen, owner)
	a, _ = step(t, a, r, nil)
	if a.SubmittedBy != "" || a.Approval != nil {
		t.Fatalf("reopened = %+v", a)
	}
}

func TestRejectClearsPendingApprovals(t *testing.T) {
	two := workflow.Requirements{MinApprovers: 2}
	a, _ := step(t, base(store.StageNone), req(workflow.ActionSubmit, owner), nil)
	a, _ = step(t, a, approve(approver, two), nil)
	a, _ = step(t, a, req(workflow.ActionReject, approver2), nil)
	if a.Stage != store.StageRejected || len(a.Approvals) != 0 {
		t.Fatalf("rejected = %+v", a)
	}
	a, _ = step(t, a, req(workflow.ActionSubmit, owner), nil)
	// The earlier approver may approve again in the new round.
	a, _ = step(t, a, approve(approver, two), nil)
	if len(a.Approvals) != 1 {
		t.Fatalf("new round = %+v", a)
	}
}

func TestRecommendationRequired(t *testing.T) {
	rq := workflow.Requirements{RequireReviewerRecommendation: true}
	a, _ := step(t, base(store.StageNone), req(workflow.ActionSubmit, owner), nil)
	step(t, a, approve(approver, rq), workflow.ErrRecommendationRequired)
	a, _ = step(t, a, req(workflow.ActionRecommend, reviewer), nil)
	if !a.Recommended {
		t.Fatal("the recommendation is not recorded")
	}
	a, _ = step(t, a, approve(approver, rq), nil)
	if a.Stage != store.StageApproved || a.Approval.Approvers != nil {
		t.Fatalf("approved = %+v", a)
	}
	// A new submission needs a new recommendation.
	a, _ = step(t, a, req(workflow.ActionReopen, owner), nil)
	a, _ = step(t, a, req(workflow.ActionSubmit, owner), nil)
	if a.Recommended {
		t.Fatal("a resubmission must clear the recommendation")
	}
	step(t, a, approve(approver, rq), workflow.ErrRecommendationRequired)
}

func TestDistinctFromSubmitter(t *testing.T) {
	// An unassigned control: the submitter becomes owner, so the submitter
	// rule matters when self-approval is allowed by the deployment.
	a := store.Assessment{Scope: scope, Catalog: "dora", ControlID: "c1", Owner: owner, Stage: store.StageNone}
	a, _ = step(t, a, req(workflow.ActionSubmit, owner), nil)
	rq := workflow.Requirements{DistinctFromSubmitter: true}
	if _, _, err := workflow.Apply(workflow.Config{AllowSelfApproval: true}, a, approve(owner, rq)); !errors.Is(err, workflow.ErrSubmitterApproval) {
		t.Fatalf("submitter approving = %v", err)
	}
	if next, _, err := workflow.Apply(workflow.Config{AllowSelfApproval: true}, a, approve(owner, workflow.Requirements{})); err != nil || !next.Approval.SelfApproved {
		t.Fatalf("basic policy with self-approval allowed = %+v %v", next.Approval, err)
	}
}

func TestRequirementsApprovers(t *testing.T) {
	for in, want := range map[int]int{-1: 1, 0: 1, 1: 1, 3: 3, 9: workflow.MaxApprovers} {
		if got := (workflow.Requirements{MinApprovers: in}).Approvers(); got != want {
			t.Errorf("Approvers(%d) = %d, want %d", in, got, want)
		}
	}
}
