// SPDX-License-Identifier: Apache-2.0

package postgres

import (
	"context"
	"encoding/json"

	"github.com/jackc/pgx/v5"

	"github.com/nexops-one/compliance-engine/sdk/adapter"
)

// RewriteFunc returns the new stored data and content hash of one record version.
type RewriteFunc = func(scope adapter.Scope, entity string, data adapter.Record) (adapter.Record, string, error)

// RewriteRecords rewrites the data and hash of every record version of a
// tenant, one transaction per workspace, and maps provenance content hashes
// through the same old-to-new hash mapping. It is used to seal data written
// before encryption at rest was enabled. It returns the rewritten row count.
func (s *Store) RewriteRecords(ctx context.Context, tenant string, fn RewriteFunc) (int, error) {
	rows, err := s.pool.Query(ctx, `SELECT DISTINCT workspace_id FROM ce_record_versions WHERE tenant_id = $1 ORDER BY workspace_id`, tenant)
	if err != nil {
		return 0, err
	}
	var workspaces []string
	for rows.Next() {
		var w string
		if err := rows.Scan(&w); err != nil {
			rows.Close()
			return 0, err
		}
		workspaces = append(workspaces, w)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, err
	}
	total := 0
	for _, w := range workspaces {
		n, err := s.rewriteWorkspace(ctx, adapter.Scope{TenantID: tenant, WorkspaceID: w}, fn)
		if err != nil {
			return total, err
		}
		total += n
	}
	return total, nil
}

func (s *Store) rewriteWorkspace(ctx context.Context, scope adapter.Scope, fn RewriteFunc) (int, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	t, w := scope.TenantID, scope.WorkspaceID
	// The revision lock keeps ingestions out while hashes change.
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`, t+"\x1f"+w); err != nil {
		return 0, err
	}
	rows, err := tx.Query(ctx, `SELECT entity, record_key, valid_from, data, hash FROM ce_record_versions
WHERE tenant_id = $1 AND workspace_id = $2`, t, w)
	if err != nil {
		return 0, err
	}
	type row struct {
		entity, key, hash string
		from              int64
		data              []byte
	}
	var all []row
	for rows.Next() {
		var r row
		if err := rows.Scan(&r.entity, &r.key, &r.from, &r.data, &r.hash); err != nil {
			rows.Close()
			return 0, err
		}
		all = append(all, r)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, err
	}
	hashes := map[string]string{}
	batch := &pgx.Batch{}
	for _, r := range all {
		rec, err := decodeRecord(r.data)
		if err != nil {
			return 0, err
		}
		next, hash, err := fn(scope, r.entity, rec)
		if err != nil {
			return 0, err
		}
		data, err := json.Marshal(next)
		if err != nil {
			return 0, err
		}
		hashes[r.hash] = hash
		batch.Queue(`UPDATE ce_record_versions SET data = $6, hash = $7
WHERE tenant_id = $1 AND workspace_id = $2 AND entity = $3 AND record_key = $4 AND valid_from = $5`, t, w, r.entity, r.key, r.from, json.RawMessage(data), hash)
	}
	for old, next := range hashes {
		batch.Queue(`UPDATE ce_provenance SET content_hash = $4 WHERE tenant_id = $1 AND workspace_id = $2 AND content_hash = $3`, t, w, old, next)
		batch.Queue(`UPDATE ce_provenance SET previous_hash = $4 WHERE tenant_id = $1 AND workspace_id = $2 AND previous_hash = $3`, t, w, old, next)
	}
	if batch.Len() > 0 {
		if err := tx.SendBatch(ctx, batch).Close(); err != nil {
			return 0, err
		}
	}
	return len(all), tx.Commit(ctx)
}
