// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"context"
	"flag"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/nexops-one/compliance-engine/pkg/access"
	"github.com/nexops-one/compliance-engine/pkg/auth"
	"github.com/nexops-one/compliance-engine/pkg/identity"
	"github.com/nexops-one/compliance-engine/sdk/adapter"
)

const tokenSynopsis = `token generate --tenant t --workspace w [--role r+r]      static token for COMPLIANCE_TOKENS
       compliance-engine token hash <token>
       compliance-engine token create --tenant t --workspace w --name n --role r+r (--email e | --service) [--expires 90d]
       compliance-engine token list --tenant t --workspace w
       compliance-engine token revoke --tenant t --workspace w <token-id>`

func runToken(ctx context.Context, args []string, env Env) int {
	if len(args) == 0 {
		return usageErr(env, tokenSynopsis)
	}
	switch args[0] {
	case "hash":
		if len(args) != 2 {
			return usageErr(env, "token hash <token>")
		}
		fmt.Fprintln(env.Stdout, auth.HashToken(args[1]))
		return 0
	case "generate":
		return tokenGenerate(args[1:], env)
	case "create", "list", "revoke":
		return tokenStored(ctx, args[0], args[1:], env)
	}
	return usageErr(env, tokenSynopsis)
}

func tokenGenerate(args []string, env Env) int {
	fs := flag.NewFlagSet("token generate", flag.ContinueOnError)
	fs.SetOutput(env.Stderr)
	tenant := fs.String("tenant", "", "tenant ID the token is bound to")
	workspace := fs.String("workspace", "", "workspace ID the token is bound to")
	role := fs.String("role", "", "roles joined by '+' (default owner+admin)")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *tenant == "" || *workspace == "" || strings.ContainsAny(*tenant+*workspace, ":,") || fs.NArg() != 0 {
		return usageErr(env, "token generate --tenant <id> --workspace <id> [--role r+r]   (IDs must not contain ':' or ',')")
	}
	suffix := ""
	if *role != "" {
		roles, err := access.ParseRoles(*role, "+")
		if err != nil {
			return fail(env, "--role: %v", err)
		}
		suffix = ":" + access.Join(roles, "+")
	}
	token, err := auth.GenerateToken()
	if err != nil {
		return fail(env, "%v", err)
	}
	fmt.Fprintf(env.Stdout, "token: %s\nCOMPLIANCE_TOKENS entry: %s:%s:%s%s\n\nGive the token to the API client and keep it secret: it is not shown again.\nConfigure only the COMPLIANCE_TOKENS entry on the server.\n",
		token, auth.HashToken(token), *tenant, *workspace, suffix)
	return 0
}

// parseExpiry accepts "<n>d" (days from now).
func parseExpiry(s string, now time.Time) (*time.Time, error) {
	if s == "" {
		return nil, nil
	}
	days, err := strconv.Atoi(strings.TrimSuffix(s, "d"))
	if !strings.HasSuffix(s, "d") || err != nil || days < 1 {
		return nil, fmt.Errorf("--expires: %q must be a number of days such as 90d", s)
	}
	t := now.Add(time.Duration(days) * 24 * time.Hour)
	return &t, nil
}

func tokenStored(ctx context.Context, cmd string, args []string, env Env) int {
	fs := flag.NewFlagSet("token "+cmd, flag.ContinueOnError)
	fs.SetOutput(env.Stderr)
	tenant := fs.String("tenant", "", "tenant ID")
	workspace := fs.String("workspace", "", "workspace ID")
	name := fs.String("name", "", "token name (create)")
	role := fs.String("role", "", "roles joined by '+' (create)")
	email := fs.String("email", "", "user the token acts as (create)")
	service := fs.Bool("service", false, "create a service token with no user (owner and auditor roles only)")
	expires := fs.String("expires", "", "lifetime in days, for example 90d (create)")
	if err := fs.Parse(args); err != nil || *tenant == "" || *workspace == "" {
		return usageErr(env, tokenSynopsis)
	}
	scope := adapter.Scope{TenantID: *tenant, WorkspaceID: *workspace}
	var req identity.TokenRequest
	switch cmd {
	case "create":
		if fs.NArg() != 0 || *name == "" || *role == "" || (*email == "") == !*service {
			return usageErr(env, "token create --tenant t --workspace w --name n --role r+r (--email e | --service) [--expires 90d]")
		}
		roles, err := access.ParseRoles(*role, "+")
		if err != nil {
			return fail(env, "--role: %v", err)
		}
		exp, err := parseExpiry(*expires, time.Now().UTC())
		if err != nil {
			return fail(env, "%v", err)
		}
		req = identity.TokenRequest{Name: *name, Roles: roles, ExpiresAt: exp, Service: *service}
	case "list":
		if fs.NArg() != 0 {
			return usageErr(env, "token list --tenant t --workspace w")
		}
	case "revoke":
		if fs.NArg() != 1 {
			return usageErr(env, "token revoke --tenant t --workspace w <token-id>")
		}
	}
	ids, pg, err := openIdentity(ctx, env)
	if err != nil {
		return fail(env, "%v", err)
	}
	defer pg.Close()
	switch cmd {
	case "create":
		created, err := ids.IssueToken(ctx, scope, *email, req)
		if err != nil {
			return fail(env, "%v", err)
		}
		fmt.Fprintf(env.Stdout, "token id: %s\nroles: %s\ntoken: %s\n\nGive the token to its holder and keep it secret: it is not shown again.\n",
			created.Token.ID, access.Join(created.Token.Roles, "+"), created.Value)
	case "list":
		tokens, err := ids.AllTokens(ctx, scope)
		if err != nil {
			return fail(env, "%v", err)
		}
		now := time.Now().UTC()
		for _, t := range tokens {
			holder := "service"
			if t.UserID != "" {
				holder = "user " + t.UserID
			}
			state := "active"
			if !t.Active(now) {
				state = "inactive"
			}
			fmt.Fprintf(env.Stdout, "%s\t%s\t%s\t%s\t%s\n", t.ID, t.Name, holder, access.Join(t.Roles, "+"), state)
		}
		if len(tokens) == 0 {
			fmt.Fprintln(env.Stdout, "no stored tokens")
		}
	case "revoke":
		if err := ids.Revoke(ctx, scope, fs.Arg(0)); err != nil {
			return fail(env, "%v", err)
		}
		fmt.Fprintf(env.Stdout, "revoked %s\n", fs.Arg(0))
	}
	return 0
}
