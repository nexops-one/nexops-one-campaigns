// SPDX-License-Identifier: Apache-2.0

package identity_test

import (
	"context"
	"errors"
	"testing"

	"github.com/nexops-one/compliance-engine/pkg/access"
	"github.com/nexops-one/compliance-engine/pkg/identity"
	"github.com/nexops-one/compliance-engine/sdk/adapter"
)

func TestWorkspaceGate(t *testing.T) {
	s, _, _ := newService(t)
	closed := errors.New("workspace is suspended")
	var seen []adapter.Scope
	s.SetWorkspaceGate(func(_ context.Context, sc adapter.Scope) error {
		seen = append(seen, sc)
		if sc.WorkspaceID == "closed" {
			return closed
		}
		return nil
	})
	if _, err := s.AddMember(ctx, scope, "anna@example.com", roles(access.RoleOwner)); err != nil {
		t.Fatal(err)
	}
	shut := adapter.Scope{TenantID: scope.TenantID, WorkspaceID: "closed"}
	if _, err := s.AddMember(ctx, shut, "anna@example.com", roles(access.RoleOwner)); !errors.Is(err, closed) {
		t.Fatalf("member in a closed workspace = %v", err)
	}
	if _, err := s.IssueToken(ctx, shut, "", identity.TokenRequest{Name: "svc", Roles: roles(access.RoleOwner), Service: true}); !errors.Is(err, closed) {
		t.Fatalf("token in a closed workspace = %v", err)
	}
	if len(seen) < 3 {
		t.Fatalf("gate calls = %v", seen)
	}
}
