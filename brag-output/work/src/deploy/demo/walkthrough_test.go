// SPDX-License-Identifier: Apache-2.0

// Package walkthrough drives the spec v2 §9 demo, steps 1 to 5, against the
// demo server: in process (compliance-engine demo's wiring) by default, or
// against a running stack when COMPLIANCE_DEMO_URL is set (demo-smoke.sh).
package walkthrough_test

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/nexops-one/compliance-engine/demo"
	"github.com/nexops-one/compliance-engine/pkg/cli"
)

// target is a demo server and what the walkthrough needs to know about it.
type target struct {
	base        string
	passwords   map[string]string
	evidenceDir string // as the server sees it
}

func newTarget(t *testing.T) target {
	t.Helper()
	if base := os.Getenv("COMPLIANCE_DEMO_URL"); base != "" {
		tg := target{base: strings.TrimSuffix(base, "/"), passwords: map[string]string{}, evidenceDir: "/demo/evidence"}
		if d := os.Getenv("COMPLIANCE_DEMO_EVIDENCE_DIR"); d != "" {
			tg.evidenceDir = d
		}
		for _, pair := range strings.Split(os.Getenv("COMPLIANCE_DEMO_PASSWORDS"), ",") {
			if email, pw, ok := strings.Cut(strings.TrimSpace(pair), "="); ok {
				tg.passwords[email] = pw
			}
		}
		if len(tg.passwords) < 3 {
			t.Fatal("COMPLIANCE_DEMO_PASSWORDS must hold email=password pairs for the demo users")
		}
		return tg
	}
	dir := t.TempDir()
	h, creds, cleanup, err := cli.DemoServer(context.Background(), dir, "walkthrough")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(cleanup)
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	tg := target{base: srv.URL, passwords: map[string]string{}, evidenceDir: dir}
	for _, c := range creds {
		tg.passwords[c.Email] = c.Password
	}
	return tg
}

// user is one demo user: a console session, then an API token created on
// the console's settings page.
type user struct {
	t       *testing.T
	tg      target
	email   string
	session string
	token   string
}

var noRedirect = &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}

var (
	csrfField = regexp.MustCompile(`name="csrf" value="([^"]+)"`)
	secret    = regexp.MustCompile(`<p class="secret">([^<]+)</p>`)
)

func (u *user) console(method, path string, form url.Values) (*http.Response, string) {
	u.t.Helper()
	var body io.Reader
	if form != nil {
		body = strings.NewReader(form.Encode())
	}
	req, _ := http.NewRequest(method, u.tg.base+path, body)
	if form != nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	if u.session != "" {
		req.AddCookie(&http.Cookie{Name: "ce_session", Value: u.session})
	}
	res, err := noRedirect.Do(req)
	if err != nil {
		u.t.Fatal(err)
	}
	defer res.Body.Close()
	data, _ := io.ReadAll(res.Body)
	return res, string(data)
}

