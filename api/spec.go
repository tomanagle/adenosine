// Package api exposes the public API specification embedded in the binary.
package api

import _ "embed"

// OpenAPI contains the canonical OpenAPI document. JSON is valid YAML, so the
// same release-pinned bytes are served for both representations.
//
//go:embed openapi.yaml
var OpenAPI []byte
