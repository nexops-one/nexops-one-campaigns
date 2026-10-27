// SPDX-License-Identifier: Apache-2.0

package storetest

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/nexops-one/compliance-engine/pkg/access"
	"github.com/nexops-one/compliance-engine/pkg/store"
	"github.com/nexops-one/compliance-engine/sdk/adapter"
)

var ctx = context.Background()

func tenantScope(tenantID string) adapter.Scope { return adapter.Scope{TenantID: tenantID} }

func user(id, email string) store.User {
	return store.User{TenantID: scopeA.TenantID, ID: id, Email: email, PasswordHash: "hash-" + id, CreatedAt: at}
}

func token(id string, scope adapter.Scope, userID string, roles ...access.Role) store.Token {
	return store.Token{ID: id, Scope: scope, Hash: "hash-" + id, Name: "name " + id, UserID: userID, Roles: roles, CreatedBy: "user:admin", CreatedAt: at}
}

func mustUser(t *testing.T, s store.Store, u store.User) {
	t.Helper()
	if err := s.CreateUser(ctx, u, Event(tenantScope(u.TenantID), "user.create")); err != nil {
		t.Fatal(err)
	}
}

func mustMember(t *testing.T, s store.Store, scope adapter.Scope, userID string, roles ...access.Role) {
	t.Helper()
	if err := s.PutMember(ctx, store.Member{Scope: scope, UserID: userID, Roles: roles, UpdatedAt: at}, Event(scope, "member.set")); err != nil {
		t.Fatal(err)
	}
}

func mustToken(t *testing.T, s store.Store, tok store.Token) {
	t.Helper()
	if err := s.CreateToken(ctx, tok, Event(tok.Scope, "token.create")); err != nil {
		t.Fatal(err)
	}
}

