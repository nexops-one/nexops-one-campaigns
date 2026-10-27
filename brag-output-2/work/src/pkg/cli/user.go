// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"strings"

	"github.com/nexops-one/compliance-engine/pkg/access"
	"github.com/nexops-one/compliance-engine/pkg/config"
	"github.com/nexops-one/compliance-engine/pkg/identity"
	"github.com/nexops-one/compliance-engine/pkg/store/postgres"
	"github.com/nexops-one/compliance-engine/sdk/adapter"
)

// openIdentity opens the configured PostgreSQL store for operator commands.
// Users and stored tokens only make sense in a persistent store.
func openIdentity(ctx context.Context, env Env) (*identity.Service, *postgres.Store, error) {
	cfg, err := config.Load(env.Getenv)
	if err != nil {
		return nil, nil, fmt.Errorf("invalid configuration:\n%v", err)
	}
	if cfg.DatabaseURL == "" {
		return nil, nil, errors.New("COMPLIANCE_DATABASE_URL is required: users, stored tokens and the audit log live in PostgreSQL " +
			"(the in-memory store loses them when the process stops)")
	}
	pg, err := openPostgres(ctx, cfg, nil)
	if err != nil {
		return nil, nil, err
	}
	return identity.New(pg, identity.Options{}), pg, nil
}

func runUser(ctx context.Context, args []string, env Env) int {
	const synopsis = "user create --tenant t --email e [--workspace w --role r+r] | user reset-password --tenant t --email e | user list --tenant t"
	if len(args) == 0 {
		return usageErr(env, synopsis)
	}
	fs := flag.NewFlagSet("user "+args[0], flag.ContinueOnError)
	fs.SetOutput(env.Stderr)
	tenant := fs.String("tenant", "", "tenant ID")
	email := fs.String("email", "", "user email address")
	workspace := fs.String("workspace", "", "workspace to grant roles in (create only)")
	role := fs.String("role", "", "roles joined by '+', for example owner+approver (create only)")
	if err := fs.Parse(args[1:]); err != nil || fs.NArg() != 0 || *tenant == "" {
		return usageErr(env, synopsis)
	}
	switch args[0] {
	case "create":
		if *email == "" || (*workspace == "") != (*role == "") {
			return usageErr(env, synopsis+"   (--workspace and --role go together)")
		}
		var roles []access.Role
		if *role != "" {
			var err error
			if roles, err = access.ParseRoles(*role, "+"); err != nil {
				return fail(env, "--role: %v", err)
			}
		}
		ids, pg, err := openIdentity(ctx, env)
		if err != nil {
			return fail(env, "%v", err)
		}
		defer pg.Close()
		u, pw, err := ids.CreateUser(ctx, *tenant, *email)
		if err != nil {
			return fail(env, "%v", err)
		}
		fmt.Fprintf(env.Stdout, "user: %s (%s)\npassword: %s\n", u.Email, u.ID, pw)
		if *workspace != "" {
			m, err := ids.AddMember(ctx, adapter.Scope{TenantID: *tenant, WorkspaceID: *workspace}, u.Email, roles)
			if err != nil {
				return fail(env, "user created, but granting roles failed: %v", err)
			}
			fmt.Fprintf(env.Stdout, "roles in %s: %s\n", *workspace, access.Join(m.Roles, "+"))
		}
		fmt.Fprintln(env.Stdout, "\nGive the password to the user over a secure channel: it is not shown again.")
		return 0
	case "reset-password":
		if *email == "" || *workspace != "" || *role != "" {
			return usageErr(env, synopsis)
		}
		ids, pg, err := openIdentity(ctx, env)
		if err != nil {
			return fail(env, "%v", err)
		}
		defer pg.Close()
		pw, err := ids.ResetPassword(ctx, *tenant, *email)
		if err != nil {
			return fail(env, "%v", err)
		}
		fmt.Fprintf(env.Stdout, "password: %s\n\nThe previous password no longer works. This one is not shown again.\n", pw)
		return 0
	case "list":
		if *email != "" || *workspace != "" || *role != "" {
			return usageErr(env, synopsis)
		}
		ids, pg, err := openIdentity(ctx, env)
		if err != nil {
			return fail(env, "%v", err)
		}
		defer pg.Close()
		users, err := ids.Users(ctx, *tenant)
		if err != nil {
			return fail(env, "%v", err)
		}
		for _, u := range users {
			state := "active"
			if u.Disabled {
				state = "disabled"
			} else if u.PasswordHash == "" {
				state = "no password"
			}
			fmt.Fprintf(env.Stdout, "%s\t%s\t%s\n", u.ID, u.Email, state)
		}
		if len(users) == 0 {
			fmt.Fprintln(env.Stdout, "no users")
		}
		return 0
	}
	return usageErr(env, strings.TrimSpace(synopsis))
}
