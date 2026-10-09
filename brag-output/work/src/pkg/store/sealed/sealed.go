// SPDX-License-Identifier: Apache-2.0

// Package sealed is a store decorator that encrypts sensitive values before
// they reach the underlying store and decrypts them on the way out: the
// sensitive fields of canonical records, evidence locations and revocation
// reasons, assessment notes, and workflow reasons and notes. Every other
// method passes through unchanged, so memory and PostgreSQL behave alike.
package sealed

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"

	"github.com/nexops-one/compliance-engine/pkg/crypt"
	"github.com/nexops-one/compliance-engine/pkg/store"
	"github.com/nexops-one/compliance-engine/sdk/adapter"
)

// Store encrypts sensitive values of an inner store.
type Store struct {
	store.Store
	ring *crypt.Keyring
	sens crypt.Sensitivity
}

var _ store.Store = (*Store)(nil)

// Wrap returns inner behind the sealing decorator.
func Wrap(inner store.Store, ring *crypt.Keyring, sens crypt.Sensitivity) *Store {
	return &Store{Store: inner, ring: ring, sens: sens}
}

// Inner returns the undecorated store.
func (s *Store) Inner() store.Store { return s.Store }

func place(scope adapter.Scope, parts ...string) string {
	out := scope.TenantID + "/" + scope.WorkspaceID
	for _, p := range parts {
		out += "/" + p
	}
	return out
}

func (s *Store) sealString(ctx context.Context, scope adapter.Scope, v string, aad string) (string, error) {
	if v == "" || crypt.IsSealed(v) {
		return v, nil
	}
	d, err := s.ring.Active(ctx, scope.TenantID)
	if err != nil {
		return "", err
	}
	return d.Seal([]byte(v), aad)
}

func (s *Store) openString(ctx context.Context, scope adapter.Scope, v string, aad string) (string, error) {
	if !crypt.IsSealed(v) {
		return v, nil // written before encryption was enabled
	}
	plain, err := crypt.Open(s.ring.Opener(ctx, scope.TenantID), v, aad)
	if err != nil {
		return "", fmt.Errorf("%s: %w", aad, err)
	}
	return string(plain), nil
}

