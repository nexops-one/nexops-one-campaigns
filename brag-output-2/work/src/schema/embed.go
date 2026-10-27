// SPDX-License-Identifier: Apache-2.0

// Package schemadata holds the source copies of the canonical schemas and the
// versioned codelists. The Go SDK embeds generated copies of the schemas and
// the Python SDK ships them as package data; run `go generate ./schema` (or
// `go run ./internal/tools/syncschema` from the repository root) after
// editing a schema here.
package schemadata

//go:generate go run -C .. ./internal/tools/syncschema

import "embed"

// Codelists holds codelist files under <version>/codelists/.
//
//go:embed v0.1.0/codelists
var Codelists embed.FS

// Sensitive holds <version>/sensitive.json, the canonical fields encrypted at rest.
//
//go:embed v0.1.0/sensitive.json
var Sensitive embed.FS
