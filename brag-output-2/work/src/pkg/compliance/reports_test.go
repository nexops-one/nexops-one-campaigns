// SPDX-License-Identifier: Apache-2.0

package compliance_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nexops-one/compliance-engine/pkg/catalog"
	"github.com/nexops-one/compliance-engine/pkg/compliance"
	"github.com/nexops-one/compliance-engine/pkg/crypt"
	"github.com/nexops-one/compliance-engine/pkg/extension"
	"github.com/nexops-one/compliance-engine/pkg/report"
	"github.com/nexops-one/compliance-engine/pkg/store"
	"github.com/nexops-one/compliance-engine/pkg/store/sealed"
	"github.com/nexops-one/compliance-engine/pkg/workflow"
)

var doraRefs = []catalog.Ref{{Catalog: "dora", Version: "1.0.0"}}

func auditActions(t *testing.T, eng *compliance.Engine) []string {
	t.Helper()
	evs, err := eng.AuditEvents(ctx, scopeA, store.AuditQuery{})
	if err != nil {
		t.Fatal(err)
	}
	out := []string{}
	for _, e := range evs {
		out = append(out, e.Action)
	}
	return out
}

func countAction(actions []string, a string) int {
	n := 0
	for _, x := range actions {
		if x == a {
			n++
		}
	}
	return n
}

func TestGenerateReport(t *testing.T) {
	w := newWorkflowEnv(t)
	w.addEvidence(t, "register.pdf", []byte("register"), monitored)
	w.approve(t, monitored)
	r, err := w.eng.GenerateReport(w.owner, scopeA, compliance.ReportRequest{Profile: report.ProfileBID, Catalogs: doraRefs})
	if err != nil {
		t.Fatal(err)
	}
	if r.Profile != report.ProfileBID || r.EvaluationID == "" || r.SnapshotID != "rev-1" || !r.Complete || r.CreatedBy != "user:"+w.ownerID ||
		r.FactsHash != report.Hash(r.Facts) || len(r.Files) != 2 || r.Files[0].Name != report.JSONFile || r.Files[1].Name != report.PDFFile {
		t.Fatalf("report = %+v", r)
	}
	var facts report.Facts
	if err := json.Unmarshal(r.Facts, &facts); err != nil {
		t.Fatal(err)
	}
	if facts.Report.ReportID != r.ID || facts.Report.EngineVersion != "dev" || facts.Report.GeneratedBy != "user:"+w.ownerID {
		t.Fatalf("meta = %+v", facts.Report)
	}
	found := false
	for _, c := range facts.Frameworks[0].Controls {
		if c.ControlID == monitored {
			found = c.Status == "ready" && c.Owner == "olga@example.com" && c.Approval != nil && c.Approval.By == "arno@example.com" && len(c.Evidence) == 1 &&
				c.Evidence[0].Location == "file" && c.Evidence[0].CheckMethod == ""
		}
	}
	if !found {
		t.Fatalf("approved control not stated as expected: %s", r.Facts)
	}
	got, err := w.eng.Report(w.owner, scopeA, r.ID)
	if err != nil || string(got.Facts) != string(r.Facts) || got.Inputs != nil {
		t.Fatalf("read back = %v inputs %s", err, got.Inputs)
	}
	pdf, err := w.eng.ReportFile(w.owner, scopeA, r.ID, report.PDFFile)
	if err != nil || !strings.HasPrefix(string(pdf.Data), "%PDF-") || pdf.SHA256 != r.Files[1].SHA256 {
		t.Fatalf("pdf = %v %s", err, pdf.SHA256)
	}
	list, err := w.eng.Reports(w.owner, scopeA)
	if err != nil || len(list) != 1 || list[0].Facts != nil {
		t.Fatalf("list = %+v %v", list, err)
	}
	actions := auditActions(t, w.eng)
	if countAction(actions, "report.generate") != 1 || countAction(actions, "report.download") != 2 {
		t.Fatalf("audit = %v", actions)
	}

	// From a stored evaluation: its snapshot and as_of are stated.
	ev := w.evaluate(t)
	r2, err := w.eng.GenerateReport(w.owner, scopeA, compliance.ReportRequest{Profile: report.ProfileBID, EvaluationID: ev.ID, Formats: []string{"json"}})
	if err != nil || r2.EvaluationID != ev.ID || !r2.AsOf.Equal(ev.Effective.AsOf) || len(r2.Files) != 1 {
		t.Fatalf("from evaluation = %+v %v", r2, err)
	}
	for _, tc := range []struct {
		req  compliance.ReportRequest
		want error
	}{
		{compliance.ReportRequest{Profile: "profile_z"}, compliance.ErrUnknownProfile},
		{compliance.ReportRequest{Profile: report.ProfileBID, Formats: []string{"xbrl-csv"}}, compliance.ErrInvalidReportRequest},
		{compliance.ReportRequest{Profile: report.ProfileBID, EvaluationID: ev.ID, SnapshotID: "rev-1"}, compliance.ErrInvalidReportRequest},
		{compliance.ReportRequest{Profile: report.ProfileBID, EvaluationID: "eval-missing"}, store.ErrNotFound},
	} {
		if _, err := w.eng.GenerateReport(w.owner, scopeA, tc.req); !errors.Is(err, tc.want) {
			t.Errorf("%+v: err = %v, want %v", tc.req, err, tc.want)
		}
	}
}

