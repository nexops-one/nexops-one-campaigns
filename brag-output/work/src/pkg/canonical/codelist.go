// SPDX-License-Identifier: Apache-2.0

package canonical

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"path"
	"sort"
)

// CodeStatus is the result of checking a coded value.
type CodeStatus string

const (
	CodeValid      CodeStatus = "valid"
	CodeInvalid    CodeStatus = "invalid"
	CodeUnverified CodeStatus = "codelist_unverified"
)

// Codelist is one versioned closed value list.
type Codelist struct {
	Name    string      `json:"codelist"`
	Version string      `json:"version"`
	Values  []CodeValue `json:"values"`
}

// CodeValue is one allowed code.
type CodeValue struct {
	Code  string `json:"code"`
	Label string `json:"label,omitempty"`
}

// Codelists holds loaded codelists by name.
type Codelists struct{ sets map[string]map[string]bool }

// LoadCodelists reads dir/*.json from fsys. Other files are ignored.
func LoadCodelists(fsys fs.FS, dir string) (*Codelists, error) {
	c := &Codelists{sets: map[string]map[string]bool{}}
	paths, err := fs.Glob(fsys, path.Join(dir, "*.json"))
	if err != nil {
		return nil, err
	}
	for _, p := range paths {
		data, err := fs.ReadFile(fsys, p)
		if err != nil {
			return nil, err
		}
		var cl Codelist
		if err := json.Unmarshal(data, &cl); err != nil {
			return nil, fmt.Errorf("codelist %s: %w", p, err)
		}
		if cl.Name == "" || len(cl.Values) == 0 {
			return nil, fmt.Errorf("codelist %s: codelist name and at least one value are required", p)
		}
		if _, dup := c.sets[cl.Name]; dup {
			return nil, fmt.Errorf("codelist %s: duplicate codelist %q", p, cl.Name)
		}
		set := map[string]bool{}
		for _, v := range cl.Values {
			set[v.Code] = true
		}
		c.sets[cl.Name] = set
	}
	return c, nil
}

// Check returns the status of value in codelist name.
func (c *Codelists) Check(name, value string) CodeStatus {
	if c == nil {
		return CodeUnverified
	}
	set, ok := c.sets[name]
	if !ok {
		return CodeUnverified
	}
	if set[value] {
		return CodeValid
	}
	return CodeInvalid
}

// Names returns the loaded codelist names, sorted.
func (c *Codelists) Names() []string {
	out := []string{}
	if c == nil {
		return out
	}
	for n := range c.sets {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}
