// SPDX-License-Identifier: Apache-2.0

package workflow

import (
	"time"

	"github.com/nexops-one/compliance-engine/pkg/catalog"
	"github.com/nexops-one/compliance-engine/pkg/engine"
	"github.com/nexops-one/compliance-engine/pkg/store"
)

// Input is everything the effective status depends on.
type Input struct {
	Computed    engine.Result
	Catalogs    []*catalog.Catalog
	Assessments []store.Assessment
	Evidence    []store.Evidence
	AsOf        time.Time
	// Overrides are the workspace review overrides, keyed by OverrideKey.
	Overrides map[string]store.ReviewOverride
}

// EvidenceRef is the state of one linked evidence item in a result.
type EvidenceRef struct {
	ID        string              `json:"id"`
	State     store.EvidenceState `json:"state"`
	Integrity store.Integrity     `json:"integrity"`
}

// Control is the effective result of one control.
type Control struct {
	ControlID        string          `json:"control_id"`
	Title            string          `json:"title"`
	Status           engine.Status   `json:"status"`
	ComputedStatus   engine.Status   `json:"computed_status"`
	Attention        string          `json:"attention,omitempty"`
	Stage            store.Stage     `json:"stage"`
	Owner            string          `json:"owner,omitempty"`
	Reviewer         string          `json:"reviewer,omitempty"`
	DueAt            *time.Time      `json:"due_at,omitempty"`
	Overdue          bool            `json:"overdue"`
	Approval         *store.Approval `json:"approval,omitempty"`
	RequiresEvidence bool            `json:"requires_evidence"`
	Evidence         []EvidenceRef   `json:"evidence"`
	VoidReason       string          `json:"void_reason,omitempty"`
	// Approvals is the progress of a quorum approval under way.
	Approvals *Quorum `json:"approvals,omitempty"`
}

// Quorum is the progress of an approval needing several approvers.
type Quorum struct {
	Given    int `json:"given"`
	Required int `json:"required"`
}

// Framework is the effective result of one catalog.
type Framework struct {
	Catalog   catalog.Ref  `json:"catalog"`
	Framework string       `json:"framework"`
	Tally     engine.Tally `json:"tally"`
	Controls  []Control    `json:"controls"`
}

// Inputs are the workflow inputs a result was computed from, stored with it so
// it can be recomputed. Evidence is reduced to what its state depends on.
type Inputs struct {
	Assessments []store.Assessment              `json:"assessments"`
	Evidence    []store.Evidence                `json:"evidence"`
	Overrides   map[string]store.ReviewOverride `json:"overrides,omitempty"`
}

// Result is an effective evaluation.
type Result struct {
	AsOf       time.Time    `json:"as_of"`
	SnapshotID string       `json:"snapshot_id"`
	Frameworks []Framework  `json:"frameworks"`
	Overall    engine.Tally `json:"overall"`
	Inputs     Inputs       `json:"inputs"`
}

// Recompute recomputes a stored result from its computed result, catalogs and inputs.
func Recompute(computed engine.Result, cats []*catalog.Catalog, r Result) Result {
	return Effective(Input{Computed: computed, Catalogs: cats, Assessments: r.Inputs.Assessments, Evidence: r.Inputs.Evidence, AsOf: r.AsOf, Overrides: r.Inputs.Overrides})
}

// reduce keeps only the fields an evidence state depends on (no URI, checksum
// or source), so stored results never widen access to evidence locations.
func reduce(e store.Evidence) store.Evidence {
	return store.Evidence{ID: e.ID, Scope: e.Scope, Integrity: e.Integrity, ValidUntil: e.ValidUntil, RevokedAt: e.RevokedAt,
		Links: append([]store.ControlRef{}, e.Links...), Checks: []store.EvidenceCheck{}, Kind: e.Kind}
}

