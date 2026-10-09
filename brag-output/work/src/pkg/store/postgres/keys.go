// SPDX-License-Identifier: Apache-2.0

package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/nexops-one/compliance-engine/pkg/store"
	"github.com/nexops-one/compliance-engine/sdk/adapter"
)

func (s *Store) TenantKeys(ctx context.Context, tenant string) (store.TenantKeys, error) {
	var doc []byte
	if err := s.pool.QueryRow(ctx, `SELECT doc FROM ce_tenant_keys WHERE tenant_id = $1`, tenant).Scan(&doc); err != nil {
		return store.TenantKeys{}, notFound(err, "keys of tenant "+tenant)
	}
	return decodeDoc[store.TenantKeys](doc, "tenant keys")
}

func (s *Store) PutTenantKeys(ctx context.Context, k store.TenantKeys, expected int64, events ...store.AuditEvent) (store.TenantKeys, error) {
	k.Version = expected + 1
	err := s.inTx(ctx, events, func(tx pgx.Tx) error {
		stored, found, err := lockedVersion(ctx, tx, `SELECT version FROM ce_tenant_keys WHERE tenant_id = $1 FOR UPDATE`, k.TenantID)
		if err != nil {
			return err
		}
		if (expected == 0 && found) || (expected != 0 && (!found || stored != expected)) {
			return fmt.Errorf("%w: keys of tenant %s changed", store.ErrConflict, k.TenantID)
		}
		doc, err := json.Marshal(k)
		if err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `INSERT INTO ce_tenant_keys (tenant_id, doc, version) VALUES ($1, $2, $3)
ON CONFLICT (tenant_id) DO UPDATE SET doc = EXCLUDED.doc, version = EXCLUDED.version`, k.TenantID, doc, k.Version)
		if uniqueViolation(err) {
			return fmt.Errorf("%w: keys of tenant %s changed", store.ErrConflict, k.TenantID)
		}
		return err
	})
	if err != nil {
		return store.TenantKeys{}, err
	}
	return s.TenantKeys(ctx, k.TenantID)
}

func (s *Store) DeleteTenantKeys(ctx context.Context, tenant string, events ...store.AuditEvent) error {
	return s.inTx(ctx, events, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `DELETE FROM ce_tenant_keys WHERE tenant_id = $1`, tenant)
		return err
	})
}

func (s *Store) KeyTenants(ctx context.Context) ([]string, error) {
	rows, err := s.pool.Query(ctx, `SELECT tenant_id FROM ce_tenant_keys ORDER BY tenant_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var t string
		if err := rows.Scan(&t); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

func (s *Store) Settings(ctx context.Context, scope adapter.Scope) (store.Settings, error) {
	var doc []byte
	err := s.pool.QueryRow(ctx, `SELECT doc FROM ce_settings WHERE tenant_id = $1 AND workspace_id = $2`, scope.TenantID, scope.WorkspaceID).Scan(&doc)
	if errors.Is(err, pgx.ErrNoRows) {
		return store.DefaultSettings(scope), nil
	}
	if err != nil {
		return store.Settings{}, err
	}
	return decodeDoc[store.Settings](doc, "settings")
}

func (s *Store) PutSettings(ctx context.Context, st store.Settings, expected int64, events ...store.AuditEvent) (store.Settings, error) {
	st.Version = expected + 1
	if st.ReviewOverrides == nil {
		st.ReviewOverrides = map[string]store.ReviewOverride{}
	}
	err := s.inTx(ctx, events, func(tx pgx.Tx) error {
		stored, found, err := lockedVersion(ctx, tx, `SELECT version FROM ce_settings WHERE tenant_id = $1 AND workspace_id = $2 FOR UPDATE`,
			st.Scope.TenantID, st.Scope.WorkspaceID)
		if err != nil {
			return err
		}
		if (expected == 0 && found) || (expected != 0 && (!found || stored != expected)) {
			return fmt.Errorf("%w: settings changed since they were read", store.ErrConflict)
		}
		doc, err := json.Marshal(st)
		if err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `INSERT INTO ce_settings (tenant_id, workspace_id, doc, version) VALUES ($1, $2, $3, $4)
ON CONFLICT (tenant_id, workspace_id) DO UPDATE SET doc = EXCLUDED.doc, version = EXCLUDED.version`,
			st.Scope.TenantID, st.Scope.WorkspaceID, doc, st.Version)
		return err
	})
	if err != nil {
		return store.Settings{}, err
	}
	return s.Settings(ctx, st.Scope)
}
