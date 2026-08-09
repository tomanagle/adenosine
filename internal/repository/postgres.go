package repository

import (
	"context"
	"errors"
	"fmt"
	"time"

	dbgen "github.com/adenosine-dev/adenosine/internal/database/generated"
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
		ID:            pgUUID(repository.ID),
		OwnerDid:      repository.OwnerDID,
		Slug:          repository.Slug,
		DisplayName:   pgText(repository.DisplayName),
		Description:   pgText(repository.Description),
		Visibility:    string(repository.Visibility),
		State:         string(repository.State),
		DefaultBranch: repository.DefaultBranch,
		StorageKey:    repository.StorageKey,
		AtUri:         pgText(repository.ATURI),
		AtCid:         pgText(repository.ATCID),
		CreatedAt:     pgTime(repository.CreatedAt),
		UpdatedAt:     pgTime(repository.UpdatedAt),
	})
	if err != nil {
		return Repository{}, mapStoreError(err)
	}
	return repositoryFromRow(row), nil
}

// GetByOwnerSlug loads an active repository route by owner DID and slug.
func (store *PostgresStore) GetByOwnerSlug(ctx context.Context, ownerDID, slug string) (Repository, error) {
	row, err := store.queries.GetRepositoryByOwnerSlug(ctx, dbgen.GetRepositoryByOwnerSlugParams{
		OwnerDid: ownerDID,
		Lower:    slug,
	})
	if err != nil {
		return Repository{}, mapStoreError(err)
	}
	return repositoryFromRow(row), nil
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
	return Repository{
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
		CreatedAt:     row.CreatedAt.Time,
		UpdatedAt:     row.UpdatedAt.Time,
	}
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
