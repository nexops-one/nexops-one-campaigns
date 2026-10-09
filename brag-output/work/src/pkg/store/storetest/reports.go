// SPDX-License-Identifier: Apache-2.0

package storetest

import (
	"bytes"
	"encoding/json"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/nexops-one/compliance-engine/pkg/store"
	"github.com/nexops-one/compliance-engine/sdk/adapter"
)

// ReportFacts are deliberately not in canonical JSON form: stores must keep
// facts byte for byte because the facts hash is computed over them.
const ReportFacts = `{"z": 1,  "a": [ "keep", "spacing" ], "text": "Ünïcode"}`

// ReportDoc builds a stored report of scope with one PDF file.
func ReportDoc(scope adapter.Scope, id string, created time.Time) (store.Report, []store.ReportFile) {
	file := store.ReportFile{ReportFileMeta: store.ReportFileMeta{Name: "report.pdf", ContentType: "application/pdf", Size: 9, SHA256: "sha256:ab"},
		Data: []byte("%PDF-1.7\n")}
	r := store.Report{
		ID: id, Scope: scope, Profile: "profile_b", CreatedAt: created, CreatedBy: "user:u1", CreatedByKind: "user",
		EvaluationID: "eval-1", SnapshotID: "rev-1", Catalogs: []string{"dora@0.1.1"}, AsOf: created, Complete: true,
		FactsHash: "sha256:00", Files: []store.ReportFileMeta{file.ReportFileMeta},
		Inputs: json.RawMessage(`{"supplied":["ict_provider.legal_name"]}`), Facts: json.RawMessage(ReportFacts),
		Validation: json.RawMessage(`{"errors": []}`),
	}
	return r, []store.ReportFile{file}
}

func auditLen(t *testing.T, s store.Store, scope adapter.Scope) int {
	t.Helper()
	evs, err := s.AuditEvents(ctx, scope, store.AuditQuery{})
	if err != nil {
		t.Fatal(err)
	}
	return len(evs)
}

