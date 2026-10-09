// SPDX-License-Identifier: Apache-2.0

package workflow_test

import (
	"encoding/json"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/nexops-one/compliance-engine/pkg/catalog"
	"github.com/nexops-one/compliance-engine/pkg/engine"
	"github.com/nexops-one/compliance-engine/pkg/store"
	"github.com/nexops-one/compliance-engine/pkg/workflow"
	"github.com/nexops-one/compliance-engine/sdk/adapter"
)

var (
	t0       = time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC)
	scope    = adapter.Scope{TenantID: "acme", WorkspaceID: "ws-1"}
	year, _  = workflow.ParseDuration("P1Y")
	owner    = "user:owner"
	approver = "user:approver"
)

var (
	monitoring  = engine.ControlResult{ControlID: "c1", Status: engine.StatusMonitoring}
	ruleFailed  = engine.ControlResult{ControlID: "c1", Status: engine.StatusInReview, Attention: engine.AttentionRuleFailed}
	incomplete  = engine.ControlResult{ControlID: "c1", Status: engine.StatusNotAssessed, Blockers: []engine.Blocker{{Reason: engine.ReasonMissing, Field: "f"}}}
	manual      = engine.ControlResult{ControlID: "c1", Status: engine.StatusNotAssessed, Blockers: []engine.Blocker{{Reason: engine.ReasonManual}}}
	oneActive   = workflow.EvidenceSummary{Active: 1}
	noEvidence  = workflow.EvidenceSummary{}
	failedCheck = workflow.EvidenceSummary{Active: 1, Failed: 1}
)

func base(stage store.Stage) store.Assessment {
	a := store.Assessment{Scope: scope, Catalog: "dora", ControlID: "c1", Owner: owner, Stage: stage}
	if stage == store.StageApproved {
		a.Approval = &store.Approval{Actor: approver, At: t0, ExpiresAt: t0.AddDate(1, 0, 0)}
	}
	return a
}

func req(action workflow.Action, actor string) workflow.ActionRequest {
	return workflow.ActionRequest{Action: action, Actor: actor, ActorKind: "user", Computed: monitoring, RequiresEvidence: true,
		Evidence: oneActive, ReviewInterval: year, At: t0, Reason: "because", EvaluationID: "eval-1", SnapshotID: "rev-1"}
}

func TestParseDuration(t *testing.T) {
	p, err := workflow.ParseDuration("P1Y2M3W4D")
	if err != nil || p != (workflow.Period{Years: 1, Months: 2, Weeks: 3, Days: 4}) {
		t.Fatalf("%+v %v", p, err)
	}
	if got := p.AddTo(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)); !got.Equal(time.Date(2027, 3, 26, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("AddTo = %v", got)
	}
	for _, bad := range []string{"", "P", "P0D", "PT1H", "1Y", "P1D1Y", "P-1D"} {
		if _, err := workflow.ParseDuration(bad); err == nil {
			t.Errorf("%q must be refused", bad)
		}
	}
}

