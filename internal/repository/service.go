package repository

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
)

type repositoryStore interface {
	Create(context.Context, Repository) (Repository, error)
	GetByOwnerSlug(context.Context, string, string) (Repository, error)
	ListByOrganization(context.Context, uuid.UUID) ([]Repository, error)
	PageByOrganization(context.Context, uuid.UUID, string, *uuid.UUID, int32) ([]Repository, error)
	UpdateState(context.Context, ID, State, time.Time) (Repository, error)
	Activate(context.Context, ID, *ATIdentity, time.Time) (Repository, error)
}

type repositoryLifecycleStore interface {
	UpdateSettings(context.Context, ID, SettingsInput, uuid.UUID, *time.Time, *ATIdentity, time.Time) (Repository, error)
	RequestDeletion(context.Context, Deletion) (Deletion, error)
	GetDeletion(context.Context, uuid.UUID) (Deletion, error)
	RestoreDeletion(context.Context, uuid.UUID, time.Time) (Repository, error)
}

func (service *Service) ListByOrganization(ctx context.Context, organizationID uuid.UUID) ([]Repository, error) {
	repositories, err := service.repositories.ListByOrganization(ctx, organizationID)
	if err != nil {
		return nil, fmt.Errorf("list organization repositories: %w", err)
	}
	return repositories, nil
}

func (service *Service) PageByOrganization(ctx context.Context, organizationID uuid.UUID, actorDID string, after *uuid.UUID, limit int) (Page, error) {
	if limit < 1 || limit > 100 {
		return Page{}, fmt.Errorf("%w: limit must be between 1 and 100", ErrValidation)
	}
	values, err := service.repositories.PageByOrganization(ctx, organizationID, actorDID, after, int32(limit+1))
	if err != nil {
		return Page{}, fmt.Errorf("page organization repositories: %w", err)
	}
	page := Page{Items: values}
	if len(values) > limit {
		page.Items = values[:limit]
		next := uuid.UUID(page.Items[len(page.Items)-1].ID)
		page.NextCursor = &next
	}
	if page.Items == nil {
		page.Items = []Repository{}
	}
	return page, nil
}

// GetByOwnerSlug resolves a current repository route.
func (service *Service) GetByOwnerSlug(ctx context.Context, ownerDID, slug string) (Repository, error) {
	repository, err := service.repositories.GetByOwnerSlug(ctx, ownerDID, slug)
	if err != nil {
		return Repository{}, fmt.Errorf("get repository by owner and slug: %w", err)
	}
	return repository, nil
}

type gitInitializer interface {
	Init(context.Context, ID, string) error
}

type gitLifecycle interface {
	SetDefaultBranch(context.Context, ID, string) error
	Quarantine(context.Context, ID) error
	Restore(context.Context, ID) error
}

type gitForker interface {
	Fork(context.Context, ID, ForkSource, string) error
	SyncFork(context.Context, ID, ForkSource, string) (ForkSync, error)
}

type forkSourceStore interface {
	GetForkSourceByURI(context.Context, string) (ForkSource, error)
}

type organizationIdentityStore interface {
	GetOrganizationIdentity(context.Context, uuid.UUID) (ATIdentity, error)
}

type clock interface {
	Now() time.Time
}

type idGenerator interface {
	New() (ID, error)
}

type publisher interface {
	Publish(context.Context, Publication) (ATIdentity, error)
}

type unpublisher interface {
	Unpublish(context.Context, Publication, ATIdentity) error
}

type endpointBuilder interface {
	For(Repository) (web, gitHTTPS, gitSSH string)
}

// Service coordinates repository metadata with Git storage.
type Service struct {
	repositories repositoryStore
	git          gitInitializer
	clock        clock
	ids          idGenerator
	publisher    publisher
	endpoints    endpointBuilder
}

