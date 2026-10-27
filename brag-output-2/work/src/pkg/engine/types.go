// SPDX-License-Identifier: Apache-2.0

// Package engine evaluates control catalogs against a snapshot. Evaluate is a
// pure function: same snapshot and catalogs, same result.
package engine

import (
	"github.com/nexops-one/compliance-engine/pkg/canonical"
	"github.com/nexops-one/compliance-engine/pkg/catalog"
)

// Status is a control status. The engine produces not_assessed, in_review and
// monitoring; ready is reserved for human approval (M11 workflow).
type Status string

const (
	StatusNotAssessed Status = "not_assessed"
	StatusInReview    Status = "in_review"
	StatusMonitoring  Status = "monitoring"
	StatusReady       Status = "ready"
	StatusRejected    Status = "rejected" // a reviewer sent the control back (workflow)
	StatusExpired     Status = "expired"  // an approval passed its review interval (workflow)
)

// BlockerReason explains why a control is not assessed.
type BlockerReason string

const (
	ReasonMissing     BlockerReason = "missing"
	ReasonNotSupplied BlockerReason = "not_supplied_by_any_adapter"
	ReasonDerived     BlockerReason = "derived_unconfirmed"
	ReasonDangling    BlockerReason = "dangling_reference"
	ReasonNoRecords   BlockerReason = "no_records"
	ReasonManual      BlockerReason = "requires_human_assessment"
)

// Blocker is one reason a control could not be assessed.
type Blocker struct {
	Reason BlockerReason `json:"reason"`
	Entity string        `json:"entity,omitempty"`
	Key    string        `json:"key,omitempty"`
	Field  string        `json:"field,omitempty"`
}

// Explanation traces a result back to its rule, inputs and records.
type Explanation struct {
	Rule            catalog.Rule `json:"rule"`
	Inputs          []string     `json:"inputs"`
	RecordsExamined []string     `json:"records_examined"`
	Failing         []string     `json:"failing,omitempty"`
}

// ControlResult is the evaluation of one control.
type ControlResult struct {
	ControlID string `json:"control_id"`
	Title     string `json:"title"`
	Status    Status `json:"status"`
	// Attention qualifies in_review: rule_failed (data or remediation needed) or,
	// in effective results, awaiting_approval, evidence_invalid, inputs_incomplete.
	Attention   string      `json:"attention,omitempty"`
	Blockers    []Blocker   `json:"blockers,omitempty"`
	Explanation Explanation `json:"explanation"`
}

// Tally holds counts, score and coverage. Score and coverage are always
// reported together.
type Tally struct {
	InScope        int    `json:"in_scope"`
	Assessable     int    `json:"assessable"`
	Ready          int    `json:"ready"`
	Monitoring     int    `json:"monitoring"`
	InReview       int    `json:"in_review"`
	NotAssessed    int    `json:"not_assessed"`
	Rejected       int    `json:"rejected"`
	Expired        int    `json:"expired"`
	ScoreNumerator int    `json:"score_numerator"`
	ScorePct       int    `json:"score_pct"`
	ScoreDefined   bool   `json:"score_defined"`
	CoveragePct    int    `json:"coverage_pct"`
	Assumptions    string `json:"assumptions,omitempty"`
}

// FrameworkResult is the evaluation of one catalog.
type FrameworkResult struct {
	Catalog   catalog.Ref     `json:"catalog"`
	Framework string          `json:"framework"`
	Tally     Tally           `json:"tally"`
	Controls  []ControlResult `json:"controls"`
}

// Result is a full evaluation.
type Result struct {
	SnapshotID    string            `json:"snapshot_id"`
	SchemaVersion string            `json:"schema_version"`
	Frameworks    []FrameworkResult `json:"frameworks"`
	Overall       Tally             `json:"overall"`
}

// Input is everything Evaluate needs. Supplied may be nil (supply unknown).
type Input struct {
	SnapshotID string
	Index      *canonical.Index
	References canonical.ReferenceReport
	Supplied   canonical.Supply
	Catalogs   []*catalog.Catalog
}
