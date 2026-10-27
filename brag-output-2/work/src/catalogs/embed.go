// SPDX-License-Identifier: Apache-2.0

// Package catalogs holds the control catalogs shipped with the open core and
// the meta-schema every catalog file must satisfy.
package catalogs

import "embed"

// FS holds the catalog files as <catalog>/<version>.yaml.
//
//go:embed */*.yaml
var FS embed.FS

// MetaSchema is the JSON Schema for catalog files.
//
//go:embed meta-schema.json
var MetaSchema []byte
