// SPDX-License-Identifier: Apache-2.0

package auth_test

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/nexops-one/compliance-engine/pkg/access"
	"github.com/nexops-one/compliance-engine/pkg/auth"
	"github.com/nexops-one/compliance-engine/pkg/extension"
	"github.com/nexops-one/compliance-engine/sdk/adapter"
)

const secretHash = "2bb80d537b1da3e38bd30361aa855686bde0eacd7162fef6a25fe97bf527a25b"

func TestHashToken(t *testing.T) {
	if got := auth.HashToken("secret"); got != secretHash {
		t.Fatalf("hash = %s", got)
	}
}

func TestGenerateToken(t *testing.T) {
	a, err := auth.GenerateToken()
	if err != nil {
		t.Fatal(err)
	}
	b, _ := auth.GenerateToken()
	if !strings.HasPrefix(a, "ce_") || len(a) != 46 || a == b {
		t.Fatalf("tokens %q %q", a, b)
	}
}

func TestParseTokens(t *testing.T) {
	entries, err := auth.ParseTokens(" " + secretHash + ":acme:ws-1 , " + auth.HashToken("other") + ":acme:ws-2")
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 || entries[0].Hash != secretHash || entries[0].Scope != (adapter.Scope{TenantID: "acme", WorkspaceID: "ws-1"}) {
		t.Fatalf("entries = %+v", entries)
	}
	if entries, err := auth.ParseTokens(""); err != nil || len(entries) != 0 {
		t.Fatalf("empty spec = %+v, %v", entries, err)
	}
	for _, bad := range []string{
		secretHash + ":acme",
		"ABC:acme:ws",
		strings.ToUpper(secretHash) + ":acme:ws",
		secretHash + "::ws",
		secretHash + ":acme:ws," + secretHash + ":acme:ws-2",
	} {
		if _, err := auth.ParseTokens(bad); err == nil {
			t.Errorf("%q should fail", bad)
		}
	}
}

func TestAuthenticate(t *testing.T) {
	s := auth.NewStaticTokens([]auth.TokenEntry{{Hash: secretHash, Scope: adapter.Scope{TenantID: "acme", WorkspaceID: "ws-1"}}})
	r := httptest.NewRequest("GET", "/", nil)
	r.Header.Set("Authorization", "Bearer secret")
	p, err := s.Authenticate(r)
	if err != nil || p.Scope.WorkspaceID != "ws-1" || p.Actor != "token:"+secretHash[:12] {
		t.Fatalf("principal = %+v, %v", p, err)
	}
	for _, header := range []string{"", "Basic secret", "Bearer ", "Bearer wrong", "bearer secret"} {
		r := httptest.NewRequest("GET", "/", nil)
		if header != "" {
			r.Header.Set("Authorization", header)
		}
		if _, err := s.Authenticate(r); !errors.Is(err, auth.ErrUnauthenticated) {
			t.Errorf("header %q: err = %v", header, err)
		}
	}
}

func TestParseTokensRoles(t *testing.T) {
	entries, err := auth.ParseTokens(secretHash + ":acme:ws-1," + auth.HashToken("aud") + ":acme:ws-1:auditor+owner")
	if err != nil {
		t.Fatal(err)
	}
	if got := access.Join(entries[0].Roles, "+"); got != "owner+admin" {
		t.Fatalf("default roles = %s", got)
	}
	if got := access.Join(entries[1].Roles, "+"); got != "auditor+owner" {
		t.Fatalf("listed roles = %s", got)
	}
	for _, bad := range []string{secretHash + ":acme:ws-1:root", secretHash + ":acme:ws-1:", secretHash + ":a:b:owner:x"} {
		if _, err := auth.ParseTokens(bad); err == nil {
			t.Errorf("%q must be refused", bad)
		}
	}
	a := auth.NewStaticTokens(entries)
	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("Authorization", "Bearer aud")
	p, err := a.Authenticate(req)
	if err != nil || p.Kind != extension.ActorToken || access.Join(p.Roles, "+") != "auditor+owner" {
		t.Fatalf("principal = %+v, %v", p, err)
	}
}

type fixed struct {
	p   extension.Principal
	err error
}

func (f fixed) Authenticate(*http.Request) (extension.Principal, error) { return f.p, f.err }

func TestChain(t *testing.T) {
	req := httptest.NewRequest("GET", "/", nil)
	no := fixed{err: errors.New("no")}
	yes := fixed{p: extension.Principal{Actor: "second"}}
	if p, err := auth.Chain(no, nil, yes).Authenticate(req); err != nil || p.Actor != "second" {
		t.Fatalf("chain = %+v, %v", p, err)
	}
	if _, err := auth.Chain(no, no).Authenticate(req); !errors.Is(err, auth.ErrUnauthenticated) {
		t.Fatalf("all fail = %v", err)
	}
	if _, err := auth.Chain().Authenticate(req); !errors.Is(err, auth.ErrUnauthenticated) {
		t.Fatalf("empty chain = %v", err)
	}
}
