// SPDX-License-Identifier: Apache-2.0

// Package console serves the engine's web console under /console: a
// server-rendered user interface built from html/template pages and one
// embedded stylesheet. It needs no JavaScript and loads nothing from outside
// the binary. Every page and action is checked against the same permission
// and feature as the API route for the same operation.
package console

import (
	"context"
	"crypto/subtle"
	"embed"
	"errors"
	"html/template"
	"io/fs"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/nexops-one/compliance-engine/pkg/access"
	"github.com/nexops-one/compliance-engine/pkg/compliance"
	"github.com/nexops-one/compliance-engine/pkg/extension"
	"github.com/nexops-one/compliance-engine/pkg/httpapi"
	"github.com/nexops-one/compliance-engine/pkg/identity"
	"github.com/nexops-one/compliance-engine/pkg/store"
	"github.com/nexops-one/compliance-engine/pkg/workflow"
)

// Prefix is where the console is mounted.
const Prefix = "/console"

// DefaultMaxUploadBytes bounds console form bodies (imports, evidence) when
// Options.MaxUploadBytes is unset.
const DefaultMaxUploadBytes = 32 << 20

// SampleBanner is shown on every page of a sample workspace.
const SampleBanner = "Sample data: fictitious organization. Results are a demonstration, not a compliance status."

//go:embed templates/*.html static/*
var assets embed.FS

// Options configures the console.
type Options struct {
	// Tenant, when set, is the tenant every sign-in uses; the sign-in form
	// then does not ask for it (COMPLIANCE_CONSOLE_TENANT).
	Tenant  string
	Version string
	Logger  *slog.Logger
	// Failures is the failed sign-in budget per client address, shared with
	// the API; nil disables the limit.
	Failures       *httpapi.FailureLimiter
	MaxUploadBytes int64
	// SampleRegister is offered for download on the import page of sample
	// workspaces (the demo's sample-register.xlsx); nil offers nothing.
	SampleRegister []byte
	Clock          func() time.Time
	// SignIn are additional sign-in methods shown on the sign-in page.
	SignIn []SignInProvider
}

// Console is the console handler.
type Console struct {
	eng     *compliance.Engine
	ids     *identity.Service
	opts    Options
	mux     *http.ServeMux
	pages   map[string]*template.Template
	static  fs.FS
	pending *pendingImports
}

// route is one console endpoint. API names the API route ("METHOD pattern")
// performing the same operation, whose permission and feature the console
// must require too.
type route struct {
	Method     string
	Pattern    string
	Public     bool
	Permission access.Permission
	Feature    extension.Feature
	API        string
	Download   bool // serves a file, not a page
	// back is the page re-rendered with the error when an action fails.
	back   func(r *http.Request) string
	handle func(w http.ResponseWriter, r *http.Request, rc *reqCtx) error
}

// reqCtx is the signed-in context of one request.
type reqCtx struct {
	p          extension.Principal
	session    store.Session
	value      string
	email      string
	workspaces []store.Member
	sample     bool
	problem    *httpapi.Problem
}

// New returns the console handler, to be mounted at Prefix.
func New(eng *compliance.Engine, ids *identity.Service, opts Options) *Console {
	if opts.Logger == nil {
		opts.Logger = slog.Default()
	}
	if opts.MaxUploadBytes <= 0 {
		opts.MaxUploadBytes = DefaultMaxUploadBytes
	}
	if opts.Clock == nil {
		opts.Clock = func() time.Time { return time.Now().UTC() }
	}
	static, _ := fs.Sub(assets, "static")
	seen := map[string]bool{}
	for _, p := range opts.SignIn {
		if !providerID.MatchString(p.ID()) || seen[p.ID()] {
			panic("console: invalid or duplicate sign-in provider ID " + p.ID())
		}
		seen[p.ID()] = true
	}
	c := &Console{eng: eng, ids: ids, opts: opts, mux: http.NewServeMux(), static: static, pending: newPendingImports(opts.Clock)}
	c.pages = parsePages()
	for _, rt := range c.routes() {
		c.mux.Handle(rt.Method+" "+rt.Pattern, c.wrap(rt))
	}
	c.mux.Handle(Prefix+"/", c.wrap(route{Public: true, handle: func(http.ResponseWriter, *http.Request, *reqCtx) error {
		return problem(http.StatusNotFound, "not_found", "There is no such page.")
	}}))
	return c
}

