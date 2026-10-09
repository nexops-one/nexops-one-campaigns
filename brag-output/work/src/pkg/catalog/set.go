// SPDX-License-Identifier: Apache-2.0

package catalog

import (
	"errors"
	"fmt"
	"sort"

	"github.com/nexops-one/compliance-engine/sdk/schema"
)

// ErrUnknownCatalog is returned for a reference that is not loaded.
var ErrUnknownCatalog = errors.New("unknown catalog")

// Set is a validated collection of catalogs.
type Set struct {
	byRef map[Ref]*Catalog
	refs  []Ref
}

// NewSet validates every catalog and rejects duplicate references.
func NewSet(reg *schema.Registry, cs []*Catalog) (*Set, error) {
	s := &Set{byRef: map[Ref]*Catalog{}}
	for _, c := range cs {
		if _, err := Validate(c, reg); err != nil {
			return nil, err
		}
		if _, dup := s.byRef[c.Ref()]; dup {
			return nil, fmt.Errorf("catalog %s is loaded twice", c.Ref())
		}
		s.byRef[c.Ref()] = c
		s.refs = append(s.refs, c.Ref())
	}
	sort.Slice(s.refs, func(i, j int) bool { return refLess(s.refs[i], s.refs[j]) })
	return s, nil
}

func refLess(a, b Ref) bool {
	if a.Catalog != b.Catalog {
		return a.Catalog < b.Catalog
	}
	va, _ := schema.ParseVersion(a.Version)
	vb, _ := schema.ParseVersion(b.Version)
	return va.Less(vb)
}

// Refs returns every loaded reference, by catalog name then ascending version.
func (s *Set) Refs() []Ref { return append([]Ref(nil), s.refs...) }

// Get returns the catalog for ref.
func (s *Set) Get(ref Ref) (*Catalog, bool) {
	c, ok := s.byRef[ref]
	return c, ok
}

// Latest returns the highest version of each catalog, sorted by name.
func (s *Set) Latest() []*Catalog {
	var out []*Catalog
	for i, r := range s.refs {
		if i+1 < len(s.refs) && s.refs[i+1].Catalog == r.Catalog {
			continue
		}
		out = append(out, s.byRef[r])
	}
	return out
}

// Resolve returns the catalogs for refs; no refs means Latest.
func (s *Set) Resolve(refs []Ref) ([]*Catalog, error) {
	if len(refs) == 0 {
		return s.Latest(), nil
	}
	out := make([]*Catalog, 0, len(refs))
	for _, r := range refs {
		c, ok := s.byRef[r]
		if !ok {
			return nil, fmt.Errorf("%w: %s", ErrUnknownCatalog, r)
		}
		out = append(out, c)
	}
	return out, nil
}
