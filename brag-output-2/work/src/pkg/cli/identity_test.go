// SPDX-License-Identifier: Apache-2.0

package cli_test

import (
	"regexp"
	"strings"
	"testing"

	"github.com/nexops-one/compliance-engine/pkg/store/postgres/pgtest"
)

func TestIdentityCommandsRequireDatabase(t *testing.T) {
	for _, args := range [][]string{
		{"user", "create", "--tenant", "acme", "--email", "a@example.com"},
		{"token", "list", "--tenant", "acme", "--workspace", "ws-1"},
		{"audit", "verify", "--tenant", "acme"},
	} {
		if code, _, errOut := run(t, nil, args...); code != 1 || !strings.Contains(errOut, "COMPLIANCE_DATABASE_URL is required") {
			t.Errorf("%v = %d %q", args, code, errOut)
		}
	}
	for _, args := range [][]string{
		{"user", "create", "--tenant", "acme"},
		{"user", "create", "--tenant", "acme", "--email", "a@example.com", "--workspace", "ws-1"},
		{"token", "create", "--tenant", "acme", "--workspace", "ws-1", "--name", "n", "--role", "owner"},
		{"token", "create", "--tenant", "acme", "--workspace", "ws-1", "--name", "n", "--role", "owner", "--service", "--email", "a@example.com"},
		{"audit", "check"},
	} {
		if code, _, _ := run(t, nil, args...); code != 2 {
			t.Errorf("%v must be a usage error, got %d", args, code)
		}
	}
	if code, _, errOut := run(t, nil, "token", "generate", "--tenant", "acme", "--workspace", "ws-1", "--role", "root"); code != 1 || !strings.Contains(errOut, "unknown role") {
		t.Fatalf("bad role = %d %q", code, errOut)
	}
	code, out, _ := run(t, nil, "token", "generate", "--tenant", "acme", "--workspace", "ws-1", "--role", "auditor")
	if code != 0 || !regexp.MustCompile(`COMPLIANCE_TOKENS entry: [0-9a-f]{64}:acme:ws-1:auditor\n`).MatchString(out) {
		t.Fatalf("generate with role = %d %q", code, out)
	}
}

func TestIdentityCommandsAgainstPostgres(t *testing.T) {
	vars := map[string]string{"COMPLIANCE_DATABASE_URL": pgtest.URL(t)}
	code, out, errOut := run(t, vars, "user", "create", "--tenant", "acme", "--email", "Ann@Example.com", "--workspace", "ws-1", "--role", "owner+approver")
	if code != 0 || !strings.Contains(out, "user: ann@example.com") || !strings.Contains(out, "roles in ws-1: owner+approver") {
		t.Fatalf("user create = %d %q %q", code, out, errOut)
	}
	if code, _, errOut := run(t, vars, "user", "create", "--tenant", "acme", "--email", "ann@example.com"); code != 1 || !strings.Contains(errOut, "already exists") {
		t.Fatalf("duplicate user = %d %q", code, errOut)
	}
	if code, out, _ := run(t, vars, "user", "reset-password", "--tenant", "acme", "--email", "ann@example.com"); code != 0 || !strings.Contains(out, "password: ") {
		t.Fatalf("reset = %d %q", code, out)
	}
	if code, out, _ := run(t, vars, "user", "list", "--tenant", "acme"); code != 0 || !strings.Contains(out, "ann@example.com\tactive") {
		t.Fatalf("list = %d %q", code, out)
	}
	if code, _, errOut := run(t, vars, "token", "create", "--tenant", "acme", "--workspace", "ws-1", "--name", "x", "--role", "admin", "--email", "ann@example.com"); code != 1 || !strings.Contains(errOut, "does not hold") {
		t.Fatalf("token beyond roles = %d %q", code, errOut)
	}
	code, out, errOut = run(t, vars, "token", "create", "--tenant", "acme", "--workspace", "ws-1", "--name", "laptop", "--role", "approver", "--email", "ann@example.com", "--expires", "30d")
	id := regexp.MustCompile(`token id: (tok-[0-9a-f]+)`).FindStringSubmatch(out)
	if code != 0 || id == nil || !strings.Contains(out, "token: ce_") {
		t.Fatalf("token create = %d %q %q", code, out, errOut)
	}
	if code, out, _ := run(t, vars, "token", "create", "--tenant", "acme", "--workspace", "ws-1", "--name", "ci", "--role", "owner", "--service"); code != 0 || !strings.Contains(out, "roles: owner") {
		t.Fatalf("service token = %d %q", code, out)
	}
	if code, out, _ := run(t, vars, "token", "list", "--tenant", "acme", "--workspace", "ws-1"); code != 0 || strings.Count(out, "\tactive") != 2 || strings.Contains(out, "ce_") {
		t.Fatalf("token list = %d %q", code, out)
	}
	if code, out, _ := run(t, vars, "token", "revoke", "--tenant", "acme", "--workspace", "ws-1", id[1]); code != 0 || !strings.Contains(out, "revoked") {
		t.Fatalf("revoke = %d %q", code, out)
	}
	if code, out, _ := run(t, vars, "token", "list", "--tenant", "acme", "--workspace", "ws-1"); code != 0 || !strings.Contains(out, "laptop\tuser") || !strings.Contains(out, "\tinactive") {
		t.Fatalf("list after revoke = %d %q", code, out)
	}
	code, out, errOut = run(t, vars, "audit", "verify", "--tenant", "acme")
	if code != 0 || !strings.Contains(out, "(tenant)\tOK\t2 events") || !strings.Contains(out, "ws-1\tOK\t4 events") {
		t.Fatalf("audit verify = %d %q %q", code, out, errOut)
	}
	if code, out, _ := run(t, vars, "audit", "verify", "--tenant", "nobody"); code != 0 || !strings.Contains(out, "no audit events") {
		t.Fatalf("empty tenant = %d %q", code, out)
	}
}
