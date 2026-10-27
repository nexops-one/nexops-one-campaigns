// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"regexp"
	"sort"
	"time"

	"github.com/nexops-one/compliance-engine/pkg/catalog"
	"github.com/nexops-one/compliance-engine/pkg/compliance"
	"github.com/nexops-one/compliance-engine/pkg/config"
	"github.com/nexops-one/compliance-engine/pkg/console"
	"github.com/nexops-one/compliance-engine/pkg/extension"
	"github.com/nexops-one/compliance-engine/pkg/httpapi"
	"github.com/nexops-one/compliance-engine/pkg/identity"
	"github.com/nexops-one/compliance-engine/pkg/store"
)

// Command is an edition's additional command. It writes to env and returns
// the process exit code, like the built-in commands.
type Command func(ctx context.Context, args []string, env Env) int

// Edition describes a binary built on the open core: its name, the commands
// it adds and the plugins it wires into serve and demo. The zero value is the
// open core.
type Edition struct {
	// Name is reported by version and GET /api/v1/about (default "open-core").
	Name string
	// Version is the build version (set with -ldflags by the build).
	Version string
	// Usage lists the additional commands, appended to the usage text.
	Usage string
	// Commands are additional commands; they may not shadow built-in ones.
	Commands map[string]Command
	// Setup reads the edition's own configuration and returns its plugins.
	// It runs once per server, after the store and identity service exist and
	// before the engine is built. An error stops the server from starting.
	Setup func(ctx context.Context, rt Runtime) (Plugins, error)
	// Docs adds generated reference documents (file name to content) to
	// docs gen, for example the edition's configuration reference.
	Docs func(ctx context.Context) (map[string][]byte, error)
}

// OpenCore is the open-core edition.
func OpenCore() Edition { return Edition{Name: "open-core"} }

func (ed Edition) name() string {
	if ed.Name == "" {
		return "open-core"
	}
	return ed.Name
}

// Runtime is what an edition's Setup may use.
type Runtime struct {
	Config   config.Config
	Getenv   func(string) string
	Store    store.Store
	Identity *identity.Service
	Logger   *slog.Logger
	Version  string
	Clock    func() time.Time
}

// Plugins are what an edition adds to a server. Every field is optional.
type Plugins struct {
	// Entitlements replaces extension.AllowOpen.
	Entitlements extension.Entitlements
	// Catalogs supplies catalogs besides the embedded ones and COMPLIANCE_CATALOG_DIR.
	Catalogs catalog.Source
	// Extensions adds report profiles and the workflow policy.
	Extensions compliance.Extensions
	// Authenticators are tried, in order, before the built-in tokens and sessions.
	Authenticators []extension.Authenticator
	// Routes are additional authenticated API routes.
	Routes func(eng *compliance.Engine) []httpapi.ExtraRoute
	// Handlers are mounted on the server mux by pattern ("METHOD /path" or
	// "/prefix/"), in front of the API. They authenticate callers themselves.
	Handlers func(eng *compliance.Engine) map[string]http.Handler
	// About adds sections to GET /api/v1/about.
	About []extension.AboutSection
	// SignIn adds console sign-in providers (for example SSO).
	SignIn []console.SignInProvider
	// Cleanup runs when the server stops.
	Cleanup func()
}

var builtinCommands = map[string]bool{
	"serve": true, "demo": true, "migrate": true, "validate": true, "templates": true, "import": true, "adapter": true, "catalog": true,
	"token": true, "user": true, "audit": true, "evidence": true, "keys": true, "retention": true, "tenant": true, "docs": true,
	"version": true, "help": true, "-h": true, "--help": true,
}

var commandName = regexp.MustCompile(`^[a-z][a-z0-9-]*$`)

// check refuses an edition whose commands shadow built-in ones.
func (ed Edition) check() error {
	names := make([]string, 0, len(ed.Commands))
	for n := range ed.Commands {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		if builtinCommands[n] {
			return fmt.Errorf("edition %s: command %q shadows a built-in command", ed.name(), n)
		}
		if !commandName.MatchString(n) || ed.Commands[n] == nil {
			return fmt.Errorf("edition %s: invalid command %q", ed.name(), n)
		}
	}
	return nil
}

// Main runs the command line of an edition with the process environment and
// returns the exit code. cmd/compliance-engine calls it with OpenCore().
func Main(ctx context.Context, args []string, ed Edition) int {
	return Run(ctx, args, Env{Stdout: os.Stdout, Stderr: os.Stderr, Getenv: os.Getenv, Version: ed.Version, Edition: ed})
}

// NewServer builds the HTTP handler of the edition's server as serve does,
// from env.Getenv, without listening; logs go to env.Stderr. The returned
// function releases the store. Editions use it in tests, for example against
// PostgreSQL.
func NewServer(ctx context.Context, env Env) (http.Handler, func(), error) {
	cfg, err := config.Load(env.Getenv)
	if err != nil {
		return nil, nil, err
	}
	w := env.Stderr
	if w == nil {
		w = io.Discard
	}
	srv, _, cleanup, err := buildEditionServer(ctx, serverSpec{cfg: cfg, ed: env.Edition, getenv: env.Getenv, version: env.Version,
		logger: newLogger(w, cfg.LogLevel), out: w})
	if err != nil {
		return nil, nil, err
	}
	return srv.Handler, cleanup, nil
}