func TestApplyTransitions(t *testing.T) {
	type tc struct {
		stage  store.Stage
		req    workflow.ActionRequest
		want   store.Stage
		err    error
		mutate func(*store.Assessment)
	}
	approve := req(workflow.ActionApprove, approver)
	cases := map[string]tc{
		"submit new":    {stage: store.StageNone, req: req(workflow.ActionSubmit, owner), want: store.StageSubmitted},
		"submit manual": {stage: store.StageNone, req: func() workflow.ActionRequest { r := req(workflow.ActionSubmit, owner); r.Computed = manual; return r }(), want: store.StageSubmitted},
		"submit incomplete": {stage: store.StageNone, req: func() workflow.ActionRequest {
			r := req(workflow.ActionSubmit, owner)
			r.Computed = incomplete
			return r
		}(), err: workflow.ErrInvalidTransition},
		"submit rule failed": {stage: store.StageNone, req: func() workflow.ActionRequest {
			r := req(workflow.ActionSubmit, owner)
			r.Computed = ruleFailed
			return r
		}(), err: workflow.ErrInvalidTransition},
		"submit by non-owner":   {stage: store.StageNone, req: req(workflow.ActionSubmit, "user:other"), err: workflow.ErrNotOwner},
		"submit twice":          {stage: store.StageSubmitted, req: req(workflow.ActionSubmit, owner), err: workflow.ErrInvalidTransition},
		"resubmit rejected":     {stage: store.StageRejected, req: req(workflow.ActionSubmit, owner), want: store.StageSubmitted},
		"submit while approved": {stage: store.StageApproved, req: req(workflow.ActionSubmit, owner), err: workflow.ErrInvalidTransition},
		"re-review expired": {stage: store.StageApproved, req: func() workflow.ActionRequest {
			r := req(workflow.ActionSubmit, owner)
			r.At = t0.AddDate(2, 0, 0)
			return r
		}(), want: store.StageSubmitted},
		"recommend submitted":       {stage: store.StageSubmitted, req: req(workflow.ActionRecommend, approver), want: store.StageSubmitted},
		"recommend new":             {stage: store.StageNone, req: req(workflow.ActionRecommend, approver), err: workflow.ErrInvalidTransition},
		"reject submitted":          {stage: store.StageSubmitted, req: req(workflow.ActionReject, approver), want: store.StageRejected},
		"reject without reason":     {stage: store.StageSubmitted, req: func() workflow.ActionRequest { r := req(workflow.ActionReject, approver); r.Reason = " "; return r }(), err: workflow.ErrReasonRequired},
		"reject approved":           {stage: store.StageApproved, req: req(workflow.ActionReject, approver), err: workflow.ErrInvalidTransition},
		"approve submitted":         {stage: store.StageSubmitted, req: approve, want: store.StageApproved},
		"approve new":               {stage: store.StageNone, req: approve, err: workflow.ErrInvalidTransition},
		"approve rejected":          {stage: store.StageRejected, req: approve, err: workflow.ErrInvalidTransition},
		"approve without evidence":  {stage: store.StageSubmitted, req: func() workflow.ActionRequest { r := approve; r.Evidence = noEvidence; return r }(), err: workflow.ErrEvidenceRequired},
		"approve with failed check": {stage: store.StageSubmitted, req: func() workflow.ActionRequest { r := approve; r.Evidence = failedCheck; return r }(), err: workflow.ErrEvidenceRequired},
		"approve, evidence optional": {stage: store.StageSubmitted, req: func() workflow.ActionRequest {
			r := approve
			r.Evidence, r.RequiresEvidence = noEvidence, false
			return r
		}(), want: store.StageApproved},
		"approve after data broke": {stage: store.StageSubmitted, req: func() workflow.ActionRequest { r := approve; r.Computed = incomplete; return r }(), err: workflow.ErrInvalidTransition},
		"reopen approved":          {stage: store.StageApproved, req: req(workflow.ActionReopen, owner), want: store.StageNone},
		"reopen by non-owner":      {stage: store.StageApproved, req: req(workflow.ActionReopen, approver), err: workflow.ErrNotOwner},
		"reopen submitted":         {stage: store.StageSubmitted, req: req(workflow.ActionReopen, owner), err: workflow.ErrInvalidTransition},
		"void approved":            {stage: store.StageApproved, req: req(workflow.ActionVoid, "system"), want: store.StageNone},
		"void new":                 {stage: store.StageNone, req: req(workflow.ActionVoid, "system"), err: workflow.ErrInvalidTransition},
		"assign keeps the stage":   {stage: store.StageSubmitted, req: req(workflow.ActionAssign, "user:admin"), want: store.StageSubmitted},
		"unknown action":           {stage: store.StageNone, req: req("dance", owner), err: workflow.ErrInvalidTransition},
	}
	for name, c := range cases {
		a := base(c.stage)
		next, tr, err := workflow.Apply(workflow.Config{}, a, c.req)
		if c.err != nil {
			if !errors.Is(err, c.err) {
				t.Errorf("%s: err = %v, want %v", name, err, c.err)
			}
			if !reflect.DeepEqual(next, a) {
				t.Errorf("%s: a refused action must not change the assessment", name)
			}
			continue
		}
		if err != nil || next.Stage != c.want || tr.FromStage != c.stage || tr.ToStage != c.want || tr.Action != string(c.req.Action) || tr.Actor != c.req.Actor {
			t.Errorf("%s: %+v %+v %v", name, next, tr, err)
		}
	}
}

