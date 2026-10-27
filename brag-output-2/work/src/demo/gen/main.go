// SPDX-License-Identifier: Apache-2.0

// Command gen writes the demo's sample register and the checksums of the
// sample evidence: run it from the repository root with go run ./demo/gen.
// TestGeneratedFilesAreFresh fails when the committed files differ from a
// fresh generation.
package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/nexops-one/compliance-engine/demo"
	"github.com/nexops-one/compliance-engine/demo/dataset"
	"github.com/nexops-one/compliance-engine/sdk/schema"
)

func main() {
	if err := run("demo"); err != nil {
		fmt.Fprintln(os.Stderr, "gen:", err)
		os.Exit(1)
	}
}

func run(dir string) error {
	if _, err := os.Stat(filepath.Join(dir, "evidence")); err != nil {
		return fmt.Errorf("run from the repository root: %w", err)
	}
	reg, err := schema.Default()
	if err != nil {
		return err
	}
	book, err := dataset.Workbook(reg.Latest())
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, demo.RegisterFile), book, 0o644); err != nil {
		return err
	}
	sums, err := demo.Sums(os.DirFS(filepath.Join(dir, "evidence")))
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, "evidence", "SHA256SUMS"), sums, 0o644); err != nil {
		return err
	}
	fmt.Printf("wrote %s and %s\n", filepath.Join(dir, demo.RegisterFile), filepath.Join(dir, "evidence", "SHA256SUMS"))
	return nil
}
