// SPDX-License-Identifier: Apache-2.0

// Package workflow holds the pure rules of the human review workflow: which
// actions are allowed from which stage, separation of duties, and the
// effective status that combines the engine's computed status with the
// assessment and its evidence. Nothing here performs I/O.
package workflow

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/nexops-one/compliance-engine/pkg/catalog"
	"github.com/nexops-one/compliance-engine/pkg/engine"
	"github.com/nexops-one/compliance-engine/pkg/store"
)

// Action is a workflow action.
type Action string

const (
	ActionAssign    Action = "assign"
	ActionSubmit    Action = "submit"
	ActionRecommend Action = "recommend"
	ActionReject    Action = "reject"
	ActionApprove   Action = "approve"
	ActionReopen    Action = "reopen"
	ActionVoid      Action = "void"   // system: an approval no longer holds
	ActionImport    Action = "import" // system: an operator carries over an assessment from another system
)

// Attention labels qualify an effective status.
const (
	AttentionRuleFailed       = engine.AttentionRuleFailed
	AttentionAwaitingApproval = "awaiting_approval"
	AttentionEvidenceInvalid  = "evidence_invalid"
	AttentionApprovalExpired  = "approval_expired"
)

// Void reasons.
const (
	VoidInputsIncomplete = "inputs_incomplete"
	VoidRuleFailed       = "rule_failed"
	VoidEvidenceInvalid  = "evidence_invalid"
)

var (
	ErrInvalidTransition = errors.New("action not allowed in the current state")
	ErrSelfApproval      = errors.New("the control owner cannot approve their own control")
	ErrEvidenceRequired  = errors.New("approval requires at least one active linked evidence item and no failed integrity check")
	ErrNotOwner          = errors.New("only the control owner can do this")
	ErrReasonRequired    = errors.New("a reason is required")
	// ErrRecommendationRequired refuses an approval the policy wants a
	// reviewer to recommend first.
	ErrRecommendationRequired = errors.New("approval requires a reviewer's recommendation first")
	// ErrSubmitterApproval refuses an approval by the person who submitted the
	// control when the policy requires them to differ.
	ErrSubmitterApproval = errors.New("the person who submitted the control cannot approve it")
	// ErrAlreadyApproved refuses a second approval by the same person towards a quorum.
	ErrAlreadyApproved = errors.New("this person has already approved; the quorum needs other approvers")
)

// Config holds deployment choices.
type Config struct {
	AllowSelfApproval bool
}

// MaxApprovers bounds a quorum.
const MaxApprovers = 5

// Requirements are what an approval needs, decided per control by the
// workflow policy. The zero value is the basic single-approver workflow.
type Requirements struct {
	// MinApprovers is the number of distinct approvers (default 1, at most MaxApprovers).
	MinApprovers int `json:"min_approvers"`
	// RequireReviewerRecommendation refuses approval until a reviewer or
	// approver has recommended the control since it was submitted.
	RequireReviewerRecommendation bool `json:"require_recommendation"`
	// DistinctFromSubmitter refuses approval by the person who submitted.
	DistinctFromSubmitter bool `json:"distinct_from_submitter"`
}

// Approvers returns the effective number of approvers required.
func (r Requirements) Approvers() int {
	switch {
	case r.MinApprovers < 1:
		return 1
	case r.MinApprovers > MaxApprovers:
		return MaxApprovers
	}
	return r.MinApprovers
}

// EvidenceSummary counts the evidence linked to a control by state.
type EvidenceSummary struct {
	Active     int // active with a checksum (verified or not)
	Failed     int // integrity_failed
	NoChecksum int // active but recorded without a checksum: never satisfies a requirement
}

// Summarize counts evidence linked to ref at t.
func Summarize(evs []store.Evidence, ref store.ControlRef, t time.Time) EvidenceSummary {
	var s EvidenceSummary
	for _, e := range evs {
		if !e.LinkedTo(ref) {
			continue
		}
		switch e.StateAt(t) {
		case store.EvidenceActive:
			if e.Integrity == store.IntegrityNoChecksum {
				s.NoChecksum++
				continue
			}
			s.Active++
		case store.EvidenceIntegrityFailed:
			s.Failed++
		}
	}
	return s
}

