// SPDX-License-Identifier: Apache-2.0

// Package cli implements the compliance-engine command line. Every command
// writes to Env and returns an exit code, so commands are unit-testable.
package cli

import (
	"context"
	"fmt"
	"io"
)

// Env is the process environment a command runs in.
type Env struct {
	Stdout  io.Writer
	Stderr  io.Writer
	Getenv  func(string) string
	Version string
	// Edition is the edition running the command; the zero value is the open core.
	Edition Edition
}

const usage = `compliance-engine: data-source-agnostic compliance engine (open core)

Usage:
  compliance-engine serve                                    run the HTTP API and the console (configured by COMPLIANCE_* variables)
  compliance-engine demo [--listen addr] [--evidence-dir d]  run the demo in memory: sample workspace, demo users, console
  compliance-engine migrate up | down --yes [--force-drop-audit] [n] | status
                                                             manage the PostgreSQL schema (COMPLIANCE_DATABASE_URL)
  compliance-engine validate [--json] <batch.json>           validate a canonical batch file offline
  compliance-engine templates [--out dir]                    write the CSV and XLSX import templates
  compliance-engine import [--dry-run] [--mode m] [--entity e] [--tenant t --workspace w] <file>
                                                             import a .csv or .xlsx file (report, then commit)
  compliance-engine import rollback --tenant t --workspace w <ingestion-id>
                                                             restore the snapshot preceding an import
  compliance-engine adapter test --manifest m.json --batches dir [--repeat dir2] [--derived e.f,...] [--junit f]
                                                             check an adapter's batch files against the contract
  compliance-engine adapter test --listen addr [--derived e.f,...] [--junit f]
                                                             local conformance endpoint for sdk/push or any HTTP client
  compliance-engine catalog validate [dir]                   validate the built-in catalogs, or those in dir
  compliance-engine catalog diff [--dir d] [--json] <a> <b>  compare two catalog versions (catalog@version)
  compliance-engine token generate --tenant t --workspace w [--role r+r]
                                                             create a static token and its COMPLIANCE_TOKENS entry
  compliance-engine token hash <token>                       print the hash of an existing token
  compliance-engine token create --tenant t --workspace w --name n --role r+r (--email e | --service) [--expires 90d]
                                                             create a stored token (PostgreSQL), shown once
  compliance-engine token list | revoke --tenant t --workspace w [<id>]
                                                             list or revoke stored tokens
  compliance-engine user create --tenant t --email e [--workspace w --role r+r]
                                                             create a user with a generated password
  compliance-engine user reset-password | list --tenant t [--email e]
                                                             reset a password or list users
  compliance-engine audit verify --tenant t                  verify the audit log hash chains
  compliance-engine evidence verify --url U --token T --file PATH <evidence-id>
                                                             hash a local evidence file and record the check
  compliance-engine keys generate --out FILE                 create a key-encryption key file (encryption at rest)
  compliance-engine keys rotate --from OLD --to NEW | --data --tenant t
                                                             re-wrap tenant keys under a new KEK, or rotate a data key
  compliance-engine keys seal-existing --tenant t            encrypt data stored before encryption was enabled
  compliance-engine retention run [--dry-run] [--tenant t --workspace w]
                                                             apply the workspace retention policies
  compliance-engine tenant delete --tenant t --yes [--override-evidence-retention]
                                                             delete a tenant and destroy its keys (crypto-shredding)
  compliance-engine docs gen [--out dir] [--check]           write (or check) the generated reference documents
  compliance-engine version                                  print the version

Outputs are operational readiness aids, not legal advice and not certification.
`

// Run executes the command line and returns the process exit code.
func Run(ctx context.Context, args []string, env Env) int {
	if err := env.Edition.check(); err != nil {
		fmt.Fprintln(env.Stderr, "error:", err)
		return 2
	}
	if len(args) == 0 {
		fmt.Fprint(env.Stderr, env.usage())
		return 2
	}
	if cmd, ok := env.Edition.Commands[args[0]]; ok {
		return cmd(ctx, args[1:], env)
	}
	switch args[0] {
	case "serve":
		return runServe(ctx, args[1:], env)
	case "demo":
		return runDemo(ctx, args[1:], env)
	case "migrate":
		return runMigrate(ctx, args[1:], env)
	case "validate":
		return runValidate(ctx, args[1:], env)
	case "templates":
		return runTemplates(args[1:], env)
	case "import":
		return runImport(ctx, args[1:], env)
	case "adapter":
		return runAdapter(ctx, args[1:], env)
	case "catalog":
		return runCatalog(ctx, args[1:], env)
	case "token":
		return runToken(ctx, args[1:], env)
	case "user":
		return runUser(ctx, args[1:], env)
	case "audit":
		return runAudit(ctx, args[1:], env)
	case "evidence":
		return runEvidence(ctx, args[1:], env)
	case "keys":
		return runKeys(ctx, args[1:], env)
	case "retention":
		return runRetention(ctx, args[1:], env)
	case "tenant":
		return runTenant(ctx, args[1:], env)
	case "docs":
		return runDocs(ctx, args[1:], env)
	case "version":
		if n := env.Edition.name(); n != "open-core" {
			fmt.Fprintf(env.Stdout, "compliance-engine %s (%s edition)\n", env.Version, n)
		} else {
			fmt.Fprintln(env.Stdout, "compliance-engine", env.Version)
		}
		return 0
	case "help", "-h", "--help":
		fmt.Fprint(env.Stdout, env.usage())
		return 0
	}
	fmt.Fprintf(env.Stderr, "unknown command %q\n\n%s", args[0], env.usage())
	return 2
}

// usage is the usage text, with the edition's commands.
func (env Env) usage() string {
	if env.Edition.Usage == "" {
		return usage
	}
	return usage + "\nCommands of the " + env.Edition.name() + " edition:\n" + env.Edition.Usage
}

func fail(env Env, format string, a ...any) int {
	fmt.Fprintf(env.Stderr, "error: "+format+"\n", a...)
	return 1
}

func usageErr(env Env, synopsis string) int {
	fmt.Fprintf(env.Stderr, "usage: compliance-engine %s\n", synopsis)
	return 2
}
