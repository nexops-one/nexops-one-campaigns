// SPDX-License-Identifier: Apache-2.0

package httpapi_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/nexops-one/compliance-engine/pkg/compliance"
	"github.com/nexops-one/compliance-engine/pkg/extension"
	"github.com/nexops-one/compliance-engine/pkg/httpapi"
	"github.com/nexops-one/compliance-engine/sdk/adapter"
)

func TestHealthzIsPublic(t *testing.T) {
	r := call(t, newAPI(t, setup{}), "GET", "/healthz", "", nil)
	if r.Status != 200 || r.Body["status"] != "ok" {
		t.Fatalf("healthz = %d %s", r.Status, r.Raw)
	}
}

func TestAuthRequired(t *testing.T) {
	h := newAPI(t, setup{})
	for _, token := range []string{"", "wrong"} {
		r := call(t, h, "GET", "/api/v1/about", token, nil)
		if r.Status != 401 || errorCode(r) != "unauthorized" || !strings.HasPrefix(r.Header.Get("WWW-Authenticate"), "Bearer") {
			t.Fatalf("token %q: %d %s", token, r.Status, r.Raw)
		}
		if strings.Contains(string(r.Raw), "schema_versions") {
			t.Fatal("an unauthenticated response must not carry data")
		}
	}
}

func TestAbout(t *testing.T) {
	r := call(t, newAPI(t, setup{}), "GET", "/api/v1/about", tokenA, nil)
	if r.Status != 200 || r.Body["edition"] != "open-core" || r.Body["version"] != "test" || r.Body["workspace_id"] != "ws-1" {
		t.Fatalf("about = %d %s", r.Status, r.Raw)
	}
	if v, _ := r.Body["schema_versions"].([]any); len(v) != 1 || v[0] != "0.1.0" {
		t.Fatalf("schema_versions = %v", r.Body["schema_versions"])
	}
	if c, _ := r.Body["catalogs"].([]any); len(c) != 3 {
		t.Fatalf("catalogs = %v", r.Body["catalogs"])
	}
}

func TestPutManifest(t *testing.T) {
	h := newAPI(t, setup{})
	m := sampleManifest(t)
	if r := call(t, h, "PUT", "/api/v1/adapters/csv-import/manifest", tokenA, m); r.Status != 200 || r.Body["name"] != "csv-import" {
		t.Fatalf("put = %d %s", r.Status, r.Raw)
	}
	if r := call(t, h, "PUT", "/api/v1/adapters/other/manifest", tokenA, m); r.Status != 422 || errorCode(r) != "name_mismatch" {
		t.Fatalf("mismatch = %d %s", r.Status, r.Raw)
	}
	bad := m
	bad.SchemaVersion = "9.0.0"
	if r := call(t, h, "PUT", "/api/v1/adapters/csv-import/manifest", tokenA, bad); r.Status != 422 || errorCode(r) != "invalid_manifest" {
		t.Fatalf("invalid = %d %s", r.Status, r.Raw)
	}
	if r := call(t, h, "PUT", "/api/v1/adapters/csv-import/manifest", tokenA, []byte(`{"name":"csv-import","extra":1}`)); r.Status != 400 || errorCode(r) != "invalid_json" {
		t.Fatalf("unknown field = %d %s", r.Status, r.Raw)
	}
}

func TestIngestionLifecycle(t *testing.T) {
	h := newAPI(t, setup{})
	if r := call(t, h, "POST", "/api/v1/ingestions", tokenA, sampleBatch(t)); r.Status != 422 || errorCode(r) != "unknown_adapter" {
		t.Fatalf("unregistered adapter = %d %s", r.Status, r.Raw)
	}
	call(t, h, "PUT", "/api/v1/adapters/csv-import/manifest", tokenA, sampleManifest(t))
	dry := call(t, h, "POST", "/api/v1/ingestions?dry_run=true", tokenA, sampleBatch(t))
	result, _ := dry.Body["result"].(map[string]any)
	if dry.Status != 200 || result["dry_run"] != true || dry.Body["completeness"] == nil {
		t.Fatalf("dry run = %d %s", dry.Status, dry.Raw)
	}
	first := call(t, h, "POST", "/api/v1/ingestions", tokenA, sampleBatch(t))
	if first.Status != 201 || first.Body["snapshot_id"] != "rev-1" || first.Body["accepted"] != float64(9) {
		t.Fatalf("ingest = %d %s", first.Status, first.Raw)
	}
	again := call(t, h, "POST", "/api/v1/ingestions", tokenA, sampleBatch(t))
	if again.Status != 200 || again.Body["no_changes"] != true {
		t.Fatalf("re-ingest = %d %s", again.Status, again.Raw)
	}
	id := first.Body["ingestion_id"].(string)
	if r := call(t, h, "GET", "/api/v1/ingestions/"+id, tokenA, nil); r.Status != 200 || r.Body["revision_after"] != float64(1) {
		t.Fatalf("get = %d %s", r.Status, r.Raw)
	}
	if r := call(t, h, "GET", "/api/v1/ingestions/ing-missing", tokenA, nil); r.Status != 404 || errorCode(r) != "not_found" {
		t.Fatalf("missing = %d %s", r.Status, r.Raw)
	}
	if r := call(t, h, "POST", "/api/v1/ingestions/"+id+"/rollback", tokenA, nil); r.Status != 201 || r.Body["snapshot_id"] != "rev-2" {
		t.Fatalf("rollback = %d %s", r.Status, r.Raw)
	}
	if r := call(t, h, "POST", "/api/v1/ingestions/"+id+"/rollback", tokenA, nil); r.Status != 409 || errorCode(r) != "already_rolled_back" {
		t.Fatalf("second rollback = %d %s", r.Status, r.Raw)
	}
	if r := call(t, h, "POST", "/api/v1/ingestions?dry_run=maybe", tokenA, sampleBatch(t)); r.Status != 400 || errorCode(r) != "invalid_parameter" {
		t.Fatalf("bad dry_run = %d %s", r.Status, r.Raw)
	}
}