// SealRecord seals the sensitive fields of a record of entity in place.
func (s *Store) SealRecord(ctx context.Context, scope adapter.Scope, entity string, rec adapter.Record) (adapter.Record, error) {
	if len(s.sens[entity]) == 0 || rec == nil {
		return rec, nil
	}
	out := store.CloneRecord(rec)
	for field := range s.sens[entity] {
		v, ok := out[field]
		if !ok || v == nil {
			continue
		}
		if str, isStr := v.(string); isStr && crypt.IsSealed(str) {
			continue
		}
		plain, err := json.Marshal(v)
		if err != nil {
			return nil, err
		}
		d, err := s.ring.Active(ctx, scope.TenantID)
		if err != nil {
			return nil, err
		}
		if out[field], err = d.Seal(plain, place(scope, entity, field)); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// OpenRecord opens the sealed sensitive fields of a record of entity.
func (s *Store) OpenRecord(ctx context.Context, scope adapter.Scope, entity string, rec adapter.Record) (adapter.Record, error) {
	if len(s.sens[entity]) == 0 || rec == nil {
		return rec, nil
	}
	out := rec
	for field := range s.sens[entity] {
		str, ok := out[field].(string)
		if !ok || !crypt.IsSealed(str) {
			continue
		}
		plain, err := crypt.Open(s.ring.Opener(ctx, scope.TenantID), str, place(scope, entity, field))
		if err != nil {
			return nil, fmt.Errorf("%s.%s: %w", entity, field, err)
		}
		dec := json.NewDecoder(bytes.NewReader(plain))
		dec.UseNumber()
		var v any
		if err := dec.Decode(&v); err != nil {
			return nil, err
		}
		out[field] = v
	}
	return out, nil
}

func (s *Store) Commit(ctx context.Context, c store.Commit) (store.Revision, error) {
	changes := make([]store.Change, len(c.Changes))
	for i, ch := range c.Changes {
		if ch.Version != nil {
			v := *ch.Version
			data, err := s.SealRecord(ctx, c.Scope, v.Entity, v.Data)
			if err != nil {
				return store.Revision{}, err
			}
			v.Data = data
			ch.Version = &v
		}
		changes[i] = ch
	}
	c.Changes = changes
	return s.Store.Commit(ctx, c)
}

func (s *Store) Records(ctx context.Context, scope adapter.Scope, revision int64) ([]store.RecordVersion, error) {
	recs, err := s.Store.Records(ctx, scope, revision)
	if err != nil {
		return nil, err
	}
	for i := range recs {
		if recs[i].Data, err = s.OpenRecord(ctx, scope, recs[i].Entity, recs[i].Data); err != nil {
			return nil, err
		}
	}
	return recs, nil
}

func evidencePlace(e store.Evidence, field string) string {
	return place(e.Scope, "evidence", e.ID, field)
}

func (s *Store) sealEvidence(ctx context.Context, e store.Evidence) (store.Evidence, error) {
	var err error
	if e.URI, err = s.sealString(ctx, e.Scope, e.URI, evidencePlace(e, "uri")); err != nil {
		return e, err
	}
	e.RevokeReason, err = s.sealString(ctx, e.Scope, e.RevokeReason, evidencePlace(e, "revoke_reason"))
	return e, err
}

func (s *Store) openEvidence(ctx context.Context, e store.Evidence) (store.Evidence, error) {
	var err error
	if e.URI, err = s.openString(ctx, e.Scope, e.URI, evidencePlace(e, "uri")); err != nil {
		return e, err
	}
	e.RevokeReason, err = s.openString(ctx, e.Scope, e.RevokeReason, evidencePlace(e, "revoke_reason"))
	return e, err
}

func (s *Store) PutEvidence(ctx context.Context, e store.Evidence, expected int64, events ...store.AuditEvent) (store.Evidence, error) {
	sealedEv, err := s.sealEvidence(ctx, e)
	if err != nil {
		return store.Evidence{}, err
	}
	saved, err := s.Store.PutEvidence(ctx, sealedEv, expected, events...)
	if err != nil {
		return store.Evidence{}, err
	}
	return s.openEvidence(ctx, saved)
}

func (s *Store) Evidence(ctx context.Context, scope adapter.Scope, id string) (store.Evidence, error) {
	e, err := s.Store.Evidence(ctx, scope, id)
	if err != nil {
		return store.Evidence{}, err
	}
	return s.openEvidence(ctx, e)
}

func (s *Store) ListEvidence(ctx context.Context, scope adapter.Scope, q store.EvidenceQuery) ([]store.Evidence, error) {
	evs, err := s.Store.ListEvidence(ctx, scope, q)
	if err != nil {
		return nil, err
	}
	for i := range evs {
		if evs[i], err = s.openEvidence(ctx, evs[i]); err != nil {
			return nil, err
		}
	}
	return evs, nil
}

func assessmentPlace(scope adapter.Scope, catalog, control, field string) string {
	return place(scope, "assessment", catalog, control, field)
}

func (s *Store) openAssessment(ctx context.Context, a store.Assessment) (store.Assessment, error) {
	var err error
	a.Notes, err = s.openString(ctx, a.Scope, a.Notes, assessmentPlace(a.Scope, a.Catalog, a.ControlID, "notes"))
	return a, err
}

func (s *Store) openTransition(ctx context.Context, scope adapter.Scope, t store.Transition) (store.Transition, error) {
	var err error
	if t.Reason, err = s.openString(ctx, scope, t.Reason, place(scope, "transition", t.Catalog, t.ControlID, "reason")); err != nil {
		return t, err
	}
	t.Note, err = s.openString(ctx, scope, t.Note, place(scope, "transition", t.Catalog, t.ControlID, "note"))
	return t, err
}

func (s *Store) SaveAssessment(ctx context.Context, a store.Assessment, expected int64, tr *store.Transition, events ...store.AuditEvent) (store.Assessment, error) {
	var err error
	if a.Notes, err = s.sealString(ctx, a.Scope, a.Notes, assessmentPlace(a.Scope, a.Catalog, a.ControlID, "notes")); err != nil {
		return store.Assessment{}, err
	}
	if tr != nil {
		t := *tr
		if t.Reason, err = s.sealString(ctx, a.Scope, t.Reason, place(a.Scope, "transition", a.Catalog, a.ControlID, "reason")); err != nil {
			return store.Assessment{}, err
		}
		if t.Note, err = s.sealString(ctx, a.Scope, t.Note, place(a.Scope, "transition", a.Catalog, a.ControlID, "note")); err != nil {
			return store.Assessment{}, err
		}
		tr = &t
	}
	saved, err := s.Store.SaveAssessment(ctx, a, expected, tr, events...)
	if err != nil {
		return store.Assessment{}, err
	}
	return s.openAssessment(ctx, saved)
}

func (s *Store) Assessment(ctx context.Context, scope adapter.Scope, catalog, controlID string) (store.Assessment, error) {
	a, err := s.Store.Assessment(ctx, scope, catalog, controlID)
	if err != nil {
		return store.Assessment{}, err
	}
	return s.openAssessment(ctx, a)
}

func (s *Store) Assessments(ctx context.Context, scope adapter.Scope, catalog string) ([]store.Assessment, error) {
	as, err := s.Store.Assessments(ctx, scope, catalog)
	if err != nil {
		return nil, err
	}
	for i := range as {
		if as[i], err = s.openAssessment(ctx, as[i]); err != nil {
			return nil, err
		}
	}
	return as, nil
}

func (s *Store) History(ctx context.Context, scope adapter.Scope, catalog, controlID string) ([]store.Transition, error) {
	h, err := s.Store.History(ctx, scope, catalog, controlID)
	if err != nil {
		return nil, err
	}
	for i := range h {
		if h[i], err = s.openTransition(ctx, scope, h[i]); err != nil {
			return nil, err
		}
	}
	return h, nil
}

// Hasher returns HMAC content hashers under each tenant's hashing key. The
// hashing key is that of the tenant's first key version, so rotating the data
// key never changes content hashes.
func Hasher(ring *crypt.Keyring) func(context.Context, adapter.Scope) (func(adapter.Record) string, error) {
	return func(ctx context.Context, scope adapter.Scope) (func(adapter.Record) string, error) {
		if _, err := ring.Active(ctx, scope.TenantID); err != nil { // creates keys on first use
			return nil, err
		}
		d, err := ring.Version(ctx, scope.TenantID, 1)
		if err != nil {
			return nil, err
		}
		return func(rec adapter.Record) string {
			data, _ := json.Marshal(rec)
			return d.HMAC(data)
		}, nil
	}
}

func isSealed(v string) bool { return crypt.IsSealed(v) }

// Ring returns the decorator's keyring.
func (s *Store) Ring() *crypt.Keyring { return s.ring }
