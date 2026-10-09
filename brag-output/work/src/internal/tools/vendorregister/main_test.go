// SPDX-License-Identifier: Apache-2.0

package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/nexops-one/compliance-engine/sdk/adapter"
	"github.com/nexops-one/compliance-engine/sdk/schema"
)

func loadFacts(t *testing.T) Facts {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", "..", Dir, "vendor-facts.json"))
	if err != nil {
		t.Fatal(err)
	}
	var f Facts
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&f); err != nil {
		t.Fatal(err)
	}
	return f
}

// TestRegisterUpToDate fails when the committed files differ from what the
// facts generate (run go run ./internal/tools/vendorregister).
func TestRegisterUpToDate(t *testing.T) {
	files, _, err := Generate(loadFacts(t))
	if err != nil {
		t.Fatal(err)
	}
	for name, want := range files {
		got, err := os.ReadFile(filepath.Join("..", "..", "..", Dir, name))
		if err != nil || !bytes.Equal(bytes.ReplaceAll(got, []byte("\r\n"), []byte("\n")), want) {
			t.Errorf("%s is out of date: run go run ./internal/tools/vendorregister", name)
		}
	}
}

func validate(t *testing.T, files map[string][]byte) {
	t.Helper()
	reg, err := schema.Default()
	if err != nil {
		t.Fatal(err)
	}
	var m adapter.Manifest
	if err := json.Unmarshal(files["manifest.json"], &m); err != nil {
		t.Fatal(err)
	}
	if errs := schema.ValidateManifestDocument(files["manifest.json"]); len(errs) != 0 {
		t.Fatalf("manifest document: %+v", errs)
	}
	if err := m.Validate(reg); err != nil {
		t.Fatalf("manifest: %v", err)
	}
	b, err := adapter.DecodeBatch(bytes.NewReader(files["nexops-one-register.json"]))
	if err != nil {
		t.Fatal(err)
	}
	s, err := reg.Resolve(b.SchemaVersion)
	if err != nil {
		t.Fatal(err)
	}
	if errs := b.ValidateEnvelope(s); len(errs) != 0 {
		t.Fatalf("envelope: %+v", errs)
	}
	for entity, recs := range b.Entities {
		for _, r := range recs {
			if errs := s.ValidateRecord(entity, r); len(errs) != 0 {
				t.Fatalf("%s: %+v", entity, errs)
			}
			for f := range r {
				if f != schema.MetaField && !m.SuppliesField(entity, f) {
					t.Errorf("%s.%s is not in the manifest", entity, f)
				}
			}
		}
	}
	if b.Source.Adapter != m.Name || b.Mode() != adapter.ModeFull {
		t.Fatalf("batch source %+v, mode %s", b.Source, b.Mode())
	}
}

func TestRegisterIsValid(t *testing.T) {
	files, pending, err := Generate(loadFacts(t))
	if err != nil {
		t.Fatal(err)
	}
	validate(t, files)
	if !bytes.Contains(files["README.md"], []byte("## Pending facts")) || len(pending) == 0 && bytes.Contains(files["README.md"], []byte("| `")) {
		t.Fatalf("README pending section:\n%s", files["README.md"])
	}
}

// TestStatedFactsAreUsed: a stated fact goes into the record; an LEI becomes
// the provider code with its type; a null fact stays out.
func TestStatedFactsAreUsed(t *testing.T) {
	f := loadFacts(t)
	lei, name, country := "529900T8BM49AURSDO55", "Example Vendor SAS", "FR"
	f.Provider["lei"] = Fact{Value: &lei}
	f.Provider["legal_name"] = Fact{Value: &name}
	f.Provider["hq_country"] = Fact{Value: &country}
	files, pending, err := Generate(f)
	if err != nil {
		t.Fatal(err)
	}
	validate(t, files)
	var b struct {
		Entities map[string][]map[string]any `json:"entities"`
	}
	_ = json.Unmarshal(files["nexops-one-register.json"], &b)
	rec := b.Entities["ict_provider"][0]
	if rec["provider_id_code"] != lei || rec["provider_id_type"] != "LEI" || rec["legal_name"] != name || rec["hq_country"] != country {
		t.Fatalf("record = %v", rec)
	}
	if _, ok := rec["currency"]; ok {
		t.Fatal("a null fact must stay out of the batch")
	}
	for _, p := range pending {
		if p == "lei" || p == "legal_name" || p == "hq_country" {
			t.Fatalf("stated fact %s listed as pending: %v", p, pending)
		}
	}
	f.Provider["mystery"] = Fact{}
	if _, _, err := Generate(f); err == nil {
		t.Fatal("an unknown fact must be refused")
	}
}
