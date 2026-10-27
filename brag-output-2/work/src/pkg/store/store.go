// SPDX-License-Identifier: Apache-2.0

// Package store defines persistence for the engine: immutable workspace
// revisions with copy-on-write record versions, the provenance log, adapter
// manifests and evaluations. Every method is scoped to one tenant workspace.
package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/nexops-one/compliance-engine/pkg/access"
	"github.com/nexops-one/compliance-engine/sdk/adapter"
)

var (
	ErrExists   = errors.New("already exists")
	ErrNotFound = errors.New("not found")
	ErrConflict = errors.New("workspace changed concurrently; retry against the current revision")
)

// SnapshotID returns the snapshot identifier of revision n ("rev-0" is empty).
func SnapshotID(n int64) string { return "rev-" + strconv.FormatInt(n, 10) }

// ParseSnapshotID parses "rev-N".
func ParseSnapshotID(s string) (int64, error) {
	num, ok := strings.CutPrefix(s, "rev-")
	n, err := strconv.ParseInt(num, 10, 64)
	if !ok || err != nil || n < 0 {
		return 0, fmt.Errorf("%w: snapshot %q", ErrNotFound, s)
	}
	return n, nil
}

// Op is a change operation.
type Op string

const (
	OpCreate Op = "create"
	OpUpdate Op = "update"
	OpDelete Op = "delete"
)

// Kind is what produced a revision.
type Kind string

const (
	KindIngestion Kind = "ingestion"
	KindRollback  Kind = "rollback"
)

// RecordVersion is one immutable version of a record.
type RecordVersion struct {
	Entity          string         `json:"entity"`
	Key             string         `json:"key"`
	Data            adapter.Record `json:"data"`
	Hash            string         `json:"hash"`
	SchemaVersion   string         `json:"schema_version"`
	Source          adapter.Source `json:"source"`
	IngestionID     string         `json:"ingestion_id"`
	SourceRecordRef string         `json:"source_record_ref,omitempty"`
	WrittenAt       time.Time      `json:"written_at"`
}

// Change is one record change in a commit. Version is nil for deletes.
type Change struct {
	Op           Op             `json:"op"`
	Entity       string         `json:"entity"`
	Key          string         `json:"key"`
	Version      *RecordVersion `json:"version,omitempty"`
	PreviousHash string         `json:"previous_hash,omitempty"`
}

// Revision is one immutable workspace state.
type Revision struct {
	Scope       adapter.Scope `json:"scope"`
	Number      int64         `json:"number"`
	SnapshotID  string        `json:"snapshot_id"`
	IngestionID string        `json:"ingestion_id"`
	Kind        Kind          `json:"kind"`
	CreatedAt   time.Time     `json:"created_at"`
}

// ProvenanceEntry records one change for audit.
type ProvenanceEntry struct {
	Seq             int64          `json:"seq"`
	IngestionID     string         `json:"ingestion_id"`
	Revision        int64          `json:"revision"`
	Entity          string         `json:"entity"`
	Key             string         `json:"key"`
	Op              Op             `json:"op"`
	Source          adapter.Source `json:"source"`
	SourceRecordRef string         `json:"source_record_ref,omitempty"`
	ContentHash     string         `json:"content_hash,omitempty"`
	PreviousHash    string         `json:"previous_hash,omitempty"`
	RecordedAt      time.Time      `json:"recorded_at"`
}

// Ingestion is the stored outcome of one ingestion or rollback. RevisionAfter
// is 0 when nothing was committed.
type Ingestion struct {
	ID             string          `json:"id"`
	Scope          adapter.Scope   `json:"scope"`
	BatchID        string          `json:"batch_id,omitempty"`
	Source         adapter.Source  `json:"source"`
	Mode           adapter.Mode    `json:"mode,omitempty"`
	Result         json.RawMessage `json:"result"`
	RevisionBefore int64           `json:"revision_before"`
	RevisionAfter  int64           `json:"revision_after"`
	RolledBackBy   string          `json:"rolled_back_by,omitempty"`
	CreatedAt      time.Time       `json:"created_at"`
}

// Commit atomically creates the next revision.
type Commit struct {
	Scope            adapter.Scope
	ExpectedRevision int64 // current revision the changes were computed against (0 = none)
	Ingestion        Ingestion
	Changes          []Change
	Kind             Kind
	RollsBack        []string // ingestion IDs this commit reverts; each is marked RolledBackBy
	At               time.Time
	Audit            []AuditEvent // appended in the same transaction; a failed append aborts the commit
}

