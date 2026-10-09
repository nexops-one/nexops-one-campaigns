// SPDX-License-Identifier: Apache-2.0

// Package api holds the OpenAPI definition of the REST API.
package api

import _ "embed"

// OpenAPI is the OpenAPI 3.1 definition served at /api/v1/openapi.yaml.
//
//go:embed openapi.yaml
var OpenAPI []byte
