// SPDX-License-Identifier: Apache-2.0

// Package retention decides what a workspace's retention policy allows to
// delete. It is pure: the store applies the plan. Floors always win over the
// policy: evidence is never deleted before its own minimum retention or while
// an approval may rely on it, kept reports keep their evaluation, snapshots
// referenced by kept evaluations or reports are kept, the latest revisions are
// kept, and audit events are kept for the configured number of days.
package retention

import (
	"sort"
	"time"

	"github.com/nexops-one/compliance-engine/pkg/store"
)

// Input is everything the planner looks at.
type Input struct {
	Policy      store.RetentionPolicy
	AuditDays   int // 0 keeps audit events forever
	Now         time.Time
	Revisions   []store.Revision
	Evaluations []store.EvaluationRef
	Reports     []store.ReportRef
	Evidence    []store.Evidence
	Assessments []store.Assessment
}

// Plan is what may be deleted.
type Plan struct {
	// KeepFrom is the oldest revision kept; revisions below it are deleted (0: none).
	KeepFrom          int64      `json:"keep_from"`
	DeleteRevisions   int        `json:"delete_revisions"`
	DeleteEvaluations []string   `json:"delete_evaluations"`
	DeleteReports     []string   `json:"delete_reports"`
	DeleteEvidence    []string   `json:"delete_evidence"`
	AuditBefore       *time.Time `json:"audit_before,omitempty"`
}

func days(n int) time.Duration { return time.Duration(n) * 24 * time.Hour }

// Decide computes the retention plan.
func Decide(in Input) Plan {
	p := Plan{DeleteEvaluations: []string{}, DeleteReports: []string{}, DeleteEvidence: []string{}}
	expired := func(t time.Time) bool {
		return in.Policy.EvaluationDays > 0 && t.Before(in.Now.Add(-days(in.Policy.EvaluationDays)))
	}

	// Reports first: kept reports pin their evaluations and snapshots, and
	// kept evaluations pin their snapshots.
	pinned := map[int64]bool{}
	reported := map[string]bool{}
	for _, r := range in.Reports {
		if expired(r.CreatedAt) {
			p.DeleteReports = append(p.DeleteReports, r.ID)
			continue
		}
		reported[r.EvaluationID] = true
		if n, err := store.ParseSnapshotID(r.SnapshotID); err == nil {
			pinned[n] = true
		}
	}
	sort.Strings(p.DeleteReports)
	for _, e := range in.Evaluations {
		if expired(e.CreatedAt) && !reported[e.ID] {
			p.DeleteEvaluations = append(p.DeleteEvaluations, e.ID)
			continue
		}
		if n, err := store.ParseSnapshotID(e.SnapshotID); err == nil {
			pinned[n] = true
		}
	}
	sort.Strings(p.DeleteEvaluations)

	revs := append([]store.Revision(nil), in.Revisions...)
	sort.Slice(revs, func(i, j int) bool { return revs[i].Number < revs[j].Number })
	keep := in.Policy.KeepRevisions
	if keep < 1 {
		keep = 1
	}
	if in.Policy.RevisionDays > 0 && len(revs) > keep {
		cutoff := in.Now.Add(-days(in.Policy.RevisionDays))
		// Only a prefix can go: stop at the first revision that must stay.
		for i, r := range revs[:len(revs)-keep] {
			if !r.CreatedAt.Before(cutoff) || pinned[r.Number] {
				break
			}
			p.KeepFrom, p.DeleteRevisions = revs[i+1].Number, i+1
		}
	}

	approved := map[store.ControlRef]bool{}
	for _, a := range in.Assessments {
		if a.Stage == store.StageApproved {
			approved[a.Ref()] = true
		}
	}
	for _, e := range in.Evidence {
		state := e.StateAt(in.Now)
		if state != store.EvidenceRevoked && state != store.EvidenceExpired {
			continue
		}
		if in.Now.Before(e.CreatedAt.Add(days(e.Retention.MinDays))) {
			continue
		}
		relied := false
		for _, l := range e.Links {
			relied = relied || approved[l]
		}
		if !relied {
			p.DeleteEvidence = append(p.DeleteEvidence, e.ID)
		}
	}
	sort.Strings(p.DeleteEvidence)

	if in.AuditDays > 0 {
		before := in.Now.Add(-days(in.AuditDays))
		p.AuditBefore = &before
	}
	return p
}
