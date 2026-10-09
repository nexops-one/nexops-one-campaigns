// SPDX-License-Identifier: Apache-2.0

package console_test

import (
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/nexops-one/compliance-engine/pkg/access"
	"github.com/nexops-one/compliance-engine/pkg/console"
	"github.com/nexops-one/compliance-engine/pkg/identity"
)

// fakeProvider signs in the user named by the callback's "user" parameter,
// as an identity provider would after authenticating them.
type fakeProvider struct {
	h       *harness
	started string // next passed to Start
}

func (p *fakeProvider) ID() string    { return "fake" }
func (p *fakeProvider) Label() string { return "Sign in with Fake IdP" }

func (p *fakeProvider) Start(w http.ResponseWriter, r *http.Request, next string) error {
	p.started = next
	http.Redirect(w, r, "https://idp.example/authorize?state=x", http.StatusFound)
	return nil
}

func (p *fakeProvider) Finish(w http.ResponseWriter, r *http.Request) (identity.SignedIn, string, error) {
	switch user := r.URL.Query().Get("user"); user {
	case "":
		return identity.SignedIn{}, "", errors.New("the identity provider refused the sign-in: access_denied")
	case "unknown":
		return identity.SignedIn{}, "", identity.ErrSignInFailed
	default:
		u, err := p.h.st.UserByEmail(ctx, scope.TenantID, user)
		if err != nil {
			return identity.SignedIn{}, "", err
		}
		in, err := p.h.ids.SignInExternal(r.Context(), scope.TenantID, u.ID, "", "fake", nil)
		return in, r.URL.Query().Get("next"), err
	}
}

func TestSignInProvider(t *testing.T) {
	fp := &fakeProvider{}
	h := newHarness(t, options{console: func(o *console.Options) { o.SignIn = []console.SignInProvider{fp} }})
	fp.h = h
	_ = h.user("anna@example.com", access.RoleOwner)
	cl := &client{h: h, ip: "192.0.2.7"}

	page := cl.get("/console/signin?next=/console/reports")
	if !strings.Contains(page.Body, `href="/console/signin/fake?next=%2Fconsole%2Freports"`) || !strings.Contains(page.Body, "Sign in with Fake IdP") {
		t.Fatalf("sign-in page has no provider button:\n%s", page.Body)
	}
	start := cl.get("/console/signin/fake?next=/console/reports")
	if start.Status != http.StatusFound || !strings.HasPrefix(start.location(), "https://idp.example/") || fp.started != "/console/reports" {
		t.Fatalf("start = %d %q next %q", start.Status, start.location(), fp.started)
	}
	if res := cl.get("/console/signin/fake?next=https://evil.example/"); fp.started != "" || res.Status != http.StatusFound {
		t.Fatalf("an outside next must be dropped: %q", fp.started)
	}

	done := cl.get("/console/signin/fake/callback?user=anna@example.com&next=/console/reports")
	var session *http.Cookie
	for _, ck := range (&http.Response{Header: done.Header}).Cookies() {
		if ck.Name == "ce_session" {
			session = ck
		}
	}
	if done.Status != http.StatusOK || session == nil || session.SameSite != http.SameSiteStrictMode || !session.HttpOnly {
		t.Fatalf("callback = %d cookie %+v", done.Status, session)
	}
	// The page continues by a same-site navigation (meta refresh), not a redirect.
	if !strings.Contains(done.Body, `<meta http-equiv="refresh" content="0;url=/console/reports">`) {
		t.Fatalf("continue page:\n%s", done.Body)
	}
	cl.session = session.Value
	if got := cl.get("/console/reports"); got.Status != http.StatusOK {
		t.Fatalf("signed-in page = %d", got.Status)
	}

	refused := (&client{h: h, ip: "192.0.2.8"}).get("/console/signin/fake/callback")
	if refused.Status != http.StatusUnauthorized || !strings.Contains(refused.Body, "access_denied") {
		t.Fatalf("refused = %d %s", refused.Status, refused.Body)
	}
	unknown := (&client{h: h, ip: "192.0.2.8"}).get("/console/signin/fake/callback?user=unknown")
	if unknown.Status != http.StatusUnauthorized || !strings.Contains(unknown.Body, identity.ErrSignInFailed.Error()) {
		t.Fatalf("unknown = %d", unknown.Status)
	}
	if res := cl.get("/console/signin/nope"); res.Status != http.StatusNotFound {
		t.Fatalf("unknown provider = %d", res.Status)
	}
}

func TestSignInProviderIDValidated(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("a duplicate provider ID must be refused")
		}
	}()
	p := &fakeProvider{}
	newHarness(t, options{console: func(o *console.Options) { o.SignIn = []console.SignInProvider{p, p} }})
}
