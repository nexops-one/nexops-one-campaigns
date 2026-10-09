// SPDX-License-Identifier: Apache-2.0

package compliance_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"testing"
	"time"

	"github.com/nexops-one/compliance-engine/pkg/compliance"
	"github.com/nexops-one/compliance-engine/pkg/ingest"
	"github.com/nexops-one/compliance-engine/pkg/store/memory"
	"github.com/nexops-one/compliance-engine/sdk/adapter"
	"github.com/nexops-one/compliance-engine/sdk/schema"
)

var (
	ctx    = context.Background()
	scopeA = compliance.Scope{TenantID: "tenant-a", WorkspaceID: "ws-1"}
	scopeB = compliance.Scope{TenantID: "tenant-a", WorkspaceID: "ws-2"}
)

func newEngine(t *testing.T, mutate ...func(*compliance.Config)) *compliance.Engine {
	t.Helper()
	n := 0
	cfg := compliance.Config{
		Store: memory.New(),
		Clock: func() time.Time { return time.Date(2026, 1, 15, 9, 0, 0, 0, time.UTC) },
		NewID: func(prefix string) string { n++; return fmt.Sprintf("%s-%d", prefix, n) },
	}
	for _, m := range mutate {
		m(&cfg)
	}
	eng, err := compliance.New(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	return eng
}

func sampleBatch(t *testing.T) adapter.Batch {
	t.Helper()
	f, err := os.Open(filepath.Join("testdata", "sample-batch.json"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	b, err := adapter.DecodeBatch(f)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// manifestFor declares exactly the fields present in b, per entity.
func manifestFor(b adapter.Batch, modes ...adapter.Mode) adapter.Manifest {
	if len(modes) == 0 {
		modes = []adapter.Mode{adapter.ModeIncremental, adapter.ModeFull}
	}
	supplies := map[string][]string{}
	for entity, recs := range b.Entities {
		seen := map[string]bool{}
		for _, r := range recs {
			for f := range r {
				if f != schema.MetaField && !seen[f] {
					seen[f] = true
					supplies[entity] = append(supplies[entity], f)
				}
			}
		}
		sort.Strings(supplies[entity])
	}
	return adapter.Manifest{Name: b.Source.Adapter, Version: b.Source.AdapterVersion, SchemaVersion: b.SchemaVersion, Supplies: supplies, Modes: modes}
}

// loadSample registers the sample manifest and ingests the sample batch into scopeA.
func loadSample(t *testing.T, eng *compliance.Engine) ingest.Result {
	t.Helper()
	b := sampleBatch(t)
	if err := eng.RegisterManifest(ctx, scopeA, manifestFor(b)); err != nil {
		t.Fatal(err)
	}
	res, err := eng.Ingest(ctx, scopeA, b)
	if err != nil {
		t.Fatal(err)
	}
	return res
}
