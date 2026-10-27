// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"bytes"
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/nexops-one/compliance-engine/pkg/docgen"
)

func runDocs(ctx context.Context, args []string, env Env) int {
	if len(args) == 0 || args[0] != "gen" {
		return usageErr(env, "docs gen [--out docs/reference] [--check]")
	}
	fs := flag.NewFlagSet("docs gen", flag.ContinueOnError)
	fs.SetOutput(env.Stderr)
	out := fs.String("out", filepath.Join("docs", "reference"), "directory to write the reference documents to")
	check := fs.Bool("check", false, "only check that the documents in --out are up to date")
	if err := fs.Parse(args[1:]); err != nil || fs.NArg() != 0 {
		return usageErr(env, "docs gen [--out docs/reference] [--check]")
	}
	in, err := docgen.Default(ctx)
	if err != nil {
		return fail(env, "%v", err)
	}
	docs, err := docgen.Generate(in)
	if err != nil {
		return fail(env, "%v", err)
	}
	if env.Edition.Docs != nil {
		extra, err := env.Edition.Docs(ctx)
		if err != nil {
			return fail(env, "%v", err)
		}
		for name, data := range extra {
			if _, dup := docs[name]; dup {
				return fail(env, "edition %s: reference document %s shadows an open-core one", env.Edition.name(), name)
			}
			docs[name] = data
		}
	}
	stale := 0
	for _, name := range docgen.Names(docs) {
		p := filepath.Join(*out, name)
		if *check {
			cur, err := os.ReadFile(p)
			if err != nil || !bytes.Equal(cur, docs[name]) {
				fmt.Fprintf(env.Stderr, "%s is out of date\n", p)
				stale++
			}
			continue
		}
		if err := os.MkdirAll(*out, 0o755); err != nil {
			return fail(env, "%v", err)
		}
		if err := os.WriteFile(p, docs[name], 0o644); err != nil {
			return fail(env, "%v", err)
		}
		fmt.Fprintln(env.Stdout, "wrote", p)
	}
	if stale > 0 {
		return fail(env, "%d reference documents are out of date: run 'compliance-engine docs gen'", stale)
	}
	if *check {
		fmt.Fprintln(env.Stdout, "reference documents are up to date")
	}
	return 0
}
