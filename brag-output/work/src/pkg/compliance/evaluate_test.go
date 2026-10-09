// SPDX-License-Identifier: Apache-2.0

package compliance_test

import (
	"encoding/json"
	"errors"
	"reflect"
	"testing"

	"github.com/nexops-one/compliance-engine/pkg/canonical"
	"github.com/nexops-one/compliance-engine/pkg/catalog"
	"github.com/nexops-one/compliance-engine/pkg/compliance"
	"github.com/nexops-one/compliance-engine/pkg/engine"
	"github.com/nexops-one/compliance-engine/pkg/store"
	"github.com/nexops-one/compliance-engine/sdk/adapter"
)

var dora = catalog.Ref{Catalog: "dora", Version: "1.0.0"}

const (
	lineKey       = `["CTR-2024-017","SAMPLEFE000000000001","SAMPLETP000000000002","F-001","EXAMPLE_CODE"]`
	assessmentKey = `["CTR-2024-017","SAMPLETP000000000002","EXAMPLE_CODE"]`
)

func controls(ev compliance.Evaluation) map[string]engine.ControlResult {
	out := map[string]engine.ControlResult{}
	for _, fw := range ev.Result.Frameworks {
		for _, c := range fw.Controls {
			out[c.ControlID] = c
		}
	}
	return out
}

func TestEvaluateSampleAgainstDORA(t *testing.T) {
	eng := newEngine(t)
	loadSample(t, eng)
	ev, err := eng.Evaluate(ctx, scopeA, "", []catalog.Ref{dora})
	if err != nil {
		t.Fatal(err)
	}
	if ev.SnapshotID != "rev-1" || ev.Result.SnapshotID != "rev-1" || !reflect.DeepEqual(ev.Catalogs, []catalog.Ref{dora}) {
		t.Fatalf("evaluation header = %+v", ev)
	}
	want := map[string]engine.Status{
		"dora-roi-reporting-entity":        engine.StatusMonitoring,
		"dora-roi-provider-identification": engine.StatusMonitoring,
		"dora-roi-ultimate-parent":         engine.StatusNotAssessed,
		"dora-roi-data-location":           engine.StatusNotAssessed,
		"dora-roi-arrangement-dates":       engine.StatusNotAssessed,
		"dora-roi-signatories":             engine.StatusNotAssessed,
		"dora-supply-chain-visibility":     engine.StatusNotAssessed,
		"dora-function-criticality":        engine.StatusMonitoring,
		"dora-substitutability-assessed":   engine.StatusMonitoring,
		"dora-exit-plans":                  engine.StatusInReview,
		"dora-incident-readiness":          engine.StatusNotAssessed,
	}
	got := map[string]engine.Status{}
	for id, c := range controls(ev) {
		got[id] = c.Status
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("statuses = %v", got)
	}
	tally := ev.Result.Frameworks[0].Tally
	if tally.InScope != 11 || tally.Assessable != 5 || tally.ScorePct != 80 || tally.CoveragePct != 45 || !tally.ScoreDefined || tally.Assumptions == "" {
		t.Fatalf("tally = %+v", tally)
	}
}

func TestEvaluateExplainsBlockers(t *testing.T) {
	eng := newEngine(t)
	loadSample(t, eng)
	ev, err := eng.Evaluate(ctx, scopeA, "", []catalog.Ref{dora})
	if err != nil {
		t.Fatal(err)
	}
	c := controls(ev)
	cases := map[string][]engine.Blocker{
		"dora-roi-data-location":       {{Reason: engine.ReasonDerived, Entity: "arrangement_service_line", Key: lineKey, Field: "data_at_rest_country"}},
		"dora-roi-arrangement-dates":   {{Reason: engine.ReasonNotSupplied, Entity: "arrangement_service_line", Key: lineKey, Field: "end_date"}},
		"dora-roi-ultimate-parent":     {{Reason: engine.ReasonMissing, Entity: "ict_provider", Key: `["SAMPLETP000000000003"]`, Field: "parent_id_code"}},
		"dora-supply-chain-visibility": {{Reason: engine.ReasonNoRecords, Entity: "supply_chain_link"}},
		"dora-incident-readiness":      {{Reason: engine.ReasonManual}},
	}
	for id, want := range cases {
		if !reflect.DeepEqual(c[id].Blockers, want) {
			t.Errorf("%s blockers = %+v", id, c[id].Blockers)
		}
	}
	if !reflect.DeepEqual(c["dora-exit-plans"].Explanation.Failing, []string{assessmentKey}) {
		t.Errorf("exit plans failing = %v", c["dora-exit-plans"].Explanation.Failing)
	}
}