// StoredEvaluation is a persisted evaluation.
type StoredEvaluation struct {
	ID         string          `json:"id"`
	Scope      adapter.Scope   `json:"scope"`
	SnapshotID string          `json:"snapshot_id"`
	Catalogs   []string        `json:"catalogs"`
	Result     json.RawMessage `json:"result"`
	CreatedAt  time.Time       `json:"created_at"`
}

// RecordStore persists revisions and record versions.
type RecordStore interface {
	// CurrentRevision returns the latest revision or ErrNotFound when there is none.
	CurrentRevision(ctx context.Context, scope adapter.Scope) (Revision, error)
	Revision(ctx context.Context, scope adapter.Scope, number int64) (Revision, error)
	// Revisions returns every revision in ascending order.
	Revisions(ctx context.Context, scope adapter.Scope) ([]Revision, error)
	// Records returns the records of a revision sorted by entity then key.
	// Revision 0 is the empty snapshot.
	Records(ctx context.Context, scope adapter.Scope, revision int64) ([]RecordVersion, error)
	// Commit creates revision ExpectedRevision+1 or fails with ErrConflict.
	Commit(ctx context.Context, c Commit) (Revision, error)
	// SaveIngestion stores an ingestion that did not produce a revision.
	SaveIngestion(ctx context.Context, ing Ingestion) error
	Ingestion(ctx context.Context, scope adapter.Scope, id string) (Ingestion, error)
}

// ProvenanceLog reads the append-only change log.
type ProvenanceLog interface {
	Provenance(ctx context.Context, scope adapter.Scope, entity, key string) ([]ProvenanceEntry, error)
}

// ManifestStore persists adapter manifests per workspace (keyed by name).
type ManifestStore interface {
	PutManifest(ctx context.Context, scope adapter.Scope, m adapter.Manifest) error
	Manifests(ctx context.Context, scope adapter.Scope) ([]adapter.Manifest, error)
}

// EvaluationStore persists evaluations.
type EvaluationStore interface {
	SaveEvaluation(ctx context.Context, e StoredEvaluation) error
	Evaluation(ctx context.Context, scope adapter.Scope, id string) (StoredEvaluation, error)
}

// Store is the full persistence contract.
type Store interface {
	RecordStore
	ProvenanceLog
	ManifestStore
	EvaluationStore
	ReportStore
	AuditLog
	IdentityStore
	EvidenceStore
	WorkflowStore
	KeyStore
	SettingsStore
	RetentionStore
	TenantStore
	WorkspaceStore
}

// AuditEvent is one entry of a scope's append-only, hash-chained audit log.
// A WorkspaceID of "" is the tenant-level chain (user administration). Seq,
// PrevHash and Hash are assigned by the store when the event is appended.
type AuditEvent struct {
	Scope      adapter.Scope   `json:"scope"`
	Seq        int64           `json:"seq"`
	At         time.Time       `json:"at"`
	Actor      string          `json:"actor"`
	ActorKind  string          `json:"actor_kind"`
	Action     string          `json:"action"`
	TargetType string          `json:"target_type"`
	TargetID   string          `json:"target_id"`
	Details    json.RawMessage `json:"details"`
	PrevHash   string          `json:"prev_hash"`
	Hash       string          `json:"hash"`
}

// AuditQuery pages through a chain. Limit 0 means no limit.
type AuditQuery struct {
	AfterSeq int64
	Limit    int
}

// AuditLog stores audit events. Appends are atomic with the action they
// record: methods that change state take their events and fail without
// effect when the events cannot be appended.
type AuditLog interface {
	// AppendAudit seals and appends events (all of one scope).
	AppendAudit(ctx context.Context, events ...AuditEvent) error
	// AuditEvents returns a chain in ascending Seq order.
	AuditEvents(ctx context.Context, scope adapter.Scope, q AuditQuery) ([]AuditEvent, error)
	// AuditScopes lists the chains of a tenant, ordered by workspace.
	AuditScopes(ctx context.Context, tenantID string) ([]adapter.Scope, error)
}

// User is a local account of one tenant. ExternalID is set for users a host
// product identifies (for example "nexops:<user id>"); it is unique per tenant.
type User struct {
	TenantID     string    `json:"tenant_id"`
	ID           string    `json:"id"`
	Email        string    `json:"email"`
	ExternalID   string    `json:"external_id,omitempty"`
	PasswordHash string    `json:"-"`
	Disabled     bool      `json:"disabled"`
	CreatedAt    time.Time `json:"created_at"`
}

