// Package transfer coordinates bilateral repository ownership transfers.
package transfer

import (
	"context"
	"crypto/sha256"
	"encoding/base32"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/adenosine-dev/adenosine/internal/repository"
	"github.com/google/uuid"
)

var recordKeyEncoding = base32.StdEncoding.WithPadding(base32.NoPadding)

const (
	ProposalCollection   = "dev.adenosine.repositoryTransfer"
	AcceptanceCollection = "dev.adenosine.repositoryTransferAcceptance"
	Lifetime             = 7 * 24 * time.Hour
)

var (
	ErrNotFound   = errors.New("repository transfer not found")
	ErrForbidden  = errors.New("repository transfer forbidden")
	ErrConflict   = errors.New("repository transfer conflict")
	ErrValidation = errors.New("repository transfer validation failed")
	ErrProvider   = errors.New("repository transfer provider unavailable")
)

// ProposalRecordKey derives the deterministic proposal key from its local UUID.
func ProposalRecordKey(id uuid.UUID) string { return strings.ReplaceAll(id.String(), "-", "") }

// AcceptanceRecordKey binds the destination acceptance to one proposal URI.
func AcceptanceRecordKey(proposalURI string) string {
	digest := sha256.Sum256([]byte(proposalURI))
	return strings.ToLower(recordKeyEncoding.EncodeToString(digest[:]))
}

type Status string

const (
	StatusPending   Status = "pending"
	StatusCompleted Status = "completed"
	StatusCancelled Status = "cancelled"
)

type OwnerKind string

const (
	OwnerAccount      OwnerKind = "account"
	OwnerOrganization OwnerKind = "organization"
)

// Owner is a resolved destination in the shared owner-route namespace.
type Owner struct {
	Kind            OwnerKind
	Alias           string
	AccountDID      string
	OrganizationID  *uuid.UUID
	Organization    *repository.ATIdentity
	RecordAuthorDID string
}

// Identity is a portable AT Protocol strong reference.
type Identity struct {
	URI string
	CID string
}

// ProposalPublication is the current-owner-authored portable transfer offer.
type ProposalPublication struct {
	ID                      uuid.UUID
	ActorDID                string
	Repository              Identity
	DestinationDID          string
	DestinationOwnerAlias   string
	DestinationOrganization *repository.ATIdentity
	CreatedAt               time.Time
	ExpiresAt               time.Time
}

// AcceptancePublication is the destination-owner-authored portable acceptance.
type AcceptancePublication struct {
	ActorDID   string
	Proposal   Identity
	Repository Identity
	CreatedAt  time.Time
}

// Transfer is the durable local state of one bilateral ownership change.
type Transfer struct {
	ID                   uuid.UUID
	RepositoryID         repository.ID
	SourceOwnerDID       string
	SourceOrganizationID *uuid.UUID
	SourceOwnerAlias     string
	SourceRepository     *Identity
	Destination          Owner
	InitiatedByDID       string
	AcceptedByDID        string
	Proposal             *Identity
	Successor            *Identity
	Acceptance           *Identity
	SourceRedirectCID    string
	Status               Status
	CreatedAt            time.Time
	ExpiresAt            time.Time
	AcceptanceStartedAt  *time.Time
	AcceptedAt           *time.Time
	CancelledAt          *time.Time
}

type Page struct {
	Items      []Transfer
	NextCursor *uuid.UUID
}

type store interface {
	ResolveOwner(context.Context, string) (Owner, error)
	ResolveOrganizationIdentity(context.Context, uuid.UUID) (repository.ATIdentity, error)
	CanInitiate(context.Context, repository.ID, string) (bool, error)
	CanAccept(context.Context, Owner, string) (bool, error)
	CanComplete(context.Context, uuid.UUID) (bool, error)
	ResolveSourceAlias(context.Context, repository.ID) (string, error)
	GetRepository(context.Context, repository.ID) (repository.Repository, error)
	GetPending(context.Context, repository.ID) (Transfer, error)
	Create(context.Context, Transfer) (Transfer, error)
	Get(context.Context, uuid.UUID) (Transfer, error)
	Page(context.Context, repository.ID, *uuid.UUID, int32) ([]Transfer, error)
	SetProposal(context.Context, uuid.UUID, Identity) (Transfer, error)
	SetSuccessor(context.Context, uuid.UUID, Identity) (Transfer, error)
	StartAcceptance(context.Context, uuid.UUID, time.Time) (Transfer, error)
	SetAcceptance(context.Context, uuid.UUID, Identity) (Transfer, error)
	SetSourceRedirect(context.Context, uuid.UUID, string) (Transfer, error)
	Cancel(context.Context, uuid.UUID, time.Time) (Transfer, error)
	Complete(context.Context, uuid.UUID, uuid.UUID, uuid.UUID, string, time.Time) (Transfer, error)
	CompletePrivate(context.Context, uuid.UUID, uuid.UUID, uuid.UUID, string, time.Time) (Transfer, error)
}

