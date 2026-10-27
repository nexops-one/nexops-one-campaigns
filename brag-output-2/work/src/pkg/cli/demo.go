// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"

	"github.com/nexops-one/compliance-engine/demo"
	"github.com/nexops-one/compliance-engine/pkg/config"
)

// demoVars are the settings of the single-command demo, applied over the
// environment; the store is always the in-memory one.
func demoVars(listen, evidenceDir string) map[string]string {
	return map[string]string{
		"COMPLIANCE_DEMO":               "true",
		"COMPLIANCE_DATABASE_URL":       "",
		"COMPLIANCE_CONSOLE":            "true",
		"COMPLIANCE_CONSOLE_TENANT":     demo.Scope.TenantID,
		"COMPLIANCE_EVIDENCE_ROOT":      evidenceDir,
		"COMPLIANCE_LISTEN":             listen,
		"COMPLIANCE_RETENTION_INTERVAL": "0",
	}
}

func overlay(vars map[string]string, getenv func(string) string) func(string) string {
	return func(k string) string {
		if v, ok := vars[k]; ok {
			return v
		}
		return getenv(k)
	}
}

func runDemo(ctx context.Context, args []string, env Env) int {
	fs := flag.NewFlagSet("demo", flag.ContinueOnError)
	fs.SetOutput(env.Stderr)
	listen := fs.String("listen", "127.0.0.1:8080", "address to listen on")
	dir := fs.String("evidence-dir", "", "directory to write the sample evidence to (default: a new temporary directory)")
	connected := fs.String("connected-workspace", "", "also prepare this sample workspace for a connected host product (demo step 6)")
	tokenFile := fs.String("token-file", "", "file receiving the connected workspace's service token (with --connected-workspace)")
	if err := fs.Parse(args); err != nil || fs.NArg() != 0 || (*connected == "") != (*tokenFile == "") {
		return usageErr(env, "demo [--listen addr] [--evidence-dir dir] [--connected-workspace name --token-file path]")
	}
	if *dir == "" {
		tmp, err := os.MkdirTemp("", "compliance-demo-evidence-")
		if err != nil {
			return fail(env, "%v", err)
		}
		*dir = tmp
	}
	abs, err := filepath.Abs(*dir)
	if err != nil {
		return fail(env, "%v", err)
	}
	if err := demo.WriteEvidence(abs); err != nil {
		return fail(env, "write the sample evidence: %v", err)
	}
	fmt.Fprintf(env.Stdout, "Sample evidence written to %s: attach it as %s with the checksum from SHA256SUMS.\n"+
		"All data lives in memory and is lost when the demo stops.\n",
		abs, fileURI(filepath.Join(abs, "ict-third-party-risk-policy.md")))
	vars := demoVars(*listen, abs)
	vars["COMPLIANCE_DEMO_CONNECTED_WORKSPACE"], vars["COMPLIANCE_DEMO_TOKEN_FILE"] = *connected, *tokenFile
	return runServe(ctx, nil, Env{Stdout: env.Stdout, Stderr: env.Stderr, Version: env.Version, Edition: env.Edition,
		Getenv: overlay(vars, env.Getenv)})
}

// fileURI renders an absolute path as a file URI (file:///C:/x on Windows).
func fileURI(p string) string {
	s := filepath.ToSlash(p)
	if len(s) > 0 && s[0] != '/' {
		s = "/" + s
	}
	return "file://" + s
}

// DemoServer builds the in-memory demo server with its sample evidence in
// evidenceDir, as compliance-engine demo does, and returns its handler and
// the demo users' credentials. It is used by the demo walkthrough test.
func DemoServer(ctx context.Context, evidenceDir string, version string) (http.Handler, []demo.Credential, func(), error) {
	return DemoServerWith(ctx, Edition{Version: version}, evidenceDir, nil)
}

// DemoServerWith is DemoServer for an edition, whose own configuration is
// read from getenv (nil: none). Tests of editions use it.
func DemoServerWith(ctx context.Context, ed Edition, evidenceDir string, getenv func(string) string) (http.Handler, []demo.Credential, func(), error) {
	if err := demo.WriteEvidence(evidenceDir); err != nil {
		return nil, nil, nil, err
	}
	if getenv == nil {
		getenv = func(string) string { return "" }
	}
	vars := overlay(demoVars("127.0.0.1:0", evidenceDir), getenv)
	cfg, err := config.Load(vars)
	if err != nil {
		return nil, nil, nil, err
	}
	srv, creds, cleanup, err := buildEditionServer(ctx, serverSpec{cfg: cfg, ed: ed, getenv: vars, version: ed.Version,
		logger: slog.New(slog.NewTextHandler(io.Discard, nil)), out: io.Discard})
	if err != nil {
		return nil, nil, nil, err
	}
	return srv.Handler, creds, cleanup, nil
}
