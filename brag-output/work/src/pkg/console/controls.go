// SPDX-License-Identifier: Apache-2.0

package console

import (
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/nexops-one/compliance-engine/pkg/access"
	"github.com/nexops-one/compliance-engine/pkg/catalog"
	"github.com/nexops-one/compliance-engine/pkg/compliance"
	"github.com/nexops-one/compliance-engine/pkg/engine"
	"github.com/nexops-one/compliance-engine/pkg/store"
	"github.com/nexops-one/compliance-engine/pkg/workflow"
)

// people renders actors: member emails where known, the actor otherwise.
type people map[string]string

func (p people) Name(actor string) string {
	if e, ok := p[actor]; ok {
		return e
	}
	return actor
}

type controlsPage struct {
	Catalogs []catalog.Ref
	Selected string
	Result   workflow.Result
	People   people
}

func (c *Console) controlsPage(w http.ResponseWriter, r *http.Request, rc *reqCtx) error {
	page := controlsPage{Catalogs: c.eng.Catalogs(), Selected: r.URL.Query().Get("catalog")}
	var refs []catalog.Ref
	if page.Selected != "" {
		ref, err := catalog.ParseRef(page.Selected)
		if err != nil {
			return problem(http.StatusBadRequest, "invalid_parameter", err.Error())
		}
		refs = []catalog.Ref{ref}
	}
	res, err := c.eng.Status(r.Context(), rc.p.Scope, refs)
	if err != nil {
		return err
	}
	var actors []string
	for _, fw := range res.Frameworks {
		for _, ctl := range fw.Controls {
			actors = append(actors, ctl.Owner)
		}
	}
	emails, err := c.eng.Emails(r.Context(), rc.p.Scope, actors...)
	if err != nil {
		return err
	}
	page.Result, page.People = res, people(emails)
	c.render(w, r, rc, "controls", "Controls", http.StatusOK, page)
	return nil
}

type controlPage struct {
	Status     compliance.ControlStatus
	Assessment compliance.AssessmentView
	Evidence   []evidenceRow
	People     people
	Path       string
	// EvaluationID is a stored evaluation of the control as shown, for the
	// approve form ("the evaluation the approver reviewed").
	EvaluationID string
	CanSubmit    bool
	IsOwner      bool
	Due          string
	Now          time.Time
}

type evidenceRow struct {
	Evidence store.Evidence
	State    store.EvidenceState
}

func controlPath(catalogName, controlID string) string {
	return Prefix + "/controls/" + url.PathEscape(catalogName) + "/" + url.PathEscape(controlID)
}

func backToControl(r *http.Request) string {
	return controlPath(r.PathValue("catalog"), r.PathValue("control"))
}

func (c *Console) controlPage(w http.ResponseWriter, r *http.Request, rc *reqCtx) error {
	ctx, sc := r.Context(), rc.p.Scope
	cs, err := c.eng.ControlStatus(ctx, sc, r.PathValue("catalog"), r.PathValue("control"))
	if err != nil {
		return err
	}
	av, err := c.eng.Assessment(ctx, sc, cs.Catalog.Catalog, cs.Control.ID)
	if err != nil {
		return err
	}
	evs, err := c.eng.ListEvidence(ctx, sc, store.EvidenceQuery{Catalog: cs.Catalog.Catalog, ControlID: cs.Control.ID})
	if err != nil {
		return err
	}
	now := c.opts.Clock()
	page := controlPage{Status: cs, Assessment: av, Path: controlPath(cs.Catalog.Catalog, cs.Control.ID), Now: now,
		IsOwner: av.Assessment.Owner == rc.p.Actor}
	actors := []string{av.Assessment.Owner, av.Assessment.Reviewer}
	if a := av.Assessment.Approval; a != nil {
		actors = append(actors, a.Actor)
	}
	for _, t := range av.History {
		actors = append(actors, t.Actor)
	}
	for _, e := range evs {
		page.Evidence = append(page.Evidence, evidenceRow{Evidence: e, State: e.StateAt(now)})
		actors = append(actors, e.Collector)
	}
	emails, err := c.eng.Emails(ctx, sc, actors...)
	if err != nil {
		return err
	}
	page.People = people(emails)
	if d := av.Assessment.DueAt; d != nil {
		page.Due = d.UTC().Format("2006-01-02")
	}
	page.CanSubmit = cs.Computed.Status == engine.StatusMonitoring
	if access.Allows(rc.p.Roles, access.PermWorkflowApprove) && av.Assessment.Stage == store.StageSubmitted && page.CanSubmit && rc.problem == nil {
		if ev, err := c.eng.Evaluate(ctx, sc, "", []catalog.Ref{cs.Catalog}); err == nil {
			page.EvaluationID = ev.ID
		}
	}
	c.render(w, r, rc, "control", cs.Control.ID+" "+cs.Control.Title, http.StatusOK, page)
	return nil
}

func formPtr(r *http.Request, name string) *string {
	if _, ok := r.PostForm[name]; !ok {
		return nil
	}
	v := strings.TrimSpace(r.PostFormValue(name))
	return &v
}

