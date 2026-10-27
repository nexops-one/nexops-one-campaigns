// SPDX-License-Identifier: Apache-2.0

package catalog

import (
	"encoding/json"
	"sort"
)

// ChangeKind classifies a control change between two catalog versions.
type ChangeKind string

const (
	ChangeAdded   ChangeKind = "added"
	ChangeRemoved ChangeKind = "removed"
	ChangeChanged ChangeKind = "changed"
)

// ControlChange is one control difference. Fields lists changed JSON fields.
type ControlChange struct {
	ControlID string     `json:"control_id"`
	Kind      ChangeKind `json:"kind"`
	Fields    []string   `json:"fields,omitempty"`
}

// Diff compares two catalogs. CatalogFields lists changed top-level fields
// other than version and controls.
type Diff struct {
	From          Ref             `json:"from"`
	To            Ref             `json:"to"`
	CatalogFields []string        `json:"catalog_fields"`
	Controls      []ControlChange `json:"controls"`
}

// DiffCatalogs returns the differences from a to b, sorted by control ID.
func DiffCatalogs(a, b *Catalog) Diff {
	d := Diff{From: a.Ref(), To: b.Ref(), CatalogFields: []string{}, Controls: []ControlChange{}}
	d.CatalogFields = changedFields(header(a), header(b))
	before := map[string]Control{}
	for _, c := range a.Controls {
		before[c.ID] = c
	}
	after := map[string]Control{}
	for _, c := range b.Controls {
		after[c.ID] = c
		old, ok := before[c.ID]
		if !ok {
			d.Controls = append(d.Controls, ControlChange{ControlID: c.ID, Kind: ChangeAdded})
			continue
		}
		if fields := changedFields(old, c); len(fields) > 0 {
			d.Controls = append(d.Controls, ControlChange{ControlID: c.ID, Kind: ChangeChanged, Fields: fields})
		}
	}
	for _, c := range a.Controls {
		if _, ok := after[c.ID]; !ok {
			d.Controls = append(d.Controls, ControlChange{ControlID: c.ID, Kind: ChangeRemoved})
		}
	}
	sort.Slice(d.Controls, func(i, j int) bool { return d.Controls[i].ControlID < d.Controls[j].ControlID })
	return d
}

func header(c *Catalog) Catalog {
	h := *c
	h.Version, h.Controls = "", nil
	return h
}

func changedFields(a, b any) []string {
	fa, fb := jsonFields(a), jsonFields(b)
	keys := map[string]bool{}
	for k := range fa {
		keys[k] = true
	}
	for k := range fb {
		keys[k] = true
	}
	out := []string{}
	for k := range keys {
		if string(fa[k]) != string(fb[k]) {
			out = append(out, k)
		}
	}
	sort.Strings(out)
	return out
}

func jsonFields(v any) map[string]json.RawMessage {
	data, _ := json.Marshal(v)
	out := map[string]json.RawMessage{}
	_ = json.Unmarshal(data, &out)
	return out
}
