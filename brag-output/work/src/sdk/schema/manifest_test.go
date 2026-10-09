// SPDX-License-Identifier: Apache-2.0

package schema_test

import (
	"strings"
	"testing"

	"github.com/nexops-one/compliance-engine/sdk/schema"
)

func hasError(errs []schema.FieldError, field, code string) bool {
	for _, e := range errs {
		if e.Field == field && e.Code == code && e.Index == -1 {
			return true
		}
	}
	return false
}

func TestValidateManifestDocument(t *testing.T) {
	valid := `{"name":"csv-import.ict_provider","version":"0.1.0","schema_version":"0.1.0",
		"supplies":{"ict_provider":["provider_id_code","legal_name"]},"modes":["incremental","full"]}`
	if errs := schema.ValidateManifestDocument([]byte(valid)); errs != nil {
		t.Fatalf("valid manifest: %v", errs)
	}
	bad := `{"version":"","schema_version":"1.0","supplies":{"ict_provider":[]},"modes":["sometimes"],"colour":"red"}`
	errs := schema.ValidateManifestDocument([]byte(bad))
	for _, want := range [][2]string{
		{"name", "required"},
		{"version", "minLength"},
		{"schema_version", "pattern"},
		{"supplies.ict_provider", "minItems"},
		{"modes.0", "enum"},
		{"colour", "unknown_field"},
	} {
		if !hasError(errs, want[0], want[1]) {
			t.Errorf("missing %s/%s in %+v", want[0], want[1], errs)
		}
	}
	if errs := schema.ValidateManifestDocument([]byte(`{"name":`)); len(errs) != 1 || errs[0].Code != "invalid_json" {
		t.Fatalf("truncated document = %+v", errs)
	}
	if !strings.Contains(string(schema.ManifestSchema), schema.ManifestSchemaID) {
		t.Fatal("ManifestSchema must declare ManifestSchemaID as its $id")
	}
}