// NewService constructs the repository application service.
func NewService(repositories repositoryStore, git gitInitializer, clock clock, ids idGenerator, publisher publisher, endpoints endpointBuilder) *Service {
	return &Service{repositories: repositories, git: git, clock: clock, ids: ids, publisher: publisher, endpoints: endpoints}
}

// Create persists metadata, initializes a bare repository, and activates it.
func (service *Service) Create(ctx context.Context, input CreateInput) (Repository, error) {
	if err := input.Validate(); err != nil {
		return Repository{}, fmt.Errorf("%w: %v", ErrValidation, err)
	}
	id, err := service.ids.New()
	if err != nil {
		return Repository{}, fmt.Errorf("generate repository ID: %w", err)
	}
	now := service.clock.Now().UTC()
	repository, err := service.repositories.Create(ctx, Repository{
		ID:               id,
		OwnerDID:         input.OwnerDID,
		OrganizationID:   input.OrganizationID,
		OrganizationSlug: input.OrganizationSlug,
		Slug:             input.Slug,
		DisplayName:      input.DisplayName,
		Description:      input.Description,
		Visibility:       input.Visibility,
		State:            StateCreating,
		DefaultBranch:    input.DefaultBranch,
		StorageKey:       id.String(),
		ForkedFrom:       input.ForkedFrom,
		CreatedAt:        now,
		UpdatedAt:        now,
	})
	if err != nil {
		return Repository{}, fmt.Errorf("create repository metadata: %w", err)
	}

	var gitErr error
	if input.ForkedFrom == nil {
		gitErr = service.git.Init(ctx, repository.ID, repository.DefaultBranch)
	} else {
		forker, ok := service.git.(gitForker)
		if !ok {
			gitErr = errors.New("Git fork support is unavailable")
		} else {
			gitErr = forker.Fork(ctx, repository.ID, *input.ForkedFrom, repository.DefaultBranch)
		}
	}
	if gitErr != nil {
		return Repository{}, service.fail(ctx, repository.ID, fmt.Errorf("initialize bare repository: %w", gitErr))
	}

	var identity *ATIdentity
	if repository.Visibility == VisibilityPublic {
		web, gitHTTPS, gitSSH := service.endpoints.For(repository)
		name := repository.DisplayName
		if name == "" {
			name = repository.Slug
		}
		published, publishErr := service.publisher.Publish(ctx, Publication{
			ID: repository.ID, OwnerDID: repository.OwnerDID, Slug: repository.Slug, Name: name,
			Organization: input.OrganizationAT,
			ForkedFrom:   forkPublicationReference(input.ForkedFrom),
			Description:  repository.Description, DefaultBranch: repository.DefaultBranch,
			GitHTTPS: gitHTTPS, GitSSH: gitSSH, Web: web,
			CreatedAt: repository.CreatedAt, UpdatedAt: repository.UpdatedAt,
		})
		if publishErr != nil {
			return Repository{}, service.fail(ctx, repository.ID, fmt.Errorf("publish repository record: %w", publishErr))
		}
		identity = &published
	}

	repository, err = service.repositories.Activate(ctx, repository.ID, identity, service.clock.Now().UTC())
	if err != nil {
		return Repository{}, service.fail(ctx, repository.ID, fmt.Errorf("activate repository: %w", err))
	}
	return repository, nil
}

