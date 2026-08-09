// Package profile manages the local projection of shared AT Protocol developer profiles.
package profile

import (
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/bluesky-social/indigo/atproto/syntax"
)

const (
	maxDisplayNameBytes = 640
	maxDisplayNameRunes = 64
	maxBioBytes         = 2560
	maxBioRunes         = 256
	maxWebsiteBytes     = 2048
	maxLocationBytes    = 640
	maxLocationRunes    = 64
)

var (
	// ErrValidation indicates invalid profile input or provider data.
	ErrValidation = errors.New("profile validation failed")
	// ErrNotFound indicates that no shared profile exists for a DID.
	ErrNotFound = errors.New("profile not found")
	// ErrProvider indicates that the profile provider could not complete an operation.
	ErrProvider = errors.New("profile provider failure")
	// ErrAuthorization indicates that no usable AT Protocol write delegation exists.
	ErrAuthorization = errors.New("AT Protocol profile authorization required")
)

// ValidationError identifies a profile field that failed validation.
type ValidationError struct {
	Field   string
	Problem string
}

func (err *ValidationError) Error() string {
	return fmt.Sprintf("%s: %s: %s", ErrValidation, err.Field, err.Problem)
}

func (err *ValidationError) Unwrap() error { return ErrValidation }

// NotFoundError identifies the DID whose profile was not found.
type NotFoundError struct{ DID string }

func (err *NotFoundError) Error() string { return fmt.Sprintf("%s: %s", ErrNotFound, err.DID) }
func (err *NotFoundError) Unwrap() error { return ErrNotFound }

// ProviderError preserves a provider cause while exposing a stable error category.
type ProviderError struct {
	Operation string
	Err       error
}

func (err *ProviderError) Error() string   { return ErrProvider.Error() + " during " + err.Operation }
func (err *ProviderError) Unwrap() []error { return []error{ErrProvider, err.Err} }

// AuthorizationError indicates that the user must authenticate with AT Protocol again.
type AuthorizationError struct{ Err error }

func (err *AuthorizationError) Error() string   { return ErrAuthorization.Error() }
func (err *AuthorizationError) Unwrap() []error { return []error{ErrAuthorization, err.Err} }

// Record is the portable dev.adenosine.profile record value. Its repository key is always self.
type Record struct {
	DisplayName string
	Bio         string
	Website     string
	Location    string
	CreatedAt   time.Time
}

// Profile combines the portable record with resolved identity and local projection data.
type Profile struct {
	DID               string
	URI               string
	CID               string
	Handle            string
	DisplayName       string
	Bio               string
	AvatarRef         string
	Website           string
	Location          string
	RepositoryCount   int64
	ContributionCount int64
	RecordCreatedAt   time.Time
	IndexedAt         time.Time
}

// UpdateInput contains only user-editable profile fields; identity comes from authentication.
type UpdateInput struct {
	DisplayName string
	Bio         string
	Website     string
	Location    string
}

// Validate checks editable profile values against the Lexicon limits.
func (input UpdateInput) Validate() error {
	if err := validateText("displayName", input.DisplayName, maxDisplayNameBytes, maxDisplayNameRunes); err != nil {
		return err
	}
	if err := validateText("bio", input.Bio, maxBioBytes, maxBioRunes); err != nil {
		return err
	}
	if err := validateText("location", input.Location, maxLocationBytes, maxLocationRunes); err != nil {
		return err
	}
	if len(input.Website) > maxWebsiteBytes || !utf8.ValidString(input.Website) {
		return &ValidationError{Field: "website", Problem: "must be valid UTF-8 and contain at most 2048 bytes"}
	}
	if input.Website != "" {
		website, err := url.Parse(input.Website)
		if err != nil || website.Scheme != "https" || website.Host == "" || website.User != nil || !website.IsAbs() {
			return &ValidationError{Field: "website", Problem: "must be an absolute HTTPS URL without user information"}
		}
	}
	return nil
}

func validateText(field, value string, maximumBytes, maximumRunes int) error {
	if !utf8.ValidString(value) || len(value) > maximumBytes || utf8.RuneCountInString(value) > maximumRunes {
		return &ValidationError{Field: field, Problem: fmt.Sprintf("must be valid UTF-8 and contain at most %d bytes and %d characters", maximumBytes, maximumRunes)}
	}
	return nil
}

func validateDID(value string) (string, error) {
	value = strings.TrimSpace(value)
	if _, err := syntax.ParseDID(value); err != nil {
		return "", &ValidationError{Field: "DID", Problem: "must be a valid AT Protocol DID"}
	}
	return value, nil
}

func validateProviderProfile(profile Profile, did string) error {
	if profile.DID != did {
		return &ValidationError{Field: "DID", Problem: "provider profile does not match requested identity"}
	}
	wantURI := "at://" + did + "/dev.adenosine.profile/self"
	if profile.URI != wantURI {
		return &ValidationError{Field: "URI", Problem: "must identify the authenticated DID's self profile record"}
	}
	if strings.TrimSpace(profile.CID) == "" {
		return &ValidationError{Field: "CID", Problem: "must not be empty"}
	}
	if profile.RecordCreatedAt.IsZero() {
		return &ValidationError{Field: "createdAt", Problem: "must not be empty"}
	}
	return (UpdateInput{
		DisplayName: profile.DisplayName,
		Bio:         profile.Bio,
		Website:     profile.Website,
		Location:    profile.Location,
	}).Validate()
}
