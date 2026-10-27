// SPDX-License-Identifier: Apache-2.0

package report

import (
	"context"
	"fmt"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/nexops-one/compliance-engine/pkg/catalog"
	"github.com/nexops-one/compliance-engine/pkg/engine"
	"github.com/nexops-one/compliance-engine/pkg/extension"
	"github.com/nexops-one/compliance-engine/pkg/store"
	"github.com/nexops-one/compliance-engine/pkg/workflow"
)

// ProfileBID identifies the readiness and evidence report.
const ProfileBID = "profile_b"

// FormatPDF is Profile B's printable rendering.
const FormatPDF = "pdf"

// PDFFile is the name of Profile B's PDF rendering.
const PDFFile = "report.pdf"

// Facts are Profile B's report document. They carry no canonical field
// values, only entity names, record keys, field names and counts, so no
// sensitive field can reach a report.
type Facts struct {
	Title        string               `json:"title"`
	Report       extension.ReportMeta `json:"report"`
	Disclaimer   string               `json:"disclaimer"`
	Catalogs     []CatalogFacts       `json:"catalogs"`
	Overall      engine.Tally         `json:"overall"`
	Frameworks   []FrameworkFacts     `json:"frameworks"`
	Completeness CompletenessFacts    `json:"completeness"`
	Limitations  []Limitation         `json:"limitations"`
}

// CatalogFacts describes one catalog version the report evaluated.
type CatalogFacts struct {
	Catalog         string `json:"catalog"`
	Version         string `json:"version"`
	Framework       string `json:"framework"`
	Jurisdiction    string `json:"jurisdiction"`
	EffectiveDate   string `json:"effective_date"`
	SourceAuthority string `json:"source_authority"`
	Assumptions     string `json:"scoring_assumptions,omitempty"`
}

// FrameworkFacts are the score, coverage and controls of one catalog.
type FrameworkFacts struct {
	Catalog   catalog.Ref    `json:"catalog"`
	Framework string         `json:"framework"`
	Tally     engine.Tally   `json:"tally"`
	Controls  []ControlFacts `json:"controls"`
}

// ApprovalFacts is the current approval of a control.
type ApprovalFacts struct {
	By           string    `json:"by"`
	At           time.Time `json:"at"`
	ExpiresAt    time.Time `json:"expires_at"`
	EvaluationID string    `json:"evaluation_id"`
	SelfApproved bool      `json:"self_approved,omitempty"`
}

// EvidenceFacts is a linked evidence item, with its location redacted.
type EvidenceFacts struct {
	ID          string              `json:"id"`
	Title       string              `json:"title"`
	Kind        string              `json:"kind"`
	Source      string              `json:"source"`
	Location    string              `json:"location"`
	Checksum    string              `json:"checksum"`
	State       store.EvidenceState `json:"state"`
	Integrity   store.Integrity     `json:"integrity"`
	CheckMethod string              `json:"check_method,omitempty"`
	CollectedAt time.Time           `json:"collected_at"`
	ValidUntil  *time.Time          `json:"valid_until,omitempty"`
}

// ControlFacts is everything the report states about one control.
type ControlFacts struct {
	ControlID            string           `json:"control_id"`
	Title                string           `json:"title"`
	Description          string           `json:"description"`
	SourceAuthority      string           `json:"source_authority"`
	Status               engine.Status    `json:"status"`
	ComputedStatus       engine.Status    `json:"computed_status"`
	Attention            string           `json:"attention,omitempty"`
	Stage                store.Stage      `json:"stage"`
	Owner                string           `json:"owner,omitempty"`
	Reviewer             string           `json:"reviewer,omitempty"`
	DueAt                *time.Time       `json:"due_at,omitempty"`
	Overdue              bool             `json:"overdue"`
	Approval             *ApprovalFacts   `json:"approval,omitempty"`
	VoidReason           string           `json:"void_reason,omitempty"`
	RequiresEvidence     bool             `json:"requires_evidence"`
	EvidenceRequirements []string         `json:"evidence_requirements"`
	Rule                 catalog.Rule     `json:"rule"`
	Inputs               []string         `json:"inputs"`
	RecordsExamined      int              `json:"records_examined"`
	Failing              []string         `json:"failing"`
	Blockers             []engine.Blocker `json:"blockers"`
	Evidence             []EvidenceFacts  `json:"evidence"`
}

