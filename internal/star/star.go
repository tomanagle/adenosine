// Package star defines portable star records and their deterministic AT Protocol identity.
package star

import (
	"context"
	"crypto/sha256"
	"encoding/base32"
	"encoding/binary"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/bluesky-social/indigo/atproto/syntax"
)

const (
	// Collection is the NSID of the portable star record.
	Collection           = "dev.adenosine.star"
	repositoryCollection = "dev.adenosine.repo"
)

var (
	// ErrValidation indicates invalid star identity or record data.
	ErrValidation = errors.New("star validation failed")
	// ErrConflict indicates that the deterministic star slot contains another record.
	ErrConflict = errors.New("star record conflict")
	// ErrAuthorization indicates that no usable AT Protocol write delegation exists.
	ErrAuthorization = errors.New("AT Protocol star authorization required")
	// ErrProvider indicates that the AT Protocol provider could not complete an operation.
	ErrProvider = errors.New("star provider failure")
	// ErrNotFound indicates that the target repository has no active local projection.
	ErrNotFound = errors.New("star repository target not found")
)

// Target is the current canonical strong reference to a projected repository record.
type Target struct {
	URI string
	CID string
}

// Star combines a portable star record with its AT Protocol envelope.
type Star struct {
	URI       string
	CID       string
	AuthorDID string
	Target    Target
	CreatedAt time.Time
	IndexedAt time.Time
}

// Projection is the bounded current star projection for one repository.
type Projection struct {
	StarCount int64
	Stars     []Star
}

type projectionStore interface {
	GetTarget(context.Context, string) (Target, int64, error)
	GetProjection(context.Context, string, int) (Projection, error)
}

// Publisher writes authoritative portable star records to the authenticated PDS.
type Publisher interface {
	CreateStar(context.Context, string, Target, time.Time) (Star, error)
	DeleteStar(context.Context, string, Target) error
}

type clock interface{ Now() time.Time }

// Service coordinates projected reads and authoritative asynchronous writes.
type Service struct {
	store     projectionStore
	publisher Publisher
	clock     clock
}

// NewService constructs the star application service.
func NewService(store projectionStore, publisher Publisher, clock clock) *Service {
	return &Service{store: store, publisher: publisher, clock: clock}
}

// Get returns at most 100 active stars from the local projection.
func (service *Service) Get(ctx context.Context, repositoryURI string) (Projection, error) {
	if err := validateRepositoryURI(repositoryURI); err != nil {
		return Projection{}, err
	}
	projection, err := service.store.GetProjection(ctx, repositoryURI, 100)
	if err != nil {
		return Projection{}, projectionError("get star projection", err)
	}
	if projection.Stars == nil {
		projection.Stars = []Star{}
	}
	return projection, nil
}

// Create publishes a star without changing the local asynchronous projection.
func (service *Service) Create(ctx context.Context, authorDID, repositoryURI string) (Star, error) {
	target, err := service.target(ctx, repositoryURI)
	if err != nil {
		return Star{}, err
	}
	return service.publisher.CreateStar(ctx, authorDID, target, service.clock.Now().UTC())
}

// Delete removes a PDS star without changing the local asynchronous projection.
func (service *Service) Delete(ctx context.Context, authorDID, repositoryURI string) error {
	target, err := service.target(ctx, repositoryURI)
	if err != nil {
		return err
	}
	return service.publisher.DeleteStar(ctx, authorDID, target)
}

func (service *Service) target(ctx context.Context, repositoryURI string) (Target, error) {
	if err := validateRepositoryURI(repositoryURI); err != nil {
		return Target{}, err
	}
	target, _, err := service.store.GetTarget(ctx, repositoryURI)
	if err != nil {
		return Target{}, projectionError("get repository target", err)
	}
	if err := target.Validate(); err != nil {
		return Target{}, fmt.Errorf("validate projected repository target: %w", err)
	}
	return target, nil
}

func projectionError(operation string, err error) error {
	if errors.Is(err, ErrNotFound) {
		return ErrNotFound
	}
	return fmt.Errorf("%s: %w", operation, err)
}

// ValidationError identifies a star field that failed validation.
type ValidationError struct {
	Field   string
	Problem string
}