// Update replaces mutable repository settings and keeps public discovery in sync.
func (service *Service) Update(ctx context.Context, current Repository, input SettingsInput) (Repository, error) {
	if current.State != StateActive {
		return Repository{}, ErrNotFound
	}
	if err := input.Validate(); err != nil {
		return Repository{}, fmt.Errorf("%w: %v", ErrValidation, err)
	}
	lifecycle, ok := service.git.(gitLifecycle)
	if !ok {
		return Repository{}, fmt.Errorf("%w: repository lifecycle support is unavailable", ErrValidation)
	}
	store, ok := service.repositories.(repositoryLifecycleStore)
	if !ok {
		return Repository{}, fmt.Errorf("%w: repository lifecycle persistence is unavailable", ErrValidation)
	}
	if input.DefaultBranch != current.DefaultBranch {
		if err := lifecycle.SetDefaultBranch(ctx, current.ID, input.DefaultBranch); err != nil {
			return Repository{}, fmt.Errorf("%w: set default branch: %v", ErrValidation, err)
		}
	}
	branchUpdated := input.DefaultBranch != current.DefaultBranch
	updateComplete := false
	defer func() {
		if branchUpdated && !updateComplete {
			_ = lifecycle.SetDefaultBranch(context.WithoutCancel(ctx), current.ID, current.DefaultBranch)
		}
	}()

	now := service.clock.Now().UTC()
	updated := current
	updated.Slug, updated.DisplayName, updated.Description = input.Slug, input.DisplayName, input.Description
	updated.Visibility, updated.DefaultBranch, updated.UpdatedAt = input.Visibility, input.DefaultBranch, now
	if input.Archived {
		updated.ArchivedAt = &now
	} else {
		updated.ArchivedAt = nil
	}

	var identity *ATIdentity
	if input.Visibility == VisibilityPublic {
		publication, err := service.publication(ctx, updated)
		if err != nil {
			return Repository{}, err
		}
		published, err := service.publisher.Publish(ctx, publication)
		if err != nil {
			return Repository{}, fmt.Errorf("publish updated repository record: %w", err)
		}
		identity = &published
	} else if current.ATURI != "" && current.ATCID != "" {
		remover, available := service.publisher.(unpublisher)
		if !available {
			return Repository{}, fmt.Errorf("%w: repository unpublication is unavailable", ErrValidation)
		}
		publication, err := service.publication(ctx, current)
		if err != nil {
			return Repository{}, err
		}
		if err := remover.Unpublish(ctx, publication, ATIdentity{URI: current.ATURI, CID: current.ATCID}); err != nil {
			return Repository{}, fmt.Errorf("unpublish private repository record: %w", err)
		}
	}

	aliasID, err := service.ids.New()
	if err != nil {
		service.compensatePublication(context.WithoutCancel(ctx), current, updated, identity)
		return Repository{}, fmt.Errorf("generate repository alias ID: %w", err)
	}
	stored, err := store.UpdateSettings(ctx, current.ID, input, uuid.UUID(aliasID), updated.ArchivedAt, identity, now)
	if err != nil {
		service.compensatePublication(context.WithoutCancel(ctx), current, updated, identity)
		return Repository{}, fmt.Errorf("update repository settings: %w", err)
	}
	updateComplete = true
	return stored, nil
}

// Delete quarantines Git data and creates a recoverable deletion resource.
func (service *Service) Delete(ctx context.Context, current Repository, actorDID string, retention time.Duration) (Deletion, error) {
	if current.State != StateActive {
		return Deletion{}, ErrNotFound
	}
	if retention <= 0 {
		return Deletion{}, fmt.Errorf("%w: deletion retention must be positive", ErrValidation)
	}
	lifecycle, ok := service.git.(gitLifecycle)
	if !ok {
		return Deletion{}, fmt.Errorf("%w: repository lifecycle support is unavailable", ErrValidation)
	}
	store, ok := service.repositories.(repositoryLifecycleStore)
	if !ok {
		return Deletion{}, fmt.Errorf("%w: repository lifecycle persistence is unavailable", ErrValidation)
	}
	if current.ATURI != "" && current.ATCID != "" {
		remover, available := service.publisher.(unpublisher)
		if !available {
			return Deletion{}, fmt.Errorf("%w: repository unpublication is unavailable", ErrValidation)
		}
		publication, err := service.publication(ctx, current)
		if err != nil {
			return Deletion{}, err
		}
		if err := remover.Unpublish(ctx, publication, ATIdentity{URI: current.ATURI, CID: current.ATCID}); err != nil {
			return Deletion{}, fmt.Errorf("unpublish deleted repository record: %w", err)
		}
	}
	if err := lifecycle.Quarantine(ctx, current.ID); err != nil {
		return Deletion{}, fmt.Errorf("quarantine repository: %w", err)
	}
	id, err := service.ids.New()
	if err != nil {
		_ = lifecycle.Restore(context.WithoutCancel(ctx), current.ID)
		return Deletion{}, fmt.Errorf("generate repository deletion ID: %w", err)
	}
	now := service.clock.Now().UTC()
	deletion, err := store.RequestDeletion(ctx, Deletion{
		ID: uuid.UUID(id), RepositoryID: current.ID, RequestedByDID: actorDID,
		RequestedAt: now, PurgeAfter: now.Add(retention),
	})
	if err != nil {
		_ = lifecycle.Restore(context.WithoutCancel(ctx), current.ID)
		if current.Visibility == VisibilityPublic {
			if publication, publicationErr := service.publication(context.WithoutCancel(ctx), current); publicationErr == nil {
				_, _ = service.publisher.Publish(context.WithoutCancel(ctx), publication)
			}
		}
		return Deletion{}, fmt.Errorf("request repository deletion: %w", err)
	}
	return deletion, nil
}

