// SPDX-License-Identifier: Apache-2.0

package catalog_test

import (
	"strings"
	"testing"

	"github.com/nexops-one/compliance-engine/pkg/catalog"
	"github.com/nexops-one/compliance-engine/sdk/schema"
)

func registry(t *testing.T) *schema.Registry {
	t.Helper()
	reg, err := schema.Default()
	if err != nil {
		t.Fatal(err)
	}
	return reg
}

func mustParse(t *testing.T, doc string) *catalog.Catalog {
	t.Helper()
	c, err := catalog.Parse([]byte(doc))
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestValidateAcceptsValidCatalog(t *testing.T) {
	s, err := catalog.Validate(mustParse(t, validYAML), registry(t))
	if err != nil {
		t.Fatal(err)
	}
	if s.Version != "0.1.0" {
		t.Fatalf("target schema = %s", s.Version)
	}
}

func TestValidateRejectsSemanticErrors(t *testing.T) {
	cases := map[string][2]string{
		"unknown requires path":     {"ict_provider.legal_name, ict_provider.hq_country", "ict_provider.nope, ict_provider.hq_country"},
		"requires on other entity":  {"ict_provider.legal_name, ict_provider.hq_country", "function.rto, ict_provider.hq_country"},
		"filter field not required": {"ict_provider.legal_name, ict_provider.hq_country]", "ict_provider.legal_name]"},
		"unknown rule entity":       {"entity: ict_provider,", "entity: nope,"},
		"rule field not required":   {"field: exit_plan_exists, value", "field: substitutability, value"},
		"manual with requires":      {"requires: []\n    rule: {kind: manual}", "requires: [function.rto]\n    rule: {kind: manual}"},
		"schema constraint unmet":   {`schema_version: "^0.1"`, `schema_version: "^0.2"`},
		"duplicate control id":      {"id: test-manual", "id: test-exit-plans"},
	}
	for name, repl := range cases {
		doc := strings.Replace(validYAML, repl[0], repl[1], 1)
		if doc == validYAML {
			t.Fatalf("%s: replacement did not apply", name)
		}
		c, err := catalog.Parse([]byte(doc))
		if err != nil {
			t.Fatalf("%s: parse failed: %v", name, err)
		}
		if _, err := catalog.Validate(c, registry(t)); err == nil {
			t.Errorf("%s: expected validation error", name)
		}
	}
}

func TestValidateReferencesResolvedNeedsReferenceField(t *testing.T) {
	doc := strings.Replace(validYAML,
		"rule: {kind: fields_complete, entity: ict_provider, filter: [{field: hq_country, in: [IE, LU]}]}",
		"rule: {kind: references_resolved, entity: ict_provider, fields: [legal_name]}", 1)
	if _, err := catalog.Validate(mustParse(t, doc), registry(t)); err == nil || !strings.Contains(err.Error(), "x-references") {
		t.Fatalf("expected x-references error, got %v", err)
	}
}
