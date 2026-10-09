// SPDX-License-Identifier: Apache-2.0

package cli_test

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/nexops-one/compliance-engine/catalogs"
	"github.com/nexops-one/compliance-engine/pkg/cli"
	"github.com/nexops-one/compliance-engine/pkg/store/postgres"
	"github.com/nexops-one/compliance-engine/pkg/store/postgres/pgtest"
)

const samplePath = "../../pkg/compliance/testdata/sample-batch.json"

func run(t *testing.T, vars map[string]string, args ...string) (int, string, string) {
	t.Helper()
	var out, errOut bytes.Buffer
	code := cli.Run(context.Background(), args, cli.Env{
		Stdout: &out, Stderr: &errOut, Version: "1.2.3",
		Getenv: func(k string) string { return vars[k] },
	})
	return code, out.String(), errOut.String()
}

func writeFile(t *testing.T, dir, name, content string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestUsageAndVersion(t *testing.T) {
	if code, _, errOut := run(t, nil); code != 2 || !strings.Contains(errOut, "Usage:") {
		t.Fatalf("no args = %d %q", code, errOut)
	}
	if code, _, _ := run(t, nil, "frobnicate"); code != 2 {
		t.Fatalf("unknown command = %d", code)
	}
	if code, out, _ := run(t, nil, "version"); code != 0 || out != "compliance-engine 1.2.3\n" {
		t.Fatalf("version = %d %q", code, out)
	}
}

func TestToken(t *testing.T) {
	if code, out, _ := run(t, nil, "token", "hash", "secret"); code != 0 || out != "2bb80d537b1da3e38bd30361aa855686bde0eacd7162fef6a25fe97bf527a25b\n" {
		t.Fatalf("hash = %d %q", code, out)
	}
	code, out, _ := run(t, nil, "token", "generate", "--tenant", "acme", "--workspace", "ws-1")
	if code != 0 || !strings.Contains(out, "token: ce_") || !strings.Contains(out, ":acme:ws-1") {
		t.Fatalf("generate = %d %q", code, out)
	}
	if code, _, _ := run(t, nil, "token", "generate", "--tenant", "a:b", "--workspace", "w"); code != 2 {
		t.Fatalf("a tenant containing ':' must be rejected, got %d", code)
	}
}

func TestValidateSample(t *testing.T) {
	code, out, errOut := run(t, nil, "validate", samplePath)
	if code != 0 || !strings.Contains(out, "accepted 9, rejected 0, warnings 4") || !strings.Contains(out, "codelists not loaded") {
		t.Fatalf("validate = %d\n%s\n%s", code, out, errOut)
	}
	code, out, _ = run(t, nil, "validate", "--json", samplePath)
	var dr struct {
		Result struct{ Accepted int } `json:"result"`
	}
	if code != 0 || json.Unmarshal([]byte(out), &dr) != nil || dr.Result.Accepted != 9 {
		t.Fatalf("validate --json = %d %q", code, out)
	}
}

func TestValidateFailures(t *testing.T) {
	dir := t.TempDir()
	bad := writeFile(t, dir, "bad.json", `{"schema_version":"0.1.0","source":{"system":"s","adapter":"a","adapter_version":"1"},"entities":{"ict_provider":[{"provider_id_code":"P1","hq_country":"Ireland"}]}}`)
	if code, out, _ := run(t, nil, "validate", bad); code != 1 || !strings.Contains(out, "error   ict_provider[0] hq_country") {
		t.Fatalf("bad record = %d %q", code, out)
	}
	env := writeFile(t, dir, "env.json", `{"schema_version":"0.1.0","source":{"system":"s","adapter":"a","adapter_version":"1"},"entities":{"nope":[]}}`)
	if code, _, errOut := run(t, nil, "validate", env); code != 1 || !strings.Contains(errOut, "envelope entities.nope") {
		t.Fatalf("bad envelope = %d %q", code, errOut)
	}
	if code, _, _ := run(t, nil, "validate", filepath.Join(dir, "missing.json")); code != 1 {
		t.Fatalf("missing file = %d", code)
	}
	if code, _, _ := run(t, nil, "validate"); code != 2 {
		t.Fatalf("no file = %d", code)
	}
}

func TestCatalogCommands(t *testing.T) {
	if code, out, _ := run(t, nil, "catalog", "validate"); code != 0 || !strings.Contains(out, "ok  dora@1.0.0  DORA, 11 control(s)") {
		t.Fatalf("validate built-in = %d %q", code, out)
	}
	dir := t.TempDir()
	dora, err := catalogs.FS.ReadFile("dora/1.0.0.yaml")
	if err != nil {
		t.Fatal(err)
	}
	next := strings.Replace(string(dora), "version: 1.0.0", "version: 1.1.0", 1)
	next = strings.Replace(next, "title: Reporting entity is identified", "title: Reporting entity is fully identified", 1)
	writeFile(t, dir, "dora/1.1.0.yaml", next)
	code, out, errOut := run(t, nil, "catalog", "diff", "--dir", dir, "dora@1.0.0", "dora@1.1.0")
	if code != 0 || !strings.Contains(out, "~ dora-roi-reporting-entity (title)") {
		t.Fatalf("diff = %d %q %q", code, out, errOut)
	}
	if code, _, _ := run(t, nil, "catalog", "diff", "dora@1.0.0", "dora@9.0.0"); code != 1 {
		t.Fatalf("unknown version = %d", code)
	}
	badDir := t.TempDir()
	writeFile(t, badDir, "x/1.0.0.yaml", "catalog: x\n")
	if code, _, _ := run(t, nil, "catalog", "validate", badDir); code != 1 {
		t.Fatalf("invalid catalog dir = %d", code)
	}
}

func TestMigrateRequiresDatabaseURL(t *testing.T) {
	if code, _, errOut := run(t, nil, "migrate", "status"); code != 1 || !strings.Contains(errOut, "COMPLIANCE_DATABASE_URL is required") {
		t.Fatalf("migrate without URL = %d %q", code, errOut)
	}
}

func TestMigrateAgainstPostgres(t *testing.T) {
	vars := map[string]string{"COMPLIANCE_DATABASE_URL": pgtest.URL(t)}
	ms, _ := postgres.Migrations()
	n := strconv.Itoa(len(ms))
	if code, out, errOut := run(t, vars, "migrate", "status"); code != 0 || !strings.Contains(out, "schema version 0, latest "+n+" (pending migrations)") {
		t.Fatalf("status = %d %q %q", code, out, errOut)
	}
	if code, out, _ := run(t, vars, "migrate", "up"); code != 0 || !strings.Contains(out, "applied migration 0010") {
		t.Fatalf("up = %d %q", code, out)
	}
	if code, out, _ := run(t, vars, "migrate", "up"); code != 0 || !strings.Contains(out, "schema is up to date") {
		t.Fatalf("second up = %d %q", code, out)
	}
	if code, _, errOut := run(t, vars, "migrate", "down"); code != 1 || !strings.Contains(errOut, "--yes") {
		t.Fatalf("down without confirmation must refuse: %d %q", code, errOut)
	}
	if code, out, _ := run(t, vars, "migrate", "status"); code != 0 || !strings.Contains(out, "schema version "+n+", latest "+n) {
		t.Fatalf("an unconfirmed down must change nothing: %d %q", code, out)
	}
	if code, out, _ := run(t, vars, "migrate", "down", "--yes", n); code != 0 || !strings.Contains(out, "reverted migration 0001") {
		t.Fatalf("down --yes %s = %d %q", n, code, out)
	}
	if code, _, _ := run(t, vars, "migrate", "up"); code != 0 {
		t.Fatal("up again")
	}
	if code, out, errOut := run(t, vars, "migrate", "down", "--yes", "--force-drop-audit", n); code != 0 || !strings.Contains(out, "reverted migration 0002") {
		t.Fatalf("down --force-drop-audit = %d %q %q", code, out, errOut)
	}
	if code, _, _ := run(t, vars, "migrate", "down", "--yes", "zero"); code != 2 {
		t.Fatalf("bad step count = %d", code)
	}
}
