// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/nexops-one/compliance-engine/demo"
	"github.com/nexops-one/compliance-engine/pkg/auth"
	"github.com/nexops-one/compliance-engine/pkg/canonical"
	"github.com/nexops-one/compliance-engine/pkg/catalog"
	"github.com/nexops-one/compliance-engine/pkg/compliance"
	"github.com/nexops-one/compliance-engine/pkg/config"
	"github.com/nexops-one/compliance-engine/pkg/console"
	"github.com/nexops-one/compliance-engine/pkg/evidence"
	"github.com/nexops-one/compliance-engine/pkg/extension"
	"github.com/nexops-one/compliance-engine/pkg/httpapi"
	"github.com/nexops-one/compliance-engine/pkg/identity"
	"github.com/nexops-one/compliance-engine/pkg/store"
	"github.com/nexops-one/compliance-engine/pkg/store/memory"
	"github.com/nexops-one/compliance-engine/pkg/store/postgres"
)

func runServe(ctx context.Context, args []string, env Env) int {
	if len(args) != 0 {
		return usageErr(env, "serve   (configure with COMPLIANCE_* environment variables)")
	}
	cfg, err := config.Load(env.Getenv)
	if err != nil {
		return fail(env, "invalid configuration:\n%v", err)
	}
	logger := newLogger(env.Stderr, cfg.LogLevel)
	srv, _, cleanup, err := buildEditionServer(ctx, serverSpec{cfg: cfg, ed: env.Edition, getenv: env.Getenv, version: env.Version, logger: logger, out: env.Stderr})
	if err != nil {
		return fail(env, "%v", err)
	}
	defer cleanup()
	errc := make(chan error, 1)
	go func() {
		logger.Info("listening", "addr", cfg.Listen, "tls", cfg.TLSCertFile != "", "version", env.Version)
		if cfg.TLSCertFile != "" {
			errc <- srv.ListenAndServeTLS(cfg.TLSCertFile, cfg.TLSKeyFile)
		} else {
			errc <- srv.ListenAndServe()
		}
	}()
	select {
	case err := <-errc:
		if errors.Is(err, http.ErrServerClosed) {
			return 0
		}
		return fail(env, "%v", err)
	case <-ctx.Done():
		logger.Info("shutting down")
		sctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		if err := srv.Shutdown(sctx); err != nil {
			return fail(env, "shutdown: %v", err)
		}
		return 0
	}
}

func newLogger(w io.Writer, level string) *slog.Logger {
	var l slog.Level
	_ = l.UnmarshalText([]byte(level))
	return slog.New(slog.NewJSONHandler(w, &slog.HandlerOptions{Level: l}))
}

// buildServer wires the configuration into an HTTP server. The returned
// function releases the store.
func buildServer(ctx context.Context, cfg config.Config, version string, logger *slog.Logger) (*http.Server, func(), error) {
	srv, _, cleanup, err := buildServerWith(ctx, cfg, version, logger, io.Discard)
	return srv, cleanup, err
}

// buildServerWith is buildServer that, in demo mode, prepares the sample
// workspace and prints the demo users' passwords to out.
func buildServerWith(ctx context.Context, cfg config.Config, version string, logger *slog.Logger, out io.Writer) (*http.Server, []demo.Credential, func(), error) {
	return buildEditionServer(ctx, serverSpec{cfg: cfg, version: version, logger: logger, out: out})
}

// serverSpec is what building a server needs.
type serverSpec struct {
	cfg     config.Config
	ed      Edition
	getenv  func(string) string // the edition's configuration source (default: none)
	version string
	logger  *slog.Logger
	out     io.Writer // receives the demo users' passwords
}

