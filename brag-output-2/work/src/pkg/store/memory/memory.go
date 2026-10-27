// SPDX-License-Identifier: Apache-2.0

// Package memory is an in-memory store for tests, embedding and demos. Data
// is lost when the process exits.
package memory

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"sync"

	"github.com/nexops-one/compliance-engine/pkg/store"
	"github.com/nexops-one/compliance-engine/sdk/adapter"
)

// Store implements store.Store in memory.
type Store struct {
	mu     sync.RWMutex
	scopes map[adapter.Scope]*scopeData
	id     identity
	wf     workflowData
	kd     keysData
	// reports are keyed by scope, then report ID.
	reports    map[adapter.Scope]map[string]storedReport
	workspaces map[adapter.Scope]store.Workspace
}

type scopeData struct {
	revisions   []store.Revision
	records     []map[string]*store.RecordVersion // records[i] is revision i+1
	ingestions  map[string]store.Ingestion
	provenance  []store.ProvenanceEntry
	manifests   map[string]adapter.Manifest
	evaluations map[string]store.StoredEvaluation
	firstKept   int64 // revisions below it were deleted by retention
}

// New returns an empty store.
func New() *Store {
	return &Store{scopes: map[adapter.Scope]*scopeData{}, id: newIdentity(), wf: newWorkflowData(), kd: newKeysData(),
		reports: map[adapter.Scope]map[string]storedReport{}, workspaces: map[adapter.Scope]store.Workspace{}}
}

var _ store.Store = (*Store)(nil)

func (s *Store) data(scope adapter.Scope) *scopeData {
	d, ok := s.scopes[scope]
	if !ok {
		d = &scopeData{ingestions: map[string]store.Ingestion{}, manifests: map[string]adapter.Manifest{}, evaluations: map[string]store.StoredEvaluation{}}
		s.scopes[scope] = d
	}
	return d
}

func recKey(entity, key string) string { return entity + "\x00" + key }

func cloneVersion(v store.RecordVersion) *store.RecordVersion {
	v.Data = store.CloneRecord(v.Data)
	return &v
}

func (s *Store) CurrentRevision(_ context.Context, scope adapter.Scope) (store.Revision, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	d := s.scopes[scope]
	if d == nil || len(d.revisions) == 0 {
		return store.Revision{}, store.ErrNotFound
	}
	return d.revisions[len(d.revisions)-1], nil
}

func (s *Store) Revision(_ context.Context, scope adapter.Scope, number int64) (store.Revision, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	d := s.scopes[scope]
	if d == nil || number < 1 || number > int64(len(d.revisions)) || number < d.firstKept {
		return store.Revision{}, fmt.Errorf("%w: revision %d", store.ErrNotFound, number)
	}
	return d.revisions[number-1], nil
}

func (s *Store) Revisions(_ context.Context, scope adapter.Scope) ([]store.Revision, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := []store.Revision{}
	if d := s.scopes[scope]; d != nil {
		for _, r := range d.revisions {
			if r.Number >= d.firstKept {
				out = append(out, r)
			}
		}
	}
	return out, nil
}

func (s *Store) Records(_ context.Context, scope adapter.Scope, revision int64) ([]store.RecordVersion, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := []store.RecordVersion{}
	if revision == 0 {
		return out, nil
	}
	d := s.scopes[scope]
	if d == nil || revision < 0 || revision > int64(len(d.records)) || revision < d.firstKept {
		return nil, fmt.Errorf("%w: revision %d", store.ErrNotFound, revision)
	}
	for _, v := range d.records[revision-1] {
		out = append(out, *cloneVersion(*v))
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Entity != out[j].Entity {
			return out[i].Entity < out[j].Entity
		}
		return out[i].Key < out[j].Key
	})
	return out, nil
}

