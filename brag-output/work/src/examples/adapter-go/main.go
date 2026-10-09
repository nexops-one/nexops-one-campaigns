// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"

	"github.com/nexops-one/compliance-engine/sdk/adapter"
	"github.com/nexops-one/compliance-engine/sdk/push"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	code := run(ctx, os.Args[1:], os.Stdout, os.Stderr, os.Getenv)
	stop()
	os.Exit(code)
}

func run(ctx context.Context, args []string, stdout, stderr io.Writer, getenv func(string) string) int {
	fs := flag.NewFlagSet("adapter-go", flag.ContinueOnError)
	fs.SetOutput(stderr)
	input := fs.String("input", "testdata/vendors.json", "vendor inventory export")
	mode := fs.String("mode", "full", "full or incremental")
	out := fs.String("out", "", "write manifest.json and batches/vendors.json to this directory")
	engine := fs.String("engine", "", "push to the engine at this base URL, with the token in COMPLIANCE_TOKEN")
	if err := fs.Parse(args); err != nil || fs.NArg() != 0 {
		return 2
	}
	m := adapter.Mode(*mode)
	if m != adapter.ModeFull && m != adapter.ModeIncremental {
		fmt.Fprintln(stderr, "-mode must be full or incremental")
		return 2
	}
	a := &VendorAdapter{Path: *input}
	b, err := a.Pull(ctx, adapter.SyncRequest{Mode: m})
	if err != nil {
		fmt.Fprintln(stderr, "error:", err)
		return 1
	}
	switch {
	case *out != "":
		if err := writeFiles(*out, a.Manifest(), b); err != nil {
			fmt.Fprintln(stderr, "error:", err)
			return 1
		}
		fmt.Fprintf(stdout, "wrote %s and %s\n", filepath.Join(*out, "manifest.json"), filepath.Join(*out, "batches", "vendors.json"))
	case *engine != "":
		c := push.New(*engine, getenv("COMPLIANCE_TOKEN"))
		if err := c.RegisterManifest(ctx, a.Manifest()); err != nil {
			fmt.Fprintln(stderr, "error:", err)
			return 1
		}
		results, err := c.Push(ctx, b)
		for _, r := range results {
			fmt.Fprintf(stdout, "ingestion %s: accepted %d, rejected %d, snapshot %s\n", r.IngestionID, r.Accepted, r.RejectedRecords, r.SnapshotID)
			for _, e := range r.Errors {
				fmt.Fprintf(stdout, "  %s[%d] %s: %s (%s)\n", e.Entity, e.Index, e.Field, e.Message, e.Code)
			}
		}
		if err != nil {
			fmt.Fprintln(stderr, "error:", err)
			return 1
		}
	default:
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(b); err != nil {
			fmt.Fprintln(stderr, "error:", err)
			return 1
		}
	}
	return 0
}

func writeFiles(dir string, m adapter.Manifest, b adapter.Batch) error {
	if err := os.MkdirAll(filepath.Join(dir, "batches"), 0o755); err != nil {
		return err
	}
	for path, v := range map[string]any{
		filepath.Join(dir, "manifest.json"):           m,
		filepath.Join(dir, "batches", "vendors.json"): b,
	} {
		data, err := json.MarshalIndent(v, "", "  ")
		if err != nil {
			return err
		}
		if err := os.WriteFile(path, append(data, '\n'), 0o644); err != nil {
			return err
		}
	}
	return nil
}
