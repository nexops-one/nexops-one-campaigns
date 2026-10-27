// SPDX-License-Identifier: Apache-2.0

package store

import (
	"context"
	"errors"
	"time"

	"github.com/nexops-one/compliance-engine/sdk/adapter"
)

// WorkspaceStatus is the lifecycle state of a registered workspace.
type WorkspaceStatus string

const (
	// WorkspaceActive workspaces accept writes.
	WorkspaceActive WorkspaceStatus = "active"
	// WorkspaceSuspended workspaces refuse writes; their data stays readable.
	WorkspaceSuspended WorkspaceStatus = "suspended"
)

// ErrWorkspaceLimit refuses registering or resuming a workspace when the
// deployment already has as many active workspaces as its limit allows.
var ErrWorkspaceLimit = errors.New("workspace limit reached")

// Workspace is a registered workspace. Workspaces are registered on first
// use, or explicitly by a system administrator.
type Workspace struct {
	Scope     adapter.Scope   `json:"scope"`
	Status    WorkspaceStatus `json:"status"`
	CreatedAt time.Time       `json:"created_at"`
	CreatedBy string          `json:"created_by"`
	// StatusAt, StatusBy and Reason describe the last status change.
	StatusAt time.Time `json:"status_at"`
	StatusBy string    `json:"status_by"`
	Reason   string    `json:"reason,omitempty"`
}

// WorkspaceStore is the workspace registry. limit bounds the number of
// active workspaces across all tenants; 0 means no limit.
type WorkspaceStore interface {
	// Workspace returns a registered workspace, or ErrNotFound.
	Workspace(ctx context.Context, scope adapter.Scope) (Workspace, error)
	// Workspaces lists the workspaces of a tenant ("" for every tenant),
	// ordered by tenant and workspace.
	Workspaces(ctx context.Context, tenantID string) ([]Workspace, error)
	// RegisterWorkspace stores w (status active) unless its scope is already
	// registered, and reports whether it did; events are appended only then.
	// The count and the insert are atomic: concurrent registrations never
	// exceed limit (ErrWorkspaceLimit).
	RegisterWorkspace(ctx context.Context, w Workspace, limit int, events ...AuditEvent) (Workspace, bool, error)
	// SetWorkspaceStatus changes a workspace's status (ErrNotFound when not
	// registered). Making a workspace active again counts against limit.
	SetWorkspaceStatus(ctx context.Context, scope adapter.Scope, status WorkspaceStatus, at time.Time, by, reason string, limit int,
		events ...AuditEvent) (Workspace, error)
	// DeleteWorkspaceData deletes every record, revision, ingestion, manifest,
	// evaluation, report, evidence item, assessment and its history, setting,
	// membership, token and session of the workspace, and its registration,
	// and appends events, in one transaction. Users, audit events and tenant
	// keys are not touched.
	DeleteWorkspaceData(ctx context.Context, scope adapter.Scope, events ...AuditEvent) (int, error)
}