// HumanAssessable reports whether people may submit and approve a control:
// its rule is satisfied (monitoring), or it is a manual control whose only
// blocker is that it needs a human assessment.
func HumanAssessable(r engine.ControlResult) bool {
	if r.Status == engine.StatusMonitoring {
		return true
	}
	return r.Status == engine.StatusNotAssessed && len(r.Blockers) == 1 && r.Blockers[0].Reason == engine.ReasonManual
}

// RequiresEvidence reports whether approving c needs linked evidence: the
// catalog's explicit flag, otherwise when it lists evidence requirements or is
// a manual control (the approval is then the whole assessment).
func RequiresEvidence(c catalog.Control) bool {
	if c.ApprovalRequiresEvidence != nil {
		return *c.ApprovalRequiresEvidence
	}
	return len(c.EvidenceRequirements) > 0 || c.Rule.Kind == catalog.KindManual
}

// ReviewInterval returns the control's review interval.
func ReviewInterval(cat *catalog.Catalog, c catalog.Control) (Period, error) {
	s := c.ReviewInterval
	if s == "" && cat != nil {
		s = cat.Scoring.ReviewInterval
	}
	if s == "" {
		s = DefaultReviewInterval
	}
	return ParseDuration(s)
}

// Evaluated is the per-control effective outcome.
type Evaluated struct {
	Status     engine.Status
	Attention  string
	VoidReason string // set when the assessment holds an approval that no longer stands
}

// Evaluate combines a computed result with an assessment and its evidence at t.
func Evaluate(computed engine.ControlResult, a store.Assessment, requiresEvidence bool, ev EvidenceSummary, t time.Time) Evaluated {
	stage := stageOf(a)
	if !HumanAssessable(computed) {
		out := Evaluated{Status: computed.Status, Attention: computed.Attention}
		if stage == store.StageApproved {
			out.VoidReason = VoidInputsIncomplete
			if computed.Status == engine.StatusInReview {
				out.VoidReason = VoidRuleFailed
			}
		}
		return out
	}
	switch stage {
	case store.StageSubmitted:
		return Evaluated{Status: engine.StatusInReview, Attention: AttentionAwaitingApproval}
	case store.StageRejected:
		return Evaluated{Status: engine.StatusRejected}
	case store.StageApproved:
		if ev.Failed > 0 || (requiresEvidence && ev.Active == 0) {
			return Evaluated{Status: engine.StatusInReview, Attention: AttentionEvidenceInvalid, VoidReason: VoidEvidenceInvalid}
		}
		if a.Approval != nil && !t.Before(a.Approval.ExpiresAt) {
			return Evaluated{Status: engine.StatusExpired, Attention: AttentionApprovalExpired}
		}
		return Evaluated{Status: engine.StatusReady}
	}
	// No human decision yet: the computed status stands (a manual control stays not_assessed).
	return Evaluated{Status: computed.Status, Attention: computed.Attention}
}

func stageOf(a store.Assessment) store.Stage {
	if a.Stage == "" {
		return store.StageNone
	}
	return a.Stage
}

// ActionRequest is one workflow action with everything needed to decide it.
type ActionRequest struct {
	Action           Action
	Actor, ActorKind string
	Reason, Note     string
	// Assign: nil leaves a field unchanged; a pointer to "" or nil clears it.
	Owner, Reviewer, Notes *string
	DueAt                  **time.Time
	// Approve.
	EvaluationID, SnapshotID string
	Requirements             Requirements
	// Import: the approval to carry over (nil imports owner, notes and due
	// date only). Owner, Notes and DueAt apply as for assign.
	Import *store.Approval
	// Context supplied by the caller.
	Computed         engine.ControlResult
	RequiresEvidence bool
	Evidence         EvidenceSummary
	ReviewInterval   Period
	At               time.Time
}