func runIdentity(t *testing.T, newStore func(t *testing.T) store.Store) {
	t.Run("users", func(t *testing.T) {
		s := newStore(t)
		mustUser(t, s, user("u2", "zoe@example.com"))
		mustUser(t, s, user("u1", "anna@example.com"))
		if err := s.CreateUser(ctx, user("u3", "anna@example.com"), Event(tenantScope("tenant-a"), "user.create")); !errors.Is(err, store.ErrExists) {
			t.Fatalf("duplicate email: %v", err)
		}
		other := user("u1", "anna@example.com")
		other.TenantID = "tenant-b"
		mustUser(t, s, other) // emails and IDs are per tenant
		got, err := s.UserByEmail(ctx, "tenant-a", "anna@example.com")
		if err != nil || got.ID != "u1" || got.PasswordHash != "hash-u1" || !got.CreatedAt.Equal(at) {
			t.Fatalf("by email = %+v, %v", got, err)
		}
		if got, err := s.User(ctx, "tenant-a", "u2"); err != nil || got.Email != "zoe@example.com" {
			t.Fatalf("by id = %+v, %v", got, err)
		}
		if _, err := s.User(ctx, "tenant-a", "nobody"); !errors.Is(err, store.ErrNotFound) {
			t.Fatalf("unknown user: %v", err)
		}
		users, err := s.Users(ctx, "tenant-a")
		if err != nil || len(users) != 2 || users[0].Email != "anna@example.com" {
			t.Fatalf("users = %+v, %v", users, err)
		}
		if err := s.SetPassword(ctx, "tenant-a", "u1", "new", Event(tenantScope("tenant-a"), "user.password_reset")); err != nil {
			t.Fatal(err)
		}
		if got, _ := s.User(ctx, "tenant-a", "u1"); got.PasswordHash != "new" {
			t.Fatalf("password = %q", got.PasswordHash)
		}
		if err := s.SetPassword(ctx, "tenant-a", "nobody", "x", Event(tenantScope("tenant-a"), "user.password_reset")); !errors.Is(err, store.ErrNotFound) {
			t.Fatalf("unknown user password: %v", err)
		}
		if got := ChainOf(t, s, tenantScope("tenant-a")); !reflect.DeepEqual(got, []string{"user.create", "user.create", "user.password_reset"}) {
			t.Fatalf("tenant chain = %v", got)
		}
	})

	t.Run("external users", func(t *testing.T) {
		s := newStore(t)
		ext := user("u1", "anna@example.com")
		ext.ExternalID = "host:42"
		mustUser(t, s, ext)
		mustUser(t, s, user("u2", "zoe@example.com")) // local user: no external ID
		dup := user("u3", "bob@example.com")
		dup.ExternalID = "host:42"
		if err := s.CreateUser(ctx, dup, Event(tenantScope("tenant-a"), "user.create")); !errors.Is(err, store.ErrExists) {
			t.Fatalf("duplicate external ID: %v", err)
		}
		other := user("u1", "anna@example.com")
		other.TenantID, other.ExternalID = "tenant-b", "host:42"
		mustUser(t, s, other) // external IDs are per tenant
		got, err := s.UserByExternalID(ctx, "tenant-a", "host:42")
		if err != nil || got.ID != "u1" || got.ExternalID != "host:42" || got.Email != "anna@example.com" {
			t.Fatalf("by external ID = %+v, %v", got, err)
		}
		if got, _ := s.User(ctx, "tenant-a", "u2"); got.ExternalID != "" {
			t.Fatalf("local user external ID = %q", got.ExternalID)
		}
		if _, err := s.UserByExternalID(ctx, "tenant-a", "host:7"); !errors.Is(err, store.ErrNotFound) {
			t.Fatalf("unknown external ID: %v", err)
		}
		if _, err := s.UserByExternalID(ctx, "tenant-a", ""); !errors.Is(err, store.ErrNotFound) {
			t.Fatalf("empty external ID matches local users: %v", err)
		}
		if err := s.SetUserEmail(ctx, "tenant-a", "u1", "anna@new.example", Event(tenantScope("tenant-a"), "user.email")); err != nil {
			t.Fatal(err)
		}
		if got, _ := s.UserByEmail(ctx, "tenant-a", "anna@new.example"); got.ID != "u1" || got.ExternalID != "host:42" {
			t.Fatalf("after email change = %+v", got)
		}
		if err := s.SetUserEmail(ctx, "tenant-a", "u1", "zoe@example.com", Event(tenantScope("tenant-a"), "user.email")); !errors.Is(err, store.ErrExists) {
			t.Fatalf("email taken: %v", err)
		}
		if err := s.SetUserEmail(ctx, "tenant-a", "nobody", "x@example.com", Event(tenantScope("tenant-a"), "user.email")); !errors.Is(err, store.ErrNotFound) {
			t.Fatalf("unknown user email: %v", err)
		}
		if got := ChainOf(t, s, tenantScope("tenant-a")); !reflect.DeepEqual(got, []string{"user.create", "user.create", "user.email"}) {
			t.Fatalf("tenant chain = %v", got)
		}
	})

	t.Run("members", func(t *testing.T) {
		s := newStore(t)
		mustUser(t, s, user("u1", "zoe@example.com"))
		mustUser(t, s, user("u2", "anna@example.com"))
		mustMember(t, s, scopeA, "u1", access.RoleAdmin, access.RoleOwner, access.RoleOwner)
		mustMember(t, s, scopeA, "u2", access.RoleAuditor)
		mustMember(t, s, scopeB, "u1", access.RoleApprover)
		m, err := s.Member(ctx, scopeA, "u1")
		if err != nil || m.Email != "zoe@example.com" || !reflect.DeepEqual(m.Roles, []access.Role{access.RoleOwner, access.RoleAdmin}) {
			t.Fatalf("member = %+v, %v", m, err)
		}
		mustMember(t, s, scopeA, "u1", access.RoleReviewer)
		if m, _ := s.Member(ctx, scopeA, "u1"); !reflect.DeepEqual(m.Roles, []access.Role{access.RoleReviewer}) {
			t.Fatalf("roles replaced = %v", m.Roles)
		}
		ms, err := s.Members(ctx, scopeA)
		if err != nil || len(ms) != 2 || ms[0].Email != "anna@example.com" || ms[1].UserID != "u1" {
			t.Fatalf("members = %+v, %v", ms, err)
		}
		if err := s.PutMember(ctx, store.Member{Scope: scopeA, UserID: "nobody", Roles: []access.Role{access.RoleOwner}, UpdatedAt: at}, Event(scopeA, "member.set")); !errors.Is(err, store.ErrNotFound) {
			t.Fatalf("unknown user: %v", err)
		}
		if _, err := s.Member(ctx, scopeB, "u2"); !errors.Is(err, store.ErrNotFound) {
			t.Fatalf("memberships are per workspace: %v", err)
		}
	})

	t.Run("delete member revokes their tokens in that workspace only", func(t *testing.T) {
		s := newStore(t)
		mustUser(t, s, user("u1", "zoe@example.com"))
		mustMember(t, s, scopeA, "u1", access.RoleOwner)
		mustMember(t, s, scopeB, "u1", access.RoleOwner)
		mustToken(t, s, token("tok-a", scopeA, "u1", access.RoleOwner))
		mustToken(t, s, token("tok-svc", scopeA, "", access.RoleOwner))
		mustToken(t, s, token("tok-b", scopeB, "u1", access.RoleOwner))
		later := at.Add(time.Hour)
		if err := s.DeleteMember(ctx, scopeA, "u1", later, Event(scopeA, "member.remove")); err != nil {
			t.Fatal(err)
		}
		if _, err := s.Member(ctx, scopeA, "u1"); !errors.Is(err, store.ErrNotFound) {
			t.Fatalf("member still present: %v", err)
		}
		if tok, _ := s.TokenByHash(ctx, "hash-tok-a"); tok.RevokedAt == nil || !tok.RevokedAt.Equal(later) {
			t.Fatalf("the user's token in the workspace must be revoked: %+v", tok)
		}
		if tok, _ := s.TokenByHash(ctx, "hash-tok-svc"); tok.RevokedAt != nil {
			t.Fatal("service tokens are not the user's")
		}
		if tok, _ := s.TokenByHash(ctx, "hash-tok-b"); tok.RevokedAt != nil {
			t.Fatal("tokens of another workspace must stay active")
		}
		if err := s.DeleteMember(ctx, scopeA, "u1", later, Event(scopeA, "member.remove")); !errors.Is(err, store.ErrNotFound) {
			t.Fatalf("second delete: %v", err)
		}
	})

	t.Run("tokens", func(t *testing.T) {
		s := newStore(t)
		mustUser(t, s, user("u1", "zoe@example.com"))
		exp := at.Add(24 * time.Hour)
		t2 := token("tok-2", scopeA, "u1", access.RoleOwner, access.RoleAuditor)
		t2.CreatedAt, t2.ExpiresAt = at.Add(time.Minute), &exp
		mustToken(t, s, t2)
		mustToken(t, s, token("tok-1", scopeA, "", access.RoleOwner))
		dup := token("tok-3", scopeA, "", access.RoleOwner)
		dup.Hash = "hash-tok-1"
		if err := s.CreateToken(ctx, dup, Event(scopeA, "token.create")); !errors.Is(err, store.ErrExists) {
			t.Fatalf("duplicate hash: %v", err)
		}
		got, err := s.TokenByHash(ctx, "hash-tok-2")
		if err != nil || got.ID != "tok-2" || got.UserID != "u1" || got.Scope != scopeA || got.ExpiresAt == nil || !got.ExpiresAt.Equal(exp) ||
			!reflect.DeepEqual(got.Roles, []access.Role{access.RoleAuditor, access.RoleOwner}) || got.Name != "name tok-2" || got.CreatedBy != "user:admin" {
			t.Fatalf("by hash = %+v, %v", got, err)
		}
		if _, err := s.TokenByHash(ctx, "nope"); !errors.Is(err, store.ErrNotFound) {
			t.Fatalf("unknown hash: %v", err)
		}
		list, err := s.Tokens(ctx, scopeA)
		if err != nil || len(list) != 2 || list[0].ID != "tok-1" || list[1].ID != "tok-2" {
			t.Fatalf("tokens = %+v, %v", list, err)
		}
		if other, _ := s.Tokens(ctx, scopeB); len(other) != 0 {
			t.Fatal("tokens must be scoped")
		}
		used := at.Add(2 * time.Hour)
		if err := s.TouchToken(ctx, "tok-1", used); err != nil {
			t.Fatal(err)
		}
		if got, _ := s.TokenByHash(ctx, "hash-tok-1"); got.LastUsedAt == nil || !got.LastUsedAt.Equal(used) {
			t.Fatalf("touch: %+v", got)
		}
		if err := s.RevokeToken(ctx, scopeB, "tok-1", used, Event(scopeB, "token.revoke")); !errors.Is(err, store.ErrNotFound) {
			t.Fatalf("revoking through another workspace: %v", err)
		}
		if err := s.RevokeToken(ctx, scopeA, "tok-1", used, Event(scopeA, "token.revoke")); err != nil {
			t.Fatal(err)
		}
		if err := s.RevokeToken(ctx, scopeA, "tok-1", used.Add(time.Hour), Event(scopeA, "token.revoke")); err != nil {
			t.Fatalf("second revoke: %v", err)
		}
		if got, _ := s.TokenByHash(ctx, "hash-tok-1"); got.RevokedAt == nil || !got.RevokedAt.Equal(used) || got.Active(used) {
			t.Fatalf("revoked: %+v", got)
		}
		if got := ChainOf(t, s, scopeA); !reflect.DeepEqual(got, []string{"token.create", "token.create", "token.revoke"}) {
			t.Fatalf("second revoke must not append an event: %v", got)
		}
	})

	t.Run("has active tokens", func(t *testing.T) {
		s := newStore(t)
		if ok, err := s.HasActiveTokens(ctx, at); err != nil || ok {
			t.Fatalf("empty: %v, %v", ok, err)
		}
		exp := at.Add(time.Hour)
		expiring := token("tok-exp", scopeA, "", access.RoleOwner)
		expiring.ExpiresAt = &exp
		mustToken(t, s, expiring)
		if ok, _ := s.HasActiveTokens(ctx, at); !ok {
			t.Fatal("an unexpired token is active")
		}
		if ok, _ := s.HasActiveTokens(ctx, exp); ok {
			t.Fatal("an expired token is not active")
		}
		mustToken(t, s, token("tok-rev", scopeB, "", access.RoleOwner))
		if err := s.RevokeToken(ctx, scopeB, "tok-rev", at, Event(scopeB, "token.revoke")); err != nil {
			t.Fatal(err)
		}
		if ok, _ := s.HasActiveTokens(ctx, exp); ok {
			t.Fatal("a revoked token is not active")
		}
	})

	t.Run("identity mutations are audited and atomic", func(t *testing.T) {
		s := newStore(t)
		tenant := tenantScope(scopeA.TenantID)
		if err := s.CreateUser(ctx, user("u1", "a@example.com"), Invalid(tenant)); err == nil {
			t.Fatal("CreateUser with an invalid event must fail")
		}
		if _, err := s.User(ctx, scopeA.TenantID, "u1"); !errors.Is(err, store.ErrNotFound) {
			t.Fatal("CreateUser must not persist the user when its event fails")
		}
		mustUser(t, s, user("u1", "a@example.com"))
		if err := s.SetPassword(ctx, scopeA.TenantID, "u1", "changed", Invalid(tenant)); err == nil {
			t.Fatal("SetPassword with an invalid event must fail")
		}
		if u, _ := s.User(ctx, scopeA.TenantID, "u1"); u.PasswordHash != "hash-u1" {
			t.Fatal("SetPassword must not persist when its event fails")
		}
		if err := s.PutMember(ctx, store.Member{Scope: scopeA, UserID: "u1", Roles: []access.Role{access.RoleOwner}, UpdatedAt: at}, Invalid(scopeA)); err == nil {
			t.Fatal("PutMember with an invalid event must fail")
		}
		if _, err := s.Member(ctx, scopeA, "u1"); !errors.Is(err, store.ErrNotFound) {
			t.Fatal("PutMember must not persist when its event fails")
		}
		mustMember(t, s, scopeA, "u1", access.RoleOwner)
		if err := s.CreateToken(ctx, token("tok-1", scopeA, "u1", access.RoleOwner), Invalid(scopeA)); err == nil {
			t.Fatal("CreateToken with an invalid event must fail")
		}
		if _, err := s.TokenByHash(ctx, "hash-tok-1"); !errors.Is(err, store.ErrNotFound) {
			t.Fatal("CreateToken must not persist when its event fails")
		}
		mustToken(t, s, token("tok-1", scopeA, "u1", access.RoleOwner))
		if err := s.RevokeToken(ctx, scopeA, "tok-1", at, Invalid(scopeA)); err == nil {
			t.Fatal("RevokeToken with an invalid event must fail")
		}
		if err := s.DeleteMember(ctx, scopeA, "u1", at, Invalid(scopeA)); err == nil {
			t.Fatal("DeleteMember with an invalid event must fail")
		}
		if _, err := s.Member(ctx, scopeA, "u1"); err != nil {
			t.Fatal("DeleteMember must not persist when its event fails")
		}
		if tok, _ := s.TokenByHash(ctx, "hash-tok-1"); tok.RevokedAt != nil {
			t.Fatal("failed revocations must not persist")
		}
		if got := ChainOf(t, s, tenant); !reflect.DeepEqual(got, []string{"user.create"}) {
			t.Fatalf("tenant chain = %v", got)
		}
		if got := ChainOf(t, s, scopeA); !reflect.DeepEqual(got, []string{"member.set", "token.create"}) {
			t.Fatalf("workspace chain = %v", got)
		}
	})
}