func (err *ValidationError) Error() string {
	return fmt.Sprintf("%s: %s: %s", ErrValidation, err.Field, err.Problem)
}

func (err *ValidationError) Unwrap() error { return ErrValidation }

// ConflictError reports an occupied deterministic record slot without exposing provider details.
type ConflictError struct{ Err error }

func (err *ConflictError) Error() string { return ErrConflict.Error() }
func (err *ConflictError) Unwrap() []error {
	if err.Err == nil {
		return []error{ErrConflict}
	}
	return []error{ErrConflict, err.Err}
}

// AuthorizationError indicates that the user must authenticate with AT Protocol again.
type AuthorizationError struct{ Err error }

func (err *AuthorizationError) Error() string { return ErrAuthorization.Error() }
func (err *AuthorizationError) Unwrap() []error {
	if err.Err == nil {
		return []error{ErrAuthorization}
	}
	return []error{ErrAuthorization, err.Err}
}

// ProviderError preserves a provider cause while exposing a redacted stable error.
type ProviderError struct {
	Operation string
	Err       error
}

func (err *ProviderError) Error() string { return ErrProvider.Error() + " during " + err.Operation }
func (err *ProviderError) Unwrap() []error {
	if err.Err == nil {
		return []error{ErrProvider}
	}
	return []error{ErrProvider, err.Err}
}

// Validate requires a canonical repository AT URI and canonical CID.
func (target Target) Validate() error {
	if err := validateRepositoryURI(target.URI); err != nil {
		return err
	}
	if err := validateCID(target.CID); err != nil {
		return &ValidationError{Field: "subject.cid", Problem: "must be a canonical CID"}
	}
	return nil
}

// ValidateCID requires a canonical CIDv1 string.
func ValidateCID(value string) error {
	if err := validateCID(value); err != nil {
		return &ValidationError{Field: "CID", Problem: "must be a canonical CID"}
	}
	return nil
}

// RecordKey derives the stable star rkey for a canonical repository URI.
func RecordKey(repositoryURI string) (string, error) {
	if err := validateRepositoryURI(repositoryURI); err != nil {
		return "", err
	}
	digest := sha256.Sum256([]byte(Collection + "\x00" + repositoryURI))
	return strings.ToLower(base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(digest[:])), nil
}

func validateRepositoryURI(value string) error {
	uri, err := syntax.ParseATURI(value)
	if err != nil || uri.String() != value || uri.Collection().String() != repositoryCollection || uri.RecordKey().String() == "" {
		return &ValidationError{Field: "subject.uri", Problem: "must be a canonical repository AT URI"}
	}
	did, err := uri.Authority().AsDID()
	if err != nil || did.String() != uri.Authority().String() {
		return &ValidationError{Field: "subject.uri", Problem: "must use a canonical DID authority"}
	}
	return nil
}

func validateCID(value string) error {
	if len(value) < 2 || value[0] != 'b' || value != strings.ToLower(value) || strings.Contains(value, "=") {
		return errors.New("CID is not canonical lowercase base32")
	}
	raw, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(strings.ToUpper(value[1:]))
	if err != nil || "b"+strings.ToLower(base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(raw)) != value {
		return errors.New("CID is not canonical base32")
	}
	version, remaining, ok := consumeCanonicalUvarint(raw)
	if !ok || version != 1 {
		return errors.New("CID version is not canonical CIDv1")
	}
	codec, remaining, ok := consumeCanonicalUvarint(remaining)
	if !ok || codec == 0 {
		return errors.New("CID codec is invalid")
	}
	hashCode, remaining, ok := consumeCanonicalUvarint(remaining)
	if !ok || hashCode == 0 {
		return errors.New("CID multihash code is invalid")
	}
	digestLength, digest, ok := consumeCanonicalUvarint(remaining)
	if !ok || digestLength == 0 || uint64(len(digest)) != digestLength {
		return errors.New("CID multihash digest is invalid")
	}
	return nil
}

func consumeCanonicalUvarint(value []byte) (uint64, []byte, bool) {
	decoded, count := binary.Uvarint(value)
	if count <= 0 || count != len(binary.AppendUvarint(nil, decoded)) {
		return 0, nil, false
	}
	return decoded, value[count:], true
}