func (c *Console) ServeHTTP(w http.ResponseWriter, r *http.Request) { c.mux.ServeHTTP(w, r) }

func (c *Console) routes() []route {
	const (
		read = access.PermDataRead
		flow = extension.FeatureWorkflowBasic
		evid = extension.FeatureEvidenceManage
	)
	return []route{
		{Method: "GET", Pattern: Prefix + "/static/{file}", Public: true, handle: c.serveStatic},
		{Method: "GET", Pattern: Prefix + "/{$}", Permission: read, handle: redirectTo(Prefix + "/controls")},
		{Method: "GET", Pattern: Prefix + "/signin", Public: true, handle: c.signInPage},
		{Method: "POST", Pattern: Prefix + "/signin", Public: true, handle: c.signIn},
		{Method: "GET", Pattern: Prefix + "/signin/{provider}", Public: true, handle: c.providerStart},
		{Method: "GET", Pattern: Prefix + "/signin/{provider}/callback", Public: true, handle: c.providerFinish},
		{Method: "POST", Pattern: Prefix + "/signout", Permission: read, handle: c.signOut},
		{Method: "GET", Pattern: Prefix + "/workspace", Permission: read, handle: c.workspacePage},
		{Method: "POST", Pattern: Prefix + "/workspace", Permission: read, back: fixed(Prefix + "/workspace"), handle: c.switchWorkspace},

		{Method: "GET", Pattern: Prefix + "/import", Permission: read, handle: c.importPage},
		{Method: "GET", Pattern: Prefix + "/import/templates.xlsx", Permission: read, Download: true, API: "GET /api/v1/templates.xlsx", handle: c.templateWorkbook},
		{Method: "GET", Pattern: Prefix + "/import/templates/{file}", Permission: read, Download: true, API: "GET /api/v1/templates/{file}", handle: c.templateCSV},
		{Method: "GET", Pattern: Prefix + "/import/sample-register.xlsx", Permission: read, Download: true, handle: c.sampleRegister},
		{Method: "POST", Pattern: Prefix + "/import", Permission: access.PermDataWrite, Feature: extension.FeatureRegisterImport,
			API: "POST /api/v1/imports", back: fixed(Prefix + "/import"), handle: c.validateImport},
		{Method: "POST", Pattern: Prefix + "/import/commit", Permission: access.PermDataWrite, Feature: extension.FeatureRegisterImport,
			API: "POST /api/v1/imports", back: fixed(Prefix + "/import"), handle: c.commitImport},
		{Method: "POST", Pattern: Prefix + "/import/{id}/rollback", Permission: access.PermDataWrite, Feature: extension.FeatureRegisterIngest,
			API: "POST /api/v1/ingestions/{id}/rollback", back: fixed(Prefix + "/import"), handle: c.rollback},
		{Method: "GET", Pattern: Prefix + "/records", Permission: read, handle: c.recordsPage},
		{Method: "GET", Pattern: Prefix + "/records/{entity}", Permission: read, handle: c.entityPage},
		{Method: "GET", Pattern: Prefix + "/records/{entity}/record", Permission: read, API: "GET /api/v1/provenance", handle: c.recordPage},
		{Method: "GET", Pattern: Prefix + "/completeness", Permission: read, API: "GET /api/v1/snapshots/{id}/completeness", handle: c.completenessPage},

		{Method: "GET", Pattern: Prefix + "/controls", Permission: read, API: "GET /api/v1/status", handle: c.controlsPage},
		{Method: "GET", Pattern: Prefix + "/controls/{catalog}/{control}", Permission: read, API: "GET /api/v1/assessments/{catalog}/{control}", handle: c.controlPage},
		{Method: "POST", Pattern: Prefix + "/controls/{catalog}/{control}/assign", Permission: access.PermWorkflowAssign, Feature: flow,
			API: "PUT /api/v1/assessments/{catalog}/{control}", back: backToControl, handle: c.assign},
		{Method: "POST", Pattern: Prefix + "/controls/{catalog}/{control}/submit", Permission: access.PermWorkflowSubmit, Feature: flow,
			API: "POST /api/v1/assessments/{catalog}/{control}/submit", back: backToControl, handle: c.act(workflow.ActionSubmit)},
		{Method: "POST", Pattern: Prefix + "/controls/{catalog}/{control}/reopen", Permission: access.PermWorkflowSubmit, Feature: flow,
			API: "POST /api/v1/assessments/{catalog}/{control}/reopen", back: backToControl, handle: c.act(workflow.ActionReopen)},
		{Method: "POST", Pattern: Prefix + "/controls/{catalog}/{control}/recommend", Permission: access.PermWorkflowReview, Feature: flow,
			API: "POST /api/v1/assessments/{catalog}/{control}/recommend", back: backToControl, handle: c.act(workflow.ActionRecommend)},
		{Method: "POST", Pattern: Prefix + "/controls/{catalog}/{control}/reject", Permission: access.PermWorkflowReview, Feature: flow,
			API: "POST /api/v1/assessments/{catalog}/{control}/reject", back: backToControl, handle: c.act(workflow.ActionReject)},
		{Method: "POST", Pattern: Prefix + "/controls/{catalog}/{control}/approve", Permission: access.PermWorkflowApprove, Feature: flow,
			API: "POST /api/v1/assessments/{catalog}/{control}/approve", back: backToControl, handle: c.act(workflow.ActionApprove)},
		{Method: "POST", Pattern: Prefix + "/controls/{catalog}/{control}/evidence", Permission: access.PermEvidenceWrite, Feature: evid,
			API: "POST /api/v1/evidence", back: backToControl, handle: c.attachEvidence},
		{Method: "POST", Pattern: Prefix + "/controls/{catalog}/{control}/evidence/link", Permission: access.PermEvidenceWrite, Feature: evid,
			API: "POST /api/v1/evidence/{id}/links", back: backToControl, handle: c.linkEvidence},
		{Method: "POST", Pattern: Prefix + "/evidence/{id}/verify", Permission: access.PermEvidenceWrite, Feature: evid,
			API: "POST /api/v1/evidence/{id}/verify", back: backField, handle: c.verifyEvidence},
		{Method: "POST", Pattern: Prefix + "/evidence/{id}/revoke", Permission: access.PermEvidenceWrite, Feature: evid,
			API: "POST /api/v1/evidence/{id}/revoke", back: backField, handle: c.revokeEvidence},

		{Method: "GET", Pattern: Prefix + "/reports", Permission: read, API: "GET /api/v1/reports", handle: c.reportsPage},
		{Method: "POST", Pattern: Prefix + "/reports", Permission: access.PermReportGenerate, API: "POST /api/v1/reports",
			back: fixed(Prefix + "/reports"), handle: c.generateReport},
		{Method: "GET", Pattern: Prefix + "/reports/{id}", Permission: read, handle: c.reportPage},
		{Method: "GET", Pattern: Prefix + "/reports/{id}/files/{name}", Permission: read, Download: true,
			API: "GET /api/v1/reports/{id}/files/{name}", handle: c.reportFile},
		{Method: "POST", Pattern: Prefix + "/reports/{id}/verify", Permission: read, API: "POST /api/v1/reports/{id}/regenerate",
			back: func(r *http.Request) string { return Prefix + "/reports/" + url.PathEscape(r.PathValue("id")) }, handle: c.verifyReport},

		{Method: "GET", Pattern: Prefix + "/settings", Permission: read, API: "GET /api/v1/settings", handle: c.settingsPage},
		{Method: "POST", Pattern: Prefix + "/settings/tokens", Permission: access.PermTokensOwn, API: "POST /api/v1/tokens",
			back: fixed(Prefix + "/settings"), handle: c.createToken},
		{Method: "POST", Pattern: Prefix + "/settings/tokens/{id}/revoke", Permission: access.PermTokensOwn, API: "DELETE /api/v1/tokens/{id}",
			back: fixed(Prefix + "/settings"), handle: c.revokeToken},
		{Method: "POST", Pattern: Prefix + "/settings/members", Permission: access.PermMembersManage, API: "PUT /api/v1/members/{email}",
			back: fixed(Prefix + "/settings"), handle: c.setMember},
		{Method: "POST", Pattern: Prefix + "/settings/members/remove", Permission: access.PermMembersManage, API: "DELETE /api/v1/members/{email}",
			back: fixed(Prefix + "/settings"), handle: c.removeMember},
		{Method: "POST", Pattern: Prefix + "/settings/retention", Permission: access.PermSettingsManage, API: "PUT /api/v1/settings",
			back: fixed(Prefix + "/settings"), handle: c.updateRetention},
		{Method: "POST", Pattern: Prefix + "/settings/overrides", Permission: access.PermSettingsManage, API: "PUT /api/v1/settings",
			back: fixed(Prefix + "/settings"), handle: c.updateOverride},
		{Method: "GET", Pattern: Prefix + "/audit", Permission: access.PermAuditRead, API: "GET /api/v1/audit", handle: c.auditPage},
	}
}

