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
	if row.ForkedFromUri.Valid && row.ForkedFromCid.Valid {
		value.ForkedFrom = &ForkSource{URI: row.ForkedFromUri.String, CID: row.ForkedFromCid.String}
		if row.ForkedFromLocalRepositoryID.Valid {
			id := ID(row.ForkedFromLocalRepositoryID.Bytes)
			value.ForkedFrom.LocalRepositoryID = &id
		}
	}
	if row.OrganizationID.Valid {
		id := uuid.UUID(row.OrganizationID.Bytes)
		value.OrganizationID = &id
	}
	return value
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
