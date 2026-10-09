// SPDX-License-Identifier: Apache-2.0

package importer

import (
	"bytes"
	"crypto/sha256"
	"encoding/csv"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"path"
	"regexp"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/nexops-one/compliance-engine/sdk/adapter"
	"github.com/nexops-one/compliance-engine/sdk/schema"
)

// Format is an import file format.
type Format string

const (
	FormatCSV  Format = "csv"
	FormatXLSX Format = "xlsx"
)

// AdapterVersion is the version the importer adapters declare.
const AdapterVersion = "0.1.0"

// Input is one file to import.
type Input struct {
	Name   string       // file name; its extension selects the format
	Data   []byte       // file content
	Entity string       // CSV only; default: file name without extension
	Mode   adapter.Mode // default: incremental
	System string       // source system; default: "file:<base name>"
	// MaxUnzippedBytes bounds the decompressed size of a workbook (XLSX only);
	// default DefaultMaxUnzippedBytes.
	MaxUnzippedBytes int64
}

// Parsed is a file turned into one canonical batch and its importer manifest.
type Parsed struct {
	Format   Format
	Batch    adapter.Batch
	Manifest adapter.Manifest
	Rows     int // non-blank data rows read
}

// FormatOf selects the format from the file extension.
func FormatOf(name string) (Format, error) {
	switch strings.ToLower(path.Ext(name)) {
	case ".csv":
		return FormatCSV, nil
	case ".xlsx":
		return FormatXLSX, nil
	}
	return "", fileErr("", "unsupported_format", fmt.Sprintf("%q: only .csv and .xlsx files can be imported", name))
}

// Parse reads a CSV or XLSX file into a canonical batch.
func Parse(s *schema.Schema, in Input) (Parsed, error) {
	format, err := FormatOf(in.Name)
	if err != nil {
		return Parsed{}, err
	}
	mode := in.Mode
	if mode == "" {
		mode = adapter.ModeIncremental
	}
	if mode != adapter.ModeFull && mode != adapter.ModeIncremental {
		return Parsed{}, fileErr("", "invalid_mode", fmt.Sprintf("mode %q must be full or incremental", mode))
	}
	var tables map[string][][]string
	var manifest adapter.Manifest
	switch format {
	case FormatCSV:
		entity := in.Entity
		if entity == "" {
			entity = strings.TrimSuffix(path.Base(in.Name), path.Ext(in.Name))
		}
		e, ok := s.Entity(entity)
		if !ok {
			return Parsed{}, fileErr(entity, "unknown_entity", fmt.Sprintf("%q is not a canonical entity: name the file <entity>.csv or pass the entity explicitly", entity))
		}
		if !utf8.Valid(in.Data) {
			return Parsed{}, fileErr(entity, "invalid_encoding", `the file is not UTF-8 text: in Excel, save it as "CSV UTF-8 (comma delimited)"`)
		}
		rows, err := readCSV(in.Data)
		if err != nil {
			return Parsed{}, fileErr(entity, "invalid_csv", err.Error())
		}
		tables = map[string][][]string{entity: rows}
		manifest = importerManifest(s, "csv-import."+entity, []*schema.Entity{e})
	case FormatXLSX:
		if tables, err = readXLSX(s, in.Data, in.MaxUnzippedBytes); err != nil {
			return Parsed{}, err
		}
		var all []*schema.Entity
		for _, name := range s.EntityNames() {
			e, _ := s.Entity(name)
			all = append(all, e)
		}
		manifest = importerManifest(s, "xlsx-import", all)
	}
	system := in.System
	if system == "" {
		system = "file:" + path.Base(in.Name)
	}
	sum := sha256.Sum256(in.Data)
	p := Parsed{Format: format, Manifest: manifest, Batch: adapter.Batch{
		SchemaVersion: s.Version,
		Batch:         &adapter.BatchInfo{BatchID: "sha256:" + hex.EncodeToString(sum[:]), Mode: mode},
		Source:        adapter.Source{System: system, Adapter: manifest.Name, AdapterVersion: AdapterVersion},
		Entities:      map[string][]adapter.Record{},
	}}
	names := make([]string, 0, len(tables))
	for name := range tables {
		names = append(names, name)
	}
	sort.Strings(names)
	var headerErrs []schema.FieldError
	for _, name := range names {
		e, _ := s.Entity(name)
		recs, errs := parseTable(e, tables[name])
		headerErrs = append(headerErrs, errs...)
		p.Rows += len(recs)
		if len(recs) > 0 {
			p.Batch.Entities[name] = recs
		}
	}
	if len(headerErrs) > 0 {
		return Parsed{}, &FileError{Errors: headerErrs}
	}
	return p, nil
}

