// SPDX-License-Identifier: Apache-2.0

package store

import (
	"context"
	"time"

	"github.com/nexops-one/compliance-engine/sdk/adapter"
)

// ControlRef names a control independently of catalog versions.
type ControlRef struct {
	Catalog   string `json:"catalog"`
	ControlID string `json:"control_id"`
}

// Integrity is the outcome of the latest checksum check of an evidence item.
type Integrity string

const (
	IntegrityUnverified Integrity = "unverified" // checksum as declared by the collector
	IntegrityVerified   Integrity = "verified"
	IntegrityFailed     Integrity = "failed"
	// IntegrityNoChecksum: recorded without a checksum (only when the deployment
	// allows it). Such evidence never satisfies an evidence requirement until
	// an attested check supplies the checksum.
	IntegrityNoChecksum Integrity = "no_checksum"
)

// EvidenceState is the derived state of an evidence item at a point in time.
type EvidenceState string

const (
	EvidenceActive          EvidenceState = "active"
	EvidenceExpired         EvidenceState = "expired"
	EvidenceRevoked         EvidenceState = "revoked"
	EvidenceIntegrityFailed EvidenceState = "integrity_failed"
)

// Retention is the minimum retention of an evidence item.
type Retention struct {
	MinDays int    `json:"min_days"`
	Basis   string `json:"basis,omitempty"`
}

// EvidenceCheck is one checksum check of an evidence item.
type EvidenceCheck struct {
	At       time.Time `json:"at"`
	Actor    string    `json:"actor"`
	Method   string    `json:"method"` // engine_file, engine_https, attested
	Observed string    `json:"observed"`
	Match    bool      `json:"match"`
}

// Evidence is a reference to an object kept in the customer's environment,
// with its checksum. The engine never stores the object itself.
type Evidence struct {
	ID           string          `json:"id"`
	Scope        adapter.Scope   `json:"scope"`
	Title        string          `json:"title"`
	Kind         string          `json:"kind"`
	Source       string          `json:"source"`
	URI          string          `json:"uri"`
	Checksum     string          `json:"checksum"`
	CollectedAt  time.Time       `json:"collected_at"`
	Collector    string          `json:"collector"`
	ValidUntil   *time.Time      `json:"valid_until,omitempty"`
	Retention    Retention       `json:"retention"`
	Integrity    Integrity       `json:"integrity"`
	Checks       []EvidenceCheck `json:"checks"`
	RevokedAt    *time.Time      `json:"revoked_at,omitempty"`
	RevokedBy    string          `json:"revoked_by,omitempty"`
	RevokeReason string          `json:"revoke_reason,omitempty"`
	Links        []ControlRef    `json:"links"`
	CreatedAt    time.Time       `json:"created_at"`
	Version      int64           `json:"version"`
}

// StateAt derives the evidence state at t: revoked, then integrity failure,
// then expiry, else active.
func (e Evidence) StateAt(t time.Time) EvidenceState {
	switch {
	case e.RevokedAt != nil && !t.Before(*e.RevokedAt):
		return EvidenceRevoked
	case e.Integrity == IntegrityFailed:
		return EvidenceIntegrityFailed
	case e.ValidUntil != nil && !t.Before(*e.ValidUntil):
		return EvidenceExpired
	}
	return EvidenceActive
}

// LinkedTo reports whether the evidence is linked to ref.
func (e Evidence) LinkedTo(ref ControlRef) bool {
	for _, l := range e.Links {
		if l == ref {
			return true
		}
	}
	return false
}

// Stage is the human review stage of an assessment.
type Stage string

const (
	StageNone      Stage = "none"
	StageSubmitted Stage = "submitted"
	StageRejected  Stage = "rejected"
	StageApproved  Stage = "approved"
)

// Approval records who approved a control, on which evaluation, until when.
type Approval struct {
	Actor        string    `json:"actor"`
	At           time.Time `json:"at"`
	EvaluationID string    `json:"evaluation_id"`
	SnapshotID   string    `json:"snapshot_id"`
	ExpiresAt    time.Time `json:"expires_at"`
	SelfApproved bool      `json:"self_approved,omitempty"`
	// Imported marks an approval carried over from another system by an
	// operator import, not given in this engine.
	Imported bool `json:"imported,omitempty"`
	// Approvers lists every approver of a quorum approval (more than one
	// approval required), in the order they approved; Actor is the last.
	Approvers []string `json:"approvers,omitempty"`
}

