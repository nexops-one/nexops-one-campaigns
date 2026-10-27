// SPDX-License-Identifier: Apache-2.0

package httpapi_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"strings"
	"testing"

	"github.com/nexops-one/compliance-engine/pkg/access"
	"github.com/nexops-one/compliance-engine/pkg/auth"
	"github.com/nexops-one/compliance-engine/pkg/compliance"
	"github.com/nexops-one/compliance-engine/pkg/extension"
	"github.com/nexops-one/compliance-engine/pkg/httpapi"
	"github.com/nexops-one/compliance-engine/pkg/identity"
	"github.com/nexops-one/compliance-engine/pkg/report"
	"github.com/nexops-one/compliance-engine/pkg/store/memory"
)

func TestReportsOverHTTP(t *testing.T) {
	api := newIdentityAPI(t, httpapi.RateLimit{})
	owner := userToken(t, api, "olga@example.com", access.RoleOwner)
	auditor := userToken(t, api, "audrey@example.com", access.RoleAuditor)
	if r := call(t, api.h, "PUT", "/api/v1/adapters/csv-import/manifest", owner, sampleManifest(t)); r.Status != 200 {
		t.Fatalf("manifest: %d %s", r.Status, r.Raw)
	}
	if r := call(t, api.h, "POST", "/api/v1/ingestions", owner, sampleBatch(t)); r.Status != 201 {
		t.Fatalf("ingestion: %d %s", r.Status, r.Raw)
	}
	req := map[string]any{"profile": "profile_b", "catalogs": []string{"dora@1.0.0"}}
	admin := userToken(t, api, "ada@example.com", access.RoleAdmin)
	for _, tok := range []string{auditor, admin} {
		if r := call(t, api.h, "POST", "/api/v1/reports", tok, req); r.Status != 403 || errorCode(r) != "forbidden" {
			t.Fatalf("auditors and admins do not generate reports: %d %s", r.Status, r.Raw)
		}
	}
	for _, tc := range []struct {
		body   any
		status int
		code   string
	}{
		{map[string]any{}, 422, "invalid_request"},
		{map[string]any{"profile": "profile_z"}, 404, "not_found"},
		{map[string]any{"profile": "profile_b", "formats": []string{"xbrl-csv"}}, 422, "invalid_request"},
		{map[string]any{"profile": "profile_b", "catalogs": []string{"dora"}}, 400, "invalid_parameter"},
		{map[string]any{"profile": "profile_b", "as_of": "2026-01-01T00:00:00Z"}, 400, "invalid_json"},
		{map[string]any{"profile": "profile_b", "evaluation_id": "eval-missing"}, 404, "not_found"},
	} {
		if r := call(t, api.h, "POST", "/api/v1/reports", owner, tc.body); r.Status != tc.status || errorCode(r) != tc.code {
			t.Errorf("%v: %d %s, want %d %s", tc.body, r.Status, errorCode(r), tc.status, tc.code)
		}
	}
	gen := call(t, api.h, "POST", "/api/v1/reports", owner, req)
	if gen.Status != 201 || gen.Body["profile"] != "profile_b" || gen.Body["facts"] == nil || gen.Body["inputs"] != nil {
		t.Fatalf("generate: %d %s", gen.Status, gen.Raw)
	}
	id := gen.Body["id"].(string)

	profiles := call(t, api.h, "GET", "/api/v1/report-profiles", auditor, nil)
	if profiles.Status != 200 || !strings.Contains(string(profiles.Raw), `"id":"profile_b"`) || !strings.Contains(string(profiles.Raw), `"allowed":true`) {
		t.Fatalf("profiles: %d %s", profiles.Status, profiles.Raw)
	}
	list := call(t, api.h, "GET", "/api/v1/reports", auditor, nil)
	if reps, _ := list.Body["reports"].([]any); list.Status != 200 || len(reps) != 1 || reps[0].(map[string]any)["facts"] != nil {
		t.Fatalf("list: %d %s", list.Status, list.Raw)
	}
	if r := call(t, api.h, "GET", "/api/v1/reports/"+id, auditor, nil); r.Status != 200 || r.Body["facts"] == nil {
		t.Fatalf("get: %d %s", r.Status, r.Raw)
	}
	pdf := call(t, api.h, "GET", "/api/v1/reports/"+id+"/files/report.pdf", auditor, nil)
	if pdf.Status != 200 || pdf.Header.Get("Content-Type") != "application/pdf" || !bytes.HasPrefix(pdf.Raw, []byte("%PDF-")) ||
		pdf.Header.Get("Content-Disposition") != `attachment; filename=`+id+`-report.pdf` || pdf.Header.Get("Cache-Control") != "no-store" ||
		pdf.Header.Get("Digest") != "sha-256="+report.Hash(pdf.Raw) {
		t.Fatalf("pdf: %d %v", pdf.Status, pdf.Header)
	}
	facts := call(t, api.h, "GET", "/api/v1/reports/"+id+"/files/report.json", auditor, nil)
	if facts.Status != 200 || facts.Header.Get("Content-Type") != "application/json" || report.Hash(facts.Raw) != gen.Body["facts_hash"] {
		t.Fatalf("report.json does not match the facts hash: %d %s", facts.Status, facts.Header)
	}
	if r := call(t, api.h, "GET", "/api/v1/reports/"+id+"/files/report.csv", auditor, nil); r.Status != 404 {
		t.Fatalf("unknown file: %d", r.Status)
	}
	if r := call(t, api.h, "GET", "/api/v1/reports/"+id, tokenOtherWorkspace(t, api), nil); r.Status != 404 {
		t.Fatalf("another workspace must not see the report: %d", r.Status)
	}
	regen := call(t, api.h, "POST", "/api/v1/reports/"+id+"/regenerate", auditor, nil)
	if regen.Status != 200 || regen.Body["identical"] != true || regen.Body["regenerated_facts_hash"] != gen.Body["facts_hash"] {
		t.Fatalf("regenerate: %d %s", regen.Status, regen.Raw)
	}
	audit := call(t, api.h, "GET", "/api/v1/audit", auditor, nil)
	for _, action := range []string{"report.generate", "report.download", "report.regenerate"} {
		if !strings.Contains(string(audit.Raw), `"action":"`+action+`"`) {
			t.Errorf("audit lacks %s", action)
		}
	}
}

