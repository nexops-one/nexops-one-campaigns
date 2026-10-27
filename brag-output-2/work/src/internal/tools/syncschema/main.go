// SPDX-License-Identifier: Apache-2.0

// Command syncschema copies schema/v*/schema.json and schema/manifest.schema.json
// into sdk/schema/ (embedded by the Go SDK module) and into
// sdk-python/compliance_engine_sdk/schemas/ (package data of the Python SDK).
// Run it from the repository root.
package main

import (
	"fmt"
	"os"
	"path/filepath"
)

func main() {
	sources, err := filepath.Glob(filepath.Join("schema", "v*", "schema.json"))
	if err != nil || len(sources) == 0 {
		fmt.Fprintln(os.Stderr, "syncschema: no schema/v*/schema.json found; run from the repository root")
		os.Exit(1)
	}
	sources = append(sources, filepath.Join("schema", "manifest.schema.json"))
	for _, src := range sources {
		rel, err := filepath.Rel("schema", src)
		if err != nil {
			fmt.Fprintln(os.Stderr, "syncschema:", err)
			os.Exit(1)
		}
		data, err := os.ReadFile(src)
		if err != nil {
			fmt.Fprintln(os.Stderr, "syncschema:", err)
			os.Exit(1)
		}
		for _, dst := range []string{filepath.Join("sdk", "schema", rel), filepath.Join("sdk-python", "compliance_engine_sdk", "schemas", rel)} {
			err := os.MkdirAll(filepath.Dir(dst), 0o755)
			if err == nil {
				err = os.WriteFile(dst, data, 0o644)
			}
			if err != nil {
				fmt.Fprintln(os.Stderr, "syncschema:", err)
				os.Exit(1)
			}
			fmt.Println("synced", dst)
		}
	}
}
