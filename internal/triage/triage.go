// Package triage defines repository-authoritative labels, milestones, and subject metadata.
package triage

import (
	"context"
	"crypto/sha256"
	"encoding/base32"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/adenosine-dev/adenosine/internal/repository"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/google/uuid"
)

const (
	// LabelCollection contains repository-owner-authored label definitions.
	LabelCollection = "dev.adenosine.repositoryLabel"
	// MilestoneCollection contains repository-owner-authored milestone definitions.
	MilestoneCollection = "dev.adenosine.repositoryMilestone"
	// MetadataCollection contains one repository-owner-authored triage snapshot per subject.
	MetadataCollection    = "dev.adenosine.subjectTriage"
	repositoryCollection  = "dev.adenosine.repo"
	issueCollection       = "dev.adenosine.issue"
	pullRequestCollection = "dev.adenosine.pullRequest"
	maximumLabels         = 20
	maximumAssignees      = 10
)

var (
	// ErrValidation indicates malformed portable triage data.
	ErrValidation = errors.New("triage validation failed")
	// ErrAuthorization indicates that the actor or record author lacks repository triage authority.
	ErrAuthorization = errors.New("repository triage authorization required")
	// ErrConflict indicates a stale or incompatible portable record slot.
	ErrConflict = errors.New("triage record conflict")
	// ErrNotFound indicates a missing repository, subject, label, or milestone.
	ErrNotFound = errors.New("triage resource not found")
	// ErrProvider indicates that the AT Protocol provider could not complete a write.
	ErrProvider  = errors.New("triage provider failure")
	colorPattern = regexp.MustCompile(`^[0-9a-f]{6}$`)
)

// SubjectKind identifies a triageable collaboration resource.
type SubjectKind string

const (
	SubjectIssue       SubjectKind = "issue"
	SubjectPullRequest SubjectKind = "pull_request"
)

// MilestoneState is the repository owner's current milestone state.
type MilestoneState string

const (
	MilestoneOpen   MilestoneState = "open"
	MilestoneClosed MilestoneState = "closed"
)

// StrongRef identifies an immutable observation of a portable record.
type StrongRef struct {
	URI string
	CID string
}

// RepositoryRoute identifies a repository through its public owner and slug route.
type RepositoryRoute struct {
	Owner string
	Slug  string
}

// RepositoryTarget is the current local target used to authorize portable writes.
type RepositoryTarget struct {
	ID         repository.ID
	Repository StrongRef
	OwnerDID   string
}

