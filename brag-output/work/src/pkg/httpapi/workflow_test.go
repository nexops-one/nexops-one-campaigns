// SPDX-License-Identifier: Apache-2.0

package httpapi_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"mime/multipart"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/nexops-one/compliance-engine/pkg/access"
	"github.com/nexops-one/compliance-engine/pkg/compliance"
	"github.com/nexops-one/compliance-engine/pkg/httpapi"
	"github.com/nexops-one/compliance-engine/pkg/identity"
)

func sha(s string) string {
	h := sha256.Sum256([]byte(s))
	return "sha256:" + hex.EncodeToString(h[:])
}

// userToken makes email a member with roles and returns a token acting as them.
func userToken(t *testing.T, api identityAPI, email string, roles ...access.Role) string {
	t.Helper()
	if _, err := api.id.AddMember(ctx, scopeA, email, roles); err != nil {
		t.Fatal(err)
	}
	tok, err := api.id.IssueToken(ctx, scopeA, email, identity.TokenRequest{Name: "test", Roles: roles})
	if err != nil {
		t.Fatal(err)
	}
	return tok.Value
}

func TestWorkflowOverHTTP(t *testing.T) {
	api := newIdentityAPI(t, httpapi.RateLimit{})
	owner := userToken(t, api, "olga@example.com", access.RoleOwner)
	approver := userToken(t, api, "arno@example.com", access.RoleApprover)
	auditor := userToken(t, api, "audrey@example.com", access.RoleAuditor)
	if r := call(t, api.h, "PUT", "/api/v1/adapters/csv-import/manifest", owner, sampleManifest(t)); r.Status != 200 {
		t.Fatalf("manifest: %d %s", r.Status, r.Raw)
	}
	if r := call(t, api.h, "POST", "/api/v1/ingestions", owner, sampleBatch(t)); r.Status != 201 {
		t.Fatalf("ingestion: %d %s", r.Status, r.Raw)
	}
	const ctl = "/api/v1/assessments/dora/dora-roi-provider-identification"

	if r := call(t, api.h, "PUT", ctl, auditor, map[string]any{"owner": "olga@example.com"}); r.Status != 403 {
		t.Fatalf("an auditor cannot assign: %d", r.Status)
	}
	if r := call(t, api.h, "PUT", ctl, owner, map[string]any{"owner": "olga@example.com", "reviewer": "arno@example.com", "due_at": "2099-01-01T00:00:00Z"}); r.Status != 200 {
		t.Fatalf("assign: %d %s", r.Status, r.Raw)
	}
	if r := call(t, api.h, "POST", ctl+"/submit", approver, nil); r.Status != 403 {
		t.Fatalf("an approver without the owner role cannot submit: %d", r.Status)
	}
	if r := call(t, api.h, "POST", ctl+"/submit", owner, nil); r.Status != 200 || r.Body["stage"] != "submitted" {
		t.Fatalf("submit: %d %s", r.Status, r.Raw)
	}
	eval := call(t, api.h, "POST", "/api/v1/evaluations", owner, map[string]any{"catalogs": []string{"dora@1.0.0"}})
	evalID, _ := eval.Body["id"].(string)
	if eval.Status != 201 || evalID == "" || eval.Body["effective"] == nil {
		t.Fatalf("evaluation: %d %s", eval.Status, eval.Raw)
	}
	if r := call(t, api.h, "POST", ctl+"/approve", approver, map[string]any{"evaluation_id": evalID}); r.Status != 409 || errorCode(r) != "evidence_required" {
		t.Fatalf("approve without evidence: %d %s", r.Status, r.Raw)
	}
	bad := map[string]any{"title": "x", "kind": "document", "source": "dms", "uri": "https://u:p@dms.example/x", "checksum": sha("x")}
	if r := call(t, api.h, "POST", "/api/v1/evidence", owner, bad); r.Status != 422 || errorCode(r) != "invalid_request" {
		t.Fatalf("credentials in uri: %d %s", r.Status, r.Raw)
	}
	good := map[string]any{"title": "Provider register extract", "kind": "document", "source": "dms", "uri": "urn:dms:provider-register:2026",
		"checksum": sha("register"), "retention": map[string]any{"min_days": 3650},
		"links": []map[string]string{{"catalog": "dora", "control_id": "dora-roi-provider-identification"}}}
	created := call(t, api.h, "POST", "/api/v1/evidence", owner, good)
	evidenceID, _ := created.Body["id"].(string)
	if created.Status != 201 || created.Body["integrity"] != "unverified" {
		t.Fatalf("evidence: %d %s", created.Status, created.Raw)
	}
	if r := call(t, api.h, "POST", "/api/v1/evidence/"+evidenceID+"/verify", owner, nil); r.Status != 422 || errorCode(r) != "cannot_verify_here" {
		t.Fatalf("verify a urn: %d %s", r.Status, r.Raw)
	}
	if r := call(t, api.h, "POST", "/api/v1/evidence/"+evidenceID+"/checks", owner, map[string]any{"checksum": sha("register")}); r.Status != 200 || r.Body["integrity"] != "verified" {
		t.Fatalf("attested check: %d %s", r.Status, r.Raw)
	}
	if r := call(t, api.h, "POST", ctl+"/approve", approver, map[string]any{"evaluation_id": evalID}); r.Status != 200 || r.Body["stage"] != "approved" {
		t.Fatalf("approve: %d %s", r.Status, r.Raw)
	}
	status := call(t, api.h, "GET", "/api/v1/status?catalogs=dora@1.0.0", auditor, nil)
	if status.Status != 200 || !strings.Contains(string(status.Raw), `"control_id":"dora-roi-provider-identification","title"`) {
		t.Fatalf("status: %d %s", status.Status, status.Raw)
	}
	fw := status.Body["frameworks"].([]any)[0].(map[string]any)
	if tally := fw["tally"].(map[string]any); tally["ready"] != float64(1) {
		t.Fatalf("tally: %v", tally)
	}
	view := call(t, api.h, "GET", ctl, auditor, nil)
	if hist, _ := view.Body["history"].([]any); view.Status != 200 || len(hist) != 3 {
		t.Fatalf("history: %d %s", view.Status, view.Raw)
	}
	if r := call(t, api.h, "POST", ctl+"/reopen", owner, map[string]any{}); r.Status != 422 {
		t.Fatalf("reopen without reason: %d %s", r.Status, r.Raw)
	}
	if r := call(t, api.h, "POST", ctl+"/reject", approver, map[string]any{"reason": "late"}); r.Status != 409 || errorCode(r) != "invalid_transition" {
		t.Fatalf("reject an approved control: %d %s", r.Status, r.Raw)
	}
	if r := call(t, api.h, "GET", "/api/v1/evidence/"+evidenceID, auditor, nil); r.Status != 200 {
		t.Fatalf("read evidence: %d", r.Status)
	}
	if r := call(t, api.h, "POST", "/api/v1/evidence/"+evidenceID+"/revoke", auditor, map[string]any{"reason": "x"}); r.Status != 403 {
		t.Fatalf("an auditor cannot revoke: %d", r.Status)
	}
	if r := call(t, api.h, "GET", "/api/v1/assessments/dora/not-a-control", auditor, nil); r.Status != 404 {
		t.Fatalf("unknown control: %d", r.Status)
	}
	audit := call(t, api.h, "GET", "/api/v1/audit?verify=true&limit=1000", auditor, nil)
	for _, want := range []string{"assessment.submit", "assessment.approve", "evidence.create", "evidence.check", "evidence.access"} {
		if !strings.Contains(string(audit.Raw), `"action":"`+want+`"`) {
			t.Errorf("audit log lacks %s", want)
		}
	}
	if !strings.Contains(string(audit.Raw), `"ok":true`) {
		t.Fatal("the audit chain must verify")
	}
}

