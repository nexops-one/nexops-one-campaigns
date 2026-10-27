// SPDX-License-Identifier: Apache-2.0

package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/nexops-one/compliance-engine/pkg/access"
	"github.com/nexops-one/compliance-engine/pkg/store"
	"github.com/nexops-one/compliance-engine/sdk/adapter"
)

func uniqueViolation(err error) bool {
	var pe *pgconn.PgError
	return errors.As(err, &pe) && pe.Code == "23505"
}

func rolesToText(rs []access.Role) []string {
	out := []string{}
	for _, r := range access.Normalize(rs) {
		out = append(out, string(r))
	}
	return out
}

func textToRoles(s []string) []access.Role {
	rs := make([]access.Role, len(s))
	for i, r := range s {
		rs[i] = access.Role(r)
	}
	return access.Normalize(rs)
}

func utcPtr(t *time.Time) *time.Time {
	if t == nil {
		return nil
	}
	u := t.UTC()
	return &u
}

// userColumns selects a user; a local user's NULL external_id reads as "".
const userColumns = `tenant_id, id, email, COALESCE(external_id, ''), password_hash, disabled, created_at`

func scanUser(row scanner) (store.User, error) {
	var u store.User
	if err := row.Scan(&u.TenantID, &u.ID, &u.Email, &u.ExternalID, &u.PasswordHash, &u.Disabled, &u.CreatedAt); err != nil {
		return store.User{}, err
	}
	u.CreatedAt = u.CreatedAt.UTC()
	return u, nil
}

func (s *Store) CreateUser(ctx context.Context, u store.User, ev store.AuditEvent) error {
	err := s.inTx(ctx, []store.AuditEvent{ev}, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `INSERT INTO ce_users (tenant_id, id, email, external_id, password_hash, disabled, created_at)
VALUES ($1, $2, $3, NULLIF($4, ''), $5, $6, $7)`,
			u.TenantID, u.ID, u.Email, u.ExternalID, u.PasswordHash, u.Disabled, u.CreatedAt)
		return err
	})
	if uniqueViolation(err) {
		return fmt.Errorf("%w: user %s", store.ErrExists, u.Email)
	}
	return err
}

func (s *Store) UserByExternalID(ctx context.Context, tenantID, externalID string) (store.User, error) {
	u, err := scanUser(s.pool.QueryRow(ctx, `SELECT `+userColumns+` FROM ce_users WHERE tenant_id = $1 AND external_id = $2`, tenantID, externalID))
	return u, notFound(err, "external user "+externalID)
}

func (s *Store) SetUserEmail(ctx context.Context, tenantID, userID, email string, ev store.AuditEvent) error {
	err := s.inTx(ctx, []store.AuditEvent{ev}, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `UPDATE ce_users SET email = $3 WHERE tenant_id = $1 AND id = $2`, tenantID, userID, email)
		return requireOne(tag, err, "user "+userID)
	})
	if uniqueViolation(err) {
		return fmt.Errorf("%w: user %s", store.ErrExists, email)
	}
	return err
}

func (s *Store) User(ctx context.Context, tenantID, id string) (store.User, error) {
	u, err := scanUser(s.pool.QueryRow(ctx, `SELECT `+userColumns+` FROM ce_users WHERE tenant_id = $1 AND id = $2`, tenantID, id))
	return u, notFound(err, "user "+id)
}

func (s *Store) UserByEmail(ctx context.Context, tenantID, email string) (store.User, error) {
	u, err := scanUser(s.pool.QueryRow(ctx, `SELECT `+userColumns+` FROM ce_users WHERE tenant_id = $1 AND email = $2`, tenantID, email))
	return u, notFound(err, "user "+email)
}

func (s *Store) Users(ctx context.Context, tenantID string) ([]store.User, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+userColumns+` FROM ce_users WHERE tenant_id = $1 ORDER BY email`, tenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []store.User{}
	for rows.Next() {
		u, err := scanUser(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

// requireOne turns an update that matched no row into ErrNotFound.
func requireOne(tag pgconn.CommandTag, err error, what string) error {
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("%w: %s", store.ErrNotFound, what)
	}
	return nil
}

func (s *Store) SetPassword(ctx context.Context, tenantID, userID, hash string, ev store.AuditEvent) error {
	return s.inTx(ctx, []store.AuditEvent{ev}, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `UPDATE ce_users SET password_hash = $3 WHERE tenant_id = $1 AND id = $2`, tenantID, userID, hash)
		return requireOne(tag, err, "user "+userID)
	})
}

