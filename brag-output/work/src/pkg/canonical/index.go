// SPDX-License-Identifier: Apache-2.0

package canonical

import (
	"sort"
	"strings"

	"github.com/nexops-one/compliance-engine/sdk/adapter"
	"github.com/nexops-one/compliance-engine/sdk/schema"
)

// SnapshotRecord is one record of a snapshot.
type SnapshotRecord struct {
	Entity string
	Key    string
	Data   adapter.Record
}

// Index gives analysis and evaluation fast access to a snapshot. It is
// immutable after construction and safe for concurrent reads.
type Index struct {
	schema   *schema.Schema
	byEntity map[string][]SnapshotRecord
	values   map[string]map[string]bool // "entity.field" -> values present
}

// NewIndex indexes recs. Value sets are built for every x-references target.
func NewIndex(s *schema.Schema, recs []SnapshotRecord) *Index {
	ix := &Index{schema: s, byEntity: map[string][]SnapshotRecord{}, values: map[string]map[string]bool{}}
	for _, r := range recs {
		ix.byEntity[r.Entity] = append(ix.byEntity[r.Entity], r)
	}
	for _, list := range ix.byEntity {
		sort.Slice(list, func(i, j int) bool { return list[i].Key < list[j].Key })
	}
	for _, en := range s.EntityNames() {
		e, _ := s.Entity(en)
		for _, fn := range e.FieldNames {
			target := e.Fields[fn].References
			if target == "" || ix.values[target] != nil {
				continue
			}
			te, tf, _ := strings.Cut(target, ".")
			set := map[string]bool{}
			for _, r := range ix.byEntity[te] {
				if v, ok := ScalarString(r.Data[tf]); ok {
					set[v] = true
				}
			}
			ix.values[target] = set
		}
	}
	return ix
}

// Schema returns the schema the index was built with.
func (ix *Index) Schema() *schema.Schema { return ix.schema }

// Records returns the records of entity sorted by key.
func (ix *Index) Records(entity string) []SnapshotRecord { return ix.byEntity[entity] }

// HasValue reports whether some record has value at path "entity.field".
// Only x-references targets are indexed.
func (ix *Index) HasValue(path, value string) bool { return ix.values[path][value] }
