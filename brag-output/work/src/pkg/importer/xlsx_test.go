// SPDX-License-Identifier: Apache-2.0

package importer_test

import (
	"bytes"
	"testing"

	"github.com/xuri/excelize/v2"

	"github.com/nexops-one/compliance-engine/pkg/importer"
	"github.com/nexops-one/compliance-engine/sdk/adapter"
)

// workbook fills the official template: rows maps an entity to rows of column -> value.
func workbook(t *testing.T, rows map[string][]map[string]string, extraSheet string) []byte {
	t.Helper()
	s := latest(t)
	tpl, err := importer.XLSXTemplate(s)
	if err != nil {
		t.Fatal(err)
	}
	f, err := excelize.OpenReader(bytes.NewReader(tpl))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	for entity, list := range rows {
		e, _ := s.Entity(entity)
		cols := importer.Columns(e)
		for i, values := range list {
			row := make([]any, len(cols))
			for c, name := range cols {
				row[c] = values[name]
			}
			cell, _ := excelize.CoordinatesToCellName(1, i+2)
			if err := f.SetSheetRow(entity, cell, &row); err != nil {
				t.Fatal(err)
			}
		}
	}
	if extraSheet != "" {
		if _, err := f.NewSheet(extraSheet); err != nil {
			t.Fatal(err)
		}
	}
	var buf bytes.Buffer
	if err := f.Write(&buf); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestParseXLSX(t *testing.T) {
	data := workbook(t, map[string][]map[string]string{
		"ict_provider": {{"provider_id_code": "0001", "legal_name": "One Ltd"}, {"provider_id_code": "P2", "legal_name": "Two Inc"}},
		"function":     {{"function_id": "F1", "financial_entity_lei": "SAMPLEFE000000000021", "rto": "240"}},
	}, "_notes")
	p, err := importer.Parse(latest(t), importer.Input{Name: "register.xlsx", Data: data})
	if err != nil {
		t.Fatal(err)
	}
	if p.Format != importer.FormatXLSX || p.Rows != 3 || p.Batch.Source.Adapter != "xlsx-import" || p.Manifest.Name != "xlsx-import" {
		t.Fatalf("parsed = %+v", p)
	}
	providers := p.Batch.Entities["ict_provider"]
	if len(providers) != 2 || providers[0]["provider_id_code"] != "0001" {
		t.Fatalf("leading zeros must survive: %#v", providers)
	}
	if got := adapter.MetaOf(providers[1]).SourceRecordRef; got != "ict_provider:3" {
		t.Fatalf("row provenance = %q", got)
	}
	if len(p.Manifest.Supplies) != len(latest(t).EntityNames()) {
		t.Fatal("the workbook manifest must supply every entity")
	}
	if _, ok := p.Batch.Entities["branch"]; ok {
		t.Fatal("empty sheets must not produce records")
	}
}

func TestParseXLSXRejectsBadWorkbooks(t *testing.T) {
	s := latest(t)
	_, err := importer.Parse(s, importer.Input{Name: "register.xlsx", Data: []byte("not a workbook")})
	if got := fileErrorCode(t, err); got != "invalid_xlsx" {
		t.Fatalf("garbage = %s", got)
	}
	_, err = importer.Parse(s, importer.Input{Name: "register.xlsx", Data: workbook(t, nil, "vendors")})
	if got := fileErrorCode(t, err); got != "unknown_sheet" {
		t.Fatalf("unknown sheet = %s", got)
	}
}

// A small upload must not expand into gigabytes of XML (zip bomb).
func TestParseXLSXBoundsUnzippedSize(t *testing.T) {
	data := workbook(t, nil, "")
	_, err := importer.Parse(latest(t), importer.Input{Name: "register.xlsx", Data: data, MaxUnzippedBytes: 4096})
	if got := fileErrorCode(t, err); got != "file_too_large" {
		t.Fatalf("code = %s (%v)", got, err)
	}
	if _, err := importer.Parse(latest(t), importer.Input{Name: "register.xlsx", Data: data}); err != nil {
		t.Fatalf("the default limit must accept the template: %v", err)
	}
}