// Apply validates an action and returns the new assessment and the
// transition to append to its history. a may be the zero value (no
// assessment yet); its Scope, Catalog and ControlID must be set by the caller.
func Apply(cfg Config, a store.Assessment, req ActionRequest) (store.Assessment, store.Transition, error) {
	before := Evaluate(req.Computed, a, req.RequiresEvidence, req.Evidence, req.At)
	stage := stageOf(a)
	next := a
	next.Stage = stage
	tr := store.Transition{
		Catalog: a.Catalog, ControlID: a.ControlID, At: req.At, Actor: req.Actor, ActorKind: req.ActorKind,
		Action: string(req.Action), FromStage: stage, Reason: strings.TrimSpace(req.Reason), Note: strings.TrimSpace(req.Note),
		FromStatus: string(before.Status),
	}
	invalid := func(why string) error {
		return fmt.Errorf("%w: %s from stage %s: %s", ErrInvalidTransition, req.Action, stage, why)
	}
	owner := func() error {
		if a.Owner != "" && a.Owner != req.Actor {
			return fmt.Errorf("%w (owner is %s)", ErrNotOwner, a.Owner)
		}
		return nil
	}
	switch req.Action {
	case ActionAssign:
		if req.Owner != nil {
			next.Owner = *req.Owner
		}
		if req.Reviewer != nil {
			next.Reviewer = *req.Reviewer
		}
		if req.Notes != nil {
			next.Notes = *req.Notes
		}
		if req.DueAt != nil {
			next.DueAt = *req.DueAt
		}
	case ActionSubmit:
		reReview := stage == store.StageApproved && before.Status == engine.StatusExpired
		if stage != store.StageNone && stage != store.StageRejected && !reReview {
			return a, tr, invalid("only new, rejected or expired controls can be submitted")
		}
		if !HumanAssessable(req.Computed) {
			return a, tr, invalid("the control is not assessable: its inputs are incomplete or its rule is not satisfied")
		}
		if err := owner(); err != nil {
			return a, tr, err
		}
		if next.Owner == "" {
			next.Owner = req.Actor
		}
		next.Stage, next.Approval = store.StageSubmitted, nil
		clearQuorum(&next)
		next.SubmittedBy = req.Actor
	case ActionRecommend:
		if stage != store.StageSubmitted {
			return a, tr, invalid("only submitted controls can be recommended")
		}
		next.Recommended = true
	case ActionReject:
		if stage != store.StageSubmitted {
			return a, tr, invalid("only submitted controls can be rejected")
		}
		if tr.Reason == "" {
			return a, tr, ErrReasonRequired
		}
		next.Stage = store.StageRejected
		next.Approvals, next.RequiredApprovals = nil, 0
	case ActionApprove:
		if stage != store.StageSubmitted {
			return a, tr, invalid("only submitted controls can be approved")
		}
		if !HumanAssessable(req.Computed) {
			return a, tr, invalid("the control is no longer assessable")
		}
		self := a.Owner == req.Actor
		if self && !cfg.AllowSelfApproval {
			return a, tr, ErrSelfApproval
		}
		if req.Evidence.Failed > 0 || (req.RequiresEvidence && req.Evidence.Active == 0) {
			return a, tr, ErrEvidenceRequired
		}
		rq := req.Requirements
		if rq.RequireReviewerRecommendation && !a.Recommended {
			return a, tr, ErrRecommendationRequired
		}
		if rq.DistinctFromSubmitter && a.SubmittedBy != "" && a.SubmittedBy == req.Actor {
			return a, tr, ErrSubmitterApproval
		}
		for _, p := range a.Approvals {
			if p.Actor == req.Actor {
				return a, tr, ErrAlreadyApproved
			}
		}
		need := rq.Approvers()
		tr.EvaluationID, tr.SelfApproved = req.EvaluationID, self
		approvers := []string{}
		for _, p := range a.Approvals {
			approvers = append(approvers, p.Actor)
		}
		approvers = append(approvers, req.Actor)
		if need > 1 {
			tr.Approvals, tr.RequiredApprovals = len(approvers), need
		}
		if len(approvers) < need {
			// Below the quorum: the approval is recorded, the stage stays submitted.
			next.Approvals = append(append([]store.PendingApproval{}, a.Approvals...),
				store.PendingApproval{Actor: req.Actor, At: req.At, EvaluationID: req.EvaluationID})
			next.RequiredApprovals = need
			break
		}
		next.Stage = store.StageApproved
		next.Approval = &store.Approval{Actor: req.Actor, At: req.At, EvaluationID: req.EvaluationID, SnapshotID: req.SnapshotID,
			ExpiresAt: req.ReviewInterval.AddTo(req.At), SelfApproved: self}
		if need > 1 {
			next.Approval.Approvers = approvers
		}
		next.Approvals, next.RequiredApprovals = nil, 0
	case ActionReopen:
		if stage != store.StageApproved {
			return a, tr, invalid("only approved controls can be reopened")
		}
		if err := owner(); err != nil {
			return a, tr, err
		}
		if tr.Reason == "" {
			return a, tr, ErrReasonRequired
		}
		next.Stage, next.Approval = store.StageNone, nil
		clearQuorum(&next)
	case ActionImport:
		if a.Version != 0 || stage != store.StageNone {
			return a, tr, invalid("an import never overwrites an existing assessment")
		}
		if req.ActorKind != "system" {
			return a, tr, invalid("only an operator import can carry over an assessment")
		}
		if tr.Reason == "" {
			return a, tr, ErrReasonRequired
		}
		if req.Owner != nil {
			next.Owner = *req.Owner
		}
		if req.Notes != nil {
			next.Notes = *req.Notes
		}
		if req.DueAt != nil {
			next.DueAt = *req.DueAt
		}
		if req.Import != nil {
			if !req.Import.ExpiresAt.After(req.Import.At) {
				return a, tr, invalid("the imported approval must expire after it was given")
			}
			ap := *req.Import
			ap.Imported, ap.SelfApproved = true, false
			next.Stage, next.Approval = store.StageApproved, &ap
		}
		// The status this engine computes is unknown at import time: the
		// transition records the stages only.
		next.UpdatedAt = req.At
		tr.FromStatus, tr.ToStage = "", next.Stage
		return next, tr, nil
	case ActionVoid:
		if stage != store.StageApproved {
			return a, tr, invalid("only approved controls can be voided")
		}
		if tr.Reason == "" {
			return a, tr, ErrReasonRequired
		}
		next.Stage, next.Approval = store.StageNone, nil
		clearQuorum(&next)
	default:
		return a, tr, fmt.Errorf("%w: unknown action %q", ErrInvalidTransition, req.Action)
	}
	next.UpdatedAt = req.At
	after := Evaluate(req.Computed, next, req.RequiresEvidence, req.Evidence, req.At)
	tr.ToStage, tr.ToStatus = next.Stage, string(after.Status)
	return next, tr, nil
}

// clearQuorum forgets the submission's recommendation and pending approvals.
func clearQuorum(a *store.Assessment) {
	a.SubmittedBy, a.Recommended, a.Approvals, a.RequiredApprovals = "", false, nil, 0
}

// OverrideKey is the settings key of a control's review override.
func OverrideKey(catalogName, controlID string) string { return catalogName + "/" + controlID }

// RequiresEvidenceWith applies a workspace override to RequiresEvidence.
func RequiresEvidenceWith(c catalog.Control, o store.ReviewOverride) bool {
	if o.ApprovalRequiresEvidence != nil {
		return *o.ApprovalRequiresEvidence
	}
	return RequiresEvidence(c)
}

// ReviewIntervalWith applies a workspace override to ReviewInterval.
func ReviewIntervalWith(cat *catalog.Catalog, c catalog.Control, o store.ReviewOverride) (Period, error) {
	if o.ReviewInterval != "" {
		return ParseDuration(o.ReviewInterval)
	}
	return ReviewInterval(cat, c)
}
