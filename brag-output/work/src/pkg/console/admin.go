// SPDX-License-Identifier: Apache-2.0

package console

import (
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/nexops-one/compliance-engine/pkg/access"
	"github.com/nexops-one/compliance-engine/pkg/audit"
	"github.com/nexops-one/compliance-engine/pkg/catalog"
	"github.com/nexops-one/compliance-engine/pkg/compliance"
	"github.com/nexops-one/compliance-engine/pkg/identity"
	"github.com/nexops-one/compliance-engine/pkg/store"
)

type reportsPage struct {
	Profiles []compliance.ProfileInfo
	Formats  []string // renderings any profile offers besides json
	Catalogs []catalog.Ref
	Reports  []store.Report
	People   people
}

func (c *Console) reportsPage(w http.ResponseWriter, r *http.Request, rc *reqCtx) error {
	ctx, sc := r.Context(), rc.p.Scope
	profiles, err := c.eng.ReportProfiles(ctx, sc)
	if err != nil {
		return err
	}
	reports, err := c.eng.Reports(ctx, sc)
	if err != nil {
		return err
	}
	var actors []string
	for _, rep := range reports {
		actors = append(actors, rep.CreatedBy)
	}
	emails, err := c.eng.Emails(ctx, sc, actors...)
	if err != nil {
		return err
	}
	page := reportsPage{Profiles: profiles, Catalogs: c.eng.Catalogs(), Reports: reports, People: people(emails)}
	seen := map[string]bool{"json": true}
	for _, p := range profiles {
		for _, f := range p.Formats {
			if !seen[f] {
				seen[f] = true
				page.Formats = append(page.Formats, f)
			}
		}
	}
	sort.Strings(page.Formats)
	c.render(w, r, rc, "reports", "Reports", http.StatusOK, page)
	return nil
}

func (c *Console) generateReport(w http.ResponseWriter, r *http.Request, rc *reqCtx) error {
	req := compliance.ReportRequest{Profile: r.PostFormValue("profile"), Formats: r.PostForm["format"],
		AllowIncomplete: r.PostFormValue("allow_incomplete."+r.PostFormValue("profile")) == "true"}
	prefix := "param." + req.Profile + "."
	for name, values := range r.PostForm {
		if p, ok := strings.CutPrefix(name, prefix); ok && len(values) > 0 && strings.TrimSpace(values[0]) != "" {
			if req.Parameters == nil {
				req.Parameters = map[string]string{}
			}
			req.Parameters[p] = strings.TrimSpace(values[0])
		}
	}
	for _, raw := range r.PostForm["catalog"] {
		ref, err := catalog.ParseRef(raw)
		if err != nil {
			return problem(http.StatusBadRequest, "invalid_parameter", err.Error())
		}
		req.Catalogs = append(req.Catalogs, ref)
	}
	rep, err := c.eng.GenerateReport(r.Context(), rc.p.Scope, req)
	if err != nil {
		return err
	}
	http.Redirect(w, r, Prefix+"/reports/"+url.PathEscape(rep.ID)+"?ok=generated", http.StatusSeeOther)
	return nil
}

type reportPage struct {
	Report       store.Report
	People       people
	Regeneration *compliance.Regeneration
}

// reportMeta finds a report's metadata without reading its facts, so that
// viewing the page is not a download.
func (c *Console) reportMeta(r *http.Request, rc *reqCtx, id string) (store.Report, people, error) {
	reports, err := c.eng.Reports(r.Context(), rc.p.Scope)
	if err != nil {
		return store.Report{}, nil, err
	}
	for _, rep := range reports {
		if rep.ID == id {
			emails, err := c.eng.Emails(r.Context(), rc.p.Scope, rep.CreatedBy)
			return rep, people(emails), err
		}
	}
	return store.Report{}, nil, problem(http.StatusNotFound, "not_found", "There is no such report.")
}

func (c *Console) reportPage(w http.ResponseWriter, r *http.Request, rc *reqCtx) error {
	rep, ppl, err := c.reportMeta(r, rc, r.PathValue("id"))
	if err != nil {
		return err
	}
	c.render(w, r, rc, "report", "Report "+rep.ID, http.StatusOK, reportPage{Report: rep, People: ppl})
	return nil
}

