// SPDX-License-Identifier: Apache-2.0

package compliance

import (
	"context"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/nexops-one/compliance-engine/pkg/evidence"
	"github.com/nexops-one/compliance-engine/pkg/extension"
	"github.com/nexops-one/compliance-engine/pkg/store"
)

// ErrEvidenceRetention means evidence of the tenant is still within its minimum retention.
var ErrEvidenceRetention = errors.New("evidence is still within its minimum retention")

// UploadEvidence stores an evidence object in managed storage (encrypted with
// the tenant key) and attaches it. URI and checksum are set by the engine; the
// checksum is computed from the uploaded bytes, so the item starts verified.
func (e *Engine) UploadEvidence(ctx context.Context, scope Scope, in EvidenceInput, r io.Reader) (store.Evidence, error) {
	if err := scope.Validate(); err != nil {
		return store.Evidence{}, err
	}
	if e.workflow.Managed == nil {
		return store.Evidence{}, evidence.ErrManagedNotConfigured
	}
	if _, err := e.require(ctx, scope, extension.FeatureEvidenceManage); err != nil {
		return store.Evidence{}, err
	}
	sum, size, err := e.workflow.Managed.Put(ctx, scope.TenantID, r)
	if err != nil {
		return store.Evidence{}, err
	}
	in.URI, in.Checksum = evidence.ManagedScheme+sum, "sha256:"+sum
	ev, err := e.addEvidence(ctx, scope, in, true)
	if err != nil {
		return store.Evidence{}, err
	}
	actor, _ := e.actor(ctx)
	return e.updateEvidence(ctx, scope, ev.ID, "evidence.check", func(x *store.Evidence) (map[string]any, error) {
		x.Integrity = store.IntegrityVerified
		x.Checks = append(x.Checks, store.EvidenceCheck{At: e.now(), Actor: actor, Method: "managed_upload", Observed: in.Checksum, Match: true})
		return map[string]any{"method": "managed_upload", "match": true, "size": size}, nil
	})
}

// EvidenceContent returns a managed evidence object and logs the access.
func (e *Engine) EvidenceContent(ctx context.Context, scope Scope, id string) ([]byte, store.Evidence, error) {
	ev, err := e.store.Evidence(ctx, scope, id)
	if err != nil {
		return nil, store.Evidence{}, err
	}
	sum, ok := evidence.ManagedSum(ev.URI)
	if !ok {
		return nil, store.Evidence{}, evidence.ErrNotManaged
	}
	if e.workflow.Managed == nil {
		return nil, store.Evidence{}, evidence.ErrManagedNotConfigured
	}
	data, err := e.workflow.Managed.Open(ctx, scope.TenantID, sum)
	if err != nil {
		return nil, store.Evidence{}, err
	}
	if err := e.store.AppendAudit(ctx, e.auditEvent(ctx, scope, "evidence.access", "evidence", id, map[string]any{"ids": []string{id}, "content": true})); err != nil {
		return nil, store.Evidence{}, err
	}
	return data, ev, nil
}

// managedChecksum hashes a managed object for verification.
func (e *Engine) managedChecksum(ctx context.Context, scope Scope, uri string) (string, bool, error) {
	sum, ok := evidence.ManagedSum(uri)
	if !ok {
		return "", false, nil
	}
	if e.workflow.Managed == nil {
		return "", true, evidence.ErrManagedNotConfigured
	}
	data, err := e.workflow.Managed.Open(ctx, scope.TenantID, sum)
	if err != nil {
		return "", true, err
	}
	return checksumBytes(data), true, nil
}

