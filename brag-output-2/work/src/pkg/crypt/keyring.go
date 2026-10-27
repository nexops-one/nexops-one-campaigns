// SPDX-License-Identifier: Apache-2.0

package crypt

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	schemadata "github.com/nexops-one/compliance-engine/schema"

	"github.com/nexops-one/compliance-engine/pkg/store"
	"github.com/nexops-one/compliance-engine/sdk/adapter"
)

// Keyring resolves tenant data keys, creating them on first use, and caches
// unwrapped keys in memory.
type Keyring struct {
	kek KEK
	st  interface {
		store.KeyStore
	}
	now   func() time.Time
	mu    sync.Mutex
	cache map[string]map[int]DataKey
}

// NewKeyring returns a keyring over st using kek.
func NewKeyring(kek KEK, st store.KeyStore, now func() time.Time) *Keyring {
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	return &Keyring{kek: kek, st: st, now: now, cache: map[string]map[int]DataKey{}}
}

// KEKID is the ID of the keyring's KEK.
func (r *Keyring) KEKID() string { return r.kek.ID }

func keyEvent(tenant, action string, at time.Time, details map[string]any) store.AuditEvent {
	data, _ := json.Marshal(details)
	return store.AuditEvent{Scope: adapter.Scope{TenantID: tenant}, At: at, Actor: "system", ActorKind: "system",
		Action: action, TargetType: "tenant_keys", TargetID: tenant, Details: data}
}

func (r *Keyring) wrap(d DataKey) (store.WrappedKey, error) {
	enc, err := r.kek.Wrap(d.Enc[:])
	if err != nil {
		return store.WrappedKey{}, err
	}
	mac, err := r.kek.Wrap(d.Mac[:])
	if err != nil {
		return store.WrappedKey{}, err
	}
	return store.WrappedKey{Version: d.Version, KEKID: r.kek.ID, WrappedEnc: enc, WrappedMac: mac, CreatedAt: r.now()}, nil
}

func unwrap(kek KEK, w store.WrappedKey) (DataKey, error) {
	if w.KEKID != kek.ID {
		return DataKey{}, fmt.Errorf("%w (keys %s, configured %s)", ErrKEKMismatch, w.KEKID, kek.ID)
	}
	enc, err := kek.Unwrap(w.WrappedEnc)
	if err != nil {
		return DataKey{}, err
	}
	mac, err := kek.Unwrap(w.WrappedMac)
	if err != nil {
		return DataKey{}, err
	}
	d := DataKey{Version: w.Version}
	copy(d.Enc[:], enc)
	copy(d.Mac[:], mac)
	return d, nil
}

// load returns the tenant's keys, creating version 1 when none exist.
func (r *Keyring) load(ctx context.Context, tenant string) (store.TenantKeys, error) {
	k, err := r.st.TenantKeys(ctx, tenant)
	if err == nil {
		return k, nil
	}
	if !errors.Is(err, store.ErrNotFound) {
		return store.TenantKeys{}, err
	}
	d, err := NewDataKey(1)
	if err != nil {
		return store.TenantKeys{}, err
	}
	w, err := r.wrap(d)
	if err != nil {
		return store.TenantKeys{}, err
	}
	k = store.TenantKeys{TenantID: tenant, Active: 1, Versions: []store.WrappedKey{w}}
	saved, err := r.st.PutTenantKeys(ctx, k, 0, keyEvent(tenant, "keys.create", r.now(), map[string]any{"version": 1, "kek_id": r.kek.ID}))
	if errors.Is(err, store.ErrConflict) || errors.Is(err, store.ErrExists) {
		return r.st.TenantKeys(ctx, tenant) // created concurrently
	}
	return saved, err
}

// Version returns version v of the tenant's data keys. It never creates keys.
func (r *Keyring) Version(ctx context.Context, tenant string, v int) (DataKey, error) {
	r.mu.Lock()
	if d, ok := r.cache[tenant][v]; ok {
		r.mu.Unlock()
		return d, nil
	}
	r.mu.Unlock()
	// Reading never creates keys: a tenant without keys (never written, or
	// deleted) has nothing to decrypt.
	k, err := r.st.TenantKeys(ctx, tenant)
	if err != nil {
		return DataKey{}, err
	}
	for _, w := range k.Versions {
		if w.Version == v {
			d, err := unwrap(r.kek, w)
			if err != nil {
				return DataKey{}, err
			}
			r.mu.Lock()
			if r.cache[tenant] == nil {
				r.cache[tenant] = map[int]DataKey{}
			}
			r.cache[tenant][v] = d
			r.mu.Unlock()
			return d, nil
		}
	}
	return DataKey{}, fmt.Errorf("tenant %s has no data key version %d", tenant, v)
}

