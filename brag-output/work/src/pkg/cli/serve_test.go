// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nexops-one/compliance-engine/catalogs"
	"github.com/nexops-one/compliance-engine/pkg/access"
	"github.com/nexops-one/compliance-engine/pkg/auth"
	"github.com/nexops-one/compliance-engine/pkg/config"
	"github.com/nexops-one/compliance-engine/pkg/identity"
	"github.com/nexops-one/compliance-engine/pkg/store/postgres"
	"github.com/nexops-one/compliance-engine/pkg/store/postgres/pgtest"
	"github.com/nexops-one/compliance-engine/sdk/adapter"
)

var tokenEntry = auth.HashToken("t0k") + ":acme:ws-1"

func quiet() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

func loadConfig(t *testing.T, vars map[string]string) config.Config {
	t.Helper()
	cfg, err := config.Load(func(k string) string { return vars[k] })
	if err != nil {
		t.Fatal(err)
	}
	return cfg
}

func TestBuildServerRequiresTokens(t *testing.T) {
	_, _, err := buildServer(context.Background(), loadConfig(t, nil), "test", quiet())
	if err == nil || !strings.Contains(err.Error(), "COMPLIANCE_TOKENS is empty") {
		t.Fatalf("err = %v", err)
	}
	_, _, err = buildServer(context.Background(), loadConfig(t, map[string]string{"COMPLIANCE_TOKENS": "nope"}), "test", quiet())
	if err == nil || !strings.Contains(err.Error(), "COMPLIANCE_TOKENS") {
		t.Fatalf("malformed tokens err = %v", err)
	}
}

func TestBuildServerInMemory(t *testing.T) {
	srv, cleanup, err := buildServer(context.Background(), loadConfig(t, map[string]string{
		"COMPLIANCE_TOKENS": tokenEntry, "COMPLIANCE_LICENSE_FILE": "license.json",
	}), "test", quiet())
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	if srv.Addr != ":8080" || srv.ReadHeaderTimeout == 0 {
		t.Fatalf("server = %+v", srv)
	}
	rec := httptest.NewRecorder()
	srv.Handler.ServeHTTP(rec, httptest.NewRequest("GET", "/healthz", nil))
	if rec.Code != 200 {
		t.Fatalf("healthz = %d", rec.Code)
	}
	req := httptest.NewRequest("GET", "/api/v1/about", nil)
	req.Header.Set("Authorization", "Bearer t0k")
	rec = httptest.NewRecorder()
	srv.Handler.ServeHTTP(rec, req)
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), `"workspace_id":"ws-1"`) || !strings.Contains(rec.Body.String(), `"version":"test"`) {
		t.Fatalf("about = %d %s", rec.Code, rec.Body)
	}
}

func TestBuildServerLoadsCatalogDir(t *testing.T) {
	dir := t.TempDir()
	dora, err := catalogs.FS.ReadFile("dora/1.0.0.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "dora"), 0o755); err != nil {
		t.Fatal(err)
	}
	next := strings.Replace(string(dora), "version: 1.0.0", "version: 1.1.0", 1)
	if err := os.WriteFile(filepath.Join(dir, "dora", "1.1.0.yaml"), []byte(next), 0o644); err != nil {
		t.Fatal(err)
	}
	srv, cleanup, err := buildServer(context.Background(), loadConfig(t, map[string]string{
		"COMPLIANCE_TOKENS": tokenEntry, "COMPLIANCE_CATALOG_DIR": dir,
	}), "test", quiet())
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	req := httptest.NewRequest("GET", "/api/v1/about", nil)
	req.Header.Set("Authorization", "Bearer t0k")
	rec := httptest.NewRecorder()
	srv.Handler.ServeHTTP(rec, req)
	if !strings.Contains(rec.Body.String(), `{"catalog":"dora","version":"1.1.0"}`) {
		t.Fatalf("about = %s", rec.Body)
	}
}

func TestBuildServerRefusesPendingMigrations(t *testing.T) {
	url := pgtest.URL(t)
	_, _, err := buildServer(context.Background(), loadConfig(t, map[string]string{
		"COMPLIANCE_TOKENS": tokenEntry, "COMPLIANCE_DATABASE_URL": url, "COMPLIANCE_AUTO_MIGRATE": "false",
	}), "test", quiet())
	if !errors.Is(err, postgres.ErrPendingMigrations) {
		t.Fatalf("err = %v", err)
	}
}

