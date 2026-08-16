package repository

import (
	"context"
	"errors"
	"fmt"
	"time"

	dbgen "github.com/adenosine-dev/adenosine/internal/database/generated"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
)

// PostgresStore persists authoritative local repository metadata.
type PostgresStore struct {
	queries *dbgen.Queries
}

// NewPostgresStore constructs a repository store from generated queries.
func NewPostgresStore(queries *dbgen.Queries) *PostgresStore {
	return &PostgresStore{queries: queries}
}

// Create inserts repository metadata.
func (store *PostgresStore) Create(ctx context.Context, repository Repository) (Repository, error) {
	row, err := store.queries.CreateRepository(ctx, dbgen.CreateRepositoryParams{
		ID:                          pgUUID(repository.ID),
		OwnerDid:                    repository.OwnerDID,
		OrganizationID:              pgOptionalUUID(repository.OrganizationID),
		Slug:                        repository.Slug,
		DisplayName:                 pgText(repository.DisplayName),
		Description:                 pgText(repository.Description),
		Visibility:                  string(repository.Visibility),
		State:                       string(repository.State),
		DefaultBranch:               repository.DefaultBranch,
		StorageKey:                  repository.StorageKey,
		AtUri:                       pgText(repository.ATURI),
		AtCid:                       pgText(repository.ATCID),
		ForkedFromUri:               pgText(forkSourceURI(repository.ForkedFrom)),
		ForkedFromCid:               pgText(forkSourceCID(repository.ForkedFrom)),
		ForkedFromLocalRepositoryID: pgForkRepositoryID(repository.ForkedFrom),
		CreatedAt:                   pgTime(repository.CreatedAt),
		UpdatedAt:                   pgTime(repository.UpdatedAt),
	})
	if err != nil {
		return Repository{}, mapStoreError(err)
	}
	created := repositoryFromRow(row)
	created.OrganizationSlug = repository.OrganizationSlug
	return created, nil
}

// GetForkSourceByURI resolves the current safe Git endpoint for a public upstream.
func (store *PostgresStore) GetForkSourceByURI(ctx context.Context, uri string) (ForkSource, error) {
	row, err := store.queries.GetForkSourceByURI(ctx, uri)
	if err != nil {
		return ForkSource{}, mapStoreError(err)
	}
	source := ForkSource{URI: row.Uri, CID: row.Cid.String, GitHTTPS: row.GitHttps.String}
	if row.LocalRepositoryID.Valid {
		id := ID(row.LocalRepositoryID.Bytes)
		source.LocalRepositoryID = &id
	}
	return source, nil
}

// GetOrganizationIdentity returns the strong reference required by organization repository records.
func (store *PostgresStore) GetOrganizationIdentity(ctx context.Context, id uuid.UUID) (ATIdentity, error) {
	row, err := store.queries.GetOrganizationByID(ctx, pgtype.UUID{Bytes: id, Valid: true})
	if err != nil {
		return ATIdentity{}, mapStoreError(err)
	}
	if !row.AtUri.Valid || !row.AtCid.Valid {
		return ATIdentity{}, fmt.Errorf("repository query: organization is not published")
	}
	return ATIdentity{URI: row.AtUri.String, CID: row.AtCid.String}, nil
}

// GetByOwnerSlug loads an active repository route by owner DID and slug.
func (store *PostgresStore) GetByOwnerSlug(ctx context.Context, ownerDID, slug string) (Repository, error) {
	row, err := store.queries.GetRepositoryByOwnerSlug(ctx, dbgen.GetRepositoryByOwnerSlugParams{
		Owner: ownerDID,
		Slug:  slug,
	})
	if err != nil {
		return Repository{}, mapStoreError(err)
	}
	value := repositoryFromRow(row)
	if value.OrganizationID != nil {
		organization, organizationErr := store.queries.GetOrganizationByID(ctx, pgtype.UUID{Bytes: *value.OrganizationID, Valid: true})
		if organizationErr != nil {
			return Repository{}, mapStoreError(organizationErr)
		}
		value.OrganizationSlug = organization.Slug
	}
	return value, nil
}

