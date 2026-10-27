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

const providersCSV = "provider_id_code,legal_name,hq_country\nP1,One Ltd,IE\nP2,Two Inc,US\n"

func TestTemplatesCommand(t *testing.T) {
	dir := t.TempDir()
	code, out, errOut := run(t, nil, "templates", "--out", dir)
	if code != 0 {
		t.Fatalf("templates = %d %q", code, errOut)
	}
	files, _ := filepath.Glob(filepath.Join(dir, "*"))
	if len(files) != 15 || !strings.Contains(out, "compliance-templates-0.1.0.xlsx") {
		t.Fatalf("files = %v", files)
	}
	head, _ := os.ReadFile(filepath.Join(dir, "ict_provider.csv"))
	if !strings.HasPrefix(string(head), "provider_id_code,") {
		t.Fatalf("ict_provider.csv = %q", head)
	}
}

func TestImportDryRunLocally(t *testing.T) {
	file := writeFile(t, t.TempDir(), "ict_provider.csv", providersCSV)
	code, out, errOut := run(t, nil, "import", "--dry-run", file)
	if code != 0 || !strings.Contains(out, "accepted 2, rejected 0") || !strings.Contains(out, "dry run: would create 2") {
		t.Fatalf("dry run = %d\n%s\n%s", code, out, errOut)
	}
	if code, _, errOut := run(t, nil, "import", file); code != 1 || !strings.Contains(errOut, "COMPLIANCE_DATABASE_URL is required") {
		t.Fatalf("a commit without a database must be refused: %d %q", code, errOut)
	}
}

func TestImportReportsRejectedRowsAndBadFiles(t *testing.T) {
	dir := t.TempDir()
	bad := writeFile(t, dir, "ict_provider.csv", "provider_id_code,hq_country\nP1,IE\nP2,Ireland\n")
	code, out, _ := run(t, nil, "import", "--dry-run", bad)
	if code != 1 || !strings.Contains(out, "ict_provider:3 hq_country") {
		t.Fatalf("rejected row = %d %q", code, out)
	}
	unknown := writeFile(t, dir, "other/ict_provider.csv", "provider_id_code,colour\nP1,red\n")
	if code, _, errOut := run(t, nil, "import", "--dry-run", unknown); code != 1 || !strings.Contains(errOut, "unknown_column") {
		t.Fatalf("bad file = %d %q", code, errOut)
	}
}

func TestImportAgainstPostgres(t *testing.T) {
	vars := map[string]string{"COMPLIANCE_DATABASE_URL": pgtest.URL(t)}
	if code, _, errOut := run(t, vars, "migrate", "up"); code != 0 {
		t.Fatalf("migrate = %q", errOut)
	}
	file := writeFile(t, t.TempDir(), "ict_provider.csv", providersCSV)
	if code, _, _ := run(t, vars, "import", file); code != 2 {
		t.Fatalf("--tenant and --workspace are required with a database, got %d", code)
	}
	scope := []string{"--tenant", "acme", "--workspace", "ws-1"}
	code, out, errOut := run(t, vars, append(append([]string{"import"}, scope...), file)...)
	if code != 0 || !strings.Contains(out, "committed snapshot rev-1") {
		t.Fatalf("import = %d\n%s\n%s", code, out, errOut)
	}
	id := regexp.MustCompile(`ingestion (ing-[0-9a-f]+)`).FindStringSubmatch(out)
	if id == nil {
		t.Fatalf("the ingestion ID must be printed: %s", out)
	}
	if code, out, _ := run(t, vars, append(append([]string{"import"}, scope...), file)...); code != 0 || !strings.Contains(out, "no changes (snapshot rev-1)") {
		t.Fatalf("re-import = %d %s", code, out)
	}
	code, out, errOut = run(t, vars, append(append([]string{"import", "rollback"}, scope...), id[1])...)
	if code != 0 || !strings.Contains(out, "snapshot rev-2") {
		t.Fatalf("rollback = %d %s %s", code, out, errOut)
	}
}
