// SPDX-License-Identifier: Apache-2.0

package catalog_test

import (
	"reflect"
	"strings"
	"testing"

	"github.com/nexops-one/compliance-engine/pkg/catalog"
)

func TestDiffCatalogs(t *testing.T) {
	a := mustParse(t, validYAML)
	next := version(validYAML, "1.1.0")
	next = strings.Replace(next, "title: Providers are named", "title: Providers are legally named", 1)
	next = strings.Replace(next, "accept_derived: true", "accept_derived: false", 1)
	next = strings.Replace(next, "  - id: test-manual", "  - id: test-new", 1)
	next = strings.Replace(next, `source_authority: "Test regulation (indicative)"`, `source_authority: "Test regulation v2 (indicative)"`, 1)
	b := mustParse(t, next)

	d := catalog.DiffCatalogs(a, b)
	if d.From.Version != "1.0.0" || d.To.Version != "1.1.0" {
		t.Fatalf("refs = %v -> %v", d.From, d.To)
	}
	if !reflect.DeepEqual(d.CatalogFields, []string{"source_authority"}) {
		t.Fatalf("catalog fields = %v", d.CatalogFields)
	}
	want := []catalog.ControlChange{
		{ControlID: "test-exit-plans", Kind: catalog.ChangeChanged, Fields: []string{"accept_derived"}},
		{ControlID: "test-manual", Kind: catalog.ChangeRemoved},
		{ControlID: "test-new", Kind: catalog.ChangeAdded},
		{ControlID: "test-provider-names", Kind: catalog.ChangeChanged, Fields: []string{"title"}},
	}
	if !reflect.DeepEqual(d.Controls, want) {
		t.Fatalf("controls = %+v", d.Controls)
	}
	if same := catalog.DiffCatalogs(a, a); len(same.Controls) != 0 || len(same.CatalogFields) != 0 {
		t.Fatalf("self diff = %+v", same)
	}
}
