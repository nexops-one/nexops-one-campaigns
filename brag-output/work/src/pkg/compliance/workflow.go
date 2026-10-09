// SPDX-License-Identifier: Apache-2.0

package compliance

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/nexops-one/compliance-engine/pkg/access"
	"github.com/nexops-one/compliance-engine/pkg/canonical"
	"github.com/nexops-one/compliance-engine/pkg/catalog"
	"github.com/nexops-one/compliance-engine/pkg/engine"
	"github.com/nexops-one/compliance-engine/pkg/extension"
	"github.com/nexops-one/compliance-engine/pkg/store"
	"github.com/nexops-one/compliance-engine/pkg/workflow"
)

var (
	// ErrStaleEvaluation means the evaluation an approver looked at no longer
	// matches the current snapshot.
	ErrStaleEvaluation = errors.New("the evaluation is stale: re-evaluate and review the current result before approving")
	// ErrInvalidAssignee means the user is not a member with a suitable role.
	ErrInvalidAssignee = errors.New("invalid assignee")
)

// computeCurrent evaluates the current snapshot against catalogs without storing anything.
func (e *Engine) compute(ctx context.Context, scope Scope, snapshotID string, cats []*catalog.Catalog) (engine.Result, error) {
	snap, err := e.Snapshot(ctx, scope, snapshotID)
	if err != nil {
		return engine.Result{}, err
	}
	sup, err := e.supply(ctx, scope)
	if err != nil {
		return engine.Result{}, err
	}
	ix := e.index(snap.Records)
	return engine.Evaluate(engine.Input{SnapshotID: snap.ID, Index: ix, References: canonical.CheckReferences(ix), Supplied: sup, Catalogs: cats}), nil
}

// effective combines a computed result with the workspace's assessments and evidence at asOf.
func (e *Engine) effective(ctx context.Context, scope Scope, computed engine.Result, cats []*catalog.Catalog, asOf time.Time) (workflow.Result, error) {
	as, err := e.store.Assessments(ctx, scope, "")
	if err != nil {
		return workflow.Result{}, err
	}
	evs, err := e.store.ListEvidence(ctx, scope, store.EvidenceQuery{})
	if err != nil {
		return workflow.Result{}, err
	}
	set, err := e.store.Settings(ctx, scope)
	if err != nil {
		return workflow.Result{}, err
	}
	return workflow.Effective(workflow.Input{Computed: computed, Catalogs: cats, Assessments: as, Evidence: evs, AsOf: asOf, Overrides: set.ReviewOverrides}), nil
}

// Status is the live effective view of the current snapshot against catalog
// versions (none = latest). It stores nothing.
func (e *Engine) Status(ctx context.Context, scope Scope, refs []catalog.Ref) (workflow.Result, error) {
	if err := scope.Validate(); err != nil {
		return workflow.Result{}, err
	}
	cats, err := e.resolveCatalogs(ctx, scope, refs)
	if err != nil {
		return workflow.Result{}, err
	}
	computed, err := e.compute(ctx, scope, "", cats)
	if err != nil {
		return workflow.Result{}, err
	}
	return e.effective(ctx, scope, computed, cats, e.now())
}

// persistVoids records, as system transitions, approvals that no longer hold.
func (e *Engine) persistVoids(ctx context.Context, scope Scope, res workflow.Result) error {
	sys := extension.WithPrincipal(ctx, extension.Principal{Scope: scope, Actor: "system", Kind: extension.ActorSystem})
	for _, fw := range res.Frameworks {
		for _, c := range fw.Controls {
			if c.VoidReason == "" || c.Stage != store.StageApproved {
				continue
			}
			a, err := e.store.Assessment(ctx, scope, fw.Catalog.Catalog, c.ControlID)
			if err != nil {
				return err
			}
			next, tr, err := workflow.Apply(workflow.Config{}, a, workflow.ActionRequest{
				Action: workflow.ActionVoid, Actor: "system", ActorKind: string(extension.ActorSystem), Reason: c.VoidReason,
				At: res.AsOf, Computed: engine.ControlResult{ControlID: c.ControlID, Status: c.ComputedStatus},
			})
			if err != nil {
				return err
			}
			tr.FromStatus, tr.ToStatus = string(c.Status), string(c.ComputedStatus)
			ev := e.auditEvent(sys, scope, "assessment.void", "control", fw.Catalog.Catalog+"/"+c.ControlID, map[string]any{"reason": c.VoidReason})
			if _, err := e.store.SaveAssessment(ctx, next, a.Version, &tr, ev); err != nil {
				return err
			}
		}
	}
	return nil
}

