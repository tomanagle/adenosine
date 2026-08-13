// Package owner resolves the shared, human-readable namespace used by profile,
// organization, and repository URLs.
package owner

import (
	"context"
	"errors"
)

var ErrNotFound = errors.New("owner not found")

type Kind string

const (
	KindAccount      Kind = "account"
	KindOrganization Kind = "organization"
)

// Owner identifies the canonical resource behind a public owner name.
type Owner struct {
	Kind             Kind
	CanonicalName    string
	AccountDID       string
	OrganizationSlug string
}

// Resolver resolves a case-insensitive owner name without probing resource APIs.
type Resolver interface {
	Resolve(context.Context, string) (Owner, error)
}
