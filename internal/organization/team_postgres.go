package organization

import (
	"context"
	"time"

	dbgen "github.com/adenosine-dev/adenosine/internal/database/generated"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
)

func (store *PostgresStore) CreateTeam(ctx context.Context, value Team) (Team, error) {
	var parentTeamID pgtype.UUID
	if value.ParentTeamID != nil {
		parentTeamID = pgRawUUID(*value.ParentTeamID)
	}
	row, err := store.queries.CreateOrganizationTeam(ctx, dbgen.CreateOrganizationTeamParams{ID: pgUUID(ID(value.ID)), OrganizationID: pgUUID(value.OrganizationID), ParentTeamID: parentTeamID, Slug: value.Slug, Name: value.Name, Description: pgText(value.Description), Visibility: string(value.Visibility), CreatedAt: pgTime(value.CreatedAt)})
	if err != nil {
		return Team{}, mapStoreError(err)
	}
	return teamFromRow(row), nil
}

func (store *PostgresStore) UpdateTeam(ctx context.Context, value Team) (Team, error) {
	var parentTeamID pgtype.UUID
	if value.ParentTeamID != nil {
		parentTeamID = pgRawUUID(*value.ParentTeamID)
	}
	row, err := store.queries.UpdateOrganizationTeam(ctx, dbgen.UpdateOrganizationTeamParams{
		ID: pgRawUUID(value.ID), OrganizationID: pgUUID(value.OrganizationID), ParentTeamID: parentTeamID,
		Name: value.Name, Description: pgText(value.Description), Visibility: string(value.Visibility), UpdatedAt: pgTime(value.UpdatedAt),
	})
	if err != nil {
		return Team{}, mapStoreError(err)
	}
	return teamFromRow(row), nil
}

func (store *PostgresStore) DeleteTeam(ctx context.Context, organizationID ID, teamID uuid.UUID, deletedAt time.Time) (int64, error) {
	count, err := store.queries.DeleteOrganizationTeamHierarchy(ctx, dbgen.DeleteOrganizationTeamHierarchyParams{OrganizationID: pgUUID(organizationID), TeamID: pgRawUUID(teamID), DeletedAt: pgTime(deletedAt)})
	return count, mapStoreError(err)
}

func (store *PostgresStore) TeamHasChildren(ctx context.Context, organizationID ID, teamID uuid.UUID) (bool, error) {
	value, err := store.queries.OrganizationTeamHasChildren(ctx, dbgen.OrganizationTeamHasChildrenParams{OrganizationID: pgUUID(organizationID), TeamID: pgRawUUID(teamID)})
	return value, mapStoreError(err)
}

func (store *PostgresStore) IsTeamDescendant(ctx context.Context, organizationID ID, teamID, candidateID uuid.UUID) (bool, error) {
	values, err := store.queries.ListOrganizationTeamDescendantIDs(ctx, dbgen.ListOrganizationTeamDescendantIDsParams{OrganizationID: pgUUID(organizationID), TeamID: pgRawUUID(teamID)})
	if err != nil {
		return false, mapStoreError(err)
	}
	for _, value := range values {
		if value.Valid && uuid.UUID(value.Bytes) == candidateID {
			return true, nil
		}
	}
	return false, nil
}

func (store *PostgresStore) ListTeams(ctx context.Context, organizationID ID, viewerDID string) ([]Team, error) {
	rows, err := store.queries.ListOrganizationTeams(ctx, dbgen.ListOrganizationTeamsParams{ViewerDid: viewerDID, OrganizationID: pgUUID(organizationID)})
	if err != nil {
		return nil, mapStoreError(err)
	}
	result := make([]Team, len(rows))
	for index, row := range rows {
		result[index] = teamFromListRow(row)
	}
	return result, nil
}

func (store *PostgresStore) PageTeams(ctx context.Context, organizationID ID, viewerDID string, includeSecret bool, after *uuid.UUID, limit int32) ([]Team, error) {
	var afterID pgtype.UUID
	if after != nil {
		afterID = pgRawUUID(*after)
	}
	rows, err := store.queries.PageOrganizationTeams(ctx, dbgen.PageOrganizationTeamsParams{ViewerDid: viewerDID, OrganizationID: pgUUID(organizationID), IncludeSecret: includeSecret, AfterID: afterID, PageLimit: limit})
	if err != nil {
		return nil, mapStoreError(err)
	}
	result := make([]Team, len(rows))
	for index, row := range rows {
		result[index] = teamFromPageRow(row)
	}
	return result, nil
}

func (store *PostgresStore) GetTeam(ctx context.Context, organizationID ID, teamID uuid.UUID) (Team, error) {
	row, err := store.queries.GetOrganizationTeam(ctx, dbgen.GetOrganizationTeamParams{ID: pgtype.UUID{Bytes: teamID, Valid: true}, OrganizationID: pgUUID(organizationID)})
	if err != nil {
		return Team{}, mapStoreError(err)
	}
	return teamFromRow(row), nil
}

