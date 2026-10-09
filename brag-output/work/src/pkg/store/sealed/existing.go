// SPDX-License-Identifier: Apache-2.0

package sealed

import (
	"context"

	"github.com/nexops-one/compliance-engine/pkg/store"
	"github.com/nexops-one/compliance-engine/sdk/adapter"
)

// Rewriter rewrites stored record versions in bulk (PostgreSQL).
type Rewriter interface {
	RewriteRecords(ctx context.Context, tenant string,
		fn func(scope adapter.Scope, entity string, data adapter.Record) (adapter.Record, string, error)) (int, error)
}

// ExistingReport counts what SealExisting rewrote.
type ExistingReport struct {
	Records     int `json:"records"`
	Evidence    int `json:"evidence"`
	Assessments int `json:"assessments"`
}

// SealExisting seals values a tenant stored before encryption was enabled:
// sensitive record fields (rehashing each version with the tenant HMAC and
// remapping provenance hashes), evidence locations and revocation reasons, and
// assessment notes. The append-only transition history is not rewritten, so
// reasons recorded before encryption stay as they were. Running it twice
// changes nothing the second time.
func (s *Store) SealExisting(ctx context.Context, rw Rewriter, tenant string) (ExistingReport, error) {
	var rep ExistingReport
	hasher := Hasher(s.ring)
	n, err := rw.RewriteRecords(ctx, tenant, func(scope adapter.Scope, entity string, data adapter.Record) (adapter.Record, string, error) {
		plain, err := s.OpenRecord(ctx, scope, entity, store.CloneRecord(data))
		if err != nil {
			return nil, "", err
		}
		hash, err := hasher(ctx, scope)
		if err != nil {
			return nil, "", err
		}
		h := hash(plain)
		sealedRec, err := s.SealRecord(ctx, scope, entity, plain)
		return sealedRec, h, err
	})
	rep.Records = n
	if err != nil {
		return rep, err
	}
	scopes, err := s.Store.AuditScopes(ctx, tenant)
	if err != nil {
		return rep, err
	}
	for _, sc := range scopes {
		if sc.WorkspaceID == "" {
			continue
		}
		evs, err := s.Store.ListEvidence(ctx, sc, store.EvidenceQuery{})
		if err != nil {
			return rep, err
		}
		for _, e := range evs {
			if isSealedOrEmpty(e.URI) && isSealedOrEmpty(e.RevokeReason) {
				continue
			}
			if _, err := s.PutEvidence(ctx, e, e.Version); err != nil {
				return rep, err
			}
			rep.Evidence++
		}
		as, err := s.Store.Assessments(ctx, sc, "")
		if err != nil {
			return rep, err
		}
		for _, a := range as {
			if isSealedOrEmpty(a.Notes) {
				continue
			}
			if _, err := s.SaveAssessment(ctx, a, a.Version, nil); err != nil {
				return rep, err
			}
			rep.Assessments++
		}
	}
	return rep, nil
}

func isSealedOrEmpty(v string) bool {
	return v == "" || isSealed(v)
}
