// SPDX-License-Identifier: Apache-2.0

package postgres

import (
	"context"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"regexp"
	"sort"
	"strconv"
)

//go:embed migrations/*.sql
var migrationFiles embed.FS

var (
	ErrPendingMigrations = errors.New("database schema has pending migrations; run 'compliance-engine migrate up' or set COMPLIANCE_AUTO_MIGRATE=true")
	ErrSchemaTooNew      = errors.New("database schema is newer than this engine; upgrade the engine or run 'compliance-engine migrate down' with the newer release")
)

// migrationLock serializes migrations across processes (pg_advisory_xact_lock key).
const migrationLock int64 = 7300422

const createMigrationTable = `CREATE TABLE IF NOT EXISTS ce_schema_migrations (
  version    INT PRIMARY KEY,
  name       TEXT NOT NULL,
  applied_at TIMESTAMPTZ NOT NULL DEFAULT now()
)`

// Migration is one versioned schema change with its rollback.
type Migration struct {
	Version int
	Name    string
	Up      string
	Down    string
}

var migrationName = regexp.MustCompile(`^(\d{4})_([a-z0-9_]+)\.(up|down)\.sql$`)

// Migrations returns the embedded migrations in ascending order. Versions
// must be contiguous from 1 and each must have an up and a down script.
func Migrations() ([]Migration, error) {
	entries, err := fs.ReadDir(migrationFiles, "migrations")
	if err != nil {
		return nil, err
	}
	byVersion := map[int]*Migration{}
	for _, e := range entries {
		m := migrationName.FindStringSubmatch(e.Name())
		if m == nil {
			return nil, fmt.Errorf("migration file %s: name must be NNNN_name.up.sql or NNNN_name.down.sql", e.Name())
		}
		v, _ := strconv.Atoi(m[1])
		body, err := fs.ReadFile(migrationFiles, "migrations/"+e.Name())
		if err != nil {
			return nil, err
		}
		mig := byVersion[v]
		if mig == nil {
			mig = &Migration{Version: v, Name: m[2]}
			byVersion[v] = mig
		}
		if m[3] == "up" {
			mig.Up = string(body)
		} else {
			mig.Down = string(body)
		}
	}
	out := make([]Migration, 0, len(byVersion))
	for _, m := range byVersion {
		out = append(out, *m)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Version < out[j].Version })
	for i, m := range out {
		if m.Version != i+1 {
			return nil, fmt.Errorf("migration versions must be contiguous from 1; found %04d at position %d", m.Version, i+1)
		}
		if m.Up == "" || m.Down == "" {
			return nil, fmt.Errorf("migration %04d_%s needs both an up and a down script", m.Version, m.Name)
		}
	}
	return out, nil
}

// MigrationStatus returns the applied and the latest available schema versions.
func (s *Store) MigrationStatus(ctx context.Context) (current, latest int, err error) {
	ms, err := Migrations()
	if err != nil {
		return 0, 0, err
	}
	if _, err := s.pool.Exec(ctx, createMigrationTable); err != nil {
		return 0, 0, err
	}
	if err := s.pool.QueryRow(ctx, `SELECT coalesce(max(version), 0) FROM ce_schema_migrations`).Scan(&current); err != nil {
		return 0, 0, err
	}
	return current, len(ms), nil
}

// CheckSchema fails when the schema is behind or ahead of this engine.
func (s *Store) CheckSchema(ctx context.Context) error {
	cur, lat, err := s.MigrationStatus(ctx)
	if err != nil {
		return err
	}
	switch {
	case cur < lat:
		return fmt.Errorf("%w (schema version %d, latest %d)", ErrPendingMigrations, cur, lat)
	case cur > lat:
		return fmt.Errorf("%w (schema version %d, engine supports %d)", ErrSchemaTooNew, cur, lat)
	}
	return nil
}

// MigrateUp applies every pending migration and returns the applied versions.
func (s *Store) MigrateUp(ctx context.Context) ([]int, error) {
	ms, err := Migrations()
	if err != nil {
		return nil, err
	}
	applied := []int{}
	for _, m := range ms {
		changed, err := s.apply(ctx, m, true, downOptions{})
		if err != nil {
			return applied, err
		}
		if changed {
			applied = append(applied, m.Version)
		}
	}
	return applied, nil
}

// DownOption changes how MigrateDown reverts migrations.
type DownOption func(*downOptions)

type downOptions struct{ forceDropAudit bool }

// ForceDropAudit lets the audit-log migration drop a table that holds events.
// Without it that down migration refuses, so audit history is never lost by accident.
func ForceDropAudit() DownOption { return func(o *downOptions) { o.forceDropAudit = true } }

// MigrateDown reverts the latest `steps` migrations and returns their versions.
func (s *Store) MigrateDown(ctx context.Context, steps int, opts ...DownOption) ([]int, error) {
	var o downOptions
	for _, opt := range opts {
		opt(&o)
	}
	if steps < 1 {
		return nil, errors.New("migrate down: steps must be at least 1")
	}
	ms, err := Migrations()
	if err != nil {
		return nil, err
	}
	reverted := []int{}
	for i := 0; i < steps; i++ {
		cur, _, err := s.MigrationStatus(ctx)
		if err != nil {
			return reverted, err
		}
		if cur == 0 {
			break
		}
		if cur > len(ms) {
			return reverted, fmt.Errorf("%w (schema version %d)", ErrSchemaTooNew, cur)
		}
		if _, err := s.apply(ctx, ms[cur-1], false, o); err != nil {
			return reverted, err
		}
		reverted = append(reverted, cur)
	}
	return reverted, nil
}

// apply runs one migration in its own transaction under the migration lock.
// It returns false when there was nothing to do.
func (s *Store) apply(ctx context.Context, m Migration, up bool, o downOptions) (bool, error) {
	if _, err := s.pool.Exec(ctx, createMigrationTable); err != nil {
		return false, err
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock($1)`, migrationLock); err != nil {
		return false, err
	}
	var applied bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM ce_schema_migrations WHERE version = $1)`, m.Version).Scan(&applied); err != nil {
		return false, err
	}
	if applied == up {
		return false, nil
	}
	script, direction := m.Up, "up"
	if !up {
		script, direction = m.Down, "down"
		if o.forceDropAudit {
			if _, err := tx.Exec(ctx, `SELECT set_config('compliance.force_drop_audit', 'on', true)`); err != nil {
				return false, err
			}
		}
	}
	if _, err := tx.Exec(ctx, script); err != nil {
		return false, fmt.Errorf("migration %04d_%s %s: %w", m.Version, m.Name, direction, err)
	}
	if up {
		_, err = tx.Exec(ctx, `INSERT INTO ce_schema_migrations (version, name) VALUES ($1, $2)`, m.Version, m.Name)
	} else {
		_, err = tx.Exec(ctx, `DELETE FROM ce_schema_migrations WHERE version = $1`, m.Version)
	}
	if err != nil {
		return false, err
	}
	return true, tx.Commit(ctx)
}
