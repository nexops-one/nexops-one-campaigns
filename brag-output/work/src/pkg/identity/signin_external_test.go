// SPDX-License-Identifier: Apache-2.0

package identity_test

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/nexops-one/compliance-engine/pkg/access"
	"github.com/nexops-one/compliance-engine/pkg/identity"
	"github.com/nexops-one/compliance-engine/pkg/store"
	"github.com/nexops-one/compliance-engine/sdk/adapter"
)

func TestSignInExternal(t *testing.T) {
	s, st, _ := newService(t)
	other := adapter.Scope{TenantID: scope.TenantID, WorkspaceID: "ws-2"}
	p, err := s.SyncExternalUser(ctx, scope, identity.ExternalUser{ID: "oidc:abc:sub-1", Email: "anna@example.com"}, roles(access.RoleOwner))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.SyncExternalUser(ctx, other, identity.ExternalUser{ID: "oidc:abc:sub-1", Email: "anna@example.com"}, roles(access.RoleAuditor)); err != nil {
		t.Fatal(err)
	}
	userID, _ := identity.UserID(p)

	in, err := s.SignInExternal(ctx, scope.TenantID, userID, "ws-2", "sso", map[string]any{"issuer": "https://idp.example", "method": "ignored"})
	if err != nil {
		t.Fatal(err)
	}
	if in.Session.WorkspaceID != "ws-2" || len(in.Workspaces) != 2 || in.Value == "" {
		t.Fatalf("signed in = %+v", in)
	}
	got, _, err := s.SessionPrincipal(ctx, in.Value)
	if err != nil || got.Scope != other || len(got.Roles) != 1 || got.Roles[0] != access.RoleAuditor {
		t.Fatalf("session principal = %+v, %v", got, err)
	}
	evs, _ := st.AuditEvents(ctx, adapter.Scope{TenantID: scope.TenantID}, store.AuditQuery{})
	last := evs[len(evs)-1]
	var details map[string]any
	_ = json.Unmarshal(last.Details, &details)
	if last.Action != "login.success" || details["method"] != "sso" || details["issuer"] != "https://idp.example" || details["workspace"] != "ws-2" {
		t.Fatalf("event = %s %v", last.Action, details)
	}

	// An unknown workspace falls back to the first membership.
	if in, err := s.SignInExternal(ctx, scope.TenantID, userID, "nope", "sso", nil); err != nil || in.Session.WorkspaceID != scope.WorkspaceID {
		t.Fatalf("fallback = %+v, %v", in.Session, err)
	}
	if _, err := s.SignInExternal(ctx, scope.TenantID, "u-unknown", "", "sso", nil); !errors.Is(err, identity.ErrSignInFailed) {
		t.Fatalf("unknown user err = %v", err)
	}
}

func TestSignInExternalRefusals(t *testing.T) {
	s, st, _ := newService(t)
	lonely := store.User{TenantID: scope.TenantID, ID: "u-lonely", Email: "lonely@example.com", CreatedAt: t0}
	disabled := store.User{TenantID: scope.TenantID, ID: "u-off", Email: "off@example.com", Disabled: true, CreatedAt: t0}
	for _, u := range []store.User{lonely, disabled} {
		if err := st.CreateUser(ctx, u, store.AuditEvent{Scope: adapter.Scope{TenantID: scope.TenantID}, At: t0, Actor: "system", ActorKind: "system",
			Action: "user.create", TargetType: "user", TargetID: u.ID, Details: json.RawMessage(`{}`)}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.SignInExternal(ctx, scope.TenantID, "u-lonely", "", "sso", nil); !errors.Is(err, identity.ErrNoWorkspace) {
		t.Fatalf("no membership err = %v", err)
	}
	if _, err := s.SignInExternal(ctx, scope.TenantID, "u-off", "", "sso", nil); !errors.Is(err, identity.ErrSignInFailed) {
		t.Fatalf("disabled err = %v", err)
	}
	chain := tenantChain(t, st)
	if n := len(chain); n < 2 || chain[n-1] != "login.failure" || chain[n-2] != "login.failure" {
		t.Fatalf("tenant chain = %v", chain)
	}
}
