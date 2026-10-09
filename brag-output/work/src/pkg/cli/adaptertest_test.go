// SPDX-License-Identifier: Apache-2.0

package cli_test

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nexops-one/compliance-engine/sdk/adapter"
	"github.com/nexops-one/compliance-engine/sdk/adaptertest"
	"github.com/nexops-one/compliance-engine/sdk/schema"
)

const vendorManifest = `{"name":"vendor-inventory","version":"1.0.0","schema_version":"0.1.0",
"supplies":{"ict_provider":["provider_id_code","legal_name","hq_country"],"cloud_resource":["resource_ref","region","country"]},
"modes":["full","incremental"]}`

func vendorBatch(mode, legalName string) string {
	return fmt.Sprintf(`{"schema_version":"0.1.0","batch":{"mode":%q},
"source":{"system":"vendor-db","adapter":"vendor-inventory","adapter_version":"1.0.0"},
"entities":{"ict_provider":[{"provider_id_code":"V1","legal_name":%q,"hq_country":"IE"}],
"cloud_resource":[{"resource_ref":"r-1","region":"eu-west-1","country":"IE",
"_meta":{"derived_fields":[{"field":"country","method":"region_lookup"}]}}]}}`, mode, legalName)
}

func TestAdapterTestPassesConformingBatches(t *testing.T) {
	dir := t.TempDir()
	m := writeFile(t, dir, "manifest.json", vendorManifest)
	writeFile(t, dir, "run1/vendors.json", vendorBatch("full", "Nimbus"))
	writeFile(t, dir, "run2/vendors.json", vendorBatch("full", "Nimbus"))
	junit := filepath.Join(dir, "report.xml")
	code, out, errOut := run(t, nil, "adapter", "test", "--manifest", m, "--batches", filepath.Join(dir, "run1"),
		"--repeat", filepath.Join(dir, "run2"), "--derived", "cloud_resource.country", "--junit", junit)
	if code != 0 || !strings.Contains(out, "PASS idempotency") || !strings.Contains(out, "result: PASS (0 failure(s), 0 warning(s))") {
		t.Fatalf("adapter test = %d\n%s\n%s", code, out, errOut)
	}
	x, err := os.ReadFile(junit)
	if err != nil || !strings.Contains(string(x), `<testsuites name="adapter conformance: vendor-inventory" tests="9" failures="0">`) {
		t.Fatalf("junit = %s, %v", x, err)
	}
}

func TestAdapterTestReportsFailures(t *testing.T) {
	dir := t.TempDir()
	m := writeFile(t, dir, "manifest.json", vendorManifest)
	writeFile(t, dir, "run1/a.json", vendorBatch("full", "Nimbus"))
	writeFile(t, dir, "run1/b.json", vendorBatch("full", "Nimbus"))
	writeFile(t, dir, "run1/c.json", `{"schema_version":`)
	writeFile(t, dir, "run2/a.json", vendorBatch("full", "Nimbus Cloud"))
	code, out, _ := run(t, nil, "adapter", "test", "--manifest", m, "--batches", filepath.Join(dir, "run1"), "--repeat", filepath.Join(dir, "run2"))
	for _, want := range []string{"FAIL envelope", "[envelope] c.json", "FAIL full_scope", "2 full-mode batches", "FAIL idempotency", "legal_name", "result: FAIL"} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q", want)
		}
	}
	if code != 1 {
		t.Fatalf("exit code = %d\n%s", code, out)
	}
}

// The CLI runner and the Go harness must agree (spec §14): every failure the
// Go checks find in a batch is printed by the runner for the same file.
func TestAdapterTestAgreesWithGoHarness(t *testing.T) {
	reg, err := schema.Default()
	if err != nil {
		t.Fatal(err)
	}
	var m adapter.Manifest
	if err := json.Unmarshal([]byte(vendorManifest), &m); err != nil {
		t.Fatal(err)
	}
	b := adapter.NewBuilder(m, "vendor-db").Mode(adapter.ModeIncremental).
		Add("ict_provider", adapter.Record{"provider_id_code": "V1", "legal_name": "Nimbus", "hq_country": "Ireland", "currency": "EUR"}).
		Add("ict_provider", adapter.Record{"provider_id_code": "V1"}).
		Add("cloud_resource", adapter.Record{"resource_ref": "r-1", "country": "IE"}).
		Build()
	data, err := json.Marshal(b)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	mp := writeFile(t, dir, "manifest.json", vendorManifest)
	bp := writeFile(t, dir, "batch.json", string(data))
	derived := map[string][]string{"cloud_resource": {"country"}}
	want := adaptertest.Failures(adaptertest.Label(adaptertest.ValidateBatch(reg, m, b, derived), "batch.json"))
	if len(want) < 4 {
		t.Fatalf("the fixture must fail records, outside_manifest, identity and derived_fields: %v", want)
	}
	code, out, _ := run(t, nil, "adapter", "test", "--manifest", mp, "--batches", bp, "--derived", "cloud_resource.country")
	if code != 1 {
		t.Fatalf("exit code = %d\n%s", code, out)
	}
	for _, f := range want {
		if !strings.Contains(out, f.String()) {
			t.Errorf("runner output lacks %q", f.String())
		}
	}
}

func TestAdapterTestUsage(t *testing.T) {
	for _, args := range [][]string{
		{"adapter"},
		{"adapter", "run"},
		{"adapter", "test"},
		{"adapter", "test", "--manifest", "m.json"},
		{"adapter", "test", "--listen", "127.0.0.1:0", "--batches", "dir"},
		{"adapter", "test", "--manifest", "m.json", "--batches", "d", "--derived", "country"},
		{"adapter", "test", "--manifest", "m.json", "--batches", "d", "--derived", "cloud_resource.colour"},
	} {
		if code, _, _ := run(t, nil, args...); code != 2 {
			t.Errorf("%v = %d, want 2", args, code)
		}
	}
	if code, _, errOut := run(t, nil, "adapter", "test", "--manifest", "missing.json", "--batches", "missing"); code != 1 || !strings.Contains(errOut, "missing.json") {
		t.Fatalf("missing files = %d %q", code, errOut)
	}
}
