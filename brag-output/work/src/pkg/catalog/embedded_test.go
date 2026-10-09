// SPDX-License-Identifier: Apache-2.0

package catalog_test

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/nexops-one/compliance-engine/pkg/catalog"
)

func TestEmbeddedCatalogsLoadAndValidate(t *testing.T) {
	cs, err := catalog.Embedded().Catalogs(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	set, err := catalog.NewSet(registry(t), cs)
	if err != nil {
		t.Fatal(err)
	}
	want := []catalog.Ref{{Catalog: "aiact", Version: "1.0.0"}, {Catalog: "dora", Version: "1.0.0"}, {Catalog: "gdpr", Version: "1.0.0"}}
	if !reflect.DeepEqual(set.Refs(), want) {
		t.Fatalf("refs = %v", set.Refs())
	}
	dora, _ := set.Get(catalog.Ref{Catalog: "dora", Version: "1.0.0"})
	var ids []string
	for _, c := range dora.Controls {
		ids = append(ids, c.ID)
	}
	wantIDs := []string{
		"dora-roi-reporting-entity", "dora-roi-provider-identification", "dora-roi-ultimate-parent",
		"dora-roi-data-location", "dora-roi-arrangement-dates", "dora-roi-signatories",
		"dora-supply-chain-visibility", "dora-function-criticality", "dora-substitutability-assessed",
		"dora-exit-plans", "dora-incident-readiness",
	}
	if !reflect.DeepEqual(ids, wantIDs) {
		t.Fatalf("dora controls = %v", ids)
	}
	for _, c := range cs {
		if !strings.Contains(c.SourceAuthority, "indicative") {
			t.Errorf("%s: catalog source authority must be marked indicative", c.Ref())
		}
		for _, ctl := range c.Controls {
			if !strings.Contains(ctl.SourceAuthority, "indicative") {
				t.Errorf("%s/%s: control source authority must be marked indicative", c.Ref(), ctl.ID)
			}
		}
	}
}
