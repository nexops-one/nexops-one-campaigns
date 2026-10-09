// SPDX-License-Identifier: Apache-2.0

package adapter

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/nexops-one/compliance-engine/sdk/schema"
)

// ManifestError lists the ways a manifest document violates the manifest JSON Schema.
type ManifestError struct {
	Errors []schema.FieldError
}

func (e *ManifestError) Error() string {
	parts := make([]string, len(e.Errors))
	for i, fe := range e.Errors {
		parts[i] = fmt.Sprintf("%s: %s (%s)", fe.Field, fe.Message, fe.Code)
	}
	return "manifest: " + strings.Join(parts, "; ")
}

// DecodeManifest reads a manifest JSON document, validates it against the
// manifest JSON Schema and decodes it. Checks that need the canonical schema
// (entities, fields, identity fields) are Manifest.Validate.
func DecodeManifest(r io.Reader) (Manifest, error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return Manifest{}, fmt.Errorf("read manifest: %w", err)
	}
	if errs := schema.ValidateManifestDocument(data); len(errs) > 0 {
		return Manifest{}, &ManifestError{Errors: errs}
	}
	var m Manifest
	if err := json.Unmarshal(data, &m); err != nil {
		return Manifest{}, fmt.Errorf("decode manifest: %w", err)
	}
	return m, nil
}
