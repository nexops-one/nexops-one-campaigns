// SPDX-License-Identifier: Apache-2.0

package report_test

import (
	"bytes"
	"compress/zlib"
	"encoding/json"
	"io"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"unicode/utf16"

	"github.com/nexops-one/compliance-engine/pkg/engine"
	"github.com/nexops-one/compliance-engine/pkg/extension"
	"github.com/nexops-one/compliance-engine/pkg/report"
	"github.com/nexops-one/compliance-engine/pkg/store"
	"github.com/nexops-one/compliance-engine/pkg/workflow"
)

// fpdf writes "stream\n<data>\nendstream"; the data may itself end in "\r".
var streamRe = regexp.MustCompile(`(?s)stream\n(.*?)\nendstream`)

// pdfPages returns the text strings shown on each page, in drawing order. It
// understands what fpdf writes: Flate content streams with UTF-16BE literal
// strings shown by Tj.
func pdfPages(t *testing.T, pdf []byte) [][]string {
	t.Helper()
	var pages [][]string
	for _, m := range streamRe.FindAllSubmatch(pdf, -1) {
		zr, err := zlib.NewReader(bytes.NewReader(m[1]))
		if err != nil {
			continue
		}
		data, err := io.ReadAll(zr)
		if err != nil || !bytes.Contains(data, []byte("Tj")) {
			continue
		}
		pages = append(pages, showStrings(data))
	}
	if len(pages) == 0 {
		t.Fatal("no page content found")
	}
	return pages
}

func pdfText(t *testing.T, pdf []byte) []string {
	var out []string
	for _, p := range pdfPages(t, pdf) {
		out = append(out, p...)
	}
	return out
}

func showStrings(content []byte) []string {
	var out []string
	for i := 0; i < len(content); i++ {
		if content[i] != '(' {
			continue
		}
		var raw []byte
		depth, j := 1, i+1
		for ; j < len(content) && depth > 0; j++ {
			c := content[j]
			switch {
			case c == '\\' && j+1 < len(content):
				j++
				switch e := content[j]; e {
				case 'n':
					raw = append(raw, '\n')
				case 'r':
					raw = append(raw, '\r')
				case 't':
					raw = append(raw, '\t')
				case 'b':
					raw = append(raw, '\b')
				case 'f':
					raw = append(raw, '\f')
				default:
					if e >= '0' && e <= '7' {
						k := j
						for k < len(content) && k < j+3 && content[k] >= '0' && content[k] <= '7' {
							k++
						}
						n, _ := strconv.ParseUint(string(content[j:k]), 8, 8)
						raw = append(raw, byte(n))
						j = k - 1
					} else {
						raw = append(raw, e)
					}
				}
			case c == '(':
				depth++
				raw = append(raw, c)
			case c == ')':
				depth--
				if depth > 0 {
					raw = append(raw, c)
				}
			default:
				raw = append(raw, c)
			}
		}
		rest := bytes.TrimLeft(content[j:], " \r\n")
		if bytes.HasPrefix(rest, []byte("Tj")) {
			u := make([]uint16, len(raw)/2)
			for k := range u {
				u[k] = uint16(raw[2*k])<<8 | uint16(raw[2*k+1])
			}
			out = append(out, string(utf16.Decode(u)))
		}
		i = j - 1
	}
	return out
}

var scoreRe = regexp.MustCompile(`^(.+): (?:score (\d+)% \((\d+)/(\d+)\)|score not defined), coverage (\d+)%, (\d+) in scope, (\d+) not assessed$`)

type parsedScore struct {
	defined                             bool
	pct, num, assessable, cov, in, notA int
}

func atoi(s string) int { n, _ := strconv.Atoi(s); return n }

func parseScores(lines []string) map[string]parsedScore {
	out := map[string]parsedScore{}
	for _, l := range lines {
		m := scoreRe.FindStringSubmatch(l)
		if m == nil {
			continue
		}
		out[m[1]] = parsedScore{defined: m[2] != "", pct: atoi(m[2]), num: atoi(m[3]), assessable: atoi(m[4]), cov: atoi(m[5]), in: atoi(m[6]), notA: atoi(m[7])}
	}
	return out
}

func scoreOf(t engine.Tally) parsedScore {
	p := parsedScore{defined: t.ScoreDefined, cov: t.CoveragePct, in: t.InScope, notA: t.NotAssessed}
	if t.ScoreDefined {
		p.pct, p.num, p.assessable = t.ScorePct, t.ScoreNumerator, t.Assessable
	}
	return p
}

func index(lines []string, s string) int {
	for i, l := range lines {
		if l == s {
			return i
		}
	}
	return -1
}