func TestBuildServerAutoMigrates(t *testing.T) {
	url := pgtest.URL(t)
	_, cleanup, err := buildServer(context.Background(), loadConfig(t, map[string]string{
		"COMPLIANCE_TOKENS": tokenEntry, "COMPLIANCE_DATABASE_URL": url,
	}), "test", quiet())
	if err != nil {
		t.Fatal(err)
	}
	cleanup()
	st, err := postgres.Open(context.Background(), url)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if cur, lat, err := st.MigrationStatus(context.Background()); err != nil || cur != lat || cur == 0 {
		t.Fatalf("status = %d/%d, %v", cur, lat, err)
	}
}

func TestServeShutsDownOnCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	var stderr bytes.Buffer
	done := make(chan int, 1)
	vars := map[string]string{"COMPLIANCE_TOKENS": tokenEntry, "COMPLIANCE_LISTEN": "127.0.0.1:0"}
	go func() {
		done <- Run(ctx, []string{"serve"}, Env{Stdout: io.Discard, Stderr: &stderr, Getenv: func(k string) string { return vars[k] }, Version: "t"})
	}()
	time.Sleep(300 * time.Millisecond)
	cancel()
	select {
	case code := <-done:
		if code != 0 {
			t.Fatalf("serve exited %d: %s", code, stderr.String())
		}
	case <-time.After(20 * time.Second):
		t.Fatal("serve did not stop after cancellation")
	}
	if !strings.Contains(stderr.String(), "in-memory store") {
		t.Fatalf("the in-memory warning must be logged: %s", stderr.String())
	}
}

func TestServeStartsWithStoredTokenOnly(t *testing.T) {
	url := pgtest.URL(t)
	vars := map[string]string{"COMPLIANCE_DATABASE_URL": url}
	if _, _, err := buildServer(context.Background(), loadConfig(t, vars), "test", quiet()); err == nil || !strings.Contains(err.Error(), "no active API token") {
		t.Fatalf("no tokens at all = %v", err)
	}
	pg, err := postgres.Open(context.Background(), url)
	if err != nil {
		t.Fatal(err)
	}
	defer pg.Close()
	ids := identity.New(pg, identity.Options{})
	created, err := ids.IssueToken(context.Background(), adapter.Scope{TenantID: "acme", WorkspaceID: "ws-1"}, "",
		identity.TokenRequest{Name: "svc", Roles: []access.Role{access.RoleAuditor}, Service: true})
	if err != nil {
		t.Fatal(err)
	}
	srv, cleanup, err := buildServer(context.Background(), loadConfig(t, vars), "test", quiet())
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	req := httptest.NewRequest("GET", "/api/v1/me", nil)
	req.Header.Set("Authorization", "Bearer "+created.Value)
	rec := httptest.NewRecorder()
	srv.Handler.ServeHTTP(rec, req)
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), `"roles":["auditor"]`) {
		t.Fatalf("me with a stored token = %d %s", rec.Code, rec.Body)
	}
}

func TestServeWithEncryption(t *testing.T) {
	keyFile := filepath.Join(t.TempDir(), "kek")
	if code := Run(context.Background(), []string{"keys", "generate", "--out", keyFile}, Env{Stdout: io.Discard, Stderr: io.Discard, Getenv: func(string) string { return "" }}); code != 0 {
		t.Fatal("keys generate")
	}
	var logs bytes.Buffer
	srv, cleanup, err := buildServer(context.Background(), loadConfig(t, map[string]string{
		"COMPLIANCE_TOKENS": tokenEntry, "COMPLIANCE_ENCRYPTION_KEY_FILE": keyFile,
	}), "test", slog.New(slog.NewJSONHandler(&logs, nil)))
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	req := httptest.NewRequest("GET", "/api/v1/about", nil)
	req.Header.Set("Authorization", "Bearer t0k")
	rec := httptest.NewRecorder()
	srv.Handler.ServeHTTP(rec, req)
	if !strings.Contains(rec.Body.String(), `"encryption_at_rest":true`) || !strings.Contains(logs.String(), "encryption at rest enabled") {
		t.Fatalf("about = %s logs = %s", rec.Body, logs.String())
	}
	logs.Reset()
	plain, cleanup2, err := buildServer(context.Background(), loadConfig(t, map[string]string{"COMPLIANCE_TOKENS": tokenEntry}), "test", slog.New(slog.NewJSONHandler(&logs, nil)))
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup2()
	rec = httptest.NewRecorder()
	plain.Handler.ServeHTTP(rec, req)
	if !strings.Contains(rec.Body.String(), `"encryption_at_rest":false`) || !strings.Contains(logs.String(), "stored unencrypted") {
		t.Fatalf("without a KEK: about = %s logs = %s", rec.Body, logs.String())
	}
}

