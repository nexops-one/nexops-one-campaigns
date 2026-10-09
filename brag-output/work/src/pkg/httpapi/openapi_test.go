// SPDX-License-Identifier: Apache-2.0

package httpapi_test

import (
	"bytes"
	"sort"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/nexops-one/compliance-engine/api"
	"github.com/nexops-one/compliance-engine/pkg/httpapi"
)

type operation struct {
	OperationID string              `yaml:"operationId"`
	Summary     string              `yaml:"summary"`
	Security    *[]map[string][]any `yaml:"security"`
	Permission  string              `yaml:"x-permission"`
}

func TestOpenAPIMatchesRoutes(t *testing.T) {
	var doc struct {
		OpenAPI  string                          `yaml:"openapi"`
		Security []map[string][]any              `yaml:"security"`
		Paths    map[string]map[string]operation `yaml:"paths"`
	}
	if err := yaml.Unmarshal(api.OpenAPI, &doc); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(doc.OpenAPI, "3.1") || len(doc.Security) != 1 {
		t.Fatalf("openapi %q security %v", doc.OpenAPI, doc.Security)
	}
	documented := map[string]operation{}
	for path, ops := range doc.Paths {
		for method, op := range ops {
			if method == "parameters" {
				continue
			}
			documented[strings.ToUpper(method)+" "+path] = op
		}
	}
	routes := map[string]httpapi.Route{}
	for _, r := range httpapi.Routes() {
		routes[r.Method+" "+r.Pattern] = r
	}
	var missing, extra []string
	for k, r := range routes {
		op, ok := documented[k]
		if !ok {
			missing = append(missing, k)
			continue
		}
		if op.OperationID == "" || op.Summary == "" {
			t.Errorf("%s: operationId and summary are required", k)
		}
		public := op.Security != nil && len(*op.Security) == 0
		if public != r.Public {
			t.Errorf("%s: public=%v in code but security override=%v in OpenAPI", k, r.Public, public)
		}
		if op.Permission != string(r.Permission) {
			t.Errorf("%s: permission %q in code but x-permission %q in OpenAPI", k, r.Permission, op.Permission)
		}
	}
	for k := range documented {
		if _, ok := routes[k]; !ok {
			extra = append(extra, k)
		}
	}
	sort.Strings(missing)
	sort.Strings(extra)
	if len(missing) > 0 || len(extra) > 0 {
		t.Fatalf("OpenAPI drift: missing %v, extra %v", missing, extra)
	}
}

func TestOpenAPIIsServed(t *testing.T) {
	r := call(t, newAPI(t, setup{}), "GET", "/api/v1/openapi.yaml", "", nil)
	if r.Status != 200 || r.Header.Get("Content-Type") != "application/yaml" || !bytes.Equal(r.Raw, api.OpenAPI) {
		t.Fatalf("openapi = %d %q", r.Status, r.Header.Get("Content-Type"))
	}
}
