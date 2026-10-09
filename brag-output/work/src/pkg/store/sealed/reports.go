// SPDX-License-Identifier: Apache-2.0

package sealed

import (
	"context"

	"github.com/nexops-one/compliance-engine/pkg/store"
	"github.com/nexops-one/compliance-engine/sdk/adapter"
)

// Report bodies (facts, validation and files) are sealed whole: a profile such
// as the register export carries sensitive register values. Metadata and
// inputs stay plaintext. Sealed bodies are stored as the envelope string,
// which no JSON document, PDF or ZIP file can be mistaken for.

func reportPlace(scope adapter.Scope, id string, part ...string) string {
	return place(scope, append([]string{"report", id}, part...)...)
}

func (s *Store) sealBytes(ctx context.Context, scope adapter.Scope, b []byte, aad string) ([]byte, error) {
	if b == nil {
		return nil, nil
	}
	sealed, err := s.sealString(ctx, scope, string(b), aad)
	return []byte(sealed), err
}

func (s *Store) openBytes(ctx context.Context, scope adapter.Scope, b []byte, aad string) ([]byte, error) {
	if b == nil {
		return nil, nil
	}
	plain, err := s.openString(ctx, scope, string(b), aad)
	return []byte(plain), err
}

func (s *Store) SaveReport(ctx context.Context, r store.Report, files []store.ReportFile, events ...store.AuditEvent) error {
	r = store.CloneReport(r)
	var err error
	if r.Facts, err = s.sealBytes(ctx, r.Scope, r.Facts, reportPlace(r.Scope, r.ID, "facts")); err != nil {
		return err
	}
	if r.Validation, err = s.sealBytes(ctx, r.Scope, r.Validation, reportPlace(r.Scope, r.ID, "validation")); err != nil {
		return err
	}
	sealedFiles := make([]store.ReportFile, len(files))
	for i, f := range files {
		if f.Data, err = s.sealBytes(ctx, r.Scope, f.Data, reportPlace(r.Scope, r.ID, "file", f.Name)); err != nil {
			return err
		}
		sealedFiles[i] = f
	}
	return s.Store.SaveReport(ctx, r, sealedFiles, events...)
}

func (s *Store) Report(ctx context.Context, scope adapter.Scope, id string) (store.Report, error) {
	r, err := s.Store.Report(ctx, scope, id)
	if err != nil {
		return r, err
	}
	if r.Facts, err = s.openBytes(ctx, scope, r.Facts, reportPlace(scope, id, "facts")); err != nil {
		return store.Report{}, err
	}
	if r.Validation, err = s.openBytes(ctx, scope, r.Validation, reportPlace(scope, id, "validation")); err != nil {
		return store.Report{}, err
	}
	return r, nil
}

func (s *Store) ReportFile(ctx context.Context, scope adapter.Scope, id, name string) (store.ReportFile, error) {
	f, err := s.Store.ReportFile(ctx, scope, id, name)
	if err != nil {
		return f, err
	}
	if f.Data, err = s.openBytes(ctx, scope, f.Data, reportPlace(scope, id, "file", name)); err != nil {
		return store.ReportFile{}, err
	}
	f.Size = int64(len(f.Data))
	return f, nil
}
