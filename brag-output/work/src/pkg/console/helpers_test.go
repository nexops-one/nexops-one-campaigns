// SPDX-License-Identifier: Apache-2.0

package console_test

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/nexops-one/compliance-engine/pkg/access"
	"github.com/nexops-one/compliance-engine/pkg/compliance"
	"github.com/nexops-one/compliance-engine/pkg/console"
	"github.com/nexops-one/compliance-engine/pkg/httpapi"
	"github.com/nexops-one/compliance-engine/pkg/identity"
	"github.com/nexops-one/compliance-engine/pkg/store/memory"
	"github.com/nexops-one/compliance-engine/sdk/adapter"
)

var (
	ctx   = context.Background()
	scope = adapter.Scope{TenantID: "acme", WorkspaceID: "ws-1"}
)

type harness struct {
	t   *testing.T
	st  *memory.Store
	eng *compliance.Engine
	ids *identity.Service
	c   *console.Console
	now time.Time
}

type options struct {
	engine   func(*compliance.Config)
	console  func(*console.Options)
	identity func(*identity.Options)
}

func newHarness(t *testing.T, o options) *harness {
	t.Helper()
	h := &harness{t: t, st: memory.New(), now: time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC)}
	clock := func() time.Time { return h.now }
	cfg := compliance.Config{Store: h.st, Clock: clock}
	if o.engine != nil {
		o.engine(&cfg)
	}
	eng, err := compliance.New(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	iopts := identity.Options{Clock: clock}
	if o.identity != nil {
		o.identity(&iopts)
	}
	h.eng, h.ids = eng, identity.New(h.st, iopts)
	co := console.Options{Tenant: "acme", Version: "test", Logger: slog.New(slog.NewTextHandler(discard{}, nil)), Clock: clock,
		Failures: httpapi.NewFailureLimiter(30)}
	if o.console != nil {
		o.console(&co)
	}
	h.c = console.New(eng, h.ids, co)
	return h
}

type discard struct{}

func (discard) Write(p []byte) (int, error) { return len(p), nil }

// user creates a workspace member with a password.
func (h *harness) user(email string, rs ...access.Role) string {
	h.t.Helper()
	return h.userIn(scope, email, rs...)
}

func (h *harness) userIn(sc adapter.Scope, email string, rs ...access.Role) string {
	h.t.Helper()
	if _, err := h.ids.AddMember(ctx, sc, email, rs); err != nil {
		h.t.Fatal(err)
	}
	pw, err := h.ids.ResetPassword(ctx, sc.TenantID, email)
	if err != nil {
		h.t.Fatal(err)
	}
	return pw
}

type response struct {
	Status int
	Header http.Header
	Body   string
}

func (r response) location() string { return r.Header.Get("Location") }

// client is a browser with a console session.
type client struct {
	h       *harness
	session string
	ip      string
}

func (h *harness) do(req *http.Request) response {
	rec := httptest.NewRecorder()
	h.c.ServeHTTP(rec, req)
	body, _ := io.ReadAll(rec.Result().Body)
	return response{Status: rec.Code, Header: rec.Result().Header, Body: string(body)}
}

var csrfField = regexp.MustCompile(`name="csrf" value="([^"]+)"`)

// signInAs signs in through the form and returns the signed-in client.
func (h *harness) signInAs(email, pw string) *client {
	h.t.Helper()
	cl := &client{h: h, ip: "192.0.2.1"}
	res, ok := cl.trySignIn("", email, pw)
	if !ok {
		h.t.Fatalf("sign-in of %s failed: %d %s", email, res.Status, res.Body)
	}
	return cl
}

func (cl *client) trySignIn(tenant, email, pw string) (response, bool) {
	h := cl.h
	page := cl.get("/console/signin")
	m := csrfField.FindStringSubmatch(page.Body)
	if m == nil {
		h.t.Fatalf("sign-in page has no csrf field: %s", page.Body)
	}
	var pre string
	for _, ck := range (&http.Response{Header: page.Header}).Cookies() {
		if ck.Name == "ce_signin" {
			pre = ck.Value
		}
	}
	form := url.Values{"csrf": {m[1]}, "email": {email}, "password": {pw}, "tenant": {tenant}}
	req := httptest.NewRequest("POST", "/console/signin", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(&http.Cookie{Name: "ce_signin", Value: pre})
	req.RemoteAddr = cl.ip + ":1234"
	res := h.do(req)
	for _, ck := range (&http.Response{Header: res.Header}).Cookies() {
		if ck.Name == "ce_session" && ck.Value != "" {
			cl.session = ck.Value
			return res, true
		}
	}
	return res, false
}

func (cl *client) request(method, path string, body io.Reader, contentType string) response {
	req := httptest.NewRequest(method, path, body)
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	if cl.session != "" {
		req.AddCookie(&http.Cookie{Name: "ce_session", Value: cl.session})
	}
	req.RemoteAddr = cl.ip + ":1234"
	return cl.h.do(req)
}

func (cl *client) get(path string) response { return cl.request("GET", path, nil, "") }

// csrf reads the form token of the signed-in session from a page.
func (cl *client) csrf() string {
	cl.h.t.Helper()
	m := csrfField.FindStringSubmatch(cl.get("/console/workspace").Body)
	if m == nil {
		cl.h.t.Fatal("no csrf token on the workspace page")
	}
	return m[1]
}

// post submits a form with the session's CSRF token.
func (cl *client) post(path string, form url.Values) response {
	if form == nil {
		form = url.Values{}
	}
	if form.Get("csrf") == "" {
		form.Set("csrf", cl.csrf())
	}
	return cl.postRaw(path, form)
}

func (cl *client) postRaw(path string, form url.Values) response {
	return cl.request("POST", path, strings.NewReader(form.Encode()), "application/x-www-form-urlencoded")
}

// upload submits a multipart form with one file.
func (cl *client) upload(path string, fields map[string]string, fileField, fileName string, data []byte) response {
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	if _, ok := fields["csrf"]; !ok {
		_ = mw.WriteField("csrf", cl.csrf())
	}
	for k, v := range fields {
		_ = mw.WriteField(k, v)
	}
	fw, _ := mw.CreateFormFile(fileField, fileName)
	_, _ = fw.Write(data)
	_ = mw.Close()
	return cl.request("POST", path, &buf, mw.FormDataContentType())
}

// mustContain fails unless body holds every fragment.
func mustContain(t *testing.T, what string, body string, fragments ...string) {
	t.Helper()
	for _, f := range fragments {
		if !strings.Contains(body, f) {
			t.Fatalf("%s does not contain %q:\n%s", what, f, body)
		}
	}
}
