// SPDX-License-Identifier: Apache-2.0

package dataset

import (
	"bytes"
	"fmt"

	"github.com/xuri/excelize/v2"

	"github.com/nexops-one/compliance-engine/pkg/importer"
	"github.com/nexops-one/compliance-engine/sdk/schema"
)

// generated is the fixed date written into the workbook properties, so that
// the file is byte for byte reproducible.
const generated = "2026-10-01T00:00:00Z"

// Workbook fills the import template of schema s with the dataset. The
// README sheet and the document properties are marked as sample data.
func Workbook(s *schema.Schema) ([]byte, error) {
	tpl, err := importer.XLSXTemplate(s)
	if err != nil {
		return nil, err
	}
	f, err := excelize.OpenReader(bytes.NewReader(tpl))
	if err != nil {
		return nil, err
	}
	defer f.Close()
	d := Build()
	for _, name := range s.EntityNames() {
		e, _ := s.Entity(name)
		cols := importer.Columns(e)
		known := map[string]bool{}
		for _, c := range cols {
			known[c] = true
		}
		for i, row := range d[name] {
			for k := range row {
				if !known[k] {
					return nil, fmt.Errorf("dataset: %s has no column %q", name, k)
				}
			}
			vals := make([]any, len(cols))
			for j, c := range cols {
				vals[j] = row[c]
			}
			cell, _ := excelize.CoordinatesToCellName(1, i+2)
			if err := f.SetSheetRow(name, cell, &vals); err != nil {
				return nil, err
			}
		}
	}
	if err := f.InsertRows(importer.ReadmeSheet, 1, 2); err != nil {
		return nil, err
	}
	if err := f.SetCellValue(importer.ReadmeSheet, "A1", Marker+": Demo Bank S.A. (fictitious). Every name and identifier is invented; results are a demonstration, not a compliance status."); err != nil {
		return nil, err
	}
	if err := f.SetDocProps(&excelize.DocProperties{
		Title:          "SAMPLE DATA: register of information of Demo Bank S.A. (fictitious)",
		Subject:        Marker,
		Creator:        "compliance-engine demo generator",
		LastModifiedBy: "compliance-engine demo generator",
		Description:    "Fictitious sample register with deliberate faults, for the compliance-engine demo. Not real data.",
		Keywords:       "SAMPLE DATA",
		Created:        generated,
		Modified:       generated,
	}); err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	if err := f.Write(&buf); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
