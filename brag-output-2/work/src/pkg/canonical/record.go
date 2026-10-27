// SPDX-License-Identifier: Apache-2.0

package canonical

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strconv"

	"github.com/nexops-one/compliance-engine/sdk/adapter"
	"github.com/nexops-one/compliance-engine/sdk/schema"
)

// Normalize converts a record to plain JSON types (numbers as json.Number) and
// drops top-level null fields: the canonical model treats null as missing.
func Normalize(rec adapter.Record) (adapter.Record, error) {
	data, err := json.Marshal(rec)
	if err != nil {
		return nil, err
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	var out adapter.Record
	if err := dec.Decode(&out); err != nil {
		return nil, err
	}
	for k, v := range out {
		if v == nil {
			delete(out, k)
		}
	}
	return out, nil
}

// Key returns the record identity: its identity values in x-identity-key
// order, encoded as a JSON array.
func Key(e *schema.Entity, rec adapter.Record) (string, error) {
	vals := make([]any, len(e.IdentityKey))
	for i, k := range e.IdentityKey {
		v, ok := rec[k]
		if !ok || v == nil {
			return "", fmt.Errorf("%s: identity field %s is missing", e.Name, k)
		}
		vals[i] = v
	}
	data, err := json.Marshal(vals)
	return string(data), err
}

// Hash returns the hex SHA-256 of the record's JSON encoding. encoding/json
// sorts map keys, so the hash is independent of field order.
func Hash(rec adapter.Record) string {
	data, _ := json.Marshal(rec)
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// FieldState is the state of one field of one record.
type FieldState string

const (
	StateProvided      FieldState = "provided"
	StateMissing       FieldState = "missing"
	StateNotApplicable FieldState = "not_applicable"
	StateDerived       FieldState = "derived"
)

// View reads field states of a record, parsing its metadata once.
type View struct {
	rec           adapter.Record
	derived       map[string]adapter.DerivedField
	notApplicable map[string]bool
}

// NewView builds a View over rec.
func NewView(rec adapter.Record) View {
	m := adapter.MetaOf(rec)
	v := View{rec: rec, derived: map[string]adapter.DerivedField{}, notApplicable: map[string]bool{}}
	for _, d := range m.DerivedFields {
		v.derived[d.Field] = d
	}
	for _, f := range m.NotApplicableFields {
		v.notApplicable[f] = true
	}
	return v
}

// State returns the field's state. Empty strings, objects and arrays count as missing.
func (v View) State(field string) FieldState {
	if val, ok := v.rec[field]; ok && !isEmpty(val) {
		if _, derived := v.derived[field]; derived {
			return StateDerived
		}
		return StateProvided
	}
	if v.notApplicable[field] {
		return StateNotApplicable
	}
	return StateMissing
}

func isEmpty(v any) bool {
	switch t := v.(type) {
	case nil:
		return true
	case string:
		return t == ""
	case map[string]any:
		return len(t) == 0
	case []any:
		return len(t) == 0
	}
	return false
}

// Derived returns the derivation declared for field.
func (v View) Derived(field string) (adapter.DerivedField, bool) {
	d, ok := v.derived[field]
	return d, ok
}

// ScalarString renders a scalar JSON value as a string for comparisons.
func ScalarString(v any) (string, bool) {
	switch t := v.(type) {
	case string:
		return t, true
	case json.Number:
		return t.String(), true
	case float64:
		return strconv.FormatFloat(t, 'f', -1, 64), true
	case int:
		return strconv.Itoa(t), true
	case int64:
		return strconv.FormatInt(t, 10), true
	case bool:
		return strconv.FormatBool(t), true
	}
	return "", false
}
