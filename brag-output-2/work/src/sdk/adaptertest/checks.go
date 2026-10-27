// SPDX-License-Identifier: Apache-2.0

// Package adaptertest is the adapter conformance suite. The checks are plain
// functions over manifests and batches, so the Go harness (Run, Evaluate) and
// the language-neutral runner (compliance-engine adapter test) apply the same
// rules to in-process adapters and to batch files produced in any language.
package adaptertest

import (
	"bytes"
	"encoding/json"
	"fmt"
	"slices"
	"sort"
	"strings"

	"github.com/nexops-one/compliance-engine/sdk/adapter"
	"github.com/nexops-one/compliance-engine/sdk/schema"
)

// Check names.
const (
	CheckManifest      = "manifest"
	CheckPull          = "pull"
	CheckEnvelope      = "envelope"
	CheckManifestMatch = "manifest_match"
	CheckRecords       = "records"
	CheckIdentity      = "identity"
	CheckOutside       = "outside_manifest"
	CheckDerived       = "derived_fields"
	CheckFullScope     = "full_scope"
	CheckIdempotency   = "idempotency"
)

// BatchChecks lists, in report order, the checks applied to the batches of a sync.
var BatchChecks = []string{CheckPull, CheckEnvelope, CheckManifestMatch, CheckRecords, CheckIdentity, CheckOutside, CheckDerived, CheckFullScope, CheckIdempotency}

// Finding is one conformance problem. Warnings are reported but do not fail
// the suite. Index is the record's position in its entity array, or -1.
type Finding struct {
	Check           string `json:"check"`
	Batch           string `json:"batch,omitempty"`
	Entity          string `json:"entity,omitempty"`
	Index           int    `json:"index"`
	SourceRecordRef string `json:"source_record_ref,omitempty"`
	Field           string `json:"field,omitempty"`
	Message         string `json:"message"`
	Warning         bool   `json:"warning,omitempty"`
}

func (f Finding) String() string {
	var b strings.Builder
	b.WriteString("[" + f.Check + "]")
	if f.Batch != "" {
		b.WriteString(" " + f.Batch)
	}
	if f.Entity != "" {
		b.WriteString(" " + f.Entity)
		if f.Index >= 0 {
			fmt.Fprintf(&b, "[%d]", f.Index)
		}
	}
	if f.SourceRecordRef != "" {
		b.WriteString(" (" + f.SourceRecordRef + ")")
	}
	if f.Field != "" {
		b.WriteString(" " + f.Field)
	}
	b.WriteString(": " + f.Message)
	if f.Warning {
		b.WriteString(" [warning]")
	}
	return b.String()
}

// Label sets Batch (a file name or run label) on every finding and returns them.
func Label(fs []Finding, batch string) []Finding {
	for i := range fs {
		fs[i].Batch = batch
	}
	return fs
}

// Failures returns the findings that are not warnings.
func Failures(fs []Finding) []Finding {
	var out []Finding
	for _, f := range fs {
		if !f.Warning {
			out = append(out, f)
		}
	}
	return out
}

// ValidateManifest checks the manifest document against the manifest JSON Schema
// and the manifest against the canonical schema version it declares.
func ValidateManifest(reg *schema.Registry, m adapter.Manifest) []Finding {
	var out []Finding
	data, err := json.Marshal(m)
	if err != nil {
		return []Finding{{Check: CheckManifest, Index: -1, Message: err.Error()}}
	}
	for _, e := range schema.ValidateManifestDocument(data) {
		out = append(out, Finding{Check: CheckManifest, Index: -1, Field: e.Field, Message: e.Message + " (" + e.Code + ")"})
	}
	if err := m.Validate(reg); err != nil {
		for _, line := range strings.Split(err.Error(), "\n") {
			out = append(out, Finding{Check: CheckManifest, Index: -1, Message: line})
		}
	}
	return out
}

