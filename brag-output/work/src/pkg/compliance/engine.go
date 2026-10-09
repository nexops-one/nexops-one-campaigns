// SPDX-License-Identifier: Apache-2.0

// Package compliance is the embeddable library facade of the compliance
// engine. A host creates one Engine and calls it with a Scope per request;
// the Scope always comes from the host or its authenticator, never from data.
package compliance

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/nexops-one/compliance-engine/pkg/canonical"
	"github.com/nexops-one/compliance-engine/pkg/catalog"
	"github.com/nexops-one/compliance-engine/pkg/evidence"
	"github.com/nexops-one/compliance-engine/pkg/extension"
	"github.com/nexops-one/compliance-engine/pkg/store"
	schemadata "github.com/nexops-one/compliance-engine/schema"
	"github.com/nexops-one/compliance-engine/sdk/adapter"
	"github.com/nexops-one/compliance-engine/sdk/schema"
)

// Scope identifies a tenant workspace.
type Scope = adapter.Scope

// DefaultMaxRecordsPerBatch is the default ingestion size limit.
const DefaultMaxRecordsPerBatch = 50000

// Limits bounds ingestion.
type Limits struct {
	MaxRecordsPerBatch int
}

// Config configures an Engine. Only Store is required.
type Config struct {
	Store            store.Store
	Schemas          *schema.Registry           // default: schemas embedded in the SDK
	Catalogs         catalog.Source             // default: catalog.Embedded(); replaces, does not add
	Codelists        *canonical.Codelists       // default: embedded codelists of the latest schema
	Entitlements     extension.Entitlements     // default: extension.AllowOpen{}
	IdentifierPolicy canonical.IdentifierPolicy // default: canonical.PolicyWarn
	Limits           Limits
	Clock            func() time.Time
	NewID            func(prefix string) string
	Workflow         WorkflowOptions
	// Hasher returns the record content hash function of a scope; nil hashes
	// with plain SHA-256. With encryption at rest it returns a tenant HMAC.
	Hasher    func(ctx context.Context, scope Scope) (func(adapter.Record) string, error)
	Retention RetentionOptions
	// Version is the engine version recorded in reports (default "dev").
	Version string
	// Extensions add report profiles besides the built-in Profile B.
	Extensions Extensions
}

// RetentionOptions holds retention floors and hooks.
type RetentionOptions struct {
	// AuditDays keeps audit events at least this many days (0: forever).
	AuditDays int
}

// WorkflowOptions configures the review workflow and evidence verification.
type WorkflowOptions struct {
	// AllowSelfApproval lets a control's owner approve it (flagged in history and audit).
	AllowSelfApproval bool
	// Verifier reads evidence objects the engine may hash; the zero value reads nothing.
	Verifier evidence.Verifier
	// Managed stores uploaded evidence objects encrypted; nil disables uploads.
	Managed *evidence.Managed
	// AllowEvidenceWithoutChecksum accepts evidence references without a
	// checksum (integrity no_checksum). They never satisfy an evidence
	// requirement until an attested check supplies the checksum.
	AllowEvidenceWithoutChecksum bool
}

var (
	ErrUnknownAdapter    = errors.New("adapter manifest is not registered for this workspace")
	ErrModeNotSupported  = errors.New("batch mode is not declared in the adapter manifest")
	ErrTooLarge          = errors.New("batch exceeds the configured record limit")
	ErrNothingToRollBack = errors.New("ingestion did not change the workspace")
	ErrAlreadyRolledBack = errors.New("ingestion was already rolled back")
	ErrInvalidManifest   = errors.New("invalid adapter manifest")
)

// BatchError reports envelope-level problems that reject a whole batch.
type BatchError struct {
	Errors []schema.FieldError `json:"errors"`
}

func (e *BatchError) Error() string {
	return fmt.Sprintf("invalid batch envelope: %d error(s), first: %s %s", len(e.Errors), e.Errors[0].Field, e.Errors[0].Code)
}

