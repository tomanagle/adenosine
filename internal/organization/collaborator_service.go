package organization

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
)

type collaboratorStore interface {
	CanAdminRepository(context.Context, ID, uuid.UUID, string) (bool, error)
	PutRepositoryCollaborator(context.Context, ID, uuid.UUID, string, RepositoryRole, time.Time) (RepositoryCollaborator, error)
	ListRepositoryCollaborators(context.Context, ID, uuid.UUID, string, int32) ([]RepositoryCollaborator, error)
	RemoveRepositoryCollaborator(context.Context, ID, uuid.UUID, string) error
	RecordAudit(context.Context, AuditEvent) error
}

type CollaboratorService struct {
	store collaboratorStore
	clock clock
}

func NewCollaboratorService(store collaboratorStore, clock clock) *CollaboratorService {
	return &CollaboratorService{store: store, clock: clock}
}

func (service *CollaboratorService) List(ctx context.Context, organizationID ID, repositoryID uuid.UUID, actorDID, after string, limit int) (CollaboratorPage, error) {
	if limit < 1 || limit > 100 {
		return CollaboratorPage{}, fmt.Errorf("%w: collaborator limit must be between 1 and 100", ErrValidation)
	}
	if err := service.authorize(ctx, organizationID, repositoryID, actorDID); err != nil {
		return CollaboratorPage{}, err
	}
	values, err := service.store.ListRepositoryCollaborators(ctx, organizationID, repositoryID, after, int32(limit+1))
	if err != nil {
		return CollaboratorPage{}, fmt.Errorf("list repository collaborators: %w", err)
	}
	page := CollaboratorPage{Items: values}
	if len(values) > limit {
		page.Items = values[:limit]
		next := page.Items[len(page.Items)-1].AccountDID
		page.NextCursor = &next
	}
	if page.Items == nil {
		page.Items = []RepositoryCollaborator{}
	}
	return page, nil
}

func (service *CollaboratorService) Put(ctx context.Context, organizationID ID, repositoryID uuid.UUID, actorDID, collaboratorDID string, role RepositoryRole) (RepositoryCollaborator, error) {
	if err := role.Validate(); err != nil {
		return RepositoryCollaborator{}, fmt.Errorf("%w: %v", ErrValidation, err)
	}
	if err := validateDID(collaboratorDID); err != nil {
		return RepositoryCollaborator{}, fmt.Errorf("%w: collaborator DID: %v", ErrValidation, err)
	}
	if err := service.authorize(ctx, organizationID, repositoryID, actorDID); err != nil {
		return RepositoryCollaborator{}, err
	}
	value, err := service.store.PutRepositoryCollaborator(ctx, organizationID, repositoryID, collaboratorDID, role, service.clock.Now().UTC())
	if err != nil {
		return RepositoryCollaborator{}, fmt.Errorf("put repository collaborator: %w", err)
	}
	if err := recordOrganizationAudit(ctx, service.store, service.clock, organizationID, actorDID, "repository.collaborator.put", "member", collaboratorDID, map[string]any{"repository_id": repositoryID.String(), "role": role}); err != nil {
		return RepositoryCollaborator{}, err
	}
	return value, nil
}

func (service *CollaboratorService) Remove(ctx context.Context, organizationID ID, repositoryID uuid.UUID, actorDID, collaboratorDID string) error {
	if err := service.authorize(ctx, organizationID, repositoryID, actorDID); err != nil {
		return err
	}
	if err := service.store.RemoveRepositoryCollaborator(ctx, organizationID, repositoryID, collaboratorDID); err != nil {
		return fmt.Errorf("remove repository collaborator: %w", err)
	}
	return recordOrganizationAudit(ctx, service.store, service.clock, organizationID, actorDID, "repository.collaborator.remove", "member", collaboratorDID, map[string]any{"repository_id": repositoryID.String()})
}

func (service *CollaboratorService) authorize(ctx context.Context, organizationID ID, repositoryID uuid.UUID, actorDID string) error {
	allowed, err := service.store.CanAdminRepository(ctx, organizationID, repositoryID, actorDID)
	if err != nil {
		return fmt.Errorf("authorize repository administrator: %w", err)
	}
	if !allowed {
		return ErrForbidden
	}
	return nil
}