func (store *PostgresStore) AddTeamMember(ctx context.Context, teamID uuid.UUID, accountDID string, role TeamRole, now time.Time) (TeamMember, error) {
	row, err := store.queries.AddOrganizationTeamMember(ctx, dbgen.AddOrganizationTeamMemberParams{TeamID: pgtype.UUID{Bytes: teamID, Valid: true}, AccountDid: accountDID, Role: string(role), CreatedAt: pgTime(now)})
	if err != nil {
		return TeamMember{}, mapStoreError(err)
	}
	return teamMemberFromRow(row, ""), nil
}

func (store *PostgresStore) GetTeamMember(ctx context.Context, teamID uuid.UUID, accountDID string) (TeamMember, error) {
	row, err := store.queries.GetOrganizationTeamMember(ctx, dbgen.GetOrganizationTeamMemberParams{TeamID: pgtype.UUID{Bytes: teamID, Valid: true}, AccountDid: accountDID})
	if err != nil {
		return TeamMember{}, mapStoreError(err)
	}
	return teamMemberFromRow(row, ""), nil
}

func (store *PostgresStore) ListTeamMembers(ctx context.Context, teamID uuid.UUID) ([]TeamMember, error) {
	rows, err := store.queries.ListOrganizationTeamMembers(ctx, pgtype.UUID{Bytes: teamID, Valid: true})
	if err != nil {
		return nil, mapStoreError(err)
	}
	result := make([]TeamMember, len(rows))
	for index, row := range rows {
		result[index] = TeamMember{TeamID: uuid.UUID(row.TeamID.Bytes), AccountDID: row.AccountDid, Handle: row.HandleCache.String, Role: TeamRole(row.Role), CreatedAt: row.CreatedAt.Time, UpdatedAt: row.UpdatedAt.Time}
	}
	return result, nil
}

func (store *PostgresStore) PageTeamMembers(ctx context.Context, teamID uuid.UUID, after string, limit int32) ([]TeamMember, error) {
	rows, err := store.queries.PageOrganizationTeamMembers(ctx, dbgen.PageOrganizationTeamMembersParams{TeamID: pgRawUUID(teamID), AfterDid: after, PageLimit: limit})
	if err != nil {
		return nil, mapStoreError(err)
	}
	result := make([]TeamMember, len(rows))
	for index, row := range rows {
		result[index] = TeamMember{TeamID: uuid.UUID(row.TeamID.Bytes), AccountDID: row.AccountDid, Handle: row.HandleCache.String, Role: TeamRole(row.Role), CreatedAt: row.CreatedAt.Time, UpdatedAt: row.UpdatedAt.Time}
	}
	return result, nil
}

func (store *PostgresStore) RemoveTeamMember(ctx context.Context, teamID uuid.UUID, accountDID string) error {
	count, err := store.queries.DeleteOrganizationTeamMember(ctx, dbgen.DeleteOrganizationTeamMemberParams{TeamID: pgtype.UUID{Bytes: teamID, Valid: true}, AccountDid: accountDID})
	if err != nil {
		return mapStoreError(err)
	}
	if count == 0 {
		return ErrNotFound
	}
	return nil
}

func (store *PostgresStore) PutTeamRepository(ctx context.Context, teamID, repositoryID uuid.UUID, role RepositoryRole, now time.Time) (TeamRepository, error) {
	row, err := store.queries.PutOrganizationTeamRepository(ctx, dbgen.PutOrganizationTeamRepositoryParams{TeamID: pgtype.UUID{Bytes: teamID, Valid: true}, RepositoryID: pgtype.UUID{Bytes: repositoryID, Valid: true}, Role: string(role), CreatedAt: pgTime(now)})
	if err != nil {
		return TeamRepository{}, mapStoreError(err)
	}
	repository, err := store.queries.GetRepository(ctx, pgtype.UUID{Bytes: repositoryID, Valid: true})
	if err != nil {
		return TeamRepository{}, mapStoreError(err)
	}
	return teamRepositoryFromRow(row, repository.Slug), nil
}

func (store *PostgresStore) ListTeamRepositories(ctx context.Context, teamID uuid.UUID) ([]TeamRepository, error) {
	rows, err := store.queries.ListOrganizationTeamRepositories(ctx, pgtype.UUID{Bytes: teamID, Valid: true})
	if err != nil {
		return nil, mapStoreError(err)
	}
	result := make([]TeamRepository, len(rows))
	for index, row := range rows {
		result[index] = TeamRepository{TeamID: uuid.UUID(row.TeamID.Bytes), RepositoryID: uuid.UUID(row.RepositoryID.Bytes), RepositorySlug: row.RepositorySlug, Role: RepositoryRole(row.Role), CreatedAt: row.CreatedAt.Time, UpdatedAt: row.UpdatedAt.Time}
	}
	return result, nil
}