// GapFacts counts the gaps of one export-required field.
type GapFacts struct {
	Entity      string `json:"entity"`
	Field       string `json:"field"`
	RoIRef      string `json:"roi_ref,omitempty"`
	Missing     int    `json:"missing"`
	Derived     int    `json:"derived"`
	Conditional bool   `json:"conditional"`
	Supplied    bool   `json:"supplied"`
}

// EntityFacts summarizes one entity of the snapshot.
type EntityFacts struct {
	Entity  string `json:"entity"`
	Records int    `json:"records"`
	Missing int    `json:"missing"`
	Derived int    `json:"derived"`
}

// CompletenessFacts summarizes the data-completeness view without values.
type CompletenessFacts struct {
	SchemaVersion       string        `json:"schema_version"`
	Records             int           `json:"records"`
	Entities            []EntityFacts `json:"entities"`
	Gaps                []GapFacts    `json:"gaps"`
	GapsSupplied        int           `json:"gaps_supplied"`
	GapsNotSupplied     int           `json:"gaps_not_supplied_by_any_adapter"`
	InvalidCodes        int           `json:"invalid_codes"`
	InvalidCodeFields   []string      `json:"invalid_code_fields"`
	UnverifiedCodelists []string      `json:"unverified_codelists"`
	DanglingReferences  int           `json:"dangling_references"`
	ReferenceCycles     int           `json:"reference_cycles"`
}

// Limitation is one stated limit of what the report shows.
type Limitation struct {
	Code string `json:"code"`
	Text string `json:"text"`
}

type profileB struct{}

// ProfileB returns the readiness and evidence report profile.
func ProfileB() extension.ReportProfile { return profileB{} }

func (profileB) ID() string                 { return ProfileBID }
func (profileB) Feature() extension.Feature { return extension.FeatureReportProfileB }
func (profileB) Formats() []string          { return []string{FormatPDF} }
func (p profileB) Generate(_ context.Context, in extension.ReportInput) (extension.ReportOutput, error) {
	facts, err := Marshal(BuildFacts(in))
	if err != nil {
		return extension.ReportOutput{}, err
	}
	out := extension.ReportOutput{Facts: facts, Complete: true,
		Files: []extension.ReportFile{{Name: JSONFile, ContentType: "application/json", Data: facts}}}
	if len(in.Formats) == 0 || slices.Contains(in.Formats, FormatPDF) {
		pdf, err := RenderPDF(facts)
		if err != nil {
			return extension.ReportOutput{}, fmt.Errorf("profile_b: render PDF: %w", err)
		}
		out.Files = append(out.Files, extension.ReportFile{Name: PDFFile, ContentType: "application/pdf", Data: pdf})
	}
	return out, nil
}

func person(people map[string]string, actor string) string {
	if email, ok := people[actor]; ok {
		return email
	}
	return actor
}

