// SPDX-License-Identifier: Apache-2.0

// Package extension defines the public extension points through which other
// editions (for example the commercial enterprise edition or a host product)
// add catalogs, authentication, adapters, report profiles and entitlements
// without modifying the open-core engine. The open core contains no license
// checking: AllowOpen allows every open feature and denies every commercial one.
package extension

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/nexops-one/compliance-engine/pkg/access"
	"github.com/nexops-one/compliance-engine/pkg/canonical"
	"github.com/nexops-one/compliance-engine/pkg/catalog"
	"github.com/nexops-one/compliance-engine/pkg/engine"
	"github.com/nexops-one/compliance-engine/pkg/store"
	"github.com/nexops-one/compliance-engine/pkg/workflow"
	"github.com/nexops-one/compliance-engine/sdk/adapter"
	"github.com/nexops-one/compliance-engine/sdk/schema"
)

// Feature is a stable identifier of an entitlement-controlled capability.
type Feature string

// Open-core features.
const (
	FeatureRegisterIngest Feature = "register.ingest"
	FeatureRegisterImport Feature = "register.import"
	FeatureEvaluationRun  Feature = "evaluation.run"
	FeatureCatalogBasic   Feature = "catalog.basic"
	FeatureEvidenceManage Feature = "evidence.manage"
	FeatureWorkflowBasic  Feature = "workflow.basic"
	FeatureReportProfileB Feature = "report.profile_b"
)

// Commercial features.
const (
	FeatureCatalogMaintained Feature = "catalog.maintained"
	FeatureReportProfileA    Feature = "report.profile_a"
	FeatureAdapterManaged    Feature = "adapter.managed"
	FeatureAuthSSO           Feature = "auth.sso"
	FeatureWorkflowAdvanced  Feature = "workflow.advanced"
	FeatureTenancyMulti      Feature = "tenancy.multi"
)

var openFeatures = map[Feature]bool{
	FeatureRegisterIngest: true, FeatureRegisterImport: true, FeatureEvaluationRun: true, FeatureCatalogBasic: true, FeatureEvidenceManage: true, FeatureWorkflowBasic: true,
	FeatureReportProfileB: true,
}

// IsOpen reports whether f belongs to the open core.
func IsOpen(f Feature) bool { return openFeatures[f] }

// Reason explains a denial, or a grace period when Allowed is true.
type Reason string

const (
	ReasonNotLicensed   Reason = "not_licensed"
	ReasonExpired       Reason = "expired"
	ReasonAddonDisabled Reason = "addon_disabled"
)

// Decision is an entitlement decision. Allowed with a GraceUntil means the
// entitlement has expired and the feature works until that time.
type Decision struct {
	Allowed    bool       `json:"allowed"`
	Reason     Reason     `json:"reason,omitempty"`
	GraceUntil *time.Time `json:"grace_until,omitempty"`
}

// Entitlements decides which features a workspace may use. Reading stored
// data must never be gated by an Entitlements implementation.
type Entitlements interface {
	Allowed(ctx context.Context, scope adapter.Scope, f Feature) Decision
}

// AllowOpen is the open-core entitlements provider.
type AllowOpen struct{}

// Allowed allows open features and denies commercial ones.
func (AllowOpen) Allowed(_ context.Context, _ adapter.Scope, f Feature) Decision {
	if IsOpen(f) {
		return Decision{Allowed: true}
	}
	return Decision{Reason: ReasonNotLicensed}
}

// NotEntitledError is returned when a feature is refused.
type NotEntitledError struct {
	Feature Feature `json:"feature"`
	Reason  Reason  `json:"reason"`
}

func (e *NotEntitledError) Error() string {
	return fmt.Sprintf("feature %s is not entitled: %s", e.Feature, e.Reason)
}

// Require returns the decision, and a *NotEntitledError when it is a denial.
func Require(ctx context.Context, e Entitlements, scope adapter.Scope, f Feature) (Decision, error) {
	d := e.Allowed(ctx, scope, f)
	if !d.Allowed {
		return d, &NotEntitledError{Feature: f, Reason: d.Reason}
	}
	return d, nil
}