func runReports(t *testing.T, newStore func(t *testing.T) store.Store) {
	t.Run("reports are stored byte for byte and immutable", func(t *testing.T) {
		s := newStore(t)
		r, files := ReportDoc(scopeA, "rpt-1", at)
		if err := s.SaveReport(ctx, r, files, Event(scopeA, "report.generate")); err != nil {
			t.Fatal(err)
		}
		got, err := s.Report(ctx, scopeA, "rpt-1")
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(got.Facts, []byte(ReportFacts)) {
			t.Fatalf("facts = %s", got.Facts)
		}
		var inputs, validation map[string]any
		if json.Unmarshal(got.Inputs, &inputs) != nil || json.Unmarshal(got.Validation, &validation) != nil || inputs["supplied"] == nil {
			t.Fatalf("inputs %s validation %s", got.Inputs, got.Validation)
		}
		got.Facts, got.Inputs, got.Validation = r.Facts, r.Inputs, r.Validation
		if !got.CreatedAt.Equal(r.CreatedAt) || !got.AsOf.Equal(r.AsOf) {
			t.Fatalf("times = %v %v", got.CreatedAt, got.AsOf)
		}
		got.CreatedAt, got.AsOf = r.CreatedAt, r.AsOf
		if !reflect.DeepEqual(got, r) {
			t.Fatalf("report = %+v\nwant %+v", got, r)
		}
		if n := auditLen(t, s, scopeA); n != 1 {
			t.Fatalf("audit events = %d", n)
		}
		r.FactsHash = "sha256:ff"
		if err := s.SaveReport(ctx, r, files, Event(scopeA, "report.generate")); !errors.Is(err, store.ErrExists) {
			t.Fatalf("reused ID = %v", err)
		}
		if n := auditLen(t, s, scopeA); n != 1 {
			t.Fatalf("a refused save appended its event: %d", n)
		}
		if again, _ := s.Report(ctx, scopeA, "rpt-1"); again.FactsHash != "sha256:00" {
			t.Fatal("a refused save changed the stored report")
		}
		f, err := s.ReportFile(ctx, scopeA, "rpt-1", "report.pdf")
		if err != nil || !bytes.Equal(f.Data, files[0].Data) || f.ContentType != "application/pdf" || f.SHA256 != "sha256:ab" || f.Size != 9 {
			t.Fatalf("file = %+v %v", f, err)
		}
		if _, err := s.ReportFile(ctx, scopeA, "rpt-1", "report.csv"); !errors.Is(err, store.ErrNotFound) {
			t.Fatalf("unknown file = %v", err)
		}
		if _, err := s.Report(ctx, scopeB, "rpt-1"); !errors.Is(err, store.ErrNotFound) {
			t.Fatalf("other workspace = %v", err)
		}
		if _, err := s.ReportFile(ctx, scopeB, "rpt-1", "report.pdf"); !errors.Is(err, store.ErrNotFound) {
			t.Fatalf("other workspace file = %v", err)
		}
	})

	t.Run("reports list newest first without bodies", func(t *testing.T) {
		s := newStore(t)
		for i, id := range []string{"rpt-b", "rpt-a", "rpt-c"} {
			r, files := ReportDoc(scopeA, id, at.Add(time.Duration(min(i, 1))*time.Hour))
			if err := s.SaveReport(ctx, r, files); err != nil {
				t.Fatal(err)
			}
		}
		list, err := s.Reports(ctx, scopeA)
		if err != nil {
			t.Fatal(err)
		}
		var ids []string
		for _, r := range list {
			ids = append(ids, r.ID)
			if r.Facts != nil || r.Inputs != nil || r.Validation != nil || len(r.Files) != 1 {
				t.Fatalf("listed report carries bodies: %+v", r)
			}
		}
		if !reflect.DeepEqual(ids, []string{"rpt-a", "rpt-c", "rpt-b"}) {
			t.Fatalf("order = %v", ids)
		}
		refs, err := s.ReportRefs(ctx, scopeA)
		if err != nil || len(refs) != 3 || refs[0].ID != "rpt-a" || refs[0].EvaluationID != "eval-1" || refs[0].SnapshotID != "rev-1" {
			t.Fatalf("refs = %+v %v", refs, err)
		}
		if empty, _ := s.Reports(ctx, scopeB); len(empty) != 0 {
			t.Fatalf("other workspace lists %v", empty)
		}
	})

	t.Run("retention and tenant deletion remove reports", func(t *testing.T) {
		s := newStore(t)
		other := adapter.Scope{TenantID: "tenant-b", WorkspaceID: "ws-1"}
		for _, sc := range []adapter.Scope{scopeA, other} {
			for _, id := range []string{"rpt-1", "rpt-2"} {
				r, files := ReportDoc(sc, id, at)
				if err := s.SaveReport(ctx, r, files); err != nil {
					t.Fatal(err)
				}
			}
		}
		c, err := s.ApplyRetention(ctx, scopeA, store.RetentionDeletion{Reports: []string{"rpt-1"}}, Event(scopeA, "retention.run"))
		if err != nil || c.Reports != 1 {
			t.Fatalf("retention = %+v %v", c, err)
		}
		if _, err := s.Report(ctx, scopeA, "rpt-1"); !errors.Is(err, store.ErrNotFound) {
			t.Fatalf("deleted report = %v", err)
		}
		if _, err := s.ReportFile(ctx, scopeA, "rpt-1", "report.pdf"); !errors.Is(err, store.ErrNotFound) {
			t.Fatalf("deleted report file = %v", err)
		}
		if _, err := s.Report(ctx, other, "rpt-1"); err != nil {
			t.Fatalf("retention reached another tenant: %v", err)
		}
		if _, err := s.DeleteTenantData(ctx, "tenant-a", Event(adapter.Scope{TenantID: "tenant-a"}, "tenant.delete")); err != nil {
			t.Fatal(err)
		}
		if list, _ := s.Reports(ctx, scopeA); len(list) != 0 {
			t.Fatalf("reports remain after tenant deletion: %v", list)
		}
		if list, _ := s.Reports(ctx, other); len(list) != 2 {
			t.Fatalf("other tenant's reports = %v", list)
		}
	})
}
