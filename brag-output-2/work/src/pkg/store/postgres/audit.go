// SPDX-License-Identifier: Apache-2.0

package postgres

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/jackc/pgx/v5"

	"github.com/nexops-one/compliance-engine/pkg/audit"
	"github.com/nexops-one/compliance-engine/pkg/store"
	"github.com/nexops-one/compliance-engine/sdk/adapter"
)

// appendAudit seals and inserts events inside tx. The per-chain advisory lock
// serializes appenders; callers that also hold the revision lock always take
// it first, so the lock order is fixed.
func appendAudit(ctx context.Context, tx pgx.Tx, events []store.AuditEvent) error {
	if len(events) == 0 {
		return nil
	}
	sc := events[0].Scope
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended('audit' || chr(31) || $1 || chr(31) || $2, 0))`,
		sc.TenantID, sc.WorkspaceID); err != nil {
		return err
	}
	var seq int64
	var hash string
	err := tx.QueryRow(ctx, `SELECT seq, hash FROM ce_audit_events WHERE tenant_id = $1 AND workspace_id = $2 ORDER BY seq DESC LIMIT 1`,
		sc.TenantID, sc.WorkspaceID).Scan(&seq, &hash)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return err
	}
	sealed, err := audit.Seal(seq, hash, events)
	if err != nil {
		return err
	}
	rows := make([][]any, len(sealed))
	for i, e := range sealed {
		rows[i] = []any{e.Scope.TenantID, e.Scope.WorkspaceID, e.Seq, e.At, e.Actor, e.ActorKind, e.Action,
			e.TargetType, e.TargetID, string(e.Details), e.PrevHash, e.Hash}
	}
	_, err = tx.CopyFrom(ctx, pgx.Identifier{"ce_audit_events"}, []string{"tenant_id", "workspace_id", "seq", "at", "actor",
		"actor_kind", "action", "target_type", "target_id", "details", "prev_hash", "hash"}, pgx.CopyFromRows(rows))
	return err
}

// inTx runs fn in a transaction that also appends events; both commit or neither.
func (s *Store) inTx(ctx context.Context, events []store.AuditEvent, fn func(tx pgx.Tx) error) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if fn != nil {
		if err := fn(tx); err != nil {
			return err
		}
	}
	if err := appendAudit(ctx, tx, events); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s *Store) AppendAudit(ctx context.Context, events ...store.AuditEvent) error {
	return s.inTx(ctx, events, nil)
}

func (s *Store) AuditEvents(ctx context.Context, scope adapter.Scope, q store.AuditQuery) ([]store.AuditEvent, error) {
	limit := any(nil)
	if q.Limit > 0 {
		limit = q.Limit
	}
	rows, err := s.pool.Query(ctx, `SELECT seq, at, actor, actor_kind, action, target_type, target_id, details, prev_hash, hash
FROM ce_audit_events WHERE tenant_id = $1 AND workspace_id = $2 AND seq > $3 ORDER BY seq LIMIT $4`,
		scope.TenantID, scope.WorkspaceID, q.AfterSeq, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []store.AuditEvent{}
	for rows.Next() {
		e := store.AuditEvent{Scope: scope}
		var details string
		if err := rows.Scan(&e.Seq, &e.At, &e.Actor, &e.ActorKind, &e.Action, &e.TargetType, &e.TargetID, &details, &e.PrevHash, &e.Hash); err != nil {
			return nil, err
		}
		e.At, e.Details = e.At.UTC(), json.RawMessage(details)
		out = append(out, e)
	}
	return out, rows.Err()
}

func (s *Store) AuditScopes(ctx context.Context, tenantID string) ([]adapter.Scope, error) {
	rows, err := s.pool.Query(ctx, `SELECT DISTINCT workspace_id FROM ce_audit_events WHERE tenant_id = $1 ORDER BY workspace_id`, tenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []adapter.Scope{}
	for rows.Next() {
		sc := adapter.Scope{TenantID: tenantID}
		if err := rows.Scan(&sc.WorkspaceID); err != nil {
			return nil, err
		}
		out = append(out, sc)
	}
	return out, rows.Err()
}
