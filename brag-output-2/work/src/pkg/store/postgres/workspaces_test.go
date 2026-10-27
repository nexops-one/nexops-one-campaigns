// SPDX-License-Identifier: Apache-2.0

package postgres_test

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/nexops-one/compliance-engine/pkg/access"
	"github.com/nexops-one/compliance-engine/pkg/store"
	"github.com/nexops-one/compliance-engine/pkg/store/storetest"
	"github.com/nexops-one/compliance-engine/sdk/adapter"
)

// TestWorkspacesBackfill: migration 0011 registers every workspace that
// already holds data or members.
func TestWorkspacesBackfill(t *testing.T) {
	s, _ := migrated(t)
	if _, err := s.MigrateDown(ctx, 1); err != nil {
		t.Fatal(err)
	}
	at := time.Date(2026, 1, 15, 9, 0, 0, 0, time.UTC)
	withData := adapter.Scope{TenantID: "acme", WorkspaceID: "data"}
	withMember := adapter.Scope{TenantID: "acme", WorkspaceID: "people"}
	src := adapter.Source{System: "s", Adapter: "a", AdapterVersion: "1"}
	if _, err := s.Commit(ctx, store.Commit{Scope: withData, Kind: store.KindIngestion, At: at,
		Changes: []store.Change{{Op: store.OpCreate, Entity: "ict_provider", Key: `["P1"]`, Version: &store.RecordVersion{
			Entity: "ict_provider", Key: `["P1"]`, Data: adapter.Record{"provider_id_code": "P1"}, Hash: "h", SchemaVersion: "0.1.0", Source: src, WrittenAt: at}}},
		Ingestion: store.Ingestion{ID: "ing-1", Scope: withData, Source: src, Mode: adapter.ModeIncremental, Result: json.RawMessage(`{}`), CreatedAt: at}}); err != nil {
		t.Fatal(err)
	}
	u := store.User{TenantID: "acme", ID: "u1", Email: "a@example.com", CreatedAt: at}
	if err := s.CreateUser(ctx, u, storetest.Event(adapter.Scope{TenantID: "acme"}, "user.create")); err != nil {
		t.Fatal(err)
	}
	if err := s.PutMember(ctx, store.Member{Scope: withMember, UserID: "u1", Roles: []access.Role{access.RoleAdmin}, UpdatedAt: at},
		storetest.Event(withMember, "member.set")); err != nil {
		t.Fatal(err)
	}
	if _, err := s.MigrateUp(ctx); err != nil {
		t.Fatal(err)
	}
	ws, err := s.Workspaces(ctx, "")
	if err != nil || len(ws) != 2 || ws[0].Scope != withData || ws[1].Scope != withMember ||
		ws[0].Status != store.WorkspaceActive || ws[0].CreatedBy != "migration" {
		t.Fatalf("backfilled = %+v, %v", ws, err)
	}
}
