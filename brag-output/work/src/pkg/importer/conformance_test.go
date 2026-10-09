// SPDX-License-Identifier: Apache-2.0

package importer_test

import (
	"testing"

	"github.com/nexops-one/compliance-engine/pkg/importer"
	"github.com/nexops-one/compliance-engine/sdk/adapter"
	"github.com/nexops-one/compliance-engine/sdk/adaptertest"
)

func TestImporterBatchesConform(t *testing.T) {
	reg := registry(t)
	s := reg.Latest()
	book, err := importer.XLSXTemplate(s)
	if err != nil {
		t.Fatal(err)
	}
	csv := []byte("provider_id_code,legal_name,hq_country\nP1,One Ltd,IE\nP2,Two Inc,US\n")
	for _, in := range []importer.Input{
		{Name: "ict_provider.csv", Data: csv},
		{Name: "ict_provider.csv", Data: csv, Mode: adapter.ModeFull},
		{Name: "register.xlsx", Data: book, Mode: adapter.ModeFull},
	} {
		p, err := importer.Parse(s, in)
		if err != nil {
			t.Fatalf("%s: %v", in.Name, err)
		}
		fs := append(adaptertest.ValidateManifest(reg, p.Manifest), adaptertest.ValidateBatch(reg, p.Manifest, p.Batch, nil)...)
		if failures := adaptertest.Failures(fs); len(failures) > 0 {
			t.Errorf("%s (%s): %v", in.Name, p.Batch.Mode(), failures)
		}
	}
}
