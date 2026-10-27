// SPDX-License-Identifier: Apache-2.0

package canonical

import (
	"slices"
	"sort"
	"strings"
)

// Dangling is a reference whose target value does not exist in the snapshot.
type Dangling struct {
	Entity string `json:"entity"`
	Key    string `json:"key"`
	Field  string `json:"field"`
	Target string `json:"target"`
	Value  string `json:"value"`
}

// Cycle is a loop in a self-referencing hierarchy (parent_lei,
// parent_id_code, overarching_arrangement_ref). Members are the identifying
// values in the loop, rotated to start with the smallest.
type Cycle struct {
	Entity  string   `json:"entity"`
	Field   string   `json:"field"`
	Members []string `json:"members"`
}

// ReferenceReport is the L2 result for a snapshot.
type ReferenceReport struct {
	Dangling []Dangling `json:"dangling"`
	Cycles   []Cycle    `json:"cycles"`
	dangling map[string]bool
}

// IsDangling reports whether entity/key/field holds a dangling reference.
func (r ReferenceReport) IsDangling(entity, key, field string) bool {
	return r.dangling[entity+"\x00"+key+"\x00"+field]
}

// CheckReferences resolves every x-references field and detects cycles.
// Missing values are not dangling; they are reported by completeness.
func CheckReferences(ix *Index) ReferenceReport {
	rep := ReferenceReport{Dangling: []Dangling{}, Cycles: []Cycle{}, dangling: map[string]bool{}}
	s := ix.Schema()
	for _, en := range s.EntityNames() {
		e, _ := s.Entity(en)
		for _, fn := range e.FieldNames {
			target := e.Fields[fn].References
			if target == "" {
				continue
			}
			for _, rec := range ix.Records(en) {
				v, ok := ScalarString(rec.Data[fn])
				if !ok || v == "" || ix.HasValue(target, v) {
					continue
				}
				rep.Dangling = append(rep.Dangling, Dangling{Entity: en, Key: rec.Key, Field: fn, Target: target, Value: v})
				rep.dangling[en+"\x00"+rec.Key+"\x00"+fn] = true
			}
			if te, tf, _ := strings.Cut(target, "."); te == en {
				rep.Cycles = append(rep.Cycles, findCycles(ix, en, fn, tf)...)
			}
		}
	}
	return rep
}

func findCycles(ix *Index, entity, field, idField string) []Cycle {
	parent := map[string]string{}
	for _, r := range ix.Records(entity) {
		id, ok1 := ScalarString(r.Data[idField])
		p, ok2 := ScalarString(r.Data[field])
		if ok1 && ok2 && id != "" && p != "" {
			parent[id] = p
		}
	}
	ids := make([]string, 0, len(parent))
	for id := range parent {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	done := map[string]bool{}
	var cycles []Cycle
	for _, start := range ids {
		pos := map[string]int{}
		var path []string
		for cur := start; !done[cur]; {
			if i, seen := pos[cur]; seen {
				cycles = append(cycles, Cycle{Entity: entity, Field: field, Members: rotateToMin(path[i:])})
				break
			}
			pos[cur] = len(path)
			path = append(path, cur)
			next, ok := parent[cur]
			if !ok {
				break
			}
			cur = next
		}
		for _, p := range path {
			done[p] = true
		}
	}
	return cycles
}

func rotateToMin(members []string) []string {
	i := slices.Index(members, slices.Min(members))
	return append(append([]string{}, members[i:]...), members[:i]...)
}
