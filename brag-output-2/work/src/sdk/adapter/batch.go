// SPDX-License-Identifier: Apache-2.0

package adapter

import (
	"encoding/json"
	"fmt"
	"io"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/nexops-one/compliance-engine/sdk/schema"
)

// Record is one canonical record: field name -> JSON value, plus optional "_meta".
type Record = map[string]any

// Source identifies where a batch comes from.
type Source struct {
	System         string `json:"system"`
	Adapter        string `json:"adapter"`
	AdapterVersion string `json:"adapter_version"`
}

// BatchInfo is the optional batch header.
type BatchInfo struct {
	BatchID     string     `json:"batch_id,omitempty"`
	GeneratedAt *time.Time `json:"generated_at,omitempty"`
	Mode        Mode       `json:"mode,omitempty"`
}

// Batch is the ingestion envelope defined by the canonical model.
type Batch struct {
	SchemaVersion string              `json:"schema_version"`
	Batch         *BatchInfo          `json:"batch,omitempty"`
	Source        Source              `json:"source"`
	Entities      map[string][]Record `json:"entities"`
}

// Mode returns the batch mode, defaulting to incremental.
func (b Batch) Mode() Mode {
	if b.Batch == nil || b.Batch.Mode == "" {
		return ModeIncremental
	}
	return b.Batch.Mode
}

// BatchID returns the batch identifier, or "".
func (b Batch) BatchID() string {
	if b.Batch == nil {
		return ""
	}
	return b.Batch.BatchID
}

// RecordCount returns the number of records across all entities.
func (b Batch) RecordCount() int {
	n := 0
	for _, recs := range b.Entities {
		n += len(recs)
	}
	return n
}

// DecodeBatch strictly decodes a JSON batch: unknown envelope fields are
// rejected and numbers keep full precision (json.Number).
func DecodeBatch(r io.Reader) (Batch, error) {
	dec := json.NewDecoder(r)
	dec.DisallowUnknownFields()
	dec.UseNumber()
	var b Batch
	if err := dec.Decode(&b); err != nil {
		return Batch{}, fmt.Errorf("decode batch: %w", err)
	}
	return b, nil
}

var schemaVersionPattern = regexp.MustCompile(`^\d+\.\d+\.\d+$`)

// ValidateEnvelope checks everything except the records themselves.
func (b Batch) ValidateEnvelope(s *schema.Schema) []schema.FieldError {
	var errs []schema.FieldError
	add := func(field, code, msg string) {
		errs = append(errs, schema.FieldError{Index: -1, Field: field, Code: code, Message: msg})
	}
	if !schemaVersionPattern.MatchString(b.SchemaVersion) {
		add("schema_version", "pattern", "schema_version must be MAJOR.MINOR.PATCH")
	}
	for field, v := range map[string]string{"source.system": b.Source.System, "source.adapter": b.Source.Adapter, "source.adapter_version": b.Source.AdapterVersion} {
		if strings.TrimSpace(v) == "" {
			add(field, "required", "field is required")
		}
		if strings.ContainsRune(v, 0) {
			add(field, "invalid_character", "text must not contain the NUL character (U+0000)")
		}
	}
	if b.Batch != nil && strings.ContainsRune(b.Batch.BatchID, 0) {
		add("batch.batch_id", "invalid_character", "text must not contain the NUL character (U+0000)")
	}
	if b.Batch != nil && b.Batch.Mode != "" && b.Batch.Mode != ModeFull && b.Batch.Mode != ModeIncremental {
		add("batch.mode", "enum", `mode must be "full" or "incremental"`)
	}
	if b.Entities == nil {
		add("entities", "required", "field is required")
	}
	for name := range b.Entities {
		if _, ok := s.Entity(name); !ok {
			add("entities."+name, "unknown_entity", fmt.Sprintf("entity %q is not defined in canonical schema %s", name, s.Version))
		}
	}
	sort.SliceStable(errs, func(i, j int) bool { return errs[i].Field < errs[j].Field })
	return errs
}