// LabelRecord is the mutable portable content of a stable label identity.
type LabelRecord struct {
	Repository  StrongRef
	Name        string
	Color       string
	Description string
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// Label combines a label record with its AT Protocol envelope and projection timestamp.
type Label struct {
	URI       string
	CID       string
	AuthorDID string
	RKey      string
	LabelRecord
	IndexedAt time.Time
}

// MilestoneRecord is the mutable portable content of a stable milestone identity.
type MilestoneRecord struct {
	Repository  StrongRef
	Title       string
	Description string
	State       MilestoneState
	DueAt       *time.Time
	ClosedAt    *time.Time
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// Milestone combines a milestone record with its AT Protocol envelope and projection timestamp.
type Milestone struct {
	URI       string
	CID       string
	AuthorDID string
	RKey      string
	MilestoneRecord
	IndexedAt time.Time
}

// Assignee is a visible account attached to a subject.
type Assignee struct {
	DID         string
	Handle      string
	DisplayName string
}

// MetadataRecord is the complete repository-authoritative triage state for one subject.
type MetadataRecord struct {
	Subject      StrongRef
	Kind         SubjectKind
	Repository   StrongRef
	LabelURIs    []string
	AssigneeDIDs []string
	MilestoneURI string
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

// Metadata is the effective, moderated triage projection for one subject.
type Metadata struct {
	URI       string
	CID       string
	AuthorDID string
	RKey      string
	MetadataRecord
	Labels    []Label
	Assignees []Assignee
	Milestone *Milestone
	IndexedAt time.Time
}

// LabelInput contains create or replacement label content.
type LabelInput struct {
	Name        string
	Color       string
	Description string
}

// MilestoneInput contains create or replacement milestone content.
type MilestoneInput struct {
	Title       string
	Description string
	State       MilestoneState
	DueAt       *time.Time
}

// MetadataInput replaces all triage associations for one subject atomically.
type MetadataInput struct {
	LabelIDs     []string
	AssigneeDIDs []string
	MilestoneID  string
}

type subjectTarget struct {
	RepositoryTarget
	Subject  StrongRef
	Metadata *Metadata
}

type store interface {
	ResolveRepository(context.Context, RepositoryRoute) (RepositoryTarget, error)
	ResolveRepositoryForRead(context.Context, RepositoryRoute) (RepositoryTarget, error)
	ListLabels(context.Context, string, string, int, string) ([]Label, error)
	GetLabel(context.Context, string, string, string) (Label, error)
	ListMilestones(context.Context, string, string, int, string) ([]Milestone, error)
	GetMilestone(context.Context, string, string, string) (Milestone, error)
	ResolveSubject(context.Context, RepositoryRoute, SubjectKind, string, string) (subjectTarget, error)
	ResolveSubjectForRead(context.Context, RepositoryRoute, SubjectKind, string, string) (subjectTarget, error)
	ResolveLabelURIs(context.Context, string, []string) ([]string, error)
	ResolveMilestoneURI(context.Context, string, string) (string, error)
	ValidateAssignees(context.Context, []string) error
}

// Publisher writes repository-owner-authored triage records.
type Publisher interface {
	CreateLabel(context.Context, string, string, LabelRecord) (Label, error)
	PutLabel(context.Context, string, string, string, LabelRecord) (Label, error)
	CreateMilestone(context.Context, string, string, MilestoneRecord) (Milestone, error)
	PutMilestone(context.Context, string, string, string, MilestoneRecord) (Milestone, error)
	PutSubjectTriage(context.Context, string, string, MetadataRecord) (Metadata, error)
	DeleteTriageRecord(context.Context, string, string, string, string) error
}

type authorizer interface {
	CanTriageRepository(context.Context, string, repository.ID) (bool, error)
}

type clock interface{ Now() time.Time }

// Service coordinates moderated reads and authorized portable writes.
type Service struct {
	store      store
	publisher  Publisher
	authorizer authorizer
	clock      clock
}

// NewService constructs repository triage orchestration.
func NewService(store store, publisher Publisher, authorizer authorizer, clock clock) *Service {
	return &Service{store: store, publisher: publisher, authorizer: authorizer, clock: clock}
}

// ListLabels returns a keyset page ordered newest first.
func (service *Service) ListLabels(ctx context.Context, route RepositoryRoute, viewerDID string, limit int, cursorURI string) ([]Label, error) {
	target, err := service.store.ResolveRepositoryForRead(ctx, route)
	if err != nil {
		return nil, err
	}
	values, err := service.store.ListLabels(ctx, target.Repository.URI, viewerDID, limit, cursorURI)
	if err != nil {
		return nil, fmt.Errorf("list repository labels: %w", err)
	}
	if values == nil {
		values = []Label{}
	}
	return values, nil
}

// GetLabel returns one effective label by stable record key.
func (service *Service) GetLabel(ctx context.Context, route RepositoryRoute, id, viewerDID string) (Label, error) {
	target, err := service.store.ResolveRepositoryForRead(ctx, route)
	if err != nil {
		return Label{}, err
	}
	return service.store.GetLabel(ctx, target.Repository.URI, id, viewerDID)
}

// CreateLabel publishes a new stable label identity.
func (service *Service) CreateLabel(ctx context.Context, actorDID string, route RepositoryRoute, input LabelInput) (Label, error) {
	target, err := service.authorizedTarget(ctx, actorDID, route)
	if err != nil {
		return Label{}, err
	}
	now := service.clock.Now().UTC()
	record := LabelRecord{Repository: target.Repository, Name: strings.TrimSpace(input.Name), Color: strings.ToLower(strings.TrimPrefix(strings.TrimSpace(input.Color), "#")), Description: strings.TrimSpace(input.Description), CreatedAt: now, UpdatedAt: now}
	if err := record.Validate(); err != nil {
		return Label{}, err
	}
	rkey, err := RandomRecordKey()
	if err != nil {
		return Label{}, err
	}
	return service.publisher.CreateLabel(ctx, target.OwnerDID, rkey, record)
}

// UpdateLabel compare-and-swaps an existing label on the current repository identity.
func (service *Service) UpdateLabel(ctx context.Context, actorDID string, route RepositoryRoute, id string, input LabelInput) (Label, error) {
	target, err := service.authorizedTarget(ctx, actorDID, route)
	if err != nil {
		return Label{}, err
	}
	current, err := service.store.GetLabel(ctx, target.Repository.URI, id, "")
	if err != nil {
		return Label{}, err
	}
	if current.Repository.URI != target.Repository.URI || current.AuthorDID != target.OwnerDID {
		return Label{}, &ConflictError{Err: errors.New("transferred label is read-only under its former authority")}
	}
	record := LabelRecord{Repository: target.Repository, Name: strings.TrimSpace(input.Name), Color: strings.ToLower(strings.TrimPrefix(strings.TrimSpace(input.Color), "#")), Description: strings.TrimSpace(input.Description), CreatedAt: current.CreatedAt, UpdatedAt: service.clock.Now().UTC()}
	if err := record.Validate(); err != nil {
		return Label{}, err
	}
	return service.publisher.PutLabel(ctx, target.OwnerDID, current.RKey, current.CID, record)
}

// DeleteLabel removes a current-owner label definition.
func (service *Service) DeleteLabel(ctx context.Context, actorDID string, route RepositoryRoute, id string) error {
	target, err := service.authorizedTarget(ctx, actorDID, route)
	if err != nil {
		return err
	}
	current, err := service.store.GetLabel(ctx, target.Repository.URI, id, "")
	if err != nil {
		return err
	}
	if current.Repository.URI != target.Repository.URI || current.AuthorDID != target.OwnerDID {
		return &ConflictError{Err: errors.New("transferred label is read-only under its former authority")}
	}
	return service.publisher.DeleteTriageRecord(ctx, target.OwnerDID, LabelCollection, current.RKey, current.CID)
}

// ListMilestones returns a keyset page ordered newest first.
func (service *Service) ListMilestones(ctx context.Context, route RepositoryRoute, viewerDID string, limit int, cursorURI string) ([]Milestone, error) {
	target, err := service.store.ResolveRepositoryForRead(ctx, route)
	if err != nil {
		return nil, err
	}
	values, err := service.store.ListMilestones(ctx, target.Repository.URI, viewerDID, limit, cursorURI)
	if err != nil {
		return nil, fmt.Errorf("list repository milestones: %w", err)
	}
	if values == nil {
		values = []Milestone{}
	}
	return values, nil
}

// GetMilestone returns one effective milestone by stable record key.
func (service *Service) GetMilestone(ctx context.Context, route RepositoryRoute, id, viewerDID string) (Milestone, error) {
	target, err := service.store.ResolveRepositoryForRead(ctx, route)
	if err != nil {
		return Milestone{}, err
	}
	return service.store.GetMilestone(ctx, target.Repository.URI, id, viewerDID)
}

// CreateMilestone publishes a new stable milestone identity.
func (service *Service) CreateMilestone(ctx context.Context, actorDID string, route RepositoryRoute, input MilestoneInput) (Milestone, error) {
	target, err := service.authorizedTarget(ctx, actorDID, route)
	if err != nil {
		return Milestone{}, err
	}
	now := service.clock.Now().UTC()
	record := milestoneRecord(target.Repository, input, now, now, nil)
	if err := record.Validate(); err != nil {
		return Milestone{}, err
	}
	rkey, err := RandomRecordKey()
	if err != nil {
		return Milestone{}, err
	}
	return service.publisher.CreateMilestone(ctx, target.OwnerDID, rkey, record)
}

// UpdateMilestone compare-and-swaps a current-owner milestone.
func (service *Service) UpdateMilestone(ctx context.Context, actorDID string, route RepositoryRoute, id string, input MilestoneInput) (Milestone, error) {
	target, err := service.authorizedTarget(ctx, actorDID, route)
	if err != nil {
		return Milestone{}, err
	}
	current, err := service.store.GetMilestone(ctx, target.Repository.URI, id, "")
	if err != nil {
		return Milestone{}, err
	}
	if current.Repository.URI != target.Repository.URI || current.AuthorDID != target.OwnerDID {
		return Milestone{}, &ConflictError{Err: errors.New("transferred milestone is read-only under its former authority")}
	}
	now := service.clock.Now().UTC()
	record := milestoneRecord(target.Repository, input, current.CreatedAt, now, current.ClosedAt)
	if err := record.Validate(); err != nil {
		return Milestone{}, err
	}
	return service.publisher.PutMilestone(ctx, target.OwnerDID, current.RKey, current.CID, record)
}

// DeleteMilestone removes a current-owner milestone definition.
func (service *Service) DeleteMilestone(ctx context.Context, actorDID string, route RepositoryRoute, id string) error {
	target, err := service.authorizedTarget(ctx, actorDID, route)
	if err != nil {
		return err
	}
	current, err := service.store.GetMilestone(ctx, target.Repository.URI, id, "")
	if err != nil {
		return err
	}
	if current.Repository.URI != target.Repository.URI || current.AuthorDID != target.OwnerDID {
		return &ConflictError{Err: errors.New("transferred milestone is read-only under its former authority")}
	}
	return service.publisher.DeleteTriageRecord(ctx, target.OwnerDID, MilestoneCollection, current.RKey, current.CID)
}

// GetMetadata returns the effective moderated metadata, or an empty representation when unset.
func (service *Service) GetMetadata(ctx context.Context, route RepositoryRoute, kind SubjectKind, subjectURI, viewerDID string) (Metadata, error) {
	target, err := service.store.ResolveSubjectForRead(ctx, route, kind, subjectURI, viewerDID)
	if err != nil {
		return Metadata{}, err
	}
	if target.Metadata != nil {
		return *target.Metadata, nil
	}
	return Metadata{MetadataRecord: MetadataRecord{Subject: target.Subject, Kind: kind, Repository: target.Repository, LabelURIs: []string{}, AssigneeDIDs: []string{}}, Labels: []Label{}, Assignees: []Assignee{}}, nil
}

// PutMetadata atomically replaces labels, assignees, and milestone for one subject.
func (service *Service) PutMetadata(ctx context.Context, actorDID string, route RepositoryRoute, kind SubjectKind, subjectURI string, input MetadataInput) (Metadata, error) {
	target, err := service.store.ResolveSubject(ctx, route, kind, subjectURI, "")
	if err != nil {
		return Metadata{}, err
	}
	if err := service.authorize(ctx, actorDID, target.RepositoryTarget); err != nil {
		return Metadata{}, err
	}
	labelIDs := uniqueSorted(input.LabelIDs)
	assignees := uniqueSorted(input.AssigneeDIDs)
	if len(labelIDs) > maximumLabels {
		return Metadata{}, &ValidationError{Field: "label_ids", Problem: "must contain at most 20 values"}
	}
	if len(assignees) > maximumAssignees {
		return Metadata{}, &ValidationError{Field: "assignee_dids", Problem: "must contain at most 10 values"}
	}
	labelURIs, err := service.store.ResolveLabelURIs(ctx, target.Repository.URI, labelIDs)
	if err != nil {
		return Metadata{}, err
	}
	milestoneURI := ""
	if input.MilestoneID != "" {
		milestoneURI, err = service.store.ResolveMilestoneURI(ctx, target.Repository.URI, input.MilestoneID)
		if err != nil {
			return Metadata{}, err
		}
	}
	if err := service.store.ValidateAssignees(ctx, assignees); err != nil {
		return Metadata{}, err
	}
	now, createdAt := service.clock.Now().UTC(), time.Time{}
	if target.Metadata != nil {
		createdAt = target.Metadata.CreatedAt
	}
	if createdAt.IsZero() {
		createdAt = now
	}
	record := MetadataRecord{Subject: target.Subject, Kind: kind, Repository: target.Repository, LabelURIs: labelURIs, AssigneeDIDs: assignees, MilestoneURI: milestoneURI, CreatedAt: createdAt, UpdatedAt: now}
	if err := record.Validate(); err != nil {
		return Metadata{}, err
	}
	swapCID := ""
	if target.Metadata != nil {
		swapCID = target.Metadata.CID
	}
	return service.publisher.PutSubjectTriage(ctx, target.OwnerDID, swapCID, record)
}

// DeleteMetadata removes all portable triage associations for a subject.
func (service *Service) DeleteMetadata(ctx context.Context, actorDID string, route RepositoryRoute, kind SubjectKind, subjectURI string) error {
	target, err := service.store.ResolveSubject(ctx, route, kind, subjectURI, "")
	if err != nil {
		return err
	}
	if err := service.authorize(ctx, actorDID, target.RepositoryTarget); err != nil {
		return err
	}
	if target.Metadata == nil {
		return nil
	}
	return service.publisher.DeleteTriageRecord(ctx, target.OwnerDID, MetadataCollection, target.Metadata.RKey, target.Metadata.CID)
}

func (service *Service) authorizedTarget(ctx context.Context, actorDID string, route RepositoryRoute) (RepositoryTarget, error) {
	target, err := service.store.ResolveRepository(ctx, route)
	if err != nil {
		return RepositoryTarget{}, err
	}
	if err := service.authorize(ctx, actorDID, target); err != nil {
		return RepositoryTarget{}, err
	}
	return target, nil
}

func (service *Service) authorize(ctx context.Context, actorDID string, target RepositoryTarget) error {
	allowed, err := service.authorizer.CanTriageRepository(ctx, actorDID, target.ID)
	if err != nil {
		return fmt.Errorf("authorize repository triage: %w", err)
	}
	if !allowed {
		return &AuthorizationError{Err: errors.New("actor lacks repository triage permission")}
	}
	return nil
}

func milestoneRecord(repository StrongRef, input MilestoneInput, createdAt, updatedAt time.Time, previousClosedAt *time.Time) MilestoneRecord {
	state := input.State
	if state == "" {
		state = MilestoneOpen
	}
	var closedAt *time.Time
	if state == MilestoneClosed {
		closedAt = previousClosedAt
		if closedAt == nil {
			value := updatedAt
			closedAt = &value
		}
	}
	return MilestoneRecord{Repository: repository, Title: strings.TrimSpace(input.Title), Description: strings.TrimSpace(input.Description), State: state, DueAt: canonicalOptionalTime(input.DueAt), ClosedAt: canonicalOptionalTime(closedAt), CreatedAt: createdAt, UpdatedAt: updatedAt}
}

func canonicalOptionalTime(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	canonical := value.UTC()
	return &canonical
}

// Validate checks label content.
func (record LabelRecord) Validate() error {
	if err := validateStrongRef(record.Repository, repositoryCollection, "repository"); err != nil {
		return err
	}
	if len(record.Name) < 1 || len(record.Name) > 50 || !utf8.ValidString(record.Name) {
		return &ValidationError{Field: "name", Problem: "must contain between 1 and 50 UTF-8 bytes"}
	}
	if !colorPattern.MatchString(record.Color) {
		return &ValidationError{Field: "color", Problem: "must be six lowercase hexadecimal characters"}
	}
	if len(record.Description) > 255 || !utf8.ValidString(record.Description) {
		return &ValidationError{Field: "description", Problem: "must contain at most 255 UTF-8 bytes"}
	}
	return validateTimes(record.CreatedAt, record.UpdatedAt)
}

// Validate checks milestone content.
func (record MilestoneRecord) Validate() error {
	if err := validateStrongRef(record.Repository, repositoryCollection, "repository"); err != nil {
		return err
	}
	if len(record.Title) < 1 || len(record.Title) > 255 || !utf8.ValidString(record.Title) {
		return &ValidationError{Field: "title", Problem: "must contain between 1 and 255 UTF-8 bytes"}
	}
	if len(record.Description) > 65535 || !utf8.ValidString(record.Description) {
		return &ValidationError{Field: "description", Problem: "must contain at most 65535 UTF-8 bytes"}
	}
	if record.State != MilestoneOpen && record.State != MilestoneClosed {
		return &ValidationError{Field: "state", Problem: "must be open or closed"}
	}
	if (record.State == MilestoneClosed) != (record.ClosedAt != nil) {
		return &ValidationError{Field: "closedAt", Problem: "must be present exactly when state is closed"}
	}
	return validateTimes(record.CreatedAt, record.UpdatedAt)
}

// Validate checks a complete subject metadata snapshot.
func (record MetadataRecord) Validate() error {
	collection := issueCollection
	if record.Kind == SubjectPullRequest {
		collection = pullRequestCollection
	} else if record.Kind != SubjectIssue {
		return &ValidationError{Field: "kind", Problem: "must be issue or pull_request"}
	}
	if err := validateStrongRef(record.Subject, collection, "subject"); err != nil {
		return err
	}
	if err := validateStrongRef(record.Repository, repositoryCollection, "repository"); err != nil {
		return err
	}
	if len(record.LabelURIs) > maximumLabels {
		return &ValidationError{Field: "labels", Problem: "must contain at most 20 values"}
	}
	if len(record.AssigneeDIDs) > maximumAssignees {
		return &ValidationError{Field: "assignees", Problem: "must contain at most 10 values"}
	}
	if !slices.IsSorted(record.LabelURIs) || hasDuplicates(record.LabelURIs) {
		return &ValidationError{Field: "labels", Problem: "must be sorted and unique"}
	}
	for _, uri := range record.LabelURIs {
		if _, err := validateATURI(uri, LabelCollection, "labels"); err != nil {
			return err
		}
	}
	if !slices.IsSorted(record.AssigneeDIDs) || hasDuplicates(record.AssigneeDIDs) {
		return &ValidationError{Field: "assignees", Problem: "must be sorted and unique"}
	}
	for _, value := range record.AssigneeDIDs {
		did, err := syntax.ParseDID(value)
		if err != nil || did.String() != value {
			return &ValidationError{Field: "assignees", Problem: "must contain canonical DIDs"}
		}
	}
	if record.MilestoneURI != "" {
		if _, err := validateATURI(record.MilestoneURI, MilestoneCollection, "milestone"); err != nil {
			return err
		}
	}
	return validateTimes(record.CreatedAt, record.UpdatedAt)
}

// Validate checks label authority and envelope identity.
func (value Label) Validate() error {
	return validatePortableEnvelope(value.URI, value.CID, value.AuthorDID, value.RKey, LabelCollection, value.Repository.URI, value.LabelRecord.Validate())
}

// Validate checks milestone authority and envelope identity.
func (value Milestone) Validate() error {
	return validatePortableEnvelope(value.URI, value.CID, value.AuthorDID, value.RKey, MilestoneCollection, value.Repository.URI, value.MilestoneRecord.Validate())
}

// Validate checks metadata authority, deterministic identity, and envelope identity.
func (value Metadata) Validate() error {
	if err := validatePortableEnvelope(value.URI, value.CID, value.AuthorDID, value.RKey, MetadataCollection, value.Repository.URI, value.MetadataRecord.Validate()); err != nil {
		return err
	}
	want, err := MetadataRecordKey(value.Subject.URI)
	if err != nil {
		return err
	}
	if value.RKey != want {
		return &ValidationError{Field: "rkey", Problem: "must be derived from the subject URI"}
	}
	return nil
}

// MetadataRecordKey derives the stable record key for a subject triage slot.
func MetadataRecordKey(subjectURI string) (string, error) {
	uri, err := syntax.ParseATURI(subjectURI)
	if err != nil || uri.String() != subjectURI || (uri.Collection().String() != issueCollection && uri.Collection().String() != pullRequestCollection) {
		return "", &ValidationError{Field: "subject", Problem: "must be a canonical issue or pull request AT URI"}
	}
	digest := sha256.Sum256([]byte(MetadataCollection + "\x00" + subjectURI))
	return strings.ToLower(base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(digest[:])), nil
}

// RandomRecordKey creates a stable caller key from UUIDv7.
func RandomRecordKey() (string, error) {
	id, err := uuid.NewV7()
	if err != nil {
		return "", fmt.Errorf("generate triage record key: %w", err)
	}
	return strings.ReplaceAll(id.String(), "-", ""), nil
}

// ValidateRecordKey checks a caller-provided AT Protocol record key.
func ValidateRecordKey(value string) error {
	rkey, err := syntax.ParseRecordKey(value)
	if err != nil || rkey.String() != value {
		return &ValidationError{Field: "rkey", Problem: "must be a canonical AT Protocol record key"}
	}
	return nil
}

// ValidationError identifies one invalid portable field.
type ValidationError struct{ Field, Problem string }

func (err *ValidationError) Error() string {
	return fmt.Sprintf("%s: %s: %s", ErrValidation, err.Field, err.Problem)
}
func (err *ValidationError) Unwrap() error { return ErrValidation }

// AuthorizationError preserves a private cause while exposing a stable error.
type AuthorizationError struct{ Err error }

func (err *AuthorizationError) Error() string { return ErrAuthorization.Error() }
func (err *AuthorizationError) Unwrap() []error {
	if err.Err == nil {
		return []error{ErrAuthorization}
	}
	return []error{ErrAuthorization, err.Err}
}

// ConflictError preserves a private cause while exposing a stable error.
type ConflictError struct{ Err error }

func (err *ConflictError) Error() string { return ErrConflict.Error() }
func (err *ConflictError) Unwrap() []error {
	if err.Err == nil {
		return []error{ErrConflict}
	}
	return []error{ErrConflict, err.Err}
}

// ProviderError preserves a provider cause while exposing a stable error.
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

func validatePortableEnvelope(uriValue, cid, authorDID, rkey, collection, repositoryURI string, recordErr error) error {
	if recordErr != nil {
		return recordErr
	}
	did, err := syntax.ParseDID(authorDID)
	if err != nil || did.String() != authorDID {
		return &ValidationError{Field: "authorDID", Problem: "must be a canonical DID"}
	}
	uri, err := validateATURI(uriValue, collection, "uri")
	if err != nil {
		return err
	}
	if uri.Authority().String() != authorDID || uri.RecordKey().String() != rkey {
		return &AuthorizationError{Err: errors.New("envelope authority or key does not match")}
	}
	owner, err := RepositoryOwnerDID(repositoryURI)
	if err != nil {
		return err
	}
	if owner != authorDID {
		return &AuthorizationError{Err: errors.New("record author is not repository authority")}
	}
	if err := validateCID(cid); err != nil {
		return err
	}
	return nil
}

// RepositoryOwnerDID returns the DID authority encoded in a repository URI.
func RepositoryOwnerDID(repositoryURI string) (string, error) {
	uri, err := validateATURI(repositoryURI, repositoryCollection, "repository")
	if err != nil {
		return "", err
	}
	return uri.Authority().String(), nil
}

func validateStrongRef(ref StrongRef, collection, field string) error {
	if _, err := validateATURI(ref.URI, collection, field+".uri"); err != nil {
		return err
	}
	if err := validateCID(ref.CID); err != nil {
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

func validateCID(value string) error {
	cid, err := syntax.ParseCID(value)
	if err != nil || cid.String() != value || value != strings.ToLower(value) || !strings.HasPrefix(value, "b") {
		return &ValidationError{Field: "cid", Problem: "must be canonical"}
	}
	return nil
}

func validateTimes(createdAt, updatedAt time.Time) error {
	if createdAt.IsZero() {
		return &ValidationError{Field: "createdAt", Problem: "must not be empty"}
	}
	if updatedAt.IsZero() || updatedAt.Before(createdAt) {
		return &ValidationError{Field: "updatedAt", Problem: "must not precede createdAt"}
	}
	return nil
}

func uniqueSorted(values []string) []string {
	result := append([]string(nil), values...)
	for index := range result {
		result[index] = strings.TrimSpace(result[index])
	}
	slices.Sort(result)
	return slices.Compact(result)
}

func hasDuplicates(values []string) bool {
	for index := 1; index < len(values); index++ {
		if values[index] == values[index-1] {
			return true
		}
	}
	return false
}
