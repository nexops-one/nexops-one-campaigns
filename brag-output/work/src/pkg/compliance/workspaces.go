// SPDX-License-Identifier: Apache-2.0

package compliance

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/nexops-one/compliance-engine/pkg/extension"
	"github.com/nexops-one/compliance-engine/pkg/store"
)

// SystemScope is the audit chain of deployment-wide actions: the workspace
// lifecycle, license installation and system administration.
var SystemScope = Scope{TenantID: "_system"}

var (
	// ErrWorkspaceSuspended refuses writes to a suspended workspace.
	ErrWorkspaceSuspended = errors.New("workspace is suspended")
	// ErrReservedTenant refuses tenant IDs starting with "_", which are
	// reserved for the engine (the _system audit chain).
	ErrReservedTenant = errors.New("tenant IDs starting with _ are reserved")
	// ErrWorkspaceExists refuses registering a workspace twice.
	ErrWorkspaceExists = errors.New("workspace is already registered")
)

// require checks that the workspace is entitled to f and may be written; it
// registers the workspace on its first write (see WorkspaceWritable).
func (e *Engine) require(ctx context.Context, scope Scope, f extension.Feature) (extension.Decision, error) {
	d, err := extension.Require(ctx, e.ents, scope, f)
	if err != nil {
		return d, err
	}
	return d, e.WorkspaceWritable(ctx, scope)
}

// WorkspaceWritable registers the workspace when it is used for the first
// time (bounded by the workspace limit of the entitlements provider), and
// refuses writes to a suspended workspace. Every engine write calls it; hosts
// call it before writes the engine does not perform (identity changes).
func (e *Engine) WorkspaceWritable(ctx context.Context, scope Scope) error {
	if err := scope.Validate(); err != nil {
		return err
	}
	w, err := e.store.Workspace(ctx, scope)
	if errors.Is(err, store.ErrNotFound) {
		w, err = e.registerWorkspace(ctx, scope, "first_use")
	}
	if err != nil {
		return err
	}
	if w.Status == store.WorkspaceSuspended {
		return fmt.Errorf("%w: %s/%s; its data stays readable", ErrWorkspaceSuspended, scope.TenantID, scope.WorkspaceID)
	}
	return nil
}

// workspaceLimit is the active workspace limit (0: none).
func (e *Engine) workspaceLimit(ctx context.Context) int {
	if l, ok := e.ents.(extension.WorkspaceLimiter); ok {
		if n := l.WorkspaceLimit(ctx); n > 0 {
			return n
		}
	}
	return 0
}

func (e *Engine) registerWorkspace(ctx context.Context, scope Scope, how string) (store.Workspace, error) {
	if strings.HasPrefix(scope.TenantID, "_") {
		return store.Workspace{}, fmt.Errorf("%w: %q", ErrReservedTenant, scope.TenantID)
	}
	actor, _ := e.actor(ctx)
	ev := e.auditEvent(ctx, SystemScope, "workspace.register", "workspace", scope.TenantID+"/"+scope.WorkspaceID,
		map[string]any{"tenant_id": scope.TenantID, "workspace_id": scope.WorkspaceID, "how": how})
	w, _, err := e.store.RegisterWorkspace(ctx, store.Workspace{Scope: scope, CreatedAt: e.now(), CreatedBy: actor}, e.workspaceLimit(ctx), ev)
	return w, err
}

// RegisterWorkspace registers a new workspace explicitly (system
// administration), counting it against the workspace limit.
func (e *Engine) RegisterWorkspace(ctx context.Context, scope Scope) (store.Workspace, error) {
	if err := scope.Validate(); err != nil {
		return store.Workspace{}, err
	}
	if _, err := e.store.Workspace(ctx, scope); err == nil {
		return store.Workspace{}, fmt.Errorf("%w: %s/%s", ErrWorkspaceExists, scope.TenantID, scope.WorkspaceID)
	} else if !errors.Is(err, store.ErrNotFound) {
		return store.Workspace{}, err
	}
	return e.registerWorkspace(ctx, scope, "explicit")
}

// Workspace returns a registered workspace.
func (e *Engine) Workspace(ctx context.Context, scope Scope) (store.Workspace, error) {
	return e.store.Workspace(ctx, scope)
}

// Workspaces lists the registered workspaces of a tenant ("" for all).
func (e *Engine) Workspaces(ctx context.Context, tenantID string) ([]store.Workspace, error) {
	return e.store.Workspaces(ctx, tenantID)
}