func (store *PostgresStore) PageTeamRepositories(ctx context.Context, teamID uuid.UUID, after *uuid.UUID, limit int32) ([]TeamRepository, error) {
	var afterID pgtype.UUID
	if after != nil {
		afterID = pgRawUUID(*after)
	}
	rows, err := store.queries.PageOrganizationTeamRepositories(ctx, dbgen.PageOrganizationTeamRepositoriesParams{TeamID: pgRawUUID(teamID), AfterID: afterID, PageLimit: limit})
	if err != nil {
		return nil, mapStoreError(err)
	}
	result := make([]TeamRepository, len(rows))
	for index, row := range rows {
		result[index] = TeamRepository{TeamID: uuid.UUID(row.TeamID.Bytes), RepositoryID: uuid.UUID(row.RepositoryID.Bytes), RepositorySlug: row.RepositorySlug, Role: RepositoryRole(row.Role), CreatedAt: row.CreatedAt.Time, UpdatedAt: row.UpdatedAt.Time}
	}
	return result, nil
}

func (store *PostgresStore) RemoveTeamRepository(ctx context.Context, teamID, repositoryID uuid.UUID) error {
	count, err := store.queries.DeleteOrganizationTeamRepository(ctx, dbgen.DeleteOrganizationTeamRepositoryParams{TeamID: pgtype.UUID{Bytes: teamID, Valid: true}, RepositoryID: pgtype.UUID{Bytes: repositoryID, Valid: true}})
	if err != nil {
		return mapStoreError(err)
	}
	if count == 0 {
		return ErrNotFound
	}
	return nil
}

func teamRepositoryFromRow(row dbgen.CoreOrganizationTeamRepository, slug string) TeamRepository {
	return TeamRepository{TeamID: uuid.UUID(row.TeamID.Bytes), RepositoryID: uuid.UUID(row.RepositoryID.Bytes), RepositorySlug: slug, Role: RepositoryRole(row.Role), CreatedAt: row.CreatedAt.Time, UpdatedAt: row.UpdatedAt.Time}
}

func teamFromRow(row dbgen.CoreOrganizationTeam) Team {
	value := Team{ID: uuid.UUID(row.ID.Bytes), OrganizationID: ID(row.OrganizationID.Bytes), Slug: row.Slug, Name: row.Name, Description: row.Description.String, Visibility: TeamVisibility(row.Visibility), CreatedAt: row.CreatedAt.Time, UpdatedAt: row.UpdatedAt.Time}
	if row.ParentTeamID.Valid {
		id := uuid.UUID(row.ParentTeamID.Bytes)
		value.ParentTeamID = &id
	}
	return value
}

func teamFromListRow(row dbgen.ListOrganizationTeamsRow) Team {
	value := Team{ID: uuid.UUID(row.ID.Bytes), OrganizationID: ID(row.OrganizationID.Bytes), Slug: row.Slug, Name: row.Name, Description: row.Description.String, Visibility: TeamVisibility(row.Visibility), CreatedAt: row.CreatedAt.Time, UpdatedAt: row.UpdatedAt.Time}
	if row.ParentTeamID.Valid {
		id := uuid.UUID(row.ParentTeamID.Bytes)
		value.ParentTeamID = &id
	}
	value.ViewerIsMember = row.ViewerIsMember
	value.ViewerRole = TeamRole(row.ViewerRole)
	return value
}

func teamFromPageRow(row dbgen.PageOrganizationTeamsRow) Team {
	value := Team{ID: uuid.UUID(row.ID.Bytes), OrganizationID: ID(row.OrganizationID.Bytes), Slug: row.Slug, Name: row.Name, Description: row.Description.String, Visibility: TeamVisibility(row.Visibility), CreatedAt: row.CreatedAt.Time, UpdatedAt: row.UpdatedAt.Time}
	if row.ParentTeamID.Valid {
		id := uuid.UUID(row.ParentTeamID.Bytes)
		value.ParentTeamID = &id
	}
	value.ViewerIsMember = row.ViewerIsMember
	value.ViewerRole = TeamRole(row.ViewerRole)
	return value
}

func teamMemberFromRow(row dbgen.CoreOrganizationTeamMember, handle string) TeamMember {
	return TeamMember{TeamID: uuid.UUID(row.TeamID.Bytes), AccountDID: row.AccountDid, Handle: handle, Role: TeamRole(row.Role), CreatedAt: row.CreatedAt.Time, UpdatedAt: row.UpdatedAt.Time}
}