func (c *Console) assign(w http.ResponseWriter, r *http.Request, rc *reqCtx) error {
	in := compliance.AssignInput{Owner: formPtr(r, "owner"), Reviewer: formPtr(r, "reviewer"), Notes: formPtr(r, "notes")}
	if due := formPtr(r, "due"); due != nil {
		v := ""
		if *due != "" {
			d, err := time.Parse("2006-01-02", *due)
			if err != nil {
				return problem(http.StatusUnprocessableEntity, "invalid_request", "The due date must be a date such as 2026-12-31.")
			}
			v = d.Format(time.RFC3339)
		}
		in.DueAt = &v
	}
	if _, err := c.eng.Assign(r.Context(), rc.p.Scope, r.PathValue("catalog"), r.PathValue("control"), in); err != nil {
		return err
	}
	http.Redirect(w, r, backToControl(r)+"?ok=assigned", http.StatusSeeOther)
	return nil
}

func (c *Console) act(action workflow.Action) func(http.ResponseWriter, *http.Request, *reqCtx) error {
	return func(w http.ResponseWriter, r *http.Request, rc *reqCtx) error {
		in := compliance.ActInput{Reason: strings.TrimSpace(r.PostFormValue("reason")), Note: strings.TrimSpace(r.PostFormValue("note")),
			EvaluationID: r.PostFormValue("evaluation_id")}
		if _, err := c.eng.Act(r.Context(), rc.p.Scope, r.PathValue("catalog"), r.PathValue("control"), action, in); err != nil {
			return err
		}
		http.Redirect(w, r, backToControl(r)+"?ok=acted", http.StatusSeeOther)
		return nil
	}
}

func optionalDate(r *http.Request, name string) (*time.Time, error) {
	v := strings.TrimSpace(r.PostFormValue(name))
	if v == "" {
		return nil, nil
	}
	d, err := time.Parse("2006-01-02", v)
	if err != nil {
		return nil, problem(http.StatusUnprocessableEntity, "invalid_request", fmt.Sprintf("%s must be a date such as 2026-12-31.", strings.ReplaceAll(name, "_", " ")))
	}
	return &d, nil
}

func (c *Console) attachEvidence(w http.ResponseWriter, r *http.Request, rc *reqCtx) error {
	valid, err := optionalDate(r, "valid_until")
	if err != nil {
		return err
	}
	in := compliance.EvidenceInput{Title: strings.TrimSpace(r.PostFormValue("title")), Kind: r.PostFormValue("kind"),
		Source: strings.TrimSpace(r.PostFormValue("source")), URI: strings.TrimSpace(r.PostFormValue("uri")),
		Checksum: strings.TrimSpace(r.PostFormValue("checksum")), ValidUntil: valid,
		Links: []store.ControlRef{{Catalog: r.PathValue("catalog"), ControlID: r.PathValue("control")}}}
	if v := strings.TrimSpace(r.PostFormValue("min_days")); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 0 {
			return problem(http.StatusUnprocessableEntity, "invalid_request", "The minimum retention must be a number of days.")
		}
		in.Retention = store.Retention{MinDays: n, Basis: strings.TrimSpace(r.PostFormValue("basis"))}
	}
	if _, err := c.eng.AddEvidence(r.Context(), rc.p.Scope, in); err != nil {
		return err
	}
	http.Redirect(w, r, backToControl(r)+"?ok=evidence", http.StatusSeeOther)
	return nil
}

func (c *Console) linkEvidence(w http.ResponseWriter, r *http.Request, rc *reqCtx) error {
	ref := store.ControlRef{Catalog: r.PathValue("catalog"), ControlID: r.PathValue("control")}
	if _, err := c.eng.LinkEvidence(r.Context(), rc.p.Scope, strings.TrimSpace(r.PostFormValue("evidence")), ref); err != nil {
		return err
	}
	http.Redirect(w, r, backToControl(r)+"?ok=evidence", http.StatusSeeOther)
	return nil
}

// backField returns the console page named by the form's back field.
func backField(r *http.Request) string {
	if b := safeNext(r.PostFormValue("back")); b != "" {
		return b
	}
	return Prefix + "/controls"
}

func (c *Console) verifyEvidence(w http.ResponseWriter, r *http.Request, rc *reqCtx) error {
	if _, err := c.eng.VerifyEvidence(r.Context(), rc.p.Scope, r.PathValue("id")); err != nil {
		return err
	}
	http.Redirect(w, r, backField(r)+"?ok=verified", http.StatusSeeOther)
	return nil
}

func (c *Console) revokeEvidence(w http.ResponseWriter, r *http.Request, rc *reqCtx) error {
	if _, err := c.eng.RevokeEvidence(r.Context(), rc.p.Scope, r.PathValue("id"), strings.TrimSpace(r.PostFormValue("reason"))); err != nil {
		return err
	}
	http.Redirect(w, r, backField(r)+"?ok=revoked", http.StatusSeeOther)
	return nil
}