func TestServeMountsConsole(t *testing.T) {
	for _, on := range []bool{true, false} {
		vars := map[string]string{"COMPLIANCE_TOKENS": tokenEntry}
		if !on {
			vars["COMPLIANCE_CONSOLE"] = "false"
		}
		srv, cleanup, err := buildServer(context.Background(), loadConfig(t, vars), "test", quiet())
		if err != nil {
			t.Fatal(err)
		}
		get := func(path string) *httptest.ResponseRecorder {
			rec := httptest.NewRecorder()
			req := httptest.NewRequest("GET", path, nil)
			req.Header.Set("Authorization", "Bearer t0k")
			srv.Handler.ServeHTTP(rec, req)
			return rec
		}
		if got := get("/api/v1/about").Code; got != 200 {
			t.Fatalf("console %v: API = %d", on, got)
		}
		signin, root := get("/console/signin"), get("/")
		if on && (signin.Code != 200 || !strings.Contains(signin.Body.String(), "Sign in") || root.Code != 303 || root.Header().Get("Location") != "/console/") {
			t.Fatalf("console on: signin %d, / %d %q", signin.Code, root.Code, root.Header().Get("Location"))
		}
		if !on && (signin.Code != 404 || root.Code != 404) {
			t.Fatalf("console off: signin %d, / %d", signin.Code, root.Code)
		}
		cleanup()
	}
}

func TestConsoleURL(t *testing.T) {
	for listen, want := range map[string]string{
		":8080":          "http://localhost:8080/console/",
		"127.0.0.1:9000": "http://127.0.0.1:9000/console/",
		"0.0.0.0:80":     "http://localhost:80/console/",
		"[::]:8443":      "http://localhost:8443/console/",
	} {
		if got := consoleURL(config.Config{Listen: listen}); got != want {
			t.Errorf("%s: %s, want %s", listen, got, want)
		}
	}
	if got := consoleURL(config.Config{Listen: ":443", TLSCertFile: "c"}); got != "https://localhost:443/console/" {
		t.Errorf("tls: %s", got)
	}
}

func TestDemoNeedsNoToken(t *testing.T) {
	cfg := loadConfig(t, map[string]string{"COMPLIANCE_DEMO": "true"})
	var out bytes.Buffer
	srv, creds, cleanup, err := buildServerWith(context.Background(), cfg, "test", quiet(), &out)
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	if srv == nil || len(creds) != 4 || !strings.Contains(out.String(), "owner@demo.invalid") || !strings.Contains(out.String(), creds[0].Password) {
		t.Fatalf("demo bootstrap: %d credentials, banner %q", len(creds), out.String())
	}
}

func TestDemoConnectedWorkspace(t *testing.T) {
	file := filepath.Join(t.TempDir(), "nexops.token")
	cfg := loadConfig(t, map[string]string{"COMPLIANCE_DEMO": "true", "COMPLIANCE_DEMO_CONNECTED_WORKSPACE": "nexops", "COMPLIANCE_DEMO_TOKEN_FILE": file})
	var out bytes.Buffer
	srv, _, cleanup, err := buildServerWith(context.Background(), cfg, "test", quiet(), &out)
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	raw, err := os.ReadFile(file)
	if err != nil || !strings.Contains(out.String(), "nexops") {
		t.Fatalf("token file: %v; output %q", err, out.String())
	}
	req := httptest.NewRequest("GET", "/api/v1/settings", nil)
	req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(string(raw)))
	rec := httptest.NewRecorder()
	srv.Handler.ServeHTTP(rec, req)
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), `"sample":true`) {
		t.Fatalf("the token reads its sample workspace: %d %s", rec.Code, rec.Body)
	}
}