// ValidateBatch applies the per-batch checks. derived lists, per entity, the
// fields the input makes the adapter infer; nil when the input forces none.
func ValidateBatch(reg *schema.Registry, m adapter.Manifest, b adapter.Batch, derived map[string][]string) []Finding {
	s, err := reg.Resolve(b.SchemaVersion)
	if err != nil {
		return []Finding{{Check: CheckEnvelope, Index: -1, Field: "schema_version", Message: err.Error()}}
	}
	var out []Finding
	for _, e := range b.ValidateEnvelope(s) {
		out = append(out, Finding{Check: CheckEnvelope, Index: -1, Field: e.Field, Message: e.Message + " (" + e.Code + ")"})
	}
	out = append(out, matchManifest(reg, s, m, b)...)
	for _, name := range sortedKeys(b.Entities) {
		e, known := s.Entity(name)
		if !known {
			continue // reported by the envelope check
		}
		supplied, inManifest := m.Supplies[name]
		if !inManifest {
			out = append(out, Finding{Check: CheckOutside, Entity: name, Index: -1, Message: "the manifest does not supply this entity"})
		}
		seen := map[string]int{}
		for i, raw := range b.Entities[name] {
			rec, err := normalize(raw)
			if err != nil {
				out = append(out, Finding{Check: CheckRecords, Entity: name, Index: i, Message: err.Error()})
				continue
			}
			meta := adapter.MetaOf(rec)
			at := func(check, field, msg string) {
				out = append(out, Finding{Check: check, Entity: name, Index: i, SourceRecordRef: meta.SourceRecordRef, Field: field, Message: msg})
			}
			for _, fe := range s.ValidateRecord(name, rec) {
				at(CheckRecords, fe.Field, fe.Message+" ("+fe.Code+")")
			}
			if inManifest {
				for _, f := range fieldNames(rec) {
					if !slices.Contains(supplied, f) {
						at(CheckOutside, f, "field is not declared in the manifest's supplies")
					}
				}
			}
			for _, f := range derived[name] {
				declared := slices.ContainsFunc(meta.DerivedFields, func(d adapter.DerivedField) bool { return d.Field == f })
				if _, present := rec[f]; present && !declared {
					at(CheckDerived, f, "the value is inferred for this input but not declared in _meta.derived_fields")
				}
			}
			if key, ok := identity(e, rec); ok {
				if first, dup := seen[key]; dup {
					at(CheckIdentity, "", fmt.Sprintf("identity key %s is also used by record %d", key, first))
				} else {
					seen[key] = i
				}
			}
		}
	}
	if b.Mode() == adapter.ModeFull {
		for _, name := range sortedKeys(m.Supplies) {
			if _, present := b.Entities[name]; !present {
				out = append(out, Finding{Check: CheckFullScope, Entity: name, Index: -1, Warning: true,
					Message: "this full batch has no " + name + " array: every " + name + " record previously sent by this adapter would be deleted"})
			}
		}
	}
	return out
}

func matchManifest(reg *schema.Registry, s *schema.Schema, m adapter.Manifest, b adapter.Batch) []Finding {
	var out []Finding
	add := func(field, msg string) {
		out = append(out, Finding{Check: CheckManifestMatch, Index: -1, Field: field, Message: msg})
	}
	if b.Source.Adapter != m.Name {
		add("source.adapter", fmt.Sprintf("source.adapter is %q but the manifest name is %q", b.Source.Adapter, m.Name))
	}
	if b.Source.AdapterVersion != m.Version {
		add("source.adapter_version", fmt.Sprintf("source.adapter_version is %q but the manifest version is %q", b.Source.AdapterVersion, m.Version))
	}
	if ms, err := reg.Resolve(m.SchemaVersion); err == nil && ms.Version != s.Version {
		add("schema_version", fmt.Sprintf("the batch uses canonical schema %s but the manifest declares %s", s.Version, ms.Version))
	}
	if !m.SupportsMode(b.Mode()) {
		add("batch.mode", fmt.Sprintf("mode %q is not declared in the manifest's modes", b.Mode()))
	}
	return out
}

// ValidateSync checks the batches of one sync together: a full sync must be a
// single batch, because each full batch deletes the records the others sent.
func ValidateSync(batches []adapter.Batch) []Finding {
	full := 0
	for _, b := range batches {
		if b.Mode() == adapter.ModeFull {
			full++
		}
	}
	if full > 1 {
		return []Finding{{Check: CheckFullScope, Index: -1, Message: fmt.Sprintf(
			"%d full-mode batches in one sync: each full batch deletes the records the others sent; send a full sync as one batch", full)}}
	}
	return nil
}

