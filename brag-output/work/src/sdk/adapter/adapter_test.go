// SPDX-License-Identifier: Apache-2.0

package adapter_test

import (
	"strings"
	"testing"

	"github.com/nexops-one/compliance-engine/sdk/adapter"
	"github.com/nexops-one/compliance-engine/sdk/schema"
)

func registry(t *testing.T) *schema.Registry {
	t.Helper()
	reg, err := schema.Default()
	if err != nil {
		t.Fatal(err)
	}
	return reg
}

func validManifest() adapter.Manifest {
	return adapter.Manifest{
		Name: "csv-import", Version: "0.1.0", SchemaVersion: "0.1.0",
		Supplies: map[string][]string{"ict_provider": {"provider_id_code", "legal_name", "hq_country"}},
		Modes:    []adapter.Mode{adapter.ModeIncremental, adapter.ModeFull},
	}
}

func TestScopeValidate(t *testing.T) {
	if err := (adapter.Scope{TenantID: "t", WorkspaceID: "w"}).Validate(); err != nil {
		t.Fatal(err)
	}
	if err := (adapter.Scope{TenantID: "t"}).Validate(); err == nil {
		t.Fatal("missing workspace must fail")
	}
}

func TestManifestValidate(t *testing.T) {
	reg := registry(t)
	if err := validManifest().Validate(reg); err != nil {
		t.Fatalf("valid manifest: %v", err)
	}
	cases := map[string]func(*adapter.Manifest){
		"name required":         func(m *adapter.Manifest) { m.Name = "" },
		"version required":      func(m *adapter.Manifest) { m.Version = "" },
		"unsupported schema":    func(m *adapter.Manifest) { m.SchemaVersion = "9.0.0" },
		"unknown entity":        func(m *adapter.Manifest) { m.Supplies["nope"] = []string{"x"} },
		"unknown field":         func(m *adapter.Manifest) { m.Supplies["ict_provider"] = append(m.Supplies["ict_provider"], "nope") },
		"identity not supplied": func(m *adapter.Manifest) { m.Supplies["ict_provider"] = []string{"legal_name"} },
		"no modes":              func(m *adapter.Manifest) { m.Modes = nil },
		"bad mode":              func(m *adapter.Manifest) { m.Modes = []adapter.Mode{"sometimes"} },
	}
	for name, mutate := range cases {
		m := validManifest()
		m.Supplies = map[string][]string{"ict_provider": {"provider_id_code", "legal_name", "hq_country"}}
		mutate(&m)
		if err := m.Validate(reg); err == nil {
			t.Errorf("%s: expected error", name)
		}
	}
	m := validManifest()
	if !m.SuppliesField("ict_provider", "legal_name") || m.SuppliesField("ict_provider", "person_type") || m.SuppliesField("function", "rto") {
		t.Error("SuppliesField wrong")
	}
	if !m.SupportsMode(adapter.ModeFull) || m.SupportsMode("other") {
		t.Error("SupportsMode wrong")
	}
}

func TestDecodeBatchStrictEnvelope(t *testing.T) {
	good := `{"schema_version":"0.1.0","source":{"system":"s","adapter":"a","adapter_version":"1"},"entities":{"function":[{"function_id":"F","financial_entity_lei":"SAMPLEFE000000000021","rto":240}]}}`
	b, err := adapter.DecodeBatch(strings.NewReader(good))
	if err != nil {
		t.Fatal(err)
	}
	if b.Mode() != adapter.ModeIncremental || b.RecordCount() != 1 || b.BatchID() != "" {
		t.Fatalf("defaults wrong: %s %d %q", b.Mode(), b.RecordCount(), b.BatchID())
	}
	if got := b.Entities["function"][0]["rto"]; got == nil || got.(interface{ String() string }).String() != "240" {
		t.Fatalf("rto should keep full precision as json.Number, got %#v", got)
	}
	bad := `{"schema_version":"0.1.0","source":{"system":"s","adapter":"a","adapter_version":"1"},"entities":{},"extra":1}`
	if _, err := adapter.DecodeBatch(strings.NewReader(bad)); err == nil {
		t.Fatal("unknown envelope field must be rejected")
	}
}

func TestValidateEnvelope(t *testing.T) {
	s := registry(t).Latest()
	b := adapter.Batch{SchemaVersion: "0.1.0", Source: adapter.Source{System: "s", Adapter: "a", AdapterVersion: "1"}, Entities: map[string][]adapter.Record{"function": {}}}
	if errs := b.ValidateEnvelope(s); len(errs) != 0 {
		t.Fatalf("valid envelope: %+v", errs)
	}
	b.Source.Adapter = ""
	b.Entities["nope"] = nil
	b.Batch = &adapter.BatchInfo{Mode: "weekly"}
	codes := map[string]bool{}
	for _, e := range b.ValidateEnvelope(s) {
		codes[e.Field+":"+e.Code] = true
	}
	for _, want := range []string{"source.adapter:required", "entities.nope:unknown_entity", "batch.mode:enum"} {
		if !codes[want] {
			t.Errorf("missing %s in %v", want, codes)
		}
	}
}

func TestMetaHelpers(t *testing.T) {
	rec := adapter.Record{"provider_id_code": "P1"}
	adapter.SetSourceRef(rec, "row-7")
	adapter.MarkNotApplicable(rec, "parent_id_code")
	adapter.MarkDerived(rec, adapter.DerivedField{Field: "hq_country", Method: "region-to-country", SourceRef: "cloud_resource:r1"})
	m := adapter.MetaOf(rec)
	if m.SourceRecordRef != "row-7" || len(m.NotApplicableFields) != 1 || m.DerivedFields[0].Method != "region-to-country" {
		t.Fatalf("meta = %+v", m)
	}
	s := registry(t).Latest()
	rec["hq_country"] = "IE"
	if errs := s.ValidateRecord("ict_provider", rec); errs != nil {
		t.Fatalf("meta must be schema-valid: %+v", errs)
	}
	if got := adapter.MetaOf(adapter.Record{}); len(got.DerivedFields) != 0 || got.SourceRecordRef != "" {
		t.Fatalf("empty meta = %+v", got)
	}
}

func TestBuilder(t *testing.T) {
	m := validManifest()
	b := adapter.NewBuilder(m, "vendor-sheet").Mode(adapter.ModeFull).BatchID("b-1").
		Add("ict_provider", adapter.Record{"provider_id_code": "P1", "legal_name": "Provider One"}).
		Build()
	if b.SchemaVersion != "0.1.0" || b.Source != (adapter.Source{System: "vendor-sheet", Adapter: "csv-import", AdapterVersion: "0.1.0"}) {
		t.Fatalf("envelope = %+v", b)
	}
	if b.Mode() != adapter.ModeFull || b.BatchID() != "b-1" || b.RecordCount() != 1 {
		t.Fatalf("batch info wrong: %+v", b.Batch)
	}
	if errs := b.ValidateEnvelope(registry(t).Latest()); len(errs) != 0 {
		t.Fatalf("builder output invalid: %+v", errs)
	}
}
