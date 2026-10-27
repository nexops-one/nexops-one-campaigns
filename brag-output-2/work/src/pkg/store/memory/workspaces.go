// SPDX-License-Identifier: Apache-2.0

package memory

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/nexops-one/compliance-engine/pkg/store"
	"github.com/nexops-one/compliance-engine/sdk/adapter"
)

func (s *Store) Workspace(_ context.Context, scope adapter.Scope) (store.Workspace, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	w, ok := s.workspaces[scope]
	if !ok {
		return store.Workspace{}, fmt.Errorf("%w: workspace %s/%s", store.ErrNotFound, scope.TenantID, scope.WorkspaceID)
	}
	return w, nil
}

func (s *Store) Workspaces(_ context.Context, tenantID string) ([]store.Workspace, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := []store.Workspace{}
	for sc, w := range s.workspaces {
		if tenantID == "" || sc.TenantID == tenantID {
			out = append(out, w)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Scope.TenantID != out[j].Scope.TenantID {
			return out[i].Scope.TenantID < out[j].Scope.TenantID
		}
		return out[i].Scope.WorkspaceID < out[j].Scope.WorkspaceID
	})
	return out, nil
}

// activeLocked counts the active workspaces.
func (s *Store) activeLocked() int {
	n := 0
	for _, w := range s.workspaces {
		if w.Status == store.WorkspaceActive {
			n++
		}
	}
	return n
}

func (s *Store) RegisterWorkspace(_ context.Context, w store.Workspace, limit int, events ...store.AuditEvent) (store.Workspace, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if cur, ok := s.workspaces[w.Scope]; ok {
		return cur, false, nil
	}
	if limit > 0 && s.activeLocked() >= limit {
		return store.Workspace{}, false, fmt.Errorf("%w: %d active workspace(s) allowed", store.ErrWorkspaceLimit, limit)
	}
	sealed, err := s.sealLocked(events)
	if err != nil {
		return store.Workspace{}, false, err
	}
	w.Status = store.WorkspaceActive
	if w.StatusAt.IsZero() {
		w.StatusAt, w.StatusBy = w.CreatedAt, w.CreatedBy
	}
	s.workspaces[w.Scope] = w
	s.storeSealedLocked(sealed)
	return w, true, nil
}

func (s *Store) SetWorkspaceStatus(_ context.Context, scope adapter.Scope, status store.WorkspaceStatus, at time.Time, by, reason string, limit int,
	events ...store.AuditEvent) (store.Workspace, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	w, ok := s.workspaces[scope]
	if !ok {
		return store.Workspace{}, fmt.Errorf("%w: workspace %s/%s", store.ErrNotFound, scope.TenantID, scope.WorkspaceID)
	}
	if status == store.WorkspaceActive && w.Status != store.WorkspaceActive && limit > 0 && s.activeLocked() >= limit {
		return store.Workspace{}, fmt.Errorf("%w: %d active workspace(s) allowed", store.ErrWorkspaceLimit, limit)
	}
	sealed, err := s.sealLocked(events)
	if err != nil {
		return store.Workspace{}, err
	}
	w.Status, w.StatusAt, w.StatusBy, w.Reason = status, at, by, reason
	s.workspaces[scope] = w
	s.storeSealedLocked(sealed)
	return w, nil
}

func (s *Store) DeleteWorkspaceData(_ context.Context, scope adapter.Scope, events ...store.AuditEvent) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	sealed, err := s.sealLocked(events)
	if err != nil {
		return 0, err
	}
	n := 0
	if d, ok := s.scopes[scope]; ok {
		n += len(d.revisions) + len(d.ingestions) + len(d.provenance) + len(d.manifests) + len(d.evaluations)
		delete(s.scopes, scope)
	}
	for _, r := range s.reports[scope] {
		n += 1 + len(r.files)
	}
	delete(s.reports, scope)
	n += len(s.wf.evidence[scope]) + len(s.wf.assessments[scope]) + len(s.wf.history[scope])
	delete(s.wf.evidence, scope)
	delete(s.wf.assessments, scope)
	delete(s.wf.history, scope)
	if _, ok := s.kd.settings[scope]; ok {
		n++
		delete(s.kd.settings, scope)
	}
	n += len(s.id.members[scope])
	delete(s.id.members, scope)
	for id, t := range s.id.tokens {
		if t.Scope == scope {
			delete(s.id.hashes, t.Hash)
			delete(s.id.tokens, id)
			n++
		}
	}
	for h, se := range s.id.sessions {
		if se.TenantID == scope.TenantID && se.WorkspaceID == scope.WorkspaceID {
			delete(s.id.sessions, h)
			n++
		}
	}
	if _, ok := s.workspaces[scope]; ok {
		n++
		delete(s.workspaces, scope)
	}
	s.storeSealedLocked(sealed)
	return n, nil
}
