// SPDX-License-Identifier: Apache-2.0

package console_test

import (
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/nexops-one/compliance-engine/pkg/access"
	"github.com/nexops-one/compliance-engine/pkg/compliance"
	"github.com/nexops-one/compliance-engine/pkg/console"
	"github.com/nexops-one/compliance-engine/pkg/engine"
	"github.com/nexops-one/compliance-engine/pkg/evidence"
	"github.com/nexops-one/compliance-engine/pkg/httpapi"
	"github.com/nexops-one/compliance-engine/sdk/adapter"
	"github.com/nexops-one/compliance-engine/sdk/schema"
)

// loadSample ingests the engine's sample batch with a manifest declaring
// exactly the fields it holds.
func (h *harness) loadSample() {
	h.t.Helper()
	f, err := os.Open(filepath.Join("..", "compliance", "testdata", "sample-batch.json"))
	if err != nil {
		h.t.Fatal(err)
	}
	defer f.Close()
	b, err := adapter.DecodeBatch(f)
	if err != nil {
		h.t.Fatal(err)
	}
	supplies := map[string][]string{}
	for entity, recs := range b.Entities {
		seen := map[string]bool{}
		for _, r := range recs {
			for field := range r {
				if field != schema.MetaField && !seen[field] {
					seen[field] = true
					supplies[entity] = append(supplies[entity], field)
				}
			}
		}
		sort.Strings(supplies[entity])
	}
	m := adapter.Manifest{Name: b.Source.Adapter, Version: b.Source.AdapterVersion, SchemaVersion: b.SchemaVersion, Supplies: supplies,
		Modes: []adapter.Mode{adapter.ModeIncremental, adapter.ModeFull}}
	if err := h.eng.RegisterManifest(ctx, scope, m); err != nil {
		h.t.Fatal(err)
	}
	if _, err := h.eng.Ingest(ctx, scope, b); err != nil {
		h.t.Fatal(err)
	}
}

// changeSample ingests the sample batch with one provider renamed.
func (h *harness) changeSample() {
	h.t.Helper()
	f, err := os.Open(filepath.Join("..", "compliance", "testdata", "sample-batch.json"))
	if err != nil {
		h.t.Fatal(err)
	}
	defer f.Close()
	b, err := adapter.DecodeBatch(f)
	if err != nil {
		h.t.Fatal(err)
	}
	b.Entities["ict_provider"][0]["legal_name"] = "Renamed Provider Ltd"
	if _, err := h.eng.Ingest(ctx, scope, b); err != nil {
		h.t.Fatal(err)
	}
}

// monitoringControl returns a DORA control the sample data satisfies.
func (h *harness) monitoringControl(requiresEvidence bool) string {
	h.t.Helper()
	res, err := h.eng.Status(ctx, scope, nil)
	if err != nil {
		h.t.Fatal(err)
	}
	for _, fw := range res.Frameworks {
		if fw.Catalog.Catalog != "dora" {
			continue
		}
		for _, c := range fw.Controls {
			if c.ComputedStatus == engine.StatusMonitoring && c.RequiresEvidence == requiresEvidence {
				return c.ControlID
			}
		}
	}
	h.t.Fatalf("no monitoring DORA control (requires evidence %v)", requiresEvidence)
	return ""
}

func TestConsoleActionsMatchAPI(t *testing.T) {
	h := newHarness(t, options{})
	api := map[string]httpapi.Route{}
	for _, r := range httpapi.Routes() {
		api[r.Method+" "+r.Pattern] = r
	}
	mirrored := 0
	for _, rt := range h.c.Routes() {
		if rt.API == "" {
			// Session actions have no API counterpart; every other action must name one.
			if rt.Method == "POST" && !rt.Public && rt.Pattern != console.Prefix+"/signout" && rt.Pattern != console.Prefix+"/workspace" {
				t.Errorf("console action %s %s names no API route", rt.Method, rt.Pattern)
			}
			continue
		}
		a, ok := api[rt.API]
		if !ok {
			t.Errorf("%s %s mirrors unknown API route %q", rt.Method, rt.Pattern, rt.API)
			continue
		}
		mirrored++
		if a.Permission != rt.Permission {
			t.Errorf("%s %s requires %q, API %s requires %q", rt.Method, rt.Pattern, rt.Permission, rt.API, a.Permission)
		}
		if a.Feature != rt.Feature {
			t.Errorf("%s %s exercises feature %q, API %s %q", rt.Method, rt.Pattern, rt.Feature, rt.API, a.Feature)
		}
	}
	if mirrored < 15 {
		t.Fatalf("only %d console routes mirror the API", mirrored)
	}
}

var scoreLine = regexp.MustCompile(`Score[^<]*<small>with coverage</small> \d+%`)

