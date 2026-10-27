// SPDX-License-Identifier: Apache-2.0

package adapter_test

import (
	"testing"

	"github.com/nexops-one/compliance-engine/sdk/adapter"
)

func TestValidateEnvelopeRejectsNUL(t *testing.T) {
	s := registry(t).Latest()
	b := adapter.Batch{
		SchemaVersion: "0.1.0", Batch: &adapter.BatchInfo{BatchID: "b\x00"},
		Source:   adapter.Source{System: "s\x00", Adapter: "a", AdapterVersion: "1"},
		Entities: map[string][]adapter.Record{},
	}
	codes := map[string]string{}
	for _, e := range b.ValidateEnvelope(s) {
		codes[e.Field] = e.Code
	}
	if codes["source.system"] != "invalid_character" || codes["batch.batch_id"] != "invalid_character" {
		t.Fatalf("errors = %v", codes)
	}
}
