// SPDX-License-Identifier: Apache-2.0

package cli_test

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/nexops-one/compliance-engine/pkg/store/postgres/pgtest"
)

func TestKeysCommands(t *testing.T) {
	dir := t.TempDir()
	k1, k2 := filepath.Join(dir, "k1"), filepath.Join(dir, "k2")
	code, out, errOut := run(t, nil, "keys", "generate", "--out", k1)
	if code != 0 || !regexp.MustCompile(`id [0-9a-f]{16}`).MatchString(out) {
		t.Fatalf("generate = %d %q %q", code, out, errOut)
	}
	if code, _, errOut := run(t, nil, "keys", "generate", "--out", k1); code != 1 || !strings.Contains(errOut, "must not exist") {
		t.Fatalf("overwriting a key file must be refused: %d %q", code, errOut)
	}
	if code, _, _ := run(t, nil, "keys", "generate", "--out", k2); code != 0 {
		t.Fatal("second key")
	}
	if code, _, errOut := run(t, nil, "keys", "rotate", "--from", k1, "--to", k2); code != 1 || !strings.Contains(errOut, "COMPLIANCE_DATABASE_URL is required") {
		t.Fatalf("rotate without a database = %d %q", code, errOut)
	}

	vars := map[string]string{"COMPLIANCE_DATABASE_URL": pgtest.URL(t), "COMPLIANCE_ENCRYPTION_KEY_FILE": k1}
	sample, err := os.ReadFile("../../pkg/compliance/testdata/sample-batch.json")
	if err != nil || len(sample) == 0 {
		t.Fatal(err)
	}
	if code, out, errOut := run(t, vars, "keys", "rotate", "--data", "--tenant", "acme"); code != 0 || !strings.Contains(out, "data key version 2") {
		t.Fatalf("rotate data = %d %q %q", code, out, errOut)
	}
	if code, out, errOut := run(t, vars, "keys", "seal-existing", "--tenant", "acme"); code != 0 || !strings.Contains(out, "sealed 0 record version(s)") {
		t.Fatalf("seal existing = %d %q %q", code, out, errOut)
	}
	if code, out, errOut := run(t, vars, "keys", "rotate", "--from", k1, "--to", k2); code != 0 || !strings.Contains(out, "re-wrapped the keys of 1 tenant(s)") {
		t.Fatalf("rotate kek = %d %q %q", code, out, errOut)
	}
	if code, _, errOut := run(t, vars, "keys", "rotate", "--data", "--tenant", "acme"); code != 1 || !strings.Contains(errOut, "different key-encryption key") {
		t.Fatalf("the old KEK must stop working after rotation: %d %q", code, errOut)
	}
	vars["COMPLIANCE_ENCRYPTION_KEY_FILE"] = k2
	if code, out, errOut := run(t, vars, "keys", "rotate", "--data", "--tenant", "acme"); code != 0 || !strings.Contains(out, "version 3") {
		t.Fatalf("rotate data under the new KEK = %d %q %q", code, out, errOut)
	}
}

func TestRetentionCommand(t *testing.T) {
	if code, _, errOut := run(t, nil, "retention", "run"); code != 1 || !strings.Contains(errOut, "COMPLIANCE_DATABASE_URL is required") {
		t.Fatalf("without a database = %d %q", code, errOut)
	}
	if code, _, _ := run(t, nil, "retention", "run", "--tenant", "acme"); code != 2 {
		t.Fatal("--tenant needs --workspace")
	}
	vars := map[string]string{"COMPLIANCE_DATABASE_URL": pgtest.URL(t)}
	if code, _, errOut := run(t, vars, "user", "create", "--tenant", "acme", "--email", "a@example.com", "--workspace", "ws-1", "--role", "admin"); code != 0 {
		t.Fatalf("setup = %q", errOut)
	}
	code, out, errOut := run(t, vars, "retention", "run", "--dry-run", "--tenant", "acme", "--workspace", "ws-1")
	if code != 0 || !strings.Contains(out, "acme/ws-1\twould delete 0 revision(s)") {
		t.Fatalf("dry run = %d %q %q", code, out, errOut)
	}
	if code, out, errOut := run(t, vars, "retention", "run"); code != 0 {
		t.Fatalf("all workspaces = %d %q %q", code, out, errOut)
	}
	if code, _, errOut := run(t, map[string]string{"COMPLIANCE_RETENTION_INTERVAL": "5s"}, "retention", "run"); code != 1 || !strings.Contains(errOut, "COMPLIANCE_RETENTION_INTERVAL") {
		t.Fatalf("bad interval = %d %q", code, errOut)
	}
}

func TestTenantDeleteCommand(t *testing.T) {
	dir := t.TempDir()
	key := filepath.Join(dir, "kek")
	if code, _, _ := run(t, nil, "keys", "generate", "--out", key); code != 0 {
		t.Fatal("keys generate")
	}
	vars := map[string]string{"COMPLIANCE_DATABASE_URL": pgtest.URL(t), "COMPLIANCE_ENCRYPTION_KEY_FILE": key}
	if code, _, errOut := run(t, vars, "user", "create", "--tenant", "gone", "--email", "a@example.com", "--workspace", "ws-1", "--role", "admin"); code != 0 {
		t.Fatalf("setup = %q", errOut)
	}
	if code, _, _ := run(t, vars, "keys", "rotate", "--data", "--tenant", "gone"); code != 0 {
		t.Fatal("create keys")
	}
	if code, _, errOut := run(t, vars, "tenant", "delete", "--tenant", "gone"); code != 1 || !strings.Contains(errOut, "--yes") {
		t.Fatalf("without --yes = %d %q", code, errOut)
	}
	code, out, errOut := run(t, vars, "tenant", "delete", "--tenant", "gone", "--yes")
	if code != 0 || !strings.Contains(out, "keys destroyed") {
		t.Fatalf("delete = %d %q %q", code, out, errOut)
	}
	if code, out, _ := run(t, vars, "user", "list", "--tenant", "gone"); code != 0 || !strings.Contains(out, "no users") {
		t.Fatalf("users after deletion = %q", out)
	}
	if code, out, _ := run(t, vars, "audit", "verify", "--tenant", "gone"); code != 0 || !strings.Contains(out, "(tenant)\tOK") {
		t.Fatalf("the audit chain survives and verifies: %q", out)
	}
	if code, _, errOut := run(t, map[string]string{"COMPLIANCE_EVIDENCE_STORAGE_DIR": dir}, "retention", "run"); code != 1 || !strings.Contains(errOut, "requires COMPLIANCE_ENCRYPTION_KEY_FILE") {
		t.Fatalf("storage without a KEK = %d %q", code, errOut)
	}
}
