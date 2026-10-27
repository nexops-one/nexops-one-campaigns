// SPDX-License-Identifier: Apache-2.0

package memory

import (
	"context"
	"fmt"
	"sort"

	"github.com/nexops-one/compliance-engine/pkg/store"
	"github.com/nexops-one/compliance-engine/sdk/adapter"
)

type storedReport struct {
	report store.Report
	files  map[string]store.ReportFile
}

func (s *Store) SaveReport(_ context.Context, r store.Report, files []store.ReportFile, events ...store.AuditEvent) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.reports[r.Scope][r.ID]; ok {
		return fmt.Errorf("%w: report %s", store.ErrExists, r.ID)
	}
	sealed, err := s.sealLocked(events)
	if err != nil {
		return err
	}
	sr := storedReport{report: store.CloneReport(r), files: map[string]store.ReportFile{}}
	for _, f := range files {
		f.Data = append([]byte{}, f.Data...)
		sr.files[f.Name] = f
	}
	if s.reports[r.Scope] == nil {
		s.reports[r.Scope] = map[string]storedReport{}
	}
	s.reports[r.Scope][r.ID] = sr
	s.storeSealedLocked(sealed)
	return nil
}

func (s *Store) Report(_ context.Context, scope adapter.Scope, id string) (store.Report, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	sr, ok := s.reports[scope][id]
	if !ok {
		return store.Report{}, fmt.Errorf("%w: report %s", store.ErrNotFound, id)
	}
	return store.CloneReport(sr.report), nil
}

func (s *Store) Reports(_ context.Context, scope adapter.Scope) ([]store.Report, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := []store.Report{}
	for _, sr := range s.reports[scope] {
		r := store.CloneReport(sr.report)
		r.Facts, r.Validation, r.Inputs = nil, nil, nil
		out = append(out, r)
	}
	sort.Slice(out, func(i, j int) bool {
		if !out[i].CreatedAt.Equal(out[j].CreatedAt) {
			return out[i].CreatedAt.After(out[j].CreatedAt)
		}
		return out[i].ID < out[j].ID
	})
	return out, nil
}

func (s *Store) ReportFile(_ context.Context, scope adapter.Scope, id, name string) (store.ReportFile, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	f, ok := s.reports[scope][id].files[name]
	if !ok {
		return store.ReportFile{}, fmt.Errorf("%w: report %s file %s", store.ErrNotFound, id, name)
	}
	f.Data = append([]byte{}, f.Data...)
	return f, nil
}

func (s *Store) ReportRefs(_ context.Context, scope adapter.Scope) ([]store.ReportRef, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := []store.ReportRef{}
	for _, sr := range s.reports[scope] {
		r := sr.report
		out = append(out, store.ReportRef{ID: r.ID, EvaluationID: r.EvaluationID, SnapshotID: r.SnapshotID, CreatedAt: r.CreatedAt})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}
