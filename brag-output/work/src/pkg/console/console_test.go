// SPDX-License-Identifier: Apache-2.0

package console_test

import (
	"io/fs"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/nexops-one/compliance-engine/pkg/access"
	"github.com/nexops-one/compliance-engine/pkg/compliance"
	"github.com/nexops-one/compliance-engine/pkg/console"
	"github.com/nexops-one/compliance-engine/pkg/httpapi"
	"github.com/nexops-one/compliance-engine/pkg/identity"
	"github.com/nexops-one/compliance-engine/sdk/adapter"
)

var param = regexp.MustCompile(`\{[^}]*\}`)

// concrete fills a route pattern's wildcards with placeholder values.
func concrete(pattern string) string {
	return param.ReplaceAllStringFunc(pattern, func(w string) string {
		if w == "{$}" {
			return ""
		}
		return "x"
	})
}

// pages lists the console's GET pages (without static files and downloads).
func pages(c *console.Console) []string {
	var out []string
	for _, rt := range c.Routes() {
		if rt.Method != "GET" || rt.Public || rt.Download || rt.Pattern == console.Prefix+"/{$}" {
			continue
		}
		out = append(out, concrete(rt.Pattern))
	}
	return out
}

func TestSessionCookieAttributes(t *testing.T) {
	h := newHarness(t, options{})
	pw := h.user("anna@example.com", access.RoleOwner)
	cl := &client{h: h, ip: "192.0.2.1"}
	res, ok := cl.trySignIn("", "anna@example.com", pw)
	if !ok || res.Status != http.StatusSeeOther || res.location() != "/console/" {
		t.Fatalf("sign-in = %d %q", res.Status, res.location())
	}
	var session *http.Cookie
	for _, ck := range (&http.Response{Header: res.Header}).Cookies() {
		if ck.Name == "ce_session" {
			session = ck
		}
	}
	if session == nil || !session.HttpOnly || !session.Secure || session.SameSite != http.SameSiteStrictMode ||
		session.Path != "/console" || session.MaxAge != int(identity.DefaultSessionMax.Seconds()) || len(session.Value) < 40 {
		t.Fatalf("session cookie = %+v", session)
	}
	if got := cl.get("/console/workspace"); got.Status != http.StatusOK {
		t.Fatalf("signed-in page = %d", got.Status)
	}
}

func TestSignInRefusals(t *testing.T) {
	h := newHarness(t, options{console: func(o *console.Options) {
		o.Tenant = ""
		o.Failures = httpapi.NewFailureLimiter(3)
	}})
	pw := h.user("anna@example.com", access.RoleOwner)
	cl := &client{h: h, ip: "198.51.100.7"}
	page := cl.get("/console/signin")
	mustContain(t, "sign-in page without a configured tenant", page.Body, `id="tenant"`, `autocomplete="current-password"`)
	for i := 0; i < 3; i++ {
		res, ok := cl.trySignIn("acme", "anna@example.com", "wrong")
		if ok || res.Status != http.StatusUnauthorized {
			t.Fatalf("wrong password = %d", res.Status)
		}
		mustContain(t, "failed sign-in", res.Body, `role="alert"`, "unknown email or wrong password")
	}
	if res, ok := cl.trySignIn("acme", "anna@example.com", pw); ok || res.Status != http.StatusTooManyRequests || res.Header.Get("Retry-After") == "" {
		t.Fatalf("the failure budget must block even a right password: %d", res.Status)
	}
	other := &client{h: h, ip: "198.51.100.8"}
	if _, ok := other.trySignIn("acme", "anna@example.com", pw); !ok {
		t.Fatal("another address keeps its budget")
	}

	// The sign-in form needs its double-submit token.
	form := url.Values{"email": {"anna@example.com"}, "password": {pw}, "tenant": {"acme"}, "csrf": {"forged"}}
	if res := other.postRaw("/console/signin", form); res.Status != http.StatusForbidden {
		t.Fatalf("forged sign-in = %d", res.Status)
	}
}

func TestSignInRedirectsToNext(t *testing.T) {
	h := newHarness(t, options{})
	pw := h.user("anna@example.com", access.RoleOwner)
	anon := &client{h: h}
	res := anon.get("/console/workspace")
	if res.Status != http.StatusSeeOther || res.location() != "/console/signin?next=%2Fconsole%2Fworkspace" {
		t.Fatalf("anonymous = %d %q", res.Status, res.location())
	}
	page := anon.get(res.location())
	mustContain(t, "sign-in page", page.Body, `name="next" value="/console/workspace"`)
	page = anon.get("/console/signin?next=https://evil.example/")
	mustContain(t, "sign-in page", page.Body, `name="next" value=""`)
	_ = pw
}

func TestCSRFRequired(t *testing.T) {
	h := newHarness(t, options{})
	pw := h.user("root@example.com", access.RoleOwner, access.RoleReviewer, access.RoleApprover, access.RoleAdmin, access.RoleAuditor)
	cl := h.signInAs("root@example.com", pw)
	n := 0
	for _, rt := range h.c.Routes() {
		if rt.Method != "POST" || rt.Public {
			continue
		}
		n++
		path := concrete(rt.Pattern)
		for _, token := range []string{"", "forged"} {
			res := cl.postRaw(path, url.Values{"csrf": {token}})
			if res.Status != http.StatusForbidden || !strings.Contains(res.Body, "invalid_csrf") {
				t.Errorf("POST %s with csrf %q = %d", path, token, res.Status)
			}
		}
	}
	if n == 0 {
		t.Fatal("no POST routes")
	}
	if res := cl.post("/console/signout", nil); res.Status != http.StatusSeeOther {
		t.Fatalf("sign-out with the token = %d", res.Status)
	}
}

