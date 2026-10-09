// SPDX-License-Identifier: Apache-2.0

package console_test

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"

	"github.com/nexops-one/compliance-engine/pkg/access"
	"github.com/nexops-one/compliance-engine/pkg/store"
)

func TestEveryPageRenders(t *testing.T) {
	h := newHarness(t, options{})
	h.loadSample()
	cl := h.signInAs("root@example.com", h.user("root@example.com", access.RoleOwner, access.RoleReviewer, access.RoleApprover, access.RoleAdmin, access.RoleAuditor))
	for _, p := range pages(h.c) {
		res := cl.get(p)
		if res.Status != http.StatusOK && res.Status != http.StatusNotFound {
			t.Errorf("GET %s = %d", p, res.Status)
		}
		if res.Status == http.StatusOK && !strings.Contains(res.Body, "</html>") {
			t.Errorf("GET %s: truncated page", p)
		}
	}
}

func auditActions(t *testing.T, h *harness) []string {
	t.Helper()
	evs, err := h.st.AuditEvents(ctx, scope, store.AuditQuery{})
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, e := range evs {
		out = append(out, e.Action)
	}
	return out
}

func count(xs []string, x string) int {
	n := 0
	for _, v := range xs {
		if v == x {
			n++
		}
	}
	return n
}

func TestReportsPages(t *testing.T) {
	h := newHarness(t, options{})
	h.loadSample()
	approver := h.signInAs("approver@example.com", h.user("approver@example.com", access.RoleApprover))
	auditor := h.signInAs("auditor@example.com", h.user("auditor@example.com", access.RoleAuditor))

	page := approver.get("/console/reports")
	mustContain(t, "reports page", page.Body, "profile_b", "report.profile_b", `name="catalog" value="dora@1.0.0"`, `name="format" value="pdf"`, "No report has been generated")
	if strings.Contains(auditor.get("/console/reports").Body, `action="/console/reports"`) {
		t.Fatal("an auditor sees no generate form")
	}
	if res := auditor.post("/console/reports", url.Values{"profile": {"profile_b"}}); res.Status != http.StatusForbidden {
		t.Fatalf("auditor generate = %d", res.Status)
	}

	res := approver.post("/console/reports", url.Values{"profile": {"profile_b"}, "catalog": {"dora@1.0.0"}, "format": {"pdf"}})
	m := regexp.MustCompile(`^/console/reports/(rpt-[0-9a-f]+)\?ok=generated$`).FindStringSubmatch(res.location())
	if res.Status != http.StatusSeeOther || m == nil {
		t.Fatalf("generate = %d %q %s", res.Status, res.location(), res.Body)
	}
	before := count(auditActions(t, h), "report.download")
	page = auditor.get("/console/reports/" + m[1])
	mustContain(t, "report page", page.Body, "profile_b", "dora@1.0.0", "report.json", "report.pdf", "Facts hash", "approver@example.com")
	if got := count(auditActions(t, h), "report.download"); got != before {
		t.Fatal("viewing the report page is not a download")
	}
	res = auditor.get("/console/reports/" + m[1] + "/files/report.pdf")
	if res.Status != 200 || !strings.HasPrefix(res.Body, "%PDF-") || !strings.Contains(res.Header.Get("Content-Disposition"), "report.pdf") ||
		res.Header.Get("Content-Type") != "application/pdf" {
		t.Fatalf("pdf = %d %v", res.Status, res.Header)
	}
	if got := count(auditActions(t, h), "report.download"); got != before+1 {
		t.Fatalf("downloads audited = %d", got-before)
	}
	res = auditor.post("/console/reports/"+m[1]+"/verify", nil)
	mustContain(t, "verify", res.Body, "Regenerated identically")
	if res := auditor.get("/console/reports/rpt-nope"); res.Status != http.StatusNotFound {
		t.Fatalf("unknown report = %d", res.Status)
	}
	if res := approver.post("/console/reports", url.Values{"profile": {"nope"}}); res.Status != http.StatusNotFound || !strings.Contains(res.Body, "<h1>Reports</h1>") {
		t.Fatalf("unknown profile = %d", res.Status)
	}
}

var secretValue = regexp.MustCompile(`<p class="secret">([^<]+)</p>`)

func TestTokenShownOnce(t *testing.T) {
	h := newHarness(t, options{})
	owner := h.signInAs("owner@example.com", h.user("owner@example.com", access.RoleOwner, access.RoleAuditor))
	res := owner.post("/console/settings/tokens", url.Values{"name": {"ci"}, "role": {"owner"}, "expires": {"2027-01-31"}})
	m := secretValue.FindStringSubmatch(res.Body)
	if res.Status != http.StatusOK || m == nil {
		t.Fatalf("create = %d %s", res.Status, res.Body)
	}
	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("Authorization", "Bearer "+m[1])
	if p, err := h.ids.Authenticate(req); err != nil || p.Actor == "" {
		t.Fatalf("the shown token authenticates: %v", err)
	}
	page := owner.get("/console/settings")
	if strings.Contains(page.Body, m[1]) || !strings.Contains(page.Body, "ci") || !strings.Contains(page.Body, "2027-01-31") {
		t.Fatal("the value is shown once; the token is listed")
	}
	if res := owner.post("/console/settings/tokens", url.Values{"name": {"x"}, "role": {"admin"}}); res.Status != http.StatusForbidden || !strings.Contains(res.Body, "role_not_held") {
		t.Fatalf("role not held = %d", res.Status)
	}
	id := regexp.MustCompile(`/console/settings/tokens/(tok-[0-9a-f]+)/revoke`).FindStringSubmatch(page.Body)
	if res := owner.post("/console/settings/tokens/"+id[1]+"/revoke", nil); res.Status != http.StatusSeeOther {
		t.Fatalf("revoke = %d", res.Status)
	}
	if _, err := h.ids.Authenticate(req); err == nil {
		t.Fatal("a revoked token is refused")
	}
}

