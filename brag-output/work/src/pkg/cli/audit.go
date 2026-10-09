// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"context"
	"flag"
	"fmt"

	"github.com/nexops-one/compliance-engine/pkg/compliance"
)

func runAudit(ctx context.Context, args []string, env Env) int {
	const synopsis = "audit verify --tenant t"
	if len(args) == 0 || args[0] != "verify" {
		return usageErr(env, synopsis)
	}
	fs := flag.NewFlagSet("audit verify", flag.ContinueOnError)
	fs.SetOutput(env.Stderr)
	tenant := fs.String("tenant", "", "tenant whose audit chains to verify")
	if err := fs.Parse(args[1:]); err != nil || fs.NArg() != 0 || *tenant == "" {
		return usageErr(env, synopsis)
	}
	_, pg, err := openIdentity(ctx, env)
	if err != nil {
		return fail(env, "%v", err)
	}
	defer pg.Close()
	scopes, err := pg.AuditScopes(ctx, *tenant)
	if err != nil {
		return fail(env, "%v", err)
	}
	broken := 0
	for _, sc := range scopes {
		r, err := compliance.VerifyChain(ctx, pg, sc)
		if err != nil {
			return fail(env, "%v", err)
		}
		name := sc.WorkspaceID
		if name == "" {
			name = "(tenant)"
		}
		if r.OK {
			fmt.Fprintf(env.Stdout, "%s\tOK\t%d events\n", name, r.Checked)
		} else {
			broken++
			fmt.Fprintf(env.Stdout, "%s\tBROKEN\tat seq %d: %s (%d events verified before it)\n", name, r.BrokenAt, r.Reason, r.Checked)
		}
	}
	if len(scopes) == 0 {
		fmt.Fprintln(env.Stdout, "no audit events for this tenant")
	}
	if broken > 0 {
		return fail(env, "%d audit chain(s) failed verification", broken)
	}
	return 0
}
