// SPDX-License-Identifier: Apache-2.0

package sealed_test

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/nexops-one/compliance-engine/pkg/store"
	"github.com/nexops-one/compliance-engine/pkg/store/memory"
	"github.com/nexops-one/compliance-engine/pkg/store/storetest"
)

func TestSealedReportBodies(t *testing.T) {
	at := time.Date(2026, 9, 30, 9, 0, 0, 0, time.UTC)
	check := func(t *testing.T, s store.Store, raw func() (facts, file []byte)) {
		t.Helper()
		r, files := storetest.ReportDoc(scope, "rpt-1", at)
		if err := s.SaveReport(ctx, r, files); err != nil {
			t.Fatal(err)
		}
		facts, file := raw()
		if bytes.Contains(facts, []byte("Ünïcode")) || !strings.HasPrefix(string(facts), "v1:") {
			t.Errorf("facts stored in plaintext: %.40s", facts)
		}
		if bytes.Contains(file, []byte("%PDF")) || !strings.HasPrefix(string(file), "v1:") {
			t.Errorf("file stored in plaintext: %.40s", file)
		}
		got, err := s.Report(ctx, scope, "rpt-1")
		if err != nil || string(got.Facts) != storetest.ReportFacts || string(got.Validation) != string(r.Validation) {
			t.Fatalf("decrypted report = %s %s %v", got.Facts, got.Validation, err)
		}
		f, err := s.ReportFile(ctx, scope, "rpt-1", "report.pdf")
		if err != nil || !bytes.Equal(f.Data, files[0].Data) || f.Size != int64(len(files[0].Data)) {
			t.Fatalf("decrypted file = %q %d %v", f.Data, f.Size, err)
		}
	}
	t.Run("memory", func(t *testing.T) {
		inner := memory.New()
		s, _ := wrap(t, inner)
		check(t, s, func() ([]byte, []byte) {
			r, _ := inner.Report(ctx, scope, "rpt-1")
			f, _ := inner.ReportFile(ctx, scope, "rpt-1", "report.pdf")
			return r.Facts, f.Data
		})
	})
	t.Run("postgres", func(t *testing.T) {
		pg := openPG(t)
		s, _ := wrap(t, pg)
		check(t, s, func() ([]byte, []byte) {
			conn, err := pgx.Connect(ctx, pgURLs[pg])
			if err != nil {
				t.Fatal(err)
			}
			defer conn.Close(ctx)
			var facts, file []byte
			if err := conn.QueryRow(ctx, `SELECT r.facts, f.content FROM ce_reports r JOIN ce_report_files f
  ON f.tenant_id = r.tenant_id AND f.workspace_id = r.workspace_id AND f.report_id = r.id WHERE r.id = 'rpt-1'`).Scan(&facts, &file); err != nil {
				t.Fatal(err)
			}
			return facts, file
		})
	})
}