// Member binds a user to a workspace with roles.
type Member struct {
	Scope     adapter.Scope `json:"scope"`
	UserID    string        `json:"user_id"`
	Email     string        `json:"email"`
	Roles     []access.Role `json:"roles"`
	UpdatedAt time.Time     `json:"updated_at"`
}

// Token is a stored API token. Only its SHA-256 hash is kept. A token with
// a UserID acts as that user; one without is a service token.
type Token struct {
	ID         string        `json:"id"`
	Scope      adapter.Scope `json:"scope"`
	Hash       string        `json:"-"`
	Name       string        `json:"name"`
	UserID     string        `json:"user_id,omitempty"`
	Roles      []access.Role `json:"roles"`
	CreatedBy  string        `json:"created_by"`
	CreatedAt  time.Time     `json:"created_at"`
	ExpiresAt  *time.Time    `json:"expires_at,omitempty"`
	RevokedAt  *time.Time    `json:"revoked_at,omitempty"`
	LastUsedAt *time.Time    `json:"last_used_at,omitempty"`
}

// Active reports whether the token is neither revoked nor expired at now.
func (t Token) Active(now time.Time) bool {
	return t.RevokedAt == nil && (t.ExpiresAt == nil || now.Before(*t.ExpiresAt))
}

// IdentityStore stores users, memberships and API tokens. Every mutating
// method appends its audit event in the same transaction.
type IdentityStore interface {
	// CreateUser fails with ErrExists when the email or the external ID is
	// taken in the tenant.
	CreateUser(ctx context.Context, u User, ev AuditEvent) error
	User(ctx context.Context, tenantID, id string) (User, error)
	UserByEmail(ctx context.Context, tenantID, email string) (User, error)
	// UserByExternalID finds a host product's user; an empty externalID
	// matches nobody.
	UserByExternalID(ctx context.Context, tenantID, externalID string) (User, error)
	// SetUserEmail changes a user's email; ErrExists when another user of the
	// tenant has it, ErrNotFound for an unknown user.
	SetUserEmail(ctx context.Context, tenantID, userID, email string, ev AuditEvent) error
	// Users lists a tenant's users ordered by email.
	Users(ctx context.Context, tenantID string) ([]User, error)
	SetPassword(ctx context.Context, tenantID, userID, hash string, ev AuditEvent) error
	// PutMember creates or replaces a membership; ErrNotFound for an unknown user.
	PutMember(ctx context.Context, m Member, ev AuditEvent) error
	Member(ctx context.Context, scope adapter.Scope, userID string) (Member, error)
	// Members lists a workspace's members ordered by email.
	Members(ctx context.Context, scope adapter.Scope) ([]Member, error)
	// DeleteMember removes a membership and revokes the user's active tokens in
	// scope; ErrNotFound when there is no such membership.
	DeleteMember(ctx context.Context, scope adapter.Scope, userID string, at time.Time, ev AuditEvent) error
	// CreateToken fails with ErrExists when the hash already exists.
	CreateToken(ctx context.Context, t Token, ev AuditEvent) error
	TokenByHash(ctx context.Context, hash string) (Token, error)
	// Tokens lists a workspace's tokens ordered by creation time, then ID.
	Tokens(ctx context.Context, scope adapter.Scope) ([]Token, error)
	// RevokeToken revokes a token of scope; ErrNotFound when unknown. Revoking
	// an already revoked token changes nothing and appends no event.
	RevokeToken(ctx context.Context, scope adapter.Scope, id string, at time.Time, ev AuditEvent) error
	TouchToken(ctx context.Context, id string, at time.Time) error
	// HasActiveTokens reports whether any tenant has a usable token at now.
	HasActiveTokens(ctx context.Context, now time.Time) (bool, error)
	// UserMembers lists a user's memberships in their tenant, ordered by workspace.
	UserMembers(ctx context.Context, tenantID, userID string) ([]Member, error)
	SessionStore
}

// NormalizeEmail trims and lower-cases an email address.
func NormalizeEmail(email string) string { return strings.ToLower(strings.TrimSpace(email)) }

// CloneRecord deep-copies a record made of JSON types.
func CloneRecord(rec adapter.Record) adapter.Record {
	if rec == nil {
		return nil
	}
	return cloneValue(map[string]any(rec)).(map[string]any)
}

func cloneValue(v any) any {
	switch t := v.(type) {
	case map[string]any:
		m := make(map[string]any, len(t))
		for k, x := range t {
			m[k] = cloneValue(x)
		}
		return m
	case []any:
		s := make([]any, len(t))
		for i, x := range t {
			s[i] = cloneValue(x)
		}
		return s
	}
	return v
}
