// SPDX-License-Identifier: Apache-2.0

package compliance_test

import (
	"bytes"
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/xuri/excelize/v2"

	"github.com/nexops-one/compliance-engine/pkg/catalog"
	"github.com/nexops-one/compliance-engine/pkg/compliance"
	"github.com/nexops-one/compliance-engine/pkg/engine"
	"github.com/nexops-one/compliance-engine/pkg/extension"
	"github.com/nexops-one/compliance-engine/pkg/importer"
	"github.com/nexops-one/compliance-engine/sdk/adapter"
)

const providersCSV = "provider_id_code,provider_id_type,legal_name,person_type,hq_country,_not_applicable\n" +
	"P1,LEI,One Ltd,LEGAL,IE,parent_id_code\n" +
	"P2,LEI,Two Inc,LEGAL,US,\n"

func csvInput(name, data string, mode adapter.Mode) importer.Input {
	return importer.Input{Name: name, Data: []byte(data), Mode: mode}
}

func mustImport(t *testing.T, eng *compliance.Engine, in importer.Input) compliance.ImportResult {
	t.Helper()
	res, err := eng.Import(ctx, scopeA, in, false)
	if err != nil {
		t.Fatal(err)
	}
	return res
}

func snapshotKeys(t *testing.T, eng *compliance.Engine) []string {
	t.Helper()
	snap, err := eng.Snapshot(ctx, scopeA, "")
	if err != nil {
		t.Fatal(err)
	}
	keys := []string{}
	for _, r := range snap.Records {
		keys = append(keys, r.Entity+"|"+r.Key)
	}
	return keys
}

func TestImportDryRunThenCommit(t *testing.T) {
	eng := newEngine(t)
	dry, err := eng.Import(ctx, scopeA, csvInput("ict_provider.csv", providersCSV, ""), true)
	if err != nil {
		t.Fatal(err)
	}
	if !dry.Result.DryRun || dry.Result.Created != 2 || dry.Completeness == nil || dry.Rows != 2 ||
		dry.Adapter != "csv-import.ict_provider" || dry.Format != importer.FormatCSV || dry.File != "ict_provider.csv" {
		t.Fatalf("dry run = %+v", dry)
	}
	if revs, _ := eng.Snapshots(ctx, scopeA); len(revs) != 0 {
		t.Fatal("a dry run must not commit")
	}
	res := mustImport(t, eng, csvInput("ict_provider.csv", providersCSV, ""))
	if res.Result.Created != 2 || res.Result.SnapshotID != "rev-1" || res.Completeness != nil {
		t.Fatalf("commit = %+v", res)
	}
	snap, _ := eng.Snapshot(ctx, scopeA, "")
	m := adapter.MetaOf(snap.Records[0].Data)
	if m.SourceRecordRef != "ict_provider:2" || !reflect.DeepEqual(m.NotApplicableFields, []string{"parent_id_code"}) {
		t.Fatalf("row provenance and not-applicable fields must be stored: %+v", m)
	}
}

func TestReimportIsNoOp(t *testing.T) {
	eng := newEngine(t)
	mustImport(t, eng, csvInput("ict_provider.csv", providersCSV, ""))
	again := mustImport(t, eng, csvInput("ict_provider.csv", providersCSV, ""))
	renamed := mustImport(t, eng, importer.Input{Name: "providers-copy.csv", Entity: "ict_provider", Data: []byte(providersCSV)})
	if !again.Result.NoChanges || !renamed.Result.NoChanges {
		t.Fatalf("again = %+v renamed = %+v", again.Result, renamed.Result)
	}
	if revs, _ := eng.Snapshots(ctx, scopeA); len(revs) != 1 {
		t.Fatalf("re-importing the same content must not create snapshots: %d", len(revs))
	}
}

func TestImportPartialFile(t *testing.T) {
	eng := newEngine(t)
	data := "function_id,financial_entity_lei,function_name,rto\n" +
		"F1,SAMPLEFE000000000021,Valuation,240\n" +
		"F2,SAMPLEFE000000000021,Payments,many\n"
	res := mustImport(t, eng, csvInput("function.csv", data, ""))
	if res.Result.Accepted != 1 || res.Result.RejectedRecords != 1 || len(res.Result.Errors) != 1 {
		t.Fatalf("result = %+v", res.Result)
	}
	e := res.Result.Errors[0]
	if e.Field != "rto" || e.Code != "type" || e.SourceRecordRef != "function:3" {
		t.Fatalf("the bad row must be reported with its location: %+v", e)
	}
	ev, err := eng.Evaluate(ctx, scopeA, "", []catalog.Ref{{Catalog: "dora", Version: "1.0.0"}})
	if err != nil {
		t.Fatal(err)
	}
	if c := controls(ev)["dora-function-criticality"]; c.Status != engine.StatusNotAssessed {
		t.Fatalf("gaps must stay visible as not_assessed: %+v", c)
	}
}