func TestApplyDetails(t *testing.T) {
	// The first submitter becomes owner when none is assigned.
	unowned := base(store.StageNone)
	unowned.Owner = ""
	next, _, err := workflow.Apply(workflow.Config{}, unowned, req(workflow.ActionSubmit, "user:first"))
	if err != nil || next.Owner != "user:first" {
		t.Fatalf("first submitter = %+v %v", next, err)
	}
	// Approval records its evaluation and expiry.
	next, tr, err := workflow.Apply(workflow.Config{}, base(store.StageSubmitted), req(workflow.ActionApprove, approver))
	if err != nil || next.Approval == nil || next.Approval.EvaluationID != "eval-1" || !next.Approval.ExpiresAt.Equal(t0.AddDate(1, 0, 0)) ||
		tr.EvaluationID != "eval-1" || tr.FromStatus != "in_review" || tr.ToStatus != "ready" {
		t.Fatalf("approve = %+v %+v %v", next.Approval, tr, err)
	}
	// Assign changes only what it is given.
	owner2, due := "user:o2", t0.AddDate(0, 1, 0)
	duep := &due
	r := req(workflow.ActionAssign, "user:admin")
	r.Owner, r.DueAt = &owner2, &duep
	next, _, err = workflow.Apply(workflow.Config{}, base(store.StageNone), r)
	if err != nil || next.Owner != owner2 || next.DueAt == nil || !next.DueAt.Equal(due) || next.Reviewer != "" {
		t.Fatalf("assign = %+v %v", next, err)
	}
}

func TestApproveSeparationOfDuties(t *testing.T) {
	a := base(store.StageSubmitted)
	if _, _, err := workflow.Apply(workflow.Config{}, a, req(workflow.ActionApprove, owner)); !errors.Is(err, workflow.ErrSelfApproval) {
		t.Fatalf("self approval = %v", err)
	}
	next, tr, err := workflow.Apply(workflow.Config{AllowSelfApproval: true}, a, req(workflow.ActionApprove, owner))
	if err != nil || !next.Approval.SelfApproved || !tr.SelfApproved {
		t.Fatalf("allowed self approval must be flagged: %+v %+v %v", next.Approval, tr, err)
	}
	next, tr, _ = workflow.Apply(workflow.Config{AllowSelfApproval: true}, a, req(workflow.ActionApprove, approver))
	if next.Approval.SelfApproved || tr.SelfApproved {
		t.Fatal("an approval by someone else is not a self approval")
	}
}

func TestEffectiveStatusTable(t *testing.T) {
	approved := base(store.StageApproved)
	cases := []struct {
		name      string
		computed  engine.ControlResult
		a         store.Assessment
		ev        workflow.EvidenceSummary
		at        time.Time
		status    engine.Status
		attention string
		void      string
	}{
		{"incomplete, new", incomplete, base(store.StageNone), oneActive, t0, engine.StatusNotAssessed, "", ""},
		{"incomplete voids approval", incomplete, approved, oneActive, t0, engine.StatusNotAssessed, "", workflow.VoidInputsIncomplete},
		{"rule failed", ruleFailed, base(store.StageNone), oneActive, t0, engine.StatusInReview, workflow.AttentionRuleFailed, ""},
		{"rule failed voids approval", ruleFailed, approved, oneActive, t0, engine.StatusInReview, workflow.AttentionRuleFailed, workflow.VoidRuleFailed},
		{"monitoring, new", monitoring, base(store.StageNone), noEvidence, t0, engine.StatusMonitoring, "", ""},
		{"no assessment at all", monitoring, store.Assessment{}, noEvidence, t0, engine.StatusMonitoring, "", ""},
		{"submitted", monitoring, base(store.StageSubmitted), oneActive, t0, engine.StatusInReview, workflow.AttentionAwaitingApproval, ""},
		{"rejected", monitoring, base(store.StageRejected), oneActive, t0, engine.StatusRejected, "", ""},
		{"approved", monitoring, approved, oneActive, t0, engine.StatusReady, "", ""},
		{"approved, evidence gone", monitoring, approved, noEvidence, t0, engine.StatusInReview, workflow.AttentionEvidenceInvalid, workflow.VoidEvidenceInvalid},
		{"approved, integrity failed", monitoring, approved, failedCheck, t0, engine.StatusInReview, workflow.AttentionEvidenceInvalid, workflow.VoidEvidenceInvalid},
		{"approved, expired", monitoring, approved, oneActive, t0.AddDate(1, 0, 0), engine.StatusExpired, workflow.AttentionApprovalExpired, ""},
		{"manual, new", manual, base(store.StageNone), noEvidence, t0, engine.StatusNotAssessed, "", ""},
		{"manual, submitted", manual, base(store.StageSubmitted), oneActive, t0, engine.StatusInReview, workflow.AttentionAwaitingApproval, ""},
		{"manual, approved", manual, approved, oneActive, t0, engine.StatusReady, "", ""},
	}
	for _, c := range cases {
		got := workflow.Evaluate(c.computed, c.a, true, c.ev, c.at)
		if got.Status != c.status || got.Attention != c.attention || got.VoidReason != c.void {
			t.Errorf("%s: %+v, want %s/%s/%s", c.name, got, c.status, c.attention, c.void)
		}
	}
}

