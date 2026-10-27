// SPDX-License-Identifier: Apache-2.0

package httpapi_test

import (
	"net/url"
	"testing"
)

func TestSnapshots(t *testing.T) {
	h := loaded(t)
	list := call(t, h, "GET", "/api/v1/snapshots", tokenA, nil)
	if snaps, _ := list.Body["snapshots"].([]any); list.Status != 200 || len(snaps) != 1 {
		t.Fatalf("list = %d %s", list.Status, list.Raw)
	}
	cur := call(t, h, "GET", "/api/v1/snapshots/current", tokenA, nil)
	entities, _ := cur.Body["entities"].(map[string]any)
	if cur.Status != 200 || cur.Body["id"] != "rev-1" || cur.Body["records"] != float64(9) || entities["ict_provider"] != float64(2) {
		t.Fatalf("current = %d %s", cur.Status, cur.Raw)
	}
	for _, id := range []string{"rev-7", "bogus"} {
		if r := call(t, h, "GET", "/api/v1/snapshots/"+id, tokenA, nil); r.Status != 404 {
			t.Errorf("%s = %d", id, r.Status)
		}
	}
	if r := call(t, h, "GET", "/api/v1/snapshots/current", tokenB, nil); r.Status != 200 || r.Body["records"] != float64(0) {
		t.Fatalf("workspace B must see an empty snapshot: %d %s", r.Status, r.Raw)
	}
}

func TestRecordsWithFieldStates(t *testing.T) {
	h := loaded(t)
	r := call(t, h, "GET", "/api/v1/snapshots/rev-1/records/arrangement_service_line", tokenA, nil)
	recs, _ := r.Body["records"].([]any)
	if r.Status != 200 || len(recs) != 1 {
		t.Fatalf("records = %d %s", r.Status, r.Raw)
	}
	states := recs[0].(map[string]any)["field_states"].(map[string]any)
	if states["data_at_rest_country"] != "derived" || states["end_date"] != "missing" || states["start_date"] != "provided" {
		t.Fatalf("field states = %v", states)
	}
	if r := call(t, h, "GET", "/api/v1/snapshots/current/records/nope", tokenA, nil); r.Status != 404 || errorCode(r) != "not_found" {
		t.Fatalf("unknown entity = %d %s", r.Status, r.Raw)
	}
}

func TestCompletenessAndProvenance(t *testing.T) {
	h := loaded(t)
	c := call(t, h, "GET", "/api/v1/snapshots/current/completeness", tokenA, nil)
	if gaps, _ := c.Body["gaps"].([]any); c.Status != 200 || c.Body["snapshot_id"] != "rev-1" || len(gaps) == 0 {
		t.Fatalf("completeness = %d %s", c.Status, c.Raw)
	}
	if r := call(t, h, "GET", "/api/v1/provenance?entity=ict_provider", tokenA, nil); r.Status != 400 || errorCode(r) != "missing_parameter" {
		t.Fatalf("missing key = %d %s", r.Status, r.Raw)
	}
	q := url.Values{"entity": {"ict_provider"}, "key": {`["SAMPLETP000000000003"]`}}
	p := call(t, h, "GET", "/api/v1/provenance?"+q.Encode(), tokenA, nil)
	if entries, _ := p.Body["entries"].([]any); p.Status != 200 || len(entries) != 1 {
		t.Fatalf("provenance = %d %s", p.Status, p.Raw)
	}
}

func TestCatalogs(t *testing.T) {
	h := newAPI(t, setup{})
	list := call(t, h, "GET", "/api/v1/catalogs", tokenA, nil)
	cats, _ := list.Body["catalogs"].([]any)
	if list.Status != 200 || len(cats) != 3 || cats[1].(map[string]any)["controls"] != float64(11) {
		t.Fatalf("catalogs = %d %s", list.Status, list.Raw)
	}
	if r := call(t, h, "GET", "/api/v1/catalogs/dora/1.0.0", tokenA, nil); r.Status != 200 || r.Body["framework"] != "DORA" {
		t.Fatalf("dora = %d %s", r.Status, r.Raw)
	}
	if r := call(t, h, "GET", "/api/v1/catalogs/dora/9.9.9", tokenA, nil); r.Status != 404 {
		t.Fatalf("unknown = %d", r.Status)
	}
	if r := call(t, h, "GET", "/api/v1/catalogs/diff?a=dora@1.0.0&b=dora@1.0.0", tokenA, nil); r.Status != 200 || r.Body["from"] == nil {
		t.Fatalf("diff = %d %s", r.Status, r.Raw)
	}
	if r := call(t, h, "GET", "/api/v1/catalogs/diff?a=dora&b=dora@1.0.0", tokenA, nil); r.Status != 400 || errorCode(r) != "invalid_parameter" {
		t.Fatalf("bad ref = %d %s", r.Status, r.Raw)
	}
}

func TestEvaluations(t *testing.T) {
	h := loaded(t)
	ev := call(t, h, "POST", "/api/v1/evaluations", tokenA, map[string]any{"catalogs": []string{"dora@1.0.0"}})
	result, _ := ev.Body["result"].(map[string]any)
	overall, _ := result["overall"].(map[string]any)
	if ev.Status != 201 || overall["score_pct"] != float64(80) || overall["coverage_pct"] != float64(45) {
		t.Fatalf("evaluation = %d %s", ev.Status, ev.Raw)
	}
	id := ev.Body["id"].(string)
	if r := call(t, h, "GET", "/api/v1/evaluations/"+id, tokenA, nil); r.Status != 200 || r.Body["id"] != id {
		t.Fatalf("get = %d %s", r.Status, r.Raw)
	}
	if r := call(t, h, "POST", "/api/v1/evaluations", tokenA, nil); r.Status != 201 {
		t.Fatalf("an empty body evaluates the latest catalogs: %d %s", r.Status, r.Raw)
	}
	if r := call(t, h, "POST", "/api/v1/evaluations", tokenA, map[string]any{"catalogs": []string{"dora@9.0.0"}}); r.Status != 404 {
		t.Fatalf("unknown catalog = %d %s", r.Status, r.Raw)
	}
	if r := call(t, h, "POST", "/api/v1/evaluations", tokenA, map[string]any{"catalogs": []string{"dora"}}); r.Status != 400 || errorCode(r) != "invalid_parameter" {
		t.Fatalf("bad ref = %d %s", r.Status, r.Raw)
	}
}

func TestEvaluationIsScopedToToken(t *testing.T) {
	h := loaded(t)
	ev := call(t, h, "POST", "/api/v1/evaluations", tokenA, map[string]any{})
	id := ev.Body["id"].(string)
	if r := call(t, h, "GET", "/api/v1/evaluations/"+id, tokenB, nil); r.Status != 404 {
		t.Fatalf("workspace B read A's evaluation: %d %s", r.Status, r.Raw)
	}
}
