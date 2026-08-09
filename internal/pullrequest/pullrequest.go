// Package pullrequest defines portable pull requests, reviewer-owned reviews, and target-authoritative statuses.
package pullrequest

import (
	"crypto/sha256"
	"encoding/base32"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/google/uuid"
)

const (
	// Collection is the NSID of contributor-authored pull request records.
	Collection = "dev.adenosine.pullRequest"
	// StatusCollection is the NSID of target-repository-owner-authored status records.
	StatusCollection = "dev.adenosine.pullRequestStatus"
	// ReviewCollection is the NSID of reviewer-authored review records.
	ReviewCollection     = "dev.adenosine.pullRequestReview"
	repositoryCollection = "dev.adenosine.repo"
	maximumBranchLength  = 255
	maximumTitleLength   = 255
	maximumBodyLength    = 65535
)

var (
	// ErrValidation indicates invalid pull request identity or record data.
	ErrValidation = errors.New("pull request validation failed")
	// ErrAuthorization indicates that a record was not authored by the required DID.
	ErrAuthorization = errors.New("AT Protocol pull request authorization required")
	// ErrPermissionDenied indicates that the actor cannot write the local target repository.
	ErrPermissionDenied = fmt.Errorf("pull request repository write permission denied: %w", ErrAuthorization)
	// ErrConflict indicates that a record changed or an expected slot has incompatible content.
	ErrConflict = errors.New("pull request record conflict")
	// ErrProvider indicates that the AT Protocol provider could not complete an operation.
	ErrProvider   = errors.New("pull request provider failure")
	gitSHAPattern = regexp.MustCompile(`^(?:[0-9a-f]{40}|[0-9a-f]{64})$`)
)

// State is the target repository owner's authoritative pull request state.
type State string

const (
	StateOpen   State = "open"
	StateClosed State = "closed"
	StateMerged State = "merged"
)

// Verdict is a reviewer's conclusion about a pull request.
type Verdict string

const (
	VerdictComment        Verdict = "comment"
	VerdictApprove        Verdict = "approve"
	VerdictRequestChanges Verdict = "request_changes"
)

// StrongRef identifies an immutable observation of an AT Protocol record.
type StrongRef struct {
	URI string
	CID string
}

// Record is contributor-authored pull request content.
type Record struct {
	SourceRepository StrongRef
	TargetRepository StrongRef
	SourceBranch     string
	TargetBranch     string
	HeadSHA          string
	Title            string
	Body             string
	CreatedAt        time.Time
	UpdatedAt        time.Time
}

// PullRequest combines contributor-authored content with its AT Protocol envelope.
type PullRequest struct {
	URI       string
	CID       string
	AuthorDID string
	Record
}

// StatusRecord is target-repository-owner-authored pull request state.
type StatusRecord struct {
	Subject          StrongRef
	TargetRepository StrongRef
	State            State
	MergeCommitSHA   string
	CreatedAt        time.Time
	UpdatedAt        time.Time
}

// Status combines authoritative state with its AT Protocol envelope.
type Status struct {
	URI       string
	CID       string
	AuthorDID string
	StatusRecord
}

// ReviewRecord is reviewer-authored pull request feedback.
type ReviewRecord struct {
	Subject   StrongRef
	Verdict   Verdict
	Body      string
	CreatedAt time.Time
	UpdatedAt time.Time
}

// Review combines reviewer-authored feedback with its AT Protocol envelope.
type Review struct {
	URI       string
	CID       string
	AuthorDID string
	ReviewRecord
}

// ConflictError reports incompatible or concurrently changed provider state without exposing details.
type ConflictError struct{ Err error }