func TestFullCSVImportOnlyReplacesItsEntity(t *testing.T) {
	eng := newEngine(t)
	mustImport(t, eng, csvInput("function.csv", "function_id,financial_entity_lei\nF1,SAMPLEFE000000000021\n", adapter.ModeFull))
	mustImport(t, eng, csvInput("ict_provider.csv", providersCSV, adapter.ModeFull))
	res := mustImport(t, eng, csvInput("ict_provider.csv", "provider_id_code,legal_name\nP1,One Ltd\n", adapter.ModeFull))
	if res.Result.Deleted != 1 {
		t.Fatalf("result = %+v", res.Result)
	}
	want := []string{`function|["F1","SAMPLEFE000000000021"]`, `ict_provider|["P1"]`}
	if got := snapshotKeys(t, eng); !reflect.DeepEqual(got, want) {
		t.Fatalf("records = %v", got)
	}
}

func setCell(t *testing.T, f *excelize.File, eng *compliance.Engine, entity, column string, row int, value string) {
	t.Helper()
	e, _ := eng.Schema().Entity(entity)
	for i, c := range importer.Columns(e) {
		if c == column {
			cell, _ := excelize.CoordinatesToCellName(i+1, row)
			if err := f.SetCellValue(entity, cell, value); err != nil {
				t.Fatal(err)
			}
			return
		}
	}
	t.Fatalf("no column %s.%s", entity, column)
}

func TestImportXLSXWorkbook(t *testing.T) {
	eng := newEngine(t)
	tpl, err := importer.XLSXTemplate(eng.Schema())
	if err != nil {
		t.Fatal(err)
	}
	f, err := excelize.OpenReader(bytes.NewReader(tpl))
	if err != nil {
		t.Fatal(err)
	}
	setCell(t, f, eng, "ict_provider", "provider_id_code", 2, "P1")
	setCell(t, f, eng, "ict_provider", "legal_name", 2, "One Ltd")
	setCell(t, f, eng, "ict_provider", "provider_id_code", 3, "P2")
	setCell(t, f, eng, "function", "function_id", 2, "F1")
	setCell(t, f, eng, "function", "financial_entity_lei", 2, "SAMPLEFE000000000021")
	var buf bytes.Buffer
	if err := f.Write(&buf); err != nil {
		t.Fatal(err)
	}
	res := mustImport(t, eng, importer.Input{Name: "register.xlsx", Data: buf.Bytes()})
	if res.Result.Created != 3 || res.Adapter != "xlsx-import" || res.Format != importer.FormatXLSX {
		t.Fatalf("result = %+v", res)
	}
}

func TestImportRejectsUnreadableFiles(t *testing.T) {
	eng := newEngine(t)
	for _, in := range []importer.Input{
		csvInput("notes.txt", "x", ""),
		csvInput("ict_provider.csv", "provider_id_code,colour\nP1,red\n", ""),
		csvInput("register.xlsx", "not a workbook", ""),
	} {
		if _, err := eng.Import(ctx, scopeA, in, false); !errors.Is(err, importer.ErrInvalidFile) {
			t.Errorf("%s: err = %v", in.Name, err)
		}
	}
	if revs, _ := eng.Snapshots(ctx, scopeA); len(revs) != 0 {
		t.Fatal("rejected files must not commit anything")
	}
}

func TestImportRollback(t *testing.T) {
	eng := newEngine(t)
	res := mustImport(t, eng, csvInput("ict_provider.csv", providersCSV, ""))
	rb, err := eng.Rollback(ctx, scopeA, res.Result.IngestionID)
	if err != nil || rb.SnapshotID != "rev-2" || rb.Deleted != 2 {
		t.Fatalf("rollback = %+v, %v", rb, err)
	}
	if got := snapshotKeys(t, eng); len(got) != 0 {
		t.Fatalf("records after rollback = %v", got)
	}
}

type denyFeature extension.Feature

func (d denyFeature) Allowed(_ context.Context, _ adapter.Scope, f extension.Feature) extension.Decision {
	if f == extension.Feature(d) {
		return extension.Decision{Reason: extension.ReasonAddonDisabled}
	}
	return extension.Decision{Allowed: true}
}

func TestImportRequiresImportEntitlement(t *testing.T) {
	eng := newEngine(t, func(c *compliance.Config) { c.Entitlements = denyFeature(extension.FeatureRegisterImport) })
	_, err := eng.Import(ctx, scopeA, csvInput("ict_provider.csv", providersCSV, ""), true)
	var ne *extension.NotEntitledError
	if !errors.As(err, &ne) || ne.Feature != extension.FeatureRegisterImport {
		t.Fatalf("err = %v", err)
	}
}
