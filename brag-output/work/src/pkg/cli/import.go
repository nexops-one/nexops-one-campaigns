// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/nexops-one/compliance-engine/pkg/canonical"
	"github.com/nexops-one/compliance-engine/pkg/compliance"
	"github.com/nexops-one/compliance-engine/pkg/config"
	"github.com/nexops-one/compliance-engine/pkg/importer"
	"github.com/nexops-one/compliance-engine/pkg/store"
	"github.com/nexops-one/compliance-engine/pkg/store/memory"
	"github.com/nexops-one/compliance-engine/pkg/store/postgres"
	"github.com/nexops-one/compliance-engine/sdk/adapter"
	"github.com/nexops-one/compliance-engine/sdk/schema"
)

const (
	importSynopsis   = "import [--dry-run] [--json] [--mode incremental|full] [--entity e] [--tenant t --workspace w] <file.csv|file.xlsx>"
	rollbackSynopsis = "import rollback --tenant t --workspace w <ingestion-id>"
)

func runTemplates(args []string, env Env) int {
	fs := flag.NewFlagSet("templates", flag.ContinueOnError)
	fs.SetOutput(env.Stderr)
	out := fs.String("out", ".", "directory to write the templates to")
	if err := fs.Parse(args); err != nil || fs.NArg() != 0 {
		return usageErr(env, "templates [--out dir]")
	}
	reg, err := schema.Default()
	if err != nil {
		return fail(env, "%v", err)
	}
	s := reg.Latest()
	if err := os.MkdirAll(*out, 0o755); err != nil {
		return fail(env, "%v", err)
	}
	write := func(name string, data []byte) error {
		p := filepath.Join(*out, name)
		if err := os.WriteFile(p, data, 0o644); err != nil {
			return err
		}
		fmt.Fprintln(env.Stdout, p)
		return nil
	}
	for _, entity := range s.EntityNames() {
		data, err := importer.CSVTemplate(s, entity)
		if err == nil {
			err = write(entity+".csv", data)
		}
		if err != nil {
			return fail(env, "%v", err)
		}
	}
	data, err := importer.XLSXTemplate(s)
	if err == nil {
		err = write(fmt.Sprintf("compliance-templates-%s.xlsx", s.Version), data)
	}
	if err != nil {
		return fail(env, "%v", err)
	}
	return 0
}

// importEngine opens the engine an import runs against: PostgreSQL when
// COMPLIANCE_DATABASE_URL is set, otherwise an in-memory store (dry runs only).
func importEngine(ctx context.Context, env Env, tenant, workspace string, dryRun bool, synopsis string) (*compliance.Engine, compliance.Scope, func(), int) {
	cfg, err := config.Load(env.Getenv)
	if err != nil {
		return nil, compliance.Scope{}, nil, fail(env, "invalid configuration:\n%v", err)
	}
	scope := compliance.Scope{TenantID: tenant, WorkspaceID: workspace}
	var st store.Store = memory.New()
	var importHasher func(context.Context, adapter.Scope) (func(adapter.Record) string, error)
	cleanup := func() {}
	switch {
	case cfg.DatabaseURL != "":
		if scope.Validate() != nil {
			return nil, scope, nil, usageErr(env, synopsis+"   (--tenant and --workspace are required with COMPLIANCE_DATABASE_URL)")
		}
		pg, err := postgres.Open(ctx, cfg.DatabaseURL)
		if err != nil {
			return nil, scope, nil, fail(env, "%v", err)
		}
		if err := pg.CheckSchema(ctx); err != nil {
			pg.Close()
			return nil, scope, nil, fail(env, "%v", err)
		}
		secured, hasher, _, err := secureStore(cfg, pg, nil)
		if err != nil {
			pg.Close()
			return nil, scope, nil, fail(env, "%v", err)
		}
		st, cleanup, importHasher = secured, pg.Close, hasher
	case !dryRun:
		return nil, scope, nil, fail(env, "COMPLIANCE_DATABASE_URL is required to commit an import; use --dry-run to validate a file locally")
	default:
		if scope.Validate() != nil {
			scope = compliance.Scope{TenantID: "local", WorkspaceID: "import"}
		}
	}
	eng, err := compliance.New(ctx, compliance.Config{
		Store: st, IdentifierPolicy: canonical.IdentifierPolicy(cfg.IdentifierPolicy),
		Limits: compliance.Limits{MaxRecordsPerBatch: cfg.MaxRecordsPerBatch}, Hasher: importHasher,
	})
	if err != nil {
		cleanup()
		return nil, scope, nil, fail(env, "%v", err)
	}
	return eng, scope, cleanup, 0
}

