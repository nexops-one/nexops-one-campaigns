// SPDX-License-Identifier: Apache-2.0

package engine_test

import (
	"reflect"
	"testing"

	"github.com/nexops-one/compliance-engine/pkg/catalog"
	"github.com/nexops-one/compliance-engine/pkg/engine"
	"github.com/nexops-one/compliance-engine/sdk/adapter"
)

func TestDerivedFilterValueBlocksInsteadOfExcluding(t *testing.T) {
	ctl := catalog.Control{
		ID: "critical-exit", Requires: []string{"ict_provider.person_type", "ict_provider.legal_name"},
		Rule: catalog.Rule{
			Kind: catalog.KindFieldsComplete, Entity: "ict_provider",
			Filter: []catalog.Condition{{Field: "person_type", In: []any{"LEGAL"}}},
		},
	}
	derived := adapter.Record{"provider_id_code": "P1", "person_type": "NATURAL"}
	adapter.MarkDerived(derived, adapter.DerivedField{Field: "person_type", Method: "guess"})
	complete := adapter.Record{"provider_id_code": "P2", "person_type": "LEGAL", "legal_name": "Two"}
	r := only(t, input(t, map[string][]adapter.Record{"ict_provider": {derived, complete}}, ctl))
	want := []engine.Blocker{{Reason: engine.ReasonDerived, Entity: "ict_provider", Key: `["P1"]`, Field: "person_type"}}
	if r.Status != engine.StatusNotAssessed || !reflect.DeepEqual(r.Blockers, want) {
		t.Fatalf("an unconfirmed filter value must block, not silently exclude: %+v", r)
	}
}

func TestFieldEqualsOverOnlyNotApplicableValuesIsNotAssessed(t *testing.T) {
	ctl := catalog.Control{
		ID: "exit", Requires: []string{"service_assessment.exit_plan_exists"},
		Rule: catalog.Rule{Kind: catalog.KindFieldEquals, Entity: "service_assessment", Field: "exit_plan_exists", Value: true},
	}
	rec := adapter.Record{"arrangement_ref": "A1", "provider_id_code": "P1", "ict_service_type": "S1"}
	adapter.MarkNotApplicable(rec, "exit_plan_exists")
	r := only(t, input(t, map[string][]adapter.Record{"service_assessment": {rec}}, ctl))
	want := []engine.Blocker{{Reason: engine.ReasonNoRecords, Entity: "service_assessment"}}
	if r.Status != engine.StatusNotAssessed || !reflect.DeepEqual(r.Blockers, want) {
		t.Fatalf("a rule that compared no values must not pass: %+v", r)
	}
}

func TestExplanationDoesNotAliasCatalog(t *testing.T) {
	ctl := catalog.Control{
		ID: "names", Requires: []string{"ict_provider.legal_name"},
		Rule: catalog.Rule{Kind: catalog.KindFieldIn, Entity: "ict_provider", Field: "legal_name", Values: []any{"One"}},
	}
	in := input(t, map[string][]adapter.Record{"ict_provider": {{"provider_id_code": "P1", "legal_name": "One"}}}, ctl)
	r := engine.Evaluate(in).Frameworks[0].Controls[0]
	r.Explanation.Inputs[0] = "mutated"
	r.Explanation.Rule.Values[0] = "mutated"
	got := in.Catalogs[0].Controls[0]
	if got.Requires[0] != "ict_provider.legal_name" || got.Rule.Values[0] != "One" {
		t.Fatalf("mutating a result changed the catalog: %+v", got)
	}
}