type publisher interface {
	PublishProposal(context.Context, ProposalPublication) (Identity, error)
	DeleteProposal(context.Context, ProposalPublication, Identity) error
	PublishAcceptance(context.Context, AcceptancePublication) (Identity, error)
}

type repositoryPublisher interface {
	Publish(context.Context, repository.Publication) (repository.ATIdentity, error)
}
type endpointBuilder interface {
	For(repository.Repository) (web, gitHTTPS, gitSSH string)
}
type idGenerator interface{ New() (uuid.UUID, error) }
type clock interface{ Now() time.Time }

// Service owns authorization, portable publication, and retryable state transitions.
type Service struct {
	store               store
	publisher           publisher
	repositoryPublisher repositoryPublisher
	endpoints           endpointBuilder
	ids                 idGenerator
	clock               clock
}

func NewService(store store, publisher publisher, repositoryPublisher repositoryPublisher, endpoints endpointBuilder, ids idGenerator, clock clock) *Service {
	return &Service{store: store, publisher: publisher, repositoryPublisher: repositoryPublisher, endpoints: endpoints, ids: ids, clock: clock}
}

// Initiate creates or resumes the sole pending transfer for a repository.
func (service *Service) Initiate(ctx context.Context, value repository.Repository, actorDID, destinationAlias string) (Transfer, error) {
	actorDID, destinationAlias = strings.TrimSpace(actorDID), strings.TrimSpace(destinationAlias)
	if actorDID == "" || destinationAlias == "" || value.State != repository.StateActive {
		return Transfer{}, fmt.Errorf("%w: active repository, actor, and destination are required", ErrValidation)
	}
	allowed, err := service.store.CanInitiate(ctx, value.ID, actorDID)
	if err != nil {
		return Transfer{}, fmt.Errorf("authorize current owner: %w", err)
	}
	if !allowed {
		return Transfer{}, ErrForbidden
	}
	sourceOwnerAlias, err := service.store.ResolveSourceAlias(ctx, value.ID)
	if err != nil {
		return Transfer{}, fmt.Errorf("resolve current owner route: %w", err)
	}
	destination, err := service.store.ResolveOwner(ctx, destinationAlias)
	if err != nil {
		return Transfer{}, err
	}
	if sameOwner(value, destination) {
		return Transfer{}, fmt.Errorf("%w: destination already owns repository", ErrValidation)
	}
	pending, err := service.store.GetPending(ctx, value.ID)
	if err == nil {
		if !service.clock.Now().UTC().Before(pending.ExpiresAt) {
			return Transfer{}, ErrConflict
		}
		if !sameDestination(pending.Destination, destination) {
			return Transfer{}, ErrConflict
		}
		pending.Destination = destination
		return service.publishPendingProposal(ctx, pending)
	}
	if !errors.Is(err, ErrNotFound) {
		return Transfer{}, fmt.Errorf("get pending transfer: %w", err)
	}
	id, err := service.ids.New()
	if err != nil {
		return Transfer{}, fmt.Errorf("generate transfer ID: %w", err)
	}
	now := service.clock.Now().UTC()
	result := Transfer{
		ID: id, RepositoryID: value.ID, SourceOwnerDID: value.OwnerDID,
		SourceOrganizationID: value.OrganizationID, SourceOwnerAlias: sourceOwnerAlias,
		Destination: destination, InitiatedByDID: actorDID, Status: StatusPending,
		CreatedAt: now, ExpiresAt: now.Add(Lifetime),
	}
	if value.ATURI != "" && value.ATCID != "" {
		result.SourceRepository = &Identity{URI: value.ATURI, CID: value.ATCID}
	}
	created, err := service.store.Create(ctx, result)
	if err != nil {
		return Transfer{}, fmt.Errorf("create transfer: %w", err)
	}
	created.Destination = destination
	return service.publishPendingProposal(ctx, created)
}