func (c *Console) reportFile(w http.ResponseWriter, r *http.Request, rc *reqCtx) error {
	f, err := c.eng.ReportFile(r.Context(), rc.p.Scope, r.PathValue("id"), r.PathValue("name"))
	if err != nil {
		return err
	}
	w.Header().Set("Digest", "sha-256="+f.SHA256)
	return download(w, f.ContentType, f.Name, f.Data)
}

func (c *Console) verifyReport(w http.ResponseWriter, r *http.Request, rc *reqCtx) error {
	rep, ppl, err := c.reportMeta(r, rc, r.PathValue("id"))
	if err != nil {
		return err
	}
	res, err := c.eng.RegenerateReport(r.Context(), rc.p.Scope, rep.ID)
	if err != nil {
		return err
	}
	c.render(w, r, rc, "report", "Report "+rep.ID, http.StatusOK, reportPage{Report: rep, People: ppl, Regeneration: &res})
	return nil
}

type overrideRow struct {
	Key, Interval, Evidence string
}

type settingsPage struct {
	Settings  store.Settings
	Overrides []overrideRow
	Tokens    []store.Token
	Members   []store.Member
	Roles     []access.Role
	NewToken  *identity.CreatedToken
}

func (c *Console) settingsPage(w http.ResponseWriter, r *http.Request, rc *reqCtx) error {
	return c.renderSettings(w, r, rc, nil)
}

func (c *Console) renderSettings(w http.ResponseWriter, r *http.Request, rc *reqCtx, created *identity.CreatedToken) error {
	ctx, p := r.Context(), rc.p
	set, err := c.eng.Settings(ctx, p.Scope)
	if err != nil {
		return err
	}
	page := settingsPage{Settings: set, Roles: access.AllRoles(), NewToken: created}
	for k, o := range set.ReviewOverrides {
		row := overrideRow{Key: k, Interval: o.ReviewInterval, Evidence: "catalog default"}
		if o.ApprovalRequiresEvidence != nil {
			row.Evidence = map[bool]string{true: "required", false: "not required"}[*o.ApprovalRequiresEvidence]
		}
		page.Overrides = append(page.Overrides, row)
	}
	sort.Slice(page.Overrides, func(i, j int) bool { return page.Overrides[i].Key < page.Overrides[j].Key })
	if page.Tokens, err = c.ids.Tokens(ctx, p); err != nil {
		return err
	}
	if access.Allows(p.Roles, access.PermMembersManage) {
		if page.Members, err = c.ids.Members(ctx, p); err != nil {
			return err
		}
	}
	c.render(w, r, rc, "settings", "Settings", http.StatusOK, page)
	return nil
}

func formRoles(r *http.Request) []access.Role {
	var out []access.Role
	for _, v := range r.PostForm["role"] {
		out = append(out, access.Role(v))
	}
	return out
}

func (c *Console) createToken(w http.ResponseWriter, r *http.Request, rc *reqCtx) error {
	req := identity.TokenRequest{Name: r.PostFormValue("name"), Roles: formRoles(r), Service: r.PostFormValue("service") == "on"}
	exp, err := optionalDate(r, "expires")
	if err != nil {
		return err
	}
	if exp != nil {
		end := exp.Add(24*time.Hour - time.Second)
		req.ExpiresAt = &end
	}
	created, err := c.ids.CreateToken(r.Context(), rc.p, req)
	if err != nil {
		return err
	}
	// The value is shown on this response only: no redirect, nothing stored.
	return c.renderSettings(w, r, rc, &created)
}

func (c *Console) revokeToken(w http.ResponseWriter, r *http.Request, rc *reqCtx) error {
	if err := c.ids.RevokeToken(r.Context(), rc.p, r.PathValue("id")); err != nil {
		return err
	}
	http.Redirect(w, r, Prefix+"/settings?ok=token", http.StatusSeeOther)
	return nil
}

func (c *Console) setMember(w http.ResponseWriter, r *http.Request, rc *reqCtx) error {
	if _, err := c.ids.SetMember(r.Context(), rc.p, r.PostFormValue("email"), formRoles(r)); err != nil {
		return err
	}
	http.Redirect(w, r, Prefix+"/settings?ok=member", http.StatusSeeOther)
	return nil
}