func (s *Store) Commit(_ context.Context, c store.Commit) (store.Revision, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	d := s.data(c.Scope)
	current := int64(len(d.revisions))
	if c.ExpectedRevision != current {
		return store.Revision{}, fmt.Errorf("%w: expected revision %d, current is %d", store.ErrConflict, c.ExpectedRevision, current)
	}
	for _, id := range c.RollsBack {
		if _, ok := d.ingestions[id]; !ok {
			return store.Revision{}, fmt.Errorf("%w: ingestion %s", store.ErrNotFound, id)
		}
	}
	sealed, err := s.sealLocked(c.Audit)
	if err != nil {
		return store.Revision{}, err
	}
	defer s.storeSealedLocked(sealed) // only reached on success below: nothing after this point fails
	next := map[string]*store.RecordVersion{}
	if current > 0 {
		for k, v := range d.records[current-1] {
			next[k] = v // unchanged versions are shared between revisions
		}
	}
	number := current + 1
	for _, ch := range c.Changes {
		k := recKey(ch.Entity, ch.Key)
		entry := store.ProvenanceEntry{
			Seq: int64(len(d.provenance) + 1), IngestionID: c.Ingestion.ID, Revision: number,
			Entity: ch.Entity, Key: ch.Key, Op: ch.Op, Source: c.Ingestion.Source,
			PreviousHash: ch.PreviousHash, RecordedAt: c.At,
		}
		if ch.Op == store.OpDelete {
			delete(next, k)
		} else {
			v := cloneVersion(*ch.Version)
			next[k] = v
			entry.ContentHash, entry.SourceRecordRef = v.Hash, v.SourceRecordRef
		}
		d.provenance = append(d.provenance, entry)
	}
	rev := store.Revision{Scope: c.Scope, Number: number, SnapshotID: store.SnapshotID(number), IngestionID: c.Ingestion.ID, Kind: c.Kind, CreatedAt: c.At}
	d.revisions = append(d.revisions, rev)
	d.records = append(d.records, next)
	ing := c.Ingestion
	ing.Scope, ing.RevisionBefore, ing.RevisionAfter = c.Scope, current, number
	d.ingestions[ing.ID] = ing
	for _, id := range c.RollsBack {
		rolled := d.ingestions[id]
		rolled.RolledBackBy = ing.ID
		d.ingestions[id] = rolled
	}
	return rev, nil
}

func (s *Store) SaveIngestion(_ context.Context, ing store.Ingestion) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	ing.Result = append(json.RawMessage(nil), ing.Result...)
	s.data(ing.Scope).ingestions[ing.ID] = ing
	return nil
}

func (s *Store) Ingestion(_ context.Context, scope adapter.Scope, id string) (store.Ingestion, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if d := s.scopes[scope]; d != nil {
		if ing, ok := d.ingestions[id]; ok {
			return ing, nil
		}
	}
	return store.Ingestion{}, fmt.Errorf("%w: ingestion %s", store.ErrNotFound, id)
}

func (s *Store) Provenance(_ context.Context, scope adapter.Scope, entity, key string) ([]store.ProvenanceEntry, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := []store.ProvenanceEntry{}
	if d := s.scopes[scope]; d != nil {
		for _, p := range d.provenance {
			if p.Entity == entity && p.Key == key {
				out = append(out, p)
			}
		}
	}
	return out, nil
}

func (s *Store) PutManifest(_ context.Context, scope adapter.Scope, m adapter.Manifest) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	data, _ := json.Marshal(m)
	var clone adapter.Manifest
	_ = json.Unmarshal(data, &clone)
	s.data(scope).manifests[m.Name] = clone
	return nil
}

func (s *Store) Manifests(_ context.Context, scope adapter.Scope) ([]adapter.Manifest, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := []adapter.Manifest{}
	if d := s.scopes[scope]; d != nil {
		for _, m := range d.manifests {
			out = append(out, m)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

func (s *Store) SaveEvaluation(_ context.Context, e store.StoredEvaluation) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.data(e.Scope).evaluations[e.ID] = e
	return nil
}

func (s *Store) Evaluation(_ context.Context, scope adapter.Scope, id string) (store.StoredEvaluation, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if d := s.scopes[scope]; d != nil {
		if e, ok := d.evaluations[id]; ok {
			return e, nil
		}
	}
	return store.StoredEvaluation{}, fmt.Errorf("%w: evaluation %s", store.ErrNotFound, id)
}
