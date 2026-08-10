// Package issue defines portable issues, author-owned comments, and repository-authoritative status records.
package issue

import (
	"context"
	"crypto/sha256"
	"encoding/base32"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/google/uuid"
)

const (
	// Collection is the NSID of reporter-authored issue records.
	Collection = "dev.adenosine.issue"
	// CommentCollection is the NSID of comment-author-owned issue comment records.
	CommentCollection = "dev.adenosine.issueComment"
	// StatusCollection is the NSID of repository-owner-authored issue status records.
	StatusCollection     = "dev.adenosine.issueStatus"
	repositoryCollection = "dev.adenosine.repo"
	maximumTitleLength   = 255
	maximumBodyLength    = 65535
)

var (
	// ErrValidation indicates invalid issue identity or record data.
	ErrValidation = errors.New("issue validation failed")
	// ErrConflict indicates that a record changed or an expected slot has incompatible content.
	ErrConflict = errors.New("issue record conflict")
	// ErrAuthorization indicates that issue authority or AT Protocol write delegation is absent.
	ErrAuthorization = errors.New("AT Protocol issue authorization required")
	// ErrProvider indicates that the AT Protocol provider could not complete an operation.
	ErrProvider = errors.New("issue provider failure")
	// ErrNotFound indicates that an issue or repository has no active local projection.
	ErrNotFound = errors.New("issue target not found")
)

// State is the repository owner's authoritative issue state.
type State string

const (
	StateOpen   State = "open"
	StateClosed State = "closed"
)

// StrongRef identifies an immutable observation of an AT Protocol record.
type StrongRef struct {
	URI string
	CID string
}

