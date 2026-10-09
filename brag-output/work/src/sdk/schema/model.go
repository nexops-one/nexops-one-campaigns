// SPDX-License-Identifier: Apache-2.0

package schema

// MetaField is the optional per-record metadata property.
const MetaField = "_meta"

// FieldKind classifies fields that get identifier checks (L1b).
type FieldKind string

const (
	KindPlain    FieldKind = "plain"
	KindLEI      FieldKind = "lei"
	KindCountry  FieldKind = "country"
	KindCurrency FieldKind = "currency"
)

const (
	patternLEI      = "^[A-Z0-9]{18}[0-9]{2}$"
	patternCountry  = "^[A-Z]{2}$"
	patternCurrency = "^[A-Z]{3}$"
)

func kindFor(pattern string) FieldKind {
	switch pattern {
	case patternLEI:
		return KindLEI
	case patternCountry:
		return KindCountry
	case patternCurrency:
		return KindCurrency
	}
	return KindPlain
}

// Field describes one canonical field, including the x-* extensions.
type Field struct {
	Name           string
	Type           string // JSON Schema type: string, integer, number, boolean, object
	Format         string
	Pattern        string
	Enum           []string
	Description    string
	RoIRef         string // x-roi-ref, e.g. B_02.02.0150
	References     string // x-references, "entity.field"
	Key            string // x-key: "identity" or "roi-composite"
	RoIRequired    bool   // x-roi-required: NOT NULL in the register data model
	RoIConditional bool   // x-roi-conditional: required but plausibly conditional
	Codelist       string // x-codelist
	Kind           FieldKind
}

// IsIdentity reports whether the field is part of the record identity.
func (f *Field) IsIdentity() bool { return f.Key == "identity" }

// Entity describes one canonical entity.
type Entity struct {
	Name        string
	Title       string
	Template    string   // x-roi-template, e.g. B_02.02
	IdentityKey []string // x-identity-key, in order
	Fields      map[string]*Field
	FieldNames  []string // sorted
}

// FieldError is a field-level validation error or warning. Index is the
// record's position in its entity array within the batch (-1 when the error
// is not about a single record).
type FieldError struct {
	Entity          string `json:"entity"`
	Index           int    `json:"index"`
	SourceRecordRef string `json:"source_record_ref,omitempty"`
	Field           string `json:"field"`
	Code            string `json:"code"`
	Message         string `json:"message"`
}
