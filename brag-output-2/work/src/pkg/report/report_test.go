// SPDX-License-Identifier: Apache-2.0

package report_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nexops-one/compliance-engine/pkg/crypt"
	"github.com/nexops-one/compliance-engine/pkg/report"
	"github.com/nexops-one/compliance-engine/sdk/schema"
)

func TestRedact(t *testing.T) {
	for in, want := range map[string]string{
		"https://dms.example.com/a/b.pdf?v=1#x":    "https://dms.example.com",
		"HTTPS://DMS.Example.com:8443/path":        "https://dms.example.com:8443",
		"https://user:pw@dms.example.com/p":        "https://dms.example.com",
		"s3://evidence-bucket/2026/exit-plan.pdf":  "s3://evidence-bucket",
		"file:///srv/evidence/exit-plan.pdf":       "file",
		"urn:example:evidence:42":                  "urn",
		"managed:sha256:" + strings.Repeat("a", 8): "managed",
		"not a uri": "reference",
		"":          "reference",
	} {
		if got := report.Redact(in); got != want {
			t.Errorf("Redact(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestProfileBFactsGolden(t *testing.T) {
	out := generate(t, newFixture(t).input(t, false))
	if !out.Complete || len(out.Blocking) != 0 {
		t.Fatalf("complete = %v blocking = %v", out.Complete, out.Blocking)
	}
	if !bytes.Equal(file(t, out, report.JSONFile), out.Facts) {
		t.Fatal("report.json must be the facts")
	}
	golden := filepath.Join("testdata", "profile_b.golden.json")
	if *update {
		if err := os.MkdirAll("testdata", 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(golden, out.Facts, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(golden)
	if err != nil {
		t.Fatalf("%v (run go test ./pkg/report -update)", err)
	}
	if !bytes.Equal(bytes.ReplaceAll(want, []byte("\r\n"), []byte("\n")), out.Facts) {
		t.Fatalf("facts differ from %s (run with -update after checking the change):\n%s", golden, out.Facts)
	}
}

func TestProfileBFactsContent(t *testing.T) {
	f := newFixture(t)
	var facts report.Facts
	if err := json.Unmarshal(generate(t, f.input(t, false)).Facts, &facts); err != nil {
		t.Fatal(err)
	}
	if facts.Report.ReportID != "rpt-1" || facts.Report.EvaluationID != f.eval.ID || facts.Disclaimer != report.Disclaimer ||
		len(facts.Catalogs) != 1 || facts.Catalogs[0].Framework == "" || facts.Report.Sample {
		t.Fatalf("header = %+v %+v", facts.Report, facts.Catalogs)
	}
	if facts.Overall != f.eval.Effective.Overall || facts.Frameworks[0].Tally != f.eval.Effective.Frameworks[0].Tally {
		t.Fatal("scores must be the effective evaluation's tallies")
	}
	byID := map[string]report.ControlFacts{}
	for _, c := range facts.Frameworks[0].Controls {
		byID[c.ControlID] = c
	}
	ok := byID["dora-roi-provider-identification"]
	if ok.Status != "ready" || ok.Owner != "olga@example.com" || ok.Approval == nil || ok.Approval.By != "arno@example.com" ||
		ok.DueAt == nil || len(ok.Evidence) != 1 || ok.RecordsExamined == 0 || ok.Description == "" {
		t.Fatalf("approved control = %+v", ok)
	}
	if e := ok.Evidence[0]; e.Location != "https://dms.example.com:8443" || e.State != "active" || e.Checksum == "" || e.Source != "GRC" {
		t.Fatalf("evidence = %+v", e)
	}
	rej := byID["dora-roi-reporting-entity"]
	if rej.Status != "rejected" || rej.Approval != nil || len(rej.Evidence) != 1 || rej.Evidence[0].State != "revoked" || rej.Evidence[0].Location != "urn" {
		t.Fatalf("rejected control = %+v", rej)
	}
	codes := map[string]bool{}
	for _, l := range facts.Limitations {
		codes[l.Code] = true
	}
	for _, c := range []string{"indicative_content", "evidence_referenced", "evidence_unverified", "point_in_time"} {
		if !codes[c] {
			t.Errorf("limitation %s missing from %v", c, facts.Limitations)
		}
	}
	if facts.Overall.NotAssessed > 0 != codes["controls_not_assessed"] {
		t.Errorf("controls_not_assessed limitation = %v with %d not assessed", codes["controls_not_assessed"], facts.Overall.NotAssessed)
	}
	if facts.Completeness.Records != len(f.input(t, false).Records) || len(facts.Completeness.Entities) == 0 {
		t.Fatalf("completeness = %+v", facts.Completeness)
	}

	sample := generate(t, f.input(t, true))
	var sf report.Facts
	if err := json.Unmarshal(sample.Facts, &sf); err != nil || !sf.Report.Sample {
		t.Fatalf("sample facts = %v %v", sf.Report, err)
	}
}

func TestProfileBDeterministic(t *testing.T) {
	f := newFixture(t)
	a, b := generate(t, f.input(t, true)), generate(t, f.input(t, true))
	if !bytes.Equal(a.Facts, b.Facts) || len(a.Files) != 2 || len(b.Files) != 2 {
		t.Fatal("facts differ between identical runs")
	}
	for i := range a.Files {
		if a.Files[i].Name != b.Files[i].Name || !bytes.Equal(a.Files[i].Data, b.Files[i].Data) {
			t.Fatalf("file %s differs between identical runs", a.Files[i].Name)
		}
	}
	in := f.input(t, false)
	in.Formats = []string{"json"}
	if out := generate(t, in); len(out.Files) != 1 || out.Files[0].Name != report.JSONFile {
		t.Fatalf("formats json = %d files", len(out.Files))
	}
}

// TestProfileBContainsNoSecrets checks facts, PDF text and raw PDF bytes for
// evidence paths, queries, notes, reasons and sensitive field values.
func TestProfileBContainsNoSecrets(t *testing.T) {
	f := newFixture(t)
	in := f.input(t, false)
	out := generate(t, in)
	pdf := file(t, out, report.PDFFile)
	text := strings.Join(pdfText(t, pdf), "\n")
	markers := []string{secretPath, secretQuery, secretNote, secretReason, "register.pdf"}
	sens, err := crypt.LoadSensitivity(in.Schema.Version)
	if err != nil {
		t.Fatal(err)
	}
	var values []string
	for _, r := range in.Records {
		for field := range sens[r.Entity] {
			if v, ok := r.Data[field]; ok && v != nil {
				values = append(values, fmt.Sprint(v))
			}
		}
	}
	if len(values) < 5 {
		t.Fatalf("the fixture must carry sensitive values to look for, got %v", values)
	}
	bodies := map[string]string{"facts": string(out.Facts), "pdf text": text}
	for _, m := range append(markers, values...) {
		for name, body := range bodies {
			if strings.Contains(body, m) {
				t.Errorf("%s contains %q", name, m)
			}
		}
	}
	// Raw bytes as well, for the text markers (numbers occur in any PDF).
	for _, m := range append(markers, "SENSITIVE-") {
		if strings.Contains(string(pdf), m) {
			t.Errorf("PDF bytes contain %q", m)
		}
	}
}

// TestNoSensitiveFieldIsAKey: reports show record keys, so no sensitive field
// may be part of one.
func TestNoSensitiveFieldIsAKey(t *testing.T) {
	reg, err := schema.Default()
	if err != nil {
		t.Fatal(err)
	}
	for _, v := range reg.Versions() {
		s, err := reg.Resolve(v)
		if err != nil {
			t.Fatal(err)
		}
		sens, err := crypt.LoadSensitivity(v)
		if err != nil {
			t.Fatal(err)
		}
		for _, name := range s.EntityNames() {
			e, _ := s.Entity(name)
			for _, k := range e.IdentityKey {
				if sens[name][k] {
					t.Errorf("schema %s: %s.%s is sensitive and part of the key", v, name, k)
				}
			}
		}
	}
}
