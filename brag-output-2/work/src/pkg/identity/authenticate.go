// SPDX-License-Identifier: Apache-2.0

package identity

import (
	"net/http"
	"strings"
	"time"

	"github.com/nexops-one/compliance-engine/pkg/access"
	"github.com/nexops-one/compliance-engine/pkg/auth"
	"github.com/nexops-one/compliance-engine/pkg/extension"
)

// touchInterval bounds how often a token's last use is written.
const touchInterval = time.Minute

var _ extension.Authenticator = (*Service)(nil)

// Authenticate resolves "Authorization: Bearer <token>" against stored
// tokens. A user token's roles are those it was issued with that the user
// still holds in the workspace; removing a role or the membership takes
// effect on the next request.
func (s *Service) Authenticate(r *http.Request) (extension.Principal, error) {
	value, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	if !ok || strings.TrimSpace(value) == "" {
		return extension.Principal{}, auth.ErrUnauthenticated
	}
	ctx := r.Context()
	t, err := s.st.TokenByHash(ctx, auth.HashToken(value))
	now := s.now()
	if err != nil || !t.Active(now) {
		return extension.Principal{}, auth.ErrUnauthenticated
	}
	p := extension.Principal{Scope: t.Scope, Actor: "token:" + t.ID, Kind: extension.ActorToken, Roles: access.Normalize(t.Roles)}
	if t.UserID != "" {
		u, err := s.st.User(ctx, t.Scope.TenantID, t.UserID)
		if err != nil || u.Disabled {
			return extension.Principal{}, auth.ErrUnauthenticated
		}
		m, err := s.st.Member(ctx, t.Scope, t.UserID)
		if err != nil {
			return extension.Principal{}, auth.ErrUnauthenticated
		}
		p.Actor, p.Kind, p.Roles = "user:"+t.UserID, extension.ActorUser, access.Intersect(t.Roles, m.Roles)
	}
	if len(p.Roles) == 0 {
		return extension.Principal{}, auth.ErrUnauthenticated
	}
	if t.LastUsedAt == nil || now.Sub(*t.LastUsedAt) >= touchInterval {
		_ = s.st.TouchToken(ctx, t.ID, now) // best effort: a failed touch never refuses a valid token
	}
	return p, nil
}