func (service *Service) publishPendingProposal(ctx context.Context, value Transfer) (Transfer, error) {
	if value.SourceRepository == nil || value.Proposal != nil {
		return value, nil
	}
	if service.publisher == nil {
		return Transfer{}, fmt.Errorf("%w: proposal publisher is unavailable", ErrProvider)
	}
	proposal, err := service.publisher.PublishProposal(ctx, proposalPublication(value))
	if err != nil {
		return Transfer{}, providerFailure("publish proposal", err)
	}
	value, err = service.store.SetProposal(ctx, value.ID, proposal)
	if err != nil {
		return Transfer{}, fmt.Errorf("store proposal identity: %w", err)
	}
	return value, nil
}

func (service *Service) Get(ctx context.Context, id uuid.UUID, actorDID string) (Transfer, error) {
	value, err := service.store.Get(ctx, id)
	if err != nil {
		return Transfer{}, err
	}
	if err := service.authorizeInspection(ctx, value, actorDID); err != nil {
		return Transfer{}, err
	}
	return value, nil
}

func (service *Service) Page(ctx context.Context, repositoryID repository.ID, actorDID string, after *uuid.UUID, limit int) (Page, error) {
	if limit < 1 || limit > 100 {
		return Page{}, fmt.Errorf("%w: limit must be between 1 and 100", ErrValidation)
	}
	allowed, err := service.store.CanInitiate(ctx, repositoryID, strings.TrimSpace(actorDID))
	if err != nil {
		return Page{}, fmt.Errorf("authorize transfer history: %w", err)
	}
	if !allowed {
		return Page{}, ErrForbidden
	}
	values, err := service.store.Page(ctx, repositoryID, after, int32(limit+1))
	if err != nil {
		return Page{}, fmt.Errorf("page transfers: %w", err)
	}
	page := Page{Items: values}
	if len(values) > limit {
		page.Items = values[:limit]
		next := page.Items[len(page.Items)-1].ID
		page.NextCursor = &next
	}
	if page.Items == nil {
		page.Items = []Transfer{}
	}
	return page, nil
}

func (service *Service) authorizeInspection(ctx context.Context, value Transfer, actorDID string) error {
	actorDID = strings.TrimSpace(actorDID)
	allowed, err := service.store.CanInitiate(ctx, value.RepositoryID, strings.TrimSpace(actorDID))
	if err != nil {
		return fmt.Errorf("authorize current repository owner: %w", err)
	}
	if allowed {
		return nil
	}
	source := Owner{Kind: OwnerAccount, AccountDID: value.SourceOwnerDID, OrganizationID: value.SourceOrganizationID}
	if value.SourceOrganizationID != nil {
		source.Kind = OwnerOrganization
	}
	allowed, err = service.store.CanAccept(ctx, source, actorDID)
	if err != nil {
		return fmt.Errorf("authorize source owner: %w", err)
	}
	if allowed {
		return nil
	}
	allowed, err = service.store.CanAccept(ctx, value.Destination, strings.TrimSpace(actorDID))
	if err != nil {
		return fmt.Errorf("authorize destination owner: %w", err)
	}
	if !allowed {
		return ErrForbidden
	}
	return nil
}

func (service *Service) AuthorizeAcceptance(ctx context.Context, id uuid.UUID, actorDID string) (Transfer, repository.Repository, error) {
	value, err := service.store.Get(ctx, id)
	if err != nil {
		return Transfer{}, repository.Repository{}, err
	}
	if value.Status != StatusPending || (value.AcceptanceStartedAt == nil && !service.clock.Now().UTC().Before(value.ExpiresAt)) {
		return Transfer{}, repository.Repository{}, ErrConflict
	}
	allowed, err := service.store.CanAccept(ctx, value.Destination, strings.TrimSpace(actorDID))
	if err != nil {
		return Transfer{}, repository.Repository{}, fmt.Errorf("authorize destination owner: %w", err)
	}
	if !allowed {
		return Transfer{}, repository.Repository{}, ErrForbidden
	}
	repo, err := service.store.GetRepository(ctx, value.RepositoryID)
	if err != nil {
		return Transfer{}, repository.Repository{}, err
	}
	return value, repo, nil
}

