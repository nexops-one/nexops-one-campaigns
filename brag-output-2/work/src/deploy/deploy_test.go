// SPDX-License-Identifier: Apache-2.0

package deploy_test

import (
	"bytes"
	"encoding/json"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/nexops-one/compliance-engine/pkg/config"
	"github.com/nexops-one/compliance-engine/sdk/adapter"
	"github.com/nexops-one/compliance-engine/sdk/schema"
)

func TestComposeUsesKnownVariables(t *testing.T) {
	data, err := os.ReadFile("docker-compose.yml")
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Services map[string]struct {
			Environment map[string]string `yaml:"environment"`
		} `yaml:"services"`
	}
	if err := yaml.Unmarshal(data, &doc); err != nil {
		t.Fatal(err)
	}
	known := map[string]bool{}
	for _, v := range config.Variables() {
		known[v.Name] = true
	}
	env := doc.Services["engine"].Environment
	for k := range env {
		if strings.HasPrefix(k, "COMPLIANCE_") && !known[k] {
			t.Errorf("compose sets unknown variable %s", k)
		}
	}
	for _, required := range []string{"COMPLIANCE_DATABASE_URL", "COMPLIANCE_TOKENS"} {
		if env[required] == "" {
			t.Errorf("compose must set %s", required)
		}
	}
	if _, ok := doc.Services["postgres"]; !ok {
		t.Error("compose must define the postgres service")
	}
}

func TestEnvExampleCoversComposeInputs(t *testing.T) {
	data, err := os.ReadFile(".env.example")
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"POSTGRES_PASSWORD=", "COMPLIANCE_TOKENS="} {
		if !bytes.Contains(data, []byte(key)) {
			t.Errorf(".env.example must define %s", strings.TrimSuffix(key, "="))
		}
	}
}

func TestSmokeManifestIsValid(t *testing.T) {
	data, err := os.ReadFile("smoke/manifest.json")
	if err != nil {
		t.Fatal(err)
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	var m adapter.Manifest
	if err := dec.Decode(&m); err != nil {
		t.Fatal(err)
	}
	reg, err := schema.Default()
	if err != nil {
		t.Fatal(err)
	}
	if err := m.Validate(reg); err != nil || m.Name != "csv-import" {
		t.Fatalf("manifest %s: %v", m.Name, err)
	}
}

func TestDemoComposeIsADemo(t *testing.T) {
	data, err := os.ReadFile("demo/compose.yml")
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Services map[string]struct {
			Environment map[string]string `yaml:"environment"`
			Volumes     []string          `yaml:"volumes"`
			Ports       []string          `yaml:"ports"`
		} `yaml:"services"`
	}
	if err := yaml.Unmarshal(data, &doc); err != nil {
		t.Fatal(err)
	}
	known := map[string]bool{}
	for _, v := range config.Variables() {
		known[v.Name] = true
	}
	eng := doc.Services["engine"]
	for k := range eng.Environment {
		if strings.HasPrefix(k, "COMPLIANCE_") && !known[k] {
			t.Errorf("demo compose sets unknown variable %s", k)
		}
	}
	if eng.Environment["COMPLIANCE_DEMO"] != "true" || eng.Environment["COMPLIANCE_EVIDENCE_ROOT"] != "/demo/evidence" ||
		eng.Environment["COMPLIANCE_CONSOLE_TENANT"] != "demo" || eng.Environment["COMPLIANCE_TOKENS"] != "" {
		t.Errorf("demo engine environment = %v", eng.Environment)
	}
	if len(eng.Volumes) != 1 || !strings.HasSuffix(eng.Volumes[0], ":/demo/evidence:ro") {
		t.Errorf("the sample evidence is mounted read-only: %v", eng.Volumes)
	}
	for _, p := range eng.Ports {
		if !strings.HasPrefix(p, "127.0.0.1:") {
			t.Errorf("the demo listens on loopback only: %s", p)
		}
	}
	if len(doc.Services["postgres"].Ports) != 0 {
		t.Error("the demo database is not published")
	}
}

