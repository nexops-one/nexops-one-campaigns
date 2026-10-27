// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"context"
	"flag"
	"fmt"
)

func runTenant(ctx context.Context, args []string, env Env) int {
	const synopsis = "tenant delete --tenant t --yes [--override-evidence-retention]"
	if len(args) == 0 || args[0] != "delete" {
		return usageErr(env, synopsis)
	}
	fs := flag.NewFlagSet("tenant delete", flag.ContinueOnError)
	fs.SetOutput(env.Stderr)
	tenant := fs.String("tenant", "", "tenant to delete")
	yes := fs.Bool("yes", false, "confirm: every record, evidence item, assessment, user and token of the tenant is deleted, and its keys are destroyed")
	override := fs.Bool("override-evidence-retention", false, "delete even evidence still within its minimum retention (recorded in the audit log)")
	if err := fs.Parse(args[1:]); err != nil || fs.NArg() != 0 || *tenant == "" {
		return usageErr(env, synopsis)
	}
	if !*yes {
		return fail(env, "tenant delete removes all data of tenant %s and destroys its keys, which also makes its data in existing backups unreadable; re-run with --yes", *tenant)
	}
	eng, pg, closeFn, err := operatorEngineStore(ctx, env)
	if err != nil {
		return fail(env, "%v", err)
	}
	defer closeFn()
	rep, err := eng.DeleteTenant(ctx, *tenant, *override, pg)
	if err != nil {
		return fail(env, "%v", err)
	}
	fmt.Fprintf(env.Stdout, "deleted tenant %s: %d row(s) in %d workspace(s); keys destroyed; %d audit event(s) past the audit floor pruned\n"+
		"Remove the tenant's static tokens from COMPLIANCE_TOKENS and restart running servers, which may cache its keys in memory.\n",
		rep.TenantID, rep.Rows, rep.Workspaces, rep.AuditPruned)
	return 0
}