// signIn signs the user in through the console and creates a personal API
// token with roles on the settings page.
func signIn(t *testing.T, tg target, email string, roles ...string) *user {
	t.Helper()
	u := &user{t: t, tg: tg, email: email}
	res, page := u.console("GET", "/console/signin", nil)
	m := csrfField.FindStringSubmatch(page)
	var pre string
	for _, ck := range res.Cookies() {
		if ck.Name == "ce_signin" {
			pre = ck.Value
		}
	}
	if m == nil || pre == "" {
		t.Fatalf("sign-in page: %d %s", res.StatusCode, page)
	}
	form := url.Values{"csrf": {m[1]}, "email": {email}, "password": {tg.passwords[email]}}
	req, _ := http.NewRequest("POST", tg.base+"/console/signin", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(&http.Cookie{Name: "ce_signin", Value: pre})
	res, err := noRedirect.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	for _, ck := range res.Cookies() {
		if ck.Name == "ce_session" {
			u.session = ck.Value
		}
	}
	if res.StatusCode != http.StatusSeeOther || u.session == "" {
		t.Fatalf("sign-in of %s = %d", email, res.StatusCode)
	}
	_, settings := u.console("GET", "/console/settings", nil)
	if !strings.Contains(settings, "Sample data: fictitious organization") {
		t.Fatal("the console shows the sample banner")
	}
	m = csrfField.FindStringSubmatch(settings)
	_, created := u.console("POST", "/console/settings/tokens", url.Values{"csrf": {m[1]}, "name": {"walkthrough"}, "role": roles})
	tok := secret.FindStringSubmatch(created)
	if tok == nil {
		t.Fatalf("token creation for %s: %s", email, created)
	}
	u.token = tok[1]
	return u
}

// api calls the API with an optional JSON body.
func (u *user) api(method, path string, body any, want int) map[string]any {
	u.t.Helper()
	var reader io.Reader
	contentType := ""
	if body != nil {
		data, _ := json.Marshal(body)
		reader, contentType = bytes.NewReader(data), "application/json"
	}
	return u.send(method, path, reader, contentType, want)
}

func (u *user) send(method, path string, body io.Reader, contentType string, want int) map[string]any {
	u.t.Helper()
	req, _ := http.NewRequest(method, u.tg.base+path, body)
	req.Header.Set("Authorization", "Bearer "+u.token)
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		u.t.Fatal(err)
	}
	defer res.Body.Close()
	data, _ := io.ReadAll(res.Body)
	if res.StatusCode != want {
		u.t.Fatalf("%s %s = %d, want %d: %s", method, path, res.StatusCode, want, data)
	}
	out := map[string]any{}
	_ = json.Unmarshal(data, &out)
	out["_raw"] = string(data)
	return out
}

func (u *user) importRegister(dryRun bool, want int) map[string]any {
	u.t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	fw, _ := mw.CreateFormFile("file", demo.RegisterFile)
	_, _ = fw.Write(demo.Register())
	_ = mw.Close()
	path := "/api/v1/imports?dry_run=false"
	if dryRun {
		path = "/api/v1/imports?dry_run=true"
	}
	return u.send("POST", path, &buf, mw.FormDataContentType(), want)
}

// evidenceSum returns the SHA-256 of a sample document from SHA256SUMS.
func evidenceSum(t *testing.T, name string) string {
	t.Helper()
	f, err := demo.Evidence().Open("SHA256SUMS")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		if sum, file, ok := strings.Cut(sc.Text(), "  "); ok && file == name {
			return "sha256:" + sum
		}
	}
	t.Fatalf("%s is not in SHA256SUMS", name)
	return ""
}

func fileURI(p string) string {
	s := filepath.ToSlash(p)
	if !strings.HasPrefix(s, "/") {
		s = "/" + s
	}
	return "file://" + s
}

func controlOf(t *testing.T, status map[string]any, id string) map[string]any {
	t.Helper()
	for _, fw := range status["frameworks"].([]any) {
		for _, c := range fw.(map[string]any)["controls"].([]any) {
			if c.(map[string]any)["control_id"] == id {
				return c.(map[string]any)
			}
		}
	}
	t.Fatalf("control %s not in status", id)
	return nil
}

