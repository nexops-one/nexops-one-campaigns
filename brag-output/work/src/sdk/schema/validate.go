// SPDX-License-Identifier: Apache-2.0

package schema

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/santhosh-tekuri/jsonschema/v6"
	"github.com/santhosh-tekuri/jsonschema/v6/kind"
)

// ValidateRecord applies L1 structural validation to one record of entity.
// It returns nil when the record is valid.
func (s *Schema) ValidateRecord(entity string, record map[string]any) []FieldError {
	v, ok := s.validators[entity]
	if !ok {
		return []FieldError{{Entity: entity, Code: "unknown_entity", Message: fmt.Sprintf("entity %q is not defined in canonical schema %s", entity, s.Version)}}
	}
	data, err := json.Marshal(record)
	if err != nil {
		return []FieldError{{Entity: entity, Code: "invalid_json", Message: err.Error()}}
	}
	inst, err := jsonschema.UnmarshalJSON(bytes.NewReader(data))
	if err != nil {
		return []FieldError{{Entity: entity, Code: "invalid_json", Message: err.Error()}}
	}
	err = v.Validate(inst)
	if err == nil {
		return nil
	}
	var ve *jsonschema.ValidationError
	if !errors.As(err, &ve) {
		return []FieldError{{Entity: entity, Code: "invalid", Message: err.Error()}}
	}
	return outputErrors(entity, ve.BasicOutput())
}

func outputErrors(entity string, out *jsonschema.OutputUnit) []FieldError {
	var errs []FieldError
	for _, u := range out.Errors {
		if u.Error == nil || strings.HasSuffix(u.KeywordLocation, "/$ref") {
			continue // "$ref" wrapper units only say "validation failed"; nested units carry the detail
		}
		base := pointerToPath(u.InstanceLocation)
		switch k := u.Error.Kind.(type) {
		case *kind.Required:
			for _, f := range k.Missing {
				errs = append(errs, FieldError{Entity: entity, Field: joinPath(base, f), Code: "required", Message: "field is required"})
			}
		case *kind.AdditionalProperties:
			for _, f := range k.Properties {
				errs = append(errs, FieldError{Entity: entity, Field: joinPath(base, f), Code: "unknown_field", Message: "field is not defined in the canonical schema"})
			}
		default:
			errs = append(errs, FieldError{Entity: entity, Field: base, Code: lastSegment(u.KeywordLocation), Message: u.Error.String()})
		}
	}
	sort.SliceStable(errs, func(i, j int) bool {
		if errs[i].Field != errs[j].Field {
			return errs[i].Field < errs[j].Field
		}
		return errs[i].Code < errs[j].Code
	})
	return errs
}

func pointerToPath(p string) string { return strings.ReplaceAll(strings.TrimPrefix(p, "/"), "/", ".") }

func joinPath(base, field string) string {
	if base == "" {
		return field
	}
	return base + "." + field
}

func lastSegment(p string) string {
	if i := strings.LastIndex(p, "/"); i >= 0 {
		return p[i+1:]
	}
	return p
}