// buildEditionServer builds the server of an edition: the open core plus the
// plugins the edition's Setup returns.
func buildEditionServer(ctx context.Context, sp serverSpec) (*http.Server, []demo.Credential, func(), error) {
	cfg, version, logger, out := sp.cfg, sp.version, sp.logger, sp.out
	if sp.getenv == nil {
		sp.getenv = func(string) string { return "" }
	}
	tokens, err := auth.ParseTokens(cfg.Tokens)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("COMPLIANCE_TOKENS: %w", err)
	}
	if cfg.LicenseFile != "" && sp.ed.Setup == nil {
		logger.Info("COMPLIANCE_LICENSE_FILE is ignored by the open-core edition")
	}
	var st store.Store
	cleanup := func() {}
	if cfg.DatabaseURL == "" {
		logger.Warn("COMPLIANCE_DATABASE_URL is empty: using the in-memory store; all data is lost when the process stops")
		st = memory.New()
	} else {
		pg, err := openPostgres(ctx, cfg, logger)
		if err != nil {
			return nil, nil, nil, err
		}
		st, cleanup = pg, pg.Close
	}
	st, hasher, sealedStore, err := secureStore(cfg, st, logger)
	if err != nil {
		cleanup()
		return nil, nil, nil, err
	}
	idle, _ := time.ParseDuration(cfg.SessionIdle) // validated by config.Load
	maxAge, _ := time.ParseDuration(cfg.SessionMax)
	ids := identity.New(st, identity.Options{SessionIdle: idle, SessionMax: maxAge})
	var plug Plugins
	if sp.ed.Setup != nil {
		plug, err = sp.ed.Setup(ctx, Runtime{Config: cfg, Getenv: sp.getenv, Store: st, Identity: ids, Logger: logger, Version: version,
			Clock: func() time.Time { return time.Now().UTC() }})
		if err != nil {
			cleanup()
			return nil, nil, nil, fmt.Errorf("%s edition: %w", sp.ed.name(), err)
		}
		if plug.Cleanup != nil {
			storeCleanup := cleanup
			cleanup = func() { plug.Cleanup(); storeCleanup() }
		}
	}
	if len(tokens) == 0 && !cfg.Demo && len(plug.Authenticators) == 0 {
		stored, err := st.HasActiveTokens(ctx, time.Now().UTC())
		if err != nil {
			cleanup()
			return nil, nil, nil, err
		}
		if !stored {
			cleanup()
			return nil, nil, nil, errors.New("COMPLIANCE_TOKENS is empty and the database holds no active API token: create a static token with " +
				"'compliance-engine token generate --tenant <id> --workspace <id>', or a stored one with 'compliance-engine user create' " +
				"and 'compliance-engine token create' (PostgreSQL only)")
		}
	}
	sources := catalog.Sources{catalog.Embedded()}
	if cfg.CatalogDir != "" {
		sources = append(sources, catalog.FSSource{FS: os.DirFS(cfg.CatalogDir), Origin: "dir"})
	}
	if plug.Catalogs != nil {
		sources = append(sources, plug.Catalogs)
	}
	eng, err := compliance.New(ctx, compliance.Config{
		Store: st, Catalogs: sources, Version: version,
		IdentifierPolicy: canonical.IdentifierPolicy(cfg.IdentifierPolicy),
		Limits:           compliance.Limits{MaxRecordsPerBatch: cfg.MaxRecordsPerBatch},
		Hasher:           hasher,
		Retention:        compliance.RetentionOptions{AuditDays: cfg.AuditRetentionDays},
		Entitlements:     plug.Entitlements,
		Extensions:       plug.Extensions,
		Workflow: compliance.WorkflowOptions{AllowSelfApproval: cfg.AllowSelfApproval, AllowEvidenceWithoutChecksum: cfg.EvidenceNoChecksum,
			Verifier: evidence.Verifier{Root: cfg.EvidenceRoot, AllowHosts: splitList(cfg.EvidenceFetchAllow)}, Managed: managedStorage(cfg, sealedStore)},
	})
	if err != nil {
		cleanup()
		return nil, nil, nil, err
	}
	ids.SetWorkspaceGate(eng.WorkspaceWritable)
	failures := httpapi.NewFailureLimiter(cfg.AuthFailureLimit)
	var extra []httpapi.ExtraRoute
	if plug.Routes != nil {
		extra = plug.Routes(eng)
	}
	authn := append(append([]extension.Authenticator{}, plug.Authenticators...), auth.NewStaticTokens(tokens), ids)
	api := httpapi.New(eng, httpapi.Options{
		Authenticator: auth.Chain(authn...), Identity: ids, MaxBodyBytes: int64(cfg.MaxBodyBytes),
		Version: version, Logger: logger, Edition: sp.ed.name(), ExtraRoutes: extra, About: plug.About,
		EncryptionAtRest: cfg.EncryptionKeyFile != "",
		RateLimit:        httpapi.RateLimit{PerMinute: cfg.RateLimit, Burst: cfg.RateBurst, AuthFailures: cfg.AuthFailureLimit},
		Failures:         failures,
	})
	handler := api
	if cfg.Console {
		handler = withConsole(api, console.New(eng, ids, console.Options{
			Tenant: cfg.ConsoleTenant, Version: version, Logger: logger, Failures: failures, MaxUploadBytes: int64(cfg.MaxBodyBytes),
			SampleRegister: demo.Register(), SignIn: plug.SignIn,
		}))
	}
	if plug.Handlers != nil {
		if hs := plug.Handlers(eng); len(hs) > 0 {
			mux := http.NewServeMux()
			for pattern, h := range hs {
				mux.Handle(pattern, h)
			}
			mux.Handle("/", handler)
			handler = mux
		}
	}
	var creds []demo.Credential
	if cfg.Demo {
		logger.Warn("COMPLIANCE_DEMO is on: the demo tenant holds fictitious sample data; never enable demo mode on a deployment with real data")
		if creds, err = demo.Bootstrap(ctx, eng, ids, consoleURL(cfg), out); err != nil {
			cleanup()
			return nil, nil, nil, fmt.Errorf("demo bootstrap: %w", err)
		}
		if cfg.DemoConnectedWorkspace != "" {
			if err := demo.PrepareConnected(ctx, eng, ids, cfg.DemoConnectedWorkspace, cfg.DemoTokenFile, out); err != nil {
				cleanup()
				return nil, nil, nil, fmt.Errorf("connected demo workspace: %w", err)
			}
		}
	}
	srv := &http.Server{
		Addr: cfg.Listen, Handler: handler,
		ReadHeaderTimeout: 10 * time.Second, ReadTimeout: 2 * time.Minute, WriteTimeout: 2 * time.Minute, IdleTimeout: 2 * time.Minute,
		ErrorLog: slog.NewLogLogger(logger.Handler(), slog.LevelWarn),
	}
	startSessionPurge(ctx, ids, logger)
	if cfg.RetentionInterval != "0" {
		interval, _ := time.ParseDuration(cfg.RetentionInterval) // validated by config.Load
		startRetentionJob(ctx, eng, interval, logger)
	}
	return srv, creds, cleanup, nil
}

