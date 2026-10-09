// SPDX-License-Identifier: Apache-2.0

package postgres_test

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/nexops-one/compliance-engine/pkg/store/postgres"
	"github.com/nexops-one/compliance-engine/pkg/store/postgres/pgtest"
	"github.com/nexops-one/compliance-engine/sdk/adapter"
)

var ctx = context.Background()

// open connects to a fresh, empty schema (unmigrated).
func open(t *testing.T) (*postgres.Store, string) {
	t.Helper()
	url := pgtest.URL(t)
	s, err := postgres.Open(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	return s, url
}

// migrated connects to a fresh schema with every migration applied.
func migrated(t *testing.T) (*postgres.Store, string) {
	t.Helper()
	s, url := open(t)
	if _, err := s.MigrateUp(ctx); err != nil {
		t.Fatal(err)
	}
	return s, url
}

func TestMigrationsAreWellFormed(t *testing.T) {
	ms, err := postgres.Migrations()
	if err != nil {
		t.Fatal(err)
	}
	names := []string{}
	for _, m := range ms {
		names = append(names, m.Name)
	}
	want := []string{"init", "audit", "identity", "evidence_workflow", "keys_settings", "retention", "tenant_delete", "reports", "sessions",
		"external_users", "workspaces"}
	if strings.Join(names, ",") != strings.Join(want, ",") ||
		!strings.Contains(ms[0].Up, "CREATE TABLE ce_revisions") || !strings.Contains(ms[0].Down, "DROP TABLE") ||
		!strings.Contains(ms[1].Up, "ce_audit_append_only") || !strings.Contains(ms[2].Up, "CREATE TABLE ce_tokens") {
		t.Fatalf("migrations = %+v", ms)
	}
}

func TestMigrateUpDownUp(t *testing.T) {
	s, _ := open(t)
	scope := adapter.Scope{TenantID: "t", WorkspaceID: "w"}
	ms, _ := postgres.Migrations()
	latest := len(ms)
	up, down := []int{}, []int{}
	for i := 1; i <= latest; i++ {
		up, down = append(up, i), append([]int{i}, down...)
	}
	status := func(wantCur, wantLat int) {
		t.Helper()
		cur, lat, err := s.MigrationStatus(ctx)
		if err != nil || cur != wantCur || lat != wantLat {
			t.Fatalf("status = %d/%d, %v; want %d/%d", cur, lat, err, wantCur, wantLat)
		}
	}
	status(0, latest)
	if err := s.CheckSchema(ctx); !errors.Is(err, postgres.ErrPendingMigrations) {
		t.Fatalf("CheckSchema on an empty schema = %v", err)
	}
	if got, err := s.MigrateUp(ctx); err != nil || !reflect.DeepEqual(got, up) {
		t.Fatalf("up = %v, %v", got, err)
	}
	if got, err := s.MigrateUp(ctx); err != nil || len(got) != 0 {
		t.Fatalf("second up = %v, %v", got, err)
	}
	status(latest, latest)
	if err := s.CheckSchema(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Revisions(ctx, scope); err != nil {
		t.Fatalf("tables must exist after up: %v", err)
	}
	if got, err := s.MigrateDown(ctx, latest); err != nil || !reflect.DeepEqual(got, down) {
		t.Fatalf("down = %v, %v", got, err)
	}
	status(0, latest)
	if _, err := s.Revisions(ctx, scope); err == nil {
		t.Fatal("tables must be gone after down")
	}
	if got, err := s.MigrateUp(ctx); err != nil || !reflect.DeepEqual(got, up) {
		t.Fatalf("up after down = %v, %v", got, err)
	}
	if _, err := s.Revisions(ctx, scope); err != nil {
		t.Fatalf("tables must exist again: %v", err)
	}
	if _, err := s.MigrateDown(ctx, 0); err == nil {
		t.Fatal("down 0 must be rejected")
	}
}

func TestOpenReportsUnreachableDatabase(t *testing.T) {
	if _, err := postgres.Open(ctx, "postgres://nobody:secret@127.0.0.1:1/none?sslmode=disable&connect_timeout=2"); err == nil {
		t.Fatal("an unreachable database must fail at Open")
	}
}
