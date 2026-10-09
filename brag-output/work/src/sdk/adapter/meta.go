// SPDX-License-Identifier: Apache-2.0

package adapter

import (
	"encoding/json"
	"slices"

	"github.com/nexops-one/compliance-engine/sdk/schema"
)

// DerivedField declares that a value was inferred rather than read from the source.
type DerivedField struct {
	Field     string `json:"field"`
	Method    string `json:"method"`
	SourceRef string `json:"source_ref,omitempty"`
}

// Meta is the optional per-record metadata ("_meta").
type Meta struct {
	SourceRecordRef     string         `json:"source_record_ref,omitempty"`
	NotApplicableFields []string       `json:"not_applicable_fields,omitempty"`
	DerivedFields       []DerivedField `json:"derived_fields,omitempty"`
}

// MetaOf returns the record's metadata. Malformed metadata yields an empty
// Meta; L1 validation reports the malformation.
func MetaOf(rec Record) Meta {
	var m Meta
	raw, ok := rec[schema.MetaField]
	if !ok || raw == nil {
		return m
	}
	data, err := json.Marshal(raw)
	if err == nil {
		_ = json.Unmarshal(data, &m)
	}
	return m
}

func setMeta(rec Record, m Meta) {
	data, _ := json.Marshal(m)
	var out map[string]any
	_ = json.Unmarshal(data, &out)
	if len(out) == 0 {
		delete(rec, schema.MetaField)
		return
	}
	rec[schema.MetaField] = out
}

// SetSourceRef records the record's identifier in the source system.
func SetSourceRef(rec Record, ref string) {
	m := MetaOf(rec)
	m.SourceRecordRef = ref
	setMeta(rec, m)
}

// MarkNotApplicable declares fields that do not apply to this record (distinct from missing).
func MarkNotApplicable(rec Record, fields ...string) {
	m := MetaOf(rec)
	for _, f := range fields {
		if !slices.Contains(m.NotApplicableFields, f) {
			m.NotApplicableFields = append(m.NotApplicableFields, f)
		}
	}
	setMeta(rec, m)
}

// MarkDerived declares that a field's value was inferred.
func MarkDerived(rec Record, d DerivedField) {
	m := MetaOf(rec)
	m.DerivedFields = slices.DeleteFunc(m.DerivedFields, func(x DerivedField) bool { return x.Field == d.Field })
	m.DerivedFields = append(m.DerivedFields, d)
	setMeta(rec, m)
}
