// SPDX-License-Identifier: Apache-2.0

// Package httpapi serves the engine's versioned REST API under /api/v1 as a
// plain http.Handler that any host can mount. The route table is the single
// source for routing, authentication, entitlement warnings and the OpenAPI
// drift test.
package httpapi

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/nexops-one/compliance-engine/pkg/access"
	"github.com/nexops-one/compliance-engine/pkg/compliance"
	"github.com/nexops-one/compliance-engine/pkg/extension"
	"github.com/nexops-one/compliance-engine/pkg/identity"
	"github.com/nexops-one/compliance-engine/pkg/workflow"
)

// DefaultMaxBodyBytes bounds request bodies when Options.MaxBodyBytes is unset.
const DefaultMaxBodyBytes = 32 << 20

// Options configures the handler. Authenticator is required.
type Options struct {
	Authenticator extension.Authenticator
	MaxBodyBytes  int64
	Version       string
	Edition       string // default "open-core"
	Logger        *slog.Logger
	// Identity serves the member and token endpoints; nil answers them with
	// 501 identity_not_configured (hosts that manage identities themselves).
	Identity  *identity.Service
	RateLimit RateLimit
	// EncryptionAtRest is reported by /api/v1/about.
	EncryptionAtRest bool
	// Failures, when set, is the failed-authentication budget shared with
	// other front ends; otherwise one is made from RateLimit.AuthFailures.
	Failures *FailureLimiter
	// ExtraRoutes are an edition's additional routes, authenticated, rate
	// limited and permission-checked like the built-in ones.
	ExtraRoutes []ExtraRoute
	// About adds sections to GET /api/v1/about.
	About []extension.AboutSection
}

// HandlerFunc serves an authenticated request. A returned error is answered
// in the API's error shape (see NewProblem).
type HandlerFunc func(w http.ResponseWriter, r *http.Request, p extension.Principal) error

// ExtraRoute is an edition's additional endpoint.
type ExtraRoute struct {
	Route
	Handle HandlerFunc
}

// Route describes one endpoint.
type Route struct {
	Method     string
	Pattern    string
	Public     bool              // no authentication
	Feature    extension.Feature // entitlement the endpoint exercises; "" for reads
	Permission access.Permission // role permission required; "" only for public routes
}

type routeDef struct {
	Route
	handle HandlerFunc
}

type server struct {
	eng      *compliance.Engine
	opts     Options
	actors   *limiter // requests per authenticated actor
	failures *limiter // failed authentications per client IP
}

// New returns the API handler.
func New(eng *compliance.Engine, opts Options) http.Handler {
	if opts.MaxBodyBytes <= 0 {
		opts.MaxBodyBytes = DefaultMaxBodyBytes
	}
	if opts.Edition == "" {
		opts.Edition = "open-core"
	}
	if opts.Logger == nil {
		opts.Logger = slog.Default()
	}
	s := &server{eng: eng, opts: opts,
		actors:   newLimiter(opts.RateLimit.PerMinute, opts.RateLimit.Burst),
		failures: newLimiter(opts.RateLimit.AuthFailures, opts.RateLimit.AuthFailures)}
	if opts.Failures != nil {
		s.failures = opts.Failures.l
	}
	mux := http.NewServeMux()
	seen := map[string]bool{}
	for _, rd := range s.routes() {
		seen[rd.Method+" "+rd.Pattern] = true
		mux.Handle(rd.Method+" "+rd.Pattern, s.wrap(rd))
	}
	for _, er := range opts.ExtraRoutes {
		key := er.Method + " " + er.Pattern
		if seen[key] || er.Handle == nil || (!er.Public && er.Permission == "") {
			panic(fmt.Sprintf("httpapi: invalid or duplicate extra route %s", key))
		}
		seen[key] = true
		mux.Handle(key, s.wrap(routeDef{Route: er.Route, handle: er.Handle}))
	}
	mux.Handle("/", s.wrap(routeDef{Route: Route{Public: true}, handle: func(http.ResponseWriter, *http.Request, extension.Principal) error {
		return newError(http.StatusNotFound, "not_found", "no such endpoint")
	}}))
	return mux
}

// Routes lists every endpoint of the API.
func Routes() []Route {
	var s *server
	defs := s.routes()
	out := make([]Route, len(defs))
	for i, d := range defs {
		out[i] = d.Route
	}
	return out
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}