// Accept publishes the bilateral successor chain and atomically changes local ownership.
func (service *Service) Accept(ctx context.Context, id uuid.UUID, actorDID string) (Transfer, error) {
	value, repo, err := service.AuthorizeAcceptance(ctx, id, actorDID)
	if err != nil {
		return Transfer{}, err
	}
	available, err := service.store.CanComplete(ctx, id)
	if err != nil {
		return Transfer{}, fmt.Errorf("check destination route: %w", err)
	}
	if !available {
		return Transfer{}, ErrConflict
	}
	now := service.clock.Now().UTC()
	if value.AcceptanceStartedAt == nil {
		value, err = service.store.StartAcceptance(ctx, id, now)
		if err != nil {
			return Transfer{}, fmt.Errorf("start transfer acceptance: %w", err)
		}
	}
	workflowAt := value.AcceptanceStartedAt.UTC()
	aliasID := uuid.NewSHA1(id, []byte("source-owner-alias"))
	sourceDIDAliasID := uuid.NewSHA1(id, []byte("source-owner-did-alias"))
	if value.SourceRepository == nil {
		return service.store.CompletePrivate(ctx, id, aliasID, sourceDIDAliasID, actorDID, now)
	}
	if value.Proposal == nil || service.publisher == nil || service.repositoryPublisher == nil || service.endpoints == nil {
		return Transfer{}, fmt.Errorf("%w: portable transfer is not ready", ErrProvider)
	}
	if value.Successor == nil {
		publication, publicationErr := service.successorPublication(ctx, value, repo, workflowAt)
		if publicationErr != nil {
			return Transfer{}, publicationErr
		}
		identity, publishErr := service.repositoryPublisher.Publish(ctx, publication)
		if publishErr != nil {
			return Transfer{}, providerFailure("publish successor", publishErr)
		}
		value, err = service.store.SetSuccessor(ctx, id, Identity{URI: identity.URI, CID: identity.CID})
		if err != nil {
			return Transfer{}, fmt.Errorf("store successor identity: %w", err)
		}
	}
	if value.Acceptance == nil {
		identity, publishErr := service.publisher.PublishAcceptance(ctx, AcceptancePublication{
			ActorDID: value.Destination.RecordAuthorDID, Proposal: *value.Proposal, Repository: *value.Successor, CreatedAt: workflowAt,
		})
		if publishErr != nil {
			return Transfer{}, providerFailure("publish acceptance", publishErr)
		}
		value, err = service.store.SetAcceptance(ctx, id, identity)
		if err != nil {
			return Transfer{}, fmt.Errorf("store acceptance identity: %w", err)
		}
	}
	if value.SourceRedirectCID == "" {
		publication, publicationErr := service.sourceRedirectPublication(ctx, value, repo, workflowAt)
		if publicationErr != nil {
			return Transfer{}, publicationErr
		}
		identity, publishErr := service.repositoryPublisher.Publish(ctx, publication)
		if publishErr != nil {
			return Transfer{}, providerFailure("publish source redirect", publishErr)
		}
		value, err = service.store.SetSourceRedirect(ctx, id, identity.CID)
		if err != nil {
			return Transfer{}, fmt.Errorf("store source redirect: %w", err)
		}
	}
	completed, err := service.store.Complete(ctx, id, aliasID, sourceDIDAliasID, actorDID, now)
	if err != nil {
		return Transfer{}, fmt.Errorf("complete transfer: %w", err)
	}
	return completed, nil
}

func (service *Service) Cancel(ctx context.Context, id uuid.UUID, actorDID string) (Transfer, error) {
	value, err := service.store.Get(ctx, id)
	if err != nil {
		return Transfer{}, err
	}
	allowed, err := service.store.CanInitiate(ctx, value.RepositoryID, actorDID)
	if err != nil {
		return Transfer{}, fmt.Errorf("authorize cancellation: %w", err)
	}
	if !allowed {
		return Transfer{}, ErrForbidden
	}
	if value.Status == StatusCancelled {
		return value, nil
	}
	if value.Status != StatusPending || value.AcceptanceStartedAt != nil {
		return Transfer{}, ErrConflict
	}
	if value.Proposal != nil && service.publisher != nil {
		if err := service.publisher.DeleteProposal(ctx, proposalPublication(value), *value.Proposal); err != nil {
			return Transfer{}, providerFailure("delete proposal", err)
		}
	}
	return service.store.Cancel(ctx, id, service.clock.Now().UTC())
}

