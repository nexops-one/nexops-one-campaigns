// SPDX-License-Identifier: Apache-2.0

package engine_test

import (
	"reflect"
	"testing"

	"github.com/nexops-one/compliance-engine/pkg/canonical"
	"github.com/nexops-one/compliance-engine/pkg/catalog"
	"github.com/nexops-one/compliance-engine/pkg/engine"
	"github.com/nexops-one/compliance-engine/sdk/adapter"
	"github.com/nexops-one/compliance-engine/sdk/schema"
)

func input(t *testing.T, data map[string][]adapter.Record, controls ...catalog.Control) engine.Input {
	t.Helper()
	reg, err := schema.Default()
	if err != nil {
		t.Fatal(err)
	}
	s := reg.Latest()
	var recs []canonical.SnapshotRecord
	for entity, list := range data {
		e, _ := s.Entity(entity)
		for _, r := range list {
			n, _ := canonical.Normalize(r)
			key, err := canonical.Key(e, n)
			if err != nil {
				t.Fatal(err)
			}
			recs = append(recs, canonical.SnapshotRecord{Entity: entity, Key: key, Data: n})
		}
	}
	ix := canonical.NewIndex(s, recs)
	cat := &catalog.Catalog{
		Catalog: "test", Version: "1.0.0", Framework: "TEST",
		Scoring:  catalog.Scoring{CountsAsReady: []string{"ready", "monitoring"}, Assumptions: "test assumptions"},
		Controls: controls,
	}
	return engine.Input{SnapshotID: "rev-1", Index: ix, References: canonical.CheckReferences(ix), Catalogs: []*catalog.Catalog{cat}}
}

func only(t *testing.T, in engine.Input) engine.ControlResult {
	t.Helper()
	res := engine.Evaluate(in)
	if len(res.Frameworks) != 1 || len(res.Frameworks[0].Controls) != 1 {
		t.Fatalf("unexpected result shape: %+v", res)
	}
	return res.Frameworks[0].Controls[0]
}

var providerNames = catalog.Control{
	ID: "names", Title: "Providers are named",
	Requires: []string{"ict_provider.legal_name", "ict_provider.hq_country"},
	Rule:     catalog.Rule{Kind: catalog.KindFieldsComplete, Entity: "ict_provider"},
}

func TestFieldsCompleteIsMonitoringWhenAllProvided(t *testing.T) {
	r := only(t, input(t, map[string][]adapter.Record{"ict_provider": {
		{"provider_id_code": "P1", "legal_name": "One", "hq_country": "IE"},
	}}, providerNames))
	if r.Status != engine.StatusMonitoring || len(r.Blockers) != 0 {
		t.Fatalf("result = %+v", r)
	}
	if !reflect.DeepEqual(r.Explanation.RecordsExamined, []string{`["P1"]`}) || !reflect.DeepEqual(r.Explanation.Inputs, providerNames.Requires) {
		t.Fatalf("explanation = %+v", r.Explanation)
	}
}

func TestMissingFieldBlocksWithSupplyAwareReason(t *testing.T) {
	in := input(t, map[string][]adapter.Record{"ict_provider": {
		{"provider_id_code": "P1", "legal_name": "One"},
		{"provider_id_code": "P2", "hq_country": "IE"},
	}}, providerNames)
	in.Supplied = func(entity, field string) bool { return field != "hq_country" }
	r := only(t, in)
	want := []engine.Blocker{
		{Reason: engine.ReasonNotSupplied, Entity: "ict_provider", Key: `["P1"]`, Field: "hq_country"},
		{Reason: engine.ReasonMissing, Entity: "ict_provider", Key: `["P2"]`, Field: "legal_name"},
	}
	if r.Status != engine.StatusNotAssessed || !reflect.DeepEqual(r.Blockers, want) {
		t.Fatalf("result = %+v", r)
	}
}

func TestNotApplicableFieldDoesNotBlock(t *testing.T) {
	rec := adapter.Record{"provider_id_code": "P1", "legal_name": "One"}
	adapter.MarkNotApplicable(rec, "hq_country")
	r := only(t, input(t, map[string][]adapter.Record{"ict_provider": {rec}}, providerNames))
	if r.Status != engine.StatusMonitoring {
		t.Fatalf("result = %+v", r)
	}
}

func TestDerivedBlocksUnlessAccepted(t *testing.T) {
	rec := adapter.Record{"provider_id_code": "P1", "legal_name": "One", "hq_country": "IE"}
	adapter.MarkDerived(rec, adapter.DerivedField{Field: "hq_country", Method: "region-to-country"})
	data := map[string][]adapter.Record{"ict_provider": {rec}}
	r := only(t, input(t, data, providerNames))
	if r.Status != engine.StatusNotAssessed || r.Blockers[0].Reason != engine.ReasonDerived {
		t.Fatalf("derived must block by default: %+v", r)
	}
	accepting := providerNames
	accepting.AcceptDerived = true
	if r := only(t, input(t, data, accepting)); r.Status != engine.StatusMonitoring {
		t.Fatalf("accept_derived must allow derived values: %+v", r)
	}
}

func TestDanglingReferenceBlocks(t *testing.T) {
	ctl := catalog.Control{
		ID: "parents", Requires: []string{"ict_provider.parent_id_code"},
		Rule: catalog.Rule{Kind: catalog.KindReferencesResolved, Entity: "ict_provider", Fields: []string{"parent_id_code"}},
	}
	r := only(t, input(t, map[string][]adapter.Record{"ict_provider": {
		{"provider_id_code": "P1", "parent_id_code": "P9"},
	}}, ctl))
	if r.Status != engine.StatusNotAssessed || r.Blockers[0].Reason != engine.ReasonDangling {
		t.Fatalf("result = %+v", r)
	}
}