func (store *PostgresStore) ListByOrganization(ctx context.Context, organizationID uuid.UUID) ([]Repository, error) {
	rows, err := store.queries.ListRepositoriesByOrganization(ctx, pgtype.UUID{Bytes: organizationID, Valid: true})
	if err != nil {
		return nil, mapStoreError(err)
	}
	result := make([]Repository, len(rows))
	for index, row := range rows {
		result[index] = repositoryFromRow(row)
	}
	return result, nil
}

func (store *PostgresStore) PageByOrganization(ctx context.Context, organizationID uuid.UUID, actorDID string, after *uuid.UUID, limit int32) ([]Repository, error) {
	var afterID pgtype.UUID
	if after != nil {
		afterID = pgtype.UUID{Bytes: *after, Valid: true}
	}
	rows, err := store.queries.PageRepositoriesByOrganization(ctx, dbgen.PageRepositoriesByOrganizationParams{
		OrganizationID: pgtype.UUID{Bytes: organizationID, Valid: true}, AccountDid: actorDID, AfterID: afterID, PageLimit: limit,
	})
	if err != nil {
		return nil, mapStoreError(err)
	}
	result := make([]Repository, len(rows))
	for index, row := range rows {
		result[index] = repositoryFromPageRow(row)
	}
	return result, nil
}

// UpdateState persists a repository lifecycle transition.
func (store *PostgresStore) UpdateState(ctx context.Context, id ID, state State, updatedAt time.Time) (Repository, error) {
	row, err := store.queries.UpdateRepositoryState(ctx, dbgen.UpdateRepositoryStateParams{
		ID:        pgUUID(id),
		State:     string(state),
		UpdatedAt: pgTime(updatedAt),
	})
	if err != nil {
		return Repository{}, mapStoreError(err)
	}
	return repositoryFromRow(row), nil
}

// Activate atomically marks a repository active and stores its optional public identity.
func (store *PostgresStore) Activate(ctx context.Context, id ID, identity *ATIdentity, updatedAt time.Time) (Repository, error) {
	var uri, cid string
	if identity != nil {
		uri, cid = identity.URI, identity.CID
	}
	row, err := store.queries.ActivateRepository(ctx, dbgen.ActivateRepositoryParams{
		ID: pgUUID(id), AtUri: pgText(uri), AtCid: pgText(cid), UpdatedAt: pgTime(updatedAt),
	})
	if err != nil {
		return Repository{}, mapStoreError(err)
	}
	return repositoryFromRow(row), nil
}

// UpdateSettings atomically preserves an old slug alias and replaces mutable metadata.
func (store *PostgresStore) UpdateSettings(ctx context.Context, id ID, input SettingsInput, aliasID uuid.UUID, archivedAt *time.Time, identity *ATIdentity, updatedAt time.Time) (Repository, error) {
	var archived pgtype.Timestamptz
	if archivedAt != nil {
		archived = pgTime(*archivedAt)
	}
	var uri, cid string
	if identity != nil {
		uri, cid = identity.URI, identity.CID
	}
	row, err := store.queries.UpdateRepositorySettings(ctx, dbgen.UpdateRepositorySettingsParams{
		ID: pgUUID(id), AliasID: pgtype.UUID{Bytes: aliasID, Valid: true}, OwnerAlias: input.OwnerAlias,
		Slug: input.Slug, DisplayName: pgText(input.DisplayName), Description: pgText(input.Description),
		Visibility: string(input.Visibility), DefaultBranch: input.DefaultBranch, ArchivedAt: archived,
		AtUri: pgText(uri), AtCid: pgText(cid), UpdatedAt: pgTime(updatedAt),
	})
	if err != nil {
		return Repository{}, mapStoreError(err)
	}
	value := repositoryFromRow(row)
	return store.withOrganizationSlug(ctx, value)
}