func (service *Service) compensatePublication(ctx context.Context, previous, attempted Repository, attemptedIdentity *ATIdentity) {
	if previous.Visibility == VisibilityPublic {
		if publication, err := service.publication(ctx, previous); err == nil {
			_, _ = service.publisher.Publish(ctx, publication)
		}
		return
	}
	if attemptedIdentity != nil {
		if remover, ok := service.publisher.(unpublisher); ok {
			if publication, err := service.publication(ctx, attempted); err == nil {
				_ = remover.Unpublish(ctx, publication, *attemptedIdentity)
			}
		}
	}
}

// GetDeletion returns an active recoverable deletion resource.
func (service *Service) GetDeletion(ctx context.Context, id uuid.UUID) (Deletion, error) {
	store, ok := service.repositories.(repositoryLifecycleStore)
	if !ok {
		return Deletion{}, fmt.Errorf("%w: repository lifecycle persistence is unavailable", ErrValidation)
	}
	value, err := store.GetDeletion(ctx, id)
	if err != nil {
		return Deletion{}, fmt.Errorf("get repository deletion: %w", err)
	}
	return value, nil
}

// RestoreDeletion restores quarantined Git data before its retention deadline.
func (service *Service) RestoreDeletion(ctx context.Context, id uuid.UUID) (Repository, error) {
	store, ok := service.repositories.(repositoryLifecycleStore)
	if !ok {
		return Repository{}, fmt.Errorf("%w: repository lifecycle persistence is unavailable", ErrValidation)
	}
	deletion, err := store.GetDeletion(ctx, id)
	if err != nil {
		return Repository{}, fmt.Errorf("get repository deletion: %w", err)
	}
	lifecycle, ok := service.git.(gitLifecycle)
	if !ok {
		return Repository{}, fmt.Errorf("%w: repository lifecycle support is unavailable", ErrValidation)
	}
	if err := lifecycle.Restore(ctx, deletion.RepositoryID); err != nil {
		return Repository{}, fmt.Errorf("restore quarantined repository: %w", err)
	}
	restored, err := store.RestoreDeletion(ctx, id, service.clock.Now().UTC())
	if err != nil {
		_ = lifecycle.Quarantine(context.WithoutCancel(ctx), deletion.RepositoryID)
		return Repository{}, fmt.Errorf("restore repository metadata: %w", err)
	}
	if restored.Visibility == VisibilityPublic {
		publication, publicationErr := service.publication(ctx, restored)
		if publicationErr != nil {
			return Repository{}, publicationErr
		}
		published, publishErr := service.publisher.Publish(ctx, publication)
		if publishErr != nil {
			return Repository{}, fmt.Errorf("republish restored repository: %w", publishErr)
		}
		input := SettingsInput{OwnerAlias: routeOwner(restored), Slug: restored.Slug, DisplayName: restored.DisplayName, Description: restored.Description, Visibility: restored.Visibility, DefaultBranch: restored.DefaultBranch, Archived: restored.ArchivedAt != nil}
		aliasID, idErr := service.ids.New()
		if idErr != nil {
			return Repository{}, fmt.Errorf("generate restoration identity update ID: %w", idErr)
		}
		restored, err = store.UpdateSettings(ctx, restored.ID, input, uuid.UUID(aliasID), restored.ArchivedAt, &published, service.clock.Now().UTC())
		if err != nil {
			return Repository{}, fmt.Errorf("store restored repository identity: %w", err)
		}
	}
	return restored, nil
}

