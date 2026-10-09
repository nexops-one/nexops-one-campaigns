// SPDX-License-Identifier: Apache-2.0

// Package importer turns CSV and XLSX files into canonical batches and
// generates the matching templates from the canonical schema. It performs no
// I/O beyond the bytes it is given and never drops a value: cells that cannot
// be converted keep their text so record validation rejects them visibly.
package importer

import (
	"bytes"
	"encoding/csv"
	"errors"
	"fmt"

	"github.com/xuri/excelize/v2"

	"github.com/nexops-one/compliance-engine/sdk/schema"
)

const (
	// ReadmeSheet is the workbook sheet documenting every column; it is ignored on import.
	ReadmeSheet = "README"
	// NotApplicableColumn lists, comma-separated, fields that do not apply to the row.
	NotApplicableColumn = "_not_applicable"
	// SourceRefColumn optionally holds the row's identifier in the source system.
	SourceRefColumn = "_source_record_ref"
)

// ErrInvalidFile is matched by every *FileError.
var ErrInvalidFile = errors.New("invalid import file")

// FileError rejects a whole file (unreadable, unknown entity, bad header).
type FileError struct {
	Errors []schema.FieldError `json:"errors"`
}

func (e *FileError) Error() string {
	return fmt.Sprintf("invalid import file: %s", e.Errors[0].Message)
}

// Is reports whether target is ErrInvalidFile.
func (e *FileError) Is(target error) bool { return target == ErrInvalidFile }

func fileErr(entity, code, message string) *FileError {
	return &FileError{Errors: []schema.FieldError{{Entity: entity, Index: -1, Code: code, Message: message}}}
}

// Columns returns the template columns of an entity: identity fields in
// x-identity-key order, the other canonical fields sorted, then the reserved
// columns.
func Columns(e *schema.Entity) []string {
	cols := append([]string(nil), e.IdentityKey...)
	identity := map[string]bool{}
	for _, k := range e.IdentityKey {
		identity[k] = true
	}
	for _, f := range e.FieldNames {
		if !identity[f] {
			cols = append(cols, f)
		}
	}
	return append(cols, NotApplicableColumn, SourceRefColumn)
}

// CSVTemplate returns a header-only CSV template for entity.
func CSVTemplate(s *schema.Schema, entity string) ([]byte, error) {
	e, ok := s.Entity(entity)
	if !ok {
		return nil, fileErr(entity, "unknown_entity", fmt.Sprintf("%q is not an entity of canonical schema %s", entity, s.Version))
	}
	var buf bytes.Buffer
	w := csv.NewWriter(&buf)
	if err := w.Write(Columns(e)); err != nil {
		return nil, err
	}
	w.Flush()
	return buf.Bytes(), w.Error()
}

func yesNo(b bool) string {
	if b {
		return "yes"
	}
	return "no"
}

// XLSXTemplate returns a workbook with a README sheet and one text-formatted
// sheet per entity.
func XLSXTemplate(s *schema.Schema) ([]byte, error) {
	f := excelize.NewFile()
	defer f.Close()
	if err := f.SetSheetName("Sheet1", ReadmeSheet); err != nil {
		return nil, err
	}
	text, err := f.NewStyle(&excelize.Style{NumFmt: 49})
	if err != nil {
		return nil, err
	}
	bold, err := f.NewStyle(&excelize.Style{NumFmt: 49, Font: &excelize.Font{Bold: true}})
	if err != nil {
		return nil, err
	}
	readme := [][]any{{"entity", "column", "register_ref", "type", "identity", "required_for_export", "conditional", "codelist", "description"}}
	for _, name := range s.EntityNames() {
		e, _ := s.Entity(name)
		if _, err := f.NewSheet(name); err != nil {
			return nil, err
		}
		cols := Columns(e)
		last, err := excelize.ColumnNumberToName(len(cols))
		if err != nil {
			return nil, err
		}
		if err := f.SetColStyle(name, "A:"+last, text); err != nil {
			return nil, err
		}
		header := make([]any, len(cols))
		for i, c := range cols {
			header[i] = c
		}
		if err := f.SetSheetRow(name, "A1", &header); err != nil {
			return nil, err
		}
		if err := f.SetRowStyle(name, 1, 1, bold); err != nil {
			return nil, err
		}
		for _, c := range cols {
			switch c {
			case NotApplicableColumn:
				readme = append(readme, []any{name, c, "", "list", "no", "no", "no", "", "Comma-separated fields that do not apply to this row (distinct from unknown)."})
			case SourceRefColumn:
				readme = append(readme, []any{name, c, "", "string", "no", "no", "no", "", "Identifier of this row in your source system (default: <entity>:<row>)."})
			default:
				fd := e.Fields[c]
				readme = append(readme, []any{name, c, fd.RoIRef, fd.Type, yesNo(fd.IsIdentity()), yesNo(fd.RoIRequired), yesNo(fd.RoIConditional), fd.Codelist, fd.Description})
			}
		}
	}
	for i, row := range readme {
		cell, err := excelize.CoordinatesToCellName(1, i+1)
		if err != nil {
			return nil, err
		}
		if err := f.SetSheetRow(ReadmeSheet, cell, &row); err != nil {
			return nil, err
		}
	}
	if err := f.SetRowStyle(ReadmeSheet, 1, 1, bold); err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	if err := f.Write(&buf); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
