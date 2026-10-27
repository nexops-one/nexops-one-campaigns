// SPDX-License-Identifier: Apache-2.0

package postgres_test

import (
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/nexops-one/compliance-engine/pkg/audit"
	"github.com/nexops-one/compliance-engine/pkg/store"
	"github.com/nexops-one/compliance-engine/pkg/store/postgres"
	"github.com/nexops-one/compliance-engine/pkg/store/storetest"
	"github.com/nexops-one/compliance-engine/sdk/adapter"
)

var auditScope = adapter.Scope{TenantID: "t", WorkspaceID: "w"}

func TestAuditTableIsAppendOnly(t *testing.T) {
	s, url := migrated(t)
	if err := s.AppendAudit(ctx, storetest.Event(auditScope, "a")); err != nil {
		t.Fatal(err)
	}
	conn, err := pgx.Connect(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close(ctx)
	for _, sql := range []string{
		`UPDATE ce_audit_events SET actor = 'someone-else'`,
		`DELETE FROM ce_audit_events`,
		`TRUNCATE ce_audit_events`,
	} {
		if _, err := conn.Exec(ctx, sql); err == nil || !strings.Contains(err.Error(), "append-only") {
			t.Errorf("%s: err = %v, want append-only refusal", sql, err)
		}
	}
	evs, err := s.AuditEvents(ctx, auditScope, store.AuditQuery{})
	if err != nil || len(evs) != 1 || evs[0].Actor != "user:u1" {
		t.Fatalf("events = %+v, %v", evs, err)
	}
}

func TestAuditDownRefusesWithEvents(t *testing.T) {
	s, _ := migrated(t)
	ms, _ := postgres.Migrations()
	if _, err := s.MigrateDown(ctx, len(ms)-2); err != nil { // migrations above the audit log: no audit data involved
		t.Fatal(err)
	}
	if err := s.AppendAudit(ctx, storetest.Event(auditScope, "a")); err != nil {
		t.Fatal(err)
	}
	if _, err := s.MigrateDown(ctx, 1); err == nil || !strings.Contains(err.Error(), "--force-drop-audit") {
		t.Fatalf("down over audit events = %v", err)
	}
	if evs, err := s.AuditEvents(ctx, auditScope, store.AuditQuery{}); err != nil || len(evs) != 1 {
		t.Fatalf("the refused down must keep the events: %v, %v", evs, err)
	}
	if got, err := s.MigrateDown(ctx, 1, postgres.ForceDropAudit()); err != nil || len(got) != 1 || got[0] != 2 {
		t.Fatalf("forced down = %v, %v", got, err)
	}
	if _, err := s.MigrateUp(ctx); err != nil {
		t.Fatal(err)
	}
}

func TestConcurrentAuditAppends(t *testing.T) {
	s, _ := migrated(t)
	var wg sync.WaitGroup
	errs := make([]error, 8)
	for i := range errs {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			errs[i] = s.AppendAudit(ctx, storetest.Event(auditScope, fmt.Sprintf("e%d", i)))
		}(i)
	}
	wg.Wait()
	for _, err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	evs, err := s.AuditEvents(ctx, auditScope, store.AuditQuery{})
	if err != nil {
		t.Fatal(err)
	}
	if r := audit.Verify(evs); !r.OK || r.Checked != 8 {
		t.Fatalf("verify = %+v", r)
	}
}

func TestAssessmentHistoryIsAppendOnly(t *testing.T) {
	s, url := migrated(t)
	a := store.Assessment{Scope: auditScope, Catalog: "dora", ControlID: "c1", Stage: store.StageNone}
	if _, err := s.SaveAssessment(ctx, a, 0, &store.Transition{Action: "assign", Actor: "u"}); err != nil {
		t.Fatal(err)
	}
	conn, err := pgx.Connect(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close(ctx)
	for _, sql := range []string{`UPDATE ce_assessment_history SET catalog = 'x'`, `DELETE FROM ce_assessment_history`, `TRUNCATE ce_assessment_history`} {
		if _, err := conn.Exec(ctx, sql); err == nil || !strings.Contains(err.Error(), "append-only") {
			t.Errorf("%s: err = %v", sql, err)
		}
	}
}
