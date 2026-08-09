// Package lexicons exposes Adenosine's implementation-independent AT Protocol schemas.
package lexicons

import "embed"

// Files contains the published Adenosine Lexicon documents.
//
//go:embed *.json
var Files embed.FS
