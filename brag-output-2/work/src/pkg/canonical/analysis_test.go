// SPDX-License-Identifier: Apache-2.0

package canonical_test

import (
	"reflect"
	"slices"
	"testing"
	"testing/fstest"

	"github.com/nexops-one/compliance-engine/pkg/canonical"
	"github.com/nexops-one/compliance-engine/sdk/adapter"
	"github.com/nexops-one/compliance-engine/sdk/schema"
)

// index builds an Index from entity -> records, computing keys.
func index(t *testing.T, s *schema.Schema, data map[string][]adapter.Record) *canonical.Index {
	t.Helper()
	var recs []canonical.SnapshotRecord
	for entity, list := range data {
		e, ok := s.Entity(entity)
		if !ok {
			t.Fatalf("unknown entity %s", entity)
		}
		for _, r := range list {
			n, err := canonical.Normalize(r)
			if err != nil {
				t.Fatal(err)
			}
			key, err := canonical.Key(e, n)
			if err != nil {
				t.Fatal(err)
			}
			recs = append(recs, canonical.SnapshotRecord{Entity: entity, Key: key, Data: n})
		}
	}
	return canonical.NewIndex(s, recs)
}

func TestIndexRecordsSortedAndValues(t *testing.T) {
	s := latestSchema(t)
	ix := index(t, s, map[string][]adapter.Record{"ict_provider": {{"provider_id_code": "P2"}, {"provider_id_code": "P1"}}})
	recs := ix.Records("ict_provider")
	if len(recs) != 2 || recs[0].Key != `["P1"]` || recs[1].Key != `["P2"]` {
		t.Fatalf("records = %+v", recs)
	}
	if !ix.HasValue("ict_provider.provider_id_code", "P1") || ix.HasValue("ict_provider.provider_id_code", "P9") {
		t.Fatal("HasValue wrong")
	}
	if len(ix.Records("function")) != 0 {
		t.Fatal("absent entity must have no records")
	}
}

func TestCheckReferencesFindsDanglingAndCycles(t *testing.T) {
	s := latestSchema(t)
	ix := index(t, s, map[string][]adapter.Record{
		"ict_provider": {
			{"provider_id_code": "P1", "parent_id_code": "P2"},
			{"provider_id_code": "P2", "parent_id_code": "P1"},
			{"provider_id_code": "P3", "parent_id_code": "P9"},
			{"provider_id_code": "P4"},
		},
		"cloud_resource": {{"resource_ref": "r1", "provider_id_code": "P4"}},
	})
	rep := canonical.CheckReferences(ix)
	want := []canonical.Dangling{{Entity: "ict_provider", Key: `["P3"]`, Field: "parent_id_code", Target: "ict_provider.provider_id_code", Value: "P9"}}
	if !reflect.DeepEqual(rep.Dangling, want) {
		t.Fatalf("dangling = %+v", rep.Dangling)
	}
	if len(rep.Cycles) != 1 || !reflect.DeepEqual(rep.Cycles[0].Members, []string{"P1", "P2"}) || rep.Cycles[0].Field != "parent_id_code" {
		t.Fatalf("cycles = %+v", rep.Cycles)
	}
	if !rep.IsDangling("ict_provider", `["P3"]`, "parent_id_code") || rep.IsDangling("ict_provider", `["P1"]`, "parent_id_code") {
		t.Fatal("IsDangling wrong")
	}
}

func TestCheckReferencesSelfLoopIsCycle(t *testing.T) {
	s := latestSchema(t)
	ix := index(t, s, map[string][]adapter.Record{"financial_entity": {{"lei": "SAMPLEFE000000000021", "parent_lei": "SAMPLEFE000000000021"}}})
	rep := canonical.CheckReferences(ix)
	if len(rep.Cycles) != 1 || len(rep.Dangling) != 0 {
		t.Fatalf("report = %+v", rep)
	}
}

func TestAnalyzeCompleteness(t *testing.T) {
	s := latestSchema(t)
	provider := adapter.Record{"provider_id_code": "P1", "legal_name": "One", "person_type": "X", "hq_country": "IE"}
	adapter.MarkDerived(provider, adapter.DerivedField{Field: "hq_country", Method: "region-to-country"})
	adapter.MarkNotApplicable(provider, "parent_id_code")
	ix := index(t, s, map[string][]adapter.Record{"ict_provider": {provider}})
	codes, err := canonical.LoadCodelists(fstest.MapFS{"cl/person_type.json": {Data: []byte(`{"codelist":"person_type","version":"1","values":[{"code":"LEGAL"}]}`)}}, "cl")
	if err != nil {
		t.Fatal(err)
	}
	supplied := func(entity, field string) bool { return field != "provider_id_type" }
	c := canonical.AnalyzeCompleteness(ix, codes, supplied)

	gaps := map[string]canonical.Gap{}
	for _, g := range c.Gaps {
		gaps[g.Field] = g
	}
	if g, ok := gaps["provider_id_type"]; !ok || g.State != canonical.StateMissing || g.Supplied || g.RoIRef != "B_05.01.0020" {
		t.Errorf("provider_id_type gap = %+v", g)
	}
	if g, ok := gaps["hq_country"]; !ok || g.State != canonical.StateDerived || !g.Supplied {
		t.Errorf("hq_country gap = %+v", g)
	}
	if _, ok := gaps["parent_id_code"]; ok {
		t.Error("not-applicable field must not be a gap")
	}
	if _, ok := gaps["legal_name"]; ok {
		t.Error("provided field must not be a gap")
	}
	if len(c.InvalidCodes) != 1 || c.InvalidCodes[0].Field != "person_type" || c.InvalidCodes[0].Value != "X" {
		t.Errorf("invalid codes = %+v", c.InvalidCodes)
	}
	if slices.Contains(c.UnverifiedCodelists, "person_type") {
		t.Error("loaded codelist must not be unverified")
	}
	var providerStats canonical.EntityCompleteness
	for _, e := range c.Entities {
		if e.Entity == "ict_provider" {
			providerStats = e
		}
	}
	if providerStats.Records != 1 || providerStats.Missing != 1 || providerStats.Derived != 1 {
		t.Errorf("entity stats = %+v", providerStats)
	}
	if len(c.Entities) != 14 || c.SchemaVersion != "0.1.0" {
		t.Errorf("entities %d schema %s", len(c.Entities), c.SchemaVersion)
	}
}

func TestAnalyzeCompletenessUnverifiedCodelists(t *testing.T) {
	s := latestSchema(t)
	ix := index(t, s, map[string][]adapter.Record{"reporting_entity": {{"lei": "SAMPLEFE000000000021", "entity_type": "EXAMPLE_CODE"}}})
	c := canonical.AnalyzeCompleteness(ix, nil, nil)
	if !reflect.DeepEqual(c.UnverifiedCodelists, []string{"entity_type"}) || len(c.InvalidCodes) != 0 {
		t.Fatalf("unverified = %v invalid = %v", c.UnverifiedCodelists, c.InvalidCodes)
	}
	for _, g := range c.Gaps {
		if !g.Supplied {
			t.Fatal("nil supply means unknown and is reported as supplied")
		}
	}
}
