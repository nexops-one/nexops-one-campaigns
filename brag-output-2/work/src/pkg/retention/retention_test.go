// SPDX-License-Identifier: Apache-2.0

package retention_test

import (
	"reflect"
	"testing"
	"time"

	"github.com/nexops-one/compliance-engine/pkg/retention"
	"github.com/nexops-one/compliance-engine/pkg/store"
)

var now = time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)

func rev(n int64, daysAgo int) store.Revision {
	return store.Revision{Number: n, SnapshotID: store.SnapshotID(n), CreatedAt: now.AddDate(0, 0, -daysAgo)}
}

func TestRetentionFloors(t *testing.T) {
	revs := []store.Revision{rev(1, 400), rev(2, 300), rev(3, 200), rev(4, 100), rev(5, 1)}
	policy := store.RetentionPolicy{RevisionDays: 150, KeepRevisions: 1, EvaluationDays: 250}

	p := retention.Decide(retention.Input{Policy: policy, Now: now, Revisions: revs})
	if p.KeepFrom != 4 || p.DeleteRevisions != 3 {
		t.Fatalf("plain cut = %+v", p)
	}
	// A kept evaluation pins its snapshot, and only a prefix can go.
	evals := []store.EvaluationRef{
		{ID: "e-old", SnapshotID: "rev-1", CreatedAt: now.AddDate(0, 0, -390)},
		{ID: "e-pin", SnapshotID: "rev-2", CreatedAt: now.AddDate(0, 0, -10)},
	}
	p = retention.Decide(retention.Input{Policy: policy, Now: now, Revisions: revs, Evaluations: evals})
	if p.KeepFrom != 2 || p.DeleteRevisions != 1 || !reflect.DeepEqual(p.DeleteEvaluations, []string{"e-old"}) {
		t.Fatalf("pinned = %+v", p)
	}
	// keep_revisions wins over age.
	p = retention.Decide(retention.Input{Policy: store.RetentionPolicy{RevisionDays: 1, KeepRevisions: 4}, Now: now, Revisions: revs})
	if p.KeepFrom != 2 {
		t.Fatalf("keep latest 4 = %+v", p)
	}
	// Zero days keep forever.
	if p := retention.Decide(retention.Input{Policy: store.RetentionPolicy{KeepRevisions: 1}, Now: now, Revisions: revs, Evaluations: evals}); p.KeepFrom != 0 || len(p.DeleteEvaluations) != 0 {
		t.Fatalf("keep forever = %+v", p)
	}

	past := now.AddDate(0, 0, -30)
	ref := store.ControlRef{Catalog: "dora", ControlID: "c1"}
	evidence := []store.Evidence{
		{ID: "active", CreatedAt: now.AddDate(-20, 0, 0)},
		{ID: "revoked-young", CreatedAt: now.AddDate(0, 0, -10), RevokedAt: &past, Retention: store.Retention{MinDays: 3650}},
		{ID: "revoked-old", CreatedAt: now.AddDate(-11, 0, 0), RevokedAt: &past, Retention: store.Retention{MinDays: 3650}},
		{ID: "expired-relied", CreatedAt: now.AddDate(-11, 0, 0), ValidUntil: &past, Links: []store.ControlRef{ref}},
		{ID: "expired-free", CreatedAt: now.AddDate(-1, 0, 0), ValidUntil: &past},
	}
	approved := []store.Assessment{{Catalog: "dora", ControlID: "c1", Stage: store.StageApproved}}
	p = retention.Decide(retention.Input{Policy: policy, Now: now, Evidence: evidence, Assessments: approved, AuditDays: 3650})
	if !reflect.DeepEqual(p.DeleteEvidence, []string{"expired-free", "revoked-old"}) {
		t.Fatalf("evidence = %v", p.DeleteEvidence)
	}
	if p.AuditBefore == nil || !p.AuditBefore.Equal(now.AddDate(0, 0, -3650)) {
		t.Fatalf("audit floor = %v", p.AuditBefore)
	}
	if p := retention.Decide(retention.Input{Now: now}); p.AuditBefore != nil {
		t.Fatal("0 audit days keeps the audit log forever")
	}
}

// TestReportsPinEvaluationsAndSnapshots: reports follow evaluation_days, and a
// kept report keeps its evaluation (even an old one) and its snapshot.
func TestReportsPinEvaluationsAndSnapshots(t *testing.T) {
	revs := []store.Revision{rev(1, 400), rev(2, 300), rev(3, 200), rev(4, 100), rev(5, 1)}
	policy := store.RetentionPolicy{RevisionDays: 150, KeepRevisions: 1, EvaluationDays: 250}
	evals := []store.EvaluationRef{
		{ID: "e-reported", SnapshotID: "rev-1", CreatedAt: now.AddDate(0, 0, -390)},
		{ID: "e-old", SnapshotID: "rev-2", CreatedAt: now.AddDate(0, 0, -390)},
	}
	reports := []store.ReportRef{
		{ID: "r-kept", EvaluationID: "e-reported", SnapshotID: "rev-1", CreatedAt: now.AddDate(0, 0, -20)},
		{ID: "r-old", EvaluationID: "e-old", SnapshotID: "rev-2", CreatedAt: now.AddDate(0, 0, -300)},
	}
	p := retention.Decide(retention.Input{Policy: policy, Now: now, Revisions: revs, Evaluations: evals, Reports: reports})
	if !reflect.DeepEqual(p.DeleteReports, []string{"r-old"}) || !reflect.DeepEqual(p.DeleteEvaluations, []string{"e-old"}) {
		t.Fatalf("reports %v evaluations %v", p.DeleteReports, p.DeleteEvaluations)
	}
	if p.KeepFrom != 0 {
		t.Fatalf("the kept report's snapshot rev-1 must stay: keep_from %d", p.KeepFrom)
	}
	forever := retention.Decide(retention.Input{Policy: store.RetentionPolicy{KeepRevisions: 1}, Now: now, Reports: reports})
	if len(forever.DeleteReports) != 0 {
		t.Fatalf("0 evaluation days keeps reports forever: %v", forever.DeleteReports)
	}
}
