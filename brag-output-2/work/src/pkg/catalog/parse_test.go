// SPDX-License-Identifier: Apache-2.0

package catalog_test

import (
	"strings"
	"testing"

	"github.com/nexops-one/compliance-engine/pkg/catalog"
)

const validYAML = `
catalog: test
version: 1.0.0
framework: TEST
jurisdiction: EU
effective_date: 2025-01-17
schema_version: "^0.1"
source_authority: "Test regulation (indicative)"
scoring:
  counts_as_ready: [ready, monitoring]
  assumptions: Score is ready or monitoring over assessable; coverage is assessable over in scope.
controls:
  - id: test-provider-names
    title: Providers are named
    description: Every provider has a legal name.
    source_authority: "Test article 1 (indicative)"
    evidence_requirements: [Provider records with legal names]
    requires: [ict_provider.legal_name, ict_provider.hq_country]
    rule: {kind: fields_complete, entity: ict_provider, filter: [{field: hq_country, in: [IE, LU]}]}
  - id: test-exit-plans
    title: Exit plans exist
    description: Every assessed service has an exit plan.
    source_authority: "Test article 2 (indicative)"
    evidence_requirements: []
    requires: [service_assessment.exit_plan_exists]
    accept_derived: true
    rule: {kind: field_equals, entity: service_assessment, field: exit_plan_exists, value: true}
  - id: test-manual
    title: Manual review
    description: Needs a human.
    source_authority: "Test article 3 (indicative)"
    evidence_requirements: [Signed review]
    requires: []
    rule: {kind: manual}
`

func TestParseValidCatalog(t *testing.T) {
	c, err := catalog.Parse([]byte(validYAML))
	if err != nil {
		t.Fatal(err)
	}
	if c.Ref() != (catalog.Ref{Catalog: "test", Version: "1.0.0"}) || c.Ref().String() != "test@1.0.0" {
		t.Fatalf("ref = %v", c.Ref())
	}
	if c.EffectiveDate != "2025-01-17" {
		t.Fatalf("unquoted YAML dates must stay dates, got %q", c.EffectiveDate)
	}
	if len(c.Controls) != 3 || c.Controls[0].Rule.Filter[0].Field != "hq_country" || !c.Controls[1].AcceptDerived {
		t.Fatalf("controls = %+v", c.Controls)
	}
	if c.Controls[1].Rule.Value != true || c.Controls[2].Rule.Kind != catalog.KindManual {
		t.Fatalf("rules = %+v", c.Controls)
	}
}

func TestParseRejectsMetaSchemaViolations(t *testing.T) {
	cases := map[string]string{
		"unknown top-level field": strings.Replace(validYAML, "framework: TEST", "framework: TEST\nextra: 1", 1),
		"bad rule kind":           strings.Replace(validYAML, "kind: manual", "kind: magic", 1),
		"field_equals no value":   strings.Replace(validYAML, ", value: true", "", 1),
		"bad control id":          strings.Replace(validYAML, "id: test-manual", "id: Test Manual", 1),
		"bad version":             strings.Replace(validYAML, "version: 1.0.0", "version: one", 1),
		"not yaml":                "{{{",
	}
	for name, doc := range cases {
		if _, err := catalog.Parse([]byte(doc)); err == nil {
			t.Errorf("%s: expected error", name)
		}
	}
}

func TestParseRef(t *testing.T) {
	r, err := catalog.ParseRef("dora@1.0.0")
	if err != nil || r != (catalog.Ref{Catalog: "dora", Version: "1.0.0"}) {
		t.Fatalf("got %v, %v", r, err)
	}
	for _, bad := range []string{"dora", "@1.0.0", "dora@", "dora@x"} {
		if _, err := catalog.ParseRef(bad); err == nil {
			t.Errorf("%q should fail", bad)
		}
	}
}

func TestParseReviewFields(t *testing.T) {
	doc := strings.Replace(validYAML, "  assumptions: Score", "  review_interval: P1Y\n  assumptions: Score", 1)
	doc = strings.Replace(doc, "    rule: {kind: manual}", "    review_interval: P6M2W\n    approval_requires_evidence: false\n    rule: {kind: manual}", 1)
	c, err := catalog.Parse([]byte(doc))
	if err != nil {
		t.Fatal(err)
	}
	m := c.Controls[2]
	if c.Scoring.ReviewInterval != "P1Y" || m.ReviewInterval != "P6M2W" || m.ApprovalRequiresEvidence == nil || *m.ApprovalRequiresEvidence {
		t.Fatalf("review fields = %+v %+v", c.Scoring, m)
	}
	for _, bad := range []string{"P", "1Y", "P1H", "PT1H", "P1D1Y"} {
		d := strings.Replace(validYAML, "    rule: {kind: manual}", "    review_interval: "+bad+"\n    rule: {kind: manual}", 1)
		if _, err := catalog.Parse([]byte(d)); err == nil {
			t.Errorf("review_interval %q must be refused", bad)
		}
	}
}