// tokenOtherWorkspace issues an owner token of another workspace of the tenant.
func tokenOtherWorkspace(t *testing.T, api identityAPI) string {
	t.Helper()
	if _, err := api.id.AddMember(ctx, scopeB, "bob@example.com", []access.Role{access.RoleOwner}); err != nil {
		t.Fatal(err)
	}
	tok, err := api.id.IssueToken(ctx, scopeB, "bob@example.com", identity.TokenRequest{Name: "other", Roles: []access.Role{access.RoleOwner}})
	if err != nil {
		t.Fatal(err)
	}
	return tok.Value
}

type blockingProfile struct{}

func (blockingProfile) ID() string                 { return "strict" }
func (blockingProfile) Feature() extension.Feature { return extension.FeatureReportProfileB }
func (blockingProfile) Formats() []string          { return nil }
func (blockingProfile) Generate(context.Context, extension.ReportInput) (extension.ReportOutput, error) {
	return extension.ReportOutput{Facts: json.RawMessage(`{}`), Blocking: []extension.Finding{
		{Severity: "error", Code: "mandatory_missing", Message: "c0050 is mandatory", Template: "B_05.01", Row: "1", Field: "c0050"}}}, nil
}

func TestExportIncompleteOverHTTP(t *testing.T) {
	eng, err := compliance.New(ctx, compliance.Config{Store: memory.New(),
		Extensions: compliance.Extensions{ReportProfiles: []extension.ReportProfile{blockingProfile{}}}})
	if err != nil {
		t.Fatal(err)
	}
	h := httpapi.New(eng, httpapi.Options{Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		Authenticator: auth.NewStaticTokens([]auth.TokenEntry{{Hash: auth.HashToken(tokenA), Scope: scopeA, Roles: []access.Role{access.RoleOwner}}})})
	r := call(t, h, "POST", "/api/v1/reports", tokenA, map[string]any{"profile": "strict"})
	e, _ := r.Body["error"].(map[string]any)
	findings, _ := e["findings"].([]any)
	if r.Status != 422 || errorCode(r) != "export_incomplete" || len(findings) != 1 || findings[0].(map[string]any)["field"] != "c0050" {
		t.Fatalf("incomplete export: %d %s", r.Status, r.Raw)
	}
}