func TestIngestionErrors(t *testing.T) {
	h := newAPI(t, setup{engine: func(c *compliance.Config) { c.Limits.MaxRecordsPerBatch = 5 }})
	call(t, h, "PUT", "/api/v1/adapters/csv-import/manifest", tokenA, sampleManifest(t))
	cases := []struct {
		body   string
		status int
		code   string
	}{
		{`{not json`, 400, "invalid_json"},
		{`{"schema_version":"0.1.0","source":{"system":"s","adapter":"csv-import","adapter_version":"1"},"entities":{"nope":[]}}`, 422, "invalid_batch"},
		{`{"schema_version":"0.9.0","source":{"system":"s","adapter":"csv-import","adapter_version":"1"},"entities":{}}`, 422, "unsupported_schema_version"},
		{`{"schema_version":"0.1.0","batch":{"mode":"full"},"source":{"system":"s","adapter":"csv-import","adapter_version":"1"},"entities":{"ict_provider":[{},{},{},{},{},{}]}}`, 413, "too_many_records"},
	}
	for _, c := range cases {
		r := call(t, h, "POST", "/api/v1/ingestions", tokenA, []byte(c.body))
		if r.Status != c.status || errorCode(r) != c.code {
			t.Errorf("%s: %d %s", c.body, r.Status, r.Raw)
		}
	}
	r := call(t, h, "POST", "/api/v1/ingestions", tokenA, []byte(cases[1].body))
	details, _ := r.Body["error"].(map[string]any)["details"].([]any)
	if len(details) != 1 {
		t.Fatalf("invalid_batch must carry field details: %s", r.Raw)
	}
}

func TestBodyLimit(t *testing.T) {
	h := newAPI(t, setup{options: func(o *httpapi.Options) { o.MaxBodyBytes = 1024 }})
	call(t, h, "PUT", "/api/v1/adapters/csv-import/manifest", tokenA, sampleManifest(t))
	r := call(t, h, "POST", "/api/v1/ingestions", tokenA, sampleBatch(t))
	if r.Status != 413 || errorCode(r) != "payload_too_large" {
		t.Fatalf("oversized body = %d %s", r.Status, r.Raw)
	}
}

func TestIngestionIsScopedToToken(t *testing.T) {
	h := loaded(t)
	ing := call(t, h, "POST", "/api/v1/ingestions", tokenA, sampleBatch(t))
	id := ing.Body["ingestion_id"].(string)
	if r := call(t, h, "GET", "/api/v1/ingestions/"+id, tokenB, nil); r.Status != 404 {
		t.Fatalf("workspace B read A's ingestion: %d %s", r.Status, r.Raw)
	}
	if r := call(t, h, "POST", "/api/v1/ingestions/"+id+"/rollback", tokenB, nil); r.Status != 404 {
		t.Fatalf("workspace B rolled back A's ingestion: %d %s", r.Status, r.Raw)
	}
}

type fixedEnts struct{ d extension.Decision }

func (f fixedEnts) Allowed(context.Context, adapter.Scope, extension.Feature) extension.Decision {
	return f.d
}

func TestEntitlementsGraceAndDenial(t *testing.T) {
	until := time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC)
	grace := newAPI(t, setup{engine: func(c *compliance.Config) {
		c.Entitlements = fixedEnts{extension.Decision{Allowed: true, Reason: extension.ReasonExpired, GraceUntil: &until}}
	}})
	r := call(t, grace, "PUT", "/api/v1/adapters/csv-import/manifest", tokenA, sampleManifest(t))
	if r.Status != 200 || !strings.Contains(r.Header.Get("Warning"), "2026-02-01T00:00:00Z") {
		t.Fatalf("grace = %d warning %q", r.Status, r.Header.Get("Warning"))
	}
	if w := r.Header.Get("Compliance-License-Warning"); w != "register.ingest expired; grace period ends 2026-02-01T00:00:00Z" {
		t.Fatalf("Compliance-License-Warning = %q", w)
	}
	if w := call(t, grace, "GET", "/api/v1/about", tokenA, nil).Header.Get("Warning"); w != "" {
		t.Fatalf("reads must not carry the warning: %q", w)
	}
	denied := newAPI(t, setup{engine: func(c *compliance.Config) {
		c.Entitlements = fixedEnts{extension.Decision{Reason: extension.ReasonAddonDisabled}}
	}})
	r = call(t, denied, "PUT", "/api/v1/adapters/csv-import/manifest", tokenA, sampleManifest(t))
	e, _ := r.Body["error"].(map[string]any)
	if r.Status != 403 || e["code"] != "feature_not_entitled" || e["feature"] != "register.ingest" || e["reason"] != "addon_disabled" {
		t.Fatalf("denied = %d %s", r.Status, r.Raw)
	}
	if r := call(t, denied, "GET", "/api/v1/about", tokenA, nil); r.Status != 200 {
		t.Fatalf("reads must never be gated: %d", r.Status)
	}
}

func TestUnknownPathIsJSON404(t *testing.T) {
	r := call(t, newAPI(t, setup{}), "GET", "/api/v1/nope", tokenA, nil)
	if r.Status != 404 || errorCode(r) != "not_found" {
		t.Fatalf("unknown path = %d %s", r.Status, r.Raw)
	}
}