func sampleInput(t *testing.T) workflow.Input {
	t.Helper()
	cat := &catalog.Catalog{Catalog: "dora", Version: "1.0.0", Framework: "DORA",
		Scoring: catalog.Scoring{CountsAsReady: []string{"ready", "monitoring"}, Assumptions: "assumptions"},
		Controls: []catalog.Control{
			{ID: "c1", EvidenceRequirements: []string{"x"}, Rule: catalog.Rule{Kind: catalog.KindFieldsComplete}},
			{ID: "c2", Rule: catalog.Rule{Kind: catalog.KindManual}},
			{ID: "c3", Rule: catalog.Rule{Kind: catalog.KindFieldsComplete}},
			{ID: "c4", Rule: catalog.Rule{Kind: catalog.KindFieldsComplete}},
		}}
	c2 := manual
	c2.ControlID = "c2"
	c3 := incomplete
	c3.ControlID = "c3"
	c4 := monitoring
	c4.ControlID = "c4"
	computed := engine.Result{SnapshotID: "rev-3", Frameworks: []engine.FrameworkResult{{
		Catalog: cat.Ref(), Framework: "DORA", Tally: engine.Tally{Assumptions: "assumptions"},
		Controls: []engine.ControlResult{monitoring, c2, c3, c4},
	}}}
	due := t0.Add(-time.Hour)
	later := t0.AddDate(0, 6, 0)
	evs := []store.Evidence{
		{ID: "ev-1", Scope: scope, URI: "https://dms.example/doc", Checksum: "sha256:ab", Integrity: store.IntegrityVerified,
			Links: []store.ControlRef{{Catalog: "dora", ControlID: "c1"}, {Catalog: "dora", ControlID: "c2"}}, ValidUntil: &later},
		{ID: "ev-2", Scope: scope, Integrity: store.IntegrityUnverified, Links: []store.ControlRef{{Catalog: "gdpr", ControlID: "x"}}},
	}
	as := []store.Assessment{
		func() store.Assessment { a := base(store.StageApproved); return a }(),
		func() store.Assessment { a := base(store.StageSubmitted); a.ControlID = "c2"; a.DueAt = &due; return a }(),
	}
	return workflow.Input{Computed: computed, Catalogs: []*catalog.Catalog{cat}, Assessments: as, Evidence: evs, AsOf: t0}
}