func (s *server) wrap(rd routeDef) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		var p extension.Principal
		err := func() error {
			if !rd.Public {
				ip := clientIP(r)
				if wait, blocked := s.failures.blocked(ip); blocked {
					return rateLimited(wait)
				}
				var err error
				if p, err = s.opts.Authenticator.Authenticate(r); err != nil {
					s.failures.take(ip)
					return errUnauthorized
				}
				if ok, wait := s.actors.take(p.Actor); !ok {
					return rateLimited(wait)
				}
				if !access.Allows(p.Roles, rd.Permission) {
					return forbidden(rd.Permission)
				}
				r = r.WithContext(extension.WithPrincipal(r.Context(), p))
				if rd.Feature != "" {
					s.graceWarning(rec, r, p, rd.Feature)
				}
			}
			r.Body = http.MaxBytesReader(rec, r.Body, s.opts.MaxBodyBytes)
			return rd.handle(rec, r, p)
		}()
		if err != nil {
			s.writeError(rec, err)
		}
		s.opts.Logger.Info("request", "method", r.Method, "path", r.URL.Path, "status", rec.status,
			"duration_ms", time.Since(start).Milliseconds(), "actor", p.Actor, "tenant", p.Scope.TenantID, "workspace", p.Scope.WorkspaceID)
	})
}

func (s *server) graceWarning(w http.ResponseWriter, r *http.Request, p extension.Principal, f extension.Feature) {
	d := s.eng.Entitled(r.Context(), p.Scope, f)
	if d.Allowed && d.GraceUntil != nil {
		until := d.GraceUntil.UTC().Format(time.RFC3339)
		w.Header().Set("Warning", fmt.Sprintf(`299 compliance-engine "entitlement for %s has expired; grace period ends %s"`, f, until))
		w.Header().Set("Compliance-License-Warning", fmt.Sprintf("%s expired; grace period ends %s", f, until))
	}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(v)
}

