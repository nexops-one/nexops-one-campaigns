// SPDX-License-Identifier: Apache-2.0

package storetest

import (
	"errors"
	"reflect"
	"testing"

	"github.com/nexops-one/compliance-engine/pkg/store"
	"github.com/nexops-one/compliance-engine/sdk/adapter"
)

func runKeys(t *testing.T, newStore func(t *testing.T) store.Store) {
	t.Run("tenant keys", func(t *testing.T) {
		s := newStore(t)
		tenant := adapter.Scope{TenantID: "tenant-a"}
		if _, err := s.TenantKeys(ctx, "tenant-a"); !errors.Is(err, store.ErrNotFound) {
			t.Fatalf("missing = %v", err)
		}
		k := store.TenantKeys{TenantID: "tenant-a", Active: 1, Versions: []store.WrappedKey{{Version: 1, KEKID: "k1", WrappedEnc: []byte{1, 2}, WrappedMac: []byte{3}, CreatedAt: at}}}
		saved, err := s.PutTenantKeys(ctx, k, 0, Event(tenant, "keys.create"))
		if err != nil || saved.Version != 1 {
			t.Fatalf("create = %+v %v", saved, err)
		}
		if _, err := s.PutTenantKeys(ctx, k, 0, Event(tenant, "keys.create")); !errors.Is(err, store.ErrConflict) {
			t.Fatalf("second create = %v", err)
		}
		got, err := s.TenantKeys(ctx, "tenant-a")
		if err != nil || !reflect.DeepEqual(got, saved) {
			t.Fatalf("read = %+v %v", got, err)
		}
		if _, err := s.PutTenantKeys(ctx, store.TenantKeys{TenantID: "tenant-b", Active: 1}, 0); err != nil {
			t.Fatal(err)
		}
		if ts, _ := s.KeyTenants(ctx); !reflect.DeepEqual(ts, []string{"tenant-a", "tenant-b"}) {
			t.Fatalf("tenants = %v", ts)
		}
		if err := s.DeleteTenantKeys(ctx, "tenant-a", Invalid(tenant)); err == nil {
			t.Fatal("an invalid event must abort the deletion")
		}
		if _, err := s.TenantKeys(ctx, "tenant-a"); err != nil {
			t.Fatal("the aborted deletion must keep the keys")
		}
		if err := s.DeleteTenantKeys(ctx, "tenant-a", Event(tenant, "keys.delete")); err != nil {
			t.Fatal(err)
		}
		if _, err := s.TenantKeys(ctx, "tenant-a"); !errors.Is(err, store.ErrNotFound) {
			t.Fatal("deleted keys must be gone")
		}
		if got := ChainOf(t, s, tenant); !reflect.DeepEqual(got, []string{"keys.create", "keys.delete"}) {
			t.Fatalf("chain = %v", got)
		}
	})

	t.Run("settings", func(t *testing.T) {
		s := newStore(t)
		def, err := s.Settings(ctx, scopeA)
		if err != nil || def.Version != 0 || def.Retention.KeepRevisions != 1 || def.ReviewOverrides == nil {
			t.Fatalf("defaults = %+v %v", def, err)
		}
		no := false
		def.Retention = store.RetentionPolicy{RevisionDays: 30, KeepRevisions: 5, EvaluationDays: 90}
		def.ReviewOverrides["dora/c1"] = store.ReviewOverride{ReviewInterval: "P6M", ApprovalRequiresEvidence: &no}
		def.Sample, def.UpdatedAt = true, at
		saved, err := s.PutSettings(ctx, def, 0, Event(scopeA, "settings.update"))
		if err != nil || saved.Version != 1 {
			t.Fatalf("save = %+v %v", saved, err)
		}
		if _, err := s.PutSettings(ctx, def, 0); !errors.Is(err, store.ErrConflict) {
			t.Fatalf("stale = %v", err)
		}
		got, _ := s.Settings(ctx, scopeA)
		if !reflect.DeepEqual(got, saved) || got.ReviewOverrides["dora/c1"].ReviewInterval != "P6M" {
			t.Fatalf("read = %+v", got)
		}
		if other, _ := s.Settings(ctx, scopeB); other.Version != 0 || other.Sample {
			t.Fatal("settings must be scoped")
		}
	})
}
