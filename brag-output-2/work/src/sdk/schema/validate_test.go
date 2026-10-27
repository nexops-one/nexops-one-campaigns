// SPDX-License-Identifier: Apache-2.0

package schema_test

import (
	"reflect"
	"testing"

	"github.com/nexops-one/compliance-engine/sdk/schema"
)

type fc struct{ Field, Code string }

func fieldsAndCodes(errs []schema.FieldError) []fc {
	out := []fc{}
	for _, e := range errs {
		out = append(out, fc{e.Field, e.Code})
	}
	return out
}

func TestValidateRecordAcceptsValidRecord(t *testing.T) {
	s := latest(t)
	rec := map[string]any{
		"function_id": "F-001", "financial_entity_lei": "SAMPLEFE000000000021",
		"rto": 240, "last_assessment_date": "2025-11-30",
	}
	if errs := s.ValidateRecord("function", rec); errs != nil {
		t.Fatalf("unexpected errors: %+v", errs)
	}
}

func TestValidateRecordReportsFieldLevelErrors(t *testing.T) {
	s := latest(t)
	rec := map[string]any{
		"function_id": "F1", "financial_entity_lei": "bad", "rto": -1,
		"last_assessment_date": "2025-13-40", "extra": 1,
	}
	got := fieldsAndCodes(s.ValidateRecord("function", rec))
	want := []fc{{"extra", "unknown_field"}, {"financial_entity_lei", "pattern"}, {"last_assessment_date", "format"}, {"rto", "minimum"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v want %v", got, want)
	}
	for _, e := range s.ValidateRecord("function", rec) {
		if e.Entity != "function" || e.Message == "" {
			t.Fatalf("incomplete error %+v", e)
		}
	}
}

func TestValidateRecordMissingIdentity(t *testing.T) {
	s := latest(t)
	got := fieldsAndCodes(s.ValidateRecord("function", map[string]any{"function_id": "F1"}))
	if !reflect.DeepEqual(got, []fc{{"financial_entity_lei", "required"}}) {
		t.Fatalf("got %v", got)
	}
}

func TestValidateRecordNestedMetaErrors(t *testing.T) {
	s := latest(t)
	rec := map[string]any{
		"function_id": "F1", "financial_entity_lei": "SAMPLEFE000000000021",
		"_meta": map[string]any{"derived_fields": []any{map[string]any{"field": "x"}}, "bogus": 1},
	}
	got := fieldsAndCodes(s.ValidateRecord("function", rec))
	want := []fc{{"_meta.bogus", "unknown_field"}, {"_meta.derived_fields.0.method", "required"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v want %v", got, want)
	}
}

func TestValidateRecordUnknownEntity(t *testing.T) {
	s := latest(t)
	got := fieldsAndCodes(s.ValidateRecord("nope", map[string]any{}))
	if !reflect.DeepEqual(got, []fc{{"", "unknown_entity"}}) {
		t.Fatalf("got %v", got)
	}
}
