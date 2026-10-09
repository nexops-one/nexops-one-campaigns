// SPDX-License-Identifier: Apache-2.0

package postgres

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/nexops-one/compliance-engine/pkg/store"
	"github.com/nexops-one/compliance-engine/sdk/adapter"
)

func (s *Store) WorkspaceScopes(ctx context.Context) ([]adapter.Scope, error) {
	rows, err := s.pool.Query(ctx, `SELECT tenant_id, workspace_id FROM ce_revisions
UNION SELECT tenant_id, workspace_id FROM ce_evidence
UNION SELECT tenant_id, workspace_id FROM ce_assessments
ORDER BY 1, 2`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []adapter.Scope{}
	for rows.Next() {
		var sc adapter.Scope
		if err := rows.Scan(&sc.TenantID, &sc.WorkspaceID); err != nil {
			return nil, err
		}
		out = append(out, sc)
	}
	return out, rows.Err()
}

func (s *Store) EvaluationRefs(ctx context.Context, scope adapter.Scope) ([]store.EvaluationRef, error) {
	rows, err := s.pool.Query(ctx, `SELECT id, snapshot_id, created_at FROM ce_evaluations WHERE tenant_id = $1 AND workspace_id = $2 ORDER BY id`,
		scope.TenantID, scope.WorkspaceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []store.EvaluationRef{}
	for rows.Next() {
		var r store.EvaluationRef
		if err := rows.Scan(&r.ID, &r.SnapshotID, &r.CreatedAt); err != nil {
			return nil, err
		}
		r.CreatedAt = r.CreatedAt.UTC()
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *Store) ApplyRetention(ctx context.Context, scope adapter.Scope, del store.RetentionDeletion, events ...store.AuditEvent) (store.RetentionCounts, error) {
	var c store.RetentionCounts
	keepFrom, evaluations, evidence := del.KeepFrom, del.Evaluations, del.Evidence
	t, w := scope.TenantID, scope.WorkspaceID
	err := s.inTx(ctx, events, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`, t+"\x1f"+w); err != nil {
			return err
		}
		exec := func(n *int, sql string, args ...any) error {
			tag, err := tx.Exec(ctx, sql, args...)
			*n += int(tag.RowsAffected())
			return err
		}
		if keepFrom > 0 {
			steps := []struct {
				n   *int
				sql string
			}{
				// Versions that ended before the first kept revision exist only in deleted revisions.
				{&c.RecordVersions, `DELETE FROM ce_record_versions WHERE tenant_id = $1 AND workspace_id = $2 AND valid_to IS NOT NULL AND valid_to <= $3`},
				{&c.Provenance, `DELETE FROM ce_provenance WHERE tenant_id = $1 AND workspace_id = $2 AND revision < $3`},
				{&c.Ingestions, `DELETE FROM ce_ingestions WHERE tenant_id = $1 AND workspace_id = $2
  AND ((revision_after <> 0 AND revision_after < $3) OR (revision_after = 0 AND revision_before < $3))`},
				{&c.Revisions, `DELETE FROM ce_revisions WHERE tenant_id = $1 AND workspace_id = $2 AND number < $3`},
			}
			for _, st := range steps {
				if err := exec(st.n, st.sql, t, w, keepFrom); err != nil {
					return err
				}
			}
		}
		if len(del.Reports) > 0 {
			if _, err := tx.Exec(ctx, `SELECT set_config('compliance.retention', 'on', true)`); err != nil {
				return err
			}
			var files int
			if err := exec(&files, `DELETE FROM ce_report_files WHERE tenant_id = $1 AND workspace_id = $2 AND report_id = ANY($3)`, t, w, del.Reports); err != nil {
				return err
			}
			if err := exec(&c.Reports, `DELETE FROM ce_reports WHERE tenant_id = $1 AND workspace_id = $2 AND id = ANY($3)`, t, w, del.Reports); err != nil {
				return err
			}
			if _, err := tx.Exec(ctx, `SELECT set_config('compliance.retention', 'off', true)`); err != nil {
				return err
			}
		}
		if len(evaluations) > 0 {
			if err := exec(&c.Evaluations, `DELETE FROM ce_evaluations WHERE tenant_id = $1 AND workspace_id = $2 AND id = ANY($3)`, t, w, evaluations); err != nil {
				return err
			}
		}
		if len(evidence) > 0 {
			var links int
			if err := exec(&links, `DELETE FROM ce_evidence_links WHERE tenant_id = $1 AND workspace_id = $2 AND evidence_id = ANY($3)`, t, w, evidence); err != nil {
				return err
			}
			if err := exec(&c.Evidence, `DELETE FROM ce_evidence WHERE tenant_id = $1 AND workspace_id = $2 AND id = ANY($3)`, t, w, evidence); err != nil {
				return err
			}
		}
		return nil
	})
	return c, err
}

func (s *Store) PruneAudit(ctx context.Context, scope adapter.Scope, before time.Time, event store.AuditEvent) (int, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	t, w := scope.TenantID, scope.WorkspaceID
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended('audit' || chr(31) || $1 || chr(31) || $2, 0))`, t, w); err != nil {
		return 0, err
	}
	// Delete a prefix of the chain: events before the first event at or after
	// `before`, and never the latest event.
	var cut *int64
	if err := tx.QueryRow(ctx, `SELECT least(
  (SELECT min(seq) FROM ce_audit_events WHERE tenant_id = $1 AND workspace_id = $2 AND at >= $3),
  (SELECT max(seq) FROM ce_audit_events WHERE tenant_id = $1 AND workspace_id = $2))`, t, w, before).Scan(&cut); err != nil {
		return 0, err
	}
	if cut == nil {
		return 0, nil
	}
	if _, err := tx.Exec(ctx, `SELECT set_config('compliance.audit_prune', 'on', true), set_config('compliance.audit_prune_before', $1, true)`,
		before.UTC().Format(time.RFC3339Nano)); err != nil {
		return 0, err
	}
	tag, err := tx.Exec(ctx, `DELETE FROM ce_audit_events WHERE tenant_id = $1 AND workspace_id = $2 AND seq < $3`, t, w, *cut)
	if err != nil {
		return 0, err
	}
	n := int(tag.RowsAffected())
	if n == 0 {
		return 0, nil
	}
	if _, err := tx.Exec(ctx, `SELECT set_config('compliance.audit_prune', 'off', true)`); err != nil {
		return 0, err
	}
	if err := appendAudit(ctx, tx, []store.AuditEvent{event}); err != nil {
		return 0, err
	}
	return n, tx.Commit(ctx)
}
