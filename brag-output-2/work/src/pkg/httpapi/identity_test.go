// SPDX-License-Identifier: Apache-2.0

package httpapi_test

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/nexops-one/compliance-engine/pkg/access"
	"github.com/nexops-one/compliance-engine/pkg/auth"
	"github.com/nexops-one/compliance-engine/pkg/compliance"
	"github.com/nexops-one/compliance-engine/pkg/extension"
	"github.com/nexops-one/compliance-engine/pkg/httpapi"
	"github.com/nexops-one/compliance-engine/pkg/identity"
	"github.com/nexops-one/compliance-engine/pkg/store/memory"
)

const (
	adminToken   = "token-admin" // static, default roles owner+admin
	auditorToken = "token-auditor"
)

type identityAPI struct {
	h  http.Handler
	id *identity.Service
}

// newIdentityAPI serves an engine and identity service over one memory store,
// authenticating static tokens first, then stored tokens.
func newIdentityAPI(t *testing.T, rl httpapi.RateLimit) identityAPI {
	t.Helper()
	st := memory.New()
	eng, err := compliance.New(ctx, compliance.Config{Store: st})
	if err != nil {
		t.Fatal(err)
	}
	id := identity.New(st, identity.Options{})
	static := auth.NewStaticTokens([]auth.TokenEntry{
		{Hash: auth.HashToken(adminToken), Scope: scopeA},
		{Hash: auth.HashToken(auditorToken), Scope: scopeA, Roles: []access.Role{access.RoleAuditor}},
	})
	h := httpapi.New(eng, httpapi.Options{
		Authenticator: auth.Chain(static, id), Identity: id, RateLimit: rl, Version: "test",
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	return identityAPI{h: h, id: id}
}

func TestEveryRouteHasAPermission(t *testing.T) {
	for _, r := range httpapi.Routes() {
		if r.Public != (r.Permission == "") {
			t.Errorf("%s %s: public=%v permission=%q", r.Method, r.Pattern, r.Public, r.Permission)
		}
	}
}

func TestRoutePermissions(t *testing.T) {
	api := newIdentityAPI(t, httpapi.RateLimit{})
	r := call(t, api.h, "POST", "/api/v1/ingestions", auditorToken, sampleBatch(t))
	if r.Status != 403 || errorCode(r) != "forbidden" {
		t.Fatalf("auditor ingestion = %d %s", r.Status, r.Raw)
	}
	if e, _ := r.Body["error"].(map[string]any); e["permission"] != "data.write" {
		t.Fatalf("forbidden body = %s", r.Raw)
	}
	if r := call(t, api.h, "GET", "/api/v1/snapshots", auditorToken, nil); r.Status != 200 {
		t.Fatalf("auditor read = %d %s", r.Status, r.Raw)
	}
	if r := call(t, api.h, "POST", "/api/v1/evaluations", auditorToken, map[string]any{}); r.Status != 403 {
		t.Fatalf("auditor evaluation = %d", r.Status)
	}
	if r := call(t, api.h, "GET", "/api/v1/members", auditorToken, nil); r.Status != 403 {
		t.Fatalf("auditor members = %d", r.Status)
	}
	if r := call(t, api.h, "GET", "/api/v1/audit", auditorToken, nil); r.Status != 200 {
		t.Fatalf("auditor audit = %d %s", r.Status, r.Raw)
	}
}

func TestMeEndpoint(t *testing.T) {
	api := newIdentityAPI(t, httpapi.RateLimit{})
	r := call(t, api.h, "GET", "/api/v1/me", auditorToken, nil)
	if r.Status != 200 || r.Body["kind"] != "token" || r.Body["workspace_id"] != scopeA.WorkspaceID {
		t.Fatalf("me = %d %s", r.Status, r.Raw)
	}
	perms, _ := r.Body["permissions"].([]any)
	if len(perms) != 3 || perms[0] != "data.read" || perms[1] != "audit.read" || perms[2] != "tokens.own" {
		t.Fatalf("auditor permissions = %v", perms)
	}
}

func TestMembersAndTokensEndpoints(t *testing.T) {
	api := newIdentityAPI(t, httpapi.RateLimit{})
	r := call(t, api.h, "PUT", "/api/v1/members/Ann@Example.com", adminToken, map[string]any{"roles": []string{"approver", "owner"}})
	if r.Status != 200 || r.Body["email"] != "ann@example.com" {
		t.Fatalf("put member = %d %s", r.Status, r.Raw)
	}
	if r := call(t, api.h, "PUT", "/api/v1/members/bob@example.com", adminToken, map[string]any{"roles": []string{"root"}}); r.Status != 422 {
		t.Fatalf("bad role = %d %s", r.Status, r.Raw)
	}
	if r := call(t, api.h, "GET", "/api/v1/members", adminToken, nil); r.Status != 200 || !strings.Contains(string(r.Raw), "ann@example.com") {
		t.Fatalf("list members = %d %s", r.Status, r.Raw)
	}

	// The operator issues Ann a token; Ann then mints her own narrower token.
	ann, err := api.id.IssueToken(ctx, scopeA, "ann@example.com", identity.TokenRequest{Name: "cli", Roles: []access.Role{access.RoleApprover, access.RoleOwner}})
	if err != nil {
		t.Fatal(err)
	}
	if r := call(t, api.h, "POST", "/api/v1/tokens", ann.Value, map[string]any{"name": "too much", "roles": []string{"admin"}}); r.Status != 403 || errorCode(r) != "role_not_held" {
		t.Fatalf("minting admin = %d %s", r.Status, r.Raw)
	}
	r = call(t, api.h, "POST", "/api/v1/tokens", ann.Value, map[string]any{"name": "ci", "roles": []string{"owner"}})
	if r.Status != 201 || r.Header.Get("Cache-Control") != "no-store" {
		t.Fatalf("create token = %d %s", r.Status, r.Raw)
	}
	value, _ := r.Body["value"].(string)
	tok, _ := r.Body["token"].(map[string]any)
	if value == "" || tok["id"] == nil || strings.Contains(string(r.Raw), auth.HashToken(value)) {
		t.Fatalf("created token body = %s", r.Raw)
	}
	if r := call(t, api.h, "GET", "/api/v1/me", value, nil); r.Status != 200 || r.Body["kind"] != "user" {
		t.Fatalf("new token me = %d %s", r.Status, r.Raw)
	}
	r = call(t, api.h, "GET", "/api/v1/tokens", adminToken, nil)
	if r.Status != 200 || strings.Contains(string(r.Raw), auth.HashToken(value)) || strings.Contains(string(r.Raw), value) {
		t.Fatalf("list tokens must not reveal hashes or values: %s", r.Raw)
	}
	if r := call(t, api.h, "DELETE", "/api/v1/tokens/"+tok["id"].(string), value, nil); r.Status != 204 {
		t.Fatalf("revoke = %d %s", r.Status, r.Raw)
	}
	if r := call(t, api.h, "GET", "/api/v1/me", value, nil); r.Status != 401 {
		t.Fatalf("revoked token = %d", r.Status)
	}
	if r := call(t, api.h, "DELETE", "/api/v1/members/ann@example.com", adminToken, nil); r.Status != 204 {
		t.Fatalf("remove member = %d %s", r.Status, r.Raw)
	}
	if r := call(t, api.h, "GET", "/api/v1/me", ann.Value, nil); r.Status != 401 {
		t.Fatalf("a removed member's token must stop working: %d", r.Status)
	}
}

func TestAuditEndpoint(t *testing.T) {
	api := newIdentityAPI(t, httpapi.RateLimit{})
	if r := call(t, api.h, "PUT", "/api/v1/adapters/csv-import/manifest", adminToken, sampleManifest(t)); r.Status != 200 {
		t.Fatalf("manifest: %d %s", r.Status, r.Raw)
	}
	if r := call(t, api.h, "POST", "/api/v1/ingestions", adminToken, sampleBatch(t)); r.Status != 201 {
		t.Fatalf("ingestion: %d %s", r.Status, r.Raw)
	}
	r := call(t, api.h, "GET", "/api/v1/audit?verify=true", adminToken, nil)
	evs, _ := r.Body["events"].([]any)
	v, _ := r.Body["verification"].(map[string]any)
	if r.Status != 200 || len(evs) != 1 || v["ok"] != true || r.Body["next_after_seq"] != float64(1) {
		t.Fatalf("audit = %d %s", r.Status, r.Raw)
	}
	ev := evs[0].(map[string]any)
	if ev["action"] != "ingestion.commit" || !strings.HasPrefix(ev["actor"].(string), "token:") || ev["actor_kind"] != "token" {
		t.Fatalf("event = %v", ev)
	}
	if r := call(t, api.h, "GET", "/api/v1/audit?after_seq=1", adminToken, nil); r.Status != 200 || len(r.Body["events"].([]any)) != 0 {
		t.Fatalf("paging = %s", r.Raw)
	}
	if r := call(t, api.h, "GET", "/api/v1/audit?limit=5000", adminToken, nil); r.Status != 400 {
		t.Fatalf("limit bound = %d", r.Status)
	}
	owner, err := api.id.CreateToken(ctx, extensionAdmin(), identity.TokenRequest{Name: "svc", Roles: []access.Role{access.RoleOwner}, Service: true})
	if err != nil {
		t.Fatal(err)
	}
	if r := call(t, api.h, "GET", "/api/v1/audit", owner.Value, nil); r.Status != 403 {
		t.Fatalf("an owner-only token must not read the audit log: %d", r.Status)
	}
}

func TestIdentityNotConfigured(t *testing.T) {
	h := newAPI(t, setup{})
	if r := call(t, h, "GET", "/api/v1/tokens", tokenA, nil); r.Status != 501 || errorCode(r) != "identity_not_configured" {
		t.Fatalf("tokens without identity = %d %s", r.Status, r.Raw)
	}
}

func TestRateLimit(t *testing.T) {
	clock := time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC)
	defer httpapi.SetClockForTest(func() time.Time { return clock })()
	api := newIdentityAPI(t, httpapi.RateLimit{PerMinute: 60, Burst: 2})
	for i := 0; i < 2; i++ {
		if r := call(t, api.h, "GET", "/api/v1/me", adminToken, nil); r.Status != 200 {
			t.Fatalf("request %d = %d", i, r.Status)
		}
	}
	r := call(t, api.h, "GET", "/api/v1/me", adminToken, nil)
	if r.Status != 429 || errorCode(r) != "rate_limited" || r.Header.Get("Retry-After") != "1" {
		t.Fatalf("third request = %d %q %s", r.Status, r.Header.Get("Retry-After"), r.Raw)
	}
	if r := call(t, api.h, "GET", "/api/v1/me", auditorToken, nil); r.Status != 200 {
		t.Fatalf("buckets are per actor: %d", r.Status)
	}
	if r := call(t, api.h, "GET", "/healthz", "", nil); r.Status != 200 {
		t.Fatal("public routes are not limited")
	}
	clock = clock.Add(time.Second)
	if r := call(t, api.h, "GET", "/api/v1/me", adminToken, nil); r.Status != 200 {
		t.Fatalf("after refill = %d", r.Status)
	}
}

func TestAuthFailuresAreRateLimited(t *testing.T) {
	clock := time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC)
	defer httpapi.SetClockForTest(func() time.Time { return clock })()
	api := newIdentityAPI(t, httpapi.RateLimit{AuthFailures: 3})
	for i := 0; i < 3; i++ {
		if r := call(t, api.h, "GET", "/api/v1/me", "guess", nil); r.Status != 401 {
			t.Fatalf("failure %d = %d", i, r.Status)
		}
	}
	r := call(t, api.h, "GET", "/api/v1/me", adminToken, nil)
	if r.Status != 429 || r.Header.Get("Retry-After") != "20" {
		t.Fatalf("after the failure budget even a valid token waits: %d %q", r.Status, r.Header.Get("Retry-After"))
	}
	req := httptest.NewRequest("GET", "/api/v1/me", nil)
	req.RemoteAddr = "198.51.100.7:4000"
	req.Header.Set("Authorization", "Bearer "+adminToken)
	rec := httptest.NewRecorder()
	api.h.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("other addresses are not affected: %d", rec.Code)
	}
	clock = clock.Add(20 * time.Second)
	if r := call(t, api.h, "GET", "/api/v1/me", adminToken, nil); r.Status != 200 {
		t.Fatalf("after refill = %d", r.Status)
	}
}

func extensionAdmin() extension.Principal {
	return extension.Principal{Scope: scopeA, Actor: "token:test-admin", Kind: extension.ActorToken, Roles: []access.Role{access.RoleAdmin}}
}
