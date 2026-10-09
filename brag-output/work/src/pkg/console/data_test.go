// SPDX-License-Identifier: Apache-2.0

package console_test

import (
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/nexops-one/compliance-engine/pkg/access"
	"github.com/nexops-one/compliance-engine/pkg/compliance"
	"github.com/nexops-one/compliance-engine/pkg/console"
)

const providersCSV = "provider_id_code,provider_id_type,legal_name,person_type,hq_country,_source_record_ref\n" +
	"PROV-1,EXAMPLE_CODE,First Provider Ltd,EXAMPLE_CODE,IE,\n" +
	"PROV-2,EXAMPLE_CODE,Second Provider Ltd,EXAMPLE_CODE,Germany,\n"

var pendingField = regexp.MustCompile(`name="pending" value="([^"]+)"`)

// validate uploads providersCSV for a dry run and returns the report page.
func validate(t *testing.T, cl *client) (response, string) {
	t.Helper()
	res := cl.upload("/console/import", map[string]string{"mode": "incremental"}, "file", "ict_provider.csv", []byte(providersCSV))
	if res.Status != http.StatusOK {
		t.Fatalf("validate = %d %s", res.Status, res.Body)
	}
	m := pendingField.FindStringSubmatch(res.Body)
	if m == nil {
		t.Fatalf("no pending import on the report: %s", res.Body)
	}
	return res, m[1]
}

func TestImportLifecycle(t *testing.T) {
	h := newHarness(t, options{})
	owner := h.signInAs("owner@example.com", h.user("owner@example.com", access.RoleOwner))

	page := owner.get("/console/import")
	mustContain(t, "import page", page.Body, "/console/import/templates.xlsx", "/console/import/templates/ict_provider.csv", `name="file"`, "No data has been ingested")

	report, pending := validate(t, owner)
	mustContain(t, "import report", report.Body, "ict_provider.csv", "Rejected records</dt><dd>1", "ict_provider:3", "hq_country", "Commit this import")
	if snap, _ := h.eng.Snapshot(ctx, scope, ""); len(snap.Records) != 0 {
		t.Fatal("a dry run stores nothing")
	}

	other := h.signInAs("owner@example.com", h.resetPassword(t, "owner@example.com"))
	res := other.post("/console/import/commit", url.Values{"pending": {pending}})
	if res.Status != http.StatusConflict || !strings.Contains(res.Body, "import_expired") {
		t.Fatalf("another session's pending import = %d", res.Status)
	}

	res = owner.post("/console/import/commit", url.Values{"pending": {pending}})
	if res.Status != http.StatusSeeOther || !strings.HasPrefix(res.location(), "/console/import?ok=imported&ingestion=") {
		t.Fatalf("commit = %d %q %s", res.Status, res.location(), res.Body)
	}
	snap, _ := h.eng.Snapshot(ctx, scope, "")
	if len(snap.Records) != 1 || snap.Records[0].Key == "" {
		t.Fatalf("committed records = %+v", snap.Records)
	}
	res = owner.post("/console/import/commit", url.Values{"pending": {pending}})
	if res.Status != http.StatusConflict || !strings.Contains(res.Body, "<h1>Import</h1>") {
		t.Fatalf("a pending import commits once = %d", res.Status)
	}

	page = owner.get("/console/import?ok=imported")
	mustContain(t, "import page after commit", page.Body, "The import was committed.", "rev-1", "Roll back to the snapshot before it")
	ing := regexp.MustCompile(`/console/import/([^/]+)/rollback`).FindStringSubmatch(page.Body)
	res = owner.post("/console/import/"+ing[1]+"/rollback", nil)
	if res.Status != http.StatusSeeOther || res.location() != "/console/import?ok=rolled_back" {
		t.Fatalf("rollback = %d %s", res.Status, res.Body)
	}
	if snap, _ := h.eng.Snapshot(ctx, scope, ""); len(snap.Records) != 0 {
		t.Fatal("rollback restores the empty snapshot")
	}
	res = owner.post("/console/import/"+ing[1]+"/rollback", nil)
	if res.Status != http.StatusConflict || !strings.Contains(res.Body, "already_rolled_back") {
		t.Fatalf("second rollback = %d", res.Status)
	}
}

func (h *harness) resetPassword(t *testing.T, email string) string {
	t.Helper()
	pw, err := h.ids.ResetPassword(ctx, scope.TenantID, email)
	if err != nil {
		t.Fatal(err)
	}
	return pw
}

