// SPDX-License-Identifier: Apache-2.0

package schema_test

import (
	"errors"
	"reflect"
	"testing"

	"github.com/nexops-one/compliance-engine/sdk/schema"
)

func latest(t *testing.T) *schema.Schema {
	t.Helper()
	reg, err := schema.Default()
	if err != nil {
		t.Fatal(err)
	}
	return reg.Latest()
}

func TestDefaultRegistryLoadsEmbeddedSchema(t *testing.T) {
	reg, err := schema.Default()
	if err != nil {
		t.Fatal(err)
	}
	if got := reg.Versions(); !reflect.DeepEqual(got, []string{"0.1.0"}) {
		t.Fatalf("versions = %v", got)
	}
	s := reg.Latest()
	if s.Version != "0.1.0" || len(s.EntityNames()) != 14 {
		t.Fatalf("latest = %s with %d entities", s.Version, len(s.EntityNames()))
	}
}

func TestEntityDescriptors(t *testing.T) {
	s := latest(t)
	line, ok := s.Entity("arrangement_service_line")
	if !ok {
		t.Fatal("arrangement_service_line missing")
	}
	wantKey := []string{"arrangement_ref", "financial_entity_lei", "provider_id_code", "function_id", "ict_service_type"}
	if !reflect.DeepEqual(line.IdentityKey, wantKey) || line.Template != "B_02.02" {
		t.Fatalf("identity %v template %s", line.IdentityKey, line.Template)
	}
	if _, meta := line.Fields[schema.MetaField]; meta {
		t.Fatal("_meta must not be a descriptor field")
	}
	loc := line.Fields["data_at_rest_country"]
	if loc.RoIRef != "B_02.02.0150" || loc.Kind != schema.KindCountry || !loc.RoIRequired || loc.Type != "string" {
		t.Fatalf("data_at_rest_country = %+v", loc)
	}
	lei := line.Fields["financial_entity_lei"]
	if lei.Kind != schema.KindLEI || lei.References != "financial_entity.lei" || !lei.IsIdentity() {
		t.Fatalf("financial_entity_lei = %+v", lei)
	}
	if line.Fields["ict_service_type"].Codelist != "ict_service_type" {
		t.Fatal("ict_service_type codelist not parsed")
	}
	_, cur, _ := s.Field("contractual_arrangement.currency")
	if cur.Kind != schema.KindCurrency {
		t.Fatalf("currency kind = %s", cur.Kind)
	}
	_, parent, _ := s.Field("ict_provider.parent_id_code")
	if !parent.RoIConditional {
		t.Fatal("parent_id_code should be conditional")
	}
	_, rto, _ := s.Field("function.rto")
	if rto.Type != "integer" || rto.Kind != schema.KindPlain {
		t.Fatalf("rto = %+v", rto)
	}
}

func TestFieldPath(t *testing.T) {
	s := latest(t)
	if _, _, ok := s.Field("function.rto"); !ok {
		t.Error("function.rto should resolve")
	}
	for _, bad := range []string{"function.nope", "nope.rto", "function", ""} {
		if _, _, ok := s.Field(bad); ok {
			t.Errorf("%q should not resolve", bad)
		}
	}
}

func TestResolveAndMatch(t *testing.T) {
	reg, _ := schema.Default()
	for _, v := range []string{"0.1.0", "0.1.7"} {
		s, err := reg.Resolve(v)
		if err != nil || s.Version != "0.1.0" {
			t.Errorf("Resolve(%s) = %v, %v", v, s, err)
		}
	}
	for _, v := range []string{"0.2.0", "1.0.0", "x"} {
		if _, err := reg.Resolve(v); !errors.Is(err, schema.ErrUnsupportedVersion) {
			t.Errorf("Resolve(%s) err = %v", v, err)
		}
	}
	if s, err := reg.Match("^0.1"); err != nil || s.Version != "0.1.0" {
		t.Errorf("Match(^0.1) = %v, %v", s, err)
	}
	if _, err := reg.Match("^0.2"); !errors.Is(err, schema.ErrUnsupportedVersion) {
		t.Errorf("Match(^0.2) err = %v", err)
	}
}
