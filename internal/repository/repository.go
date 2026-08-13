// Package repository contains local repository lifecycle behavior.
package repository

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
)

var (
	// ErrNotFound indicates that a repository does not exist locally.
	ErrNotFound = errors.New("repository not found")
	// ErrAlreadyExists indicates that an active owner/slug route already exists.
	ErrAlreadyExists = errors.New("repository already exists")
	// ErrValidation indicates invalid repository input.
	ErrValidation = errors.New("repository validation failed")

	slugPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]*$`)
)

// ID is an immutable local repository identifier.
type ID uuid.UUID

// String returns the canonical UUID representation.
func (id ID) String() string {
	return uuid.UUID(id).String()
}

// Visibility controls who may read a repository.
type Visibility string

const (
	VisibilityPublic  Visibility = "public"
	VisibilityPrivate Visibility = "private"
)

// State records progress across database and filesystem operations.
type State string

const (
	StateCreating State = "creating"
	StateActive   State = "active"
	StateFailed   State = "failed"
	StateDeleting State = "deleting"
	StateDeleted  State = "deleted"
)

// Repository is authoritative local repository metadata.
type Repository struct {
	ID               ID
	OwnerDID         string
	OrganizationID   *uuid.UUID
	OrganizationSlug string
	Slug             string
	DisplayName      string
	Description      string
	Visibility       Visibility
	State            State
	DefaultBranch    string
	StorageKey       string
	ATURI            string
	ATCID            string
	ForkedFrom       *ForkSource
	ForkCount        int64
	ViewerCanAdmin   bool
	ArchivedAt       *time.Time
	CreatedAt        time.Time
	UpdatedAt        time.Time
}

// Page is a bounded keyset page of repositories.
type Page struct {
	Items      []Repository
	NextCursor *uuid.UUID
}

// ATIdentity is the canonical identity returned for a published repository record.
type ATIdentity struct {
	URI string
	CID string
}

// ForkSource identifies the portable upstream and, when hosted here, its local Git storage.
type ForkSource struct {
	URI               string
	CID               string
	GitHTTPS          string
	LocalRepositoryID *ID
}

// ForkSync reports a safe upstream default-branch synchronization.
type ForkSync struct {
	BeforeSHA string
	AfterSHA  string
	Updated   bool
}

// Publication is the portable public repository record to publish for an owner.
type Publication struct {
	ID            ID
	OwnerDID      string
	Organization  *ATIdentity
	ForkedFrom    *ATIdentity
	Slug          string
	Name          string
	Description   string
	DefaultBranch string
	GitHTTPS      string
	GitSSH        string
	Web           string
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

// CreateInput is validated repository creation data.
type CreateInput struct {
	OwnerDID         string
	OrganizationID   *uuid.UUID
	OrganizationSlug string
	OrganizationAT   *ATIdentity
	ForkedFrom       *ForkSource
	Slug             string
	DisplayName      string
	Description      string
	Visibility       Visibility
	DefaultBranch    string
}

// SettingsInput is the complete mutable repository settings document.
type SettingsInput struct {
	OwnerAlias    string
	Slug          string
	DisplayName   string
	Description   string
	Visibility    Visibility
	DefaultBranch string
	Archived      bool
}

// Validate checks repository settings at the domain boundary.
func (input SettingsInput) Validate() error {
	if strings.TrimSpace(input.OwnerAlias) == "" {
		return fmt.Errorf("owner route must not be empty")
	}
	return CreateInput{
		OwnerDID: "did:placeholder:settings", Slug: input.Slug, DisplayName: input.DisplayName,
		Description: input.Description, Visibility: input.Visibility, DefaultBranch: input.DefaultBranch,
	}.Validate()
}

// Deletion describes a recoverable repository deletion request.
type Deletion struct {
	ID             uuid.UUID
	RepositoryID   ID
	RequestedByDID string
	RequestedAt    time.Time
	PurgeAfter     time.Time
}

// Validate checks repository input at the domain boundary.
func (input CreateInput) Validate() error {
	if strings.TrimSpace(input.OwnerDID) == "" {
		return fmt.Errorf("owner DID must not be empty")
	}
	if (input.OrganizationID == nil) != (input.OrganizationSlug == "") {
		return fmt.Errorf("organization ID and slug must be provided together")
	}
	if input.OrganizationID != nil && (input.OrganizationAT == nil || input.OrganizationAT.URI == "" || input.OrganizationAT.CID == "") {
		return fmt.Errorf("organization AT identity must be provided for an organization repository")
	}
	if input.ForkedFrom != nil {
		if strings.TrimSpace(input.ForkedFrom.URI) == "" || strings.TrimSpace(input.ForkedFrom.CID) == "" {
			return fmt.Errorf("fork source must include a strong reference")
		}
		if input.ForkedFrom.LocalRepositoryID == nil && strings.TrimSpace(input.ForkedFrom.GitHTTPS) == "" {
			return fmt.Errorf("fork source must include a local repository or HTTPS endpoint")
		}
		if input.Visibility != VisibilityPublic {
			return fmt.Errorf("public fork sources must remain public")
		}
	}
	if len(input.Slug) > 100 || !slugPattern.MatchString(input.Slug) {
		return fmt.Errorf("slug must match %s and contain at most 100 characters", slugPattern)
	}
	if len(input.DisplayName) > 255 {
		return fmt.Errorf("display name must contain at most 255 characters")
	}
	if len(input.Description) > 2000 {
		return fmt.Errorf("description must contain at most 2000 characters")
	}
	if input.Visibility != VisibilityPublic && input.Visibility != VisibilityPrivate {
		return fmt.Errorf("visibility must be public or private")
	}
	if input.DefaultBranch == "" || len(input.DefaultBranch) > 255 || strings.HasPrefix(input.DefaultBranch, "-") {
		return fmt.Errorf("default branch must be a valid non-option branch name")
	}
	return nil
}
