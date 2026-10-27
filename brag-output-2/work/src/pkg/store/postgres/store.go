// SPDX-License-Identifier: Apache-2.0

package postgres

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/nexops-one/compliance-engine/pkg/store"
	"github.com/nexops-one/compliance-engine/sdk/adapter"
)

var _ store.Store = (*Store)(nil)

type scanner interface{ Scan(dest ...any) error }

type execer interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
}

func notFound(err error, what string) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("%w: %s", store.ErrNotFound, what)
	}
	return err
}

func decodeRecord(data []byte) (adapter.Record, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	var rec adapter.Record
	if err := dec.Decode(&rec); err != nil {
		return nil, fmt.Errorf("decode stored record: %w", err)
	}
	return rec, nil
}

func rawOrNull(b json.RawMessage) json.RawMessage {
	if len(b) == 0 {
		return json.RawMessage("null")
	}
	return b
}

func scanRevision(row scanner, scope adapter.Scope) (store.Revision, error) {
	var r store.Revision
	var kind string
	if err := row.Scan(&r.Number, &r.IngestionID, &kind, &r.CreatedAt); err != nil {
		return store.Revision{}, err
	}
	r.Scope, r.Kind, r.SnapshotID, r.CreatedAt = scope, store.Kind(kind), store.SnapshotID(r.Number), r.CreatedAt.UTC()
	return r, nil
}

const revisionColumns = `number, ingestion_id, kind, created_at`

func (s *Store) CurrentRevision(ctx context.Context, scope adapter.Scope) (store.Revision, error) {
	row := s.pool.QueryRow(ctx, `SELECT `+revisionColumns+` FROM ce_revisions WHERE tenant_id = $1 AND workspace_id = $2 ORDER BY number DESC LIMIT 1`,
		scope.TenantID, scope.WorkspaceID)
	r, err := scanRevision(row, scope)
	if err != nil {
		return store.Revision{}, notFound(err, "revision")
	}
	return r, nil
}

func (s *Store) Revision(ctx context.Context, scope adapter.Scope, number int64) (store.Revision, error) {
	row := s.pool.QueryRow(ctx, `SELECT `+revisionColumns+` FROM ce_revisions WHERE tenant_id = $1 AND workspace_id = $2 AND number = $3`,
		scope.TenantID, scope.WorkspaceID, number)
	r, err := scanRevision(row, scope)
	if err != nil {
		return store.Revision{}, notFound(err, fmt.Sprintf("revision %d", number))
	}
	return r, nil
}