func proposalPublication(value Transfer) ProposalPublication {
	return ProposalPublication{
		ID: value.ID, ActorDID: value.SourceOwnerDID, Repository: *value.SourceRepository,
		DestinationDID: value.Destination.AccountDID, DestinationOwnerAlias: value.Destination.Alias,
		DestinationOrganization: value.Destination.Organization, CreatedAt: value.CreatedAt, ExpiresAt: value.ExpiresAt,
	}
}

func (service *Service) successorPublication(ctx context.Context, value Transfer, repo repository.Repository, now time.Time) (repository.Publication, error) {
	repo.OwnerDID, repo.OrganizationID, repo.OrganizationSlug, repo.UpdatedAt = value.Destination.RecordAuthorDID, value.Destination.OrganizationID, value.Destination.Alias, now
	if repo.OrganizationID == nil {
		repo.OrganizationSlug = ""
	}
	web, gitHTTPS, gitSSH := service.endpoints.For(repo)
	publication := repositoryPublication(repo, web, gitHTTPS, gitSSH, now)
	publication.TransferredFrom = &repository.ATIdentity{URI: value.SourceRepository.URI, CID: value.SourceRepository.CID}
	if value.Destination.OrganizationID != nil {
		if value.Destination.Organization != nil {
			publication.Organization = value.Destination.Organization
		} else {
			identity, err := service.store.ResolveOrganizationIdentity(ctx, *value.Destination.OrganizationID)
			if err != nil {
				return repository.Publication{}, fmt.Errorf("resolve destination organization: %w", err)
			}
			publication.Organization = &identity
		}
	}
	return publication, nil
}

func (service *Service) sourceRedirectPublication(ctx context.Context, value Transfer, repo repository.Repository, now time.Time) (repository.Publication, error) {
	if value.SourceOrganizationID != nil {
		repo.OrganizationSlug = value.SourceOwnerAlias
	}
	web, gitHTTPS, gitSSH := service.endpoints.For(repo)
	publication := repositoryPublication(repo, web, gitHTTPS, gitSSH, now)
	publication.OwnerDID = value.SourceOwnerDID
	publication.TransferredTo = &repository.ATIdentity{URI: value.Successor.URI, CID: value.Successor.CID}
	if value.SourceOrganizationID != nil {
		identity, err := service.store.ResolveOrganizationIdentity(ctx, *value.SourceOrganizationID)
		if err != nil {
			return repository.Publication{}, fmt.Errorf("resolve source organization: %w", err)
		}
		publication.Organization = &identity
	}
	return publication, nil
}

func repositoryPublication(value repository.Repository, web, gitHTTPS, gitSSH string, updatedAt time.Time) repository.Publication {
	name := value.DisplayName
	if name == "" {
		name = value.Slug
	}
	return repository.Publication{
		ID: value.ID, OwnerDID: value.OwnerDID, Slug: value.Slug, Name: name, Description: value.Description,
		DefaultBranch: value.DefaultBranch, GitHTTPS: gitHTTPS, GitSSH: gitSSH, Web: web,
		TransferredFrom: value.TransferredFrom,
		CreatedAt:       value.CreatedAt, UpdatedAt: updatedAt,
	}
}

func sameOwner(value repository.Repository, owner Owner) bool {
	if value.OrganizationID != nil || owner.OrganizationID != nil {
		return value.OrganizationID != nil && owner.OrganizationID != nil && *value.OrganizationID == *owner.OrganizationID
	}
	return value.OwnerDID == owner.AccountDID
}

func sameDestination(left, right Owner) bool {
	if left.OrganizationID != nil || right.OrganizationID != nil {
		return left.OrganizationID != nil && right.OrganizationID != nil && *left.OrganizationID == *right.OrganizationID
	}
	return left.AccountDID == right.AccountDID
}

type providerError struct {
	operation string
	cause     error
}

func (value *providerError) Error() string   { return ErrProvider.Error() + ": " + value.operation }
func (value *providerError) Unwrap() []error { return []error{ErrProvider, value.cause} }

func providerFailure(operation string, cause error) error {
	return &providerError{operation: operation, cause: cause}
}
