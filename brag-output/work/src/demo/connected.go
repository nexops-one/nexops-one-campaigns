// SPDX-License-Identifier: Apache-2.0

package demo

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"regexp"

	"github.com/nexops-one/compliance-engine/pkg/access"
	"github.com/nexops-one/compliance-engine/pkg/compliance"
	"github.com/nexops-one/compliance-engine/pkg/extension"
	"github.com/nexops-one/compliance-engine/pkg/identity"
	"github.com/nexops-one/compliance-engine/sdk/adapter"
)

var workspaceName = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,62}$`)

// PrepareConnected prepares a second sample workspace of the demo tenant for
// a connected host product (spec v2 §9 step 6): it flags the workspace as a
// sample, gives the demo users the same roles there, and writes the value of
// a new service token (role owner: push register data, evaluate, read) to
// tokenFile with mode 0600. The value is never printed. The workspace is
// prepared once; a token is created only when tokenFile does not exist.
func PrepareConnected(ctx context.Context, eng *compliance.Engine, ids *identity.Service, workspace, tokenFile string, out io.Writer) error {
	if !workspaceName.MatchString(workspace) || workspace == Scope.WorkspaceID {
		return fmt.Errorf("connected demo workspace %q: use lowercase letters, digits and hyphens, other than %q", workspace, Scope.WorkspaceID)
	}
	if tokenFile == "" {
		return errors.New("connected demo workspace: a token file is required")
	}
	scope := adapter.Scope{TenantID: Scope.TenantID, WorkspaceID: workspace}
	ctx = extension.WithPrincipal(ctx, extension.Principal{Scope: scope, Actor: Actor, Kind: extension.ActorSystem})
	members, err := ids.MembersOf(ctx, scope)
	if err != nil {
		return err
	}
	if len(members) == 0 {
		yes := true
		if _, err := eng.UpdateSettings(ctx, scope, compliance.SettingsInput{Sample: &yes}); err != nil {
			return err
		}
		for _, u := range Users {
			if _, err := ids.AddMember(ctx, scope, u.Email, u.Roles); err != nil {
				return err
			}
		}
	}
	if _, err := os.Stat(tokenFile); err == nil {
		fmt.Fprintf(out, "Connected demo workspace %s/%s: keeping the token in %s.\n", scope.TenantID, workspace, tokenFile)
		return nil
	} else if !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	tok, err := ids.IssueToken(ctx, scope, "", identity.TokenRequest{Name: "connected demo (" + workspace + ")", Roles: []access.Role{access.RoleOwner}, Service: true})
	if err != nil {
		return err
	}
	f, err := os.OpenFile(tokenFile, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return fmt.Errorf("write the connected demo token: %w", err)
	}
	if _, err := fmt.Fprintln(f, tok.Value); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	fmt.Fprintf(out, "Connected demo workspace %s/%s: a service token (role owner, %s) was written to %s.\n",
		scope.TenantID, workspace, tok.Token.ID, tokenFile)
	return nil
}
