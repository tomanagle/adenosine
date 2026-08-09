// Package migrations exposes the ordered SQL migrations embedded in the binary.
package migrations

import "embed"

// Files contains all release migrations. Migration files are immutable once
// released; schema changes must be introduced in a new ordered file.
//
//go:embed *.sql
var Files embed.FS