func (s *Store) PutMember(ctx context.Context, m store.Member, ev store.AuditEvent) error {
	return s.inTx(ctx, []store.AuditEvent{ev}, func(tx pgx.Tx) error {
		var exists bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM ce_users WHERE tenant_id = $1 AND id = $2)`,
			m.Scope.TenantID, m.UserID).Scan(&exists); err != nil {
			return err
		}
		if !exists {
			return fmt.Errorf("%w: user %s", store.ErrNotFound, m.UserID)
		}
		_, err := tx.Exec(ctx, `INSERT INTO ce_members (tenant_id, workspace_id, user_id, roles, updated_at) VALUES ($1, $2, $3, $4, $5)
ON CONFLICT (tenant_id, workspace_id, user_id) DO UPDATE SET roles = EXCLUDED.roles, updated_at = EXCLUDED.updated_at`,
			m.Scope.TenantID, m.Scope.WorkspaceID, m.UserID, rolesToText(m.Roles), m.UpdatedAt)
		return err
	})
}

const memberQuery = `SELECT m.user_id, u.email, m.roles, m.updated_at FROM ce_members m
JOIN ce_users u ON u.tenant_id = m.tenant_id AND u.id = m.user_id
WHERE m.tenant_id = $1 AND m.workspace_id = $2`

func scanMember(row scanner, scope adapter.Scope) (store.Member, error) {
	m := store.Member{Scope: scope}
	var roles []string
	if err := row.Scan(&m.UserID, &m.Email, &roles, &m.UpdatedAt); err != nil {
		return store.Member{}, err
	}
	m.Roles, m.UpdatedAt = textToRoles(roles), m.UpdatedAt.UTC()
	return m, nil
}

func (s *Store) Member(ctx context.Context, scope adapter.Scope, userID string) (store.Member, error) {
	m, err := scanMember(s.pool.QueryRow(ctx, memberQuery+` AND m.user_id = $3`, scope.TenantID, scope.WorkspaceID, userID), scope)
	return m, notFound(err, "member "+userID)
}

func (s *Store) Members(ctx context.Context, scope adapter.Scope) ([]store.Member, error) {
	rows, err := s.pool.Query(ctx, memberQuery+` ORDER BY u.email`, scope.TenantID, scope.WorkspaceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []store.Member{}
	for rows.Next() {
		m, err := scanMember(rows, scope)
		if err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

func (s *Store) DeleteMember(ctx context.Context, scope adapter.Scope, userID string, at time.Time, ev store.AuditEvent) error {
	return s.inTx(ctx, []store.AuditEvent{ev}, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `DELETE FROM ce_members WHERE tenant_id = $1 AND workspace_id = $2 AND user_id = $3`,
			scope.TenantID, scope.WorkspaceID, userID)
		if err := requireOne(tag, err, "member "+userID); err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `UPDATE ce_tokens SET revoked_at = $4
WHERE tenant_id = $1 AND workspace_id = $2 AND user_id = $3 AND revoked_at IS NULL`, scope.TenantID, scope.WorkspaceID, userID, at)
		return err
	})
}

const tokenColumns = `id, tenant_id, workspace_id, hash, name, user_id, roles, created_by, created_at, expires_at, revoked_at, last_used_at`

func scanToken(row scanner) (store.Token, error) {
	var t store.Token
	var roles []string
	if err := row.Scan(&t.ID, &t.Scope.TenantID, &t.Scope.WorkspaceID, &t.Hash, &t.Name, &t.UserID, &roles, &t.CreatedBy,
		&t.CreatedAt, &t.ExpiresAt, &t.RevokedAt, &t.LastUsedAt); err != nil {
		return store.Token{}, err
	}
	t.Roles, t.CreatedAt = textToRoles(roles), t.CreatedAt.UTC()
	t.ExpiresAt, t.RevokedAt, t.LastUsedAt = utcPtr(t.ExpiresAt), utcPtr(t.RevokedAt), utcPtr(t.LastUsedAt)
	return t, nil
}

func (s *Store) CreateToken(ctx context.Context, t store.Token, ev store.AuditEvent) error {
	err := s.inTx(ctx, []store.AuditEvent{ev}, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `INSERT INTO ce_tokens (`+tokenColumns+`) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)`,
			t.ID, t.Scope.TenantID, t.Scope.WorkspaceID, t.Hash, t.Name, t.UserID, rolesToText(t.Roles), t.CreatedBy,
			t.CreatedAt, t.ExpiresAt, t.RevokedAt, t.LastUsedAt)
		return err
	})
	if uniqueViolation(err) {
		return fmt.Errorf("%w: token", store.ErrExists)
	}
	return err
}

func (s *Store) TokenByHash(ctx context.Context, hash string) (store.Token, error) {
	t, err := scanToken(s.pool.QueryRow(ctx, `SELECT `+tokenColumns+` FROM ce_tokens WHERE hash = $1`, hash))
	return t, notFound(err, "token")
}

func (s *Store) Tokens(ctx context.Context, scope adapter.Scope) ([]store.Token, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+tokenColumns+` FROM ce_tokens WHERE tenant_id = $1 AND workspace_id = $2 ORDER BY created_at, id`,
		scope.TenantID, scope.WorkspaceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []store.Token{}
	for rows.Next() {
		t, err := scanToken(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

func (s *Store) RevokeToken(ctx context.Context, scope adapter.Scope, id string, at time.Time, ev store.AuditEvent) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var revoked *time.Time
	if err := tx.QueryRow(ctx, `SELECT revoked_at FROM ce_tokens WHERE id = $1 AND tenant_id = $2 AND workspace_id = $3 FOR UPDATE`,
		id, scope.TenantID, scope.WorkspaceID).Scan(&revoked); err != nil {
		return notFound(err, "token "+id)
	}
	if revoked != nil {
		return nil
	}
	if _, err := tx.Exec(ctx, `UPDATE ce_tokens SET revoked_at = $2 WHERE id = $1`, id, at); err != nil {
		return err
	}
	if err := appendAudit(ctx, tx, []store.AuditEvent{ev}); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s *Store) TouchToken(ctx context.Context, id string, at time.Time) error {
	tag, err := s.pool.Exec(ctx, `UPDATE ce_tokens SET last_used_at = $2 WHERE id = $1`, id, at)
	return requireOne(tag, err, "token "+id)
}

func (s *Store) HasActiveTokens(ctx context.Context, now time.Time) (bool, error) {
	var ok bool
	err := s.pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM ce_tokens WHERE revoked_at IS NULL AND (expires_at IS NULL OR expires_at > $1))`, now).Scan(&ok)
	return ok, err
}
