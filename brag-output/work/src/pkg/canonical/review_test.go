// SPDX-License-Identifier: Apache-2.0

package canonical_test

import (
	"testing"

	"github.com/nexops-one/compliance-engine/pkg/canonical"
	"github.com/nexops-one/compliance-engine/sdk/adapter"
)

func TestEmptyObjectsAndArraysAreMissing(t *testing.T) {
	v := canonical.NewView(adapter.Record{"sovereignty_indicators": map[string]any{}, "list": []any{}, "full": map[string]any{"a": "b"}})
	if got := v.State("sovereignty_indicators"); got != canonical.StateMissing {
		t.Errorf("empty object = %s, want missing", got)
	}
	if got := v.State("list"); got != canonical.StateMissing {
		t.Errorf("empty array = %s, want missing", got)
	}
	if got := v.State("full"); got != canonical.StateProvided {
		t.Errorf("non-empty object = %s, want provided", got)
	}
}