func importerManifest(s *schema.Schema, name string, entities []*schema.Entity) adapter.Manifest {
	supplies := map[string][]string{}
	for _, e := range entities {
		supplies[e.Name] = append([]string(nil), e.FieldNames...)
	}
	return adapter.Manifest{Name: name, Version: AdapterVersion, SchemaVersion: s.Version, Supplies: supplies,
		Modes: []adapter.Mode{adapter.ModeIncremental, adapter.ModeFull}}
}

// readCSV returns the rows indexed by line (rows[i] starts on line i+1):
// encoding/csv skips blank lines, so they are re-inserted as empty rows to
// keep row numbers equal to what a spreadsheet shows.
func readCSV(data []byte) ([][]string, error) {
	data = bytes.TrimPrefix(data, []byte("\xef\xbb\xbf"))
	r := csv.NewReader(bytes.NewReader(data))
	r.FieldsPerRecord = -1
	first, _, _ := bytes.Cut(data, []byte("\n"))
	if bytes.Contains(first, []byte(";")) && !bytes.Contains(first, []byte(",")) {
		r.Comma = ';'
	}
	var rows [][]string
	for {
		rec, err := r.Read()
		if err == io.EOF {
			return rows, nil
		}
		if err != nil {
			return nil, err
		}
		line, _ := r.FieldPos(0)
		for len(rows) < line-1 {
			rows = append(rows, nil)
		}
		rows = append(rows, rec)
	}
}

// parseTable converts a header row plus data rows. Header problems reject the
// table; cell problems never do (the cell keeps its text for L1).
func parseTable(e *schema.Entity, rows [][]string) ([]adapter.Record, []schema.FieldError) {
	if len(rows) == 0 {
		return nil, nil
	}
	var errs []schema.FieldError
	headerErr := func(field, code, msg string) {
		errs = append(errs, schema.FieldError{Entity: e.Name, Index: -1, Field: field, Code: code, Message: msg})
	}
	cols := make([]string, len(rows[0]))
	seen := map[string]bool{}
	for i, h := range rows[0] {
		h = strings.TrimSpace(h)
		cols[i] = h
		if h == "" {
			continue
		}
		if seen[h] {
			headerErr(h, "duplicate_column", fmt.Sprintf("column %q appears more than once", h))
		}
		seen[h] = true
		if h != NotApplicableColumn && h != SourceRefColumn && e.Fields[h] == nil {
			headerErr(h, "unknown_column", fmt.Sprintf("column %q is not a field of %s (see the template)", h, e.Name))
		}
	}
	for _, k := range e.IdentityKey {
		if !seen[k] {
			headerErr(k, "missing_column", fmt.Sprintf("identity column %q is required", k))
		}
	}
	if len(errs) > 0 {
		return nil, errs
	}
	var recs []adapter.Record
	for i, row := range rows[1:] {
		if blank(row) {
			continue
		}
		rec := adapter.Record{}
		ref := fmt.Sprintf("%s:%d", e.Name, i+2)
		var notApplicable []string
		for c, cell := range row {
			v := strings.TrimSpace(cell)
			if v == "" {
				continue
			}
			col := ""
			if c < len(cols) {
				col = cols[c]
			}
			switch col {
			case "":
				rec[fmt.Sprintf("_column_%d", c+1)] = v
			case NotApplicableColumn:
				for _, f := range strings.Split(v, ",") {
					if f = strings.TrimSpace(f); f != "" {
						notApplicable = append(notApplicable, f)
					}
				}
			case SourceRefColumn:
				ref = v
			default:
				rec[col] = convert(e.Fields[col], v)
			}
		}
		adapter.SetSourceRef(rec, ref)
		if len(notApplicable) > 0 {
			adapter.MarkNotApplicable(rec, notApplicable...)
		}
		recs = append(recs, rec)
	}
	return recs, nil
}

func blank(row []string) bool {
	for _, c := range row {
		if strings.TrimSpace(c) != "" {
			return false
		}
	}
	return true
}

var (
	integerText = regexp.MustCompile(`^-?(0|[1-9][0-9]*)$`)
	numberText  = regexp.MustCompile(`^-?(0|[1-9][0-9]*)(\.[0-9]+)?([eE][+-]?[0-9]+)?$`)
)

// convert maps cell text to the JSON type of the field; text that does not
// convert is returned unchanged so validation reports it.
func convert(f *schema.Field, v string) any {
	switch f.Type {
	case "integer":
		if integerText.MatchString(v) {
			return json.Number(v)
		}
	case "number":
		if numberText.MatchString(v) {
			return json.Number(v)
		}
	case "boolean":
		switch strings.ToLower(v) {
		case "true", "yes", "y", "1":
			return true
		case "false", "no", "n", "0":
			return false
		}
	case "object":
		dec := json.NewDecoder(strings.NewReader(v))
		dec.UseNumber()
		var m map[string]any
		if err := dec.Decode(&m); err == nil && m != nil {
			return m
		}
	}
	return v
}
