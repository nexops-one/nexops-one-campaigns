// SPDX-License-Identifier: Apache-2.0

package postgres

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/nexops-one/compliance-engine/pkg/store"
	"github.com/nexops-one/compliance-engine/sdk/adapter"
)

func (s *Store) SaveReport(ctx context.Context, r store.Report, files []store.ReportFile, events ...store.AuditEvent) error {
	t, w := r.Scope.TenantID, r.Scope.WorkspaceID
	meta := store.CloneReport(r)
	meta.Facts, meta.Validation = nil, nil
	doc, err := json.Marshal(meta)
	if err != nil {
		return err
	}
	var validation []byte
	if r.Validation != nil {
		validation = r.Validation
	}
	err = s.inTx(ctx, events, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `INSERT INTO ce_reports (tenant_id, workspace_id, id, doc, facts, validation, created_at) VALUES ($1, $2, $3, $4, $5, $6, $7)`,
			t, w, r.ID, doc, []byte(r.Facts), validation, r.CreatedAt); err != nil {
			return err
		}
		for _, f := range files {
			if _, err := tx.Exec(ctx, `INSERT INTO ce_report_files (tenant_id, workspace_id, report_id, name, content_type, sha256, content)
VALUES ($1, $2, $3, $4, $5, $6, $7)`, t, w, r.ID, f.Name, f.ContentType, f.SHA256, f.Data); err != nil {
				return err
			}
		}
		return nil
	})
	if uniqueViolation(err) {
		return fmt.Errorf("%w: report %s", store.ErrExists, r.ID)
	}
	return err
}

func (s *Store) Report(ctx context.Context, scope adapter.Scope, id string) (store.Report, error) {
	var doc, facts, validation []byte
	err := s.pool.QueryRow(ctx, `SELECT doc, facts, validation FROM ce_reports WHERE tenant_id = $1 AND workspace_id = $2 AND id = $3`,
		scope.TenantID, scope.WorkspaceID, id).Scan(&doc, &facts, &validation)
	if err != nil {
		return store.Report{}, notFound(err, "report "+id)
	}
	r, err := decodeDoc[store.Report](doc, "report")
	if err != nil {
		return store.Report{}, err
	}
	r.Facts = facts
	if validation != nil {
		r.Validation = validation
	}
	return r, nil
}

func (s *Store) Reports(ctx context.Context, scope adapter.Scope) ([]store.Report, error) {
	rows, err := s.pool.Query(ctx, `SELECT doc FROM ce_reports WHERE tenant_id = $1 AND workspace_id = $2 ORDER BY created_at DESC, id`,
		scope.TenantID, scope.WorkspaceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []store.Report{}
	for rows.Next() {
		var doc []byte
		if err := rows.Scan(&doc); err != nil {
			return nil, err
		}
		r, err := decodeDoc[store.Report](doc, "report")
		if err != nil {
			return nil, err
		}
		r.Inputs = nil
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *Store) ReportFile(ctx context.Context, scope adapter.Scope, id, name string) (store.ReportFile, error) {
	var f store.ReportFile
	err := s.pool.QueryRow(ctx, `SELECT name, content_type, sha256, content FROM ce_report_files
WHERE tenant_id = $1 AND workspace_id = $2 AND report_id = $3 AND name = $4`, scope.TenantID, scope.WorkspaceID, id, name).
		Scan(&f.Name, &f.ContentType, &f.SHA256, &f.Data)
	if err != nil {
		return store.ReportFile{}, notFound(err, "report "+id+" file "+name)
	}
	f.Size = int64(len(f.Data))
	return f, nil
}

func (s *Store) ReportRefs(ctx context.Context, scope adapter.Scope) ([]store.ReportRef, error) {
	rows, err := s.pool.Query(ctx, `SELECT id, doc->>'evaluation_id', doc->>'snapshot_id', created_at FROM ce_reports
WHERE tenant_id = $1 AND workspace_id = $2 ORDER BY id`, scope.TenantID, scope.WorkspaceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []store.ReportRef{}
	for rows.Next() {
		var r store.ReportRef
		if err := rows.Scan(&r.ID, &r.EvaluationID, &r.SnapshotID, &r.CreatedAt); err != nil {
			return nil, err
		}
		r.CreatedAt = r.CreatedAt.UTC()
		out = append(out, r)
	}
	return out, rows.Err()
}
