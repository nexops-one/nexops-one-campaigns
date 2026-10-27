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

func decodeDoc[T any](data []byte, what string) (T, error) {
	var v T
	if err := json.Unmarshal(data, &v); err != nil {
		return v, fmt.Errorf("decode stored %s: %w", what, err)
	}
	return v, nil
}

// lockedVersion reads a document's version with a row lock; found is false
// when the row does not exist.
func lockedVersion(ctx context.Context, tx pgx.Tx, sql string, args ...any) (int64, bool, error) {
	var v int64
	err := tx.QueryRow(ctx, sql, args...).Scan(&v)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, false, nil
	}
	return v, err == nil, err
}

func (s *Store) PutEvidence(ctx context.Context, e store.Evidence, expected int64, events ...store.AuditEvent) (store.Evidence, error) {
	t, w := e.Scope.TenantID, e.Scope.WorkspaceID
	e.Version = expected + 1
	err := s.inTx(ctx, events, func(tx pgx.Tx) error {
		stored, found, err := lockedVersion(ctx, tx, `SELECT version FROM ce_evidence WHERE tenant_id = $1 AND workspace_id = $2 AND id = $3 FOR UPDATE`, t, w, e.ID)
		if err != nil {
			return err
		}
		switch {
		case expected == 0 && found:
			return fmt.Errorf("%w: evidence %s", store.ErrExists, e.ID)
		case expected != 0 && (!found || stored != expected):
			return fmt.Errorf("%w: evidence %s changed since it was read", store.ErrConflict, e.ID)
		}
		doc, err := json.Marshal(e)
		if err != nil {
			return err
		}
		if expected == 0 {
			_, err = tx.Exec(ctx, `INSERT INTO ce_evidence (tenant_id, workspace_id, id, doc, version, created_at) VALUES ($1, $2, $3, $4, $5, $6)`,
				t, w, e.ID, doc, e.Version, e.CreatedAt)
		} else {
			_, err = tx.Exec(ctx, `UPDATE ce_evidence SET doc = $4, version = $5 WHERE tenant_id = $1 AND workspace_id = $2 AND id = $3`,
				t, w, e.ID, doc, e.Version)
		}
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `DELETE FROM ce_evidence_links WHERE tenant_id = $1 AND workspace_id = $2 AND evidence_id = $3`, t, w, e.ID); err != nil {
			return err
		}
		for _, l := range e.Links {
			if _, err := tx.Exec(ctx, `INSERT INTO ce_evidence_links (tenant_id, workspace_id, evidence_id, catalog, control_id)
VALUES ($1, $2, $3, $4, $5) ON CONFLICT DO NOTHING`, t, w, e.ID, l.Catalog, l.ControlID); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return store.Evidence{}, err
	}
	return s.Evidence(ctx, e.Scope, e.ID)
}

func (s *Store) Evidence(ctx context.Context, scope adapter.Scope, id string) (store.Evidence, error) {
	var doc []byte
	err := s.pool.QueryRow(ctx, `SELECT doc FROM ce_evidence WHERE tenant_id = $1 AND workspace_id = $2 AND id = $3`,
		scope.TenantID, scope.WorkspaceID, id).Scan(&doc)
	if err != nil {
		return store.Evidence{}, notFound(err, "evidence "+id)
	}
	return decodeDoc[store.Evidence](doc, "evidence")
}

func (s *Store) ListEvidence(ctx context.Context, scope adapter.Scope, q store.EvidenceQuery) ([]store.Evidence, error) {
	rows, err := s.pool.Query(ctx, `SELECT e.doc FROM ce_evidence e
WHERE e.tenant_id = $1 AND e.workspace_id = $2
  AND (($3 = '' AND $4 = '') OR EXISTS (SELECT 1 FROM ce_evidence_links l
       WHERE l.tenant_id = e.tenant_id AND l.workspace_id = e.workspace_id AND l.evidence_id = e.id
         AND ($3 = '' OR l.catalog = $3) AND ($4 = '' OR l.control_id = $4)))
ORDER BY e.created_at, e.id`, scope.TenantID, scope.WorkspaceID, q.Catalog, q.ControlID)
	if err != nil {
		return nil, err
	}
	return collectDocs[store.Evidence](rows, "evidence")
}

func collectDocs[T any](rows pgx.Rows, what string) ([]T, error) {
	defer rows.Close()
	out := []T{}
	for rows.Next() {
		var doc []byte
		if err := rows.Scan(&doc); err != nil {
			return nil, err
		}
		v, err := decodeDoc[T](doc, what)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

func (s *Store) Assessment(ctx context.Context, scope adapter.Scope, catalog, controlID string) (store.Assessment, error) {
	var doc []byte
	err := s.pool.QueryRow(ctx, `SELECT doc FROM ce_assessments WHERE tenant_id = $1 AND workspace_id = $2 AND catalog = $3 AND control_id = $4`,
		scope.TenantID, scope.WorkspaceID, catalog, controlID).Scan(&doc)
	if err != nil {
		return store.Assessment{}, notFound(err, "assessment "+catalog+"/"+controlID)
	}
	return decodeDoc[store.Assessment](doc, "assessment")
}

func (s *Store) Assessments(ctx context.Context, scope adapter.Scope, catalog string) ([]store.Assessment, error) {
	rows, err := s.pool.Query(ctx, `SELECT doc FROM ce_assessments WHERE tenant_id = $1 AND workspace_id = $2 AND ($3 = '' OR catalog = $3)
ORDER BY catalog, control_id`, scope.TenantID, scope.WorkspaceID, catalog)
	if err != nil {
		return nil, err
	}
	return collectDocs[store.Assessment](rows, "assessment")
}

func (s *Store) SaveAssessment(ctx context.Context, a store.Assessment, expected int64, tr *store.Transition, events ...store.AuditEvent) (store.Assessment, error) {
	t, w := a.Scope.TenantID, a.Scope.WorkspaceID
	a.Version = expected + 1
	err := s.inTx(ctx, events, func(tx pgx.Tx) error {
		// One lock per workspace serializes versions and history sequence numbers.
		if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended('assessments' || chr(31) || $1 || chr(31) || $2, 0))`, t, w); err != nil {
			return err
		}
		stored, found, err := lockedVersion(ctx, tx, `SELECT version FROM ce_assessments WHERE tenant_id = $1 AND workspace_id = $2 AND catalog = $3 AND control_id = $4`,
			t, w, a.Catalog, a.ControlID)
		if err != nil {
			return err
		}
		if (expected == 0 && found) || (expected != 0 && (!found || stored != expected)) {
			return fmt.Errorf("%w: assessment %s/%s changed since it was read", store.ErrConflict, a.Catalog, a.ControlID)
		}
		doc, err := json.Marshal(a)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO ce_assessments (tenant_id, workspace_id, catalog, control_id, doc, version) VALUES ($1, $2, $3, $4, $5, $6)
ON CONFLICT (tenant_id, workspace_id, catalog, control_id) DO UPDATE SET doc = EXCLUDED.doc, version = EXCLUDED.version`,
			t, w, a.Catalog, a.ControlID, doc, a.Version); err != nil {
			return err
		}
		if tr == nil {
			return nil
		}
		var seq int64
		if err := tx.QueryRow(ctx, `SELECT coalesce(max(seq), 0) + 1 FROM ce_assessment_history WHERE tenant_id = $1 AND workspace_id = $2`, t, w).Scan(&seq); err != nil {
			return err
		}
		h := *tr
		h.Seq, h.Catalog, h.ControlID = seq, a.Catalog, a.ControlID
		hdoc, err := json.Marshal(h)
		if err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `INSERT INTO ce_assessment_history (tenant_id, workspace_id, seq, catalog, control_id, doc) VALUES ($1, $2, $3, $4, $5, $6)`,
			t, w, seq, a.Catalog, a.ControlID, hdoc)
		return err
	})
	if err != nil {
		return store.Assessment{}, err
	}
	return s.Assessment(ctx, a.Scope, a.Catalog, a.ControlID)
}

func (s *Store) History(ctx context.Context, scope adapter.Scope, catalog, controlID string) ([]store.Transition, error) {
	rows, err := s.pool.Query(ctx, `SELECT doc FROM ce_assessment_history WHERE tenant_id = $1 AND workspace_id = $2 AND catalog = $3 AND control_id = $4 ORDER BY seq`,
		scope.TenantID, scope.WorkspaceID, catalog, controlID)
	if err != nil {
		return nil, err
	}
	return collectDocs[store.Transition](rows, "transition")
}
