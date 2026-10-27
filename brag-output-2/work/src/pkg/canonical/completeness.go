// SPDX-License-Identifier: Apache-2.0

package canonical

import "sort"

// Supply reports whether any registered adapter declares it can supply entity.field.
type Supply func(entity, field string) bool

// Gap is an export-required field that is missing or only derived.
type Gap struct {
	Entity      string     `json:"entity"`
	Key         string     `json:"key"`
	Field       string     `json:"field"`
	RoIRef      string     `json:"roi_ref,omitempty"`
	State       FieldState `json:"state"`
	Conditional bool       `json:"conditional"`
	Supplied    bool       `json:"supplied"`
}

// CodeIssue is a coded value not present in its loaded codelist.
type CodeIssue struct {
	Entity   string `json:"entity"`
	Key      string `json:"key"`
	Field    string `json:"field"`
	Codelist string `json:"codelist"`
	Value    string `json:"value"`
}

// EntityCompleteness summarizes one entity.
type EntityCompleteness struct {
	Entity  string `json:"entity"`
	Records int    `json:"records"`
	Missing int    `json:"missing"`
	Derived int    `json:"derived"`
}

// Completeness is the data-completeness view (L3) plus the L2 report.
type Completeness struct {
	SnapshotID          string               `json:"snapshot_id,omitempty"`
	SchemaVersion       string               `json:"schema_version"`
	Entities            []EntityCompleteness `json:"entities"`
	Gaps                []Gap                `json:"gaps"`
	InvalidCodes        []CodeIssue          `json:"invalid_codes"`
	UnverifiedCodelists []string             `json:"unverified_codelists"`
	References          ReferenceReport      `json:"references"`
}

// AnalyzeCompleteness computes the completeness view. A nil supplied function
// means supply information is unknown; gaps are then reported as supplied.
func AnalyzeCompleteness(ix *Index, codes *Codelists, supplied Supply) Completeness {
	s := ix.Schema()
	out := Completeness{
		SchemaVersion: s.Version, Entities: []EntityCompleteness{}, Gaps: []Gap{},
		InvalidCodes: []CodeIssue{}, UnverifiedCodelists: []string{}, References: CheckReferences(ix),
	}
	unverified := map[string]bool{}
	for _, en := range s.EntityNames() {
		e, _ := s.Entity(en)
		stats := EntityCompleteness{Entity: en, Records: len(ix.Records(en))}
		for _, rec := range ix.Records(en) {
			view := NewView(rec.Data)
			for _, fn := range e.FieldNames {
				f := e.Fields[fn]
				st := view.State(fn)
				if f.RoIRequired && (st == StateMissing || st == StateDerived) {
					if st == StateMissing {
						stats.Missing++
					} else {
						stats.Derived++
					}
					out.Gaps = append(out.Gaps, Gap{
						Entity: en, Key: rec.Key, Field: fn, RoIRef: f.RoIRef, State: st,
						Conditional: f.RoIConditional, Supplied: supplied == nil || supplied(en, fn),
					})
				}
				if f.Codelist == "" || (st != StateProvided && st != StateDerived) {
					continue
				}
				v, _ := ScalarString(rec.Data[fn])
				switch codes.Check(f.Codelist, v) {
				case CodeInvalid:
					out.InvalidCodes = append(out.InvalidCodes, CodeIssue{Entity: en, Key: rec.Key, Field: fn, Codelist: f.Codelist, Value: v})
				case CodeUnverified:
					unverified[f.Codelist] = true
				}
			}
		}
		out.Entities = append(out.Entities, stats)
	}
	for name := range unverified {
		out.UnverifiedCodelists = append(out.UnverifiedCodelists, name)
	}
	sort.Strings(out.UnverifiedCodelists)
	return out
}
