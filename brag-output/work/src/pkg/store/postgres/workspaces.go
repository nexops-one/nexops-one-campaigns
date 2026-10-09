// SPDX-License-Identifier: Apache-2.0

package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/nexops-one/compliance-engine/pkg/store"
	"github.com/nexops-one/compliance-engine/sdk/adapter"
)

const workspaceColumns = `tenant_id, workspace_id, status, created_at, created_by, status_at, status_by, reason`

func scanWorkspace(row pgx.Row) (store.Workspace, error) {
	var w store.Workspace
	var status string
	if err := row.Scan(&w.Scope.TenantID, &w.Scope.WorkspaceID, &status, &w.CreatedAt, &w.CreatedBy, &w.StatusAt, &w.StatusBy, &w.Reason); err != nil {
		return store.Workspace{}, err
	}
	w.Status, w.CreatedAt, w.StatusAt = store.WorkspaceStatus(status), w.CreatedAt.UTC(), w.StatusAt.UTC()
	return w, nil
}

func (s *Store) Workspace(ctx context.Context, scope adapter.Scope) (store.Workspace, error) {
	w, err := scanWorkspace(s.pool.QueryRow(ctx, `SELECT `+workspaceColumns+` FROM ce_workspaces WHERE tenant_id = $1 AND workspace_id = $2`,
		scope.TenantID, scope.WorkspaceID))
	if err != nil {
		return store.Workspace{}, notFound(err, "workspace")
	}
	return w, nil
}

func (s *Store) Workspaces(ctx context.Context, tenantID string) ([]store.Workspace, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+workspaceColumns+` FROM ce_workspaces WHERE $1 = '' OR tenant_id = $1 ORDER BY tenant_id, workspace_id`, tenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []store.Workspace{}
	for rows.Next() {
		w, err := scanWorkspace(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, w)
	}
	return out, rows.Err()
}

// lockWorkspaces serializes changes that count active workspaces, so that
// concurrent registrations never exceed the limit.
func lockWorkspaces(ctx context.Context, tx pgx.Tx) error {
	_, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended('workspaces', 0))`)
	return err
}

func checkLimit(ctx context.Context, tx pgx.Tx, limit int) error {
	if limit <= 0 {
		return nil
	}
	var n int
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM ce_workspaces WHERE status = 'active'`).Scan(&n); err != nil {
		return err
	}
	if n >= limit {
		return fmt.Errorf("%w: %d active workspace(s) allowed", store.ErrWorkspaceLimit, limit)
	}
	return nil
}

func (s *Store) RegisterWorkspace(ctx context.Context, w store.Workspace, limit int, events ...store.AuditEvent) (store.Workspace, bool, error) {
	if w.StatusAt.IsZero() {
		w.StatusAt, w.StatusBy = w.CreatedAt, w.CreatedBy
	}
	w.Status = store.WorkspaceActive
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return store.Workspace{}, false, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := lockWorkspaces(ctx, tx); err != nil {
		return store.Workspace{}, false, err
	}
	cur, err := scanWorkspace(tx.QueryRow(ctx, `SELECT `+workspaceColumns+` FROM ce_workspaces WHERE tenant_id = $1 AND workspace_id = $2`,
		w.Scope.TenantID, w.Scope.WorkspaceID))
	if err == nil {
		return cur, false, nil
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return store.Workspace{}, false, err
	}
	if err := checkLimit(ctx, tx, limit); err != nil {
		return store.Workspace{}, false, err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO ce_workspaces (`+workspaceColumns+`) VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
		w.Scope.TenantID, w.Scope.WorkspaceID, string(w.Status), w.CreatedAt, w.CreatedBy, w.StatusAt, w.StatusBy, w.Reason); err != nil {
		return store.Workspace{}, false, err
	}
	if err := appendAudit(ctx, tx, events); err != nil {
		return store.Workspace{}, false, err
	}
	if err := tx.Commit(ctx); err != nil {
		return store.Workspace{}, false, err
	}
	return w, true, nil
}

func (s *Store) SetWorkspaceStatus(ctx context.Context, scope adapter.Scope, status store.WorkspaceStatus, at time.Time, by, reason string, limit int,
	events ...store.AuditEvent) (store.Workspace, error) {
	var out store.Workspace
	err := s.inTx(ctx, events, func(tx pgx.Tx) error {
		if err := lockWorkspaces(ctx, tx); err != nil {
			return err
		}
		cur, err := scanWorkspace(tx.QueryRow(ctx, `SELECT `+workspaceColumns+` FROM ce_workspaces WHERE tenant_id = $1 AND workspace_id = $2 FOR UPDATE`,
			scope.TenantID, scope.WorkspaceID))
		if err != nil {
			return notFound(err, "workspace")
		}
		if status == store.WorkspaceActive && cur.Status != store.WorkspaceActive {
			if err := checkLimit(ctx, tx, limit); err != nil {
				return err
			}
		}
		if _, err := tx.Exec(ctx, `UPDATE ce_workspaces SET status = $3, status_at = $4, status_by = $5, reason = $6 WHERE tenant_id = $1 AND workspace_id = $2`,
			scope.TenantID, scope.WorkspaceID, string(status), at, by, reason); err != nil {
			return err
		}
		cur.Status, cur.StatusAt, cur.StatusBy, cur.Reason = status, at.UTC(), by, reason
		out = cur
		return nil
	})
	return out, err
}

// workspaceTables hold workspace data, deleted children before parents.
var workspaceTables = []string{
	"ce_evidence_links", "ce_evidence", "ce_assessment_history", "ce_assessments", "ce_settings",
	"ce_record_versions", "ce_provenance", "ce_ingestions", "ce_revisions", "ce_manifests", "ce_report_files", "ce_reports", "ce_evaluations",
	"ce_sessions", "ce_tokens", "ce_members", "ce_workspaces",
}

func (s *Store) DeleteWorkspaceData(ctx context.Context, scope adapter.Scope, events ...store.AuditEvent) (int, error) {
	total := 0
	err := s.inTx(ctx, events, func(tx pgx.Tx) error {
		// The append-only trigger lets a deletion name its tenant; only this
		// workspace's rows are deleted.
		if _, err := tx.Exec(ctx, `SELECT set_config('compliance.tenant_delete', $1, true)`, scope.TenantID); err != nil {
			return err
		}
		for _, table := range workspaceTables {
			tag, err := tx.Exec(ctx, `DELETE FROM `+table+` WHERE tenant_id = $1 AND workspace_id = $2`, scope.TenantID, scope.WorkspaceID)
			if err != nil {
				return err
			}
			total += int(tag.RowsAffected())
		}
		_, err := tx.Exec(ctx, `SELECT set_config('compliance.tenant_delete', '', true)`)
		return err
	})
	return total, err
}
