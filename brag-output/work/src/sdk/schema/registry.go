// SPDX-License-Identifier: Apache-2.0

package schema

import (
	"bytes"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"path"
	"sort"
	"strings"
	"sync"

	"github.com/santhosh-tekuri/jsonschema/v6"
)

//go:embed v*/schema.json
var embedded embed.FS

// ErrUnsupportedVersion is returned when no loaded schema matches a version.
var ErrUnsupportedVersion = errors.New("unsupported canonical schema version")

// Schema is one parsed and compiled canonical schema version.
type Schema struct {
	Version    string
	ID         string
	Raw        []byte
	Entities   map[string]*Entity
	names      []string
	validators map[string]*jsonschema.Schema
}

// EntityNames returns the entity names in sorted order.
func (s *Schema) EntityNames() []string { return append([]string(nil), s.names...) }

// Entity returns the descriptor for name.
func (s *Schema) Entity(name string) (*Entity, bool) {
	e, ok := s.Entities[name]
	return e, ok
}

// Field resolves a path "entity.field".
func (s *Schema) Field(p string) (*Entity, *Field, bool) {
	en, fn, ok := strings.Cut(p, ".")
	if !ok {
		return nil, nil, false
	}
	e, ok := s.Entities[en]
	if !ok {
		return nil, nil, false
	}
	f, ok := e.Fields[fn]
	if !ok {
		return nil, nil, false
	}
	return e, f, true
}

// Registry holds every loaded schema version.
type Registry struct {
	byVersion map[string]*Schema
	versions  []Version
}

var (
	defaultOnce sync.Once
	defaultReg  *Registry
	defaultErr  error
)

// Default returns the registry of the schemas embedded in this SDK release.
func Default() (*Registry, error) {
	defaultOnce.Do(func() { defaultReg, defaultErr = Load(embedded) })
	return defaultReg, defaultErr
}

// Load reads every v<semver>/schema.json in fsys.
func Load(fsys fs.FS) (*Registry, error) {
	matches, err := fs.Glob(fsys, "v*/schema.json")
	if err != nil {
		return nil, err
	}
	if len(matches) == 0 {
		return nil, errors.New("no canonical schemas found")
	}
	r := &Registry{byVersion: map[string]*Schema{}}
	for _, m := range matches {
		v, err := ParseVersion(path.Dir(m))
		if err != nil {
			return nil, fmt.Errorf("schema directory %s: %w", m, err)
		}
		raw, err := fs.ReadFile(fsys, m)
		if err != nil {
			return nil, err
		}
		s, err := Parse(v.String(), raw)
		if err != nil {
			return nil, fmt.Errorf("schema %s: %w", v, err)
		}
		r.byVersion[v.String()] = s
		r.versions = append(r.versions, v)
	}
	sort.Slice(r.versions, func(i, j int) bool { return r.versions[i].Less(r.versions[j]) })
	return r, nil
}

// Versions returns the loaded versions in ascending order.
func (r *Registry) Versions() []string {
	out := make([]string, len(r.versions))
	for i, v := range r.versions {
		out[i] = v.String()
	}
	return out
}

// Latest returns the highest loaded version.
func (r *Registry) Latest() *Schema { return r.byVersion[r.versions[len(r.versions)-1].String()] }

// Resolve returns the schema a batch declaring version is validated with: any
// patch of a loaded MAJOR.MINOR resolves to the highest loaded patch of it.
func (r *Registry) Resolve(version string) (*Schema, error) {
	want, err := ParseVersion(version)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrUnsupportedVersion, err)
	}
	var best *Schema
	for _, v := range r.versions {
		if v.Major == want.Major && v.Minor == want.Minor {
			best = r.byVersion[v.String()]
		}
	}
	if best == nil {
		return nil, fmt.Errorf("%w: %s (supported: %s)", ErrUnsupportedVersion, version, strings.Join(r.Versions(), ", "))
	}
	return best, nil
}