func TestSettingsOverHTTP(t *testing.T) {
	api := newIdentityAPI(t, httpapi.RateLimit{})
	owner := userToken(t, api, "olga@example.com", access.RoleOwner)
	if r := call(t, api.h, "GET", "/api/v1/settings", owner, nil); r.Status != 200 || r.Body["version"] != float64(0) {
		t.Fatalf("defaults: %d %s", r.Status, r.Raw)
	}
	body := map[string]any{"retention": map[string]any{"revision_days": 365, "keep_revisions": 3, "evaluation_days": 730}}
	if r := call(t, api.h, "PUT", "/api/v1/settings", owner, body); r.Status != 403 {
		t.Fatalf("an owner cannot change settings: %d", r.Status)
	}
	r := call(t, api.h, "PUT", "/api/v1/settings", adminToken, body)
	if ret, _ := r.Body["retention"].(map[string]any); r.Status != 200 || ret["keep_revisions"] != float64(3) {
		t.Fatalf("put: %d %s", r.Status, r.Raw)
	}
	if r := call(t, api.h, "PUT", "/api/v1/settings", adminToken, map[string]any{"review_overrides": map[string]any{"dora/x": map[string]any{}}}); r.Status != 422 {
		t.Fatalf("unknown control override: %d %s", r.Status, r.Raw)
	}
}