// Routes lists the console endpoints and the API routes they mirror.
func (c *Console) Routes() []Route {
	var out []Route
	for _, rt := range c.routes() {
		out = append(out, Route{Method: rt.Method, Pattern: rt.Pattern, Public: rt.Public, Permission: rt.Permission, Feature: rt.Feature,
			API: rt.API, Download: rt.Download})
	}
	return out
}

// Route describes one console endpoint (see Console.Routes).
type Route struct {
	Method, Pattern string
	Public          bool
	Permission      access.Permission
	Feature         extension.Feature
	API             string
	Download        bool
}

func fixed(path string) func(*http.Request) string { return func(*http.Request) string { return path } }

func redirectTo(path string) func(http.ResponseWriter, *http.Request, *reqCtx) error {
	return func(w http.ResponseWriter, r *http.Request, _ *reqCtx) error {
		target := path
		if r.URL.RawQuery != "" {
			target += "?" + r.URL.RawQuery
		}
		http.Redirect(w, r, target, http.StatusSeeOther)
		return nil
	}
}

// problem builds a console refusal with an API error code.
func problem(status int, code, message string) error {
	return &consoleError{httpapi.Problem{Status: status, Code: code, Message: message}}
}

type consoleError struct{ p httpapi.Problem }

func (e *consoleError) Error() string { return e.p.Message }