func (store *PostgresStore) RequestDeletion(ctx context.Context, deletion Deletion) (Deletion, error) {
	row, err := store.queries.RequestRepositoryDeletion(ctx, dbgen.RequestRepositoryDeletionParams{
		ID: pgtype.UUID{Bytes: deletion.ID, Valid: true}, RepositoryID: pgUUID(deletion.RepositoryID),
		RequestedByDid: deletion.RequestedByDID, RequestedAt: pgTime(deletion.RequestedAt), PurgeAfter: pgTime(deletion.PurgeAfter),
	})
	if err != nil {
		return Deletion{}, mapStoreError(err)
	}
	return deletionFromRow(row), nil
}

func (store *PostgresStore) GetDeletion(ctx context.Context, id uuid.UUID) (Deletion, error) {
	row, err := store.queries.GetRepositoryDeletion(ctx, pgtype.UUID{Bytes: id, Valid: true})
	if err != nil {
		return Deletion{}, mapStoreError(err)
	}
	return deletionFromRow(row), nil
}

func (store *PostgresStore) RestoreDeletion(ctx context.Context, id uuid.UUID, restoredAt time.Time) (Repository, error) {
	row, err := store.queries.RestoreRepositoryDeletion(ctx, dbgen.RestoreRepositoryDeletionParams{ID: pgtype.UUID{Bytes: id, Valid: true}, RestoredAt: pgTime(restoredAt)})
	if err != nil {
		return Repository{}, mapStoreError(err)
	}
	return store.withOrganizationSlug(ctx, repositoryFromRow(row))
}

func (store *PostgresStore) ListDueDeletions(ctx context.Context, now time.Time, after *uuid.UUID, limit int32) ([]Deletion, error) {
	rows, err := store.queries.ListDueRepositoryDeletions(ctx, dbgen.ListDueRepositoryDeletionsParams{Now: pgTime(now), AfterID: pgOptionalRawUUID(after), PageLimit: limit})
	if err != nil {
		return nil, fmt.Errorf("repository query: list due deletions: %w", err)
	}
	result := make([]Deletion, len(rows))
	for index, row := range rows {
		result[index] = deletionFromRow(row)
	}
	return result, nil
}

func (store *PostgresStore) MarkPurged(ctx context.Context, id uuid.UUID, purgedAt time.Time) error {
	if err := store.queries.MarkRepositoryPurged(ctx, dbgen.MarkRepositoryPurgedParams{PurgedAt: pgTime(purgedAt), ID: pgtype.UUID{Bytes: id, Valid: true}}); err != nil {
		return fmt.Errorf("repository query: mark purged: %w", err)
	}
	return nil
}

func deletionFromRow(row dbgen.CoreRepositoryDeletion) Deletion {
	return Deletion{ID: uuid.UUID(row.ID.Bytes), RepositoryID: ID(row.RepositoryID.Bytes), RequestedByDID: row.RequestedByDid, RequestedAt: row.RequestedAt.Time, PurgeAfter: row.PurgeAfter.Time}
}