// Active returns the tenant's data keys for new writes.
func (r *Keyring) Active(ctx context.Context, tenant string) (DataKey, error) {
	k, err := r.load(ctx, tenant)
	if err != nil {
		return DataKey{}, err
	}
	return r.Version(ctx, tenant, k.Active)
}

// Opener returns a key lookup for Open bound to tenant.
func (r *Keyring) Opener(ctx context.Context, tenant string) func(int) (DataKey, error) {
	return func(v int) (DataKey, error) { return r.Version(ctx, tenant, v) }
}

// RotateData adds a new active data key version for tenant. Older versions
// stay available for reading existing values.
func (r *Keyring) RotateData(ctx context.Context, tenant string) (int, error) {
	k, err := r.load(ctx, tenant)
	if err != nil {
		return 0, err
	}
	// Refuse to add a key under a KEK that cannot open the existing ones.
	if _, err := r.Version(ctx, tenant, k.Active); err != nil {
		return 0, err
	}
	next := 0
	for _, w := range k.Versions {
		if w.Version > next {
			next = w.Version
		}
	}
	next++
	d, err := NewDataKey(next)
	if err != nil {
		return 0, err
	}
	w, err := r.wrap(d)
	if err != nil {
		return 0, err
	}
	k.Versions, k.Active = append(k.Versions, w), next
	if _, err := r.st.PutTenantKeys(ctx, k, k.Version, keyEvent(tenant, "keys.rotate_data", r.now(), map[string]any{"version": next})); err != nil {
		return 0, err
	}
	return next, nil
}

// Forget drops cached keys of tenant (after deletion or rotation).
func (r *Keyring) Forget(tenant string) {
	r.mu.Lock()
	delete(r.cache, tenant)
	r.mu.Unlock()
}

// RewrapAll re-wraps every tenant's keys from one KEK to another and returns
// the number of tenants rewrapped. Tenants already under `to` are skipped.
func RewrapAll(ctx context.Context, st store.KeyStore, from, to KEK, now func() time.Time) (int, error) {
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	tenants, err := st.KeyTenants(ctx)
	if err != nil {
		return 0, err
	}
	n := 0
	for _, t := range tenants {
		k, err := st.TenantKeys(ctx, t)
		if err != nil {
			return n, err
		}
		changed := false
		for i, w := range k.Versions {
			if w.KEKID == to.ID {
				continue
			}
			d, err := unwrap(from, w)
			if err != nil {
				return n, fmt.Errorf("tenant %s: %w", t, err)
			}
			nw, err := (&Keyring{kek: to, now: now}).wrap(d)
			if err != nil {
				return n, err
			}
			nw.CreatedAt = w.CreatedAt
			k.Versions[i], changed = nw, true
		}
		if !changed {
			continue
		}
		if _, err := st.PutTenantKeys(ctx, k, k.Version, keyEvent(t, "keys.rotate_kek", now(), map[string]any{"from": from.ID, "to": to.ID})); err != nil {
			return n, err
		}
		n++
	}
	return n, nil
}

// Sensitivity lists sensitive fields per entity.
type Sensitivity map[string]map[string]bool

// Has reports whether entity.field is sensitive.
func (s Sensitivity) Has(entity, field string) bool { return s[entity][field] }

// LoadSensitivity reads the embedded sensitivity map of a schema version.
func LoadSensitivity(schemaVersion string) (Sensitivity, error) {
	data, err := schemadata.Sensitive.ReadFile("v" + schemaVersion + "/sensitive.json")
	if err != nil {
		return nil, fmt.Errorf("no sensitivity map for schema %s: %w", schemaVersion, err)
	}
	var doc struct {
		Fields []string `json:"fields"`
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		return nil, err
	}
	s := Sensitivity{}
	for _, f := range doc.Fields {
		entity, field, ok := strings.Cut(f, ".")
		if !ok {
			return nil, fmt.Errorf("sensitivity map: %q is not entity.field", f)
		}
		if s[entity] == nil {
			s[entity] = map[string]bool{}
		}
		s[entity][field] = true
	}
	return s, nil
}