type problemKey struct{}

// setHeaders applies the security headers of every console response.
func setHeaders(w http.ResponseWriter) {
	h := w.Header()
	h.Set("Content-Security-Policy", "default-src 'self'; form-action 'self'; frame-ancestors 'none'; base-uri 'none'")
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Referrer-Policy", "same-origin")
	h.Set("X-Frame-Options", "DENY")
	h.Set("Cache-Control", "no-store")
}

func (c *Console) wrap(rt route) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		setHeaders(w)
		rc := &reqCtx{}
		if p, ok := r.Context().Value(problemKey{}).(*httpapi.Problem); ok {
			rc.problem = p
		}
		err := func() error {
			if r.Method == http.MethodPost {
				r.Body = http.MaxBytesReader(w, r.Body, c.opts.MaxUploadBytes)
			}
			if rt.Public {
				return rt.handle(w, r, rc)
			}
			if err := c.authenticate(r, rc); err != nil {
				clearCookie(w, sessionCookie)
				next := ""
				if r.Method == http.MethodGet {
					next = "?next=" + url.QueryEscape(r.URL.RequestURI())
				}
				http.Redirect(w, r, Prefix+"/signin"+next, http.StatusSeeOther)
				return nil
			}
			r = r.WithContext(extension.WithPrincipal(r.Context(), rc.p))
			if r.Method == http.MethodPost && rc.problem == nil {
				if err := parseForm(r); err != nil {
					return err
				}
				if !validCSRF(r.PostFormValue("csrf"), csrfToken(rc.value)) {
					return problem(http.StatusForbidden, "invalid_csrf", "The form has expired or did not come from this console. Reload the page and try again.")
				}
			}
			if !access.Allows(rc.p.Roles, rt.Permission) {
				p := httpapi.Forbidden(rt.Permission)
				return &consoleError{p}
			}
			if rt.Feature != "" {
				if d := c.eng.Entitled(r.Context(), rc.p.Scope, rt.Feature); !d.Allowed {
					return &consoleError{httpapi.Problem{Status: http.StatusForbidden, Code: "feature_not_entitled", Feature: rt.Feature,
						Message: "This workspace's license does not include " + string(rt.Feature) + "."}}
				}
			}
			return rt.handle(w, r, rc)
		}()
		if err != nil {
			c.fail(w, r, rc, rt, err)
		}
	})
}