// openPostgres connects and brings the schema up to date (COMPLIANCE_AUTO_MIGRATE)
// or checks that it is (otherwise).
func openPostgres(ctx context.Context, cfg config.Config, logger *slog.Logger) (*postgres.Store, error) {
	pg, err := postgres.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		return nil, err
	}
	if cfg.AutoMigrate {
		applied, err := pg.MigrateUp(ctx)
		if err != nil {
			pg.Close()
			return nil, err
		}
		if len(applied) > 0 && logger != nil {
			logger.Info("applied database migrations", "versions", applied)
		}
	} else if err := pg.CheckSchema(ctx); err != nil {
		pg.Close()
		return nil, err
	}
	return pg, nil
}

// splitList splits a comma-separated configuration value, dropping empty items.
func splitList(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// startSessionPurge deletes expired console sessions every hour.
func startSessionPurge(ctx context.Context, ids *identity.Service, logger *slog.Logger) {
	go func() {
		t := time.NewTicker(time.Hour)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				if n, err := ids.PurgeSessions(ctx); err != nil {
					logger.Error("session purge failed", "error", err)
				} else if n > 0 {
					logger.Info("expired sessions deleted", "sessions", n)
				}
			}
		}
	}()
}

// withConsole serves the console under /console and redirects / to it; every
// other path goes to the API.
func withConsole(api http.Handler, c http.Handler) http.Handler {
	mux := http.NewServeMux()
	mux.Handle(console.Prefix+"/", c)
	mux.Handle("GET /{$}", http.RedirectHandler(console.Prefix+"/", http.StatusSeeOther))
	mux.Handle("GET "+console.Prefix, http.RedirectHandler(console.Prefix+"/", http.StatusSeeOther))
	mux.Handle("/", api)
	return mux
}

// consoleURL is where the console of cfg is reached from the local machine.
func consoleURL(cfg config.Config) string {
	host, port, err := net.SplitHostPort(cfg.Listen)
	if err != nil {
		return console.Prefix + "/"
	}
	if host == "" || host == "0.0.0.0" || host == "::" {
		host = "localhost"
	}
	scheme := "http"
	if cfg.TLSCertFile != "" {
		scheme = "https"
	}
	return scheme + "://" + net.JoinHostPort(host, port) + console.Prefix + "/"
}