// TestSPDXHeaders checks that every Go file of the engine, the SDK and the
// example adapter starts with the Apache-2.0 SPDX identifier.
func TestSPDXHeaders(t *testing.T) {
	const header = "// SPDX-License-Identifier: Apache-2.0\n"
	n := 0
	err := filepath.WalkDir("..", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() && (d.Name() == ".git" || d.Name() == "bin" || d.Name() == "dist" || d.Name() == "node_modules") {
			return filepath.SkipDir
		}
		if d.IsDir() || !strings.HasSuffix(p, ".go") {
			return nil
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		n++
		if !bytes.HasPrefix(data, []byte(header)) {
			t.Errorf("%s does not start with %q", p, strings.TrimSpace(header))
		}
		return nil
	})
	if err != nil || n < 200 {
		t.Fatalf("checked %d files: %v", n, err)
	}
	for _, f := range []string{"../LICENSE", "../sdk/LICENSE", "../NOTICE"} {
		if _, err := os.Stat(f); err != nil {
			t.Errorf("%s: %v", f, err)
		}
	}
}

func TestDockerfileIsMultiArchDistroless(t *testing.T) {
	data, err := os.ReadFile("Dockerfile")
	if err != nil {
		t.Fatal(err)
	}
	s := string(data)
	for _, want := range []string{
		"FROM --platform=$BUILDPLATFORM golang:", "ARG TARGETOS", "ARG TARGETARCH", "GOOS=$TARGETOS GOARCH=$TARGETARCH",
		"CGO_ENABLED=0", "FROM gcr.io/distroless/static-debian12:nonroot", "USER nonroot:nonroot", `org.opencontainers.image.licenses="Apache-2.0"`,
	} {
		if !strings.Contains(s, want) {
			t.Errorf("Dockerfile lacks %q", want)
		}
	}
}

func TestScriptsParse(t *testing.T) {
	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Skip("bash is not installed")
	}
	for _, f := range []string{"smoke.sh", "demo/demo-smoke.sh", "release/release.sh", "offline/build.sh", "offline/install.sh", "../scripts/check-dco.sh"} {
		if out, err := exec.Command(bash, "-n", f).CombinedOutput(); err != nil {
			t.Errorf("%s: %v %s", f, err, out)
		}
	}
}

func TestOfflineBundleFiles(t *testing.T) {
	data, err := os.ReadFile("offline/build.sh")
	if err != nil {
		t.Fatal(err)
	}
	_, block, ok := strings.Cut(string(data), "# BEGIN INCLUDE")
	block, _, ok2 := strings.Cut(block, "# END INCLUDE")
	if !ok || !ok2 {
		t.Fatal("offline/build.sh has no include block")
	}
	n := 0
	for _, line := range strings.Split(block, "\n") {
		p := strings.TrimSpace(line)
		if p == "" || strings.HasPrefix(p, "include=") || p == ")" {
			continue
		}
		n++
		if _, err := os.Stat(filepath.Join("..", p)); err != nil {
			t.Errorf("the offline bundle includes %s: %v", p, err)
		}
	}
	if n < 10 {
		t.Fatalf("include block lists %d paths", n)
	}
	compose, err := os.ReadFile("offline/compose.yml")
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Services map[string]struct {
			Image       string            `yaml:"image"`
			Build       any               `yaml:"build"`
			Environment map[string]string `yaml:"environment"`
		} `yaml:"services"`
	}
	if err := yaml.Unmarshal(compose, &doc); err != nil {
		t.Fatal(err)
	}
	known := map[string]bool{}
	for _, v := range config.Variables() {
		known[v.Name] = true
	}
	eng := doc.Services["engine"]
	if eng.Build != nil || !strings.HasPrefix(eng.Image, "compliance-engine:") {
		t.Errorf("the offline engine uses the loaded image, never a build: %+v", eng)
	}
	for k := range eng.Environment {
		if strings.HasPrefix(k, "COMPLIANCE_") && !known[k] {
			t.Errorf("offline compose sets unknown variable %s", k)
		}
	}
}
