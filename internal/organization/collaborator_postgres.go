package organization

import (
	"context"
	"fmt"
	"time"

	dbgen "github.com/adenosine-dev/adenosine/internal/database/generated"
	"github.com/google/uuid"
)

func (store *PostgresStore) CanAdminRepository(ctx context.Context, organizationID ID, repositoryID uuid.UUID, accountDID string) (bool, error) {
	allowed, err := store.queries.CanAdminOrganizationRepository(ctx, dbgen.CanAdminOrganizationRepositoryParams{
		OrganizationID: pgUUID(organizationID), RepositoryID: pgRawUUID(repositoryID), AccountDid: accountDID,
	})
	if err != nil {
		return false, mapStoreError(err)
	}
	return allowed, nil
}

func (store *PostgresStore) PutRepositoryCollaborator(ctx context.Context, organizationID ID, repositoryID uuid.UUID, accountDID string, role RepositoryRole, now time.Time) (RepositoryCollaborator, error) {
	tx, err := store.db.Begin(ctx)
	if err != nil {
		return RepositoryCollaborator{}, fmt.Errorf("begin repository collaborator update: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	queries := dbgen.New(tx)
	if err := queries.EnsureAccount(ctx, dbgen.EnsureAccountParams{Did: accountDID, SeenAt: pgTime(now)}); err != nil {
		return RepositoryCollaborator{}, mapStoreError(err)
	}
	row, err := queries.PutOrganizationRepositoryCollaborator(ctx, dbgen.PutOrganizationRepositoryCollaboratorParams{
		AccountDid: accountDID, Role: string(role), UpdatedAt: pgTime(now),
		RepositoryID: pgRawUUID(repositoryID), OrganizationID: pgUUID(organizationID),
	})
	if err != nil {
		return RepositoryCollaborator{}, mapStoreError(err)
	}
	if err := tx.Commit(ctx); err != nil {
		return RepositoryCollaborator{}, fmt.Errorf("commit repository collaborator update: %w", err)
	}
	return collaboratorFromRow(row, "", ""), nil
}

func (store *PostgresStore) ListRepositoryCollaborators(ctx context.Context, organizationID ID, repositoryID uuid.UUID, after string, limit int32) ([]RepositoryCollaborator, error) {
	rows, err := store.queries.ListOrganizationRepositoryCollaborators(ctx, dbgen.ListOrganizationRepositoryCollaboratorsParams{
		OrganizationID: pgUUID(organizationID), RepositoryID: pgRawUUID(repositoryID),
		AfterDid: pgText(after), PageLimit: limit,
	})
	if err != nil {
		return nil, mapStoreError(err)
	}
	result := make([]RepositoryCollaborator, len(rows))
	for index, row := range rows {
		result[index] = RepositoryCollaborator{RepositoryID: uuid.UUID(row.RepositoryID.Bytes), RepositorySlug: row.RepositorySlug, AccountDID: row.AccountDid, Handle: row.HandleCache.String, Role: RepositoryRole(row.Role), CreatedAt: row.CreatedAt.Time, UpdatedAt: row.UpdatedAt.Time}
	}
	return result, nil
}

func (store *PostgresStore) RemoveRepositoryCollaborator(ctx context.Context, organizationID ID, repositoryID uuid.UUID, accountDID string) error {
	count, err := store.queries.DeleteOrganizationRepositoryCollaborator(ctx, dbgen.DeleteOrganizationRepositoryCollaboratorParams{RepositoryID: pgRawUUID(repositoryID), AccountDid: accountDID, OrganizationID: pgUUID(organizationID)})
	if err != nil {
		return mapStoreError(err)
	}
	if count == 0 {
		return ErrNotFound
	}
	return nil
}

func collaboratorFromRow(row dbgen.CoreRepositoryCollaborator, handle, slug string) RepositoryCollaborator {
	return RepositoryCollaborator{RepositoryID: uuid.UUID(row.RepositoryID.Bytes), RepositorySlug: slug, AccountDID: row.AccountDid, Handle: handle, Role: RepositoryRole(row.Role), CreatedAt: row.CreatedAt.Time, UpdatedAt: row.UpdatedAt.Time}
}