// CatalogSource supplies additional catalogs (for example maintained catalogs).
type CatalogSource = catalog.Source

// ActorKind says what kind of caller a principal is.
type ActorKind string

const (
	ActorUser   ActorKind = "user"   // a person (local account, SSO, or a host product's user)
	ActorToken  ActorKind = "token"  // a service or bootstrap token not tied to a person
	ActorSystem ActorKind = "system" // the engine itself, the CLI operator, or a library caller
)

// Principal is an authenticated caller bound to one workspace. Actor is a
// stable identifier recorded in the audit log; Roles are the caller's
// workspace roles, checked against the access matrix.
type Principal struct {
	Scope adapter.Scope
	Actor string
	Kind  ActorKind
	Roles []access.Role
}

type principalKey struct{}

// WithPrincipal returns ctx carrying p, so the engine can attribute actions.
func WithPrincipal(ctx context.Context, p Principal) context.Context {
	return context.WithValue(ctx, principalKey{}, p)
}

// PrincipalFrom returns the principal carried by ctx.
func PrincipalFrom(ctx context.Context) (Principal, bool) {
	p, ok := ctx.Value(principalKey{}).(Principal)
	return p, ok
}

// Authenticator resolves the caller of an HTTP request.
type Authenticator interface {
	Authenticate(r *http.Request) (Principal, error)
}

// AdapterFactory builds a registered adapter from configuration.
type AdapterFactory interface {
	Manifest() adapter.Manifest
	Feature() Feature
	New(config json.RawMessage) (adapter.Adapter, error)
}

// ReportProfile is a report generator (SPEC-GOV-005). Generate must be a pure
// function of its input: the engine regenerates reports to prove they are
// reproducible, comparing the facts hash and every file hash.
type ReportProfile interface {
	ID() string
	// Feature is the entitlement generation requires. Reading a stored report
	// is never gated.
	Feature() Feature
	// Formats lists the renderings the profile offers besides its JSON facts,
	// for example "pdf" or "xbrl-csv".
	Formats() []string
	Generate(ctx context.Context, in ReportInput) (ReportOutput, error)
}

// ReportMeta identifies a report and how it was produced. Every profile embeds
// it in its facts.
type ReportMeta struct {
	ReportID      string        `json:"report_id"`
	Profile       string        `json:"profile"`
	Scope         adapter.Scope `json:"scope"`
	EngineVersion string        `json:"engine_version"`
	SchemaVersion string        `json:"schema_version"`
	SnapshotID    string        `json:"snapshot_id"`
	EvaluationID  string        `json:"evaluation_id"`
	Catalogs      []catalog.Ref `json:"catalogs"`
	AsOf          time.Time     `json:"as_of"`
	GeneratedAt   time.Time     `json:"generated_at"`
	GeneratedBy   string        `json:"generated_by"`
	GeneratorKind ActorKind     `json:"generator_kind"`
	// Sample marks a workspace holding fictitious demonstration data; profiles
	// must label such reports and must not offer a way to drop the label.
	Sample bool `json:"sample"`
}

// ReportEvidence is a linked evidence item as reports may show it: the
// location is reduced to scheme and host, and reasons are left out.
type ReportEvidence struct {
	ID          string              `json:"id"`
	Title       string              `json:"title"`
	Kind        string              `json:"kind"`
	Source      string              `json:"source"`
	Location    string              `json:"location"`
	Checksum    string              `json:"checksum"`
	Integrity   store.Integrity     `json:"integrity"`
	CheckMethod string              `json:"check_method,omitempty"` // method of the latest check: engine_file, engine_https, attested
	CollectedAt time.Time           `json:"collected_at"`
	ValidUntil  *time.Time          `json:"valid_until,omitempty"`
	RevokedAt   *time.Time          `json:"revoked_at,omitempty"`
	Links       []store.ControlRef  `json:"links"`
	State       store.EvidenceState `json:"state"` // at Meta.AsOf
}

