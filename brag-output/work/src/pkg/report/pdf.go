// SPDX-License-Identifier: Apache-2.0

package report

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/nexops-one/compliance-engine/pkg/engine"
	"github.com/nexops-one/compliance-engine/pkg/report/pdfdoc"
)

// Section headings of the Profile B PDF, in order.
const (
	SectionScope        = "1. Scope and inputs"
	SectionScores       = "2. Score and coverage"
	SectionControls     = "3. Controls"
	SectionDetails      = "4. Control details"
	SectionCompleteness = "5. Data completeness"
	SectionLimitations  = "6. Limitations"
	SectionDisclaimer   = "7. Disclaimer"
)

// ControlTableColumns are the headers of the control table.
var ControlTableColumns = []string{"Control", "Status", "Attention", "Stage", "Owner", "Approved until", "Due"}

// None stands for an empty table cell.
const None = "-"

func day(t *time.Time) string {
	if t == nil || t.IsZero() {
		return None
	}
	return t.UTC().Format("2006-01-02")
}

func stamp(t time.Time) string { return t.UTC().Format("2006-01-02 15:04:05 UTC") }

func orNone(s string) string {
	if s == "" {
		return None
	}
	return s
}

// ScoreLine states a tally in the form the PDF prints and its test parses.
func ScoreLine(name string, t engine.Tally) string {
	score := "score not defined"
	if t.ScoreDefined {
		score = fmt.Sprintf("score %d%% (%d/%d)", t.ScorePct, t.ScoreNumerator, t.Assessable)
	}
	return fmt.Sprintf("%s: %s, coverage %d%%, %d in scope, %d not assessed", name, score, t.CoveragePct, t.InScope, t.NotAssessed)
}

// ControlRow is a control's row in the control table.
func ControlRow(c ControlFacts) []string {
	due := day(c.DueAt)
	if c.Overdue {
		due += " overdue"
	}
	until := None
	if c.Approval != nil {
		until = day(&c.Approval.ExpiresAt)
	}
	return []string{c.ControlID, string(c.Status), orNone(c.Attention), string(c.Stage), orNone(c.Owner), until, due}
}

