// SPDX-License-Identifier: Apache-2.0

package postgres_test

import (
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/nexops-one/compliance-engine/pkg/store"
	"github.com/nexops-one/compliance-engine/pkg/store/storetest"
)

func TestReportTablesAreImmutable(t *testing.T) {
	s, url := migrated(t)
	r, files := storetest.ReportDoc(auditScope, "rpt-1", time.Date(2026, 9, 30, 9, 0, 0, 0, time.UTC))
	if err := s.SaveReport(ctx, r, files); err != nil {
		t.Fatal(err)
	}
	conn, err := pgx.Connect(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close(ctx)
	for _, sql := range []string{
		`UPDATE ce_reports SET facts = '\x7b7d'`,
		`UPDATE ce_report_files SET content = '\x00'`,
		`DELETE FROM ce_report_files`,
		`DELETE FROM ce_reports`,
		`TRUNCATE ce_report_files`,
		`TRUNCATE ce_reports CASCADE`,
	} {
		if _, err := conn.Exec(ctx, sql); err == nil || !strings.Contains(err.Error(), "immutable") {
			t.Errorf("%s: err = %v, want immutable refusal", sql, err)
		}
	}
	// Naming another tenant for deletion does not unlock this one.
	tx, err := conn.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, `SELECT set_config('compliance.tenant_delete', 'other', true)`); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, `DELETE FROM ce_report_files`); err == nil {
		t.Error("deleting another tenant's report files was allowed")
	}
	_ = tx.Rollback(ctx)
	got, err := s.Report(ctx, auditScope, "rpt-1")
	if err != nil || string(got.Facts) != storetest.ReportFacts {
		t.Fatalf("report = %s %v", got.Facts, err)
	}
	if f, err := s.ReportFile(ctx, auditScope, "rpt-1", "report.pdf"); err != nil || string(f.Data) != "%PDF-1.7\n" {
		t.Fatalf("file = %q %v", f.Data, err)
	}
	if c, err := s.ApplyRetention(ctx, auditScope, store.RetentionDeletion{Reports: []string{"rpt-1"}}); err != nil || c.Reports != 1 {
		t.Fatalf("retention through the store = %+v %v", c, err)
	}
}
