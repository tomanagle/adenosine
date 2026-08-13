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

type clock interface {
	Now() time.Time
}

type idGenerator interface {
	New() (ID, error)
}

type publisher interface {
	Publish(context.Context, Publication) (ATIdentity, error)
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
		CreatedAt:        now,
		UpdatedAt:        now,
	})
	if err != nil {
		return Repository{}, fmt.Errorf("create repository metadata: %w", err)
	}

	if err := service.git.Init(ctx, repository.ID, repository.DefaultBranch); err != nil {
		return Repository{}, service.fail(ctx, repository.ID, fmt.Errorf("initialize bare repository: %w", err))
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

func (service *Service) fail(ctx context.Context, id ID, cause error) error {
	_, err := service.repositories.UpdateState(context.WithoutCancel(ctx), id, StateFailed, service.clock.Now().UTC())
	if err != nil {
		err = fmt.Errorf("mark repository failed: %w", err)
	}
	return errors.Join(cause, err)
}
