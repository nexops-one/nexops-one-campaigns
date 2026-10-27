// SPDX-License-Identifier: Apache-2.0

package cli_test

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// python returns a Python interpreter with jsonschema, or skips the test.
func python(t *testing.T) string {
	t.Helper()
	for _, name := range []string{"python3", "python"} {
		p, err := exec.LookPath(name)
		if err != nil {
			continue
		}
		if exec.Command(p, "-c", "import jsonschema").Run() == nil {
			return p
		}
	}
	t.Skip("python with jsonschema is not installed: the Python SDK's conformance is not checked")
	return ""
}

func readJSON(t *testing.T, path string) any {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var v any
	if err := json.Unmarshal(data, &v); err != nil {
		t.Fatalf("%s: %v", path, err)
	}
	return v
}

// TestPythonExampleAdapterConforms proves the Python SDK's conformance the
// way any adapter's is checked: the language-neutral runner accepts the
// batches of the Python example adapter, idempotency included. The batch is
// also the same as the Go example adapter's.
func TestPythonExampleAdapterConforms(t *testing.T) {
	py := python(t)
	root := filepath.Join("..", "..")
	example := filepath.Join(root, "sdk-python", "examples", "vendor_adapter.py")
	dir := t.TempDir()
	for _, run := range []string{"run1", "run2"} {
		out, err := exec.Command(py, example, "--out", filepath.Join(dir, run)).CombinedOutput()
		if err != nil {
			t.Fatalf("python example: %v\n%s", err, out)
		}
	}
	code, out, errOut := run(t, nil, "adapter", "test", "--manifest", filepath.Join(dir, "run1", "manifest.json"),
		"--batches", filepath.Join(dir, "run1", "batches"), "--repeat", filepath.Join(dir, "run2", "batches"),
		"--derived", "cloud_resource.country")
	if code != 0 || !strings.Contains(out, "PASS idempotency") || !strings.Contains(out, "result: PASS") {
		t.Fatalf("adapter test = %d\n%s\n%s", code, out, errOut)
	}

	// The Python example maps the same export exactly like the Go example.
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go tool not on PATH: the Go example is not run")
	}
	goOut := filepath.Join(dir, "go")
	abs, _ := filepath.Abs(goOut)
	cmd := exec.Command("go", "run", ".", "-out", abs)
	cmd.Dir = filepath.Join(root, "examples", "adapter-go")
	if b, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("go example: %v\n%s", err, b)
	}
	for _, f := range []string{"manifest.json", filepath.Join("batches", "vendors.json")} {
		if got, want := readJSON(t, filepath.Join(dir, "run1", f)), readJSON(t, filepath.Join(goOut, f)); !reflect.DeepEqual(got, want) {
			t.Errorf("%s differs between the Python and the Go example:\npython: %v\ngo:     %v", f, got, want)
		}
	}
}