// Engine is the compliance engine. It is safe for concurrent use.
type Engine struct {
	store     store.Store
	schemas   *schema.Registry
	catalogs  *catalog.Set
	codelists *canonical.Codelists
	ents      extension.Entitlements
	policy    canonical.IdentifierPolicy
	limits    Limits
	now       func() time.Time
	newID     func(string) string
	workflow  WorkflowOptions
	hasher    func(context.Context, Scope) (func(adapter.Record) string, error)
	retention RetentionOptions
	version   string
	profiles  map[string]extension.ReportProfile
	flow      extension.WorkflowPolicy
}

// New builds an Engine, loading and validating catalogs and codelists.
func New(ctx context.Context, cfg Config) (*Engine, error) {
	if cfg.Store == nil {
		return nil, errors.New("compliance: Config.Store is required")
	}
	e := &Engine{
		store: cfg.Store, schemas: cfg.Schemas, codelists: cfg.Codelists, ents: cfg.Entitlements,
		policy: cfg.IdentifierPolicy, limits: cfg.Limits, now: cfg.Clock, newID: cfg.NewID, workflow: cfg.Workflow, hasher: cfg.Hasher, retention: cfg.Retention,
		version: cfg.Version,
	}
	if e.version == "" {
		e.version = "dev"
	}
	var err error
	if e.profiles, err = registerProfiles(cfg.Extensions.ReportProfiles); err != nil {
		return nil, err
	}
	if e.flow = cfg.Extensions.WorkflowPolicy; e.flow == nil {
		e.flow = extension.BasicWorkflow{}
	}
	switch e.policy {
	case "":
		e.policy = canonical.PolicyWarn
	case canonical.PolicyWarn, canonical.PolicyReject:
	default:
		return nil, fmt.Errorf("compliance: unknown identifier policy %q", e.policy)
	}
	if e.schemas == nil {
		reg, err := schema.Default()
		if err != nil {
			return nil, fmt.Errorf("compliance: load schemas: %w", err)
		}
		e.schemas = reg
	}
	src := cfg.Catalogs
	if src == nil {
		src = catalog.Embedded()
	}
	cs, err := src.Catalogs(ctx)
	if err != nil {
		return nil, fmt.Errorf("compliance: load catalogs: %w", err)
	}
	if e.catalogs, err = catalog.NewSet(e.schemas, cs); err != nil {
		return nil, fmt.Errorf("compliance: %w", err)
	}
	if e.codelists == nil {
		dir := "v" + e.schemas.Latest().Version + "/codelists"
		if e.codelists, err = canonical.LoadCodelists(schemadata.Codelists, dir); err != nil {
			return nil, fmt.Errorf("compliance: load codelists: %w", err)
		}
	}
	if e.ents == nil {
		e.ents = extension.AllowOpen{}
	}
	if e.limits.MaxRecordsPerBatch <= 0 {
		e.limits.MaxRecordsPerBatch = DefaultMaxRecordsPerBatch
	}
	if e.now == nil {
		e.now = func() time.Time { return time.Now().UTC() }
	}
	if e.newID == nil {
		e.newID = randomID
	}
	return e, nil
}

func randomID(prefix string) string {
	var b [8]byte
	_, _ = rand.Read(b[:])
	return prefix + "-" + hex.EncodeToString(b[:])
}

// Entitled returns the entitlement decision for a feature so hosts can
// surface grace periods. It never blocks anything by itself.
func (e *Engine) Entitled(ctx context.Context, scope Scope, f extension.Feature) extension.Decision {
	return e.ents.Allowed(ctx, scope, f)
}

// SchemaVersions lists the canonical schema versions this engine accepts.
func (e *Engine) SchemaVersions() []string { return e.schemas.Versions() }

// Schema returns the latest canonical schema, used to describe records.
func (e *Engine) Schema() *schema.Schema { return e.schemas.Latest() }

// Catalogs lists the loaded catalog versions.
func (e *Engine) Catalogs() []catalog.Ref { return e.catalogs.Refs() }

func (e *Engine) currentRevision(ctx context.Context, scope Scope) (int64, error) {
	rev, err := e.store.CurrentRevision(ctx, scope)
	if errors.Is(err, store.ErrNotFound) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	return rev.Number, nil
}

func mustJSON(v any) json.RawMessage {
	data, err := json.Marshal(v)
	if err != nil {
		panic(fmt.Sprintf("compliance: encode %T: %v", v, err)) // only plain data structs are encoded
	}
	return data
}
