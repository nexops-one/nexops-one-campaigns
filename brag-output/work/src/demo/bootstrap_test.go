// SPDX-License-Identifier: Apache-2.0

package demo_test

import (
	"bytes"
	"context"
	"io"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"github.com/nexops-one/compliance-engine/demo"
	"github.com/nexops-one/compliance-engine/pkg/access"
	"github.com/nexops-one/compliance-engine/pkg/compliance"
	"github.com/nexops-one/compliance-engine/pkg/extension"
	"github.com/nexops-one/compliance-engine/pkg/identity"
	"github.com/nexops-one/compliance-engine/pkg/store/memory"
	"github.com/nexops-one/compliance-engine/sdk/adapter"
)

func TestBootstrapOnce(t *testing.T) {
	ctx := context.Background()
	st := memory.New()
	eng, err := compliance.New(ctx, compliance.Config{Store: st})
	if err != nil {
		t.Fatal(err)
	}
	ids := identity.New(st, identity.Options{})
	var out bytes.Buffer
	creds, err := demo.Bootstrap(ctx, eng, ids, "http://localhost:8080/console/", &out)
	if err != nil || len(creds) != len(demo.Users) {
		t.Fatalf("bootstrap = %d credentials, %v", len(creds), err)
	}
	for _, c := range creds {
		if _, err := ids.SignIn(ctx, demo.Scope.TenantID, c.Email, c.Password); err != nil {
			t.Errorf("%s cannot sign in: %v", c.Email, err)
		}
		if !strings.Contains(out.String(), c.Password) {
			t.Errorf("the password of %s is not printed", c.Email)
		}
	}
	if set, _ := eng.Settings(ctx, demo.Scope); !set.Sample {
		t.Fatal("the demo workspace is flagged as a sample")
	}
	if snap, _ := eng.Snapshot(ctx, demo.Scope, ""); len(snap.Records) != 0 {
		t.Fatal("the bootstrap never imports the sample register")
	}
	out.Reset()
	again, err := demo.Bootstrap(ctx, eng, ids, "http://localhost:8080/console/", &out)
	if err != nil || again != nil || !strings.Contains(out.String(), "already prepared") || strings.Contains(out.String(), creds[0].Password) {
		t.Fatalf("second bootstrap = %v, %v, %q", again, err, out.String())
	}
}

func TestPrepareConnected(t *testing.T) {
	ctx := context.Background()
	st := memory.New()
	eng, err := compliance.New(ctx, compliance.Config{Store: st})
	if err != nil {
		t.Fatal(err)
	}
	ids := identity.New(st, identity.Options{})
	if _, err := demo.Bootstrap(ctx, eng, ids, "http://localhost:8080/console/", io.Discard); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(t.TempDir(), "nexops.token")
	var out bytes.Buffer
	if err := demo.PrepareConnected(ctx, eng, ids, "nexops", file, &out); err != nil {
		t.Fatal(err)
	}
	connected := adapter.Scope{TenantID: demo.Scope.TenantID, WorkspaceID: "nexops"}
	raw, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	value := strings.TrimSpace(string(raw))
	if strings.Contains(out.String(), value) {
		t.Fatal("the token value is written to the file only, never printed")
	}
	if runtime.GOOS != "windows" {
		if fi, _ := os.Stat(file); fi.Mode().Perm() != 0o600 {
			t.Fatalf("token file mode = %v", fi.Mode().Perm())
		}
	}
	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("Authorization", "Bearer "+value)
	p, err := ids.Authenticate(req)
	if err != nil || p.Scope != connected || !reflect.DeepEqual(p.Roles, []access.Role{access.RoleOwner}) || p.Kind != extension.ActorToken {
		t.Fatalf("token principal = %+v, %v", p, err)
	}
	if set, _ := eng.Settings(ctx, connected); !set.Sample {
		t.Fatal("the connected workspace is flagged as a sample")
	}
	members, _ := ids.MembersOf(ctx, connected)
	if len(members) != len(demo.Users) {
		t.Fatalf("members = %+v", members)
	}
	if !strings.Contains(out.String(), file) || !strings.Contains(out.String(), "nexops") {
		t.Fatalf("output = %q", out.String())
	}

	// Again: the existing token file is kept and no token is created.
	if err := demo.PrepareConnected(ctx, eng, ids, "nexops", file, io.Discard); err != nil {
		t.Fatal(err)
	}
	if again, _ := os.ReadFile(file); string(again) != string(raw) {
		t.Fatal("an existing token file must be kept")
	}
	if toks, _ := st.Tokens(ctx, connected); len(toks) != 1 {
		t.Fatalf("tokens = %d", len(toks))
	}
	// A missing file gets a new token; the workspace is not prepared twice.
	if err := os.Remove(file); err != nil {
		t.Fatal(err)
	}
	if err := demo.PrepareConnected(ctx, eng, ids, "nexops", file, io.Discard); err != nil {
		t.Fatal(err)
	}
	if toks, _ := st.Tokens(ctx, connected); len(toks) != 2 {
		t.Fatalf("tokens after the file was removed = %d", len(toks))
	}
	for _, bad := range []string{"", demo.Scope.WorkspaceID, "has space"} {
		if err := demo.PrepareConnected(ctx, eng, ids, bad, file+"x", io.Discard); err == nil {
			t.Errorf("workspace %q must be refused", bad)
		}
	}
	if err := demo.PrepareConnected(ctx, eng, ids, "other", "", io.Discard); err == nil {
		t.Error("a token file is required")
	}
}
