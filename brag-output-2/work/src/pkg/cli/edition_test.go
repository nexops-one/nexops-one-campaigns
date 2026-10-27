// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/nexops-one/compliance-engine/catalogs"
	"github.com/nexops-one/compliance-engine/pkg/access"
	"github.com/nexops-one/compliance-engine/pkg/catalog"
	"github.com/nexops-one/compliance-engine/pkg/compliance"
	"github.com/nexops-one/compliance-engine/pkg/console"
	"github.com/nexops-one/compliance-engine/pkg/extension"
	"github.com/nexops-one/compliance-engine/pkg/httpapi"
	"github.com/nexops-one/compliance-engine/pkg/identity"
	"github.com/nexops-one/compliance-engine/sdk/adapter"
)

type allowAll struct{}

func (allowAll) Allowed(context.Context, adapter.Scope, extension.Feature) extension.Decision {
	return extension.Decision{Allowed: true}
}

type testProfile struct{}

func (testProfile) ID() string                 { return "test_profile" }
func (testProfile) Feature() extension.Feature { return extension.FeatureReportProfileA }
func (testProfile) Formats() []string          { return nil }
func (testProfile) Generate(_ context.Context, in extension.ReportInput) (extension.ReportOutput, error) {
	facts, _ := json.Marshal(map[string]any{"meta": in.Meta})
	return extension.ReportOutput{Facts: facts, Complete: true}, nil
}

// headerAuth authenticates "X-Test-User: <workspace>" as an approver there.
type headerAuth struct{}

func (headerAuth) Authenticate(r *http.Request) (extension.Principal, error) {
	ws := r.Header.Get("X-Test-User")
	if ws == "" {
		return extension.Principal{}, errors.New("no test user")
	}
	return extension.Principal{Scope: adapter.Scope{TenantID: "acme", WorkspaceID: ws}, Actor: "user:test", Kind: extension.ActorUser,
		Roles: []access.Role{access.RoleApprover}}, nil
}

type aboutSection struct{ value string }

func (aboutSection) AboutKey() string { return "edition_info" }
func (a aboutSection) About(context.Context, adapter.Scope) any {
	return map[string]string{"setting": a.value}
}

type noProvider struct{}

func (noProvider) ID() string                                             { return "test-sso" }
func (noProvider) Label() string                                          { return "Sign in with the test IdP" }
func (noProvider) Start(http.ResponseWriter, *http.Request, string) error { return nil }
func (noProvider) Finish(http.ResponseWriter, *http.Request) (identity.SignedIn, string, error) {
	return identity.SignedIn{}, "", identity.ErrSignInFailed
}

func testEdition(t *testing.T, cleaned *bool) Edition {
	dora, err := catalogs.FS.ReadFile("dora/1.0.0.yaml")
	if err != nil {
		t.Fatal(err)
	}
	extra := fstest.MapFS{"dora/1.9.0.yaml": {Data: bytes.Replace(dora, []byte("version: 1.0.0"), []byte("version: 1.9.0"), 1)}}
	return Edition{
		Name: "test", Version: "9.9.9",
		Usage: "  compliance-engine hello                                    say hello\n",
		Commands: map[string]Command{"hello": func(_ context.Context, args []string, env Env) int {
			fmt.Fprintln(env.Stdout, "hello", strings.Join(args, " "), env.Getenv("COMPLIANCE_TEST_GREETING"))
			return 0
		}},
		Setup: func(_ context.Context, rt Runtime) (Plugins, error) {
			if rt.Store == nil || rt.Identity == nil || rt.Logger == nil || rt.Clock == nil {
				return Plugins{}, errors.New("incomplete runtime")
			}
			if rt.Getenv("COMPLIANCE_TEST_FAIL") == "true" {
				return Plugins{}, errors.New("COMPLIANCE_TEST_FAIL is set")
			}
			return Plugins{
				Entitlements:   allowAll{},
				Catalogs:       catalog.FSSource{FS: extra},
				Extensions:     compliance.Extensions{ReportProfiles: []extension.ReportProfile{testProfile{}}},
				Authenticators: []extension.Authenticator{headerAuth{}},
				Routes: func(eng *compliance.Engine) []httpapi.ExtraRoute {
					return []httpapi.ExtraRoute{{Route: httpapi.Route{Method: "GET", Pattern: "/api/v1/test/catalogs", Permission: access.PermDataRead},
						Handle: func(w http.ResponseWriter, _ *http.Request, _ extension.Principal) error {
							httpapi.WriteJSON(w, http.StatusOK, eng.Catalogs())
							return nil
						}}}
				},
				Handlers: func(*compliance.Engine) map[string]http.Handler {
					return map[string]http.Handler{"GET /api/v1/admin/ping": http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
						_, _ = io.WriteString(w, "pong")
					})}
				},
				About:   []extension.AboutSection{aboutSection{rt.Getenv("COMPLIANCE_TEST_GREETING")}},
				SignIn:  []console.SignInProvider{noProvider{}},
				Cleanup: func() { *cleaned = true },
			}, nil
		},
		Docs: func(context.Context) (map[string][]byte, error) {
			return map[string][]byte{"test-edition.md": []byte("# Test edition\n")}, nil
		},
	}
}