// controlOf finds the latest version of a catalog holding the control among
// those the workspace is entitled to.
func (e *Engine) controlOf(ctx context.Context, scope Scope, catalogName, controlID string) (*catalog.Catalog, catalog.Control, error) {
	for _, c := range e.latestFor(ctx, scope) {
		if c.Catalog != catalogName {
			continue
		}
		for _, ctl := range c.Controls {
			if ctl.ID == controlID {
				return c, ctl, nil
			}
		}
	}
	return nil, catalog.Control{}, fmt.Errorf("%w: %s/%s", ErrUnknownControl, catalogName, controlID)
}

// AssessmentView is an assessment with its history. Exists is false when no
// one has acted on the control yet.
type AssessmentView struct {
	Assessment store.Assessment   `json:"assessment"`
	History    []store.Transition `json:"history"`
	Exists     bool               `json:"exists"`
}

func (e *Engine) loadAssessment(ctx context.Context, scope Scope, catalogName, controlID string) (store.Assessment, bool, error) {
	a, err := e.store.Assessment(ctx, scope, catalogName, controlID)
	if errors.Is(err, store.ErrNotFound) {
		return store.Assessment{Scope: scope, Catalog: catalogName, ControlID: controlID, Stage: store.StageNone}, false, nil
	}
	return a, err == nil, err
}

// Assessment returns a control's assessment and history.
func (e *Engine) Assessment(ctx context.Context, scope Scope, catalogName, controlID string) (AssessmentView, error) {
	if err := scope.Validate(); err != nil {
		return AssessmentView{}, err
	}
	if _, _, err := e.controlOf(ctx, scope, catalogName, controlID); err != nil {
		return AssessmentView{}, err
	}
	a, exists, err := e.loadAssessment(ctx, scope, catalogName, controlID)
	if err != nil {
		return AssessmentView{}, err
	}
	h, err := e.store.History(ctx, scope, catalogName, controlID)
	if err != nil {
		return AssessmentView{}, err
	}
	return AssessmentView{Assessment: a, History: h, Exists: exists}, nil
}

// Assessments lists the workspace's assessments ("" = every catalog).
func (e *Engine) Assessments(ctx context.Context, scope Scope, catalogName string) ([]store.Assessment, error) {
	if err := scope.Validate(); err != nil {
		return nil, err
	}
	return e.store.Assessments(ctx, scope, catalogName)
}

// AssignInput changes an assessment's people, due date or notes. Nil fields
// are left unchanged; an empty string clears the field.
type AssignInput struct {
	Owner    *string `json:"owner,omitempty"`    // member email with the owner role
	Reviewer *string `json:"reviewer,omitempty"` // member email with the reviewer or approver role
	DueAt    *string `json:"due_at,omitempty"`   // RFC 3339
	Notes    *string `json:"notes,omitempty"`
}

// memberActor resolves a member email to the actor string of a user holding one of roles.
func (e *Engine) memberActor(ctx context.Context, scope Scope, email string, roles ...access.Role) (string, error) {
	u, err := e.store.UserByEmail(ctx, scope.TenantID, store.NormalizeEmail(email))
	if err != nil {
		return "", fmt.Errorf("%w: %s is not a user of this tenant", ErrInvalidAssignee, email)
	}
	m, err := e.store.Member(ctx, scope, u.ID)
	if err != nil {
		return "", fmt.Errorf("%w: %s is not a member of this workspace", ErrInvalidAssignee, email)
	}
	for _, r := range roles {
		if access.Contains(m.Roles, r) {
			return "user:" + u.ID, nil
		}
	}
	return "", fmt.Errorf("%w: %s needs one of the roles %s", ErrInvalidAssignee, email, access.Join(roles, ", "))
}