func TestPendingImportExpires(t *testing.T) {
	h := newHarness(t, options{})
	owner := h.signInAs("owner@example.com", h.user("owner@example.com", access.RoleOwner))
	_, pending := validate(t, owner)
	h.now = h.now.Add(29 * time.Minute)
	owner.get("/console/import") // keeps the session active
	h.now = h.now.Add(2 * time.Minute)
	res := owner.post("/console/import/commit", url.Values{"pending": {pending}})
	if res.Status != http.StatusConflict {
		t.Fatalf("expired pending import = %d", res.Status)
	}
}

func TestImportRefusals(t *testing.T) {
	h := newHarness(t, options{})
	auditor := h.signInAs("auditor@example.com", h.user("auditor@example.com", access.RoleAuditor))
	page := auditor.get("/console/import")
	if strings.Contains(page.Body, `name="file"`) {
		t.Fatal("an auditor sees no upload form")
	}
	res := auditor.upload("/console/import", nil, "file", "ict_provider.csv", []byte(providersCSV))
	if res.Status != http.StatusForbidden || !strings.Contains(res.Body, "data.write") {
		t.Fatalf("auditor upload = %d", res.Status)
	}
	owner := h.signInAs("owner@example.com", h.user("owner@example.com", access.RoleOwner))
	res = owner.upload("/console/import", nil, "file", "notes.txt", []byte("hello"))
	if res.Status != http.StatusUnprocessableEntity || !strings.Contains(res.Body, "invalid_file") || !strings.Contains(res.Body, "<h1>Import</h1>") {
		t.Fatalf("bad file = %d %s", res.Status, res.Body)
	}
	res = owner.post("/console/import", nil)
	if res.Status != http.StatusBadRequest || !strings.Contains(res.Body, "missing_parameter") {
		t.Fatalf("no file = %d", res.Status)
	}
}

func TestTemplateDownloads(t *testing.T) {
	h := newHarness(t, options{console: func(o *console.Options) { o.SampleRegister = []byte("PK-sample") }})
	cl := h.signInAs("auditor@example.com", h.user("auditor@example.com", access.RoleAuditor))
	res := cl.get("/console/import/templates.xlsx")
	if res.Status != 200 || !strings.Contains(res.Header.Get("Content-Disposition"), "compliance-templates-") || !strings.HasPrefix(res.Body, "PK") {
		t.Fatalf("workbook = %d %v", res.Status, res.Header)
	}
	res = cl.get("/console/import/templates/ict_provider.csv")
	if res.Status != 200 || !strings.HasPrefix(res.Body, "provider_id_code,") {
		t.Fatalf("csv = %d %s", res.Status, res.Body)
	}
	if res := cl.get("/console/import/templates/nope.csv"); res.Status != 404 {
		t.Fatalf("unknown entity = %d", res.Status)
	}
	if res := cl.get("/console/import/sample-register.xlsx"); res.Status != 404 {
		t.Fatalf("sample register outside a sample workspace = %d", res.Status)
	}
	yes := true
	if _, err := h.eng.UpdateSettings(ctx, scope, compliance.SettingsInput{Sample: &yes}); err != nil {
		t.Fatal(err)
	}
	mustContain(t, "sample import page", cl.get("/console/import").Body, "/console/import/sample-register.xlsx")
	if res := cl.get("/console/import/sample-register.xlsx"); res.Status != 200 || res.Body != "PK-sample" {
		t.Fatalf("sample register = %d", res.Status)
	}
}

func TestRecordsAndCompletenessPages(t *testing.T) {
	h := newHarness(t, options{})
	owner := h.signInAs("owner@example.com", h.user("owner@example.com", access.RoleOwner))
	_, pending := validate(t, owner)
	owner.post("/console/import/commit", url.Values{"pending": {pending}})

	page := owner.get("/console/records")
	mustContain(t, "records page", page.Body, "rev-1", `<a href="/console/records/ict_provider">ict_provider</a>`)
	page = owner.get("/console/records/ict_provider")
	key := regexp.MustCompile(`record\?key=([^"]+)"`).FindStringSubmatch(page.Body)
	if key == nil {
		t.Fatalf("entity page = %s", page.Body)
	}
	page = owner.get("/console/records/ict_provider/record?key=" + key[1])
	mustContain(t, "record page", page.Body, "First Provider Ltd", "provided", "missing", "Provenance", "create")
	if res := owner.get("/console/records/ict_provider/record?key=nope"); res.Status != 404 {
		t.Fatalf("unknown record = %d", res.Status)
	}
	if res := owner.get("/console/records/nope"); res.Status != 404 {
		t.Fatalf("unknown entity = %d", res.Status)
	}
	page = owner.get("/console/completeness")
	mustContain(t, "completeness page", page.Body, "not_supplied_by_any_adapter", "ict_provider", "Codelists not verified")
}
