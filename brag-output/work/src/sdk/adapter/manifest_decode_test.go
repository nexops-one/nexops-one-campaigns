// SPDX-License-Identifier: Apache-2.0

package adapter_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/nexops-one/compliance-engine/sdk/adapter"
)

func TestDecodeManifest(t *testing.T) {
	m, err := adapter.DecodeManifest(strings.NewReader(`{"name":"vendor-inventory","version":"1.2.0","schema_version":"0.1.0",
		"supplies":{"ict_provider":["provider_id_code","legal_name"]},"modes":["full"]}`))
	if err != nil {
		t.Fatal(err)
	}
	if m.Name != "vendor-inventory" || !m.SupportsMode(adapter.ModeFull) || !m.SuppliesField("ict_provider", "legal_name") {
		t.Fatalf("decoded = %+v", m)
	}
	_, err = adapter.DecodeManifest(strings.NewReader(`{"name":"my adapter","version":"1","schema_version":"0.1.0","supplies":{},"modes":["full"],"extra":1}`))
	var me *adapter.ManifestError
	if !errors.As(err, &me) || len(me.Errors) != 2 {
		t.Fatalf("err = %v", err)
	}
	if !strings.Contains(err.Error(), "name:") || !strings.Contains(err.Error(), "extra:") {
		t.Fatalf("message = %q", err.Error())
	}
}