func (err *ConflictError) Error() string { return ErrConflict.Error() }
func (err *ConflictError) Unwrap() []error {
	if err.Err == nil {
		return []error{ErrConflict}
	}
	return []error{ErrConflict, err.Err}
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

// ValidationError identifies a pull request field that failed validation.
type ValidationError struct {
	Field   string
	Problem string
}

func (err *ValidationError) Error() string {
	return fmt.Sprintf("%s: %s: %s", ErrValidation, err.Field, err.Problem)
}
func (err *ValidationError) Unwrap() error { return ErrValidation }

// AuthorizationError reports an invalid authorship relationship without exposing credentials.
type AuthorizationError struct{ Err error }

func (err *AuthorizationError) Error() string { return ErrAuthorization.Error() }
func (err *AuthorizationError) Unwrap() []error {
	if err.Err == nil {
		return []error{ErrAuthorization}
	}
	return []error{ErrAuthorization, err.Err}
}

// Validate checks contributor-authored pull request content.
func (record Record) Validate() error {
	if err := validateStrongRef(record.SourceRepository, repositoryCollection, "sourceRepository"); err != nil {
		return err
	}
	if err := validateStrongRef(record.TargetRepository, repositoryCollection, "targetRepository"); err != nil {
		return err
	}
	if err := validateBranch(record.SourceBranch, "sourceBranch"); err != nil {
		return err
	}
	if err := validateBranch(record.TargetBranch, "targetBranch"); err != nil {
		return err
	}
	if record.SourceRepository.URI == record.TargetRepository.URI && record.SourceBranch == record.TargetBranch {
		return &ValidationError{Field: "sourceRepository", Problem: "source and target repository branches must differ"}
	}
	if err := validateGitSHA(record.HeadSHA, "headSHA"); err != nil {
		return err
	}
	if len(record.Title) == 0 || len(record.Title) > maximumTitleLength || !utf8.ValidString(record.Title) {
		return &ValidationError{Field: "title", Problem: "must contain between 1 and 255 bytes"}
	}
	if len(record.Body) > maximumBodyLength || !utf8.ValidString(record.Body) {
		return &ValidationError{Field: "body", Problem: "must contain at most 65535 bytes"}
	}
	return validateTimes(record.CreatedAt, record.UpdatedAt)
}

// Validate checks target-authoritative status content.
func (record StatusRecord) Validate() error {
	if err := validateStrongRef(record.Subject, Collection, "subject"); err != nil {
		return err
	}
	if err := validateStrongRef(record.TargetRepository, repositoryCollection, "targetRepository"); err != nil {
		return err
	}
	if record.State != StateOpen && record.State != StateClosed && record.State != StateMerged {
		return &ValidationError{Field: "state", Problem: "must be open, closed, or merged"}
	}
	if record.State == StateMerged {
		if err := validateGitSHA(record.MergeCommitSHA, "mergeCommitSHA"); err != nil {
			return err
		}
	} else if record.MergeCommitSHA != "" {
		return &ValidationError{Field: "mergeCommitSHA", Problem: "must be absent unless state is merged"}
	}
	return validateTimes(record.CreatedAt, record.UpdatedAt)
}

// Validate checks reviewer-authored review content.
func (record ReviewRecord) Validate() error {
	if err := validateStrongRef(record.Subject, Collection, "subject"); err != nil {
		return err
	}
	if record.Verdict != VerdictComment && record.Verdict != VerdictApprove && record.Verdict != VerdictRequestChanges {
		return &ValidationError{Field: "verdict", Problem: "must be comment, approve, or request_changes"}
	}
	if len(record.Body) > maximumBodyLength || !utf8.ValidString(record.Body) {
		return &ValidationError{Field: "body", Problem: "must contain at most 65535 bytes"}
	}
	return validateTimes(record.CreatedAt, record.UpdatedAt)
}

// Validate checks pull request content and proves that the contributor owns its envelope.
func (value PullRequest) Validate() error {
	if err := value.Record.Validate(); err != nil {
		return err
	}
	uri, err := validateEnvelope(value.URI, value.CID, value.AuthorDID, Collection)
	if err != nil {
		return err
	}
	return ValidateRecordKey(uri.RecordKey().String())
}

// Validate checks status content, deterministic identity, and target-repository-owner authority.
func (value Status) Validate() error {
	if err := value.StatusRecord.Validate(); err != nil {
		return err
	}
	uri, err := validateEnvelope(value.URI, value.CID, value.AuthorDID, StatusCollection)
	if err != nil {
		return err
	}
	owner, err := RepositoryOwnerDID(value.TargetRepository.URI)
	if err != nil {
		return err
	}
	if owner != value.AuthorDID {
		return &AuthorizationError{Err: errors.New("status author is not target repository owner")}
	}
	wantRKey, err := StatusRecordKey(value.Subject.URI)
	if err != nil {
		return err
	}
	if uri.RecordKey().String() != wantRKey {
		return &ValidationError{Field: "URI", Problem: "must use the deterministic pull request status record key"}
	}
	return nil
}

// Validate checks review content and proves that the reviewer owns its envelope.
func (value Review) Validate() error {
	if err := value.ReviewRecord.Validate(); err != nil {
		return err
	}
	uri, err := validateEnvelope(value.URI, value.CID, value.AuthorDID, ReviewCollection)
	if err != nil {
		return err
	}
	return ValidateRecordKey(uri.RecordKey().String())
}

// RepositoryOwnerDID returns the canonical DID authority of a repository reference.
func RepositoryOwnerDID(repositoryURI string) (string, error) {
	uri, err := validateATURI(repositoryURI, repositoryCollection, "targetRepository.uri")
	if err != nil {
		return "", err
	}
	return uri.Authority().String(), nil
}

// StatusRecordKey derives the deterministic status rkey for a canonical pull request URI.
func StatusRecordKey(pullRequestURI string) (string, error) {
	if _, err := validateATURI(pullRequestURI, Collection, "subject.uri"); err != nil {
		return "", err
	}
	digest := sha256.Sum256([]byte(StatusCollection + "\x00" + pullRequestURI))
	return strings.ToLower(base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(digest[:])), nil
}

// RandomRecordKey creates a retry-stable key from a compact UUIDv7 value.
func RandomRecordKey() (string, error) {
	id, err := uuid.NewV7()
	if err != nil {
		return "", fmt.Errorf("generate pull request record key: %w", err)
	}
	return strings.ReplaceAll(id.String(), "-", ""), nil
}

// ValidateRecordKey checks a caller-generated record key.
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

func validateBranch(value, field string) error {
	if len(value) == 0 || len(value) > maximumBranchLength || !utf8.ValidString(value) || value == "@" || strings.HasPrefix(value, "-") || strings.HasPrefix(value, "/") || strings.HasSuffix(value, "/") || strings.HasSuffix(value, ".") || strings.Contains(value, "//") || strings.Contains(value, "..") || strings.Contains(value, "@{") {
		return &ValidationError{Field: field, Problem: "must be a safe Git branch name containing at most 255 bytes"}
	}
	for _, component := range strings.Split(value, "/") {
		if strings.HasPrefix(component, ".") || strings.HasSuffix(component, ".lock") {
			return &ValidationError{Field: field, Problem: "must be a safe Git branch name containing at most 255 bytes"}
		}
	}
	for _, character := range value {
		if unicode.IsSpace(character) || unicode.IsControl(character) || strings.ContainsRune("~^:?*[\\", character) {
			return &ValidationError{Field: field, Problem: "must be a safe Git branch name containing at most 255 bytes"}
		}
	}
	return nil
}

func validateGitSHA(value, field string) error {
	if !gitSHAPattern.MatchString(value) || strings.Trim(value, "0") == "" {
		return &ValidationError{Field: field, Problem: "must be a lowercase 40 or 64 character hexadecimal Git object ID"}
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
