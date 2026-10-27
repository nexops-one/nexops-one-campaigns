# Writing an edition

An edition is a binary built on the open core that adds commands and
features without forking it. The commercial edition is one; a partner or a
host product can write its own the same way. The open core never imports an
edition.

## The entry point

```go
package main

import (
	"context"
	"os"

	"github.com/nexops-one/compliance-engine/pkg/cli"
)

var version = "dev"

func main() {
	os.Exit(cli.Main(context.Background(), os.Args[1:], cli.Edition{
		Name:     "acme",      // reported by version and GET /api/v1/about
		Version:  version,
		Usage:    "  acme-engine hello                         an extra command\n",
		Commands: map[string]cli.Command{"hello": hello},
		Setup:    setup,       // the edition's plugins, built once per server
		Docs:     docs,        // extra generated reference documents
	}))
}
```

Every open command (`serve`, `demo`, `migrate`, `import`, `docs gen`, ...)
works unchanged. An edition's command may not shadow a built-in one.

## Plugins

`Setup(ctx, cli.Runtime) (cli.Plugins, error)` runs when `serve` or `demo`
builds a server, after the store and the identity service exist and before
the engine is built. `Runtime` holds the loaded open configuration, `Getenv`
for the edition's own `COMPLIANCE_*` variables, the store, the identity
service, the logger and the clock. An error stops the server from starting:
use it for invalid configuration, never for a missing license (reads must
never depend on a license).

| Plugin | Purpose |
|---|---|
| `Entitlements` | replaces `extension.AllowOpen`; implement `extension.WorkspaceLimiter` too to bound the active workspaces |
| `Catalogs` | an extra `catalog.Source`; set `Catalog.Feature` to gate catalogs, `Origin` to say where they come from |
| `Extensions.ReportProfiles` | report profiles (`extension.ReportProfile`; `extension.ParameterizedProfile` for parameters) |
| `Extensions.WorkflowPolicy` | what approvals need (`extension.WorkflowPolicy`: quorum, recommendation, submitter rules) |
| `Authenticators` | tried before the engine's tokens and sessions |
| `Routes` | extra authenticated API routes (`httpapi.ExtraRoute`): authentication, rate limits and the route's permission are applied as for built-in routes; check entitlements in the handler |
| `Handlers` | handlers mounted on the server in front of the API (they authenticate callers themselves) |
| `About` | sections of `GET /api/v1/about` (`extension.AboutSection`) |
| `SignIn` | console sign-in methods (`console.SignInProvider`: a start and a callback route; open the session with `identity.Service.SignInExternal`) |
| `Cleanup` | runs when the server stops |

Helpers for handlers: `httpapi.NewProblem`, `httpapi.WriteJSON`,
`httpapi.DecodeJSON`, `httpapi.WriteError` answer in the API's shapes.
Workspace settings extensions (`Engine.SettingsExtension`,
`Engine.PutSettingsExtension`) keep an edition's per-workspace settings,
audited as `settings.update`.

## Configuration and documentation

Declare the edition's variables in a struct with `env`, `default` and `doc`
tags, read it with `config.LoadInto(rt.Getenv, &cfg)`, and render its
reference with `docgen.ConfigurationTable` from `Edition.Docs`: `docs gen`
then writes it next to the open references.

## Tests

`cli.DemoServerWith(ctx, edition, evidenceDir, getenv)` builds the in-memory
demo server of an edition; `cli.NewServer(ctx, env)` builds a server from a
configuration, for example against PostgreSQL. `cli.Run` runs commands with a
test environment.