func TestEvaluateDefaultsToLatestCatalogs(t *testing.T) {
	eng := newEngine(t)
	loadSample(t, eng)
	ev, err := eng.Evaluate(ctx, scopeA, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	var refs []string
	for _, fw := range ev.Result.Frameworks {
		refs = append(refs, fw.Catalog.String())
	}
	if !reflect.DeepEqual(refs, []string{"aiact@1.0.0", "dora@1.0.0", "gdpr@1.0.0"}) {
		t.Fatalf("frameworks = %v", refs)
	}
	o := ev.Result.Overall
	if o.InScope != 17 || o.Assessable != 9 || o.Monitoring != 8 || o.InReview != 1 || o.ScorePct != 88 || o.CoveragePct != 52 {
		t.Fatalf("overall = %+v", o)
	}
}

func TestEvaluationIsPersistedAndReproducible(t *testing.T) {
	eng := newEngine(t)
	loadSample(t, eng)
	first, err := eng.Evaluate(ctx, scopeA, "rev-1", nil)
	if err != nil {
		t.Fatal(err)
	}
	stored, err := eng.Evaluation(ctx, scopeA, first.ID)
	if err != nil {
		t.Fatal(err)
	}
	second, _ := eng.Evaluate(ctx, scopeA, "rev-1", nil)
	a, _ := json.Marshal(first.Result)
	b, _ := json.Marshal(stored.Result)
	c, _ := json.Marshal(second.Result)
	if string(a) != string(b) || string(a) != string(c) || first.ID == second.ID {
		t.Fatal("an evaluation must be stored intact and reproducible from its snapshot")
	}
	if _, err := eng.Evaluation(ctx, scopeB, first.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("evaluations must be scoped: %v", err)
	}
	if _, err := eng.Evaluate(ctx, scopeA, "", []catalog.Ref{{Catalog: "dora", Version: "9.0.0"}}); !errors.Is(err, catalog.ErrUnknownCatalog) {
		t.Fatalf("unknown catalog err = %v", err)
	}
}

func TestEvaluateEmptyWorkspaceIsHonest(t *testing.T) {
	ev, err := newEngine(t).Evaluate(ctx, scopeA, "", []catalog.Ref{dora})
	if err != nil {
		t.Fatal(err)
	}
	tally := ev.Result.Frameworks[0].Tally
	if ev.SnapshotID != "rev-0" || tally.Assessable != 0 || tally.ScoreDefined || tally.CoveragePct != 0 {
		t.Fatalf("an empty workspace must be entirely not_assessed: %+v", tally)
	}
}

func TestCompletenessOnSample(t *testing.T) {
	eng := newEngine(t)
	loadSample(t, eng)
	c, err := eng.Completeness(ctx, scopeA, "")
	if err != nil {
		t.Fatal(err)
	}
	if c.SnapshotID != "rev-1" {
		t.Fatalf("snapshot = %s", c.SnapshotID)
	}
	var endDate *canonical.Gap
	for i, g := range c.Gaps {
		if g.Entity == "arrangement_service_line" && g.Field == "end_date" {
			endDate = &c.Gaps[i]
		}
	}
	if endDate == nil || endDate.State != canonical.StateMissing || endDate.Supplied || !endDate.Conditional {
		t.Fatalf("end_date gap = %+v", endDate)
	}
	found := false
	for _, n := range c.UnverifiedCodelists {
		found = found || n == "entity_type"
	}
	if !found {
		t.Fatalf("unverified codelists = %v", c.UnverifiedCodelists)
	}
	if len(c.References.Dangling) != 2 {
		t.Fatalf("dangling = %+v", c.References.Dangling)
	}
	for _, d := range c.References.Dangling {
		if d.Target != "ict_service_type.id" {
			t.Fatalf("unexpected dangling reference %+v", d)
		}
	}
}

func TestNullFieldIsMissingInCompleteness(t *testing.T) {
	eng := newEngine(t)
	m := adapter.Manifest{Name: "t", Version: "1", SchemaVersion: "0.1.0", Modes: []adapter.Mode{adapter.ModeIncremental},
		Supplies: map[string][]string{"function": {"function_id", "financial_entity_lei", "rto"}}}
	if err := eng.RegisterManifest(ctx, scopeA, m); err != nil {
		t.Fatal(err)
	}
	b := adapter.NewBuilder(m, "test").Add("function", adapter.Record{"function_id": "F1", "financial_entity_lei": "SAMPLEFE000000000021", "rto": nil}).Build()
	res, err := eng.Ingest(ctx, scopeA, b)
	if err != nil || res.Accepted != 1 {
		t.Fatalf("res = %+v, %v", res, err)
	}
	c, _ := eng.Completeness(ctx, scopeA, "")
	for _, g := range c.Gaps {
		if g.Entity == "function" && g.Field == "rto" && g.State == canonical.StateMissing && g.Supplied {
			return
		}
	}
	t.Fatalf("rto must be a missing (not rejected) field: %+v", c.Gaps)
}

func TestCatalogAccessAndDiff(t *testing.T) {
	eng := newEngine(t)
	c, err := eng.Catalog(dora)
	if err != nil || len(c.Controls) != 11 {
		t.Fatalf("catalog = %v, %v", c, err)
	}
	d, err := eng.CatalogDiff(dora, dora)
	if err != nil || len(d.Controls) != 0 {
		t.Fatalf("diff = %+v, %v", d, err)
	}
	if _, err := eng.CatalogDiff(dora, catalog.Ref{Catalog: "dora", Version: "2.0.0"}); !errors.Is(err, catalog.ErrUnknownCatalog) {
		t.Fatalf("err = %v", err)
	}
}