type entry struct {
	entity string
	index  int
	ref    string
	rec    adapter.Record
	json   string
}

func records(reg *schema.Registry, batches []adapter.Batch) map[string]entry {
	out := map[string]entry{}
	for _, b := range batches {
		s, err := reg.Resolve(b.SchemaVersion)
		if err != nil {
			continue
		}
		for name, recs := range b.Entities {
			e, ok := s.Entity(name)
			if !ok {
				continue
			}
			for i, raw := range recs {
				rec, err := normalize(raw)
				if err != nil {
					continue
				}
				key, ok := identity(e, rec)
				if !ok {
					continue
				}
				data, _ := json.Marshal(rec)
				out[name+"\x00"+key] = entry{entity: name, index: i, ref: adapter.MetaOf(rec).SourceRecordRef, rec: rec, json: string(data)}
			}
		}
	}
	return out
}

// CompareRuns compares two syncs of the same input: the second must not
// change anything the first committed.
func CompareRuns(reg *schema.Registry, m adapter.Manifest, first, second []adapter.Batch) []Finding {
	a, b := records(reg, first), records(reg, second)
	fullSecond := false
	for _, bt := range second {
		fullSecond = fullSecond || bt.Mode() == adapter.ModeFull
	}
	keys := sortedKeys(a)
	for k := range b {
		if _, ok := a[k]; !ok {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	var out []Finding
	for _, k := range keys {
		x, inFirst := a[k]
		y, inSecond := b[k]
		switch {
		case inFirst && inSecond && x.json != y.json:
			out = append(out, Finding{Check: CheckIdempotency, Batch: "run 2", Entity: y.entity, Index: y.index, SourceRecordRef: y.ref,
				Message: fmt.Sprintf("fields %s differ from the first run on the same input: the second sync would record an update", strings.Join(diff(x.rec, y.rec), ", "))})
		case !inFirst && inSecond:
			out = append(out, Finding{Check: CheckIdempotency, Batch: "run 2", Entity: y.entity, Index: y.index, SourceRecordRef: y.ref,
				Message: "the record is absent from the first run on the same input: the second sync would create it"})
		case inFirst && !inSecond && fullSecond && slices.Contains(sortedKeys(m.Supplies), x.entity):
			out = append(out, Finding{Check: CheckIdempotency, Batch: "run 1", Entity: x.entity, Index: x.index, SourceRecordRef: x.ref,
				Message: "the record is missing from the second full sync on the same input: it would be deleted"})
		}
	}
	return out
}

// normalize applies the engine's record normalization: numbers keep their
// exact text (json.Number) and top-level nulls are dropped (null = missing).
func normalize(rec adapter.Record) (adapter.Record, error) {
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

// identity encodes the record's identity values in x-identity-key order.
func identity(e *schema.Entity, rec adapter.Record) (string, bool) {
	vals := make([]any, len(e.IdentityKey))
	for i, k := range e.IdentityKey {
		v, ok := rec[k]
		if !ok {
			return "", false
		}
		vals[i] = v
	}
	data, err := json.Marshal(vals)
	return string(data), err == nil
}

func diff(a, b adapter.Record) []string {
	var out []string
	for _, k := range sortedKeys(mergeKeys(a, b)) {
		x, _ := json.Marshal(a[k])
		y, _ := json.Marshal(b[k])
		if !bytes.Equal(x, y) {
			out = append(out, k)
		}
	}
	return out
}

func mergeKeys(a, b adapter.Record) map[string]bool {
	out := map[string]bool{}
	for k := range a {
		out[k] = true
	}
	for k := range b {
		out[k] = true
	}
	return out
}

func fieldNames(rec adapter.Record) []string {
	var out []string
	for k := range rec {
		if k != schema.MetaField {
			out = append(out, k)
		}
	}
	sort.Strings(out)
	return out
}

func sortedKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