// Record is reporter-authored issue content.
type Record struct {
	Repository StrongRef
	Title      string
	Body       string
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

// Issue combines reporter-authored content with its AT Protocol envelope.
type Issue struct {
	URI       string
	CID       string
	AuthorDID string
	Record
}

// CommentRecord is comment-author-owned content attached to an issue observation.
type CommentRecord struct {
	Subject   StrongRef
	Parent    *StrongRef
	Body      string
	CreatedAt time.Time
	UpdatedAt time.Time
}

// Comment combines comment-author-owned content with its AT Protocol envelope.
type Comment struct {
	URI       string
	CID       string
	AuthorDID string
	CommentRecord
}

// ProjectedIssue is the current locally indexed issue and authoritative state.
type ProjectedIssue struct {
	Issue
	State        State
	Status       StrongRef
	CommentCount int64
	IndexedAt    time.Time
}

// Projection is the bounded current issue projection for one repository.
type Projection struct {
	IssueCount     int64
	OpenIssueCount int64
	Issues         []ProjectedIssue
}

// CreateInput contains reporter-authored issue content.
type CreateInput struct {
	RepositoryURI string
	Title         string
	Body          string
}

// StatusInput contains a repository owner's requested issue state.
type StatusInput struct {
	IssueURI string
	State    State
}

type statusTarget struct {
	Subject         StrongRef
	Repository      StrongRef
	StatusCreatedAt time.Time
}

type projectionStore interface {
	GetProjection(context.Context, string, int) (Projection, error)
	GetRepositoryTarget(context.Context, string) (StrongRef, error)
	GetStatusTarget(context.Context, string) (statusTarget, error)
}

// Publisher writes reporter issues and repository-authoritative statuses.
type Publisher interface {
	CreateIssue(context.Context, string, string, Record) (Issue, error)
	PutIssueStatus(context.Context, string, StatusRecord) (Status, error)
}

type clock interface{ Now() time.Time }

// Service coordinates projected issue reads and asynchronous authoritative writes.
type Service struct {
	store     projectionStore
	publisher Publisher
	clock     clock
}

// NewService constructs the issue application service.
func NewService(store projectionStore, publisher Publisher, clock clock) *Service {
	return &Service{store: store, publisher: publisher, clock: clock}
}

// Get returns at most 100 current active projected issues for a repository.
func (service *Service) Get(ctx context.Context, repositoryURI string) (Projection, error) {
	if _, err := RepositoryOwnerDID(repositoryURI); err != nil {
		return Projection{}, err
	}
	projection, err := service.store.GetProjection(ctx, repositoryURI, 100)
	if err != nil {
		return Projection{}, projectionError("get issue projection", err)
	}
	if projection.Issues == nil {
		projection.Issues = []ProjectedIssue{}
	}
	return projection, nil
}

// Create publishes a reporter-authored issue without changing the local projection.
func (service *Service) Create(ctx context.Context, authorDID string, input CreateInput) (Issue, error) {
	if _, err := RepositoryOwnerDID(input.RepositoryURI); err != nil {
		return Issue{}, err
	}
	repository, err := service.store.GetRepositoryTarget(ctx, input.RepositoryURI)
	if err != nil {
		return Issue{}, projectionError("get issue repository target", err)
	}
	now := service.clock.Now().UTC()
	record := Record{Repository: repository, Title: input.Title, Body: input.Body, CreatedAt: now, UpdatedAt: now}
	if err := record.Validate(); err != nil {
		return Issue{}, err
	}
	rkey, err := RandomRecordKey()
	if err != nil {
		return Issue{}, fmt.Errorf("create issue record key: %w", err)
	}
	return service.publisher.CreateIssue(ctx, authorDID, rkey, record)
}

// PutStatus publishes repository-authoritative state without changing the local projection.
func (service *Service) PutStatus(ctx context.Context, authorDID string, input StatusInput) (Status, error) {
	if _, err := validateATURI(input.IssueURI, Collection, "issue_uri"); err != nil {
		return Status{}, err
	}
	target, err := service.store.GetStatusTarget(ctx, input.IssueURI)
	if err != nil {
		return Status{}, projectionError("get issue status target", err)
	}
	ownerDID, err := RepositoryOwnerDID(target.Repository.URI)
	if err != nil {
		return Status{}, err
	}
	if authorDID != ownerDID {
		return Status{}, &AuthorizationError{Err: errors.New("status author is not repository owner")}
	}
	now := service.clock.Now().UTC()
	createdAt := target.StatusCreatedAt
	if createdAt.IsZero() {
		createdAt = now
	}
	record := StatusRecord{Subject: target.Subject, Repository: target.Repository, State: input.State, CreatedAt: createdAt, UpdatedAt: now}
	if err := record.Validate(); err != nil {
		return Status{}, err
	}
	return service.publisher.PutIssueStatus(ctx, authorDID, record)
}

func projectionError(operation string, err error) error {
	if errors.Is(err, ErrNotFound) {
		return ErrNotFound
	}
	return fmt.Errorf("%s: %w", operation, err)
}

// StatusRecord is the repository owner's authoritative state for an issue.
type StatusRecord struct {
	Subject    StrongRef
	Repository StrongRef
	State      State
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

// Status combines authoritative state with its AT Protocol envelope.
type Status struct {
	URI       string
	CID       string
	AuthorDID string
	StatusRecord
}

// Validate checks issue content and proves that its envelope is owned by AuthorDID.
func (value Issue) Validate() error {
	if err := value.Record.Validate(); err != nil {
		return err
	}
	uri, err := validateEnvelope(value.URI, value.CID, value.AuthorDID, Collection)
	if err != nil {
		return err
	}
	return ValidateRecordKey(uri.RecordKey().String())
}

// Validate checks comment content and proves that its envelope is owned by AuthorDID.
func (value Comment) Validate() error {
	if err := value.CommentRecord.Validate(); err != nil {
		return err
	}
	uri, err := validateEnvelope(value.URI, value.CID, value.AuthorDID, CommentCollection)
	if err != nil {
		return err
	}
	if value.Parent != nil && value.Parent.URI == value.URI {
		return &ValidationError{Field: "parent.uri", Problem: "must differ from the comment URI"}
	}
	return ValidateRecordKey(uri.RecordKey().String())
}

// Validate checks status content, deterministic identity, and repository-owner authority.
func (value Status) Validate() error {
	if err := value.StatusRecord.Validate(); err != nil {
		return err
	}
	uri, err := validateEnvelope(value.URI, value.CID, value.AuthorDID, StatusCollection)
	if err != nil {
		return err
	}
	owner, err := RepositoryOwnerDID(value.Repository.URI)
	if err != nil {
		return err
	}
	if owner != value.AuthorDID {
		return &AuthorizationError{Err: errors.New("status author is not repository owner")}
	}
	wantRKey, err := StatusRecordKey(value.Subject.URI)
	if err != nil {
		return err
	}
	if uri.RecordKey().String() != wantRKey {
		return &ValidationError{Field: "URI", Problem: "must use the deterministic issue status record key"}
	}
	return nil
}

// ValidationError identifies an issue field that failed validation.
type ValidationError struct {
	Field   string
	Problem string
}

func (err *ValidationError) Error() string {
	return fmt.Sprintf("%s: %s: %s", ErrValidation, err.Field, err.Problem)
}
func (err *ValidationError) Unwrap() error { return ErrValidation }

// ConflictError reports incompatible or concurrently changed provider state without exposing details.
type ConflictError struct{ Err error }

func (err *ConflictError) Error() string { return ErrConflict.Error() }
func (err *ConflictError) Unwrap() []error {
	if err.Err == nil {
		return []error{ErrConflict}
	}
	return []error{ErrConflict, err.Err}
}

// AuthorizationError reports missing authority or delegation without exposing credential details.
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

// Validate checks reporter-authored issue content.
func (record Record) Validate() error {
	if err := validateStrongRef(record.Repository, repositoryCollection, "repository"); err != nil {
		return err
	}
	if len(record.Title) > maximumTitleLength {
		return &ValidationError{Field: "title", Problem: "must contain at most 255 bytes"}
	}
	if len(record.Body) > maximumBodyLength {
		return &ValidationError{Field: "body", Problem: "must contain at most 65535 bytes"}
	}
	return validateTimes(record.CreatedAt, record.UpdatedAt)
}

// Validate checks comment-author-owned issue comment content.
func (record CommentRecord) Validate() error {
	if err := validateStrongRef(record.Subject, Collection, "subject"); err != nil {
		return err
	}
	if record.Parent != nil {
		if err := validateStrongRef(*record.Parent, CommentCollection, "parent"); err != nil {
			return err
		}
	}
	if len(record.Body) > maximumBodyLength {
		return &ValidationError{Field: "body", Problem: "must contain at most 65535 bytes"}
	}
	return validateTimes(record.CreatedAt, record.UpdatedAt)
}

// Validate checks repository-authored issue status content and its authority relationship.
func (record StatusRecord) Validate() error {
	if err := validateStrongRef(record.Subject, Collection, "subject"); err != nil {
		return err
	}
	if err := validateStrongRef(record.Repository, repositoryCollection, "repository"); err != nil {
		return err
	}
	if record.State != StateOpen && record.State != StateClosed {
		return &ValidationError{Field: "state", Problem: "must be open or closed"}
	}
	return validateTimes(record.CreatedAt, record.UpdatedAt)
}

// RepositoryOwnerDID returns the canonical DID authority of a repository reference.
func RepositoryOwnerDID(repositoryURI string) (string, error) {
	uri, err := validateATURI(repositoryURI, repositoryCollection, "repository.uri")
	if err != nil {
		return "", err
	}
	return uri.Authority().String(), nil
}

// StatusRecordKey derives the deterministic status rkey for a canonical issue URI.
func StatusRecordKey(issueURI string) (string, error) {
	if _, err := validateATURI(issueURI, Collection, "subject.uri"); err != nil {
		return "", err
	}
	digest := sha256.Sum256([]byte(StatusCollection + "\x00" + issueURI))
	return strings.ToLower(base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(digest[:])), nil
}

// RandomRecordKey creates a retry-stable issue key from a UUIDv7 value.
func RandomRecordKey() (string, error) {
	id, err := uuid.NewV7()
	if err != nil {
		return "", fmt.Errorf("generate issue record key: %w", err)
	}
	return strings.ReplaceAll(id.String(), "-", ""), nil
}

// ValidateRecordKey checks a caller-generated issue record key.
func ValidateRecordKey(value string) error {
	rkey, err := syntax.ParseRecordKey(value)
	if err != nil || rkey.String() != value {
		return &ValidationError{Field: "rkey", Problem: "must be a canonical AT Protocol record key"}
	}
	return nil
}

// ValidateCID requires a canonical CID string.
func ValidateCID(value string) error {
	cid, err := syntax.ParseCID(value)
	if err != nil || cid.String() != value || value != strings.ToLower(value) || !strings.HasPrefix(value, "b") {
		return &ValidationError{Field: "CID", Problem: "must be a canonical CID"}
	}
	return nil
}

func validateStrongRef(ref StrongRef, collection, field string) error {
	if _, err := validateATURI(ref.URI, collection, field+".uri"); err != nil {
		return err
	}
	if err := ValidateCID(ref.CID); err != nil {
		return &ValidationError{Field: field + ".cid", Problem: "must be a canonical CID"}
	}
	return nil
}

func validateATURI(value, collection, field string) (syntax.ATURI, error) {
	uri, err := syntax.ParseATURI(value)
	if err != nil || uri.String() != value || uri.Collection().String() != collection || uri.RecordKey().String() == "" {
		return "", &ValidationError{Field: field, Problem: "must be a canonical " + collection + " AT URI"}
	}
	did, err := uri.Authority().AsDID()
	if err != nil || did.String() != uri.Authority().String() {
		return "", &ValidationError{Field: field, Problem: "must use a canonical DID authority"}
	}
	return uri, nil
}

func validateEnvelope(uriValue, cidValue, authorDID, collection string) (syntax.ATURI, error) {
	did, err := syntax.ParseDID(authorDID)
	if err != nil || did.String() != authorDID {
		return "", &ValidationError{Field: "authorDID", Problem: "must be a canonical AT Protocol DID"}
	}
	uri, err := validateATURI(uriValue, collection, "URI")
	if err != nil {
		return "", err
	}
	if uri.Authority().String() != authorDID {
		return "", &AuthorizationError{Err: errors.New("record is not authored by its envelope authority")}
	}
	if err := ValidateCID(cidValue); err != nil {
		return "", err
	}
	return uri, nil
}

func validateTimes(createdAt, updatedAt time.Time) error {
	if createdAt.IsZero() {
		return &ValidationError{Field: "createdAt", Problem: "must not be empty"}
	}
	if updatedAt.IsZero() {
		return &ValidationError{Field: "updatedAt", Problem: "must not be empty"}
	}
	if updatedAt.Before(createdAt) {
		return &ValidationError{Field: "updatedAt", Problem: "must not precede createdAt"}
	}
	return nil
}
