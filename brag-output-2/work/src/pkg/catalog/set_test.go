// SPDX-License-Identifier: Apache-2.0

package catalog_test

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/nexops-one/compliance-engine/pkg/catalog"
)

func version(doc, v string) string {
	return strings.Replace(doc, "version: 1.0.0", "version: "+v, 1)
}

func TestFSSourceAndSet(t *testing.T) {
	fsys := fstest.MapFS{
		"test/1.0.0.yaml": {Data: []byte(validYAML)},
		"test/1.1.0.yaml": {Data: []byte(version(validYAML, "1.1.0"))},
		"test/README.md":  {Data: []byte("ignored")},
	}
	cs, err := catalog.FSSource{FS: fsys}.Catalogs(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	set, err := catalog.NewSet(registry(t), cs)
	if err != nil {
		t.Fatal(err)
	}
	want := []catalog.Ref{{Catalog: "test", Version: "1.0.0"}, {Catalog: "test", Version: "1.1.0"}}
	if !reflect.DeepEqual(set.Refs(), want) {
		t.Fatalf("refs = %v", set.Refs())
	}
	latest := set.Latest()
	if len(latest) != 1 || latest[0].Version != "1.1.0" {
		t.Fatalf("latest = %v", latest)
	}
	got, err := set.Resolve([]catalog.Ref{{Catalog: "test", Version: "1.0.0"}})
	if err != nil || got[0].Version != "1.0.0" {
		t.Fatalf("resolve = %v, %v", got, err)
	}
	if got, _ := set.Resolve(nil); len(got) != 1 || got[0].Version != "1.1.0" {
		t.Fatalf("resolve(nil) = %v", got)
	}
	if _, err := set.Resolve([]catalog.Ref{{Catalog: "test", Version: "9.9.9"}}); !errors.Is(err, catalog.ErrUnknownCatalog) {
		t.Fatalf("unknown ref err = %v", err)
	}
}

func TestFSSourceRejectsMisnamedFile(t *testing.T) {
	fsys := fstest.MapFS{"test/2.0.0.yaml": {Data: []byte(validYAML)}}
	if _, err := (catalog.FSSource{FS: fsys}).Catalogs(context.Background()); err == nil {
		t.Fatal("file name must match catalog and version")
	}
}

func TestNewSetRejectsDuplicatesAndInvalid(t *testing.T) {
	a := mustParse(t, validYAML)
	b := mustParse(t, validYAML)
	if _, err := catalog.NewSet(registry(t), []*catalog.Catalog{a, b}); err == nil {
		t.Fatal("duplicate refs must fail")
	}
	bad := mustParse(t, strings.Replace(validYAML, "entity: ict_provider,", "entity: nope,", 1))
	if _, err := catalog.NewSet(registry(t), []*catalog.Catalog{bad}); err == nil {
		t.Fatal("invalid catalog must fail")
	}
}

func TestSourcesConcatenate(t *testing.T) {
	one := catalog.FSSource{FS: fstest.MapFS{"test/1.0.0.yaml": {Data: []byte(validYAML)}}}
	two := catalog.FSSource{FS: fstest.MapFS{"test/1.1.0.yaml": {Data: []byte(version(validYAML, "1.1.0"))}}}
	cs, err := catalog.Sources{one, two}.Catalogs(context.Background())
	if err != nil || len(cs) != 2 {
		t.Fatalf("got %d catalogs, %v", len(cs), err)
	}
}
