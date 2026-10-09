// SPDX-License-Identifier: Apache-2.0

package memory

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"

	"github.com/nexops-one/compliance-engine/pkg/store"
	"github.com/nexops-one/compliance-engine/sdk/adapter"
)

type workflowData struct {
	evidence    map[adapter.Scope]map[string]store.Evidence
	assessments map[adapter.Scope]map[store.ControlRef]store.Assessment
	history     map[adapter.Scope][]store.Transition
}

func newWorkflowData() workflowData {
	return workflowData{
		evidence:    map[adapter.Scope]map[string]store.Evidence{},
		assessments: map[adapter.Scope]map[store.ControlRef]store.Assessment{},
		history:     map[adapter.Scope][]store.Transition{},
	}
}

// deepCopy clones plain data documents through JSON.
func deepCopy[T any](v T) T {
	data, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	var out T
	if err := json.Unmarshal(data, &out); err != nil {
		panic(err)
	}
	return out
}

func (s *Store) PutEvidence(_ context.Context, e store.Evidence, expected int64, events ...store.AuditEvent) (store.Evidence, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	cur, exists := s.wf.evidence[e.Scope][e.ID]
	switch {
	case expected == 0 && exists:
		return store.Evidence{}, fmt.Errorf("%w: evidence %s", store.ErrExists, e.ID)
	case expected != 0 && (!exists || cur.Version != expected):
		return store.Evidence{}, fmt.Errorf("%w: evidence %s changed since it was read", store.ErrConflict, e.ID)
	}
	sealed, err := s.sealLocked(events)
	if err != nil {
		return store.Evidence{}, err
	}
	e.Version = expected + 1
	e = deepCopy(e)
	if s.wf.evidence[e.Scope] == nil {
		s.wf.evidence[e.Scope] = map[string]store.Evidence{}
	}
	s.wf.evidence[e.Scope][e.ID] = e
	s.storeSealedLocked(sealed)
	return deepCopy(e), nil
}

func (s *Store) Evidence(_ context.Context, scope adapter.Scope, id string) (store.Evidence, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	e, ok := s.wf.evidence[scope][id]
	if !ok {
		return store.Evidence{}, fmt.Errorf("%w: evidence %s", store.ErrNotFound, id)
	}
	return deepCopy(e), nil
}

func (s *Store) ListEvidence(_ context.Context, scope adapter.Scope, q store.EvidenceQuery) ([]store.Evidence, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := []store.Evidence{}
	for _, e := range s.wf.evidence[scope] {
		match := q.Catalog == "" && q.ControlID == ""
		for _, l := range e.Links {
			match = match || ((q.Catalog == "" || l.Catalog == q.Catalog) && (q.ControlID == "" || l.ControlID == q.ControlID))
		}
		if match {
			out = append(out, deepCopy(e))
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

func (s *Store) Assessment(_ context.Context, scope adapter.Scope, catalog, controlID string) (store.Assessment, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	a, ok := s.wf.assessments[scope][store.ControlRef{Catalog: catalog, ControlID: controlID}]
	if !ok {
		return store.Assessment{}, fmt.Errorf("%w: assessment %s/%s", store.ErrNotFound, catalog, controlID)
	}
	return deepCopy(a), nil
}

func (s *Store) Assessments(_ context.Context, scope adapter.Scope, catalog string) ([]store.Assessment, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := []store.Assessment{}
	for ref, a := range s.wf.assessments[scope] {
		if catalog == "" || ref.Catalog == catalog {
			out = append(out, deepCopy(a))
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Catalog != out[j].Catalog {
			return out[i].Catalog < out[j].Catalog
		}
		return out[i].ControlID < out[j].ControlID
	})
	return out, nil
}

func (s *Store) SaveAssessment(_ context.Context, a store.Assessment, expected int64, tr *store.Transition, events ...store.AuditEvent) (store.Assessment, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	ref := a.Ref()
	cur, exists := s.wf.assessments[a.Scope][ref]
	if (expected == 0 && exists) || (expected != 0 && (!exists || cur.Version != expected)) {
		return store.Assessment{}, fmt.Errorf("%w: assessment %s/%s changed since it was read", store.ErrConflict, ref.Catalog, ref.ControlID)
	}
	sealed, err := s.sealLocked(events)
	if err != nil {
		return store.Assessment{}, err
	}
	a.Version = expected + 1
	a = deepCopy(a)
	if s.wf.assessments[a.Scope] == nil {
		s.wf.assessments[a.Scope] = map[store.ControlRef]store.Assessment{}
	}
	s.wf.assessments[a.Scope][ref] = a
	if tr != nil {
		t := deepCopy(*tr)
		t.Seq = int64(len(s.wf.history[a.Scope]) + 1)
		t.Catalog, t.ControlID = ref.Catalog, ref.ControlID
		s.wf.history[a.Scope] = append(s.wf.history[a.Scope], t)
	}
	s.storeSealedLocked(sealed)
	return deepCopy(a), nil
}

func (s *Store) History(_ context.Context, scope adapter.Scope, catalog, controlID string) ([]store.Transition, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := []store.Transition{}
	for _, t := range s.wf.history[scope] {
		if t.Catalog == catalog && t.ControlID == controlID {
			out = append(out, deepCopy(t))
		}
	}
	return out, nil
}