func TestMembersPage(t *testing.T) {
	h := newHarness(t, options{})
	admin := h.signInAs("admin@example.com", h.user("admin@example.com", access.RoleAdmin))
	owner := h.signInAs("owner@example.com", h.user("owner@example.com", access.RoleOwner))
	if strings.Contains(owner.get("/console/settings").Body, "Add a member") {
		t.Fatal("only admins manage members")
	}
	if res := owner.post("/console/settings/members", url.Values{"email": {"x@example.com"}, "role": {"owner"}}); res.Status != http.StatusForbidden {
		t.Fatalf("owner adds member = %d", res.Status)
	}
	if res := admin.post("/console/settings/members", url.Values{"email": {"new@example.com"}, "role": {"owner", "auditor"}}); res.Status != http.StatusSeeOther {
		t.Fatalf("add = %d %s", res.Status, res.Body)
	}
	mustContain(t, "members", admin.get("/console/settings").Body, "new@example.com", "Remove new@example.com")
	if res := admin.post("/console/settings/members/remove", url.Values{"email": {"new@example.com"}}); res.Status != http.StatusSeeOther {
		t.Fatalf("remove = %d", res.Status)
	}
	res := admin.post("/console/settings/members/remove", url.Values{"email": {"admin@example.com"}})
	if res.Status != http.StatusConflict || !strings.Contains(res.Body, "last_admin") {
		t.Fatalf("removing the last admin = %d", res.Status)
	}
}

func TestWorkspaceSettingsPage(t *testing.T) {
	h := newHarness(t, options{})
	admin := h.signInAs("admin@example.com", h.user("admin@example.com", access.RoleAdmin))
	res := admin.post("/console/settings/retention", url.Values{"revision_days": {"400"}, "keep_revisions": {"3"}, "evaluation_days": {"730"}})
	if res.Status != http.StatusSeeOther {
		t.Fatalf("retention = %d %s", res.Status, res.Body)
	}
	if set, _ := h.eng.Settings(ctx, scope); set.Retention != (store.RetentionPolicy{RevisionDays: 400, KeepRevisions: 3, EvaluationDays: 730}) {
		t.Fatalf("retention = %+v", set.Retention)
	}
	if res := admin.post("/console/settings/retention", url.Values{"keep_revisions": {"0"}}); res.Status != http.StatusUnprocessableEntity {
		t.Fatalf("invalid retention = %d", res.Status)
	}
	if res := admin.post("/console/settings/overrides", url.Values{"control": {"dora/nope"}}); res.Status != http.StatusUnprocessableEntity {
		t.Fatalf("unknown control override = %d", res.Status)
	}
	res = admin.post("/console/settings/overrides", url.Values{"control": {"dora/dora-roi-data-location"}, "interval": {"P180D"}, "evidence": {"not_required"}})
	if res.Status != http.StatusSeeOther {
		t.Fatalf("override = %d %s", res.Status, res.Body)
	}
	mustContain(t, "settings", admin.get("/console/settings").Body, "dora/dora-roi-data-location", "P180D", "not required")
	admin.post("/console/settings/overrides", url.Values{"control": {"dora/dora-roi-data-location"}, "remove": {"on"}})
	if set, _ := h.eng.Settings(ctx, scope); len(set.ReviewOverrides) != 0 {
		t.Fatalf("overrides = %+v", set.ReviewOverrides)
	}
	owner := h.signInAs("owner@example.com", h.user("owner@example.com", access.RoleOwner))
	if res := owner.post("/console/settings/retention", url.Values{"keep_revisions": {"2"}}); res.Status != http.StatusForbidden {
		t.Fatalf("owner changes retention = %d", res.Status)
	}
}

func TestAuditPage(t *testing.T) {
	h := newHarness(t, options{})
	h.loadSample()
	auditor := h.signInAs("auditor@example.com", h.user("auditor@example.com", access.RoleAuditor))
	owner := h.signInAs("owner@example.com", h.user("owner@example.com", access.RoleOwner))
	page := auditor.get("/console/audit?verify=1")
	mustContain(t, "audit page", page.Body, "ingestion.commit", "member.set", "The chain verifies")
	if strings.Contains(owner.get("/console/settings").Body, `href="/console/audit"`) {
		t.Fatal("owners see no audit link")
	}
	if res := owner.get("/console/audit"); res.Status != http.StatusForbidden {
		t.Fatalf("owner audit = %d", res.Status)
	}
	if res := auditor.get("/console/audit?after=9999"); res.Status != 200 || !strings.Contains(res.Body, "No events after #9999") {
		t.Fatalf("past the end = %d", res.Status)
	}
}