// Effective computes effective statuses, score and coverage.
func Effective(in Input) Result {
	res := Result{AsOf: in.AsOf, SnapshotID: in.Computed.SnapshotID, Frameworks: []Framework{},
		Inputs: Inputs{Assessments: []store.Assessment{}, Evidence: []store.Evidence{}}}
	byName := map[catalog.Ref]*catalog.Catalog{}
	for _, c := range in.Catalogs {
		byName[c.Ref()] = c
	}
	assessments := map[store.ControlRef]store.Assessment{}
	for _, a := range in.Assessments {
		assessments[a.Ref()] = a
	}
	usedEvidence := map[string]bool{}
	var tallies []engine.Tally
	for _, fr := range in.Computed.Frameworks {
		cat := byName[fr.Catalog]
		out := Framework{Catalog: fr.Catalog, Framework: fr.Framework, Controls: []Control{}}
		var statuses []engine.ControlResult
		for _, cr := range fr.Controls {
			ref := store.ControlRef{Catalog: fr.Catalog.Catalog, ControlID: cr.ControlID}
			a, has := assessments[ref]
			requires := false
			if cat != nil {
				for _, c := range cat.Controls {
					if c.ID == cr.ControlID {
						requires = RequiresEvidenceWith(c, in.Overrides[OverrideKey(fr.Catalog.Catalog, c.ID)])
					}
				}
			}
			ev := Summarize(in.Evidence, ref, in.AsOf)
			e := Evaluate(cr, a, requires, ev, in.AsOf)
			ctl := Control{
				ControlID: cr.ControlID, Title: cr.Title, Status: e.Status, ComputedStatus: cr.Status, Attention: e.Attention,
				Stage: stageOf(a), Owner: a.Owner, Reviewer: a.Reviewer, DueAt: a.DueAt, Approval: a.Approval,
				RequiresEvidence: requires, Evidence: []EvidenceRef{}, VoidReason: e.VoidReason,
			}
			if ctl.Stage == store.StageSubmitted && a.RequiredApprovals > 1 {
				ctl.Approvals = &Quorum{Given: len(a.Approvals), Required: a.RequiredApprovals}
			}
			ctl.Overdue = a.DueAt != nil && in.AsOf.After(*a.DueAt) && e.Status != engine.StatusReady
			for _, x := range in.Evidence {
				if x.LinkedTo(ref) {
					ctl.Evidence = append(ctl.Evidence, EvidenceRef{ID: x.ID, State: x.StateAt(in.AsOf), Integrity: x.Integrity})
					if !usedEvidence[x.ID] {
						usedEvidence[x.ID] = true
						res.Inputs.Evidence = append(res.Inputs.Evidence, reduce(x))
					}
				}
			}
			if has {
				res.Inputs.Assessments = append(res.Inputs.Assessments, a)
			}
			if o, ok := in.Overrides[OverrideKey(fr.Catalog.Catalog, cr.ControlID)]; ok {
				if res.Inputs.Overrides == nil {
					res.Inputs.Overrides = map[string]store.ReviewOverride{}
				}
				res.Inputs.Overrides[OverrideKey(fr.Catalog.Catalog, cr.ControlID)] = o
			}
			out.Controls = append(out.Controls, ctl)
			statuses = append(statuses, engine.ControlResult{ControlID: cr.ControlID, Status: e.Status})
		}
		scoring := fr.Tally
		out.Tally = engine.TallyOf(statuses, catalog.Scoring{CountsAsReady: countsAsReady(cat), Assumptions: scoring.Assumptions})
		tallies = append(tallies, out.Tally)
		res.Frameworks = append(res.Frameworks, out)
	}
	res.Overall = engine.SumTallies(tallies...)
	return res
}

func countsAsReady(cat *catalog.Catalog) []string {
	if cat == nil || len(cat.Scoring.CountsAsReady) == 0 {
		return []string{string(engine.StatusReady), string(engine.StatusMonitoring)}
	}
	return cat.Scoring.CountsAsReady
}
