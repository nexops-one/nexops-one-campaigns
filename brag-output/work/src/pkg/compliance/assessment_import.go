// SPDX-License-Identifier: Apache-2.0

package compliance

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/nexops-one/compliance-engine/pkg/extension"
	"github.com/nexops-one/compliance-engine/pkg/store"
	"github.com/nexops-one/compliance-engine/pkg/workflow"
)

// ImportedApproval is an approval given in another system.
type ImportedApproval struct {
	Actor     string    `json:"actor"` // who approved, as the other system knew them (for example "migration:m8")
	At        time.Time `json:"at"`
	ExpiresAt time.Time `json:"expires_at"`
}

// ImportInput carries over one control's assessment from another system.
type ImportInput struct {
	Catalog   string            `json:"catalog"`
	ControlID string            `json:"control_id"`
	Owner     string            `json:"owner,omitempty"` // actor of a tenant user ("user:<id>"); not role-checked
	DueAt     *time.Time        `json:"due_at,omitempty"`
	Notes     string            `json:"notes,omitempty"`
	Approval  *ImportedApproval `json:"approval,omitempty"`
	Reason    string            `json:"reason"`
	Note      string            `json:"note,omitempty"`
}

// ImportAssessment carries over an assessment from another system, for a
// host's data migration. It is an operator action (the context's principal
// must be of kind system) and is not offered by the HTTP API. It never
// overwrites an assessment this engine already has. An imported approval is
// marked imported and follows the effective-status rules like any other: it
// is voided when the control is not assessable or its evidence does not hold,
// and expires at ExpiresAt.
func (e *Engine) ImportAssessment(ctx context.Context, scope Scope, in ImportInput) (store.Assessment, error) {
	if err := scope.Validate(); err != nil {
		return store.Assessment{}, err
	}
	if _, err := e.require(ctx, scope, extension.FeatureWorkflowBasic); err != nil {
		return store.Assessment{}, err
	}
	if _, _, err := e.controlOf(ctx, scope, in.Catalog, in.ControlID); err != nil {
		return store.Assessment{}, err
	}
	if in.Owner != "" {
		id, ok := strings.CutPrefix(in.Owner, "user:")
		if _, err := e.store.User(ctx, scope.TenantID, id); !ok || err != nil {
			return store.Assessment{}, fmt.Errorf("%w: owner %s is not a user of this tenant", ErrInvalidAssignee, in.Owner)
		}
	}
	a, _, err := e.loadAssessment(ctx, scope, in.Catalog, in.ControlID)
	if err != nil {
		return store.Assessment{}, err
	}
	req := workflow.ActionRequest{Action: workflow.ActionImport, Reason: in.Reason, Note: in.Note, At: e.now()}
	req.Actor, req.ActorKind = e.actor(ctx)
	if _, ok := extension.PrincipalFrom(ctx); !ok {
		req.ActorKind = "" // a library caller without a principal is not an operator import
	}
	owner, notes := in.Owner, in.Notes
	due := in.DueAt
	if due != nil {
		u := due.UTC()
		due = &u
	}
	req.Owner, req.Notes, req.DueAt = &owner, &notes, &due
	if in.Approval != nil {
		req.Import = &store.Approval{Actor: in.Approval.Actor, At: in.Approval.At.UTC(), ExpiresAt: in.Approval.ExpiresAt.UTC()}
	}
	next, tr, err := workflow.Apply(workflow.Config{}, a, req)
	if err != nil {
		return store.Assessment{}, err
	}
	details := map[string]any{"to_stage": tr.ToStage, "reason_given": true, "note_given": tr.Note != ""}
	if next.Owner != "" {
		details["owner"] = next.Owner
	}
	if next.Approval != nil {
		details["approval_expires_at"] = next.Approval.ExpiresAt
	}
	ev := e.auditEvent(ctx, scope, "assessment.import", "control", in.Catalog+"/"+in.ControlID, details)
	saved, err := e.store.SaveAssessment(ctx, next, a.Version, &tr, ev)
	if errors.Is(err, store.ErrConflict) {
		return store.Assessment{}, fmt.Errorf("%w: %v", workflow.ErrInvalidTransition, err)
	}
	return saved, err
}