func (service *Service) publication(ctx context.Context, value Repository) (Publication, error) {
	web, gitHTTPS, gitSSH := service.endpoints.For(value)
	name := value.DisplayName
	if name == "" {
		name = value.Slug
	}
	publication := Publication{ID: value.ID, OwnerDID: value.OwnerDID, ForkedFrom: forkPublicationReference(value.ForkedFrom), TransferredFrom: value.TransferredFrom, Slug: value.Slug, Name: name, Description: value.Description, DefaultBranch: value.DefaultBranch, GitHTTPS: gitHTTPS, GitSSH: gitSSH, Web: web, CreatedAt: value.CreatedAt, UpdatedAt: value.UpdatedAt}
	if value.OrganizationID != nil {
		store, ok := service.repositories.(organizationIdentityStore)
		if !ok {
			return Publication{}, fmt.Errorf("resolve repository organization identity: unsupported store")
		}
		identity, err := store.GetOrganizationIdentity(ctx, *value.OrganizationID)
		if err != nil {
			return Publication{}, fmt.Errorf("resolve repository organization identity: %w", err)
		}
		publication.Organization = &identity
	}
	return publication, nil
}

func routeOwner(value Repository) string {
	if value.OrganizationSlug != "" {
		return value.OrganizationSlug
	}
	return value.OwnerDID
}

// SyncFork fast-forwards a fork's default branch to its current upstream head.
func (service *Service) SyncFork(ctx context.Context, repository Repository) (ForkSync, error) {
	if repository.ForkedFrom == nil {
		return ForkSync{}, fmt.Errorf("%w: repository is not a fork", ErrValidation)
	}
	source := *repository.ForkedFrom
	if source.LocalRepositoryID == nil {
		store, ok := service.repositories.(forkSourceStore)
		if !ok {
			return ForkSync{}, fmt.Errorf("%w: fork source lookup is unavailable", ErrValidation)
		}
		var err error
		source, err = store.GetForkSourceByURI(ctx, repository.ForkedFrom.URI)
		if err != nil {
			return ForkSync{}, fmt.Errorf("resolve fork source: %w", err)
		}
	}
	forker, ok := service.git.(gitForker)
	if !ok {
		return ForkSync{}, fmt.Errorf("%w: Git fork support is unavailable", ErrValidation)
	}
	result, err := forker.SyncFork(ctx, repository.ID, source, repository.DefaultBranch)
	if err != nil {
		return ForkSync{}, fmt.Errorf("sync fork: %w", err)
	}
	return result, nil
}

func forkPublicationReference(source *ForkSource) *ATIdentity {
	if source == nil {
		return nil
	}
	return &ATIdentity{URI: source.URI, CID: source.CID}
}

func (service *Service) fail(ctx context.Context, id ID, cause error) error {
	_, err := service.repositories.UpdateState(context.WithoutCancel(ctx), id, StateFailed, service.clock.Now().UTC())
	if err != nil {
		err = fmt.Errorf("mark repository failed: %w", err)
	}
	return errors.Join(cause, err)
}