func (s *server) routes() []routeDef {
	const (
		read   = access.PermDataRead
		write  = access.PermDataWrite
		ingest = extension.FeatureRegisterIngest
		evid   = extension.FeatureEvidenceManage
		flow   = extension.FeatureWorkflowBasic
	)
	return []routeDef{
		{Route{"GET", "/healthz", true, "", ""}, s.healthz},
		{Route{"GET", "/api/v1/openapi.yaml", true, "", ""}, s.openAPI},
		{Route{"GET", "/api/v1/about", false, "", read}, s.about},
		{Route{"GET", "/api/v1/me", false, "", read}, s.me},
		{Route{"PUT", "/api/v1/adapters/{name}/manifest", false, ingest, write}, s.putManifest},
		{Route{"POST", "/api/v1/ingestions", false, ingest, write}, s.postIngestion},
		{Route{"GET", "/api/v1/ingestions/{id}", false, "", read}, s.getIngestion},
		{Route{"POST", "/api/v1/ingestions/{id}/rollback", false, ingest, write}, s.rollback},
		{Route{"POST", "/api/v1/imports", false, extension.FeatureRegisterImport, write}, s.postImport},
		{Route{"GET", "/api/v1/templates.xlsx", false, "", read}, s.templateWorkbook},
		{Route{"GET", "/api/v1/templates/{file}", false, "", read}, s.templateCSV},
		{Route{"GET", "/api/v1/snapshots", false, "", read}, s.listSnapshots},
		{Route{"GET", "/api/v1/snapshots/{id}", false, "", read}, s.getSnapshot},
		{Route{"GET", "/api/v1/snapshots/{id}/records/{entity}", false, "", read}, s.getRecords},
		{Route{"GET", "/api/v1/snapshots/{id}/completeness", false, "", read}, s.getCompleteness},
		{Route{"GET", "/api/v1/provenance", false, "", read}, s.getProvenance},
		{Route{"GET", "/api/v1/catalogs", false, "", read}, s.listCatalogs},
		{Route{"GET", "/api/v1/catalogs/diff", false, "", read}, s.diffCatalogs},
		{Route{"GET", "/api/v1/catalogs/{catalog}/{version}", false, "", read}, s.getCatalog},
		{Route{"POST", "/api/v1/evaluations", false, extension.FeatureEvaluationRun, access.PermEvaluationRun}, s.postEvaluation},
		{Route{"GET", "/api/v1/evaluations/{id}", false, "", read}, s.getEvaluation},
		{Route{"GET", "/api/v1/report-profiles", false, "", read}, s.listReportProfiles},
		{Route{"POST", "/api/v1/reports", false, "", access.PermReportGenerate}, s.postReport},
		{Route{"GET", "/api/v1/reports", false, "", read}, s.listReports},
		{Route{"GET", "/api/v1/reports/{id}", false, "", read}, s.getReport},
		{Route{"GET", "/api/v1/reports/{id}/files/{name}", false, "", read}, s.getReportFile},
		{Route{"POST", "/api/v1/reports/{id}/regenerate", false, "", read}, s.postReportRegenerate},
		{Route{"GET", "/api/v1/members", false, "", access.PermMembersManage}, s.listMembers},
		{Route{"PUT", "/api/v1/members/{email}", false, "", access.PermMembersManage}, s.putMember},
		{Route{"DELETE", "/api/v1/members/{email}", false, "", access.PermMembersManage}, s.deleteMember},
		{Route{"GET", "/api/v1/tokens", false, "", access.PermTokensOwn}, s.listTokens},
		{Route{"POST", "/api/v1/tokens", false, "", access.PermTokensOwn}, s.postToken},
		{Route{"DELETE", "/api/v1/tokens/{id}", false, "", access.PermTokensOwn}, s.deleteToken},
		{Route{"GET", "/api/v1/audit", false, "", access.PermAuditRead}, s.getAudit},
		{Route{"POST", "/api/v1/evidence", false, evid, access.PermEvidenceWrite}, s.postEvidence},
		{Route{"POST", "/api/v1/evidence/upload", false, evid, access.PermEvidenceWrite}, s.postEvidenceUpload},
		{Route{"GET", "/api/v1/evidence/{id}/content", false, "", read}, s.getEvidenceContent},
		{Route{"GET", "/api/v1/evidence", false, "", read}, s.listEvidence},
		{Route{"GET", "/api/v1/evidence/{id}", false, "", read}, s.getEvidence},
		{Route{"POST", "/api/v1/evidence/{id}/links", false, evid, access.PermEvidenceWrite}, s.postEvidenceLink},
		{Route{"DELETE", "/api/v1/evidence/{id}/links/{catalog}/{control}", false, evid, access.PermEvidenceWrite}, s.deleteEvidenceLink},
		{Route{"POST", "/api/v1/evidence/{id}/revoke", false, evid, access.PermEvidenceWrite}, s.postEvidenceRevoke},
		{Route{"POST", "/api/v1/evidence/{id}/verify", false, evid, access.PermEvidenceWrite}, s.postEvidenceVerify},
		{Route{"POST", "/api/v1/evidence/{id}/checks", false, evid, access.PermEvidenceWrite}, s.postEvidenceCheck},
		{Route{"GET", "/api/v1/assessments", false, "", read}, s.listAssessments},
		{Route{"GET", "/api/v1/assessments/{catalog}/{control}", false, "", read}, s.getAssessment},
		{Route{"PUT", "/api/v1/assessments/{catalog}/{control}", false, flow, access.PermWorkflowAssign}, s.putAssessment},
		{Route{"POST", "/api/v1/assessments/{catalog}/{control}/submit", false, flow, access.PermWorkflowSubmit}, s.act(workflow.ActionSubmit)},
		{Route{"POST", "/api/v1/assessments/{catalog}/{control}/reopen", false, flow, access.PermWorkflowSubmit}, s.act(workflow.ActionReopen)},
		{Route{"POST", "/api/v1/assessments/{catalog}/{control}/recommend", false, flow, access.PermWorkflowReview}, s.act(workflow.ActionRecommend)},
		{Route{"POST", "/api/v1/assessments/{catalog}/{control}/reject", false, flow, access.PermWorkflowReview}, s.act(workflow.ActionReject)},
		{Route{"POST", "/api/v1/assessments/{catalog}/{control}/approve", false, flow, access.PermWorkflowApprove}, s.act(workflow.ActionApprove)},
		{Route{"GET", "/api/v1/status", false, "", read}, s.getStatus},
		{Route{"GET", "/api/v1/settings", false, "", read}, s.getSettings},
		{Route{"PUT", "/api/v1/settings", false, "", access.PermSettingsManage}, s.putSettings},
	}
}
