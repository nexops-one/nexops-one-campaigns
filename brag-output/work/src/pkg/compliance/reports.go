// SPDX-License-Identifier: Apache-2.0

package compliance

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"sort"
	"strings"

	"github.com/nexops-one/compliance-engine/pkg/canonical"
	"github.com/nexops-one/compliance-engine/pkg/catalog"
	"github.com/nexops-one/compliance-engine/pkg/engine"
	"github.com/nexops-one/compliance-engine/pkg/extension"
	"github.com/nexops-one/compliance-engine/pkg/report"
	"github.com/nexops-one/compliance-engine/pkg/store"
	"github.com/nexops-one/compliance-engine/pkg/workflow"
)

var (
	// ErrUnknownProfile means no registered report profile has the ID.
	ErrUnknownProfile = errors.New("unknown report profile")
	// ErrInvalidReportRequest wraps refused report requests.
	ErrInvalidReportRequest = errors.New("invalid report request")
)

// ExportIncompleteError is returned when a profile refuses an incomplete export.
type ExportIncompleteError struct {
	Profile  string              `json:"profile"`
	Findings []extension.Finding `json:"findings"`
}

func (e *ExportIncompleteError) Error() string {
	return fmt.Sprintf("profile %s refuses an incomplete export: %d blocking finding(s)", e.Profile, len(e.Findings))
}

// Extensions add capabilities to an Engine.
type Extensions struct {
	// ReportProfiles are registered besides Profile B; IDs must be unique.
	ReportProfiles []extension.ReportProfile
	// WorkflowPolicy decides what approvals need (default extension.BasicWorkflow).
	WorkflowPolicy extension.WorkflowPolicy
}

func registerProfiles(extra []extension.ReportProfile) (map[string]extension.ReportProfile, error) {
	out := map[string]extension.ReportProfile{}
	for _, p := range append([]extension.ReportProfile{report.ProfileB()}, extra...) {
		id := p.ID()
		if id == "" || strings.ContainsAny(id, "/ ") {
			return nil, fmt.Errorf("compliance: invalid report profile ID %q", id)
		}
		if _, dup := out[id]; dup {
			return nil, fmt.Errorf("compliance: report profile %q is registered twice", id)
		}
		out[id] = p
	}
	return out, nil
}

// ProfileInfo describes a registered report profile for a workspace.
type ProfileInfo struct {
	ID          string             `json:"id"`
	Feature     extension.Feature  `json:"feature"`
	Formats     []string           `json:"formats"`
	Entitlement extension.Decision `json:"entitlement"`
	// Parameters are the report parameters the profile accepts.
	Parameters []extension.ParameterSpec `json:"parameters,omitempty"`
}

// ReportProfiles lists the registered profiles, by ID, with the workspace's
// entitlement to generate each.
func (e *Engine) ReportProfiles(ctx context.Context, scope Scope) ([]ProfileInfo, error) {
	if err := scope.Validate(); err != nil {
		return nil, err
	}
	out := []ProfileInfo{}
	for _, id := range e.profileIDs() {
		p := e.profiles[id]
		out = append(out, ProfileInfo{ID: id, Feature: p.Feature(), Formats: append([]string{"json"}, p.Formats()...),
			Entitlement: e.ents.Allowed(ctx, scope, p.Feature()), Parameters: ParameterSpecs(p)})
	}
	return out, nil
}

