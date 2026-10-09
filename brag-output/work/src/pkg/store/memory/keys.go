// SPDX-License-Identifier: Apache-2.0

package memory

import (
	"context"
	"fmt"
	"sort"

	"github.com/nexops-one/compliance-engine/pkg/store"
	"github.com/nexops-one/compliance-engine/sdk/adapter"
)

type keysData struct {
	keys     map[string]store.TenantKeys
	settings map[adapter.Scope]store.Settings
}

func newKeysData() keysData {
	return keysData{keys: map[string]store.TenantKeys{}, settings: map[adapter.Scope]store.Settings{}}
}

func (s *Store) TenantKeys(_ context.Context, tenant string) (store.TenantKeys, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	k, ok := s.kd.keys[tenant]
	if !ok {
		return store.TenantKeys{}, fmt.Errorf("%w: keys of tenant %s", store.ErrNotFound, tenant)
	}
	return deepCopy(k), nil
}

func (s *Store) PutTenantKeys(_ context.Context, k store.TenantKeys, expected int64, events ...store.AuditEvent) (store.TenantKeys, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	cur, exists := s.kd.keys[k.TenantID]
	if (expected == 0 && exists) || (expected != 0 && (!exists || cur.Version != expected)) {
		return store.TenantKeys{}, fmt.Errorf("%w: keys of tenant %s changed", store.ErrConflict, k.TenantID)
	}
	sealed, err := s.sealLocked(events)
	if err != nil {
		return store.TenantKeys{}, err
	}
	k.Version = expected + 1
	s.kd.keys[k.TenantID] = deepCopy(k)
	s.storeSealedLocked(sealed)
	return deepCopy(k), nil
}

func (s *Store) DeleteTenantKeys(_ context.Context, tenant string, events ...store.AuditEvent) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	sealed, err := s.sealLocked(events)
	if err != nil {
		return err
	}
	delete(s.kd.keys, tenant)
	s.storeSealedLocked(sealed)
	return nil
}

func (s *Store) KeyTenants(_ context.Context) ([]string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := []string{}
	for t := range s.kd.keys {
		out = append(out, t)
	}
	sort.Strings(out)
	return out, nil
}

func (s *Store) Settings(_ context.Context, scope adapter.Scope) (store.Settings, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if st, ok := s.kd.settings[scope]; ok {
		return deepCopy(st), nil
	}
	return store.DefaultSettings(scope), nil
}

func (s *Store) PutSettings(_ context.Context, st store.Settings, expected int64, events ...store.AuditEvent) (store.Settings, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	cur, exists := s.kd.settings[st.Scope]
	if (expected == 0 && exists) || (expected != 0 && (!exists || cur.Version != expected)) {
		return store.Settings{}, fmt.Errorf("%w: settings changed since they were read", store.ErrConflict)
	}
	sealed, err := s.sealLocked(events)
	if err != nil {
		return store.Settings{}, err
	}
	st.Version = expected + 1
	if st.ReviewOverrides == nil {
		st.ReviewOverrides = map[string]store.ReviewOverride{}
	}
	s.kd.settings[st.Scope] = deepCopy(st)
	s.storeSealedLocked(sealed)
	return deepCopy(st), nil
}
