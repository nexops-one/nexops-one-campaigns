// SPDX-License-Identifier: Apache-2.0

package schemadata_test

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestSDKSchemaCopiesMatchSource(t *testing.T) {
	sources, err := filepath.Glob(filepath.Join("v*", "schema.json"))
	if err != nil || len(sources) == 0 {
		t.Fatalf("no source schemas found: %v", err)
	}
	sources = append(sources, "manifest.schema.json")
	for _, src := range sources {
		want, err := os.ReadFile(src)
		if err != nil {
			t.Fatal(err)
		}
		for _, dir := range []string{filepath.Join("..", "sdk", "schema"), filepath.Join("..", "sdk-python", "compliance_engine_sdk", "schemas")} {
			got, err := os.ReadFile(filepath.Join(dir, src))
			if err != nil {
				t.Fatalf("missing copy of %s in %s: run `go run ./internal/tools/syncschema` from the repo root", src, dir)
			}
			if !bytes.Equal(want, got) {
				t.Fatalf("copy of %s in %s is stale: run `go run ./internal/tools/syncschema` from the repo root", src, dir)
			}
		}
	}
}