// ReportInput is everything a profile may use. Records hold decrypted values:
// a profile decides which of them, if any, it exports.
type ReportInput struct {
	Meta         ReportMeta
	Schema       *schema.Schema
	Catalogs     []*catalog.Catalog
	Records      []store.RecordVersion
	Computed     engine.Result
	Effective    workflow.Result
	Completeness canonical.Completeness
	Evidence     []ReportEvidence
	// People maps actor IDs (for example "user:u-1") to member emails.
	People map[string]string
	// Formats are the renderings requested; empty means every format.
	Formats []string
	// Parameters are the request's report parameters (see
	// ParameterizedProfile), already checked against the declared specs.
	Parameters map[string]string
	// AllowIncomplete asks the profile to produce an export marked
	// incomplete instead of refusing it with blocking findings.
	AllowIncomplete bool
}

// ParameterSpec declares one report parameter a profile accepts.
type ParameterSpec struct {
	Name        string `json:"name"`
	Label       string `json:"label"`
	Description string `json:"description,omitempty"`
	// Required parameters have no default the profile can derive.
	Required bool `json:"required,omitempty"`
	// Pattern is a regular expression a value must match entirely.
	Pattern string `json:"pattern,omitempty"`
}

// ParameterizedProfile is a ReportProfile that accepts parameters. A
// profile that does not implement it accepts none.
type ParameterizedProfile interface {
	Parameters() []ParameterSpec
}

// ErrInvalidParameter is wrapped by a profile refusing a parameter value
// (for example a value its data contradicts); the API answers 422
// invalid_parameter.
var ErrInvalidParameter = errors.New("invalid report parameter")

// ReportFile is one rendering of a report.
type ReportFile struct {
	Name        string
	ContentType string
	Data        []byte
}

// Finding is one problem a profile's validation found.
type Finding struct {
	Severity string `json:"severity"` // error or warning
	Code     string `json:"code"`
	Message  string `json:"message"`
	Template string `json:"template,omitempty"`
	Row      string `json:"row,omitempty"`
	Field    string `json:"field,omitempty"`
}

// ReportOutput is what a profile produced. When Complete is false and
// Blocking is not empty, the engine refuses to store the report.
type ReportOutput struct {
	Facts      json.RawMessage
	Validation json.RawMessage
	Files      []ReportFile
	Complete   bool
	Blocking   []Finding
}

// AboutSection adds a named section to GET /api/v1/about, for example the
// license state of a commercial edition. About must not fail: a section that
// cannot be computed reports why in its value.
type AboutSection interface {
	AboutKey() string
	About(ctx context.Context, scope adapter.Scope) any
}

// WorkspaceLimiter is implemented by an Entitlements provider that bounds the
// number of active workspaces of the deployment (for example a commercial
// license). WorkspaceLimit returns 0 when there is no limit. The engine
// enforces it when a workspace is registered or resumed.
type WorkspaceLimiter interface {
	WorkspaceLimit(ctx context.Context) int
}

// ApprovalRequirements are what approving one control needs.
type ApprovalRequirements = workflow.Requirements

// WorkflowPolicy decides, per control, what an approval needs: how many
// distinct approvers, whether a reviewer must recommend first, and whether
// the submitter may approve. It is asked at each approval.
type WorkflowPolicy interface {
	Requirements(ctx context.Context, scope adapter.Scope, catalog, controlID string) ApprovalRequirements
}

// BasicWorkflow is the open-core policy: a single approver, who is not the
// control owner unless self-approval is allowed by the deployment.
type BasicWorkflow struct{}

// Requirements returns the single-approver requirements.
func (BasicWorkflow) Requirements(context.Context, adapter.Scope, string, string) ApprovalRequirements {
	return ApprovalRequirements{MinApprovers: 1}
}
