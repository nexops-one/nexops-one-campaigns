// SPDX-License-Identifier: Apache-2.0

package catalog

import (
	"context"
	"fmt"
	"io/fs"
	"path"
	"sort"

	"github.com/nexops-one/compliance-engine/catalogs"
)

// Embedded returns the catalogs shipped with the engine.
func Embedded() Source { return FSSource{FS: catalogs.FS, Origin: "embedded"} }

// Source supplies catalogs. The enterprise edition adds maintained catalogs
// through its own Source.
type Source interface {
	Catalogs(ctx context.Context) ([]*Catalog, error)
}

// FSSource reads <catalog>/<version>.yaml files from FS. Origin and Feature
// are set on every catalog it returns.
type FSSource struct {
	FS      fs.FS
	Origin  string
	Feature string
}

// Catalogs parses every catalog file in FS.
func (s FSSource) Catalogs(ctx context.Context) ([]*Catalog, error) {
	paths, err := fs.Glob(s.FS, "*/*.yaml")
	if err != nil {
		return nil, err
	}
	sort.Strings(paths)
	out := make([]*Catalog, 0, len(paths))
	for _, p := range paths {
		data, err := fs.ReadFile(s.FS, p)
		if err != nil {
			return nil, err
		}
		c, err := Parse(data)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", p, err)
		}
		if want := path.Join(c.Catalog, c.Version+".yaml"); p != want {
			return nil, fmt.Errorf("%s: file must be named %s to match its content", p, want)
		}
		c.Origin, c.Feature = s.Origin, s.Feature
		out = append(out, c)
	}
	return out, nil
}

// Sources concatenates several sources.
type Sources []Source

// Catalogs returns the catalogs of every source, in order.
func (ss Sources) Catalogs(ctx context.Context) ([]*Catalog, error) {
	var out []*Catalog
	for _, s := range ss {
		cs, err := s.Catalogs(ctx)
		if err != nil {
			return nil, err
		}
		out = append(out, cs...)
	}
	return out, nil
}
