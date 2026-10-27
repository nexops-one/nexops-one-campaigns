// SPDX-License-Identifier: Apache-2.0

package postgres

import (
	"context"

	"github.com/jackc/pgx/v5"

	"github.com/nexops-one/compliance-engine/pkg/store"
)

// tenantTables are deleted in this order (children before parents).
var tenantTables = []string{
	"ce_evidence_links", "ce_evidence", "ce_assessment_history", "ce_assessments", "ce_settings",
	"ce_record_versions", "ce_provenance", "ce_ingestions", "ce_revisions", "ce_manifests", "ce_report_files", "ce_reports", "ce_evaluations",
	"ce_sessions", "ce_tokens", "ce_members", "ce_users", "ce_workspaces",
}

func (s *Store) DeleteTenantData(ctx context.Context, tenant string, events ...store.AuditEvent) (int, error) {
	total := 0
	err := s.inTx(ctx, events, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `SELECT set_config('compliance.tenant_delete', $1, true)`, tenant); err != nil {
			return err
		}
		for _, table := range tenantTables {
			tag, err := tx.Exec(ctx, `DELETE FROM `+table+` WHERE tenant_id = $1`, tenant)
			if err != nil {
				return err
			}
			total += int(tag.RowsAffected())
		}
		_, err := tx.Exec(ctx, `SELECT set_config('compliance.tenant_delete', '', true)`)
		return err
	})
	return total, err
}
