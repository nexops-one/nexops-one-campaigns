// SPDX-License-Identifier: Apache-2.0

package demo

import (
	"context"
	"fmt"
	"io"

	"github.com/nexops-one/compliance-engine/pkg/access"
	"github.com/nexops-one/compliance-engine/pkg/compliance"
	"github.com/nexops-one/compliance-engine/pkg/extension"
	"github.com/nexops-one/compliance-engine/pkg/identity"
	"github.com/nexops-one/compliance-engine/sdk/adapter"
)

// Scope is the demo's sample workspace.
var Scope = adapter.Scope{TenantID: "demo", WorkspaceID: "sample"}

// Actor is recorded for what the bootstrap does.
const Actor = "system:demo-bootstrap"

// Credential is a demo user and the password generated for it.
type Credential struct {
	Email    string
	Roles    []access.Role
	Password string
}

// Users are the demo accounts and their roles. The approver also reviews;
// the admin manages members, tokens and settings.
var Users = []struct {
	Email string
	Roles []access.Role
}{
	{"owner@demo.invalid", []access.Role{access.RoleOwner}},
	{"approver@demo.invalid", []access.Role{access.RoleReviewer, access.RoleApprover}},
	{"auditor@demo.invalid", []access.Role{access.RoleAuditor}},
	{"admin@demo.invalid", []access.Role{access.RoleAdmin}},
}

// Bootstrap prepares the sample workspace: it flags it as a sample, creates
// the demo users with generated passwords and prints them once to out, with
// the console URL. It never imports the sample register: the presenter does.
// When the workspace already has members it changes nothing and returns no
// credentials.
func Bootstrap(ctx context.Context, eng *compliance.Engine, ids *identity.Service, consoleURL string, out io.Writer) ([]Credential, error) {
	ctx = extension.WithPrincipal(ctx, extension.Principal{Scope: Scope, Actor: Actor, Kind: extension.ActorSystem})
	members, err := ids.MembersOf(ctx, Scope)
	if err != nil {
		return nil, err
	}
	if len(members) > 0 {
		fmt.Fprintf(out, "\nDemo workspace %s/%s is already prepared. Console: %s\n"+
			"Forgot a password? compliance-engine user reset-password --tenant %s --email owner@demo.invalid\n\n",
			Scope.TenantID, Scope.WorkspaceID, consoleURL, Scope.TenantID)
		return nil, nil
	}
	yes := true
	if _, err := eng.UpdateSettings(ctx, Scope, compliance.SettingsInput{Sample: &yes}); err != nil {
		return nil, err
	}
	var creds []Credential
	for _, u := range Users {
		if _, err := ids.AddMember(ctx, Scope, u.Email, u.Roles); err != nil {
			return nil, err
		}
		pw, err := ids.ResetPassword(ctx, Scope.TenantID, u.Email)
		if err != nil {
			return nil, err
		}
		creds = append(creds, Credential{Email: u.Email, Roles: u.Roles, Password: pw})
	}
	fmt.Fprintf(out, "\n================ compliance-engine demo ================\n"+
		"SAMPLE DATA: fictitious organization. Results are a demonstration, not a compliance status.\n\n"+
		"Console: %s (tenant %s, workspace %s)\n\n", consoleURL, Scope.TenantID, Scope.WorkspaceID)
	for _, c := range creds {
		fmt.Fprintf(out, "  %-24s %-20s password: %s\n", c.Email, access.Join(c.Roles, "+"), c.Password)
	}
	fmt.Fprintf(out, "\nThe passwords are shown once. Step 1: import the sample register from the console's Import page.\n"+
		"=========================================================\n\n")
	return creds, nil
}
