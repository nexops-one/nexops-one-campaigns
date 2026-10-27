// SPDX-License-Identifier: Apache-2.0

package compliance_test

import (
	"testing"
	"time"

	"github.com/nexops-one/compliance-engine/pkg/compliance"
	"github.com/nexops-one/compliance-engine/pkg/store"
)

func TestRunRetention(t *testing.T) {
	w := newWorkflowEnv(t, func(c *compliance.Config) { c.Retention.AuditDays = 30 })
	for i := 0; i < 3; i++ { // revisions 2..4, one per 10 days
		*w.now = w.now.Add(10 * 24 * time.Hour)
		b := sampleBatch(t)
		b.Batch.Mode = "incremental"
		b.Entities["ict_provider"][0]["legal_name"] = []string{"A", "B", "C"}[i]
		if _, err := w.eng.Ingest(ctx, scopeA, b); err != nil {
			t.Fatal(err)
		}
	}
	pinned := w.evaluate(t) // of rev-4
	*w.now = w.now.Add(100 * 24 * time.Hour)
	if _, err := w.eng.UpdateSettings(w.owner, scopeA, compliance.SettingsInput{Retention: &store.RetentionPolicy{RevisionDays: 50, KeepRevisions: 1, EvaluationDays: 0}}); err != nil {
		t.Fatal(err)
	}
	dry, err := w.eng.RunRetention(ctx, scopeA, true)
	if err != nil || dry.Plan.KeepFrom != 4 || dry.Deleted.Revisions != 0 {
		t.Fatalf("dry run = %+v %v", dry, err)
	}
	if revs, _ := w.eng.Snapshots(ctx, scopeA); len(revs) != 4 {
		t.Fatal("a dry run deletes nothing")
	}
	rep, err := w.eng.RunRetention(ctx, scopeA, false)
	if err != nil || rep.Deleted.Revisions != 3 || rep.AuditPruned == 0 {
		t.Fatalf("run = %+v %v", rep, err)
	}
	if _, err := w.eng.Evaluation(ctx, scopeA, pinned.ID); err != nil {
		t.Fatal("kept evaluations stay readable")
	}
	if _, err := w.eng.Snapshot(ctx, scopeA, "rev-4"); err != nil {
		t.Fatal("the pinned snapshot is kept")
	}
	if r, err := w.eng.VerifyAudit(ctx, scopeA); err != nil || !r.OK {
		t.Fatalf("the pruned audit chain must verify: %+v %v", r, err)
	}
	evs, _ := w.st.AuditEvents(ctx, scopeA, store.AuditQuery{})
	last := evs[len(evs)-1]
	if last.Action != "audit.pruned" || evs[len(evs)-2].Action != "retention.run" {
		t.Fatalf("retention events = %s, %s", evs[len(evs)-2].Action, last.Action)
	}
}