func (e *Engine) profileIDs() []string {
	ids := make([]string, 0, len(e.profiles))
	for id := range e.profiles {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

func (e *Engine) profile(id string) (extension.ReportProfile, error) {
	p, ok := e.profiles[id]
	if !ok {
		return nil, fmt.Errorf("%w: %q (registered: %s)", ErrUnknownProfile, id, strings.Join(e.profileIDs(), ", "))
	}
	return p, nil
}

// ReportRequest asks for a report. With EvaluationID the report states that
// stored evaluation; otherwise the engine first evaluates SnapshotID ("" =
// current) against Catalogs (none = latest).
type ReportRequest struct {
	Profile      string        `json:"profile"`
	EvaluationID string        `json:"evaluation_id,omitempty"`
	SnapshotID   string        `json:"snapshot_id,omitempty"`
	Catalogs     []catalog.Ref `json:"catalogs,omitempty"`
	// Formats are the renderings wanted besides the JSON facts (none = all).
	Formats []string `json:"formats,omitempty"`
	// Parameters are the profile's report parameters (see
	// extension.ParameterizedProfile); unknown names are refused.
	Parameters map[string]string `json:"parameters,omitempty"`
	// AllowIncomplete asks for an export marked incomplete instead of a
	// refusal (export_incomplete) when the profile finds blocking problems.
	AllowIncomplete bool `json:"allow_incomplete,omitempty"`
}

// ReportInputs is what reproducing a report needs besides its snapshot and
// evaluation. It is stored with the report.
type ReportInputs struct {
	Meta     extension.ReportMeta       `json:"meta"`
	Formats  []string                   `json:"formats"`
	Supplied []string                   `json:"supplied"` // "entity.field" pairs the manifests declared
	Evidence []extension.ReportEvidence `json:"evidence"`
	People   map[string]string          `json:"people"`
	// Parameters and AllowIncomplete are the request's, so regeneration
	// reproduces the same output.
	Parameters      map[string]string `json:"parameters,omitempty"`
	AllowIncomplete bool              `json:"allow_incomplete,omitempty"`
}

// GenerateReport generates, stores and audits a report.
func (e *Engine) GenerateReport(ctx context.Context, scope Scope, req ReportRequest) (store.Report, error) {
	if err := scope.Validate(); err != nil {
		return store.Report{}, err
	}
	p, err := e.profile(req.Profile)
	if err != nil {
		return store.Report{}, err
	}
	formats := []string{}
	for _, f := range req.Formats {
		if f != "json" && !slices.Contains(p.Formats(), f) {
			return store.Report{}, fmt.Errorf("%w: profile %s has no format %q (formats: json, %s)", ErrInvalidReportRequest, p.ID(), f, strings.Join(p.Formats(), ", "))
		}
		if !slices.Contains(formats, f) {
			formats = append(formats, f)
		}
	}
	if err := checkParameters(p, req.Parameters); err != nil {
		return store.Report{}, err
	}
	if _, err := e.require(ctx, scope, p.Feature()); err != nil {
		return store.Report{}, err
	}
	ev, err := e.reportEvaluation(ctx, scope, req)
	if err != nil {
		return store.Report{}, err
	}
	cats, err := e.catalogs.Resolve(ev.Catalogs)
	if err != nil {
		return store.Report{}, err
	}
	snap, err := e.Snapshot(ctx, scope, ev.SnapshotID)
	if err != nil {
		return store.Report{}, err
	}
	sup, err := e.supply(ctx, scope)
	if err != nil {
		return store.Report{}, err
	}
	supplied := suppliedPairs(e.schemas.Latest().EntityNames(), func(entity string) []string {
		en, _ := e.schemas.Latest().Entity(entity)
		return en.FieldNames
	}, sup)
	computed := e.computeWith(snap, cats, supplyOf(supplied))
	if !sameJSON(computed, ev.Result) {
		return store.Report{}, fmt.Errorf("%w: the adapter manifests changed since evaluation %s; evaluate again", ErrStaleEvaluation, ev.ID)
	}
	settings, err := e.store.Settings(ctx, scope)
	if err != nil {
		return store.Report{}, err
	}
	evidence, err := e.reportEvidence(ctx, scope, *ev.Effective)
	if err != nil {
		return store.Report{}, err
	}
	people, err := e.people(ctx, scope, *ev.Effective)
	if err != nil {
		return store.Report{}, err
	}
	actor, kind := e.actor(ctx)
	inputs := ReportInputs{
		Meta: extension.ReportMeta{
			ReportID: e.newID("rpt"), Profile: p.ID(), Scope: scope, EngineVersion: e.version, SchemaVersion: ev.Result.SchemaVersion,
			SnapshotID: ev.SnapshotID, EvaluationID: ev.ID, Catalogs: append([]catalog.Ref{}, ev.Catalogs...), AsOf: ev.Effective.AsOf,
			GeneratedAt: e.now(), GeneratedBy: actor, GeneratorKind: extension.ActorKind(kind), Sample: settings.Sample,
		},
		Formats: formats, Supplied: supplied, Evidence: evidence, People: people,
		Parameters: req.Parameters, AllowIncomplete: req.AllowIncomplete,
	}
	out, err := e.runProfile(ctx, p, inputs, snap.Records, cats, computed, *ev.Effective)
	if err != nil {
		return store.Report{}, err
	}
	if !out.Complete && len(out.Blocking) > 0 {
		return store.Report{}, &ExportIncompleteError{Profile: p.ID(), Findings: out.Blocking}
	}
	files, metas, err := reportFiles(out)
	if err != nil {
		return store.Report{}, err
	}
	m := inputs.Meta
	r := store.Report{
		ID: m.ReportID, Scope: scope, Profile: p.ID(), CreatedAt: m.GeneratedAt, CreatedBy: m.GeneratedBy, CreatedByKind: kind,
		EvaluationID: m.EvaluationID, SnapshotID: m.SnapshotID, Catalogs: refNames(m.Catalogs), AsOf: m.AsOf, Sample: m.Sample,
		Complete: out.Complete, FactsHash: report.Hash(out.Facts), Files: metas, Inputs: mustJSON(inputs),
		Facts: out.Facts, Validation: out.Validation,
	}
	names := make([]string, len(metas))
	for i, f := range metas {
		names[i] = f.Name
	}
	audit := e.auditEvent(ctx, scope, "report.generate", "report", r.ID, map[string]any{
		"profile": r.Profile, "evaluation_id": r.EvaluationID, "snapshot_id": r.SnapshotID, "facts_hash": r.FactsHash,
		"files": names, "complete": r.Complete, "sample": r.Sample,
	})
	if err := e.store.SaveReport(ctx, r, files, audit); err != nil {
		return store.Report{}, err
	}
	return r, nil
}

// reportEvaluation returns the evaluation a report states.
func (e *Engine) reportEvaluation(ctx context.Context, scope Scope, req ReportRequest) (Evaluation, error) {
	if req.EvaluationID == "" {
		return e.Evaluate(ctx, scope, req.SnapshotID, req.Catalogs)
	}
	if req.SnapshotID != "" || len(req.Catalogs) > 0 {
		return Evaluation{}, fmt.Errorf("%w: give evaluation_id, or snapshot_id and catalogs, not both", ErrInvalidReportRequest)
	}
	ev, err := e.Evaluation(ctx, scope, req.EvaluationID)
	if err != nil {
		return Evaluation{}, err
	}
	if ev.Effective == nil {
		return Evaluation{}, fmt.Errorf("%w: evaluation %s has no effective result; evaluate again", ErrInvalidReportRequest, ev.ID)
	}
	return ev, nil
}

func (e *Engine) computeWith(snap Snapshot, cats []*catalog.Catalog, sup canonical.Supply) engine.Result {
	ix := e.index(snap.Records)
	return engine.Evaluate(engine.Input{SnapshotID: snap.ID, Index: ix, References: canonical.CheckReferences(ix), Supplied: sup, Catalogs: cats})
}

// suppliedPairs lists, sorted, every entity.field some manifest supplies.
func suppliedPairs(entities []string, fields func(string) []string, sup canonical.Supply) []string {
	out := []string{}
	for _, en := range entities {
		for _, f := range fields(en) {
			if sup(en, f) {
				out = append(out, en+"."+f)
			}
		}
	}
	sort.Strings(out)
	return out
}

func supplyOf(pairs []string) canonical.Supply {
	set := map[string]bool{}
	for _, p := range pairs {
		set[p] = true
	}
	return func(entity, field string) bool { return set[entity+"."+field] }
}

func sameJSON(a, b any) bool { return bytes.Equal(mustJSON(a), mustJSON(b)) }

func refNames(refs []catalog.Ref) []string {
	out := make([]string, len(refs))
	for i, r := range refs {
		out[i] = r.String()
	}
	return out
}

// reportEvidence returns the evidence linked to the result's controls,
// reduced to what reports may show.
func (e *Engine) reportEvidence(ctx context.Context, scope Scope, res workflow.Result) ([]extension.ReportEvidence, error) {
	linked := map[string]bool{}
	for _, fw := range res.Frameworks {
		for _, c := range fw.Controls {
			for _, x := range c.Evidence {
				linked[x.ID] = true
			}
		}
	}
	evs, err := e.store.ListEvidence(ctx, scope, store.EvidenceQuery{})
	if err != nil {
		return nil, err
	}
	out := []extension.ReportEvidence{}
	for _, x := range evs {
		if !linked[x.ID] {
			continue
		}
		method := ""
		if n := len(x.Checks); n > 0 {
			method = x.Checks[n-1].Method
		}
		out = append(out, extension.ReportEvidence{ID: x.ID, Title: x.Title, Kind: x.Kind, Source: x.Source, Location: report.Redact(x.URI),
			Checksum: x.Checksum, Integrity: x.Integrity, CheckMethod: method, CollectedAt: x.CollectedAt, ValidUntil: x.ValidUntil,
			RevokedAt: x.RevokedAt, Links: append([]store.ControlRef{}, x.Links...), State: x.StateAt(res.AsOf)})
	}
	return out, nil
}

// people maps the user actors a result names to their emails.
func (e *Engine) people(ctx context.Context, scope Scope, res workflow.Result) (map[string]string, error) {
	var actors []string
	for _, fw := range res.Frameworks {
		for _, c := range fw.Controls {
			actors = append(actors, c.Owner, c.Reviewer)
			if c.Approval != nil {
				actors = append(actors, c.Approval.Actor)
				actors = append(actors, c.Approval.Approvers...)
			}
		}
	}
	if p, ok := extension.PrincipalFrom(ctx); ok {
		actors = append(actors, p.Actor)
	}
	return e.Emails(ctx, scope, actors...)
}

func (e *Engine) runProfile(ctx context.Context, p extension.ReportProfile, in ReportInputs, recs []store.RecordVersion,
	cats []*catalog.Catalog, computed engine.Result, eff workflow.Result) (extension.ReportOutput, error) {
	comp := canonical.AnalyzeCompleteness(e.index(recs), e.codelists, supplyOf(in.Supplied))
	comp.SnapshotID = in.Meta.SnapshotID
	out, err := p.Generate(ctx, extension.ReportInput{
		Meta: in.Meta, Schema: e.schemas.Latest(), Catalogs: cats, Records: recs, Computed: computed, Effective: eff,
		Completeness: comp, Evidence: in.Evidence, People: in.People, Formats: in.Formats,
		Parameters: cloneParams(in.Parameters), AllowIncomplete: in.AllowIncomplete,
	})
	if err != nil {
		return out, fmt.Errorf("report profile %s: %w", p.ID(), err)
	}
	if len(out.Facts) == 0 || !json.Valid(out.Facts) {
		return out, fmt.Errorf("report profile %s returned invalid facts", p.ID())
	}
	return out, nil
}

// ParameterSpecs returns the parameters a profile accepts.
func ParameterSpecs(p extension.ReportProfile) []extension.ParameterSpec {
	if pp, ok := p.(extension.ParameterizedProfile); ok {
		return pp.Parameters()
	}
	return nil
}

// checkParameters refuses parameters the profile does not declare and values
// not matching their pattern.
func checkParameters(p extension.ReportProfile, params map[string]string) error {
	specs := map[string]extension.ParameterSpec{}
	for _, s := range ParameterSpecs(p) {
		specs[s.Name] = s
	}
	names := make([]string, 0, len(params))
	for n := range params {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		s, ok := specs[n]
		if !ok {
			return fmt.Errorf("%w: profile %s has no parameter %q", extension.ErrInvalidParameter, p.ID(), n)
		}
		if s.Pattern != "" {
			re, err := regexp.Compile("^(?:" + s.Pattern + ")$")
			if err != nil {
				return fmt.Errorf("report profile %s: parameter %s has an invalid pattern: %w", p.ID(), n, err)
			}
			if !re.MatchString(params[n]) {
				return fmt.Errorf("%w: %s must match %s", extension.ErrInvalidParameter, n, s.Pattern)
			}
		}
	}
	return nil
}

func cloneParams(in map[string]string) map[string]string {
	out := map[string]string{}
	for k, v := range in {
		out[k] = v
	}
	return out
}

// reportFiles checks and describes a profile's files; report.json (the
// facts) is added when the profile did not render it.
func reportFiles(out extension.ReportOutput) ([]store.ReportFile, []store.ReportFileMeta, error) {
	all := out.Files
	if !slices.ContainsFunc(all, func(f extension.ReportFile) bool { return f.Name == report.JSONFile }) {
		all = append([]extension.ReportFile{{Name: report.JSONFile, ContentType: "application/json", Data: out.Facts}}, all...)
	}
	files := []store.ReportFile{}
	metas := []store.ReportFileMeta{}
	seen := map[string]bool{}
	for _, f := range all {
		if f.Name == "" || strings.ContainsAny(f.Name, "/\\") || seen[f.Name] || f.ContentType == "" {
			return nil, nil, fmt.Errorf("report profile returned an invalid or duplicate file %q", f.Name)
		}
		if f.Name == report.JSONFile && !bytes.Equal(f.Data, out.Facts) {
			return nil, nil, fmt.Errorf("report profile returned a %s that differs from its facts", report.JSONFile)
		}
		seen[f.Name] = true
		m := store.ReportFileMeta{Name: f.Name, ContentType: f.ContentType, Size: int64(len(f.Data)), SHA256: report.Hash(f.Data)}
		metas = append(metas, m)
		files = append(files, store.ReportFile{ReportFileMeta: m, Data: f.Data})
	}
	return files, metas, nil
}

// Reports lists a workspace's reports, newest first, without facts.
func (e *Engine) Reports(ctx context.Context, scope Scope) ([]store.Report, error) {
	if err := scope.Validate(); err != nil {
		return nil, err
	}
	return e.store.Reports(ctx, scope)
}

// Report returns a report with its facts and logs the download. Reading is
// never gated by entitlements.
func (e *Engine) Report(ctx context.Context, scope Scope, id string) (store.Report, error) {
	if err := scope.Validate(); err != nil {
		return store.Report{}, err
	}
	r, err := e.store.Report(ctx, scope, id)
	if err != nil {
		return store.Report{}, err
	}
	r.Inputs = nil
	if err := e.store.AppendAudit(ctx, e.auditEvent(ctx, scope, "report.download", "report", id, map[string]any{"part": "facts"})); err != nil {
		return store.Report{}, err
	}
	return r, nil
}

// ReportFile returns one rendering of a report and logs the download.
func (e *Engine) ReportFile(ctx context.Context, scope Scope, id, name string) (store.ReportFile, error) {
	if err := scope.Validate(); err != nil {
		return store.ReportFile{}, err
	}
	f, err := e.store.ReportFile(ctx, scope, id, name)
	if err != nil {
		return store.ReportFile{}, err
	}
	if err := e.store.AppendAudit(ctx, e.auditEvent(ctx, scope, "report.download", "report", id, map[string]any{"part": name})); err != nil {
		return store.ReportFile{}, err
	}
	return f, nil
}

// FileCheck is the regeneration outcome of one rendering.
type FileCheck struct {
	Name      string `json:"name"`
	Identical bool   `json:"identical"`
}

// Regeneration is the outcome of rebuilding a report from its inputs.
type Regeneration struct {
	ReportID             string `json:"report_id"`
	Identical            bool   `json:"identical"`
	FactsHash            string `json:"facts_hash"`
	RegeneratedFactsHash string `json:"regenerated_facts_hash,omitempty"`
	// Mismatch names the first difference: evaluation (the stored evaluation
	// no longer follows from its snapshot and inputs), facts or files.
	Mismatch string      `json:"mismatch,omitempty"`
	Files    []FileCheck `json:"files"`
}

// RegenerateReport rebuilds a report from its stored snapshot, evaluation and
// inputs and compares the facts hash and file hashes. It stores nothing but
// its audit event, and is not gated by entitlements.
func (e *Engine) RegenerateReport(ctx context.Context, scope Scope, id string) (Regeneration, error) {
	if err := scope.Validate(); err != nil {
		return Regeneration{}, err
	}
	r, err := e.store.Report(ctx, scope, id)
	if err != nil {
		return Regeneration{}, err
	}
	res, err := e.regenerate(ctx, scope, r)
	if err != nil {
		return Regeneration{}, err
	}
	ev := e.auditEvent(ctx, scope, "report.regenerate", "report", id, map[string]any{"identical": res.Identical, "mismatch": res.Mismatch})
	if err := e.store.AppendAudit(ctx, ev); err != nil {
		return Regeneration{}, err
	}
	return res, nil
}

func (e *Engine) regenerate(ctx context.Context, scope Scope, r store.Report) (Regeneration, error) {
	res := Regeneration{ReportID: r.ID, FactsHash: r.FactsHash, Files: []FileCheck{}}
	var in ReportInputs
	if err := json.Unmarshal(r.Inputs, &in); err != nil {
		return res, fmt.Errorf("compliance: decode report %s inputs: %w", r.ID, err)
	}
	p, err := e.profile(r.Profile)
	if err != nil {
		return res, err
	}
	ev, err := e.Evaluation(ctx, scope, r.EvaluationID)
	if err != nil {
		return res, err
	}
	cats, err := e.catalogs.Resolve(ev.Catalogs)
	if err != nil {
		return res, err
	}
	snap, err := e.Snapshot(ctx, scope, r.SnapshotID)
	if err != nil {
		return res, err
	}
	computed := e.computeWith(snap, cats, supplyOf(in.Supplied))
	if ev.Effective == nil || !sameJSON(computed, ev.Result) || ev.SnapshotID != r.SnapshotID {
		res.Mismatch = "evaluation"
		return res, nil
	}
	eff := workflow.Recompute(computed, cats, *ev.Effective)
	if !sameJSON(eff, *ev.Effective) {
		res.Mismatch = "evaluation"
		return res, nil
	}
	out, err := e.runProfile(ctx, p, in, snap.Records, cats, computed, eff)
	if err != nil {
		return res, err
	}
	res.RegeneratedFactsHash = report.Hash(out.Facts)
	_, metas, err := reportFiles(out)
	if err != nil {
		return res, err
	}
	regenerated := map[string]string{}
	for _, m := range metas {
		regenerated[m.Name] = m.SHA256
	}
	filesOK := len(metas) == len(r.Files)
	for _, f := range r.Files {
		ok := regenerated[f.Name] == f.SHA256
		filesOK = filesOK && ok
		res.Files = append(res.Files, FileCheck{Name: f.Name, Identical: ok})
	}
	switch {
	case res.RegeneratedFactsHash != r.FactsHash:
		res.Mismatch = "facts"
	case !filesOK:
		res.Mismatch = "files"
	default:
		res.Identical = true
	}
	return res, nil
}
