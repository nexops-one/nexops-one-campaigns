// SPDX-License-Identifier: Apache-2.0

package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/nexops-one/compliance-engine/pkg/store"
	"github.com/nexops-one/compliance-engine/sdk/adapter"
)

func (s *Store) UserMembers(ctx context.Context, tenantID, userID string) ([]store.Member, error) {
	rows, err := s.pool.Query(ctx, `SELECT m.workspace_id, m.user_id, u.email, m.roles, m.updated_at FROM ce_members m
JOIN ce_users u ON u.tenant_id = m.tenant_id AND u.id = m.user_id
WHERE m.tenant_id = $1 AND m.user_id = $2 ORDER BY m.workspace_id`, tenantID, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []store.Member{}
	for rows.Next() {
		m := store.Member{Scope: adapter.Scope{TenantID: tenantID}}
		var roles []string
		if err := rows.Scan(&m.Scope.WorkspaceID, &m.UserID, &m.Email, &roles, &m.UpdatedAt); err != nil {
			return nil, err
		}
		m.Roles, m.UpdatedAt = textToRoles(roles), m.UpdatedAt.UTC()
		out = append(out, m)
	}
	return out, rows.Err()
}

func (s *Store) CreateSession(ctx context.Context, se store.Session, ev store.AuditEvent) error {
	err := s.inTx(ctx, []store.AuditEvent{ev}, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `INSERT INTO ce_sessions (hash, tenant_id, user_id, workspace_id, created_at, last_seen_at, expires_at)
VALUES ($1, $2, $3, $4, $5, $6, $7)`, se.Hash, se.TenantID, se.UserID, se.WorkspaceID, se.CreatedAt, se.LastSeenAt, se.ExpiresAt)
		return err
	})
	if uniqueViolation(err) {
		return fmt.Errorf("%w: session", store.ErrExists)
	}
	return err
}

func (s *Store) SessionByHash(ctx context.Context, hash string) (store.Session, error) {
	se := store.Session{Hash: hash}
	err := s.pool.QueryRow(ctx, `SELECT tenant_id, user_id, workspace_id, created_at, last_seen_at, expires_at
FROM ce_sessions WHERE hash = $1`, hash).Scan(&se.TenantID, &se.UserID, &se.WorkspaceID, &se.CreatedAt, &se.LastSeenAt, &se.ExpiresAt)
	if err != nil {
		return store.Session{}, notFound(err, "session")
	}
	se.CreatedAt, se.LastSeenAt, se.ExpiresAt = se.CreatedAt.UTC(), se.LastSeenAt.UTC(), se.ExpiresAt.UTC()
	return se, nil
}

func (s *Store) TouchSession(ctx context.Context, hash string, at time.Time) error {
	tag, err := s.pool.Exec(ctx, `UPDATE ce_sessions SET last_seen_at = $2 WHERE hash = $1`, hash, at)
	return requireOne(tag, err, "session")
}

func (s *Store) SetSessionWorkspace(ctx context.Context, hash, workspaceID string) error {
	tag, err := s.pool.Exec(ctx, `UPDATE ce_sessions SET workspace_id = $2 WHERE hash = $1`, hash, workspaceID)
	return requireOne(tag, err, "session")
}

func (s *Store) DeleteSession(ctx context.Context, hash string, events ...store.AuditEvent) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	tag, err := tx.Exec(ctx, `DELETE FROM ce_sessions WHERE hash = $1`, hash)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return nil
	}
	if err := appendAudit(ctx, tx, events); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s *Store) DeleteExpiredSessions(ctx context.Context, now, idleBefore time.Time) (int, error) {
	tag, err := s.pool.Exec(ctx, `DELETE FROM ce_sessions WHERE expires_at <= $1 OR last_seen_at < $2`, now, idleBefore)
	if err != nil {
		return 0, err
	}
	return int(tag.RowsAffected()), nil
}
