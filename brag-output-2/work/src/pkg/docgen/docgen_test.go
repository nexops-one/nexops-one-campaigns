// SPDX-License-Identifier: Apache-2.0

package docgen_test

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nexops-one/compliance-engine/pkg/access"
	"github.com/nexops-one/compliance-engine/pkg/config"
	"github.com/nexops-one/compliance-engine/pkg/docgen"
)

func generate(t *testing.T) map[string][]byte {
	t.Helper()
	in, err := docgen.Default(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	docs, err := docgen.Generate(in)
	if err != nil {
		t.Fatal(err)
	}
	return docs
}

// TestGeneratedDocsUpToDate is the drift check: run 'go run ./cmd/compliance-engine docs gen'.
func TestGeneratedDocsUpToDate(t *testing.T) {
	for name, want := range generate(t) {
		got, err := os.ReadFile(filepath.Join("..", "..", "docs", "reference", name))
		if err != nil || !bytes.Equal(got, want) {
			t.Errorf("docs/reference/%s is out of date: run 'go run ./cmd/compliance-engine docs gen'", name)
		}
	}
}

func TestReferenceContent(t *testing.T) {
	docs := generate(t)
	for _, v := range config.Variables() {
		if !bytes.Contains(docs["configuration.md"], []byte("`"+v.Name+"`")) {
			t.Errorf("configuration.md misses %s", v.Name)
		}
	}
	roles := string(docs["roles.md"])
	for _, p := range access.Permissions() {
		line := ""
		for _, l := range strings.Split(roles, "\n") {
			if strings.HasPrefix(l, "| "+string(p)+" |") {
				line = l
			}
		}
		cells := strings.Split(strings.Trim(line, "|"), "|")
		if len(cells) != len(access.AllRoles())+1 {
			t.Fatalf("%s row = %q", p, line)
		}
		for i, r := range access.AllRoles() {
			if got, want := strings.TrimSpace(cells[i+1]) == "✓", access.Allows([]access.Role{r}, p); got != want {
				t.Errorf("%s / %s = %v, want %v", p, r, got, want)
			}
		}
	}
	for _, frag := range []string{"## `ict_provider`", "B_05.01", "| `legal_name` |"} {
		if !bytes.Contains(docs["canonical-fields.md"], []byte(frag)) {
			t.Errorf("canonical-fields.md misses %q", frag)
		}
	}
	for _, frag := range []string{"`dora@1.0.0`", "`dora-exit-plans`", "`gdpr@1.0.0`"} {
		if !bytes.Contains(docs["catalogs.md"], []byte(frag)) {
			t.Errorf("catalogs.md misses %q", frag)
		}
	}
}