func TestScoreAlwaysWithCoverage(t *testing.T) {
	h := newHarness(t, options{})
	h.loadSample()
	cl := h.signInAs("auditor@example.com", h.user("auditor@example.com", access.RoleAuditor))
	for _, path := range []string{"/console/controls", "/console/controls?catalog=dora@1.0.0"} {
		body := cl.get(path).Body
		scores := strings.Count(body, `class="metric">Score`)
		if scores == 0 || len(scoreLine.FindAllString(body, -1)) != scores {
			t.Fatalf("%s: %d scores, %d with coverage:\n%s", path, scores, len(scoreLine.FindAllString(body, -1)), body)
		}
	}
	mustContain(t, "controls", cl.get("/console/controls").Body, "Overall", "not assessed", "DORA")
	if res := cl.get("/console/controls?catalog=nope"); res.Status != http.StatusBadRequest {
		t.Fatalf("bad catalog = %d", res.Status)
	}
}

// evidenceFile writes a file under a fresh evidence root and returns the
// root, the file URI and its checksum.
func evidenceFile(t *testing.T) (string, string, string) {
	t.Helper()
	root := t.TempDir()
	data := []byte("SAMPLE DATA: ICT third-party risk policy\n")
	p := filepath.Join(root, "policy.md")
	if err := os.WriteFile(p, data, 0o600); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(data)
	return root, "file:///" + strings.TrimPrefix(filepath.ToSlash(p), "/"), "sha256:" + hex.EncodeToString(sum[:])
}

var evaluationField = regexp.MustCompile(`name="evaluation_id" value="([^"]*)"`)

func TestControlReviewCycle(t *testing.T) {
	root, uri, sum := evidenceFile(t)
	h := newHarness(t, options{engine: func(c *compliance.Config) { c.Workflow.Verifier = evidence.Verifier{Root: root} }})
	h.loadSample()
	ctl := h.monitoringControl(true)
	path := "/console/controls/dora/" + ctl
	owner := h.signInAs("owner@example.com", h.user("owner@example.com", access.RoleOwner))
	approver := h.signInAs("approver@example.com", h.user("approver@example.com", access.RoleApprover, access.RoleReviewer))
	auditor := h.signInAs("auditor@example.com", h.user("auditor@example.com", access.RoleAuditor))

	page := auditor.get(path)
	mustContain(t, "auditor's control page", page.Body, ctl, "Effective status", "No evidence is linked")
	for _, form := range []string{"/assign", "/submit", "/approve", "/evidence\""} {
		if strings.Contains(page.Body, path+form) {
			t.Fatalf("an auditor sees the %s form", form)
		}
	}
	if res := auditor.post(path+"/submit", nil); res.Status != http.StatusForbidden || !strings.Contains(res.Body, "workflow.submit") {
		t.Fatalf("auditor submit = %d", res.Status)
	}

	steps := []struct {
		cl   *client
		path string
		form url.Values
		ok   string
	}{
		{owner, path + "/assign", url.Values{"owner": {"owner@example.com"}, "reviewer": {"approver@example.com"}, "due": {"2026-12-31"}, "notes": {"quarterly"}}, "assigned"},
		{owner, path + "/evidence", url.Values{"title": {"ICT third-party risk policy"}, "kind": {"document"}, "source": {"SharePoint"}, "uri": {uri}, "checksum": {sum}}, "evidence"},
		{owner, path + "/submit", nil, "acted"},
		{approver, path + "/recommend", url.Values{"note": {"looks complete"}}, "acted"},
	}
	for _, s := range steps {
		res := s.cl.post(s.path, s.form)
		if res.Status != http.StatusSeeOther || res.location() != path+"?ok="+s.ok {
			t.Fatalf("%s = %d %q\n%s", s.path, res.Status, res.location(), res.Body)
		}
	}
	page = owner.get(path)
	evid := regexp.MustCompile(`/console/evidence/(evd-[0-9a-f]+)/verify`).FindStringSubmatch(page.Body)
	if evid == nil {
		t.Fatalf("no verify form: %s", page.Body)
	}
	res := owner.post("/console/evidence/"+evid[1]+"/verify", url.Values{"back": {path}})
	if res.Status != http.StatusSeeOther || res.location() != path+"?ok=verified" {
		t.Fatalf("verify = %d %s", res.Status, res.Body)
	}

	page = approver.get(path)
	m := evaluationField.FindStringSubmatch(page.Body)
	if m == nil || m[1] == "" {
		t.Fatalf("the approver's page embeds the evaluation to approve: %s", page.Body)
	}
	res = approver.post(path+"/approve", url.Values{"evaluation_id": {m[1]}})
	if res.Status != http.StatusSeeOther {
		t.Fatalf("approve = %d %s", res.Status, res.Body)
	}
	page = auditor.get(path)
	mustContain(t, "approved control", page.Body, `badge st-ready">ready`, "by approver@example.com", "owner@example.com", "2026-12-31",
		"looks complete", "verified")
	if res := owner.post(path+"/reopen", url.Values{"reason": {"scope changed"}}); res.Status != http.StatusSeeOther {
		t.Fatalf("reopen = %d", res.Status)
	}
}

