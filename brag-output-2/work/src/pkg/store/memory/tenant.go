// SPDX-License-Identifier: Apache-2.0

package memory

import (
	"context"

	"github.com/nexops-one/compliance-engine/pkg/store"
)

func (s *Store) DeleteTenantData(_ context.Context, tenant string, events ...store.AuditEvent) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	sealed, err := s.sealLocked(events)
	if err != nil {
		return 0, err
	}
	n := 0
	for sc, d := range s.scopes {
		if sc.TenantID == tenant {
			n += len(d.revisions) + len(d.ingestions) + len(d.provenance) + len(d.manifests) + len(d.evaluations)
			delete(s.scopes, sc)
		}
	}
	for sc, m := range s.reports {
		if sc.TenantID == tenant {
			for _, r := range m {
				n += 1 + len(r.files)
			}
			delete(s.reports, sc)
		}
	}
	for sc, m := range s.wf.evidence {
		if sc.TenantID == tenant {
			n += len(m)
			delete(s.wf.evidence, sc)
		}
	}
	for sc, m := range s.wf.assessments {
		if sc.TenantID == tenant {
			n += len(m) + len(s.wf.history[sc])
			delete(s.wf.assessments, sc)
			delete(s.wf.history, sc)
		}
	}
	for sc := range s.kd.settings {
		if sc.TenantID == tenant {
			n++
			delete(s.kd.settings, sc)
		}
	}
	for sc, m := range s.id.members {
		if sc.TenantID == tenant {
			n += len(m)
			delete(s.id.members, sc)
		}
	}
	n += len(s.id.users[tenant])
	delete(s.id.users, tenant)
	for id, t := range s.id.tokens {
		if t.Scope.TenantID == tenant {
			delete(s.id.hashes, t.Hash)
			delete(s.id.tokens, id)
			n++
		}
	}
	for h, se := range s.id.sessions {
		if se.TenantID == tenant {
			delete(s.id.sessions, h)
			n++
		}
	}
	for sc := range s.workspaces {
		if sc.TenantID == tenant {
			delete(s.workspaces, sc)
			n++
		}
	}
	s.storeSealedLocked(sealed)
	return n, nil
}
