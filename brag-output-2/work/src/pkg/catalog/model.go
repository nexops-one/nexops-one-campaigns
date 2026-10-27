// SPDX-License-Identifier: Apache-2.0

// Package catalog loads, validates and compares versioned control catalogs.
// Catalogs are data: YAML files validated against a meta-schema and against
// the canonical schema they target.
package catalog

import (
	"fmt"
	"strings"

	"github.com/nexops-one/compliance-engine/sdk/schema"
)

// Ref identifies one catalog version.
type Ref struct {
	Catalog string `json:"catalog"`
	Version string `json:"version"`
}

func (r Ref) String() string { return r.Catalog + "@" + r.Version }

// ParseRef parses "catalog@version".
func ParseRef(s string) (Ref, error) {
	name, version, ok := strings.Cut(s, "@")
	if !ok || name == "" {
		return Ref{}, fmt.Errorf("invalid catalog reference %q: want catalog@version", s)
	}
	if _, err := schema.ParseVersion(version); err != nil {
		return Ref{}, fmt.Errorf("invalid catalog reference %q: %w", s, err)
	}
	return Ref{Catalog: name, Version: version}, nil
}

// RuleKind is one of the fixed rule kinds.
type RuleKind string

const (
	KindRecordsExist       RuleKind = "records_exist"
	KindFieldsComplete     RuleKind = "fields_complete"
	KindReferencesResolved RuleKind = "references_resolved"
	KindFieldEquals        RuleKind = "field_equals"
	KindFieldIn            RuleKind = "field_in"
	KindManual             RuleKind = "manual"
)

// Condition selects records: Equals, or membership in In.
type Condition struct {
	Field  string `json:"field"`
	Equals any    `json:"equals,omitempty"`
	In     []any  `json:"in,omitempty"`
}

// Rule is a control's data-defined rule. Fields, Field and filter fields are
// bare field names of Entity.
type Rule struct {
	Kind   RuleKind    `json:"kind"`
	Entity string      `json:"entity,omitempty"`
	Min    int         `json:"min,omitempty"`
	Fields []string    `json:"fields,omitempty"`
	Field  string      `json:"field,omitempty"`
	Value  any         `json:"value,omitempty"`
	Values []any       `json:"values,omitempty"`
	Filter []Condition `json:"filter,omitempty"`
}

// Control is one catalog control. Requires are full paths "entity.field".
type Control struct {
	ID                   string   `json:"id"`
	Title                string   `json:"title"`
	Description          string   `json:"description"`
	SourceAuthority      string   `json:"source_authority"`
	EvidenceRequirements []string `json:"evidence_requirements"`
	Requires             []string `json:"requires"`
	AcceptDerived        bool     `json:"accept_derived,omitempty"`
	Rule                 Rule     `json:"rule"`
	// ReviewInterval is how long an approval holds (ISO 8601 period such as
	// P365D); empty uses the catalog's scoring.review_interval, then P365D.
	ReviewInterval string `json:"review_interval,omitempty"`
	// ApprovalRequiresEvidence overrides the default: evidence is required when
	// evidence_requirements is not empty or the rule is manual.
	ApprovalRequiresEvidence *bool `json:"approval_requires_evidence,omitempty"`
}

// Scoring states which statuses count towards the score, and why.
type Scoring struct {
	CountsAsReady  []string `json:"counts_as_ready"`
	Assumptions    string   `json:"assumptions"`
	ReviewInterval string   `json:"review_interval,omitempty"`
}

// Catalog is one versioned control catalog.
type Catalog struct {
	Catalog         string    `json:"catalog"`
	Version         string    `json:"version"`
	Framework       string    `json:"framework"`
	Jurisdiction    string    `json:"jurisdiction"`
	EffectiveDate   string    `json:"effective_date"`
	SchemaVersion   string    `json:"schema_version"`
	SourceAuthority string    `json:"source_authority"`
	Scoring         Scoring   `json:"scoring"`
	Controls        []Control `json:"controls"`
	// Feature is the entitlement a workspace needs to evaluate against this
	// catalog; empty for open catalogs. Set by the catalog's source, never
	// by the catalog file (the meta-schema refuses unknown keys).
	Feature string `json:"feature,omitempty"`
	// Origin says where the catalog was loaded from: embedded, dir, or
	// feed:<bundle-id> for a signed maintained-catalog bundle.
	Origin string `json:"origin,omitempty"`
}

// Ref returns the catalog's reference.
func (c *Catalog) Ref() Ref { return Ref{Catalog: c.Catalog, Version: c.Version} }
