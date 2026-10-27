// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"

	"github.com/nexops-one/compliance-engine/pkg/canonical"
	"github.com/nexops-one/compliance-engine/pkg/compliance"
	"github.com/nexops-one/compliance-engine/pkg/store/memory"
	"github.com/nexops-one/compliance-engine/sdk/adapter"
	"github.com/nexops-one/compliance-engine/sdk/schema"
)

func runValidate(ctx context.Context, args []string, env Env) int {
	fs := flag.NewFlagSet("validate", flag.ContinueOnError)
	fs.SetOutput(env.Stderr)
	asJSON := fs.Bool("json", false, "print the full dry-run result as JSON")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() != 1 {
		return usageErr(env, "validate [--json] <batch.json>")
	}
	f, err := os.Open(fs.Arg(0))
	if err != nil {
		return fail(env, "%v", err)
	}
	defer f.Close()
	b, err := adapter.DecodeBatch(f)
	if err != nil {
		return fail(env, "%v", err)
	}
	reg, err := schema.Default()
	if err != nil {
		return fail(env, "%v", err)
	}
	s, err := reg.Resolve(b.SchemaVersion)
	if err != nil {
		return fail(env, "%v", err)
	}
	if errs := b.ValidateEnvelope(s); len(errs) > 0 {
		for _, e := range errs {
			fmt.Fprintf(env.Stderr, "envelope %s: %s (%s)\n", e.Field, e.Message, e.Code)
		}
		return 1
	}
	eng, err := compliance.New(ctx, compliance.Config{Store: memory.New()})
	if err != nil {
		return fail(env, "%v", err)
	}
	scope := compliance.Scope{TenantID: "local", WorkspaceID: "validate"}
	if err := eng.RegisterManifest(ctx, scope, manifestFromBatch(s, b)); err != nil {
		return fail(env, "%v", err)
	}
	dr, err := eng.DryRun(ctx, scope, b)
	if err != nil {
		var be *compliance.BatchError
		if errors.As(err, &be) {
			for _, e := range be.Errors {
				fmt.Fprintf(env.Stderr, "envelope %s: %s (%s)\n", e.Field, e.Message, e.Code)
			}
			return 1
		}
		return fail(env, "%v", err)
	}
	if *asJSON {
		enc := json.NewEncoder(env.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(dr); err != nil {
			return fail(env, "%v", err)
		}
	} else {
		printSummary(env.Stdout, b, dr)
	}
	if dr.Result.RejectedRecords > 0 {
		return 1
	}
	return 0
}

// manifestFromBatch declares every canonical field the batch contains plus the
// identity fields of each entity, so offline validation reports record
// problems rather than manifest problems.
func manifestFromBatch(s *schema.Schema, b adapter.Batch) adapter.Manifest {
	supplies := map[string][]string{}
	for entity, recs := range b.Entities {
		e, ok := s.Entity(entity)
		if !ok {
			continue
		}
		seen := map[string]bool{}
		for _, k := range e.IdentityKey {
			seen[k] = true
		}
		for _, r := range recs {
			for f := range r {
				if _, known := e.Fields[f]; known {
					seen[f] = true
				}
			}
		}
		for f := range seen {
			supplies[entity] = append(supplies[entity], f)
		}
		sort.Strings(supplies[entity])
	}
	version := b.Source.AdapterVersion
	if version == "" {
		version = "unknown"
	}
	return adapter.Manifest{Name: b.Source.Adapter, Version: version, SchemaVersion: b.SchemaVersion, Supplies: supplies,
		Modes: []adapter.Mode{adapter.ModeIncremental, adapter.ModeFull}}
}

func printSummary(w io.Writer, b adapter.Batch, dr compliance.DryRunResult) {
	r, c := dr.Result, dr.Completeness
	fmt.Fprintf(w, "batch: schema %s, source %s (%s %s), %d record(s), mode %s\n",
		r.SchemaVersion, r.Source.System, r.Source.Adapter, r.Source.AdapterVersion, b.RecordCount(), r.Mode)
	fmt.Fprintf(w, "accepted %d, rejected %d, warnings %d\n", r.Accepted, r.RejectedRecords, len(r.Warnings))
	for _, e := range r.Errors {
		fmt.Fprintf(w, "  error   %s[%d] %s: %s (%s)\n", e.Entity, e.Index, e.Field, e.Message, e.Code)
	}
	for _, e := range r.Warnings {
		fmt.Fprintf(w, "  warning %s[%d] %s: %s (%s)\n", e.Entity, e.Index, e.Field, e.Message, e.Code)
	}
	missing, derived := 0, 0
	for _, g := range c.Gaps {
		if g.State == canonical.StateMissing {
			missing++
		} else {
			derived++
		}
	}
	fmt.Fprintf(w, "completeness: %d missing and %d derived export-required field(s), %d dangling reference(s), %d invalid code(s)\n",
		missing, derived, len(c.References.Dangling), len(c.InvalidCodes))
	if len(c.UnverifiedCodelists) > 0 {
		fmt.Fprintf(w, "codelists not loaded (values unverified): %s\n", strings.Join(c.UnverifiedCodelists, ", "))
	}
}
