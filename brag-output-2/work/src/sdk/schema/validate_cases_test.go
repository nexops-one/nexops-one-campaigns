// SPDX-License-Identifier: Apache-2.0

package schema_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/nexops-one/compliance-engine/sdk/schema"
)

// TestValidationCases checks the cases shared with the Python SDK, so both
// SDKs report the same codes for the same records.
func TestValidationCases(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("testdata", "validation-cases.json"))
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		SchemaVersion string `json:"schema_version"`
		Cases         []struct {
			Name   string         `json:"name"`
			Entity string         `json:"entity"`
			Record map[string]any `json:"record"`
			Errors []struct {
				Field string `json:"field"`
				Code  string `json:"code"`
			} `json:"errors"`
		} `json:"cases"`
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatal(err)
	}
	reg, err := schema.Default()
	if err != nil {
		t.Fatal(err)
	}
	s, err := reg.Resolve(doc.SchemaVersion)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range doc.Cases {
		got := s.ValidateRecord(c.Entity, c.Record)
		ok := len(got) == len(c.Errors)
		for i := 0; ok && i < len(got); i++ {
			ok = got[i].Field == c.Errors[i].Field && got[i].Code == c.Errors[i].Code
		}
		if !ok {
			t.Errorf("%s: got %+v, want %+v", c.Name, got, c.Errors)
		}
	}
}
