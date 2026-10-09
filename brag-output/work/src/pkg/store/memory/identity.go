// SPDX-License-Identifier: Apache-2.0

package memory

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/nexops-one/compliance-engine/pkg/access"
	"github.com/nexops-one/compliance-engine/pkg/audit"
	"github.com/nexops-one/compliance-engine/pkg/store"
	"github.com/nexops-one/compliance-engine/sdk/adapter"
)

// identity holds users, memberships, tokens and audit chains. It is guarded
// by Store.mu like the record data.
type identity struct {
	audit   map[adapter.Scope][]store.AuditEvent
	users   map[string]map[string]store.User // tenant -> id -> user
	members map[adapter.Scope]map[string]store.Member
	tokens  map[string]store.Token // id -> token
	hashes  map[string]string      // hash -> id
	// sessions are keyed by hash.
	sessions map[string]store.Session
}

func newIdentity() identity {
	return identity{
		audit: map[adapter.Scope][]store.AuditEvent{}, users: map[string]map[string]store.User{},
		members: map[adapter.Scope]map[string]store.Member{}, tokens: map[string]store.Token{}, hashes: map[string]string{},
		sessions: map[string]store.Session{},
	}
}

// sealLocked seals events after the last event of their scope without
// storing them, so callers can validate before mutating anything.
func (s *Store) sealLocked(events []store.AuditEvent) ([]store.AuditEvent, error) {
	if len(events) == 0 {
		return nil, nil
	}
	chain := s.id.audit[events[0].Scope]
	var seq int64
	var hash string
	if n := len(chain); n > 0 {
		seq, hash = chain[n-1].Seq, chain[n-1].Hash
	}
	return audit.Seal(seq, hash, events)
}

func (s *Store) storeSealedLocked(sealed []store.AuditEvent) {
	if len(sealed) > 0 {
		sc := sealed[0].Scope
		s.id.audit[sc] = append(s.id.audit[sc], sealed...)
	}
}

func (s *Store) AppendAudit(_ context.Context, events ...store.AuditEvent) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	sealed, err := s.sealLocked(events)
	if err != nil {
		return err
	}
	s.storeSealedLocked(sealed)
	return nil
}

func (s *Store) AuditEvents(_ context.Context, scope adapter.Scope, q store.AuditQuery) ([]store.AuditEvent, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := []store.AuditEvent{}
	for _, e := range s.id.audit[scope] {
		if e.Seq <= q.AfterSeq {
			continue
		}
		if q.Limit > 0 && len(out) == q.Limit {
			break
		}
		e.Details = append([]byte(nil), e.Details...)
		out = append(out, e)
	}
	return out, nil
}

func (s *Store) AuditScopes(_ context.Context, tenantID string) ([]adapter.Scope, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := []adapter.Scope{}
	for sc := range s.id.audit {
		if sc.TenantID == tenantID {
			out = append(out, sc)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].WorkspaceID < out[j].WorkspaceID })
	return out, nil
}

// mutate runs apply only when ev seals, then stores ev: both or neither.
func (s *Store) mutate(ev store.AuditEvent, apply func() error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	sealed, err := s.sealLocked([]store.AuditEvent{ev})
	if err != nil {
		return err
	}
	if err := apply(); err != nil {
		return err
	}
	s.storeSealedLocked(sealed)
	return nil
}

func cloneRoles(rs []access.Role) []access.Role { return access.Normalize(rs) }

func cloneTime(t *time.Time) *time.Time {
	if t == nil {
		return nil
	}
	c := *t
	return &c
}

func cloneToken(t store.Token) store.Token {
	t.Roles = cloneRoles(t.Roles)
	t.ExpiresAt, t.RevokedAt, t.LastUsedAt = cloneTime(t.ExpiresAt), cloneTime(t.RevokedAt), cloneTime(t.LastUsedAt)
	return t
}

func (s *Store) CreateUser(_ context.Context, u store.User, ev store.AuditEvent) error {
	return s.mutate(ev, func() error {
		byID := s.id.users[u.TenantID]
		for _, x := range byID {
			if x.Email == u.Email {
				return fmt.Errorf("%w: user %s", store.ErrExists, u.Email)
			}
			if u.ExternalID != "" && x.ExternalID == u.ExternalID {
				return fmt.Errorf("%w: external user %s", store.ErrExists, u.ExternalID)
			}
		}
		if _, ok := byID[u.ID]; ok {
			return fmt.Errorf("%w: user id %s", store.ErrExists, u.ID)
		}
		if byID == nil {
			byID = map[string]store.User{}
			s.id.users[u.TenantID] = byID
		}
		byID[u.ID] = u
		return nil
	})
}

func (s *Store) User(_ context.Context, tenantID, id string) (store.User, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if u, ok := s.id.users[tenantID][id]; ok {
		return u, nil
	}
	return store.User{}, fmt.Errorf("%w: user %s", store.ErrNotFound, id)
}

func (s *Store) UserByExternalID(_ context.Context, tenantID, externalID string) (store.User, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, u := range s.id.users[tenantID] {
		if externalID != "" && u.ExternalID == externalID {
			return u, nil
		}
	}
	return store.User{}, fmt.Errorf("%w: external user %s", store.ErrNotFound, externalID)
}

func (s *Store) SetUserEmail(_ context.Context, tenantID, userID, email string, ev store.AuditEvent) error {
	return s.mutate(ev, func() error {
		u, ok := s.id.users[tenantID][userID]
		if !ok {
			return fmt.Errorf("%w: user %s", store.ErrNotFound, userID)
		}
		for id, x := range s.id.users[tenantID] {
			if id != userID && x.Email == email {
				return fmt.Errorf("%w: user %s", store.ErrExists, email)
			}
		}
		u.Email = email
		s.id.users[tenantID][userID] = u
		return nil
	})
}

