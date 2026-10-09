// SPDX-License-Identifier: Apache-2.0

package canonical_test

import (
	"reflect"
	"testing"
	"testing/fstest"

	"github.com/nexops-one/compliance-engine/pkg/canonical"
	schemadata "github.com/nexops-one/compliance-engine/schema"
)

func TestCodelists(t *testing.T) {
	fsys := fstest.MapFS{
		"v0.1.0/codelists/entity_type.json": {Data: []byte(`{"codelist":"entity_type","version":"2025-04-28","values":[{"code":"eba_CT:x12","label":"Credit institution"}]}`)},
		"v0.1.0/codelists/README.md":        {Data: []byte("ignored")},
	}
	c, err := canonical.LoadCodelists(fsys, "v0.1.0/codelists")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(c.Names(), []string{"entity_type"}) {
		t.Fatalf("names = %v", c.Names())
	}
	if c.Check("entity_type", "eba_CT:x12") != canonical.CodeValid {
		t.Error("known code should be valid")
	}
	if c.Check("entity_type", "nope") != canonical.CodeInvalid {
		t.Error("unknown code should be invalid")
	}
	if c.Check("person_type", "anything") != canonical.CodeUnverified {
		t.Error("unloaded codelist should be unverified")
	}
	var none *canonical.Codelists
	if none.Check("entity_type", "x") != canonical.CodeUnverified {
		t.Error("nil codelists should be unverified")
	}
}

func TestLoadCodelistsRejectsBadFiles(t *testing.T) {
	cases := map[string]string{
		"no name":   `{"version":"1","values":[{"code":"a"}]}`,
		"no values": `{"codelist":"x","version":"1","values":[]}`,
		"bad json":  `{`,
	}
	for name, data := range cases {
		fsys := fstest.MapFS{"cl/x.json": {Data: []byte(data)}}
		if _, err := canonical.LoadCodelists(fsys, "cl"); err == nil {
			t.Errorf("%s: expected error", name)
		}
	}
	dup := fstest.MapFS{
		"cl/a.json": {Data: []byte(`{"codelist":"x","version":"1","values":[{"code":"a"}]}`)},
		"cl/b.json": {Data: []byte(`{"codelist":"x","version":"1","values":[{"code":"b"}]}`)},
	}
	if _, err := canonical.LoadCodelists(dup, "cl"); err == nil {
		t.Error("duplicate codelist names must fail")
	}
}

func TestEmbeddedCodelistsLoad(t *testing.T) {
	c, err := canonical.LoadCodelists(schemadata.Codelists, "v0.1.0/codelists")
	if err != nil {
		t.Fatal(err)
	}
	if len(c.Names()) != 0 {
		t.Fatalf("no official codelists are shipped yet, got %v", c.Names())
	}
}