func TestEffective(t *testing.T) {
	res := workflow.Effective(sampleInput(t))
	fw := res.Frameworks[0]
	want := map[string]engine.Status{"c1": engine.StatusReady, "c2": engine.StatusInReview, "c3": engine.StatusNotAssessed, "c4": engine.StatusMonitoring}
	for _, c := range fw.Controls {
		if c.Status != want[c.ControlID] {
			t.Errorf("%s = %s, want %s", c.ControlID, c.Status, want[c.ControlID])
		}
	}
	c1, c2 := fw.Controls[0], fw.Controls[1]
	if c1.ComputedStatus != engine.StatusMonitoring || !c1.RequiresEvidence || len(c1.Evidence) != 1 || c1.Evidence[0].State != store.EvidenceActive {
		t.Fatalf("c1 = %+v", c1)
	}
	if !c2.Overdue || c2.Attention != workflow.AttentionAwaitingApproval || !c2.RequiresEvidence {
		t.Fatalf("c2 = %+v", c2)
	}
	if fw.Tally.Ready != 1 || fw.Tally.Monitoring != 1 || fw.Tally.InReview != 1 || fw.Tally.Assessable != 3 || fw.Tally.ScorePct != 66 || fw.Tally.CoveragePct != 75 || fw.Tally.Assumptions != "assumptions" {
		t.Fatalf("tally = %+v", fw.Tally)
	}
	if res.Overall.InScope != 4 || res.Overall.ScorePct != 66 {
		t.Fatalf("overall = %+v", res.Overall)
	}
	if len(res.Inputs.Evidence) != 1 || res.Inputs.Evidence[0].URI != "" || res.Inputs.Evidence[0].Checksum != "" || len(res.Inputs.Assessments) != 2 {
		t.Fatalf("inputs must hold used evidence without locations: %+v", res.Inputs)
	}
}

func TestEffectiveIsReproducible(t *testing.T) {
	in := sampleInput(t)
	res := workflow.Effective(in)
	data, err := json.Marshal(res)
	if err != nil {
		t.Fatal(err)
	}
	var stored workflow.Result
	if err := json.Unmarshal(data, &stored); err != nil {
		t.Fatal(err)
	}
	again := workflow.Recompute(in.Computed, in.Catalogs, stored)
	a, _ := json.Marshal(again)
	if string(a) != string(data) {
		t.Fatalf("recomputed result differs:\n%s\n%s", a, data)
	}
}

func TestSummarizeNoChecksum(t *testing.T) {
	ref := store.ControlRef{Catalog: "dora", ControlID: "c1"}
	evs := []store.Evidence{
		{ID: "e1", Integrity: store.IntegrityNoChecksum, Links: []store.ControlRef{ref}},
		{ID: "e2", Integrity: store.IntegrityUnverified, Links: []store.ControlRef{ref}},
		{ID: "e3", Integrity: store.IntegrityNoChecksum, Links: []store.ControlRef{{Catalog: "dora", ControlID: "other"}}},
	}
	if got := workflow.Summarize(evs, ref, t0); got != (workflow.EvidenceSummary{Active: 1, NoChecksum: 1}) {
		t.Fatalf("summary = %+v", got)
	}
	if got := workflow.Summarize(evs[:1], ref, t0); got.Active != 0 {
		t.Fatalf("evidence without a checksum must not count as active: %+v", got)
	}
}

func TestNoChecksumNeverSatisfiesApproval(t *testing.T) {
	onlyNoChecksum := workflow.EvidenceSummary{NoChecksum: 2}
	r := req(workflow.ActionApprove, approver)
	r.Evidence = onlyNoChecksum
	if _, _, err := workflow.Apply(workflow.Config{}, base(store.StageSubmitted), r); !errors.Is(err, workflow.ErrEvidenceRequired) {
		t.Fatalf("approve with only checksum-less evidence = %v", err)
	}
	r.RequiresEvidence = false
	if _, _, err := workflow.Apply(workflow.Config{}, base(store.StageSubmitted), r); err != nil {
		t.Fatalf("approve without an evidence requirement = %v", err)
	}
	got := workflow.Evaluate(monitoring, base(store.StageApproved), true, onlyNoChecksum, t0)
	if got.Status != engine.StatusInReview || got.Attention != workflow.AttentionEvidenceInvalid || got.VoidReason != workflow.VoidEvidenceInvalid {
		t.Fatalf("approved with only checksum-less evidence = %+v", got)
	}
}

func importReq(approval *store.Approval) workflow.ActionRequest {
	ownerActor, notes := "user:m8owner", "carried over"
	due := ptr(t0.AddDate(0, 3, 0))
	return workflow.ActionRequest{Action: workflow.ActionImport, Actor: "migration:m8", ActorKind: "system", At: t0,
		Reason: "migrated from M8 manual status", Note: "M8 status: ready", Owner: &ownerActor, Notes: &notes, DueAt: &due, Import: approval}
}

