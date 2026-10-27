// SPDX-License-Identifier: Apache-2.0

package schema

import (
	"bytes"
	_ "embed"
	"errors"
	"sync"

	"github.com/santhosh-tekuri/jsonschema/v6"
)

// ManifestSchemaID is the $id of the adapter manifest JSON Schema.
const ManifestSchemaID = "https://example.org/compliance-engine/adapter-manifest/1/manifest.schema.json"

// ManifestSchema is the adapter manifest JSON Schema: the language-neutral
// form of the manifest contract (generated copy of schema/manifest.schema.json).
//
//go:embed manifest.schema.json
var ManifestSchema []byte

var (
	manifestOnce      sync.Once
	manifestValidator *jsonschema.Schema
	manifestErr       error
)

func manifestSchema() (*jsonschema.Schema, error) {
	manifestOnce.Do(func() {
		inst, err := jsonschema.UnmarshalJSON(bytes.NewReader(ManifestSchema))
		if err != nil {
			manifestErr = err
			return
		}
		c := jsonschema.NewCompiler()
		if err := c.AddResource(ManifestSchemaID, inst); err != nil {
			manifestErr = err
			return
		}
		manifestValidator, manifestErr = c.Compile(ManifestSchemaID)
	})
	return manifestValidator, manifestErr
}

// ValidateManifestDocument validates a manifest JSON document against
// ManifestSchema. It returns nil when the document is valid. Every error has
// Index -1 and Field set to the JSON path of the offending value.
func ValidateManifestDocument(data []byte) []FieldError {
	v, err := manifestSchema()
	if err != nil {
		return []FieldError{{Index: -1, Code: "invalid_schema", Message: err.Error()}}
	}
	inst, err := jsonschema.UnmarshalJSON(bytes.NewReader(data))
	if err != nil {
		return []FieldError{{Index: -1, Code: "invalid_json", Message: err.Error()}}
	}
	err = v.Validate(inst)
	if err == nil {
		return nil
	}
	var ve *jsonschema.ValidationError
	if !errors.As(err, &ve) {
		return []FieldError{{Index: -1, Code: "invalid", Message: err.Error()}}
	}
	errs := outputErrors("", ve.BasicOutput())
	for i := range errs {
		errs[i].Index = -1
		if errs[i].Code == "unknown_field" {
			errs[i].Message = "field is not defined in the manifest schema"
		}
	}
	return errs
}
