// SPDX-License-Identifier: Apache-2.0

package store

import (
	"context"
	"encoding/json"
	"time"

	"github.com/nexops-one/compliance-engine/sdk/adapter"
)

// WrappedKey is one version of a tenant's data keys, wrapped by a KEK.
type WrappedKey struct {
	Version    int       `json:"version"`
	KEKID      string    `json:"kek_id"`
	WrappedEnc []byte    `json:"wrapped_enc"`
	WrappedMac []byte    `json:"wrapped_mac"`
	CreatedAt  time.Time `json:"created_at"`
}

// TenantKeys lists a tenant's data key versions; Active is used for new writes.
type TenantKeys struct {
	TenantID string       `json:"tenant_id"`
	Active   int          `json:"active"`
	Versions []WrappedKey `json:"versions"`
	Version  int64        `json:"version"`
}

// KeyStore stores wrapped tenant keys. Deleting them makes every value sealed
// with them unreadable (crypto-shredding).
type KeyStore interface {
	TenantKeys(ctx context.Context, tenantID string) (TenantKeys, error)
	// PutTenantKeys creates (expected version 0) or replaces keys; ErrConflict on a version mismatch.
	PutTenantKeys(ctx context.Context, k TenantKeys, expectedVersion int64, events ...AuditEvent) (TenantKeys, error)
	DeleteTenantKeys(ctx context.Context, tenantID string, events ...AuditEvent) error
	// KeyTenants lists tenants that have keys.
	KeyTenants(ctx context.Context) ([]string, error)
}

// RetentionPolicy is a workspace's retention policy. Zero days keep forever.
type RetentionPolicy struct {
	RevisionDays   int `json:"revision_days"`
	KeepRevisions  int `json:"keep_revisions"`
	EvaluationDays int `json:"evaluation_days"`
}

// ReviewOverride replaces a control's catalog review settings in one workspace.
type ReviewOverride struct {
	ReviewInterval           string `json:"review_interval,omitempty"`
	ApprovalRequiresEvidence *bool  `json:"approval_requires_evidence,omitempty"`
}

// Settings are a workspace's settings.
type Settings struct {
	Scope           adapter.Scope             `json:"scope"`
	Retention       RetentionPolicy           `json:"retention"`
	ReviewOverrides map[string]ReviewOverride `json:"review_overrides"` // key "<catalog>/<control>"
	Sample          bool                      `json:"sample"`
	// Extensions hold editions' workspace settings by key (for example the
	// advanced workflow rules). The open core stores them and nothing else.
	Extensions map[string]json.RawMessage `json:"extensions,omitempty"`
	UpdatedAt  time.Time                  `json:"updated_at"`
	Version    int64                      `json:"version"`
}

// DefaultSettings are the settings of a workspace that never saved any.
func DefaultSettings(scope adapter.Scope) Settings {
	return Settings{Scope: scope, Retention: RetentionPolicy{KeepRevisions: 1}, ReviewOverrides: map[string]ReviewOverride{}}
}

// SettingsStore stores workspace settings.
type SettingsStore interface {
	// Settings returns the stored settings, or DefaultSettings (Version 0).
	Settings(ctx context.Context, scope adapter.Scope) (Settings, error)
	PutSettings(ctx context.Context, s Settings, expectedVersion int64, events ...AuditEvent) (Settings, error)
}

// EvaluationRef identifies a stored evaluation for retention.
type EvaluationRef struct {
	ID         string    `json:"id"`
	SnapshotID string    `json:"snapshot_id"`
	CreatedAt  time.Time `json:"created_at"`
}

// RetentionCounts reports what a retention run deleted.
type RetentionCounts struct {
	Revisions      int `json:"revisions"`
	RecordVersions int `json:"record_versions"`
	Provenance     int `json:"provenance"`
	Ingestions     int `json:"ingestions"`
	Evaluations    int `json:"evaluations"`
	Reports        int `json:"reports"`
	Evidence       int `json:"evidence"`
}

// RetentionDeletion is what one retention run deletes in a workspace.
type RetentionDeletion struct {
	// KeepFrom is the oldest revision kept; 0 deletes no revision.
	KeepFrom    int64
	Evaluations []string
	Reports     []string
	Evidence    []string
}

// RetentionStore deletes data a retention plan allows to delete.
type RetentionStore interface {
	// WorkspaceScopes lists every workspace holding data.
	WorkspaceScopes(ctx context.Context) ([]adapter.Scope, error)
	EvaluationRefs(ctx context.Context, scope adapter.Scope) ([]EvaluationRef, error)
	// ApplyRetention deletes revisions below d.KeepFrom (with record versions,
	// provenance and ingestions that only belong to them), the listed
	// evaluations, reports (with their files) and evidence, and appends
	// events, in one transaction.
	ApplyRetention(ctx context.Context, scope adapter.Scope, d RetentionDeletion, events ...AuditEvent) (RetentionCounts, error)
	// PruneAudit deletes the oldest audit events of scope's chain recorded
	// before `before`, always keeping the latest event, and appends event
	// (which chains after the kept events). It returns the deleted count.
	PruneAudit(ctx context.Context, scope adapter.Scope, before time.Time, event AuditEvent) (int, error)
}

// TenantStore deletes a whole tenant.
type TenantStore interface {
	// DeleteTenantData deletes every record, revision, ingestion, manifest,
	// evaluation, report, evidence item, assessment and its history, setting, user,
	// membership and token of the tenant, and appends events, in one
	// transaction. Audit events and tenant keys are not touched.
	DeleteTenantData(ctx context.Context, tenantID string, events ...AuditEvent) (int, error)
}
