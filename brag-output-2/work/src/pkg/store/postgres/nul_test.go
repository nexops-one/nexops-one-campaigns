// SPDX-License-Identifier: Apache-2.0

package postgres_test

import (
	"testing"

	"github.com/nexops-one/compliance-engine/pkg/compliance"
	"github.com/nexops-one/compliance-engine/sdk/adapter"
)

func TestNULInRecordIsRejectedNotAServerError(t *testing.T) {
	s, _ := migrated(t)
	eng, err := compliance.New(ctx, compliance.Config{Store: s})
	if err != nil {
		t.Fatal(err)
	}
	scope := compliance.Scope{TenantID: "acme", WorkspaceID: "ws-1"}
	m := adapter.Manifest{Name: "t", Version: "1", SchemaVersion: "0.1.0", Modes: []adapter.Mode{adapter.ModeIncremental},
		Supplies: map[string][]string{"ict_provider": {"provider_id_code", "legal_name"}}}
	if err := eng.RegisterManifest(ctx, scope, m); err != nil {
		t.Fatal(err)
	}
	b := adapter.NewBuilder(m, "test").
		Add("ict_provider", adapter.Record{"provider_id_code": "P1", "legal_name": "a\x00b"}).
		Add("ict_provider", adapter.Record{"provider_id_code": "P2", "legal_name": "fine"}).Build()
	res, err := eng.Ingest(ctx, scope, b)
	if err != nil {
		t.Fatalf("a NUL character must be a field error, not a store failure: %v", err)
	}
	if res.Accepted != 1 || res.RejectedRecords != 1 || res.Errors[0].Code != "invalid_character" {
		t.Fatalf("res = %+v", res)
	}
}