func TestSecurityHeaders(t *testing.T) {
	h := newHarness(t, options{})
	pw := h.user("root@example.com", access.RoleOwner, access.RoleAdmin, access.RoleAuditor)
	cl := h.signInAs("root@example.com", pw)
	paths := append(pages(h.c), "/console/signin", "/console/nope", "/console/static/console.css")
	for _, p := range paths {
		res := cl.get(p)
		hd := res.Header
		if !strings.HasPrefix(hd.Get("Content-Security-Policy"), "default-src 'self'") || !strings.Contains(hd.Get("Content-Security-Policy"), "frame-ancestors 'none'") ||
			hd.Get("X-Content-Type-Options") != "nosniff" || hd.Get("Referrer-Policy") != "same-origin" {
			t.Errorf("%s: headers %v", p, hd)
		}
		if p != "/console/static/console.css" && hd.Get("Cache-Control") != "no-store" {
			t.Errorf("%s: Cache-Control %q", p, hd.Get("Cache-Control"))
		}
	}
}

func TestSampleBannerOnEveryPage(t *testing.T) {
	h := newHarness(t, options{})
	pw := h.user("root@example.com", access.RoleOwner, access.RoleAdmin, access.RoleAuditor)
	yes := true
	if _, err := h.eng.UpdateSettings(ctx, scope, compliance.SettingsInput{Sample: &yes}); err != nil {
		t.Fatal(err)
	}
	cl := h.signInAs("root@example.com", pw)
	for _, p := range pages(h.c) {
		res := cl.get(p)
		mustContain(t, p, res.Body, console.SampleBanner)
	}
	other := adapter.Scope{TenantID: "acme", WorkspaceID: "ws-2"}
	pw2 := h.userIn(other, "zoe@example.com", access.RoleOwner)
	if res := h.signInAs("zoe@example.com", pw2).get("/console/workspace"); strings.Contains(res.Body, console.SampleBanner) {
		t.Fatal("a workspace that is not a sample shows no banner")
	}
}

func TestNoExternalResources(t *testing.T) {
	external := regexp.MustCompile(`(?i)https?://|(src|href|action)\s*=\s*["']?//|url\(\s*["']?(https?:)?//|@import|<script`)
	err := filepath.WalkDir(".", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !(strings.HasSuffix(path, ".html") || strings.HasSuffix(path, ".css")) {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if m := external.Find(data); m != nil {
			t.Errorf("%s references %q", path, m)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestSignOutEndsSession(t *testing.T) {
	h := newHarness(t, options{})
	pw := h.user("anna@example.com", access.RoleOwner)
	cl := h.signInAs("anna@example.com", pw)
	res := cl.post("/console/signout", nil)
	if res.Status != http.StatusSeeOther || res.location() != "/console/signin?ok=signed_out" {
		t.Fatalf("sign-out = %d %q", res.Status, res.location())
	}
	if res := cl.get("/console/workspace"); res.Status != http.StatusSeeOther {
		t.Fatalf("after sign-out = %d", res.Status)
	}
}

func TestSessionTimeoutInConsole(t *testing.T) {
	h := newHarness(t, options{})
	pw := h.user("anna@example.com", access.RoleOwner)
	cl := h.signInAs("anna@example.com", pw)
	h.now = h.now.Add(identity.DefaultSessionIdle + time.Minute)
	if res := cl.get("/console/workspace"); res.Status != http.StatusSeeOther || !strings.HasPrefix(res.location(), "/console/signin") {
		t.Fatalf("idle session = %d %q", res.Status, res.location())
	}
}

func TestRemovedMemberLosesConsole(t *testing.T) {
	h := newHarness(t, options{})
	pwRoot := h.user("root@example.com", access.RoleAdmin)
	pw := h.user("anna@example.com", access.RoleOwner)
	cl := h.signInAs("anna@example.com", pw)
	root := h.signInAs("root@example.com", pwRoot)
	p, _, err := h.ids.SessionPrincipal(ctx, root.session)
	if err != nil {
		t.Fatal(err)
	}
	if err := h.ids.RemoveMember(ctx, p, "anna@example.com"); err != nil {
		t.Fatal(err)
	}
	if res := cl.get("/console/workspace"); res.Status != http.StatusSeeOther {
		t.Fatalf("removed member = %d", res.Status)
	}
}

func TestWorkspaceSwitch(t *testing.T) {
	h := newHarness(t, options{})
	pw := h.user("anna@example.com", access.RoleOwner)
	other := adapter.Scope{TenantID: "acme", WorkspaceID: "ws-2"}
	h.userIn(other, "anna@example.com", access.RoleAuditor)
	pw, _ = h.ids.ResetPassword(ctx, "acme", "anna@example.com")
	cl := &client{h: h, ip: "192.0.2.1"}
	res, ok := cl.trySignIn("", "anna@example.com", pw)
	if !ok || res.location() != "/console/workspace" {
		t.Fatalf("a member of two workspaces chooses one: %q", res.location())
	}
	page := cl.get("/console/workspace")
	mustContain(t, "workspace page", page.Body, "ws-2", "Open ws-2", "auditor")
	if res := cl.post("/console/workspace", url.Values{"workspace": {"ws-2"}}); res.Status != http.StatusSeeOther {
		t.Fatalf("switch = %d %s", res.Status, res.Body)
	}
	mustContain(t, "after switch", cl.get("/console/workspace").Body, "acme / ws-2")
	res = cl.post("/console/workspace", url.Values{"workspace": {"ws-9"}})
	if res.Status != http.StatusForbidden || !strings.Contains(res.Body, `role="alert"`) {
		t.Fatalf("switch to a foreign workspace = %d", res.Status)
	}
}