// dropManagedObject removes a deleted evidence item's object when no other
// evidence item of the tenant still refers to it.
func (e *Engine) dropManagedObject(ctx context.Context, scope Scope, ev store.Evidence) error {
	sum, ok := evidence.ManagedSum(ev.URI)
	if !ok || e.workflow.Managed == nil {
		return nil
	}
	scopes, err := e.store.WorkspaceScopes(ctx)
	if err != nil {
		return err
	}
	for _, sc := range scopes {
		if sc.TenantID != scope.TenantID {
			continue
		}
		evs, err := e.store.ListEvidence(ctx, sc, store.EvidenceQuery{})
		if err != nil {
			return err
		}
		for _, x := range evs {
			if x.URI == ev.URI && !(sc == scope && x.ID == ev.ID) {
				return nil
			}
		}
	}
	return e.workflow.Managed.Delete(scope.TenantID, sum)
}

// TenantDeletion reports a tenant deletion.
type TenantDeletion struct {
	TenantID    string `json:"tenant_id"`
	Rows        int    `json:"rows"`
	Workspaces  int    `json:"workspaces"`
	AuditPruned int    `json:"audit_pruned"`
	Overridden  bool   `json:"evidence_retention_overridden"`
}

// DeleteTenant deletes all data of a tenant, then its keys, which makes any
// copy of its encrypted data (backups included) unreadable. Audit chains are
// kept within the audit floor and record the deletion. Evidence still within
// its minimum retention blocks the deletion unless overridden.
func (e *Engine) DeleteTenant(ctx context.Context, tenant string, overrideEvidenceRetention bool, keys store.KeyStore) (TenantDeletion, error) {
	rep := TenantDeletion{TenantID: tenant, Overridden: overrideEvidenceRetention}
	if tenant == "" {
		return rep, errors.New("tenant is required")
	}
	scopes, err := e.store.WorkspaceScopes(ctx)
	if err != nil {
		return rep, err
	}
	now := e.now()
	var blocking []string
	for _, sc := range scopes {
		if sc.TenantID != tenant {
			continue
		}
		rep.Workspaces++
		evs, err := e.store.ListEvidence(ctx, sc, store.EvidenceQuery{})
		if err != nil {
			return rep, err
		}
		for _, x := range evs {
			if now.Before(x.CreatedAt.Add(time.Duration(x.Retention.MinDays) * 24 * time.Hour)) {
				blocking = append(blocking, sc.WorkspaceID+"/"+x.ID)
			}
		}
	}
	if len(blocking) > 0 && !overrideEvidenceRetention {
		return rep, fmt.Errorf("%w: %d item(s), for example %s; pass --override-evidence-retention to delete anyway", ErrEvidenceRetention, len(blocking), blocking[0])
	}
	tenantScope := Scope{TenantID: tenant}
	ev := e.auditEvent(ctx, tenantScope, "tenant.delete", "tenant", tenant, map[string]any{
		"workspaces": rep.Workspaces, "evidence_retention_overridden": overrideEvidenceRetention, "evidence_within_retention": len(blocking),
	})
	if rep.Rows, err = e.store.DeleteTenantData(ctx, tenant, ev); err != nil {
		return rep, err
	}
	if keys != nil {
		if err := keys.DeleteTenantKeys(ctx, tenant, e.auditEvent(ctx, tenantScope, "keys.delete", "tenant_keys", tenant, map[string]any{})); err != nil {
			return rep, err
		}
	}
	if e.workflow.Managed != nil {
		if err := e.workflow.Managed.DeleteTenant(tenant); err != nil {
			return rep, err
		}
	}
	if e.retention.AuditDays > 0 {
		before := now.Add(-time.Duration(e.retention.AuditDays) * 24 * time.Hour)
		chains, err := e.store.AuditScopes(ctx, tenant)
		if err != nil {
			return rep, err
		}
		for _, sc := range chains {
			n, err := e.store.PruneAudit(ctx, sc, before, e.auditEvent(ctx, sc, "audit.pruned", "workspace", sc.WorkspaceID, map[string]any{"before": before}))
			if err != nil {
				return rep, err
			}
			rep.AuditPruned += n
		}
	}
	return rep, nil
}