func TestEvidenceUploadOverHTTP(t *testing.T) {
	api := newIdentityAPI(t, httpapi.RateLimit{})
	owner := userToken(t, api, "olga@example.com", access.RoleOwner)
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	for k, v := range map[string]string{"title": "Exit plan", "kind": "document", "source": "upload"} {
		_ = mw.WriteField(k, v)
	}
	_ = mw.WriteField("link", "dora/dora-exit-plans")
	fw, _ := mw.CreateFormFile("file", "exit.pdf")
	_, _ = fw.Write([]byte("pdf bytes"))
	_ = mw.Close()
	req := httptest.NewRequest("POST", "/api/v1/evidence/upload", &body)
	req.Header.Set("Authorization", "Bearer "+owner)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	rec := httptest.NewRecorder()
	api.h.ServeHTTP(rec, req)
	if rec.Code != 501 || !strings.Contains(rec.Body.String(), "managed_storage_not_configured") {
		t.Fatalf("upload without managed storage = %d %s", rec.Code, rec.Body)
	}
}

func TestEvidenceWithoutChecksumOverHTTP(t *testing.T) {
	h := newAPI(t, setup{engine: func(c *compliance.Config) { c.Workflow.AllowEvidenceWithoutChecksum = true }})
	in := map[string]any{"title": "M8 policy", "kind": "document", "source": "M8", "uri": "urn:m8:evidence:1",
		"links": []map[string]string{{"catalog": "dora", "control_id": "dora-incident-readiness"}}}
	r := call(t, h, "POST", "/api/v1/evidence", tokenA, in)
	if r.Status != 201 || r.Body["integrity"] != "no_checksum" {
		t.Fatalf("create without checksum: %d %s", r.Status, r.Raw)
	}
	id, _ := r.Body["id"].(string)
	if r := call(t, h, "POST", "/api/v1/evidence/"+id+"/verify", tokenA, nil); r.Status != 409 || errorCode(r) != "no_checksum" {
		t.Fatalf("engine-side check: %d %s", r.Status, r.Raw)
	}
	r = call(t, h, "POST", "/api/v1/evidence/"+id+"/checks", tokenA, map[string]string{"checksum": sha("policy")})
	if r.Status != 200 || r.Body["integrity"] != "verified" || r.Body["checksum"] != sha("policy") {
		t.Fatalf("attested check adopts the checksum: %d %s", r.Status, r.Raw)
	}

	strict := newAPI(t, setup{})
	if r := call(t, strict, "POST", "/api/v1/evidence", tokenA, in); r.Status != 422 {
		t.Fatalf("without the option: %d %s", r.Status, r.Raw)
	}
}