// ActInput carries the free-text and approval parts of an action.
type ActInput struct {
	Reason       string `json:"reason,omitempty"`
	Note         string `json:"note,omitempty"`
	EvaluationID string `json:"evaluation_id,omitempty"` // approve: the evaluation the approver reviewed
}

// Assign sets a control's owner, reviewer, due date or notes.
func (e *Engine) Assign(ctx context.Context, scope Scope, catalogName, controlID string, in AssignInput) (store.Assessment, error) {
	req := workflow.ActionRequest{Action: workflow.ActionAssign, Notes: in.Notes}
	if in.Owner != nil {
		owner := ""
		if *in.Owner != "" {
			var err error
			if owner, err = e.memberActor(ctx, scope, *in.Owner, access.RoleOwner); err != nil {
				return store.Assessment{}, err
			}
		}
		req.Owner = &owner
	}
	if in.Reviewer != nil {
		reviewer := ""
		if *in.Reviewer != "" {
			var err error
			if reviewer, err = e.memberActor(ctx, scope, *in.Reviewer, access.RoleReviewer, access.RoleApprover); err != nil {
				return store.Assessment{}, err
			}
		}
		req.Reviewer = &reviewer
	}
	if in.DueAt != nil {
		var due *time.Time
		if *in.DueAt != "" {
			t, err := time.Parse(time.RFC3339, *in.DueAt)
			if err != nil {
				return store.Assessment{}, fmt.Errorf("%w: due_at must be an RFC 3339 time", ErrInvalidAssignee)
			}
			t = t.UTC()
			due = &t
		}
		req.DueAt = &due
	}
	return e.act(ctx, scope, catalogName, controlID, req)
}

// Act performs a workflow action (submit, recommend, reject, approve, reopen).
func (e *Engine) Act(ctx context.Context, scope Scope, catalogName, controlID string, action workflow.Action, in ActInput) (store.Assessment, error) {
	switch action {
	case workflow.ActionSubmit, workflow.ActionRecommend, workflow.ActionReject, workflow.ActionApprove, workflow.ActionReopen:
	default:
		return store.Assessment{}, fmt.Errorf("%w: %q is not a user action", workflow.ErrInvalidTransition, action)
	}
	return e.act(ctx, scope, catalogName, controlID, workflow.ActionRequest{Action: action, Reason: in.Reason, Note: in.Note, EvaluationID: in.EvaluationID})
}

