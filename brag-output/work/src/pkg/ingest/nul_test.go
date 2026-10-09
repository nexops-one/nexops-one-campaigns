// SPDX-License-Identifier: Apache-2.0

package ingest_test

import (
	"testing"

	"github.com/nexops-one/compliance-engine/pkg/ingest"
	"github.com/nexops-one/compliance-engine/sdk/adapter"
)

func TestPlanRejectsNULCharacters(t *testing.T) {
	res, changes := ingest.Plan(batch(adapter.ModeIncremental,
		adapter.Record{"provider_id_code": "P1", "legal_name": "a\x00b"},
		adapter.Record{"provider_id_code": "P2\x00"},
		adapter.Record{"provider_id_code": "P3", "legal_name": "ok"},
	), options(t))
	if res.Accepted != 1 || res.RejectedRecords != 2 || len(changes) != 1 {
		t.Fatalf("records containing NUL must be rejected as data, not reach the store: %+v", res)
	}
	got := map[string]string{}
	for _, e := range res.Errors {
		got[e.Field] = e.Code
	}
	if got["legal_name"] != "invalid_character" || got["provider_id_code"] != "invalid_character" {
		t.Fatalf("errors = %+v", res.Errors)
	}
}
