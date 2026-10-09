// SPDX-License-Identifier: Apache-2.0

package main

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nexops-one/compliance-engine/sdk/adapter"
	"github.com/nexops-one/compliance-engine/sdk/adaptertest"
)

var ctx = context.Background()

// The conformance suite every adapter should run in its own tests.
func TestConformance(t *testing.T) {
	derived := map[string][]string{"cloud_resource": {"country"}}
	adaptertest.Run(t, &VendorAdapter{Path: "testdata/vendors.json"},
		adaptertest.Fixture{Name: "full", Request: adapter.SyncRequest{Mode: adapter.ModeFull}, Derived: derived},
		adaptertest.Fixture{Name: "incremental", Request: adapter.SyncRequest{Mode: adapter.ModeIncremental}, Derived: derived},
	)
}

func TestMapping(t *testing.T) {
	b, err := (&VendorAdapter{Path: "testdata/vendors.json"}).Pull(ctx, adapter.SyncRequest{Mode: adapter.ModeFull})
	if err != nil {
		t.Fatal(err)
	}
	if b.BatchID() != "vendors@2026-09-28T18:00:00Z" || b.Mode() != adapter.ModeFull {
		t.Fatalf("batch info = %+v", b.Batch)
	}
	p := b.Entities["ict_provider"]
	if len(p) != 2 || p[0]["provider_id_code"] != "SAMPLETP000000000087" || p[0]["provider_id_type"] != "LEI" || p[1]["provider_id_code"] != "vendor:V2" {
		t.Fatalf("providers = %v", p)
	}
	if _, ok := p[1]["provider_id_type"]; ok {
		t.Fatal("without an LEI, provider_id_type must stay missing")
	}
	r := b.Entities["cloud_resource"]
	if len(r) != 3 || r[0]["country"] != "IE" || r[1]["country"] != "DE" || len(adapter.MetaOf(r[0]).DerivedFields) != 1 {
		t.Fatalf("resources = %v", r)
	}
	if _, ok := r[2]["country"]; ok || len(adapter.MetaOf(r[2]).DerivedFields) != 0 {
		t.Fatal("an unknown region must leave country missing")
	}
	if ref := adapter.MetaOf(r[2]).SourceRecordRef; ref != "vendors/V2/resources/st-9" {
		t.Fatalf("source ref = %q", ref)
	}
}

func TestRunWritesFilesForTheCLIRunner(t *testing.T) {
	dir := t.TempDir()
	var out, errOut bytes.Buffer
	if code := run(ctx, []string{"-out", dir}, &out, &errOut, os.Getenv); code != 0 {
		t.Fatalf("run = %d %s", code, errOut.String())
	}
	f, err := os.Open(filepath.Join(dir, "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if m, err := adapter.DecodeManifest(f); err != nil || m.Name != "vendor-inventory" {
		t.Fatalf("manifest = %+v, %v", m, err)
	}
	g, err := os.Open(filepath.Join(dir, "batches", "vendors.json"))
	if err != nil {
		t.Fatal(err)
	}
	defer g.Close()
	if b, err := adapter.DecodeBatch(g); err != nil || b.RecordCount() != 5 {
		t.Fatalf("batch = %d records, %v", b.RecordCount(), err)
	}
	if code := run(ctx, []string{"-mode", "sometimes"}, &out, &errOut, os.Getenv); code != 2 {
		t.Fatalf("bad mode = %d", code)
	}
}

func TestRunPushesToAnEngine(t *testing.T) {
	var calls []string
	var auth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls = append(calls, r.Method+" "+r.URL.Path)
		auth = r.Header.Get("Authorization")
		if r.Method == http.MethodPut {
			_, _ = io.WriteString(w, `{}`)
			return
		}
		w.WriteHeader(http.StatusCreated)
		_, _ = io.WriteString(w, `{"ingestion_id":"ing-1","accepted":5,"snapshot_id":"rev-1"}`)
	}))
	defer srv.Close()
	var out, errOut bytes.Buffer
	env := func(k string) string {
		if k == "COMPLIANCE_TOKEN" {
			return "tok"
		}
		return ""
	}
	if code := run(ctx, []string{"-engine", srv.URL}, &out, &errOut, env); code != 0 {
		t.Fatalf("run = %d %s", code, errOut.String())
	}
	if strings.Join(calls, ", ") != "PUT /api/v1/adapters/vendor-inventory/manifest, POST /api/v1/ingestions" || auth != "Bearer tok" {
		t.Fatalf("calls = %v, auth = %q", calls, auth)
	}
	if !strings.Contains(out.String(), "ingestion ing-1: accepted 5, rejected 0, snapshot rev-1") {
		t.Fatalf("output = %q", out.String())
	}
}
