// SPDX-License-Identifier: Apache-2.0

package memory

import (
	"context"
	"sort"
	"time"

	"github.com/nexops-one/compliance-engine/pkg/store"
	"github.com/nexops-one/compliance-engine/sdk/adapter"
)

func (s *Store) WorkspaceScopes(_ context.Context) ([]adapter.Scope, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	seen := map[adapter.Scope]bool{}
	for sc := range s.scopes {
		seen[sc] = true
	}
	for sc := range s.wf.evidence {
		seen[sc] = true
	}
	for sc := range s.wf.assessments {
		seen[sc] = true
	}
	out := []adapter.Scope{}
	for sc := range seen {
		out = append(out, sc)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].TenantID != out[j].TenantID {
			return out[i].TenantID < out[j].TenantID
		}
		return out[i].WorkspaceID < out[j].WorkspaceID
	})
	return out, nil
}

func (s *Store) EvaluationRefs(_ context.Context, scope adapter.Scope) ([]store.EvaluationRef, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := []store.EvaluationRef{}
	if d := s.scopes[scope]; d != nil {
		for _, e := range d.evaluations {
			out = append(out, store.EvaluationRef{ID: e.ID, SnapshotID: e.SnapshotID, CreatedAt: e.CreatedAt})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

func (s *Store) ApplyRetention(_ context.Context, scope adapter.Scope, del store.RetentionDeletion, events ...store.AuditEvent) (store.RetentionCounts, error) {
	keepFrom := del.KeepFrom
	s.mu.Lock()
	defer s.mu.Unlock()
	sealed, err := s.sealLocked(events)
	if err != nil {
		return store.RetentionCounts{}, err
	}
	var c store.RetentionCounts
	if d := s.scopes[scope]; d != nil {
		if keepFrom > d.firstKept && keepFrom <= int64(len(d.revisions)) {
			kept := map[*store.RecordVersion]bool{}
			for i := keepFrom - 1; i < int64(len(d.records)); i++ {
				for _, v := range d.records[i] {
					kept[v] = true
				}
			}
			gone := map[*store.RecordVersion]bool{}
			for i := int64(0); i < keepFrom-1; i++ {
				if d.records[i] == nil {
					continue
				}
				for _, v := range d.records[i] {
					if !kept[v] {
						gone[v] = true
					}
				}
				d.records[i] = nil
				c.Revisions++
			}
			c.RecordVersions = len(gone)
			prov := d.provenance[:0]
			for _, p := range d.provenance {
				if p.Revision < keepFrom {
					c.Provenance++
					continue
				}
				prov = append(prov, p)
			}
			d.provenance = prov
			for id, ing := range d.ingestions {
				if (ing.RevisionAfter != 0 && ing.RevisionAfter < keepFrom) || (ing.RevisionAfter == 0 && ing.RevisionBefore < keepFrom) {
					delete(d.ingestions, id)
					c.Ingestions++
				}
			}
			d.firstKept = keepFrom
		}
		for _, id := range del.Evaluations {
			if _, ok := d.evaluations[id]; ok {
				delete(d.evaluations, id)
				c.Evaluations++
			}
		}
	}
	for _, id := range del.Reports {
		if _, ok := s.reports[scope][id]; ok {
			delete(s.reports[scope], id)
			c.Reports++
		}
	}
	for _, id := range del.Evidence {
		if _, ok := s.wf.evidence[scope][id]; ok {
			delete(s.wf.evidence[scope], id)
			c.Evidence++
		}
	}
	s.storeSealedLocked(sealed)
	return c, nil
}

func (s *Store) PruneAudit(_ context.Context, scope adapter.Scope, before time.Time, event store.AuditEvent) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	chain := s.id.audit[scope]
	cut := len(chain) - 1 // always keep the latest event
	for i, e := range chain {
		if !e.At.Before(before) {
			cut = min(cut, i)
			break
		}
	}
	if cut <= 0 {
		return 0, nil
	}
	kept := append([]store.AuditEvent(nil), chain[cut:]...)
	s.id.audit[scope] = kept
	sealed, err := s.sealLocked([]store.AuditEvent{event})
	if err != nil {
		s.id.audit[scope] = chain
		return 0, err
	}
	s.storeSealedLocked(sealed)
	return cut, nil
}