// SuspendWorkspace stops writes to a workspace; its data stays readable.
// Suspending frees a slot of the workspace limit.
func (e *Engine) SuspendWorkspace(ctx context.Context, scope Scope, reason string) (store.Workspace, error) {
	return e.setWorkspaceStatus(ctx, scope, store.WorkspaceSuspended, strings.TrimSpace(reason))
}

// ResumeWorkspace accepts writes again, counting against the workspace limit.
func (e *Engine) ResumeWorkspace(ctx context.Context, scope Scope) (store.Workspace, error) {
	return e.setWorkspaceStatus(ctx, scope, store.WorkspaceActive, "")
}

func (e *Engine) setWorkspaceStatus(ctx context.Context, scope Scope, status store.WorkspaceStatus, reason string) (store.Workspace, error) {
	if err := scope.Validate(); err != nil {
		return store.Workspace{}, err
	}
	action := "workspace.suspend"
	if status == store.WorkspaceActive {
		action = "workspace.resume"
	}
	actor, _ := e.actor(ctx)
	ev := e.auditEvent(ctx, SystemScope, action, "workspace", scope.TenantID+"/"+scope.WorkspaceID,
		map[string]any{"tenant_id": scope.TenantID, "workspace_id": scope.WorkspaceID, "reason": reason})
	return e.store.SetWorkspaceStatus(ctx, scope, status, e.now(), actor, reason, e.workspaceLimit(ctx), ev)
}

// WorkspaceUsage counts registered workspaces against the limit.
type WorkspaceUsage struct {
	// Limit is the maximum number of active workspaces; 0 means no limit.
	Limit     int            `json:"limit"`
	Active    int            `json:"active"`
	Suspended int            `json:"suspended"`
	ByTenant  map[string]int `json:"active_by_tenant"`
}

// Usage reports the workspace usage of the deployment.
func (e *Engine) Usage(ctx context.Context) (WorkspaceUsage, error) {
	ws, err := e.store.Workspaces(ctx, "")
	if err != nil {
		return WorkspaceUsage{}, err
	}
	u := WorkspaceUsage{Limit: e.workspaceLimit(ctx), ByTenant: map[string]int{}}
	for _, w := range ws {
		if w.Status == store.WorkspaceActive {
			u.Active++
			u.ByTenant[w.Scope.TenantID]++
		} else {
			u.Suspended++
		}
	}
	return u, nil
}

// WorkspaceDeletion reports a workspace deletion.
type WorkspaceDeletion struct {
	Scope      Scope `json:"scope"`
	Rows       int   `json:"rows"`
	Overridden bool  `json:"evidence_retention_overridden"`
}

// DeleteWorkspace deletes all data of a workspace and its registration.
// Users stay (they belong to the tenant), the workspace's audit chain is kept
// and the deletion is recorded in the _system chain. Keys are per tenant, so
// this is not crypto-shredding: delete the tenant for that. Evidence still
// within its minimum retention blocks the deletion unless overridden.
func (e *Engine) DeleteWorkspace(ctx context.Context, scope Scope, overrideEvidenceRetention bool) (WorkspaceDeletion, error) {
	rep := WorkspaceDeletion{Scope: scope, Overridden: overrideEvidenceRetention}
	if err := scope.Validate(); err != nil {
		return rep, err
	}
	evs, err := e.store.ListEvidence(ctx, scope, store.EvidenceQuery{})
	if err != nil {
		return rep, err
	}
	now := e.now()
	var blocking []string
	for _, x := range evs {
		if now.Before(x.CreatedAt.Add(time.Duration(x.Retention.MinDays) * 24 * time.Hour)) {
			blocking = append(blocking, x.ID)
		}
	}
	if len(blocking) > 0 && !overrideEvidenceRetention {
		return rep, fmt.Errorf("%w: %d item(s), for example %s", ErrEvidenceRetention, len(blocking), blocking[0])
	}
	ev := e.auditEvent(ctx, SystemScope, "workspace.delete", "workspace", scope.TenantID+"/"+scope.WorkspaceID, map[string]any{
		"tenant_id": scope.TenantID, "workspace_id": scope.WorkspaceID,
		"evidence_retention_overridden": overrideEvidenceRetention, "evidence_within_retention": len(blocking),
	})
	if rep.Rows, err = e.store.DeleteWorkspaceData(ctx, scope, ev); err != nil {
		return rep, err
	}
	for _, x := range evs {
		if err := e.dropManagedObject(ctx, scope, x); err != nil {
			return rep, err
		}
	}
	return rep, nil
}
