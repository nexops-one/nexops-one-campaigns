// SPDX-License-Identifier: Apache-2.0

package catalog

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/santhosh-tekuri/jsonschema/v6"
	"gopkg.in/yaml.v3"

	"github.com/nexops-one/compliance-engine/catalogs"
)

const metaSchemaID = "https://github.com/nexops-one/compliance-engine/catalogs/meta-schema.json"

var (
	metaOnce   sync.Once
	metaSchema *jsonschema.Schema
	metaErr    error
)

func compiledMetaSchema() (*jsonschema.Schema, error) {
	metaOnce.Do(func() {
		doc, err := jsonschema.UnmarshalJSON(bytes.NewReader(catalogs.MetaSchema))
		if err != nil {
			metaErr = err
			return
		}
		c := jsonschema.NewCompiler()
		c.AssertFormat()
		if metaErr = c.AddResource(metaSchemaID, doc); metaErr != nil {
			return
		}
		metaSchema, metaErr = c.Compile(metaSchemaID)
	})
	return metaSchema, metaErr
}

// Parse decodes a YAML catalog and validates it against the meta-schema.
// Semantic validation against the canonical schema is done by Validate.
func Parse(data []byte) (*Catalog, error) {
	var doc any
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("decode catalog: %w", err)
	}
	js, err := json.Marshal(normalizeYAML(doc))
	if err != nil {
		return nil, fmt.Errorf("decode catalog: %w", err)
	}
	meta, err := compiledMetaSchema()
	if err != nil {
		return nil, fmt.Errorf("catalog meta-schema: %w", err)
	}
	inst, err := jsonschema.UnmarshalJSON(bytes.NewReader(js))
	if err != nil {
		return nil, err
	}
	if err := meta.Validate(inst); err != nil {
		return nil, fmt.Errorf("catalog does not match the meta-schema: %w", err)
	}
	var c Catalog
	if err := json.Unmarshal(js, &c); err != nil {
		return nil, fmt.Errorf("decode catalog: %w", err)
	}
	return &c, nil
}

// normalizeYAML makes yaml.v3 output JSON-compatible. yaml.v3 turns unquoted
// dates into time.Time; midnight UTC values become "YYYY-MM-DD" again.
func normalizeYAML(v any) any {
	switch t := v.(type) {
	case map[string]any:
		for k, x := range t {
			t[k] = normalizeYAML(x)
		}
		return t
	case []any:
		for i, x := range t {
			t[i] = normalizeYAML(x)
		}
		return t
	case time.Time:
		if t.Equal(t.Truncate(24*time.Hour)) && t.Location() == time.UTC {
			return t.Format("2006-01-02")
		}
		return t.Format(time.RFC3339)
	}
	return v
}