func TestControlRefusals(t *testing.T) {
	root, uri, sum := evidenceFile(t)
	h := newHarness(t, options{engine: func(c *compliance.Config) { c.Workflow.Verifier = evidence.Verifier{Root: root} }})
	h.loadSample()
	ctl := h.monitoringControl(true)
	path := "/console/controls/dora/" + ctl
	both := h.signInAs("both@example.com", h.user("both@example.com", access.RoleOwner, access.RoleApprover))
	approver := h.signInAs("approver@example.com", h.user("approver@example.com", access.RoleApprover))

	both.post(path+"/assign", url.Values{"owner": {"both@example.com"}})
	both.post(path+"/submit", nil)
	m := evaluationField.FindStringSubmatch(approver.get(path).Body)

	res := approver.post(path+"/approve", url.Values{"evaluation_id": {m[1]}})
	if res.Status != http.StatusConflict || !strings.Contains(res.Body, "evidence_required") || !strings.Contains(res.Body, `role="alert"`) {
		t.Fatalf("approval without evidence = %d", res.Status)
	}
	both.post(path+"/evidence", url.Values{"title": {"Policy"}, "kind": {"document"}, "source": {"SharePoint"}, "uri": {uri}, "checksum": {sum}})
	m = evaluationField.FindStringSubmatch(both.get(path).Body)
	res = both.post(path+"/approve", url.Values{"evaluation_id": {m[1]}})
	if res.Status != http.StatusConflict || !strings.Contains(res.Body, "self_approval_forbidden") {
		t.Fatalf("self-approval = %d", res.Status)
	}
	res = approver.post(path+"/approve", url.Values{"evaluation_id": {"eval-unknown"}})
	if res.Status != http.StatusNotFound && res.Status != http.StatusConflict {
		t.Fatalf("unknown evaluation = %d", res.Status)
	}
	res = approver.post(path+"/reject", url.Values{"reason": {""}})
	if res.Status != http.StatusUnprocessableEntity {
		t.Fatalf("reject without reason = %d", res.Status)
	}
	res = both.post(path+"/assign", url.Values{"owner": {"nobody@example.com"}})
	if res.Status != http.StatusUnprocessableEntity || !strings.Contains(res.Body, "not a user") {
		t.Fatalf("unknown owner = %d %s", res.Status, res.Body)
	}
	if res := approver.get("/console/controls/dora/NOPE"); res.Status != http.StatusNotFound {
		t.Fatalf("unknown control = %d", res.Status)
	}
}

func TestStaleApprovalRefused(t *testing.T) {
	root, uri, sum := evidenceFile(t)
	h := newHarness(t, options{engine: func(c *compliance.Config) { c.Workflow.Verifier = evidence.Verifier{Root: root} }})
	h.loadSample()
	ctl := h.monitoringControl(true)
	path := "/console/controls/dora/" + ctl
	owner := h.signInAs("owner@example.com", h.user("owner@example.com", access.RoleOwner))
	approver := h.signInAs("approver@example.com", h.user("approver@example.com", access.RoleApprover))
	owner.post(path+"/assign", url.Values{"owner": {"owner@example.com"}})
	owner.post(path+"/evidence", url.Values{"title": {"Policy"}, "kind": {"document"}, "source": {"SharePoint"}, "uri": {uri}, "checksum": {sum}})
	owner.post(path+"/submit", nil)
	m := evaluationField.FindStringSubmatch(approver.get(path).Body)
	h.changeSample() // a new snapshot after the approver looked
	if snap, _ := h.eng.Snapshot(ctx, scope, ""); snap.ID != "rev-2" {
		t.Fatalf("snapshot = %s", snap.ID)
	}
	res := approver.post(path+"/approve", url.Values{"evaluation_id": {m[1]}})
	if res.Status != http.StatusConflict || !strings.Contains(res.Body, "stale_evaluation") {
		t.Fatalf("stale approval = %d", res.Status)
	}
	m = evaluationField.FindStringSubmatch(approver.get(path).Body)
	if res := approver.post(path+"/approve", url.Values{"evaluation_id": {m[1]}}); res.Status != http.StatusSeeOther {
		t.Fatalf("approval of the current evaluation = %d %s", res.Status, res.Body)
	}
}
