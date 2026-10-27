// SPDX-License-Identifier: Apache-2.0

package identity

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/nexops-one/compliance-engine/pkg/access"
	"github.com/nexops-one/compliance-engine/pkg/extension"
	"github.com/nexops-one/compliance-engine/pkg/store"
	"github.com/nexops-one/compliance-engine/sdk/adapter"
)

// HostActor is recorded for changes a host authenticator makes when the
// context carries no principal.
const HostActor = "host"

// ErrEmailInUse: the email belongs to another user of the tenant. A host
// never takes over a local account or another host user.
var ErrEmailInUse = errors.New("the email belongs to another user of this tenant")

// ExternalUser is a user a host product authenticated.
type ExternalUser struct {
	ID    string // stable host identifier, for example "nexops:<user id>"
	Email string
}

// SyncExternalUser makes the engine's view of a host user match the host:
// it creates the user (without a password) on first sight, follows email
// changes, and sets the user's roles in scope. It writes, and audits, only
// what changed. It returns the principal the user acts as. Removing the
// admin role from the workspace's last admin is refused with ErrLastAdmin.
func (s *Service) SyncExternalUser(ctx context.Context, scope adapter.Scope, ext ExternalUser, roles []access.Role) (extension.Principal, error) {
	if err := scope.Validate(); err != nil {
		return extension.Principal{}, fmt.Errorf("%w: %v", ErrInvalidRequest, err)
	}
	if strings.TrimSpace(ext.ID) == "" {
		return extension.Principal{}, fmt.Errorf("%w: external user ID is required", ErrInvalidRequest)
	}
	email, err := normalizeEmail(ext.Email)
	if err != nil {
		return extension.Principal{}, err
	}
	if len(roles) == 0 {
		return extension.Principal{}, fmt.Errorf("%w: at least one role is required", ErrInvalidRequest)
	}
	if err := access.Validate(roles); err != nil {
		return extension.Principal{}, fmt.Errorf("%w: %v", ErrInvalidRequest, err)
	}
	roles = access.Normalize(roles)
	if _, ok := extension.PrincipalFrom(ctx); !ok {
		ctx = withCaller(ctx, extension.Principal{Scope: scope, Actor: HostActor, Kind: extension.ActorSystem})
	}
	tenantScope := adapter.Scope{TenantID: scope.TenantID}

	u, err := s.st.UserByExternalID(ctx, scope.TenantID, ext.ID)
	switch {
	case errors.Is(err, store.ErrNotFound):
		if err := s.emailFree(ctx, scope.TenantID, email, "", ext.ID); err != nil {
			return extension.Principal{}, err
		}
		u = store.User{TenantID: scope.TenantID, ID: s.newID("usr"), Email: email, ExternalID: ext.ID, CreatedAt: s.now()}
		ev := s.event(ctx, tenantScope, "user.create", "user", u.ID, map[string]any{"email": email, "external_id": ext.ID, "password": false})
		if err := s.st.CreateUser(ctx, u, ev); err != nil {
			// A concurrent sync of the same user created it first: use that one.
			again, gerr := s.st.UserByExternalID(ctx, scope.TenantID, ext.ID)
			if !errors.Is(err, store.ErrExists) || gerr != nil {
				return extension.Principal{}, emailConflict(err)
			}
			u = again
		}
	case err != nil:
		return extension.Principal{}, err
	case u.Disabled:
		return extension.Principal{}, fmt.Errorf("%w: user %s is disabled", ErrForbidden, u.ID)
	case u.Email != email:
		if err := s.emailFree(ctx, scope.TenantID, email, u.ID, ext.ID); err != nil {
			return extension.Principal{}, err
		}
		ev := s.event(ctx, tenantScope, "user.email", "user", u.ID, map[string]any{"from": u.Email, "to": email, "external_id": ext.ID})
		if err := s.st.SetUserEmail(ctx, scope.TenantID, u.ID, email, ev); err != nil {
			return extension.Principal{}, emailConflict(err)
		}
	}

	cur, err := s.st.Member(ctx, scope, u.ID)
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		return extension.Principal{}, err
	}
	if err != nil || !sameRoles(cur.Roles, roles) {
		if err == nil && access.Contains(cur.Roles, access.RoleAdmin) && !access.Contains(roles, access.RoleAdmin) {
			if n, err := s.otherAdmins(ctx, scope, u.ID); err != nil {
				return extension.Principal{}, err
			} else if n == 0 {
				return extension.Principal{}, ErrLastAdmin
			}
		}
		m := store.Member{Scope: scope, UserID: u.ID, Email: email, Roles: roles, UpdatedAt: s.now()}
		ev := s.event(ctx, scope, "member.sync", "user", u.ID, map[string]any{"email": email, "external_id": ext.ID, "roles": roles})
		if err := s.st.PutMember(ctx, m, ev); err != nil {
			return extension.Principal{}, err
		}
	}
	return extension.Principal{Scope: scope, Actor: "user:" + u.ID, Kind: extension.ActorUser, Roles: roles}, nil
}

// emailFree refuses an email held by another user: neither selfID nor, for a
// user not yet seen, the user a concurrent sync created for selfExternalID.
func (s *Service) emailFree(ctx context.Context, tenantID, email, selfID, selfExternalID string) error {
	other, err := s.st.UserByEmail(ctx, tenantID, email)
	switch {
	case errors.Is(err, store.ErrNotFound):
		return nil
	case err != nil:
		return err
	case other.ID != selfID && (other.ExternalID == "" || other.ExternalID != selfExternalID):
		return fmt.Errorf("%w: %s", ErrEmailInUse, email)
	}
	return nil
}

// emailConflict maps a store uniqueness conflict (a concurrent sync) to ErrEmailInUse.
func emailConflict(err error) error {
	if errors.Is(err, store.ErrExists) {
		return fmt.Errorf("%w: %v", ErrEmailInUse, err)
	}
	return err
}

func sameRoles(a, b []access.Role) bool {
	a, b = access.Normalize(a), access.Normalize(b)
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