func TestNoRecordsIsNotAssessed(t *testing.T) {
	r := only(t, input(t, nil, providerNames))
	want := []engine.Blocker{{Reason: engine.ReasonNoRecords, Entity: "ict_provider"}}
	if r.Status != engine.StatusNotAssessed || !reflect.DeepEqual(r.Blockers, want) {
		t.Fatalf("an empty rule set must never pass: %+v", r)
	}
}

func TestFieldEqualsFailureIsInReview(t *testing.T) {
	ctl := catalog.Control{
		ID: "exit", Requires: []string{"service_assessment.exit_plan_exists"},
		Rule: catalog.Rule{Kind: catalog.KindFieldEquals, Entity: "service_assessment", Field: "exit_plan_exists", Value: true},
	}
	r := only(t, input(t, map[string][]adapter.Record{"service_assessment": {
		{"arrangement_ref": "A1", "provider_id_code": "P1", "ict_service_type": "S1", "exit_plan_exists": false},
		{"arrangement_ref": "A2", "provider_id_code": "P1", "ict_service_type": "S1", "exit_plan_exists": true},
	}}, ctl))
	if r.Status != engine.StatusInReview || r.Attention != engine.AttentionRuleFailed || !reflect.DeepEqual(r.Explanation.Failing, []string{`["A1","P1","S1"]`}) {
		t.Fatalf("result = %+v", r)
	}
}

func TestFieldInAndFilter(t *testing.T) {
	ctl := catalog.Control{
		ID: "eu-hq", Requires: []string{"ict_provider.hq_country", "ict_provider.person_type"},
		Rule: catalog.Rule{
			Kind: catalog.KindFieldIn, Entity: "ict_provider", Field: "hq_country", Values: []any{"IE", "LU"},
			Filter: []catalog.Condition{{Field: "person_type", Equals: "LEGAL"}},
		},
	}
	data := map[string][]adapter.Record{"ict_provider": {
		{"provider_id_code": "P1", "hq_country": "IE", "person_type": "LEGAL"},
		{"provider_id_code": "P2", "hq_country": "US", "person_type": "NATURAL"},
	}}
	if r := only(t, input(t, data, ctl)); r.Status != engine.StatusMonitoring || len(r.Explanation.RecordsExamined) != 1 {
		t.Fatalf("filter should exclude P2: %+v", r)
	}
	data["ict_provider"] = append(data["ict_provider"], adapter.Record{"provider_id_code": "P3", "hq_country": "US"})
	r := only(t, input(t, data, ctl))
	if r.Status != engine.StatusNotAssessed || r.Blockers[0].Field != "person_type" || r.Blockers[0].Key != `["P3"]` {
		t.Fatalf("missing filter field must block: %+v", r)
	}
}

func TestRecordsExistMin(t *testing.T) {
	ctl := catalog.Control{
		ID: "chain", Requires: []string{"supply_chain_link.rank"},
		Rule: catalog.Rule{Kind: catalog.KindRecordsExist, Entity: "supply_chain_link", Min: 2},
	}
	r := only(t, input(t, map[string][]adapter.Record{"supply_chain_link": {
		{"arrangement_ref": "A1", "ict_service_type": "S1", "provider_id_code": "P1", "rank": 1, "recipient_id_code": "P2"},
	}}, ctl))
	if r.Status != engine.StatusInReview {
		t.Fatalf("one record with min 2 must be in_review: %+v", r)
	}
}

func TestManualIsNotAssessed(t *testing.T) {
	r := only(t, input(t, nil, catalog.Control{ID: "m", Rule: catalog.Rule{Kind: catalog.KindManual}}))
	if r.Status != engine.StatusNotAssessed || r.Blockers[0].Reason != engine.ReasonManual {
		t.Fatalf("result = %+v", r)
	}
}

func TestTallyScoreAndCoverage(t *testing.T) {
	exit := catalog.Control{
		ID: "exit", Requires: []string{"service_assessment.exit_plan_exists"},
		Rule: catalog.Rule{Kind: catalog.KindFieldEquals, Entity: "service_assessment", Field: "exit_plan_exists", Value: true},
	}
	manual := catalog.Control{ID: "m", Rule: catalog.Rule{Kind: catalog.KindManual}}
	in := input(t, map[string][]adapter.Record{
		"ict_provider":       {{"provider_id_code": "P1", "legal_name": "One", "hq_country": "IE"}},
		"service_assessment": {{"arrangement_ref": "A1", "provider_id_code": "P1", "ict_service_type": "S1", "exit_plan_exists": false}},
	}, providerNames, exit, manual)
	res := engine.Evaluate(in)
	got := res.Frameworks[0].Tally
	want := engine.Tally{InScope: 3, Assessable: 2, Monitoring: 1, InReview: 1, NotAssessed: 1, ScoreNumerator: 1, ScorePct: 50, ScoreDefined: true, CoveragePct: 66, Assumptions: "test assumptions"}
	if got != want {
		t.Fatalf("tally = %+v", got)
	}
	overall := res.Overall
	overall.Assumptions = want.Assumptions
	if overall != want || res.SnapshotID != "rev-1" || res.SchemaVersion != "0.1.0" {
		t.Fatalf("overall = %+v", res.Overall)
	}
	empty := engine.Evaluate(input(t, nil, manual)).Frameworks[0].Tally
	if empty.ScoreDefined || empty.ScorePct != 0 || empty.CoveragePct != 0 {
		t.Fatalf("nothing assessable must leave the score undefined: %+v", empty)
	}
}
