// SPDX-License-Identifier: Apache-2.0

package store

import (
	"context"
	"time"
)

// Session is a console sign-in. Only the SHA-256 hash of the session value
// is stored. A session is bound to one workspace of its user's tenant.
type Session struct {
	Hash        string    `json:"-"`
	TenantID    string    `json:"tenant_id"`
	UserID      string    `json:"user_id"`
	WorkspaceID string    `json:"workspace_id"`
	CreatedAt   time.Time `json:"created_at"`
	LastSeenAt  time.Time `json:"last_seen_at"`
	ExpiresAt   time.Time `json:"expires_at"` // absolute expiry
}

// SessionStore stores console sessions. Sessions are not sensitive data and
// are never sealed.
type SessionStore interface {
	// CreateSession fails with ErrExists when the hash exists. The audit
	// event (login.success) is appended in the same transaction.
	CreateSession(ctx context.Context, s Session, ev AuditEvent) error
	SessionByHash(ctx context.Context, hash string) (Session, error)
	// TouchSession records activity; ErrNotFound when the session is gone.
	TouchSession(ctx context.Context, hash string, at time.Time) error
	// SetSessionWorkspace binds the session to another workspace.
	SetSessionWorkspace(ctx context.Context, hash, workspaceID string) error
	// DeleteSession removes a session (sign-out) with its optional audit
	// events; deleting an unknown session changes nothing and appends nothing.
	DeleteSession(ctx context.Context, hash string, events ...AuditEvent) error
	// DeleteExpiredSessions removes sessions whose absolute expiry or idle
	// cut-off (LastSeenAt before idleBefore) has passed, and counts them.
	DeleteExpiredSessions(ctx context.Context, now, idleBefore time.Time) (int, error)
}