func repositoryFromRow(row dbgen.CoreRepository) Repository {
	value := Repository{
		ID:            ID(row.ID.Bytes),
		OwnerDID:      row.OwnerDid,
		Slug:          row.Slug,
		DisplayName:   row.DisplayName.String,
		Description:   row.Description.String,
		Visibility:    Visibility(row.Visibility),
		State:         State(row.State),
		DefaultBranch: row.DefaultBranch,
		StorageKey:    row.StorageKey,
		ATURI:         row.AtUri.String,
		ATCID:         row.AtCid.String,
		ForkCount:     row.ForkCount,
		CreatedAt:     row.CreatedAt.Time,
		UpdatedAt:     row.UpdatedAt.Time,
	}
	if row.ArchivedAt.Valid {
		archivedAt := row.ArchivedAt.Time
		value.ArchivedAt = &archivedAt
	}
	if row.ForkedFromUri.Valid && row.ForkedFromCid.Valid {
		value.ForkedFrom = &ForkSource{URI: row.ForkedFromUri.String, CID: row.ForkedFromCid.String}
		if row.ForkedFromLocalRepositoryID.Valid {
			id := ID(row.ForkedFromLocalRepositoryID.Bytes)
			value.ForkedFrom.LocalRepositoryID = &id
		}
	}
	if row.TransferredFromUri.Valid && row.TransferredFromCid.Valid {
		value.TransferredFrom = &ATIdentity{URI: row.TransferredFromUri.String, CID: row.TransferredFromCid.String}
	}
	if row.OrganizationID.Valid {
		id := uuid.UUID(row.OrganizationID.Bytes)
		value.OrganizationID = &id
	}
	return value
}

func (store *PostgresStore) withOrganizationSlug(ctx context.Context, value Repository) (Repository, error) {
	if value.OrganizationID == nil {
		return value, nil
	}
	organization, err := store.queries.GetOrganizationByID(ctx, pgtype.UUID{Bytes: *value.OrganizationID, Valid: true})
	if err != nil {
		return Repository{}, mapStoreError(err)
	}
	value.OrganizationSlug = organization.Slug
	return value, nil
}

func forkSourceURI(source *ForkSource) string {
	if source == nil {
		return ""
	}
	return source.URI
}

func forkSourceCID(source *ForkSource) string {
	if source == nil {
		return ""
	}
	return source.CID
}

func pgForkRepositoryID(source *ForkSource) pgtype.UUID {
	if source == nil || source.LocalRepositoryID == nil {
		return pgtype.UUID{}
	}
	return pgUUID(*source.LocalRepositoryID)
}

func repositoryFromPageRow(row dbgen.PageRepositoriesByOrganizationRow) Repository {
	value := repositoryFromRow(dbgen.CoreRepository{
		ID: row.ID, OwnerDid: row.OwnerDid, Slug: row.Slug, DisplayName: row.DisplayName,
		Description: row.Description, Visibility: row.Visibility, State: row.State,
		DefaultBranch: row.DefaultBranch, StorageKey: row.StorageKey, AtUri: row.AtUri,
		AtCid: row.AtCid, CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt,
		DeletedAt: row.DeletedAt, OrganizationID: row.OrganizationID,
		ForkedFromUri: row.ForkedFromUri, ForkedFromCid: row.ForkedFromCid,
		ForkedFromLocalRepositoryID: row.ForkedFromLocalRepositoryID,
		TransferredFromUri:          row.TransferredFromUri,
		TransferredFromCid:          row.TransferredFromCid,
		ForkCount:                   row.ForkCount,
	})
	value.ViewerCanAdmin = row.ViewerCanAdmin.Bool
	return value
}

func pgOptionalUUID(id *uuid.UUID) pgtype.UUID {
	if id == nil {
		return pgtype.UUID{}
	}
	return pgtype.UUID{Bytes: *id, Valid: true}
}

func pgOptionalRawUUID(id *uuid.UUID) pgtype.UUID {
	if id == nil {
		return pgtype.UUID{}
	}
	return pgtype.UUID{Bytes: *id, Valid: true}
}

func mapStoreError(err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	var postgresError *pgconn.PgError
	if errors.As(err, &postgresError) && postgresError.Code == "23505" {
		return ErrAlreadyExists
	}
	return fmt.Errorf("repository query: %w", err)
}

func pgUUID(id ID) pgtype.UUID {
	return pgtype.UUID{Bytes: [16]byte(id), Valid: true}
}

func pgText(value string) pgtype.Text {
	return pgtype.Text{String: value, Valid: value != ""}
}

func pgTime(value time.Time) pgtype.Timestamptz {
	return pgtype.Timestamptz{Time: value, Valid: true}
}
