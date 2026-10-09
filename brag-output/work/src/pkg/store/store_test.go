// SPDX-License-Identifier: Apache-2.0

package store_test

import (
	"testing"

	"github.com/nexops-one/compliance-engine/pkg/store"
	"github.com/nexops-one/compliance-engine/sdk/adapter"
)

func TestSnapshotIDRoundTrip(t *testing.T) {
	for _, n := range []int64{0, 1, 42} {
		got, err := store.ParseSnapshotID(store.SnapshotID(n))
		if err != nil || got != n {
			t.Errorf("round trip %d = %d, %v", n, got, err)
		}
	}
	for _, bad := range []string{"", "rev-", "rev--1", "snap-1", "rev-x"} {
		if _, err := store.ParseSnapshotID(bad); err == nil {
			t.Errorf("%q should fail", bad)
		}
	}
}

func TestCloneRecordIsDeep(t *testing.T) {
	orig := adapter.Record{"a": map[string]any{"b": []any{"c"}}}
	clone := store.CloneRecord(orig)
	clone["a"].(map[string]any)["b"].([]any)[0] = "changed"
	if orig["a"].(map[string]any)["b"].([]any)[0] != "c" {
		t.Fatal("CloneRecord must deep-copy nested maps and slices")
	}
}
