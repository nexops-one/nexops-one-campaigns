// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	"github.com/nexops-one/compliance-engine/sdk/adapter"
	"github.com/nexops-one/compliance-engine/sdk/adaptertest"
	"github.com/nexops-one/compliance-engine/sdk/schema"
)

const adapterTestSynopsis = `adapter test --manifest m.json --batches <dir|file> [--repeat <dir|file>] [--derived entity.field,...] [--junit report.xml]
       compliance-engine adapter test --listen addr [--derived entity.field,...] [--junit report.xml]`

// fileChecks are the checks of the file mode; pull does not apply to files.
var fileChecks = []string{adaptertest.CheckEnvelope, adaptertest.CheckManifestMatch, adaptertest.CheckRecords,
	adaptertest.CheckIdentity, adaptertest.CheckOutside, adaptertest.CheckDerived, adaptertest.CheckFullScope}

func runAdapter(ctx context.Context, args []string, env Env) int {
	if len(args) == 0 || args[0] != "test" {
		return usageErr(env, adapterTestSynopsis)
	}
	fs := flag.NewFlagSet("adapter test", flag.ContinueOnError)
	fs.SetOutput(env.Stderr)
	manifestPath := fs.String("manifest", "", "adapter manifest JSON file")
	batches := fs.String("batches", "", "batch file, or directory of *.json batch files, produced by one sync")
	repeat := fs.String("repeat", "", "batches produced by a second sync of the same input (idempotency check)")
	derivedList := fs.String("derived", "", "comma-separated entity.field values the input makes the adapter infer")
	junit := fs.String("junit", "", "also write a JUnit XML report to this file")
	listen := fs.String("listen", "", "serve a local conformance endpoint on this address instead of reading files")
	if err := fs.Parse(args[1:]); err != nil || fs.NArg() != 0 {
		return usageErr(env, adapterTestSynopsis)
	}
	reg, err := schema.Default()
	if err != nil {
		return fail(env, "%v", err)
	}
	derived, err := parseDerived(reg.Latest(), *derivedList)
	if err != nil {
		fmt.Fprintln(env.Stderr, err)
		return usageErr(env, adapterTestSynopsis)
	}
	if *listen != "" {
		if *manifestPath != "" || *batches != "" || *repeat != "" {
			return usageErr(env, adapterTestSynopsis)
		}
		ln, err := net.Listen("tcp", *listen)
		if err != nil {
			return fail(env, "%v", err)
		}
		return serveConformance(ctx, ln, newConformanceServer(reg, derived), env, *junit)
	}
	if *manifestPath == "" || *batches == "" {
		return usageErr(env, adapterTestSynopsis)
	}
	r, err := fileReport(reg, *manifestPath, *batches, *repeat, derived)
	if err != nil {
		return fail(env, "%v", err)
	}
	return finish(env, r, *junit)
}

// parseDerived turns "entity.field,..." into a map, rejecting unknown fields
// (a misspelled field would silently never be checked).
func parseDerived(s *schema.Schema, list string) (map[string][]string, error) {
	out := map[string][]string{}
	if list == "" {
		return out, nil
	}
	for _, p := range strings.Split(list, ",") {
		p = strings.TrimSpace(p)
		if _, _, ok := s.Field(p); !ok {
			return nil, fmt.Errorf("--derived: %q is not an entity.field of canonical schema %s", p, s.Version)
		}
		entity, field, _ := strings.Cut(p, ".")
		out[entity] = append(out[entity], field)
	}
	return out, nil
}

type namedBatch struct {
	name  string
	batch adapter.Batch
}

func batchesOf(nbs []namedBatch) []adapter.Batch {
	out := make([]adapter.Batch, len(nbs))
	for i, nb := range nbs {
		out[i] = nb.batch
	}
	return out
}

// loadBatches reads a batch file or every *.json file of a directory. Files
// that are not valid batches become envelope findings.
func loadBatches(path, prefix string) ([]namedBatch, []adaptertest.Finding, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, nil, err
	}
	files := []string{path}
	if info.IsDir() {
		files, err = filepath.Glob(filepath.Join(path, "*.json"))
		if err != nil {
			return nil, nil, err
		}
		if len(files) == 0 {
			return nil, nil, fmt.Errorf("no *.json batch files in %s", path)
		}
		sort.Strings(files)
	}
	var out []namedBatch
	var findings []adaptertest.Finding
	for _, f := range files {
		name := prefix + filepath.Base(f)
		file, err := os.Open(f)
		if err != nil {
			return nil, nil, err
		}
		b, err := adapter.DecodeBatch(file)
		file.Close()
		if err != nil {
			findings = append(findings, adaptertest.Finding{Check: adaptertest.CheckEnvelope, Batch: name, Index: -1, Message: err.Error()})
			continue
		}
		out = append(out, namedBatch{name: name, batch: b})
	}
	return out, findings, nil
}

func fileReport(reg *schema.Registry, manifestPath, batchesPath, repeatPath string, derived map[string][]string) (adaptertest.Report, error) {
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		return adaptertest.Report{}, err
	}
	var m adapter.Manifest
	var mf []adaptertest.Finding
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&m); err != nil {
		mf = []adaptertest.Finding{{Check: adaptertest.CheckManifest, Index: -1, Message: "decode: " + err.Error()}}
	} else {
		mf = adaptertest.ValidateManifest(reg, m)
	}
	mf = adaptertest.Label(mf, filepath.Base(manifestPath))

	first, bf, err := loadBatches(batchesPath, "")
	if err != nil {
		return adaptertest.Report{}, err
	}
	for _, nb := range first {
		bf = append(bf, adaptertest.Label(adaptertest.ValidateBatch(reg, m, nb.batch, derived), nb.name)...)
	}
	bf = append(bf, adaptertest.ValidateSync(batchesOf(first))...)
	checks := slices.Clone(fileChecks)
	if repeatPath != "" {
		second, rf, err := loadBatches(repeatPath, "repeat/")
		if err != nil {
			return adaptertest.Report{}, err
		}
		bf = append(bf, rf...)
		bf = append(bf, adaptertest.CompareRuns(reg, m, batchesOf(first), batchesOf(second))...)
		checks = append(checks, adaptertest.CheckIdempotency)
	}
	adaptertest.SortFindings(bf)
	name := m.Name
	if name == "" {
		name = manifestPath
	}
	return adaptertest.Report{Adapter: name, Suites: []adaptertest.Suite{
		{Name: "manifest", Checks: []string{adaptertest.CheckManifest}, Findings: mf},
		{Name: "batches", Checks: checks, Findings: bf},
	}}, nil
}

// finish prints the report, writes the JUnit file if asked, and returns the exit code.
func finish(env Env, r adaptertest.Report, junitPath string) int {
	if err := adaptertest.WriteText(env.Stdout, r); err != nil {
		return fail(env, "%v", err)
	}
	if junitPath != "" {
		var buf bytes.Buffer
		if err := adaptertest.WriteJUnit(&buf, r); err != nil {
			return fail(env, "%v", err)
		}
		if err := os.WriteFile(junitPath, buf.Bytes(), 0o644); err != nil {
			return fail(env, "%v", err)
		}
		fmt.Fprintf(env.Stdout, "JUnit report: %s\n", junitPath)
	}
	if r.Failed() {
		return 1
	}
	return 0
}