func ptr(t time.Time) *time.Time { return &t }

func TestImport(t *testing.T) {
	fresh := store.Assessment{Scope: scope, Catalog: "dora", ControlID: "c1"}
	approval := &store.Approval{Actor: "migration:m8", At: t0.AddDate(0, -1, 0), ExpiresAt: t0.AddDate(0, 6, 0)}
	next, tr, err := workflow.Apply(workflow.Config{}, fresh, importReq(approval))
	if err != nil {
		t.Fatal(err)
	}
	if next.Stage != store.StageApproved || next.Owner != "user:m8owner" || next.Notes != "carried over" || next.DueAt == nil ||
		next.Approval == nil || !next.Approval.Imported || !next.Approval.ExpiresAt.Equal(approval.ExpiresAt) || next.Approval.Actor != "migration:m8" {
		t.Fatalf("imported assessment = %+v (approval %+v)", next, next.Approval)
	}
	if tr.Action != "import" || tr.FromStage != store.StageNone || tr.ToStage != store.StageApproved || tr.Reason == "" || tr.Note != "M8 status: ready" ||
		tr.ActorKind != "system" || tr.FromStatus != "" || tr.ToStatus != "" {
		t.Fatalf("transition = %+v", tr)
	}
	if approval.Imported {
		t.Fatal("the caller's approval must not be modified")
	}

	plain, tr, err := workflow.Apply(workflow.Config{}, fresh, importReq(nil))
	if err != nil || plain.Stage != store.StageNone || plain.Approval != nil || tr.ToStage != store.StageNone {
		t.Fatalf("import without approval = %+v, %+v, %v", plain, tr, err)
	}

	for name, c := range map[string]struct {
		a   store.Assessment
		req workflow.ActionRequest
	}{
		"over an existing assessment":           {a: func() store.Assessment { a := base(store.StageNone); a.Version = 1; return a }(), req: importReq(nil)},
		"by a user":                             {a: fresh, req: func() workflow.ActionRequest { r := importReq(nil); r.ActorKind = "user"; return r }()},
		"without a reason":                      {a: fresh, req: func() workflow.ActionRequest { r := importReq(nil); r.Reason = " "; return r }()},
		"approval expiring before it was given": {a: fresh, req: importReq(&store.Approval{Actor: "migration:m8", At: t0, ExpiresAt: t0.AddDate(0, 0, -1)})},
	} {
		if _, _, err := workflow.Apply(workflow.Config{}, c.a, c.req); err == nil {
			t.Errorf("import %s must be refused", name)
		}
	}
}

func TestImportedApprovalFollowsTheEffectiveRules(t *testing.T) {
	fresh := store.Assessment{Scope: scope, Catalog: "dora", ControlID: "c1"}
	imported, _, err := workflow.Apply(workflow.Config{}, fresh, importReq(&store.Approval{Actor: "migration:m8", At: t0.AddDate(-1, 0, 0), ExpiresAt: t0.AddDate(0, 1, 0)}))
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name     string
		computed engine.ControlResult
		ev       workflow.EvidenceSummary
		at       time.Time
		want     engine.Status
		void     string
	}{
		{"holds", monitoring, oneActive, t0, engine.StatusReady, ""},
		{"voided when not assessed", incomplete, oneActive, t0, engine.StatusNotAssessed, workflow.VoidInputsIncomplete},
		{"voided when the rule fails", ruleFailed, oneActive, t0, engine.StatusInReview, workflow.VoidRuleFailed},
		{"downgraded without checksummed evidence", monitoring, workflow.EvidenceSummary{NoChecksum: 1}, t0, engine.StatusInReview, workflow.VoidEvidenceInvalid},
		{"expires", monitoring, oneActive, t0.AddDate(0, 2, 0), engine.StatusExpired, ""},
	}
	for _, c := range cases {
		got := workflow.Evaluate(c.computed, imported, true, c.ev, c.at)
		if got.Status != c.want || got.VoidReason != c.void {
			t.Errorf("%s: %+v", c.name, got)
		}
	}
}