func serveEdition(t *testing.T, ed Edition, vars map[string]string) (http.Handler, func(), error) {
	t.Helper()
	getenv := func(k string) string { return vars[k] }
	srv, _, cleanup, err := buildEditionServer(context.Background(), serverSpec{cfg: loadConfig(t, vars), ed: ed, getenv: getenv,
		version: ed.Version, logger: quiet(), out: io.Discard})
	if err != nil {
		return nil, nil, err
	}
	return srv.Handler, cleanup, nil
}

func get(h http.Handler, path string, header ...string) *httptest.ResponseRecorder {
	req := httptest.NewRequest("GET", path, nil)
	for i := 0; i+1 < len(header); i += 2 {
		req.Header.Set(header[i], header[i+1])
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestEditionPlugins(t *testing.T) {
	cleaned := false
	// No COMPLIANCE_TOKENS: the edition's authenticator is enough to start.
	h, cleanup, err := serveEdition(t, testEdition(t, &cleaned), map[string]string{"COMPLIANCE_TEST_GREETING": "bonjour"})
	if err != nil {
		t.Fatal(err)
	}
	about := get(h, "/api/v1/about", "X-Test-User", "ws-1")
	if about.Code != 200 || !strings.Contains(about.Body.String(), `"edition":"test"`) ||
		!strings.Contains(about.Body.String(), `"edition_info":{"setting":"bonjour"}`) ||
		!strings.Contains(about.Body.String(), `{"catalog":"dora","version":"1.9.0"}`) {
		t.Fatalf("about = %d %s", about.Code, about.Body)
	}
	if r := get(h, "/api/v1/test/catalogs", "X-Test-User", "ws-1"); r.Code != 200 || !strings.Contains(r.Body.String(), "1.9.0") {
		t.Fatalf("extra route = %d %s", r.Code, r.Body)
	}
	if r := get(h, "/api/v1/test/catalogs"); r.Code != 401 {
		t.Fatalf("extra route unauthenticated = %d", r.Code)
	}
	profiles := get(h, "/api/v1/report-profiles", "X-Test-User", "ws-1")
	var body struct {
		Profiles []compliance.ProfileInfo `json:"profiles"`
	}
	_ = json.Unmarshal(profiles.Body.Bytes(), &body)
	if ps := body.Profiles; len(ps) != 2 || ps[1].ID != "test_profile" || !ps[1].Entitlement.Allowed {
		t.Fatalf("report profiles = %s", profiles.Body)
	}
	if r := get(h, "/api/v1/admin/ping"); r.Code != 200 || r.Body.String() != "pong" {
		t.Fatalf("handler = %d %s", r.Code, r.Body)
	}
	if r := get(h, "/console/signin"); !strings.Contains(r.Body.String(), "Sign in with the test IdP") {
		t.Fatalf("console sign-in page has no provider:\n%s", r.Body)
	}
	if r := get(h, "/healthz"); r.Code != 200 {
		t.Fatalf("healthz = %d", r.Code)
	}
	cleanup()
	if !cleaned {
		t.Fatal("the edition's cleanup did not run")
	}
}

func TestEditionSetupFailureStopsStart(t *testing.T) {
	cleaned := false
	_, _, err := serveEdition(t, testEdition(t, &cleaned), map[string]string{"COMPLIANCE_TEST_FAIL": "true"})
	if err == nil || !strings.Contains(err.Error(), "test edition: COMPLIANCE_TEST_FAIL is set") {
		t.Fatalf("err = %v", err)
	}
}

func runEdition(ed Edition, vars map[string]string, args ...string) (int, string, string) {
	var out, errOut bytes.Buffer
	code := Run(context.Background(), args, Env{Stdout: &out, Stderr: &errOut, Version: ed.Version, Edition: ed,
		Getenv: func(k string) string { return vars[k] }})
	return code, out.String(), errOut.String()
}

func TestEditionCommands(t *testing.T) {
	cleaned := false
	ed := testEdition(t, &cleaned)
	if code, out, _ := runEdition(ed, map[string]string{"COMPLIANCE_TEST_GREETING": "hej"}, "hello", "world"); code != 0 || out != "hello world hej\n" {
		t.Fatalf("hello = %d %q", code, out)
	}
	if code, out, _ := runEdition(ed, nil, "version"); code != 0 || out != "compliance-engine 9.9.9 (test edition)\n" {
		t.Fatalf("version = %d %q", code, out)
	}
	if code, out, _ := runEdition(ed, nil, "help"); code != 0 || !strings.Contains(out, "Commands of the test edition:") || !strings.Contains(out, "say hello") {
		t.Fatalf("help = %d %q", code, out)
	}
	ed.Commands["serve"] = ed.Commands["hello"]
	if code, _, errOut := runEdition(ed, nil, "hello"); code != 2 || !strings.Contains(errOut, `command "serve" shadows a built-in command`) {
		t.Fatalf("shadowing = %d %q", code, errOut)
	}
}

func TestEditionDocs(t *testing.T) {
	cleaned := false
	ed := testEdition(t, &cleaned)
	dir := t.TempDir()
	if code, _, errOut := runEdition(ed, nil, "docs", "gen", "--out", dir); code != 0 {
		t.Fatalf("docs gen = %d %s", code, errOut)
	}
	if data, err := os.ReadFile(filepath.Join(dir, "test-edition.md")); err != nil || string(data) != "# Test edition\n" {
		t.Fatalf("edition document = %q, %v", data, err)
	}
	if _, err := os.Stat(filepath.Join(dir, "configuration.md")); err != nil {
		t.Fatalf("open-core documents must still be written: %v", err)
	}
	ed.Docs = func(context.Context) (map[string][]byte, error) {
		return map[string][]byte{"configuration.md": []byte("x")}, nil
	}
	if code, _, errOut := runEdition(ed, nil, "docs", "gen", "--out", dir); code != 1 || !strings.Contains(errOut, "shadows an open-core one") {
		t.Fatalf("shadowing document = %d %s", code, errOut)
	}
}

// TestOpenCoreHasNoEnterpriseDeps keeps the edition boundary: no package of
// this module, and so not the open-core binary, depends on the enterprise
// edition.
func TestOpenCoreHasNoEnterpriseDeps(t *testing.T) {
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go tool not on PATH")
	}
	cmd := exec.Command("go", "list", "-deps", "./...")
	cmd.Dir = filepath.Join("..", "..")
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("go list: %v", err)
	}
	pkgs := strings.Fields(string(out))
	if len(pkgs) < 50 || !strings.Contains(string(out), "github.com/nexops-one/compliance-engine/cmd/compliance-engine") {
		t.Fatalf("go list returned too little: %d packages", len(pkgs))
	}
	for _, p := range pkgs {
		if strings.Contains(p, "compliance-engine-enterprise") {
			t.Errorf("open-core package graph contains %s", p)
		}
	}
}
