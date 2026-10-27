// SPDX-License-Identifier: Apache-2.0

package importer_test

import (
	"bytes"
	"encoding/csv"
	"errors"
	"reflect"
	"testing"

	"github.com/xuri/excelize/v2"

	"github.com/nexops-one/compliance-engine/pkg/importer"
	"github.com/nexops-one/compliance-engine/sdk/schema"
)

func latest(t *testing.T) *schema.Schema {
	t.Helper()
	reg, err := schema.Default()
	if err != nil {
		t.Fatal(err)
	}
	return reg.Latest()
}

func TestColumns(t *testing.T) {
	e, _ := latest(t).Entity("arrangement_service_line")
	cols := importer.Columns(e)
	if !reflect.DeepEqual(cols[:5], e.IdentityKey) {
		t.Fatalf("identity fields must come first: %v", cols[:5])
	}
	if cols[len(cols)-2] != importer.NotApplicableColumn || cols[len(cols)-1] != importer.SourceRefColumn {
		t.Fatalf("reserved columns must come last: %v", cols[len(cols)-2:])
	}
	if len(cols) != len(e.FieldNames)+2 {
		t.Fatalf("every field must have a column: %d vs %d", len(cols), len(e.FieldNames)+2)
	}
}

func TestCSVTemplate(t *testing.T) {
	s := latest(t)
	data, err := importer.CSVTemplate(s, "ict_provider")
	if err != nil {
		t.Fatal(err)
	}
	rows, err := csv.NewReader(bytes.NewReader(data)).ReadAll()
	e, _ := s.Entity("ict_provider")
	if err != nil || len(rows) != 1 || !reflect.DeepEqual(rows[0], importer.Columns(e)) {
		t.Fatalf("template = %q, %v", data, err)
	}
	_, err = importer.CSVTemplate(s, "nope")
	var fe *importer.FileError
	if !errors.As(err, &fe) || !errors.Is(err, importer.ErrInvalidFile) || fe.Errors[0].Code != "unknown_entity" {
		t.Fatalf("unknown entity err = %v", err)
	}
}

func TestXLSXTemplate(t *testing.T) {
	s := latest(t)
	data, err := importer.XLSXTemplate(s)
	if err != nil {
		t.Fatal(err)
	}
	f, err := excelize.OpenReader(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	want := append([]string{importer.ReadmeSheet}, s.EntityNames()...)
	if !reflect.DeepEqual(f.GetSheetList(), want) {
		t.Fatalf("sheets = %v", f.GetSheetList())
	}
	e, _ := s.Entity("ict_provider")
	rows, _ := f.GetRows("ict_provider")
	if len(rows) != 1 || !reflect.DeepEqual(rows[0], importer.Columns(e)) {
		t.Fatalf("ict_provider header = %v", rows)
	}
	styleID, err := f.GetColStyle("ict_provider", "A")
	if err != nil {
		t.Fatal(err)
	}
	style, err := f.GetStyle(styleID)
	if err != nil || style.NumFmt != 49 {
		t.Fatalf("template columns must be text-formatted: %+v, %v", style, err)
	}
	readme, _ := f.GetRows(importer.ReadmeSheet)
	found := false
	for _, r := range readme {
		if len(r) > 2 && r[0] == "arrangement_service_line" && r[1] == "data_at_rest_country" && r[2] == "B_02.02.0150" {
			found = true
		}
	}
	if !found || readme[0][0] != "entity" {
		t.Fatalf("README must document every column; first row %v", readme[0])
	}
}

func registry(t *testing.T) *schema.Registry {
	t.Helper()
	reg, err := schema.Default()
	if err != nil {
		t.Fatal(err)
	}
	return reg
}
