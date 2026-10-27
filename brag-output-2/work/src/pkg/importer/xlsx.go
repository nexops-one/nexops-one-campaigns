// SPDX-License-Identifier: Apache-2.0

package importer

import (
	"archive/zip"
	"bytes"
	"fmt"
	"strings"

	"github.com/xuri/excelize/v2"

	"github.com/nexops-one/compliance-engine/sdk/schema"
)

// DefaultMaxUnzippedBytes bounds the decompressed size of an imported workbook.
const DefaultMaxUnzippedBytes = 256 << 20

// readXLSX returns one table per entity sheet. README and sheets whose name
// starts with "_" are ignored; any other sheet must be a canonical entity.
func readXLSX(s *schema.Schema, data []byte, limit int64) (map[string][][]string, error) {
	if limit <= 0 {
		limit = DefaultMaxUnzippedBytes
	}
	// A workbook is a zip archive: bound what it expands to before parsing it.
	// archive/zip fails when an entry holds more than its declared size.
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, fileErr("", "invalid_xlsx", "the file is not a readable .xlsx workbook")
	}
	var total uint64
	for _, zf := range zr.File {
		total += zf.UncompressedSize64
	}
	if total > uint64(limit) {
		return nil, fileErr("", "file_too_large", fmt.Sprintf("the workbook expands to %d bytes, more than the %d-byte limit", total, limit))
	}
	f, err := excelize.OpenReader(bytes.NewReader(data), excelize.Options{UnzipSizeLimit: limit, UnzipXMLSizeLimit: min(limit, 16<<20)})
	if err != nil {
		return nil, fileErr("", "invalid_xlsx", "the file is not a readable .xlsx workbook")
	}
	defer f.Close()
	tables := map[string][][]string{}
	var errs []schema.FieldError
	for _, sheet := range f.GetSheetList() {
		if sheet == ReadmeSheet || strings.HasPrefix(sheet, "_") {
			continue
		}
		if _, ok := s.Entity(sheet); !ok {
			errs = append(errs, schema.FieldError{Entity: sheet, Index: -1, Code: "unknown_sheet",
				Message: fmt.Sprintf("sheet %q is not a canonical entity (prefix its name with _ to keep it in the workbook)", sheet)})
			continue
		}
		rows, err := f.GetRows(sheet)
		if err != nil {
			errs = append(errs, schema.FieldError{Entity: sheet, Index: -1, Code: "invalid_xlsx", Message: err.Error()})
			continue
		}
		tables[sheet] = rows
	}
	if len(errs) > 0 {
		return nil, &FileError{Errors: errs}
	}
	return tables, nil
}