// Match returns the highest loaded schema satisfying constraint (see Version.Satisfies).
func (r *Registry) Match(constraint string) (*Schema, error) {
	var best *Schema
	for _, v := range r.versions {
		ok, err := v.Satisfies(constraint)
		if err != nil {
			return nil, fmt.Errorf("%w: %v", ErrUnsupportedVersion, err)
		}
		if ok {
			best = r.byVersion[v.String()]
		}
	}
	if best == nil {
		return nil, fmt.Errorf("%w: no loaded schema satisfies %q (loaded: %s)", ErrUnsupportedVersion, constraint, strings.Join(r.Versions(), ", "))
	}
	return best, nil
}

type rawSchema struct {
	ID         string               `json:"$id"`
	Defs       map[string]rawEntity `json:"$defs"`
	Properties struct {
		Entities struct {
			Properties map[string]json.RawMessage `json:"properties"`
		} `json:"entities"`
	} `json:"properties"`
}

type rawEntity struct {
	Title       string              `json:"title"`
	Template    string              `json:"x-roi-template"`
	IdentityKey []string            `json:"x-identity-key"`
	Properties  map[string]rawField `json:"properties"`
}

type rawField struct {
	Type           json.RawMessage `json:"type"`
	Format         string          `json:"format"`
	Pattern        string          `json:"pattern"`
	Enum           []string        `json:"enum"`
	Description    string          `json:"description"`
	RoIRef         string          `json:"x-roi-ref"`
	References     string          `json:"x-references"`
	Key            string          `json:"x-key"`
	Codelist       string          `json:"x-codelist"`
	RoIRequired    bool            `json:"x-roi-required"`
	RoIConditional bool            `json:"x-roi-conditional"`
}

// Parse builds descriptors from a canonical schema document and compiles one
// validator per entity.
func Parse(version string, raw []byte) (*Schema, error) {
	var doc rawSchema
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, fmt.Errorf("decode: %w", err)
	}
	if doc.ID == "" {
		return nil, errors.New("schema has no $id")
	}
	s := &Schema{Version: version, ID: doc.ID, Raw: raw, Entities: map[string]*Entity{}, validators: map[string]*jsonschema.Schema{}}
	for name := range doc.Properties.Entities.Properties {
		def, ok := doc.Defs[name]
		if !ok {
			return nil, fmt.Errorf("entity %s has no definition", name)
		}
		e, err := newEntity(name, def)
		if err != nil {
			return nil, err
		}
		s.Entities[name] = e
		s.names = append(s.names, name)
	}
	sort.Strings(s.names)
	instance, err := jsonschema.UnmarshalJSON(bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}
	c := jsonschema.NewCompiler()
	c.AssertFormat()
	if err := c.AddResource(doc.ID, instance); err != nil {
		return nil, err
	}
	for _, name := range s.names {
		v, err := c.Compile(doc.ID + "#/$defs/" + name)
		if err != nil {
			return nil, fmt.Errorf("compile %s: %w", name, err)
		}
		s.validators[name] = v
	}
	return s, nil
}

func newEntity(name string, def rawEntity) (*Entity, error) {
	e := &Entity{Name: name, Title: def.Title, Template: def.Template, IdentityKey: def.IdentityKey, Fields: map[string]*Field{}}
	if len(e.IdentityKey) == 0 {
		return nil, fmt.Errorf("entity %s has no x-identity-key", name)
	}
	for fname, rf := range def.Properties {
		if fname == MetaField {
			continue
		}
		var typ string
		if len(rf.Type) > 0 {
			if err := json.Unmarshal(rf.Type, &typ); err != nil {
				return nil, fmt.Errorf("%s.%s: unsupported type %s", name, fname, rf.Type)
			}
		}
		e.Fields[fname] = &Field{
			Name: fname, Type: typ, Format: rf.Format, Pattern: rf.Pattern, Enum: rf.Enum,
			Description: rf.Description, RoIRef: rf.RoIRef, References: rf.References, Key: rf.Key,
			RoIRequired: rf.RoIRequired, RoIConditional: rf.RoIConditional, Codelist: rf.Codelist,
			Kind: kindFor(rf.Pattern),
		}
		e.FieldNames = append(e.FieldNames, fname)
	}
	sort.Strings(e.FieldNames)
	for _, k := range e.IdentityKey {
		if _, ok := e.Fields[k]; !ok {
			return nil, fmt.Errorf("entity %s identity field %s is not defined", name, k)
		}
	}
	return e, nil
}