// BuildFacts computes Profile B's facts. It is pure and deterministic.
func BuildFacts(in extension.ReportInput) Facts {
	f := Facts{Title: "Readiness and evidence report", Report: in.Meta, Disclaimer: Disclaimer,
		Catalogs: []CatalogFacts{}, Overall: in.Effective.Overall, Frameworks: []FrameworkFacts{}}
	if f.Report.Catalogs == nil {
		f.Report.Catalogs = []catalog.Ref{}
	}
	cats := map[catalog.Ref]*catalog.Catalog{}
	for _, c := range in.Catalogs {
		cats[c.Ref()] = c
		f.Catalogs = append(f.Catalogs, CatalogFacts{Catalog: c.Catalog, Version: c.Version, Framework: c.Framework,
			Jurisdiction: c.Jurisdiction, EffectiveDate: c.EffectiveDate, SourceAuthority: c.SourceAuthority, Assumptions: c.Scoring.Assumptions})
	}
	computed := map[catalog.Ref]map[string]engine.ControlResult{}
	for _, fr := range in.Computed.Frameworks {
		computed[fr.Catalog] = map[string]engine.ControlResult{}
		for _, cr := range fr.Controls {
			computed[fr.Catalog][cr.ControlID] = cr
		}
	}
	evidence := map[string]extension.ReportEvidence{}
	for _, e := range in.Evidence {
		evidence[e.ID] = e
	}
	for _, fw := range in.Effective.Frameworks {
		out := FrameworkFacts{Catalog: fw.Catalog, Framework: fw.Framework, Tally: fw.Tally, Controls: []ControlFacts{}}
		for _, c := range fw.Controls {
			out.Controls = append(out.Controls, controlFacts(in, cats[fw.Catalog], computed[fw.Catalog][c.ControlID], c, evidence))
		}
		f.Frameworks = append(f.Frameworks, out)
	}
	f.Completeness = completenessFacts(in)
	f.Limitations = limitations(in, f)
	return f
}

func controlFacts(in extension.ReportInput, cat *catalog.Catalog, cr engine.ControlResult, c workflow.Control, evidence map[string]extension.ReportEvidence) ControlFacts {
	out := ControlFacts{
		ControlID: c.ControlID, Title: c.Title, Status: c.Status, ComputedStatus: c.ComputedStatus, Attention: c.Attention,
		Stage: c.Stage, Owner: person(in.People, c.Owner), Reviewer: person(in.People, c.Reviewer), DueAt: c.DueAt, Overdue: c.Overdue,
		VoidReason: c.VoidReason, RequiresEvidence: c.RequiresEvidence, EvidenceRequirements: []string{},
		Rule: cr.Explanation.Rule, Inputs: append([]string{}, cr.Explanation.Inputs...), RecordsExamined: len(cr.Explanation.RecordsExamined),
		Failing: append([]string{}, cr.Explanation.Failing...), Blockers: append([]engine.Blocker{}, cr.Blockers...), Evidence: []EvidenceFacts{},
	}
	if cat != nil {
		for _, ctl := range cat.Controls {
			if ctl.ID == c.ControlID {
				out.Description, out.SourceAuthority = ctl.Description, ctl.SourceAuthority
				out.EvidenceRequirements = append(out.EvidenceRequirements, ctl.EvidenceRequirements...)
			}
		}
	}
	if a := c.Approval; a != nil && c.Stage == store.StageApproved {
		out.Approval = &ApprovalFacts{By: person(in.People, a.Actor), At: a.At, ExpiresAt: a.ExpiresAt, EvaluationID: a.EvaluationID, SelfApproved: a.SelfApproved}
	}
	for _, ref := range c.Evidence {
		e, ok := evidence[ref.ID]
		if !ok {
			continue
		}
		out.Evidence = append(out.Evidence, EvidenceFacts{ID: e.ID, Title: e.Title, Kind: e.Kind, Source: e.Source, Location: e.Location,
			Checksum: e.Checksum, State: ref.State, Integrity: ref.Integrity, CheckMethod: e.CheckMethod, CollectedAt: e.CollectedAt, ValidUntil: e.ValidUntil})
	}
	return out
}

