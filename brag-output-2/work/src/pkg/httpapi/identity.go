// SPDX-License-Identifier: Apache-2.0

package httpapi

import (
	"net/http"
	"strconv"

	"github.com/nexops-one/compliance-engine/pkg/access"
	"github.com/nexops-one/compliance-engine/pkg/audit"
	"github.com/nexops-one/compliance-engine/pkg/extension"
	"github.com/nexops-one/compliance-engine/pkg/identity"
	"github.com/nexops-one/compliance-engine/pkg/store"
)

type meResponse struct {
	Actor       string        `json:"actor"`
	Kind        string        `json:"kind"`
	Roles       []access.Role `json:"roles"`
	Permissions []string      `json:"permissions"`
	TenantID    string        `json:"tenant_id"`
	WorkspaceID string        `json:"workspace_id"`
}

func (s *server) me(w http.ResponseWriter, _ *http.Request, p extension.Principal) error {
	perms := []string{}
	for _, perm := range access.Permissions() {
		if access.Allows(p.Roles, perm) {
			perms = append(perms, string(perm))
		}
	}
	writeJSON(w, http.StatusOK, meResponse{Actor: p.Actor, Kind: string(p.Kind), Roles: access.Normalize(p.Roles), Permissions: perms,
		TenantID: p.Scope.TenantID, WorkspaceID: p.Scope.WorkspaceID})
	return nil
}

func (s *server) identity() (*identity.Service, error) {
	if s.opts.Identity == nil {
		return nil, newError(http.StatusNotImplemented, "identity_not_configured", "this deployment does not manage members and tokens through the engine")
	}
	return s.opts.Identity, nil
}

func (s *server) listMembers(w http.ResponseWriter, r *http.Request, p extension.Principal) error {
	id, err := s.identity()
	if err != nil {
		return err
	}
	ms, err := id.Members(r.Context(), p)
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, map[string]any{"members": ms})
	return nil
}

func (s *server) putMember(w http.ResponseWriter, r *http.Request, p extension.Principal) error {
	id, err := s.identity()
	if err != nil {
		return err
	}
	var body struct {
		Roles []access.Role `json:"roles"`
	}
	if err := decodeStrict(r, &body, false); err != nil {
		return err
	}
	m, err := id.SetMember(r.Context(), p, r.PathValue("email"), body.Roles)
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, m)
	return nil
}

func (s *server) deleteMember(w http.ResponseWriter, r *http.Request, p extension.Principal) error {
	id, err := s.identity()
	if err != nil {
		return err
	}
	if err := id.RemoveMember(r.Context(), p, r.PathValue("email")); err != nil {
		return err
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

func (s *server) listTokens(w http.ResponseWriter, r *http.Request, p extension.Principal) error {
	id, err := s.identity()
	if err != nil {
		return err
	}
	ts, err := id.Tokens(r.Context(), p)
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, map[string]any{"tokens": ts})
	return nil
}

func (s *server) postToken(w http.ResponseWriter, r *http.Request, p extension.Principal) error {
	id, err := s.identity()
	if err != nil {
		return err
	}
	var req identity.TokenRequest
	if err := decodeStrict(r, &req, false); err != nil {
		return err
	}
	created, err := id.CreateToken(r.Context(), p, req)
	if err != nil {
		return err
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusCreated, created)
	return nil
}

func (s *server) deleteToken(w http.ResponseWriter, r *http.Request, p extension.Principal) error {
	id, err := s.identity()
	if err != nil {
		return err
	}
	if err := id.RevokeToken(r.Context(), p, r.PathValue("id")); err != nil {
		return err
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

func intParam(r *http.Request, name string, def, max int) (int, error) {
	v := r.URL.Query().Get(name)
	if v == "" {
		return def, nil
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < 0 || n > max {
		return 0, newError(http.StatusBadRequest, "invalid_parameter", name+" must be an integer from 0 to "+strconv.Itoa(max))
	}
	return n, nil
}

type auditResponse struct {
	Events       []store.AuditEvent `json:"events"`
	NextAfterSeq int64              `json:"next_after_seq"`
	Verification *audit.Result      `json:"verification,omitempty"`
}

func (s *server) getAudit(w http.ResponseWriter, r *http.Request, p extension.Principal) error {
	after, err := intParam(r, "after_seq", 0, int(^uint(0)>>1))
	if err != nil {
		return err
	}
	limit, err := intParam(r, "limit", 100, 1000)
	if err != nil {
		return err
	}
	if limit == 0 {
		limit = 100
	}
	verify, err := boolParam(r, "verify")
	if err != nil {
		return err
	}
	evs, err := s.eng.AuditEvents(r.Context(), p.Scope, store.AuditQuery{AfterSeq: int64(after), Limit: limit})
	if err != nil {
		return err
	}
	res := auditResponse{Events: evs, NextAfterSeq: int64(after)}
	if n := len(evs); n > 0 {
		res.NextAfterSeq = evs[n-1].Seq
	}
	if verify {
		v, err := s.eng.VerifyAudit(r.Context(), p.Scope)
		if err != nil {
			return err
		}
		res.Verification = &v
	}
	writeJSON(w, http.StatusOK, res)
	return nil
}