func (s *Store) UserByEmail(_ context.Context, tenantID, email string) (store.User, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, u := range s.id.users[tenantID] {
		if u.Email == email {
			return u, nil
		}
	}
	return store.User{}, fmt.Errorf("%w: user %s", store.ErrNotFound, email)
}

func (s *Store) Users(_ context.Context, tenantID string) ([]store.User, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := []store.User{}
	for _, u := range s.id.users[tenantID] {
		out = append(out, u)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Email < out[j].Email })
	return out, nil
}

func (s *Store) SetPassword(_ context.Context, tenantID, userID, hash string, ev store.AuditEvent) error {
	return s.mutate(ev, func() error {
		u, ok := s.id.users[tenantID][userID]
		if !ok {
			return fmt.Errorf("%w: user %s", store.ErrNotFound, userID)
		}
		u.PasswordHash = hash
		s.id.users[tenantID][userID] = u
		return nil
	})
}

func (s *Store) PutMember(_ context.Context, m store.Member, ev store.AuditEvent) error {
	return s.mutate(ev, func() error {
		u, ok := s.id.users[m.Scope.TenantID][m.UserID]
		if !ok {
			return fmt.Errorf("%w: user %s", store.ErrNotFound, m.UserID)
		}
		m.Email, m.Roles = u.Email, cloneRoles(m.Roles)
		if s.id.members[m.Scope] == nil {
			s.id.members[m.Scope] = map[string]store.Member{}
		}
		s.id.members[m.Scope][m.UserID] = m
		return nil
	})
}

func (s *Store) memberLocked(scope adapter.Scope, userID string) (store.Member, error) {
	m, ok := s.id.members[scope][userID]
	if !ok {
		return store.Member{}, fmt.Errorf("%w: member %s", store.ErrNotFound, userID)
	}
	m.Email = s.id.users[scope.TenantID][userID].Email
	m.Roles = cloneRoles(m.Roles)
	return m, nil
}

func (s *Store) Member(_ context.Context, scope adapter.Scope, userID string) (store.Member, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.memberLocked(scope, userID)
}

func (s *Store) Members(_ context.Context, scope adapter.Scope) ([]store.Member, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := []store.Member{}
	for id := range s.id.members[scope] {
		m, _ := s.memberLocked(scope, id)
		out = append(out, m)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Email < out[j].Email })
	return out, nil
}

func (s *Store) DeleteMember(_ context.Context, scope adapter.Scope, userID string, at time.Time, ev store.AuditEvent) error {
	return s.mutate(ev, func() error {
		if _, ok := s.id.members[scope][userID]; !ok {
			return fmt.Errorf("%w: member %s", store.ErrNotFound, userID)
		}
		delete(s.id.members[scope], userID)
		for id, t := range s.id.tokens {
			if t.Scope == scope && t.UserID == userID && t.RevokedAt == nil {
				t.RevokedAt = cloneTime(&at)
				s.id.tokens[id] = t
			}
		}
		return nil
	})
}

func (s *Store) CreateToken(_ context.Context, t store.Token, ev store.AuditEvent) error {
	return s.mutate(ev, func() error {
		if _, ok := s.id.hashes[t.Hash]; ok {
			return fmt.Errorf("%w: token hash", store.ErrExists)
		}
		if _, ok := s.id.tokens[t.ID]; ok {
			return fmt.Errorf("%w: token %s", store.ErrExists, t.ID)
		}
		s.id.tokens[t.ID] = cloneToken(t)
		s.id.hashes[t.Hash] = t.ID
		return nil
	})
}

func (s *Store) TokenByHash(_ context.Context, hash string) (store.Token, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if id, ok := s.id.hashes[hash]; ok {
		return cloneToken(s.id.tokens[id]), nil
	}
	return store.Token{}, fmt.Errorf("%w: token", store.ErrNotFound)
}

func (s *Store) Tokens(_ context.Context, scope adapter.Scope) ([]store.Token, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := []store.Token{}
	for _, t := range s.id.tokens {
		if t.Scope == scope {
			out = append(out, cloneToken(t))
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if !out[i].CreatedAt.Equal(out[j].CreatedAt) {
			return out[i].CreatedAt.Before(out[j].CreatedAt)
		}
		return out[i].ID < out[j].ID
	})
	return out, nil
}

func (s *Store) RevokeToken(_ context.Context, scope adapter.Scope, id string, at time.Time, ev store.AuditEvent) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	t, ok := s.id.tokens[id]
	if !ok || t.Scope != scope {
		return fmt.Errorf("%w: token %s", store.ErrNotFound, id)
	}
	if t.RevokedAt != nil {
		return nil
	}
	sealed, err := s.sealLocked([]store.AuditEvent{ev})
	if err != nil {
		return err
	}
	t.RevokedAt = cloneTime(&at)
	s.id.tokens[id] = t
	s.storeSealedLocked(sealed)
	return nil
}

func (s *Store) TouchToken(_ context.Context, id string, at time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	t, ok := s.id.tokens[id]
	if !ok {
		return fmt.Errorf("%w: token %s", store.ErrNotFound, id)
	}
	t.LastUsedAt = cloneTime(&at)
	s.id.tokens[id] = t
	return nil
}

func (s *Store) HasActiveTokens(_ context.Context, now time.Time) (bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, t := range s.id.tokens {
		if t.Active(now) {
			return true, nil
		}
	}
	return false, nil
}
