// SPDX-License-Identifier: Apache-2.0

package compliance

import (
	"context"

	"github.com/nexops-one/compliance-engine/pkg/retention"
	"github.com/nexops-one/compliance-engine/pkg/store"
)

// RetentionReport describes a retention run (or, with DryRun, what it would delete).
type RetentionReport struct {
	Scope       Scope                 `json:"scope"`
	DryRun      bool                  `json:"dry_run"`
	Plan        retention.Plan        `json:"plan"`
	Deleted     store.RetentionCounts `json:"deleted"`
	AuditPruned int                   `json:"audit_pruned"`
}

// RunRetention applies the workspace's retention policy. Floors always win:
// evidence within its minimum retention or relied on by an approval, the
// evaluations of kept reports, snapshots of kept evaluations and reports, the
// latest revisions and recent audit events are kept.
func (e *Engine) RunRetention(ctx context.Context, scope Scope, dryRun bool) (RetentionReport, error) {
	rep := RetentionReport{Scope: scope, DryRun: dryRun}
	if err := scope.Validate(); err != nil {
		return rep, err
	}
	set, err := e.store.Settings(ctx, scope)
	if err != nil {
		return rep, err
	}
	revs, err := e.store.Revisions(ctx, scope)
	if err != nil {
		return rep, err
	}
	evals, err := e.store.EvaluationRefs(ctx, scope)
	if err != nil {
		return rep, err
	}
	reports, err := e.store.ReportRefs(ctx, scope)
	if err != nil {
		return rep, err
	}
	evs, err := e.store.ListEvidence(ctx, scope, store.EvidenceQuery{})
	if err != nil {
		return rep, err
	}
	as, err := e.store.Assessments(ctx, scope, "")
	if err != nil {
		return rep, err
	}
	rep.Plan = retention.Decide(retention.Input{Policy: set.Retention, AuditDays: e.retention.AuditDays, Now: e.now(),
		Revisions: revs, Evaluations: evals, Reports: reports, Evidence: evs, Assessments: as})
	if dryRun {
		return rep, nil
	}
	if rep.Plan.KeepFrom > 0 || len(rep.Plan.DeleteEvaluations) > 0 || len(rep.Plan.DeleteReports) > 0 || len(rep.Plan.DeleteEvidence) > 0 {
		ev := e.auditEvent(ctx, scope, "retention.run", "workspace", scope.WorkspaceID, map[string]any{
			"keep_from": rep.Plan.KeepFrom, "evaluations": rep.Plan.DeleteEvaluations, "reports": rep.Plan.DeleteReports, "evidence": rep.Plan.DeleteEvidence,
		})
		del := store.RetentionDeletion{KeepFrom: rep.Plan.KeepFrom, Evaluations: rep.Plan.DeleteEvaluations, Reports: rep.Plan.DeleteReports,
			Evidence: rep.Plan.DeleteEvidence}
		if rep.Deleted, err = e.store.ApplyRetention(ctx, scope, del, ev); err != nil {
			return rep, err
		}
		deleted := map[string]bool{}
		for _, id := range rep.Plan.DeleteEvidence {
			deleted[id] = true
		}
		for _, x := range evs {
			if deleted[x.ID] {
				if err := e.dropManagedObject(ctx, scope, x); err != nil {
					return rep, err
				}
			}
		}
	}
	if rep.Plan.AuditBefore != nil {
		ev := e.auditEvent(ctx, scope, "audit.pruned", "workspace", scope.WorkspaceID, map[string]any{"before": rep.Plan.AuditBefore})
		if rep.AuditPruned, err = e.store.PruneAudit(ctx, scope, *rep.Plan.AuditBefore, ev); err != nil {
			return rep, err
		}
	}
	return rep, nil
}

// WorkspaceScopes lists every workspace holding data (retention runs, CLI).
func (e *Engine) WorkspaceScopes(ctx context.Context) ([]Scope, error) {
	return e.store.WorkspaceScopes(ctx)
}
