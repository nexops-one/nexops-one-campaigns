// SPDX-License-Identifier: Apache-2.0

package importer_test

import (
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/nexops-one/compliance-engine/pkg/importer"
	"github.com/nexops-one/compliance-engine/sdk/adapter"
)

func parseCSV(t *testing.T, name, data string, mode adapter.Mode) importer.Parsed {
	t.Helper()
	p, err := importer.Parse(latest(t), importer.Input{Name: name, Data: []byte(data), Mode: mode})
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func fileErrorCode(t *testing.T, err error) string {
	t.Helper()
	var fe *importer.FileError
	if !errors.As(err, &fe) || !errors.Is(err, importer.ErrInvalidFile) {
		t.Fatalf("want *FileError, got %v", err)
	}
	return fe.Errors[0].Code
}

func TestParseCSVTypedValues(t *testing.T) {
	p := parseCSV(t, "function.csv", "function_id,financial_entity_lei,rto,rpo,function_name\nF1,SAMPLEFE000000000021,240,60,Valuation\nF2,SAMPLEFE000000000021,many,,\n", "")
	recs := p.Batch.Entities["function"]
	if p.Rows != 2 || len(recs) != 2 {
		t.Fatalf("rows = %d, records = %d", p.Rows, len(recs))
	}
	if recs[0]["rto"] != json.Number("240") || recs[0]["function_name"] != "Valuation" {
		t.Fatalf("typed values = %#v", recs[0])
	}
	if recs[1]["rto"] != "many" {
		t.Fatalf("an unconvertible cell must keep its text for L1 to reject, got %#v", recs[1]["rto"])
	}
	if _, ok := recs[1]["rpo"]; ok {
		t.Fatal("an empty cell must be absent, not an empty value")
	}
	if got := adapter.MetaOf(recs[1]).SourceRecordRef; got != "function:3" {
		t.Fatalf("row provenance = %q", got)
	}
	sa := parseCSV(t, "service_assessment.csv", "arrangement_ref,provider_id_code,ict_service_type,exit_plan_exists,alternatives_identified\nA1,P1,S1,Yes,0\n", "")
	if r := sa.Batch.Entities["service_assessment"][0]; r["exit_plan_exists"] != true || r["alternatives_identified"] != false {
		t.Fatalf("booleans = %#v", r)
	}
	cr := parseCSV(t, "cloud_resource.csv", "resource_ref,sovereignty_indicators\nr1,\"{\"\"provider_hq_country\"\":\"\"IE\"\"}\"\n", "")
	if si, ok := cr.Batch.Entities["cloud_resource"][0]["sovereignty_indicators"].(map[string]any); !ok || si["provider_hq_country"] != "IE" {
		t.Fatalf("object = %#v", cr.Batch.Entities["cloud_resource"][0])
	}
}

func TestParseCSVExcelExport(t *testing.T) {
	p := parseCSV(t, "ict_provider.csv", "\xef\xbb\xbfprovider_id_code;legal_name;hq_country\r\nP1;One Ltd;IE\r\n\r\nP2;Two Inc;US\r\n", "")
	recs := p.Batch.Entities["ict_provider"]
	if len(recs) != 2 || recs[0]["provider_id_code"] != "P1" || recs[1]["legal_name"] != "Two Inc" {
		t.Fatalf("records = %#v", recs)
	}
	if got := adapter.MetaOf(recs[1]).SourceRecordRef; got != "ict_provider:4" {
		t.Fatalf("row numbers must count blank lines: %q", got)
	}
}

func TestParseCSVReservedColumns(t *testing.T) {
	p := parseCSV(t, "ict_provider.csv", "provider_id_code,legal_name,_not_applicable,_source_record_ref\nP1,One,\"parent_id_code, parent_id_type\",crm-17\nP2,Two,,\n", "")
	recs := p.Batch.Entities["ict_provider"]
	m := adapter.MetaOf(recs[0])
	if m.SourceRecordRef != "crm-17" || !reflect.DeepEqual(m.NotApplicableFields, []string{"parent_id_code", "parent_id_type"}) {
		t.Fatalf("meta = %+v", m)
	}
	if adapter.MetaOf(recs[1]).SourceRecordRef != "ict_provider:3" {
		t.Fatal("the default source ref applies when the column is empty")
	}
}

func TestParseCSVExtraCellIsKept(t *testing.T) {
	p := parseCSV(t, "ict_provider.csv", "provider_id_code,legal_name\nP1,One,surprise\n", "")
	if p.Batch.Entities["ict_provider"][0]["_column_3"] != "surprise" {
		t.Fatalf("a value without a header must be kept for L1 to reject: %#v", p.Batch.Entities["ict_provider"][0])
	}
}

func TestParseBatchAndManifest(t *testing.T) {
	data := "provider_id_code,legal_name\nP1,One\n"
	p := parseCSV(t, "ict_provider.csv", data, adapter.ModeFull)
	b := p.Batch
	if !strings.HasPrefix(b.BatchID(), "sha256:") || b.Mode() != adapter.ModeFull || b.SchemaVersion != "0.1.0" {
		t.Fatalf("batch header = %+v %+v", b, b.Batch)
	}
	if b.Source != (adapter.Source{System: "file:ict_provider.csv", Adapter: "csv-import.ict_provider", AdapterVersion: importer.AdapterVersion}) {
		t.Fatalf("source = %+v", b.Source)
	}
	again, err := importer.Parse(latest(t), importer.Input{Name: "copy.csv", Entity: "ict_provider", Data: []byte(data)})
	if err != nil || again.Batch.BatchID() != b.BatchID() {
		t.Fatalf("the batch ID must depend on content only: %v", err)
	}
	m := p.Manifest
	e, _ := latest(t).Entity("ict_provider")
	if m.Name != "csv-import.ict_provider" || len(m.Supplies) != 1 || len(m.Supplies["ict_provider"]) != len(e.FieldNames) {
		t.Fatalf("manifest = %+v", m)
	}
	if err := m.Validate(registry(t)); err != nil {
		t.Fatalf("the importer manifest must be valid: %v", err)
	}
}

func TestParseRejectsBadFiles(t *testing.T) {
	s := latest(t)
	cases := map[string]importer.Input{
		"unsupported_format": {Name: "x.txt", Data: []byte("a")},
		"invalid_mode":       {Name: "ict_provider.csv", Data: []byte("provider_id_code\nP1\n"), Mode: "sometimes"},
		"unknown_entity":     {Name: "vendors.csv", Data: []byte("provider_id_code\nP1\n")},
		"invalid_csv":        {Name: "ict_provider.csv", Data: []byte("provider_id_code\n\"P1\n")},
		"unknown_column":     {Name: "ict_provider.csv", Data: []byte("provider_id_code,colour\nP1,red\n")},
		"duplicate_column":   {Name: "ict_provider.csv", Data: []byte("provider_id_code,legal_name,legal_name\nP1,a,b\n")},
		"missing_column":     {Name: "ict_provider.csv", Data: []byte("legal_name\nOne\n")},
	}
	for want, in := range cases {
		_, err := importer.Parse(s, in)
		if got := fileErrorCode(t, err); got != want {
			t.Errorf("%s: code = %s (%v)", want, got, err)
		}
	}
}

// Excel's default "CSV" format writes Windows-1252, not UTF-8: "Société" must
// be rejected with a clear message, never silently turned into U+FFFD.
func TestParseCSVRejectsNonUTF8(t *testing.T) {
	data := []byte("provider_id_code,legal_name\nP1,Soci\xe9t\xe9\n")
	_, err := importer.Parse(latest(t), importer.Input{Name: "ict_provider.csv", Data: data})
	if got := fileErrorCode(t, err); got != "invalid_encoding" {
		t.Fatalf("code = %s (%v)", got, err)
	}
}
