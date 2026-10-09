// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"context"
	"flag"
	"fmt"
	"strconv"

	"github.com/nexops-one/compliance-engine/pkg/config"
	"github.com/nexops-one/compliance-engine/pkg/store/postgres"
)

func runMigrate(ctx context.Context, args []string, env Env) int {
	const synopsis = "migrate up | down --yes [--force-drop-audit] [n] | status"
	if len(args) == 0 {
		return usageErr(env, synopsis)
	}
	steps := 1
	var downOpts []postgres.DownOption
	switch args[0] {
	case "up", "status":
		if len(args) != 1 {
			return usageErr(env, synopsis)
		}
	case "down":
		fs := flag.NewFlagSet("migrate down", flag.ContinueOnError)
		fs.SetOutput(env.Stderr)
		yes := fs.Bool("yes", false, "confirm that reverted migrations drop their tables and data")
		forceAudit := fs.Bool("force-drop-audit", false, "allow reverting the audit-log migration even though it holds audit events")
		if err := fs.Parse(args[1:]); err != nil || fs.NArg() > 1 {
			return usageErr(env, synopsis)
		}
		if fs.NArg() == 1 {
			n, err := strconv.Atoi(fs.Arg(0))
			if err != nil || n < 1 {
				return usageErr(env, synopsis)
			}
			steps = n
		}
		if *forceAudit {
			downOpts = append(downOpts, postgres.ForceDropAudit())
		}
		if !*yes {
			return fail(env, "migrate down drops the tables of each reverted migration and deletes their data; back up the database first (pg_dump), then re-run with --yes")
		}
	default:
		return usageErr(env, synopsis)
	}
	cfg, err := config.Load(env.Getenv)
	if err != nil {
		return fail(env, "invalid configuration:\n%v", err)
	}
	if cfg.DatabaseURL == "" {
		return fail(env, "COMPLIANCE_DATABASE_URL is required")
	}
	st, err := postgres.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		return fail(env, "%v", err)
	}
	defer st.Close()
	switch args[0] {
	case "up":
		applied, err := st.MigrateUp(ctx)
		for _, v := range applied {
			fmt.Fprintf(env.Stdout, "applied migration %04d\n", v)
		}
		if err != nil {
			return fail(env, "%v", err)
		}
		if len(applied) == 0 {
			fmt.Fprintln(env.Stdout, "schema is up to date")
		}
	case "down":
		reverted, err := st.MigrateDown(ctx, steps, downOpts...)
		for _, v := range reverted {
			fmt.Fprintf(env.Stdout, "reverted migration %04d\n", v)
		}
		if err != nil {
			return fail(env, "%v", err)
		}
		if len(reverted) == 0 {
			fmt.Fprintln(env.Stdout, "nothing to revert")
		}
	case "status":
		cur, lat, err := st.MigrationStatus(ctx)
		if err != nil {
			return fail(env, "%v", err)
		}
		suffix := ""
		if cur < lat {
			suffix = " (pending migrations)"
		}
		fmt.Fprintf(env.Stdout, "schema version %d, latest %d%s\n", cur, lat, suffix)
	}
	return 0
}