func parseForm(r *http.Request) error {
	if strings.HasPrefix(r.Header.Get("Content-Type"), "multipart/form-data") {
		return r.ParseMultipartForm(8 << 20)
	}
	return r.ParseForm()
}

// fail answers a failed request: actions re-render their page with the
// error and the API's status; pages render the error page.
func (c *Console) fail(w http.ResponseWriter, r *http.Request, rc *reqCtx, rt route, err error) {
	p, ok := describe(err)
	if !ok {
		c.opts.Logger.Error("console request failed", "method", r.Method, "path", r.URL.Path, "error", err)
		p = httpapi.Problem{Status: http.StatusInternalServerError, Code: "internal", Message: "Something went wrong. The error has been logged."}
	}
	if p.Status == http.StatusTooManyRequests {
		w.Header().Set("Retry-After", "60")
	}
	if r.Method == http.MethodPost && rt.back != nil && rc.problem == nil && rc.value != "" {
		back := rt.back(r)
		u, perr := url.Parse(back)
		if perr == nil && strings.HasPrefix(u.Path, Prefix+"/") {
			r2 := r.Clone(context.WithValue(r.Context(), problemKey{}, &p))
			r2.Method, r2.URL, r2.RequestURI, r2.Body = http.MethodGet, u, back, http.NoBody
			r2.Form, r2.PostForm, r2.MultipartForm = nil, nil, nil
			c.mux.ServeHTTP(w, r2)
			return
		}
	}
	c.render(w, r, rc, "error", "Error", p.Status, &p)
}

func describe(err error) (httpapi.Problem, bool) {
	var ce *consoleError
	if errors.As(err, &ce) {
		return ce.p, true
	}
	var mbe *http.MaxBytesError
	if errors.As(err, &mbe) {
		return httpapi.Problem{Status: http.StatusRequestEntityTooLarge, Code: "payload_too_large", Message: "The upload is larger than this server accepts."}, true
	}
	return httpapi.Describe(err)
}

// authenticate resolves the session cookie into rc.
func (c *Console) authenticate(r *http.Request, rc *reqCtx) error {
	ck, err := r.Cookie(sessionCookie)
	if err != nil {
		return identity.ErrSessionEnded
	}
	p, se, err := c.ids.SessionPrincipal(r.Context(), ck.Value)
	if err != nil {
		return err
	}
	rc.p, rc.session, rc.value = p, se, ck.Value
	if rc.workspaces, err = c.ids.Workspaces(r.Context(), se); err != nil {
		return err
	}
	for _, m := range rc.workspaces {
		if m.Scope == p.Scope {
			rc.email = m.Email
		}
	}
	set, err := c.eng.Settings(r.Context(), p.Scope)
	if err != nil {
		return err
	}
	rc.sample = set.Sample
	return nil
}

func validCSRF(got, want string) bool {
	return got != "" && subtle.ConstantTimeCompare([]byte(got), []byte(want)) == 1
}

func (c *Console) serveStatic(w http.ResponseWriter, r *http.Request, _ *reqCtx) error {
	data, err := fs.ReadFile(c.static, r.PathValue("file"))
	if err != nil {
		return problem(http.StatusNotFound, "not_found", "There is no such file.")
	}
	w.Header().Set("Content-Type", "text/css; charset=utf-8")
	w.Header().Set("Cache-Control", "public, max-age=3600")
	_, _ = w.Write(data)
	return nil
}
