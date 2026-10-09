// SPDX-License-Identifier: Apache-2.0

package memory

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/nexops-one/compliance-engine/pkg/store"
)

func (s *Store) UserMembers(_ context.Context, tenantID, userID string) ([]store.Member, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := []store.Member{}
	for sc, ms := range s.id.members {
		if _, ok := ms[userID]; ok && sc.TenantID == tenantID {
			m, _ := s.memberLocked(sc, userID)
			out = append(out, m)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Scope.WorkspaceID < out[j].Scope.WorkspaceID })
	return out, nil
}

func (s *Store) CreateSession(_ context.Context, se store.Session, ev store.AuditEvent) error {
	return s.mutate(ev, func() error {
		if _, ok := s.id.sessions[se.Hash]; ok {
			return fmt.Errorf("%w: session", store.ErrExists)
		}
		s.id.sessions[se.Hash] = se
		return nil
	})
}

func (s *Store) SessionByHash(_ context.Context, hash string) (store.Session, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if se, ok := s.id.sessions[hash]; ok {
		return se, nil
	}
	return store.Session{}, fmt.Errorf("%w: session", store.ErrNotFound)
}

func (s *Store) updateSession(hash string, change func(*store.Session)) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	se, ok := s.id.sessions[hash]
	if !ok {
		return fmt.Errorf("%w: session", store.ErrNotFound)
	}
	change(&se)
	s.id.sessions[hash] = se
	return nil
}

func (s *Store) TouchSession(_ context.Context, hash string, at time.Time) error {
	return s.updateSession(hash, func(se *store.Session) { se.LastSeenAt = at })
}

func (s *Store) SetSessionWorkspace(_ context.Context, hash, workspaceID string) error {
	return s.updateSession(hash, func(se *store.Session) { se.WorkspaceID = workspaceID })
}

func (s *Store) DeleteSession(_ context.Context, hash string, events ...store.AuditEvent) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.id.sessions[hash]; !ok {
		return nil
	}
	sealed, err := s.sealLocked(events)
	if err != nil {
		return err
	}
	delete(s.id.sessions, hash)
	s.storeSealedLocked(sealed)
	return nil
}

func (s *Store) DeleteExpiredSessions(_ context.Context, now, idleBefore time.Time) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := 0
	for h, se := range s.id.sessions {
		if !now.Before(se.ExpiresAt) || se.LastSeenAt.Before(idleBefore) {
			delete(s.id.sessions, h)
			n++
		}
	}
	return n, nil
}