// PendingApproval is one approval given towards a quorum not yet reached.
type PendingApproval struct {
	Actor        string    `json:"actor"`
	At           time.Time `json:"at"`
	EvaluationID string    `json:"evaluation_id,omitempty"`
}

// Assessment is the human side of one control in one workspace, keyed by
// catalog name and control ID so it survives catalog version updates.
type Assessment struct {
	Scope     adapter.Scope `json:"scope"`
	Catalog   string        `json:"catalog"`
	ControlID string        `json:"control_id"`
	Owner     string        `json:"owner,omitempty"`
	Reviewer  string        `json:"reviewer,omitempty"`
	DueAt     *time.Time    `json:"due_at,omitempty"`
	Notes     string        `json:"notes,omitempty"`
	Stage     Stage         `json:"stage"`
	Approval  *Approval     `json:"approval,omitempty"`
	// SubmittedBy is the actor of the last submit; Recommended records a
	// reviewer's recommendation since then; Approvals are the approvals given
	// since then towards RequiredApprovals (set when more than one is
	// required). All are cleared by the next submit, reject or reopen.
	SubmittedBy       string            `json:"submitted_by,omitempty"`
	Recommended       bool              `json:"recommended,omitempty"`
	Approvals         []PendingApproval `json:"approvals,omitempty"`
	RequiredApprovals int               `json:"required_approvals,omitempty"`
	UpdatedAt         time.Time         `json:"updated_at"`
	Version           int64             `json:"version"`
}

// Ref returns the assessment's control reference.
func (a Assessment) Ref() ControlRef { return ControlRef{Catalog: a.Catalog, ControlID: a.ControlID} }

// Transition is one entry of an assessment's append-only history.
type Transition struct {
	Seq          int64     `json:"seq"`
	Catalog      string    `json:"catalog"`
	ControlID    string    `json:"control_id"`
	At           time.Time `json:"at"`
	Actor        string    `json:"actor"`
	ActorKind    string    `json:"actor_kind"`
	Action       string    `json:"action"`
	FromStage    Stage     `json:"from_stage"`
	ToStage      Stage     `json:"to_stage"`
	FromStatus   string    `json:"from_status,omitempty"`
	ToStatus     string    `json:"to_status,omitempty"`
	Reason       string    `json:"reason,omitempty"`
	Note         string    `json:"note,omitempty"`
	EvaluationID string    `json:"evaluation_id,omitempty"`
	SelfApproved bool      `json:"self_approved,omitempty"`
	// Approvals and RequiredApprovals record a quorum approval's progress:
	// an approve below the quorum keeps the stage submitted.
	Approvals         int `json:"approvals,omitempty"`
	RequiredApprovals int `json:"required_approvals,omitempty"`
}

// EvidenceQuery filters evidence by linked control; empty fields match all.
type EvidenceQuery struct {
	Catalog   string
	ControlID string
}

// EvidenceStore stores evidence documents. PutEvidence creates (expected
// version 0) or replaces a document whose stored version equals the expected
// one, increments Version, and appends events in the same transaction.
type EvidenceStore interface {
	PutEvidence(ctx context.Context, e Evidence, expectedVersion int64, events ...AuditEvent) (Evidence, error)
	Evidence(ctx context.Context, scope adapter.Scope, id string) (Evidence, error)
	// ListEvidence orders by creation time, then ID.
	ListEvidence(ctx context.Context, scope adapter.Scope, q EvidenceQuery) ([]Evidence, error)
}

// WorkflowStore stores assessments and their append-only history.
type WorkflowStore interface {
	Assessment(ctx context.Context, scope adapter.Scope, catalog, controlID string) (Assessment, error)
	// Assessments lists a workspace's assessments ("" = every catalog), by catalog then control.
	Assessments(ctx context.Context, scope adapter.Scope, catalog string) ([]Assessment, error)
	// SaveAssessment creates (expected version 0) or replaces an assessment,
	// appends tr (when not nil; its Seq is assigned per workspace) and events,
	// all in one transaction. It returns the stored assessment.
	SaveAssessment(ctx context.Context, a Assessment, expectedVersion int64, tr *Transition, events ...AuditEvent) (Assessment, error)
	// History lists a control's transitions in ascending Seq.
	History(ctx context.Context, scope adapter.Scope, catalog, controlID string) ([]Transition, error)
}