func (c *Console) removeMember(w http.ResponseWriter, r *http.Request, rc *reqCtx) error {
	if err := c.ids.RemoveMember(r.Context(), rc.p, r.PostFormValue("email")); err != nil {
		return err
	}
	http.Redirect(w, r, Prefix+"/settings?ok=removed", http.StatusSeeOther)
	return nil
}

func formInt(r *http.Request, name string) (int, error) {
	v := strings.TrimSpace(r.PostFormValue(name))
	if v == "" {
		return 0, nil
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return 0, problem(http.StatusUnprocessableEntity, "invalid_request", strings.ReplaceAll(name, "_", " ")+" must be a whole number.")
	}
	return n, nil
}

func (c *Console) updateRetention(w http.ResponseWriter, r *http.Request, rc *reqCtx) error {
	var pol store.RetentionPolicy
	var err error
	if pol.RevisionDays, err = formInt(r, "revision_days"); err != nil {
		return err
	}
	if pol.KeepRevisions, err = formInt(r, "keep_revisions"); err != nil {
		return err
	}
	if pol.EvaluationDays, err = formInt(r, "evaluation_days"); err != nil {
		return err
	}
	if _, err := c.eng.UpdateSettings(r.Context(), rc.p.Scope, compliance.SettingsInput{Retention: &pol}); err != nil {
		return err
	}
	http.Redirect(w, r, Prefix+"/settings?ok=settings", http.StatusSeeOther)
	return nil
}

// updateOverride sets or removes one review override; the others are kept.
func (c *Console) updateOverride(w http.ResponseWriter, r *http.Request, rc *reqCtx) error {
	set, err := c.eng.Settings(r.Context(), rc.p.Scope)
	if err != nil {
		return err
	}
	next := map[string]store.ReviewOverride{}
	for k, v := range set.ReviewOverrides {
		next[k] = v
	}
	key := strings.TrimSpace(r.PostFormValue("control"))
	if key == "" {
		return problem(http.StatusUnprocessableEntity, "invalid_request", "Name the control as <catalog>/<control>.")
	}
	if r.PostFormValue("remove") == "on" {
		delete(next, key)
	} else {
		o := store.ReviewOverride{ReviewInterval: strings.TrimSpace(r.PostFormValue("interval"))}
		switch r.PostFormValue("evidence") {
		case "required":
			v := true
			o.ApprovalRequiresEvidence = &v
		case "not_required":
			v := false
			o.ApprovalRequiresEvidence = &v
		}
		next[key] = o
	}
	if _, err := c.eng.UpdateSettings(r.Context(), rc.p.Scope, compliance.SettingsInput{ReviewOverrides: next}); err != nil {
		return err
	}
	http.Redirect(w, r, Prefix+"/settings?ok=settings", http.StatusSeeOther)
	return nil
}

// auditPageSize is how many events one audit page shows.
const auditPageSize = 50

type auditPage struct {
	Events       []store.AuditEvent
	After        int64
	Next         int64
	More         bool
	Verification *audit.Result
}

func (c *Console) auditPage(w http.ResponseWriter, r *http.Request, rc *reqCtx) error {
	after, _ := strconv.ParseInt(r.URL.Query().Get("after"), 10, 64)
	if after < 0 {
		after = 0
	}
	evs, err := c.eng.AuditEvents(r.Context(), rc.p.Scope, store.AuditQuery{AfterSeq: after, Limit: auditPageSize + 1})
	if err != nil {
		return err
	}
	page := auditPage{Events: evs, After: after}
	if len(evs) > auditPageSize {
		page.Events, page.More = evs[:auditPageSize], true
	}
	if n := len(page.Events); n > 0 {
		page.Next = page.Events[n-1].Seq
	}
	if r.URL.Query().Get("verify") == "1" {
		v, err := c.eng.VerifyAudit(r.Context(), rc.p.Scope)
		if err != nil {
			return err
		}
		page.Verification = &v
	}
	c.render(w, r, rc, "audit", "Audit log", http.StatusOK, page)
	return nil
}