func TestRegenerateIdentical(t *testing.T) {
	w := newWorkflowEnv(t)
	w.addEvidence(t, "register.pdf", []byte("register"), monitored)
	w.approve(t, monitored)
	r, err := w.eng.GenerateReport(w.owner, scopeA, compliance.ReportRequest{Profile: report.ProfileBID})
	if err != nil {
		t.Fatal(err)
	}
	// Time passes and the workspace changes: the report still reproduces.
	*w.now = w.now.Add(400 * 24 * time.Hour)
	b := sampleBatch(t)
	b.Entities["ict_provider"][0]["legal_name"] = "Renamed Provider Ltd"
	if _, err := w.eng.Ingest(ctx, scopeA, b); err != nil {
		t.Fatal(err)
	}
	if _, err := w.eng.Act(w.owner, scopeA, "dora", monitored, workflow.ActionReopen, compliance.ActInput{Reason: "re-review"}); err != nil {
		t.Fatal(err)
	}
	res, err := w.eng.RegenerateReport(w.owner, scopeA, r.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Identical || res.Mismatch != "" || res.RegeneratedFactsHash != r.FactsHash || len(res.Files) != 2 || !res.Files[0].Identical || !res.Files[1].Identical {
		t.Fatalf("regeneration = %+v", res)
	}
	if countAction(auditActions(t, w.eng), "report.regenerate") != 1 {
		t.Fatal("regeneration must be audited")
	}
}

// flaky is a report profile that is not a pure function of its input.
type flaky struct {
	id      string
	feature extension.Feature
	calls   *atomic.Int64
	output  func(n int64) extension.ReportOutput
}

func (f flaky) ID() string                 { return f.id }
func (f flaky) Feature() extension.Feature { return f.feature }
func (f flaky) Formats() []string          { return []string{"txt"} }
func (f flaky) Generate(context.Context, extension.ReportInput) (extension.ReportOutput, error) {
	return f.output(f.calls.Add(1)), nil
}

func counterProfile(id string, feature extension.Feature) flaky {
	return flaky{id: id, feature: feature, calls: &atomic.Int64{}, output: func(n int64) extension.ReportOutput {
		return extension.ReportOutput{Facts: json.RawMessage(`{"call":` + string(rune('0'+n%10)) + `}`), Complete: true}
	}}
}

func TestRegenerateDetectsTampering(t *testing.T) {
	w := newWorkflowEnv(t, func(c *compliance.Config) {
		c.Extensions.ReportProfiles = []extension.ReportProfile{counterProfile("counter", extension.FeatureReportProfileB)}
	})
	r, err := w.eng.GenerateReport(w.owner, scopeA, compliance.ReportRequest{Profile: report.ProfileBID})
	if err != nil {
		t.Fatal(err)
	}
	// A stored evaluation that no longer follows from its snapshot.
	stored, err := w.st.Evaluation(ctx, scopeA, r.EvaluationID)
	if err != nil {
		t.Fatal(err)
	}
	tampered := strings.Replace(string(stored.Result), `"status":"in_review"`, `"status":"monitoring"`, 1)
	if tampered == string(stored.Result) {
		t.Fatal("the fixture must have an in_review control to tamper with")
	}
	stored.Result = json.RawMessage(tampered)
	if err := w.st.SaveEvaluation(ctx, stored); err != nil {
		t.Fatal(err)
	}
	if res, err := w.eng.RegenerateReport(w.owner, scopeA, r.ID); err != nil || res.Identical || res.Mismatch != "evaluation" {
		t.Fatalf("tampered evaluation = %+v %v", res, err)
	}
	// A profile that does not reproduce its facts.
	c, err := w.eng.GenerateReport(w.owner, scopeA, compliance.ReportRequest{Profile: "counter"})
	if err != nil {
		t.Fatal(err)
	}
	if res, err := w.eng.RegenerateReport(w.owner, scopeA, c.ID); err != nil || res.Identical || res.Mismatch != "facts" {
		t.Fatalf("non-deterministic profile = %+v %v", res, err)
	}
}

// switchable allows a commercial feature until it is turned off.
type switchable struct{ on atomic.Bool }

func (s *switchable) Allowed(ctx context.Context, scope compliance.Scope, f extension.Feature) extension.Decision {
	if f == extension.FeatureReportProfileA && s.on.Load() {
		return extension.Decision{Allowed: true}
	}
	return extension.AllowOpen{}.Allowed(ctx, scope, f)
}

func TestReportEntitlement(t *testing.T) {
	ents := &switchable{}
	w := newWorkflowEnv(t, func(c *compliance.Config) {
		c.Entitlements = ents
		c.Extensions.ReportProfiles = []extension.ReportProfile{flaky{id: "profile_a", feature: extension.FeatureReportProfileA, calls: &atomic.Int64{},
			output: func(int64) extension.ReportOutput {
				return extension.ReportOutput{Facts: json.RawMessage(`{"register":true}`), Complete: true,
					Files: []extension.ReportFile{{Name: "register.txt", ContentType: "text/plain", Data: []byte("rows")}}}
			}}}
	})
	var ne *extension.NotEntitledError
	if _, err := w.eng.GenerateReport(w.owner, scopeA, compliance.ReportRequest{Profile: "profile_a"}); !errors.As(err, &ne) || ne.Feature != extension.FeatureReportProfileA {
		t.Fatalf("not entitled = %v", err)
	}
	ents.on.Store(true)
	r, err := w.eng.GenerateReport(w.owner, scopeA, compliance.ReportRequest{Profile: "profile_a", Formats: []string{"txt"}})
	if err != nil || len(r.Files) != 2 || r.Files[0].Name != report.JSONFile {
		t.Fatalf("entitled = %+v %v", r, err)
	}
	ents.on.Store(false)
	if _, err := w.eng.Report(w.owner, scopeA, r.ID); err != nil {
		t.Fatalf("reading must not be gated: %v", err)
	}
	if _, err := w.eng.ReportFile(w.owner, scopeA, r.ID, "register.txt"); err != nil {
		t.Fatalf("downloading must not be gated: %v", err)
	}
	if res, err := w.eng.RegenerateReport(w.owner, scopeA, r.ID); err != nil || !res.Identical {
		t.Fatalf("verifying must not be gated: %+v %v", res, err)
	}
	profiles, err := w.eng.ReportProfiles(ctx, scopeA)
	if err != nil || len(profiles) != 2 || profiles[0].ID != "profile_a" || profiles[0].Entitlement.Allowed ||
		profiles[1].ID != report.ProfileBID || !profiles[1].Entitlement.Allowed || profiles[1].Formats[0] != "json" {
		t.Fatalf("profiles = %+v %v", profiles, err)
	}
	if _, err := compliance.New(ctx, compliance.Config{Store: w.st, Extensions: compliance.Extensions{
		ReportProfiles: []extension.ReportProfile{counterProfile(report.ProfileBID, extension.FeatureReportProfileB)}}}); err == nil {
		t.Fatal("a duplicate profile ID must be refused")
	}
}

func TestReportBlocksIncompleteExport(t *testing.T) {
	blocking := []extension.Finding{{Severity: "error", Code: "mandatory_missing", Message: "B_05.01 c0050 is mandatory", Template: "B_05.01", Row: "1", Field: "c0050"}}
	w := newWorkflowEnv(t, func(c *compliance.Config) {
		c.Extensions.ReportProfiles = []extension.ReportProfile{flaky{id: "strict", feature: extension.FeatureReportProfileB, calls: &atomic.Int64{},
			output: func(int64) extension.ReportOutput {
				return extension.ReportOutput{Facts: json.RawMessage(`{}`), Complete: false, Blocking: blocking}
			}}}
	})
	var ie *compliance.ExportIncompleteError
	if _, err := w.eng.GenerateReport(w.owner, scopeA, compliance.ReportRequest{Profile: "strict"}); !errors.As(err, &ie) || len(ie.Findings) != 1 || ie.Findings[0].Field != "c0050" {
		t.Fatalf("incomplete export = %v", err)
	}
	if list, _ := w.eng.Reports(w.owner, scopeA); len(list) != 0 {
		t.Fatalf("a refused export was stored: %v", list)
	}
	if countAction(auditActions(t, w.eng), "report.generate") != 0 {
		t.Fatal("a refused export was audited as generated")
	}
}

func TestSampleWorkspaceReports(t *testing.T) {
	w := newWorkflowEnv(t)
	admin := principalCtx(scopeA, "admin")
	sample := true
	if _, err := w.eng.UpdateSettings(admin, scopeA, compliance.SettingsInput{Sample: &sample}); err != nil {
		t.Fatal(err)
	}
	r, err := w.eng.GenerateReport(w.owner, scopeA, compliance.ReportRequest{Profile: report.ProfileBID})
	if err != nil {
		t.Fatal(err)
	}
	var facts report.Facts
	if err := json.Unmarshal(r.Facts, &facts); err != nil || !r.Sample || !facts.Report.Sample {
		t.Fatalf("sample report = %v %v", r.Sample, err)
	}
}

// TestReportsContainNoSecrets generates a report with encryption at rest and
// scans it for evidence paths, notes, reasons and sensitive field values.
func TestReportsContainNoSecrets(t *testing.T) {
	kek, err := crypt.NewKEK([]byte(strings.Repeat("k", 32)))
	if err != nil {
		t.Fatal(err)
	}
	sens, err := crypt.LoadSensitivity("0.1.0")
	if err != nil {
		t.Fatal(err)
	}
	var sealedStore *sealed.Store
	w := newWorkflowEnv(t, func(c *compliance.Config) {
		ring := crypt.NewKeyring(kek, c.Store, nil)
		sealedStore = sealed.Wrap(c.Store, ring, sens)
		c.Store, c.Hasher = sealedStore, sealed.Hasher(ring)
	})
	ev, err := w.eng.AddEvidence(w.owner, scopeA, compliance.EvidenceInput{Title: "Provider register", Kind: "document", Source: "dms",
		URI: "https://dms.example.com/SECRET-PATH/register.pdf?v=SECRET-QUERY", Checksum: checksumOf([]byte("x")),
		Links: []store.ControlRef{{Catalog: "dora", ControlID: monitored}}})
	if err != nil {
		t.Fatal(err)
	}
	notes := "SECRET-NOTES"
	if _, err := w.eng.Assign(w.owner, scopeA, "dora", ruleFailed, compliance.AssignInput{Notes: &notes}); err != nil {
		t.Fatal(err)
	}
	if _, err := w.eng.RevokeEvidence(w.owner, scopeA, ev.ID, "SECRET-REASON"); err != nil {
		t.Fatal(err)
	}
	r, err := w.eng.GenerateReport(w.owner, scopeA, compliance.ReportRequest{Profile: report.ProfileBID})
	if err != nil {
		t.Fatal(err)
	}
	pdf, err := w.eng.ReportFile(w.owner, scopeA, r.ID, report.PDFFile)
	if err != nil {
		t.Fatal(err)
	}
	markers := []string{"SECRET-PATH", "SECRET-QUERY", "SECRET-NOTES", "SECRET-REASON", "48000"} // 48000: the sample's annual_cost
	for _, m := range markers {
		if strings.Contains(string(r.Facts), m) || strings.Contains(string(r.Inputs), m) {
			t.Errorf("report facts or inputs contain %q", m)
		}
	}
	if !strings.Contains(string(r.Facts), `"location": "https://dms.example.com"`) {
		t.Errorf("the redacted location is missing: %s", r.Facts)
	}
	// Report bodies are sealed below the decorator.
	raw, err := sealedStore.Inner().Report(ctx, scopeA, r.ID)
	if err != nil || !crypt.IsSealed(string(raw.Facts)) {
		t.Fatalf("stored facts are not sealed: %.30s %v", raw.Facts, err)
	}
	rawPDF, err := sealedStore.Inner().ReportFile(ctx, scopeA, r.ID, report.PDFFile)
	if err != nil || strings.HasPrefix(string(rawPDF.Data), "%PDF") || !strings.HasPrefix(string(pdf.Data), "%PDF") {
		t.Fatal("the stored PDF must be sealed and the read one decrypted")
	}
	if res, err := w.eng.RegenerateReport(w.owner, scopeA, r.ID); err != nil || !res.Identical {
		t.Fatalf("regeneration under encryption = %+v %v", res, err)
	}
}

func TestRetentionKeepsReportedEvaluations(t *testing.T) {
	w := newWorkflowEnv(t)
	admin := principalCtx(scopeA, "admin")
	days := 30
	if _, err := w.eng.UpdateSettings(admin, scopeA, compliance.SettingsInput{Retention: &store.RetentionPolicy{EvaluationDays: days, KeepRevisions: 1}}); err != nil {
		t.Fatal(err)
	}
	r, err := w.eng.GenerateReport(w.owner, scopeA, compliance.ReportRequest{Profile: report.ProfileBID})
	if err != nil {
		t.Fatal(err)
	}
	*w.now = w.now.Add(20 * 24 * time.Hour)
	other := w.evaluate(t) // not reported
	*w.now = w.now.Add(25 * 24 * time.Hour)
	// The report and its evaluation are 45 days old, past the 30-day policy: both go.
	rep, err := w.eng.RunRetention(admin, scopeA, false)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Deleted.Reports != 1 || len(rep.Plan.DeleteEvaluations) != 1 || rep.Plan.DeleteEvaluations[0] != r.EvaluationID {
		t.Fatalf("retention = %+v", rep)
	}
	if _, err := w.eng.Evaluation(ctx, scopeA, other.ID); err != nil {
		t.Fatalf("the young evaluation must stay: %v", err)
	}

	// A young report keeps an old evaluation.
	r2, err := w.eng.GenerateReport(w.owner, scopeA, compliance.ReportRequest{Profile: report.ProfileBID, EvaluationID: other.ID})
	if err != nil {
		t.Fatal(err)
	}
	*w.now = w.now.Add(20 * 24 * time.Hour) // other is now 45 days old, r2 20
	rep, err = w.eng.RunRetention(admin, scopeA, false)
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range rep.Plan.DeleteEvaluations {
		if id == other.ID {
			t.Fatal("the evaluation of a kept report was deleted")
		}
	}
	if res, err := w.eng.RegenerateReport(w.owner, scopeA, r2.ID); err != nil || !res.Identical {
		t.Fatalf("a kept report must stay reproducible: %+v %v", res, err)
	}
}