// TestPDFFactsMatchJSON parses the scores and the control table back out of
// the PDF text and compares them with the JSON facts.
func TestPDFFactsMatchJSON(t *testing.T) {
	out := generate(t, newFixture(t).input(t, false))
	var facts report.Facts
	if err := json.Unmarshal(out.Facts, &facts); err != nil {
		t.Fatal(err)
	}
	lines := pdfText(t, file(t, out, report.PDFFile))

	scores := parseScores(lines)
	if got, ok := scores["Overall"]; !ok || got != scoreOf(facts.Overall) {
		t.Errorf("overall score in PDF = %+v, JSON %+v", got, scoreOf(facts.Overall))
	}
	for _, fw := range facts.Frameworks {
		name := fw.Framework + " (" + fw.Catalog.Catalog + "@" + fw.Catalog.Version + ")"
		if got, ok := scores[name]; !ok || got != scoreOf(fw.Tally) {
			t.Errorf("%s score in PDF = %+v (found %v), JSON %+v", name, got, ok, scoreOf(fw.Tally))
		}
	}

	start, end := index(lines, report.SectionControls), index(lines, report.SectionDetails)
	if start < 0 || end < start {
		t.Fatalf("sections not found: %d %d", start, end)
	}
	table := lines[start:end]
	for _, fw := range facts.Frameworks {
		for _, c := range fw.Controls {
			i := index(table, c.ControlID)
			if i < 0 || i+len(report.ControlTableColumns) > len(table) {
				t.Errorf("control %s not in the PDF table", c.ControlID)
				continue
			}
			row := table[i : i+len(report.ControlTableColumns)]
			want := []string{c.ControlID, string(c.Status), c.Attention, string(c.Stage), c.Owner, "", ""}
			for k, v := range want {
				if v == "" {
					want[k] = report.None
				}
			}
			if c.Approval != nil {
				want[5] = c.Approval.ExpiresAt.UTC().Format("2006-01-02")
			}
			if c.DueAt != nil {
				want[6] = c.DueAt.UTC().Format("2006-01-02")
			}
			if c.Overdue {
				want[6] += " overdue"
			}
			if strings.Join(row, "|") != strings.Join(want, "|") {
				t.Errorf("PDF row %v, JSON facts %v", row, want)
			}
		}
	}
	joined := strings.Join(lines, " ")
	for _, fw := range facts.Frameworks {
		for _, c := range fw.Controls {
			if !strings.Contains(joined, c.ControlID+": "+c.Title) {
				t.Errorf("details of %s missing", c.ControlID)
			}
		}
	}
	for _, s := range []string{report.SectionScope, report.SectionScores, report.SectionCompleteness, report.SectionLimitations, report.SectionDisclaimer} {
		if index(lines, s) < 0 {
			t.Errorf("section %q missing", s)
		}
	}
	for _, l := range facts.Limitations {
		if !strings.Contains(joined, l.Text) {
			t.Errorf("limitation %q missing from the PDF", l.Code)
		}
	}
}

func TestPDFSampleWatermarkOnEveryPage(t *testing.T) {
	f := newFixture(t)
	pages := pdfPages(t, file(t, generate(t, f.input(t, true)), report.PDFFile))
	if len(pages) < 2 {
		t.Fatalf("expected a multi-page report, got %d page(s)", len(pages))
	}
	for i, p := range pages {
		if n := strings.Count(strings.Join(p, "\n"), report.SampleWatermark); n != 1 {
			t.Errorf("page %d carries the watermark %d times", i+1, n)
		}
	}
	for i, p := range pdfPages(t, file(t, generate(t, f.input(t, false)), report.PDFFile)) {
		if strings.Contains(strings.Join(p, "\n"), report.SampleWatermark) {
			t.Errorf("page %d of a non-sample report carries the watermark", i+1)
		}
	}
}

func TestPDFIsSelfContained(t *testing.T) {
	pdf := file(t, generate(t, newFixture(t).input(t, false)), report.PDFFile)
	if !bytes.HasPrefix(pdf, []byte("%PDF-")) {
		t.Fatal("not a PDF")
	}
	descriptors, embedded := bytes.Count(pdf, []byte("/Type /FontDescriptor")), bytes.Count(pdf, []byte("/FontFile2"))
	if descriptors == 0 || descriptors != embedded {
		t.Errorf("font descriptors %d, embedded font files %d", descriptors, embedded)
	}
	for _, s := range []string{"/URI", "/Launch", "/GoToR", "/JavaScript", "/Helvetica", "/Times", "/Courier"} {
		if bytes.Contains(pdf, []byte(s)) {
			t.Errorf("PDF refers to %s", s)
		}
	}
}

func TestEvidenceWithoutChecksumIsStated(t *testing.T) {
	in := newFixture(t).input(t, false)
	in.Evidence = append(in.Evidence, extension.ReportEvidence{ID: "evd-m8", Title: "Carried-over policy", Kind: "document", Source: "M8",
		Location: "urn", Integrity: store.IntegrityNoChecksum, State: store.EvidenceActive, CollectedAt: in.Meta.AsOf,
		Links: []store.ControlRef{{Catalog: "dora", ControlID: "dora-roi-provider-identification"}}})
	for i, c := range in.Effective.Frameworks[0].Controls {
		if c.ControlID == "dora-roi-provider-identification" {
			in.Effective.Frameworks[0].Controls[i].Evidence = append(c.Evidence[:len(c.Evidence):len(c.Evidence)],
				workflow.EvidenceRef{ID: "evd-m8", State: store.EvidenceActive, Integrity: store.IntegrityNoChecksum})
		}
	}
	out := generate(t, in)
	var facts report.Facts
	if err := json.Unmarshal(out.Facts, &facts); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, l := range facts.Limitations {
		if l.Code == "evidence_no_checksum" && strings.Contains(l.Text, "1 linked evidence item(s)") {
			found = true
		}
	}
	if !found {
		t.Fatalf("limitations = %+v", facts.Limitations)
	}
	if text := strings.Join(pdfText(t, file(t, out, report.PDFFile)), " "); !strings.Contains(text, "checksum missing") {
		t.Fatal("the PDF must say the checksum is missing")
	}
}
