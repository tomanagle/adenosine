// Package release manages repository releases and their downloadable assets.
package release

import (
	"errors"
	"fmt"
	"io"
	"mime"
	"strings"
	"time"
	"unicode"

	"github.com/adenosine-dev/adenosine/internal/repository"
	"github.com/google/uuid"
)

const maxReleaseBodyBytes = 1024 * 1024

var (
	ErrNotFound      = errors.New("release not found")
	ErrConflict      = errors.New("release already exists")
	ErrValidation    = errors.New("release validation failed")
	ErrQuotaExceeded = errors.New("release asset quota exceeded")
	ErrSizeMismatch  = errors.New("release asset size mismatch")
)

type State string

const (
	StateDraft     State = "draft"
	StatePublished State = "published"
	StateDeleting  State = "deleting"
)

type Release struct {
	ID           uuid.UUID
	RepositoryID repository.ID
	TagName      string
	TargetSHA    string
	Name         string
	Body         string
	State        State
	Prerelease   bool
	CreatedByDID string
	CreatedAt    time.Time
	UpdatedAt    time.Time
	PublishedAt  *time.Time
}

type Asset struct {
	ID           uuid.UUID
	ReleaseID    uuid.UUID
	RepositoryID repository.ID
	Name         string
	ContentType  string
	SizeBytes    int64
	SHA256       string
	StorageKey   string
	CreatedAt    time.Time
}

type CreateInput struct {
	TagName      string
	Name         string
	Body         string
	Draft        bool
	Prerelease   bool
	CreatedByDID string
}

type UpdateInput struct {
	Name       string
	Body       string
	Draft      bool
	Prerelease bool
}

type AssetInput struct {
	Name        string
	ContentType string
	SizeBytes   int64
	Body        io.Reader
}

type Limits struct {
	AssetBytes      int64
	ReleaseBytes    int64
	RepositoryBytes int64
}

type Page[T any] struct {
	Items      []T
	NextCursor *uuid.UUID
}

func (input CreateInput) Validate() error {
	if err := validateTagName(input.TagName); err != nil {
		return err
	}
	if err := validateText(input.Name, "name", 1, 255); err != nil {
		return err
	}
	if len(input.Body) > maxReleaseBodyBytes {
		return fmt.Errorf("%w: body exceeds %d bytes", ErrValidation, maxReleaseBodyBytes)
	}
	if strings.TrimSpace(input.CreatedByDID) == "" || len(input.CreatedByDID) > 2048 {
		return fmt.Errorf("%w: creator DID is required", ErrValidation)
	}
	return nil
}

func (input UpdateInput) Validate() error {
	if err := validateText(input.Name, "name", 1, 255); err != nil {
		return err
	}
	if len(input.Body) > maxReleaseBodyBytes {
		return fmt.Errorf("%w: body exceeds %d bytes", ErrValidation, maxReleaseBodyBytes)
	}
	return nil
}

func (input AssetInput) Validate(maxBytes int64) (string, error) {
	if err := validateText(input.Name, "asset name", 1, 255); err != nil {
		return "", err
	}
	if input.Name == "." || input.Name == ".." || strings.ContainsAny(input.Name, `/\`) {
		return "", fmt.Errorf("%w: asset name must be a file name", ErrValidation)
	}
	for _, value := range input.Name {
		if unicode.IsControl(value) {
			return "", fmt.Errorf("%w: asset name contains a control character", ErrValidation)
		}
	}
	if input.SizeBytes < 0 || input.SizeBytes > maxBytes {
		return "", fmt.Errorf("%w: asset size must be between 0 and %d bytes", ErrQuotaExceeded, maxBytes)
	}
	if input.Body == nil {
		return "", fmt.Errorf("%w: asset body is required", ErrValidation)
	}
	mediaType, _, err := mime.ParseMediaType(input.ContentType)
	if err != nil || len(mediaType) > 255 || !strings.Contains(mediaType, "/") {
		return "", fmt.Errorf("%w: content type is invalid", ErrValidation)
	}
	return mediaType, nil
}

func (limits Limits) Validate() error {
	if limits.AssetBytes <= 0 || limits.ReleaseBytes < limits.AssetBytes || limits.RepositoryBytes < limits.ReleaseBytes {
		return fmt.Errorf("%w: asset quotas must be positive and monotonically increasing", ErrValidation)
	}
	return nil
}

func validateTagName(value string) error {
	if err := validateText(value, "tag name", 1, 1024); err != nil {
		return err
	}
	if strings.HasPrefix(value, "-") || strings.Contains(value, "..") || strings.ContainsAny(value, "~^:?*[\\ ") ||
		strings.HasPrefix(value, "/") || strings.HasSuffix(value, "/") || strings.HasSuffix(value, ".") || strings.HasSuffix(value, ".lock") || strings.Contains(value, "@{") || strings.Contains(value, "//") {
		return fmt.Errorf("%w: tag name is not a valid Git ref name", ErrValidation)
	}
	for _, character := range value {
		if unicode.IsControl(character) {
			return fmt.Errorf("%w: tag name contains a control character", ErrValidation)
		}
	}
	return nil
}

func validateText(value, name string, minimum, maximum int) error {
	if strings.TrimSpace(value) != value || len(value) < minimum || len(value) > maximum {
		return fmt.Errorf("%w: %s must contain between %d and %d bytes without surrounding whitespace", ErrValidation, name, minimum, maximum)
	}
	return nil
}