func runImport(ctx context.Context, args []string, env Env) int {
	if len(args) > 0 && args[0] == "rollback" {
		return runImportRollback(ctx, args[1:], env)
	}
	fs := flag.NewFlagSet("import", flag.ContinueOnError)
	fs.SetOutput(env.Stderr)
	dryRun := fs.Bool("dry-run", false, "report what the import would do without committing")
	asJSON := fs.Bool("json", false, "print the full report as JSON")
	mode := fs.String("mode", "incremental", "incremental, or full to replace this importer's previous records")
	entity := fs.String("entity", "", "canonical entity of a CSV file (default: the file name)")
	tenant := fs.String("tenant", "", "tenant ID")
	workspace := fs.String("workspace", "", "workspace ID")
	if err := fs.Parse(args); err != nil || fs.NArg() != 1 {
		return usageErr(env, importSynopsis)
	}
	data, err := os.ReadFile(fs.Arg(0))
	if err != nil {
		return fail(env, "%v", err)
	}
	eng, scope, cleanup, code := importEngine(ctx, env, *tenant, *workspace, *dryRun, importSynopsis)
	if code != 0 {
		return code
	}
	defer cleanup()
	res, err := eng.Import(ctx, scope, importer.Input{Name: filepath.Base(fs.Arg(0)), Data: data, Entity: *entity, Mode: adapter.Mode(*mode)}, *dryRun)
	if err != nil {
		var fe *importer.FileError
		if errors.As(err, &fe) {
			for _, e := range fe.Errors {
				fmt.Fprintf(env.Stderr, "file: %s %s: %s (%s)\n", e.Entity, e.Field, e.Message, e.Code)
			}
			return 1
		}
		return fail(env, "%v", err)
	}
	if *asJSON {
		enc := json.NewEncoder(env.Stdout)
		enc.SetIndent("", "  ")
		_ = enc.Encode(res)
	} else {
		printImport(env.Stdout, res)
	}
	if res.Result.RejectedRecords > 0 {
		return 1
	}
	return 0
}

func location(e schema.FieldError) string {
	if e.SourceRecordRef != "" {
		return e.SourceRecordRef
	}
	return fmt.Sprintf("%s[%d]", e.Entity, e.Index)
}

func printImport(w io.Writer, res compliance.ImportResult) {
	r := res.Result
	fmt.Fprintf(w, "import: %s (%s, adapter %s), %d row(s), mode %s\n", res.File, res.Format, res.Adapter, res.Rows, r.Mode)
	fmt.Fprintf(w, "accepted %d, rejected %d, warnings %d\n", r.Accepted, r.RejectedRecords, len(r.Warnings))
	for _, e := range r.Errors {
		fmt.Fprintf(w, "  error   %s %s: %s (%s)\n", location(e), e.Field, e.Message, e.Code)
	}
	for _, e := range r.Warnings {
		fmt.Fprintf(w, "  warning %s %s: %s (%s)\n", location(e), e.Field, e.Message, e.Code)
	}
	switch {
	case r.DryRun:
		fmt.Fprintf(w, "dry run: would create %d, update %d, delete %d; nothing committed\n", r.Created, r.Updated, r.Deleted)
	case r.NoChanges:
		fmt.Fprintf(w, "no changes (snapshot %s)\n", r.SnapshotID)
	default:
		fmt.Fprintf(w, "committed snapshot %s: created %d, updated %d, deleted %d (ingestion %s)\n", r.SnapshotID, r.Created, r.Updated, r.Deleted, r.IngestionID)
	}
	if res.Completeness != nil {
		missing := 0
		for _, g := range res.Completeness.Gaps {
			if g.State == canonical.StateMissing {
				missing++
			}
		}
		fmt.Fprintf(w, "completeness: %d missing export-required field(s) in the resulting snapshot\n", missing)
	}
}

func runImportRollback(ctx context.Context, args []string, env Env) int {
	fs := flag.NewFlagSet("import rollback", flag.ContinueOnError)
	fs.SetOutput(env.Stderr)
	tenant := fs.String("tenant", "", "tenant ID")
	workspace := fs.String("workspace", "", "workspace ID")
	if err := fs.Parse(args); err != nil || fs.NArg() != 1 {
		return usageErr(env, rollbackSynopsis)
	}
	cfg, err := config.Load(env.Getenv)
	if err != nil {
		return fail(env, "invalid configuration:\n%v", err)
	}
	if cfg.DatabaseURL == "" {
		return fail(env, "COMPLIANCE_DATABASE_URL is required")
	}
	eng, scope, cleanup, code := importEngine(ctx, env, *tenant, *workspace, false, rollbackSynopsis)
	if code != 0 {
		return code
	}
	defer cleanup()
	rb, err := eng.Rollback(ctx, scope, fs.Arg(0))
	if err != nil {
		return fail(env, "%v", err)
	}
	fmt.Fprintf(env.Stdout, "rolled back %s: snapshot %s (created %d, updated %d, deleted %d)\n", rb.RolledBack, rb.SnapshotID, rb.Created, rb.Updated, rb.Deleted)
	return 0
}
