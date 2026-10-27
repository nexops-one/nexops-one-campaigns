// SPDX-License-Identifier: Apache-2.0

package deploy_test

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

const vendorDD = "../docs/vendor-dd"

// TestVendorPackMarked: every document of the vendor due-diligence pack says
// it is pending legal review in its first lines.
func TestVendorPackMarked(t *testing.T) {
	n := 0
	err := filepath.WalkDir(vendorDD, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(p, ".md") {
			return err
		}
		n++
		f, err := os.Open(p)
		if err != nil {
			return err
		}
		defer f.Close()
		sc := bufio.NewScanner(f)
		marked := false
		for i := 0; i < 3 && sc.Scan(); i++ {
			marked = marked || sc.Text() == "Status: pending legal review"
		}
		if !marked {
			t.Errorf("%s does not start with 'Status: pending legal review'", p)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if n < 8 {
		t.Fatalf("only %d documents in the pack", n)
	}
	var facts struct {
		Status string `json:"status"`
	}
	data, err := os.ReadFile(filepath.Join(vendorDD, "register", "vendor-facts.json"))
	if err != nil || json.Unmarshal(data, &facts) != nil || facts.Status != "pending legal review" {
		t.Fatalf("vendor-facts.json status = %q, %v", facts.Status, err)
	}
}

var mdLink = regexp.MustCompile(`\]\(([^)#]+)(#[^)]*)?\)`)

// TestVendorPackLinks: every relative link of the pack resolves.
func TestVendorPackLinks(t *testing.T) {
	err := filepath.WalkDir(vendorDD, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(p, ".md") {
			return err
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		for _, m := range mdLink.FindAllSubmatch(data, -1) {
			target := string(bytes.TrimSpace(m[1]))
			if strings.Contains(target, "://") {
				continue
			}
			if _, err := os.Stat(filepath.Join(filepath.Dir(p), target)); err != nil {
				t.Errorf("%s links to %s, which does not exist", p, target)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