func (e *Engine) act(ctx context.Context, scope Scope, catalogName, controlID string, req workflow.ActionRequest) (store.Assessment, error) {
	if err := scope.Validate(); err != nil {
		return store.Assessment{}, err
	}
	if _, err := e.require(ctx, scope, extension.FeatureWorkflowBasic); err != nil {
		return store.Assessment{}, err
	}
	cat, ctl, err := e.controlOf(ctx, scope, catalogName, controlID)
	if err != nil {
		return store.Assessment{}, err
	}
	if req.Action == workflow.ActionApprove {
		if cat, err = e.approvalCatalog(ctx, scope, catalogName, req.EvaluationID); err != nil {
			return store.Assessment{}, err
		}
	}
	computed, err := e.compute(ctx, scope, "", []*catalog.Catalog{cat})
	if err != nil {
		return store.Assessment{}, err
	}
	var cr engine.ControlResult
	for _, c := range computed.Frameworks[0].Controls {
		if c.ControlID == controlID {
			cr = c
		}
	}
	if req.Action == workflow.ActionApprove {
		if err := e.checkFresh(ctx, scope, req.EvaluationID, computed.SnapshotID, catalogName, cr); err != nil {
			return store.Assessment{}, err
		}
		req.SnapshotID = computed.SnapshotID
		req.Requirements = e.flow.Requirements(ctx, scope, catalogName, controlID)
	}
	set, err := e.store.Settings(ctx, scope)
	if err != nil {
		return store.Assessment{}, err
	}
	override := set.ReviewOverrides[workflow.OverrideKey(catalogName, controlID)]
	interval, err := workflow.ReviewIntervalWith(cat, ctl, override)
	if err != nil {
		return store.Assessment{}, err
	}
	a, _, err := e.loadAssessment(ctx, scope, catalogName, controlID)
	if err != nil {
		return store.Assessment{}, err
	}
	evs, err := e.store.ListEvidence(ctx, scope, store.EvidenceQuery{Catalog: catalogName, ControlID: controlID})
	if err != nil {
		return store.Assessment{}, err
	}
	now := e.now()
	req.Actor, req.ActorKind = e.actor(ctx)
	req.Computed, req.RequiresEvidence, req.ReviewInterval, req.At = cr, workflow.RequiresEvidenceWith(ctl, override), interval, now
	req.Evidence = workflow.Summarize(evs, store.ControlRef{Catalog: catalogName, ControlID: controlID}, now)
	next, tr, err := workflow.Apply(workflow.Config{AllowSelfApproval: e.workflow.AllowSelfApproval}, a, req)
	if err != nil {
		return store.Assessment{}, err
	}
	details := map[string]any{"from_stage": tr.FromStage, "to_stage": tr.ToStage, "from_status": tr.FromStatus, "to_status": tr.ToStatus}
	// Reasons and notes are kept, encrypted at rest, in the history only; the
	// audit log (plaintext, so its chain verifies without keys) records that they exist.
	for k, v := range map[string]string{"evaluation_id": tr.EvaluationID, "owner": next.Owner, "reviewer": next.Reviewer} {
		if v != "" {
			details[k] = v
		}
	}
	details["reason_given"], details["note_given"] = tr.Reason != "", tr.Note != ""
	if tr.SelfApproved {
		details["self_approved"] = true
	}
	ev := e.auditEvent(ctx, scope, "assessment."+string(req.Action), "control", catalogName+"/"+controlID, details)
	return e.store.SaveAssessment(ctx, next, a.Version, &tr, ev)
}

// approvalCatalog returns the catalog version of the named evaluation.
func (e *Engine) approvalCatalog(ctx context.Context, scope Scope, catalogName, evaluationID string) (*catalog.Catalog, error) {
	if strings.TrimSpace(evaluationID) == "" {
		return nil, fmt.Errorf("%w: evaluation_id is required to approve", ErrStaleEvaluation)
	}
	ev, err := e.Evaluation(ctx, scope, evaluationID)
	if err != nil {
		return nil, err
	}
	for _, ref := range ev.Catalogs {
		if ref.Catalog == catalogName {
			c, ok := e.catalogs.Get(ref)
			if !ok {
				return nil, fmt.Errorf("%w: catalog %s is no longer loaded", ErrStaleEvaluation, ref)
			}
			return c, nil
		}
	}
	return nil, fmt.Errorf("%w: evaluation %s does not cover catalog %s", ErrStaleEvaluation, evaluationID, catalogName)
}

// checkFresh refuses an approval based on an evaluation of another snapshot,
// or whose computed status of the control differs from the current one.
func (e *Engine) checkFresh(ctx context.Context, scope Scope, evaluationID, snapshotID, catalogName string, current engine.ControlResult) error {
	ev, err := e.Evaluation(ctx, scope, evaluationID)
	if err != nil {
		return err
	}
	if ev.SnapshotID != snapshotID {
		return fmt.Errorf("%w: evaluation %s is of %s, the current snapshot is %s", ErrStaleEvaluation, evaluationID, ev.SnapshotID, snapshotID)
	}
	for _, fw := range ev.Result.Frameworks {
		if fw.Catalog.Catalog != catalogName {
			continue
		}
		for _, c := range fw.Controls {
			if c.ControlID == current.ControlID && c.Status != current.Status {
				return fmt.Errorf("%w: the control was %s in evaluation %s and is %s now", ErrStaleEvaluation, c.Status, evaluationID, current.Status)
			}
		}
	}
	return nil
}
