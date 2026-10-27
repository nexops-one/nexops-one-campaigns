// SPDX-License-Identifier: Apache-2.0

package demo_test

import (
	"bytes"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nexops-one/compliance-engine/demo"
	"github.com/nexops-one/compliance-engine/demo/dataset"
	"github.com/nexops-one/compliance-engine/sdk/schema"
)

// TestGeneratedFilesAreFresh fails when the committed register or checksums
// differ from a fresh generation: run go run ./demo/gen.
func TestGeneratedFilesAreFresh(t *testing.T) {
	reg, err := schema.Default()
	if err != nil {
		t.Fatal(err)
	}
	book, err := dataset.Workbook(reg.Latest())
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(book, demo.Register()) {
		t.Fatal("demo/sample-register.xlsx is stale: run go run ./demo/gen")
	}
	sums, err := demo.Sums(demo.Evidence())
	if err != nil {
		t.Fatal(err)
	}
	committed, err := fs.ReadFile(demo.Evidence(), "SHA256SUMS")
	if err != nil || !bytes.Equal(sums, committed) {
		t.Fatalf("demo/evidence/SHA256SUMS is stale: run go run ./demo/gen\n%s", sums)
	}
}

func TestEvidenceIsMarkedSample(t *testing.T) {
	n := 0
	err := fs.WalkDir(demo.Evidence(), ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || p == "SHA256SUMS" {
			return err
		}
		n++
		data, err := fs.ReadFile(demo.Evidence(), p)
		if err != nil {
			return err
		}
		if !strings.HasPrefix(string(data), "SAMPLE DATA") {
			t.Errorf("%s does not start with SAMPLE DATA", p)
		}
		return nil
	})
	if err != nil || n != 3 {
		t.Fatalf("%d evidence documents, %v", n, err)
	}
}

func TestWriteEvidence(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "evidence")
	if err := demo.WriteEvidence(dir); err != nil {
		t.Fatal(err)
	}
	sums, err := demo.Sums(os.DirFS(dir))
	if err != nil {
		t.Fatal(err)
	}
	committed, _ := fs.ReadFile(demo.Evidence(), "SHA256SUMS")
	if !bytes.Equal(sums, committed) {
		t.Fatal("written evidence differs from the embedded documents")
	}
}