func TestWalkthrough(t *testing.T) {
	tg := newTarget(t)
	owner := signIn(t, tg, "owner@demo.invalid", "owner")
	approver := signIn(t, tg, "approver@demo.invalid", "reviewer", "approver")
	auditor := signIn(t, tg, "auditor@demo.invalid", "auditor")
	const control = "dora-roi-provider-identification"

	// Step 1: import the spreadsheet, dry run first.
	dry := owner.importRegister(true, http.StatusOK)["result"].(map[string]any)
	if dry["rejected_records"].(float64) != 2 || len(dry["errors"].([]any)) != 2 || len(dry["warnings"].([]any)) != 1 {
		t.Fatalf("dry run: rejected %v, errors %v, warnings %v", dry["rejected_records"], dry["errors"], dry["warnings"])
	}
	committed := owner.importRegister(false, http.StatusCreated)["result"].(map[string]any)
	if committed["snapshot_id"] != "rev-1" {
		t.Fatalf("commit = %v", committed)
	}

	// Step 2: the validation report and the canonical model.
	comp := auditor.api("GET", "/api/v1/snapshots/current/completeness", nil, http.StatusOK)
	if gaps := comp["gaps"].([]any); len(gaps) != 9 {
		t.Fatalf("export-required gaps = %d", len(gaps))
	}

	// Step 3: not-assessed controls.
	status := auditor.api("GET", "/api/v1/status?catalogs=dora@1.0.0", nil, http.StatusOK)
	for _, id := range []string{"dora-roi-data-location", "dora-exit-plans", "dora-incident-readiness"} {
		if c := controlOf(t, status, id); c["status"] != "not_assessed" {
			t.Fatalf("%s = %v", id, c["status"])
		}
	}
	if c := controlOf(t, status, control); c["status"] != "monitoring" {
		t.Fatalf("%s = %v", control, c["status"])
	}

	// Step 4: owner, evidence, review and approval.
	owner.api("PUT", "/api/v1/assessments/dora/"+control, map[string]any{"owner": "owner@demo.invalid", "reviewer": "approver@demo.invalid"}, http.StatusOK)
	doc := "provider-audit-summary.md"
	ev := owner.api("POST", "/api/v1/evidence", map[string]any{
		"title": "Provider master data review Q4 2025", "kind": "report", "source": "GRC",
		"uri": fileURI(tg.evidenceDir + "/" + doc), "checksum": evidenceSum(t, doc),
		"links": []map[string]string{{"catalog": "dora", "control_id": control}},
	}, http.StatusCreated)
	verified := owner.api("POST", "/api/v1/evidence/"+ev["id"].(string)+"/verify", nil, http.StatusOK)
	if verified["integrity"] != "verified" {
		t.Fatalf("verify = %v", verified["_raw"])
	}
	owner.api("POST", "/api/v1/assessments/dora/"+control+"/submit", map[string]any{}, http.StatusOK)
	approver.api("POST", "/api/v1/assessments/dora/"+control+"/recommend", map[string]any{"note": "master data complete"}, http.StatusOK)
	eval := approver.api("POST", "/api/v1/evaluations", map[string]any{"catalogs": []string{"dora@1.0.0"}}, http.StatusCreated)
	approver.api("POST", "/api/v1/assessments/dora/"+control+"/approve", map[string]any{"evaluation_id": eval["id"]}, http.StatusOK)
	status = auditor.api("GET", "/api/v1/status?catalogs=dora@1.0.0", nil, http.StatusOK)
	if c := controlOf(t, status, control); c["status"] != "ready" {
		t.Fatalf("after approval %s = %v", control, c["status"])
	}

	// Step 5: Profile B, marked as a sample.
	rep := approver.api("POST", "/api/v1/reports", map[string]any{"profile": "profile_b", "catalogs": []string{"dora@1.0.0"}}, http.StatusCreated)
	if rep["sample"] != true {
		t.Fatalf("report = %v", rep["_raw"])
	}
	id := rep["id"].(string)
	pdf := auditor.api("GET", "/api/v1/reports/"+id+"/files/report.pdf", nil, http.StatusOK)["_raw"].(string)
	if !strings.HasPrefix(pdf, "%PDF-") {
		t.Fatal("report.pdf is not a PDF")
	}
	if regen := auditor.api("POST", "/api/v1/reports/"+id+"/regenerate", nil, http.StatusOK); regen["identical"] != true {
		t.Fatalf("regenerate = %v", regen["_raw"])
	}
	if v := auditor.api("GET", "/api/v1/audit?verify=true&limit=1", nil, http.StatusOK)["verification"].(map[string]any); v["ok"] != true {
		t.Fatalf("audit chain = %v", v)
	}
}