// RenderPDF renders Profile B facts. It reads nothing but the facts.
func RenderPDF(factsJSON []byte) ([]byte, error) {
	var f Facts
	if err := json.Unmarshal(factsJSON, &f); err != nil {
		return nil, fmt.Errorf("decode facts: %w", err)
	}
	m := f.Report
	opts := pdfdoc.Options{
		Title:    f.Title,
		Subtitle: fmt.Sprintf("Tenant %s, workspace %s, snapshot %s, as of %s", m.Scope.TenantID, m.Scope.WorkspaceID, m.SnapshotID, stamp(m.AsOf)),
		Footer:   fmt.Sprintf("Report %s, facts %s", m.ReportID, Hash(factsJSON)),
		Notice:   "Not legal advice, not certification. See section 7.",
		Date:     m.GeneratedAt,
	}
	if m.Sample {
		opts.Watermark = SampleWatermark
	}
	d := pdfdoc.New(opts)
	c := pdfdoc.Clean

	d.Heading(SectionScope)
	if m.Sample {
		d.KV("Sample data", "Fictitious organization. Results are a demonstration, not a compliance status.")
	}
	d.KV("Report", m.ReportID+" ("+m.Profile+")")
	d.KV("Scope", "tenant "+m.Scope.TenantID+", workspace "+m.Scope.WorkspaceID)
	d.KV("Snapshot", m.SnapshotID)
	d.KV("Evaluation", m.EvaluationID)
	d.KV("As of", stamp(m.AsOf))
	d.KV("Generated", stamp(m.GeneratedAt)+" by "+c(m.GeneratedBy)+" ("+string(m.GeneratorKind)+")")
	d.KV("Engine", "compliance-engine "+m.EngineVersion+", canonical schema "+m.SchemaVersion)
	for _, cat := range f.Catalogs {
		d.KV("Catalog", c(fmt.Sprintf("%s@%s: %s (%s, effective %s); source: %s", cat.Catalog, cat.Version, cat.Framework,
			cat.Jurisdiction, cat.EffectiveDate, cat.SourceAuthority)))
	}

	d.Heading(SectionScores)
	d.Line(ScoreLine("Overall", f.Overall))
	for _, fw := range f.Frameworks {
		d.Line(ScoreLine(fmt.Sprintf("%s (%s@%s)", fw.Framework, fw.Catalog.Catalog, fw.Catalog.Version), fw.Tally))
	}
	d.Space(1)
	d.Para("The score counts controls ready (and, where the catalog says so, monitoring) among assessable controls. " +
		"Coverage is the share of in-scope controls that could be assessed. The two are always stated together.")

	d.Heading(SectionControls)
	cols := make([]pdfdoc.Column, len(ControlTableColumns))
	weights := []float64{30, 11, 14, 10, 21, 11, 13}
	for i, h := range ControlTableColumns {
		cols[i] = pdfdoc.Column{Header: h, Weight: weights[i]}
	}
	for _, fw := range f.Frameworks {
		d.Subheading(fmt.Sprintf("%s (%s@%s)", fw.Framework, fw.Catalog.Catalog, fw.Catalog.Version))
		rows := [][]string{}
		for _, ctl := range fw.Controls {
			row := ControlRow(ctl)
			for i := range row {
				row[i] = c(row[i])
			}
			rows = append(rows, row)
		}
		d.Table(cols, rows)
	}

	d.Heading(SectionDetails)
	for _, fw := range f.Frameworks {
		for _, ctl := range fw.Controls {
			d.Subheading(c(ctl.ControlID + ": " + ctl.Title))
			d.Para(c(ctl.Description))
			status := string(ctl.Status)
			if ctl.Attention != "" {
				status += " (" + ctl.Attention + ")"
			}
			d.KV("Status", fmt.Sprintf("%s; computed %s; stage %s", status, ctl.ComputedStatus, ctl.Stage))
			if ctl.Approval != nil {
				self := ""
				if ctl.Approval.SelfApproved {
					self = "; self-approved"
				}
				d.KV("Approval", c(fmt.Sprintf("%s on %s, until %s, evaluation %s%s", ctl.Approval.By, day(&ctl.Approval.At),
					day(&ctl.Approval.ExpiresAt), ctl.Approval.EvaluationID, self)))
			}
			if ctl.VoidReason != "" {
				d.KV("Approval voided", ctl.VoidReason)
			}
			d.KV("Source", c(orNone(ctl.SourceAuthority)))
			d.KV("Rule", c(ruleText(ctl)))
			d.KV("Records examined", fmt.Sprintf("%d", ctl.RecordsExamined))
			if len(ctl.Failing) > 0 {
				d.KV("Failing records", c(strings.Join(ctl.Failing, ", ")))
			}
			for _, b := range ctl.Blockers {
				d.KV("Blocker", c(strings.Join(nonEmpty(string(b.Reason), b.Entity, b.Key, b.Field), " ")))
			}
			req := "not required for approval"
			if ctl.RequiresEvidence {
				req = "required for approval"
			}
			if len(ctl.EvidenceRequirements) > 0 {
				req += "; expected: " + strings.Join(ctl.EvidenceRequirements, "; ")
			}
			d.KV("Evidence", c(req))
			if len(ctl.Evidence) == 0 {
				d.KV("Linked evidence", "none")
			}
			for _, e := range ctl.Evidence {
				check, checksum := string(e.Integrity), e.Checksum
				if checksum == "" {
					check, checksum = "unchecked", "checksum missing"
				}
				if e.CheckMethod != "" {
					check += " by " + e.CheckMethod
				}
				d.KV("Linked evidence", c(fmt.Sprintf("%s: %s (%s, %s); %s; %s; %s; collected %s, valid until %s",
					e.ID, e.Title, e.Kind, e.Source, e.Location, checksum, string(e.State)+", "+check, day(&e.CollectedAt), day(e.ValidUntil))))
			}
		}
	}

	d.Heading(SectionCompleteness)
	cp := f.Completeness
	d.Line(fmt.Sprintf("Canonical schema %s, %d records.", cp.SchemaVersion, cp.Records))
	entRows := [][]string{}
	for _, e := range cp.Entities {
		entRows = append(entRows, []string{e.Entity, fmt.Sprint(e.Records), fmt.Sprint(e.Missing), fmt.Sprint(e.Derived)})
	}
	d.Table([]pdfdoc.Column{{Header: "Entity", Weight: 40}, {Header: "Records", Weight: 20}, {Header: "Missing", Weight: 20}, {Header: "Derived", Weight: 20}}, entRows)
	d.Space(2)
	d.Line(fmt.Sprintf("Export-required gaps: %d with a supplying adapter, %d not supplied by any adapter.", cp.GapsSupplied, cp.GapsNotSupplied))
	if len(cp.Gaps) > 0 {
		gapRows := [][]string{}
		for _, g := range cp.Gaps {
			supplied := "yes"
			if !g.Supplied {
				supplied = "no adapter"
			}
			gapRows = append(gapRows, []string{g.Entity + "." + g.Field, orNone(g.RoIRef), fmt.Sprint(g.Missing), fmt.Sprint(g.Derived), supplied})
		}
		d.Table([]pdfdoc.Column{{Header: "Field", Weight: 45}, {Header: "RoI ref", Weight: 15}, {Header: "Missing", Weight: 12},
			{Header: "Derived", Weight: 12}, {Header: "Supplied", Weight: 16}}, gapRows)
		d.Space(2)
	}
	d.Line(fmt.Sprintf("Invalid coded values: %d.", cp.InvalidCodes))
	if len(cp.InvalidCodeFields) > 0 {
		d.KV("In fields", strings.Join(cp.InvalidCodeFields, ", "))
	}
	if len(cp.UnverifiedCodelists) > 0 {
		d.KV("Unverified codelists", strings.Join(cp.UnverifiedCodelists, ", "))
	}
	d.Line(fmt.Sprintf("Dangling references: %d. Reference cycles: %d.", cp.DanglingReferences, cp.ReferenceCycles))

	d.Heading(SectionLimitations)
	for _, l := range f.Limitations {
		d.Bullet(c(l.Text))
	}

	d.Heading(SectionDisclaimer)
	d.Para(c(f.Disclaimer))
	return d.Bytes()
}

func nonEmpty(parts ...string) []string {
	out := []string{}
	for _, p := range parts {
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

func ruleText(c ControlFacts) string {
	r := c.Rule
	s := string(r.Kind)
	if r.Entity != "" {
		s += " on " + r.Entity
	}
	if r.Min > 0 {
		s += fmt.Sprintf(", at least %d", r.Min)
	}
	if len(r.Fields) > 0 {
		s += ", fields " + strings.Join(r.Fields, ", ")
	}
	if r.Field != "" {
		s += ", field " + r.Field
	}
	if r.Value != nil {
		s += fmt.Sprintf(" = %v", r.Value)
	}
	if len(r.Values) > 0 {
		s += fmt.Sprintf(" in %v", r.Values)
	}
	for _, cond := range r.Filter {
		if cond.Equals != nil {
			s += fmt.Sprintf("; where %s = %v", cond.Field, cond.Equals)
		} else {
			s += fmt.Sprintf("; where %s in %v", cond.Field, cond.In)
		}
	}
	if len(c.Inputs) > 0 {
		s += "; inputs " + strings.Join(c.Inputs, ", ")
	}
	return s
}
