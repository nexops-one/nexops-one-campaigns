// SPDX-License-Identifier: Apache-2.0

package canonical_test

import (
	"encoding/json"
	"testing"

	"github.com/nexops-one/compliance-engine/pkg/canonical"
	"github.com/nexops-one/compliance-engine/sdk/adapter"
)

func TestNormalizeDropsNullsAndUsesJSONNumbers(t *testing.T) {
	rec, err := canonical.Normalize(adapter.Record{"rto": 240, "end_date": nil, "name": "x"})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := rec["end_date"]; ok {
		t.Fatal("null field must be dropped")
	}
	if n, ok := rec["rto"].(json.Number); !ok || n.String() != "240" {
		t.Fatalf("rto = %#v", rec["rto"])
	}
}

func TestKeyFollowsIdentityOrder(t *testing.T) {
	s := latestSchema(t)
	line, _ := s.Entity("arrangement_service_line")
	rec := adapter.Record{
		"ict_service_type": "S1", "function_id": "F1", "provider_id_code": "P1",
		"financial_entity_lei": "L1", "arrangement_ref": "A1", "start_date": "2024-01-01",
	}
	key, err := canonical.Key(line, rec)
	if err != nil || key != `["A1","L1","P1","F1","S1"]` {
		t.Fatalf("key = %s, %v", key, err)
	}
	link, _ := s.Entity("supply_chain_link")
	n, _ := canonical.Normalize(adapter.Record{"arrangement_ref": "A1", "ict_service_type": "S1", "provider_id_code": "P1", "rank": 1, "recipient_id_code": "P2"})
	k1, _ := canonical.Key(link, n)
	k2, _ := canonical.Key(link, adapter.Record{"arrangement_ref": "A1", "ict_service_type": "S1", "provider_id_code": "P1", "rank": 1, "recipient_id_code": "P2"})
	if k1 != k2 {
		t.Fatalf("json.Number and int keys differ: %s vs %s", k1, k2)
	}
	if _, err := canonical.Key(line, adapter.Record{"arrangement_ref": "A1"}); err == nil {
		t.Fatal("missing identity field must fail")
	}
}

func TestHashIsStableAcrossKeyOrderAndNumberTypes(t *testing.T) {
	a, _ := canonical.Normalize(adapter.Record{"b": 1, "a": "x"})
	b := adapter.Record{"a": "x", "b": 1}
	if canonical.Hash(a) != canonical.Hash(b) {
		t.Fatal("hash must not depend on key order or number representation")
	}
	if canonical.Hash(a) == canonical.Hash(adapter.Record{"a": "y", "b": 1}) {
		t.Fatal("different content must hash differently")
	}
}

func TestViewFieldStates(t *testing.T) {
	rec := adapter.Record{"country": "IE", "region": "eu-west-1", "empty": ""}
	adapter.MarkNotApplicable(rec, "parent_id_code")
	adapter.MarkDerived(rec, adapter.DerivedField{Field: "country", Method: "region-to-country"})
	v := canonical.NewView(rec)
	cases := map[string]canonical.FieldState{
		"country":        canonical.StateDerived,
		"region":         canonical.StateProvided,
		"parent_id_code": canonical.StateNotApplicable,
		"empty":          canonical.StateMissing,
		"absent":         canonical.StateMissing,
	}
	for field, want := range cases {
		if got := v.State(field); got != want {
			t.Errorf("%s: got %s want %s", field, got, want)
		}
	}
	if d, ok := v.Derived("country"); !ok || d.Method != "region-to-country" {
		t.Errorf("Derived(country) = %+v, %v", d, ok)
	}
}

func TestScalarString(t *testing.T) {
	cases := []struct {
		in   any
		want string
		ok   bool
	}{
		{"x", "x", true}, {json.Number("1.5"), "1.5", true}, {3, "3", true}, {int64(4), "4", true},
		{2.0, "2", true}, {true, "true", true}, {map[string]any{}, "", false}, {nil, "", false},
	}
	for _, c := range cases {
		got, ok := canonical.ScalarString(c.in)
		if got != c.want || ok != c.ok {
			t.Errorf("ScalarString(%#v) = %q, %v", c.in, got, ok)
		}
	}
}