func (s *Store) Revisions(ctx context.Context, scope adapter.Scope) ([]store.Revision, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+revisionColumns+` FROM ce_revisions WHERE tenant_id = $1 AND workspace_id = $2 ORDER BY number`,
		scope.TenantID, scope.WorkspaceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []store.Revision{}
	for rows.Next() {
		r, err := scanRevision(rows, scope)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *Store) Records(ctx context.Context, scope adapter.Scope, revision int64) ([]store.RecordVersion, error) {
	out := []store.RecordVersion{}
	if revision == 0 {
		return out, nil
	}
	var exists bool
	if err := s.pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM ce_revisions WHERE tenant_id = $1 AND workspace_id = $2 AND number = $3)`,
		scope.TenantID, scope.WorkspaceID, revision).Scan(&exists); err != nil {
		return nil, err
	}
	if !exists {
		return nil, fmt.Errorf("%w: revision %d", store.ErrNotFound, revision)
	}
	rows, err := s.pool.Query(ctx, `SELECT entity, record_key, data, hash, schema_version, source_system, source_adapter,
  source_adapter_version, ingestion_id, source_record_ref, written_at
FROM ce_record_versions
WHERE tenant_id = $1 AND workspace_id = $2 AND valid_from <= $3 AND (valid_to IS NULL OR valid_to > $3)`,
		scope.TenantID, scope.WorkspaceID, revision)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var v store.RecordVersion
		var data []byte
		if err := rows.Scan(&v.Entity, &v.Key, &data, &v.Hash, &v.SchemaVersion, &v.Source.System, &v.Source.Adapter,
			&v.Source.AdapterVersion, &v.IngestionID, &v.SourceRecordRef, &v.WrittenAt); err != nil {
			return nil, err
		}
		if v.Data, err = decodeRecord(data); err != nil {
			return nil, err
		}
		v.WrittenAt = v.WrittenAt.UTC()
		out = append(out, v)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Entity != out[j].Entity {
			return out[i].Entity < out[j].Entity
		}
		return out[i].Key < out[j].Key
	})
	return out, nil
}

func (s *Store) Commit(ctx context.Context, c store.Commit) (store.Revision, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return store.Revision{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	t, w := c.Scope.TenantID, c.Scope.WorkspaceID
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`, t+"\x1f"+w); err != nil {
		return store.Revision{}, err
	}
	var current int64
	if err := tx.QueryRow(ctx, `SELECT coalesce(max(number), 0) FROM ce_revisions WHERE tenant_id = $1 AND workspace_id = $2`, t, w).Scan(&current); err != nil {
		return store.Revision{}, err
	}
	if c.ExpectedRevision != current {
		return store.Revision{}, fmt.Errorf("%w: expected revision %d, current is %d", store.ErrConflict, c.ExpectedRevision, current)
	}
	if len(c.RollsBack) > 0 {
		unique := map[string]bool{}
		for _, id := range c.RollsBack {
			unique[id] = true
		}
		var found int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM ce_ingestions WHERE tenant_id = $1 AND workspace_id = $2 AND id = ANY($3)`,
			t, w, c.RollsBack).Scan(&found); err != nil {
			return store.Revision{}, err
		}
		if found != len(unique) {
			return store.Revision{}, fmt.Errorf("%w: ingestion to roll back", store.ErrNotFound)
		}
	}
	number := current + 1
	var seq int64
	if err := tx.QueryRow(ctx, `SELECT coalesce(max(seq), 0) FROM ce_provenance WHERE tenant_id = $1 AND workspace_id = $2`, t, w).Scan(&seq); err != nil {
		return store.Revision{}, err
	}
	src := c.Ingestion.Source
	var closeEntities, closeKeys []string
	var versions, provenance [][]any
	for _, ch := range c.Changes {
		seq++
		ref, hash := "", ""
		if ch.Op != store.OpCreate {
			closeEntities = append(closeEntities, ch.Entity)
			closeKeys = append(closeKeys, ch.Key)
		}
		if ch.Op != store.OpDelete {
			v := ch.Version
			data, err := json.Marshal(v.Data)
			if err != nil {
				return store.Revision{}, err
			}
			versions = append(versions, []any{t, w, v.Entity, v.Key, number, json.RawMessage(data), v.Hash, v.SchemaVersion,
				v.Source.System, v.Source.Adapter, v.Source.AdapterVersion, v.IngestionID, v.SourceRecordRef, v.WrittenAt})
			ref, hash = v.SourceRecordRef, v.Hash
		}
		provenance = append(provenance, []any{t, w, seq, c.Ingestion.ID, number, ch.Entity, ch.Key, string(ch.Op),
			src.System, src.Adapter, src.AdapterVersion, ref, hash, ch.PreviousHash, c.At})
	}
	if len(closeKeys) > 0 {
		if _, err := tx.Exec(ctx, `UPDATE ce_record_versions SET valid_to = $3
WHERE tenant_id = $1 AND workspace_id = $2 AND valid_to IS NULL
  AND (entity, record_key) IN (SELECT * FROM unnest($4::text[], $5::text[]))`, t, w, number, closeEntities, closeKeys); err != nil {
			return store.Revision{}, err
		}
	}
	if len(versions) > 0 {
		if _, err := tx.CopyFrom(ctx, pgx.Identifier{"ce_record_versions"}, []string{"tenant_id", "workspace_id", "entity", "record_key",
			"valid_from", "data", "hash", "schema_version", "source_system", "source_adapter", "source_adapter_version", "ingestion_id",
			"source_record_ref", "written_at"}, pgx.CopyFromRows(versions)); err != nil {
			return store.Revision{}, err
		}
	}
	if len(provenance) > 0 {
		if _, err := tx.CopyFrom(ctx, pgx.Identifier{"ce_provenance"}, []string{"tenant_id", "workspace_id", "seq", "ingestion_id",
			"revision", "entity", "record_key", "op", "source_system", "source_adapter", "source_adapter_version", "source_record_ref",
			"content_hash", "previous_hash", "recorded_at"}, pgx.CopyFromRows(provenance)); err != nil {
			return store.Revision{}, err
		}
	}
	if _, err := tx.Exec(ctx, `INSERT INTO ce_revisions (tenant_id, workspace_id, number, ingestion_id, kind, created_at) VALUES ($1, $2, $3, $4, $5, $6)`,
		t, w, number, c.Ingestion.ID, string(c.Kind), c.At); err != nil {
		return store.Revision{}, err
	}
	ing := c.Ingestion
	ing.Scope, ing.RevisionBefore, ing.RevisionAfter, ing.RolledBackBy = c.Scope, current, number, ""
	if err := writeIngestion(ctx, tx, ing, false); err != nil {
		return store.Revision{}, err
	}
	if len(c.RollsBack) > 0 {
		if _, err := tx.Exec(ctx, `UPDATE ce_ingestions SET rolled_back_by = $3 WHERE tenant_id = $1 AND workspace_id = $2 AND id = ANY($4)`,
			t, w, ing.ID, c.RollsBack); err != nil {
			return store.Revision{}, err
		}
	}
	if err := appendAudit(ctx, tx, c.Audit); err != nil {
		return store.Revision{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return store.Revision{}, err
	}
	return store.Revision{Scope: c.Scope, Number: number, SnapshotID: store.SnapshotID(number), IngestionID: c.Ingestion.ID, Kind: c.Kind, CreatedAt: c.At.UTC()}, nil
}

func writeIngestion(ctx context.Context, q execer, ing store.Ingestion, upsert bool) error {
	sql := `INSERT INTO ce_ingestions (tenant_id, workspace_id, id, batch_id, source_system, source_adapter, source_adapter_version,
  mode, result, revision_before, revision_after, rolled_back_by, created_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)`
	if upsert {
		sql += ` ON CONFLICT (tenant_id, workspace_id, id) DO UPDATE SET batch_id = EXCLUDED.batch_id,
  source_system = EXCLUDED.source_system, source_adapter = EXCLUDED.source_adapter,
  source_adapter_version = EXCLUDED.source_adapter_version, mode = EXCLUDED.mode, result = EXCLUDED.result,
  revision_before = EXCLUDED.revision_before, revision_after = EXCLUDED.revision_after,
  rolled_back_by = EXCLUDED.rolled_back_by, created_at = EXCLUDED.created_at`
	}
	_, err := q.Exec(ctx, sql, ing.Scope.TenantID, ing.Scope.WorkspaceID, ing.ID, ing.BatchID, ing.Source.System, ing.Source.Adapter,
		ing.Source.AdapterVersion, string(ing.Mode), rawOrNull(ing.Result), ing.RevisionBefore, ing.RevisionAfter, ing.RolledBackBy, ing.CreatedAt)
	return err
}

func (s *Store) SaveIngestion(ctx context.Context, ing store.Ingestion) error {
	return writeIngestion(ctx, s.pool, ing, true)
}

func (s *Store) Ingestion(ctx context.Context, scope adapter.Scope, id string) (store.Ingestion, error) {
	ing := store.Ingestion{ID: id, Scope: scope}
	var mode string
	var result []byte
	err := s.pool.QueryRow(ctx, `SELECT batch_id, source_system, source_adapter, source_adapter_version, mode, result,
  revision_before, revision_after, rolled_back_by, created_at
FROM ce_ingestions WHERE tenant_id = $1 AND workspace_id = $2 AND id = $3`, scope.TenantID, scope.WorkspaceID, id).Scan(
		&ing.BatchID, &ing.Source.System, &ing.Source.Adapter, &ing.Source.AdapterVersion, &mode, &result,
		&ing.RevisionBefore, &ing.RevisionAfter, &ing.RolledBackBy, &ing.CreatedAt)
	if err != nil {
		return store.Ingestion{}, notFound(err, "ingestion "+id)
	}
	ing.Mode, ing.Result, ing.CreatedAt = adapter.Mode(mode), json.RawMessage(result), ing.CreatedAt.UTC()
	return ing, nil
}

func (s *Store) Provenance(ctx context.Context, scope adapter.Scope, entity, key string) ([]store.ProvenanceEntry, error) {
	rows, err := s.pool.Query(ctx, `SELECT seq, ingestion_id, revision, op, source_system, source_adapter, source_adapter_version,
  source_record_ref, content_hash, previous_hash, recorded_at
FROM ce_provenance WHERE tenant_id = $1 AND workspace_id = $2 AND entity = $3 AND record_key = $4 ORDER BY seq`,
		scope.TenantID, scope.WorkspaceID, entity, key)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []store.ProvenanceEntry{}
	for rows.Next() {
		p := store.ProvenanceEntry{Entity: entity, Key: key}
		var op string
		if err := rows.Scan(&p.Seq, &p.IngestionID, &p.Revision, &op, &p.Source.System, &p.Source.Adapter, &p.Source.AdapterVersion,
			&p.SourceRecordRef, &p.ContentHash, &p.PreviousHash, &p.RecordedAt); err != nil {
			return nil, err
		}
		p.Op, p.RecordedAt = store.Op(op), p.RecordedAt.UTC()
		out = append(out, p)
	}
	return out, rows.Err()
}

func (s *Store) PutManifest(ctx context.Context, scope adapter.Scope, m adapter.Manifest) error {
	data, err := json.Marshal(m)
	if err != nil {
		return err
	}
	_, err = s.pool.Exec(ctx, `INSERT INTO ce_manifests (tenant_id, workspace_id, name, manifest, updated_at) VALUES ($1, $2, $3, $4, $5)
ON CONFLICT (tenant_id, workspace_id, name) DO UPDATE SET manifest = EXCLUDED.manifest, updated_at = EXCLUDED.updated_at`,
		scope.TenantID, scope.WorkspaceID, m.Name, json.RawMessage(data), time.Now().UTC())
	return err
}

func (s *Store) Manifests(ctx context.Context, scope adapter.Scope) ([]adapter.Manifest, error) {
	rows, err := s.pool.Query(ctx, `SELECT manifest FROM ce_manifests WHERE tenant_id = $1 AND workspace_id = $2`, scope.TenantID, scope.WorkspaceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []adapter.Manifest{}
	for rows.Next() {
		var data []byte
		if err := rows.Scan(&data); err != nil {
			return nil, err
		}
		var m adapter.Manifest
		if err := json.Unmarshal(data, &m); err != nil {
			return nil, fmt.Errorf("decode stored manifest: %w", err)
		}
		out = append(out, m)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

func (s *Store) SaveEvaluation(ctx context.Context, e store.StoredEvaluation) error {
	catalogs := e.Catalogs
	if catalogs == nil {
		catalogs = []string{}
	}
	_, err := s.pool.Exec(ctx, `INSERT INTO ce_evaluations (tenant_id, workspace_id, id, snapshot_id, catalogs, result, created_at)
VALUES ($1, $2, $3, $4, $5, $6, $7)
ON CONFLICT (tenant_id, workspace_id, id) DO UPDATE SET snapshot_id = EXCLUDED.snapshot_id, catalogs = EXCLUDED.catalogs,
  result = EXCLUDED.result, created_at = EXCLUDED.created_at`,
		e.Scope.TenantID, e.Scope.WorkspaceID, e.ID, e.SnapshotID, catalogs, rawOrNull(e.Result), e.CreatedAt)
	return err
}

func (s *Store) Evaluation(ctx context.Context, scope adapter.Scope, id string) (store.StoredEvaluation, error) {
	e := store.StoredEvaluation{ID: id, Scope: scope}
	var result []byte
	err := s.pool.QueryRow(ctx, `SELECT snapshot_id, catalogs, result, created_at FROM ce_evaluations
WHERE tenant_id = $1 AND workspace_id = $2 AND id = $3`, scope.TenantID, scope.WorkspaceID, id).Scan(&e.SnapshotID, &e.Catalogs, &result, &e.CreatedAt)
	if err != nil {
		return store.StoredEvaluation{}, notFound(err, "evaluation "+id)
	}
	e.Result, e.CreatedAt = json.RawMessage(result), e.CreatedAt.UTC()
	return e, nil
}