func completenessFacts(in extension.ReportInput) CompletenessFacts {
	c := in.Completeness
	out := CompletenessFacts{SchemaVersion: c.SchemaVersion, Entities: []EntityFacts{}, Gaps: []GapFacts{}, InvalidCodeFields: []string{},
		UnverifiedCodelists: append([]string{}, c.UnverifiedCodelists...), InvalidCodes: len(c.InvalidCodes),
		DanglingReferences: len(c.References.Dangling), ReferenceCycles: len(c.References.Cycles)}
	sort.Strings(out.UnverifiedCodelists)
	for _, e := range c.Entities {
		out.Records += e.Records
		out.Entities = append(out.Entities, EntityFacts{Entity: e.Entity, Records: e.Records, Missing: e.Missing, Derived: e.Derived})
	}
	gaps := map[string]*GapFacts{}
	var order []string
	for _, g := range c.Gaps {
		k := g.Entity + "." + g.Field
		gf := gaps[k]
		if gf == nil {
			gf = &GapFacts{Entity: g.Entity, Field: g.Field, RoIRef: g.RoIRef, Conditional: g.Conditional, Supplied: g.Supplied}
			gaps[k] = gf
			order = append(order, k)
		}
		if g.State == "derived" {
			gf.Derived++
		} else {
			gf.Missing++
		}
		if g.Supplied {
			out.GapsSupplied++
		} else {
			out.GapsNotSupplied++
		}
	}
	sort.Strings(order)
	for _, k := range order {
		out.Gaps = append(out.Gaps, *gaps[k])
	}
	seen := map[string]bool{}
	for _, ci := range c.InvalidCodes {
		if k := ci.Entity + "." + ci.Field; !seen[k] {
			seen[k] = true
			out.InvalidCodeFields = append(out.InvalidCodeFields, k)
		}
	}
	sort.Strings(out.InvalidCodeFields)
	return out
}

func limitations(in extension.ReportInput, f Facts) []Limitation {
	out := []Limitation{{Code: "indicative_content",
		Text: "Control catalogs and their mapping to regulatory requirements are indicative pending legal review."}}
	t := f.Overall
	if t.NotAssessed > 0 {
		out = append(out, Limitation{Code: "controls_not_assessed", Text: fmt.Sprintf(
			"%d of %d controls could not be assessed from the data supplied; the score covers assessable controls only (coverage %d%%).",
			t.NotAssessed, t.InScope, t.CoveragePct)})
	}
	if !t.ScoreDefined {
		out = append(out, Limitation{Code: "score_undefined", Text: "No control could be assessed, so no score is stated."})
	}
	out = append(out, Limitation{Code: "evidence_referenced",
		Text: "Evidence is referenced, not inspected: the engine records locations and checksums and does not assess the content of evidence documents."})
	var unverified, attested, noChecksum int
	for _, e := range in.Evidence {
		switch {
		case e.Integrity == store.IntegrityNoChecksum:
			noChecksum++
		case e.Integrity == store.IntegrityUnverified:
			unverified++
		case e.Integrity == store.IntegrityVerified && e.CheckMethod == "attested":
			attested++
		}
	}
	if unverified > 0 {
		out = append(out, Limitation{Code: "evidence_unverified", Text: fmt.Sprintf(
			"%d linked evidence item(s) carry a checksum declared by the collector that has not been verified.", unverified)})
	}
	if noChecksum > 0 {
		out = append(out, Limitation{Code: "evidence_no_checksum", Text: fmt.Sprintf(
			"%d linked evidence item(s) were recorded without a checksum; they cannot show that the referenced document is unchanged and do not satisfy evidence requirements.", noChecksum)})
	}
	if attested > 0 {
		out = append(out, Limitation{Code: "evidence_attested", Text: fmt.Sprintf(
			"%d linked evidence item(s) were verified by a check the customer ran and attested, not by the engine.", attested)})
	}
	if len(f.Completeness.UnverifiedCodelists) > 0 {
		out = append(out, Limitation{Code: "codelists_unverified", Text: "Coded values were not checked against official lists for: " +
			strings.Join(f.Completeness.UnverifiedCodelists, ", ") + "."})
	}
	derived := 0
	for _, e := range f.Completeness.Entities {
		derived += e.Derived
	}
	if derived > 0 {
		out = append(out, Limitation{Code: "derived_values", Text: fmt.Sprintf(
			"%d export-required value(s) are derived by an adapter and not confirmed at the source.", derived)})
	}
	for _, c := range f.Catalogs {
		if c.Assumptions != "" {
			out = append(out, Limitation{Code: "scoring_assumptions", Text: c.Catalog + "@" + c.Version + ": " + c.Assumptions})
		}
	}
	out = append(out, Limitation{Code: "point_in_time", Text: "Statuses, approvals and evidence validity are stated as of " +
		f.Report.AsOf.UTC().Format(time.RFC3339) + " and change over time."})
	return out
}
