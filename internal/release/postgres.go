package release

import (
	"context"
	"errors"
	"fmt"
	"time"

	dbgen "github.com/adenosine-dev/adenosine/internal/database/generated"
	"github.com/adenosine-dev/adenosine/internal/repository"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
)

type transactionBeginner interface {
	Begin(context.Context) (pgx.Tx, error)
}

type PostgresStore struct {
	db      transactionBeginner
	queries *dbgen.Queries
}

func NewPostgresStore(db transactionBeginner, queries *dbgen.Queries) *PostgresStore {
	return &PostgresStore{db: db, queries: queries}
}

func (store *PostgresStore) CreateRelease(ctx context.Context, value Release) (Release, error) {
	row, err := store.queries.CreateRelease(ctx, dbgen.CreateReleaseParams{
		ID: pgUUID(value.ID), RepositoryID: pgUUID(uuid.UUID(value.RepositoryID)), TagName: value.TagName,
		TargetSha: value.TargetSHA, Name: value.Name, Body: value.Body, State: string(value.State),
		Prerelease: value.Prerelease, CreatedByDid: value.CreatedByDID, CreatedAt: pgTime(value.CreatedAt),
		PublishedAt: optionalTime(value.PublishedAt),
	})
	if err != nil {
		return Release{}, mapStoreError(err)
	}
	return releaseFromRow(row), nil
}

func (store *PostgresStore) GetRelease(ctx context.Context, repositoryID repository.ID, id uuid.UUID) (Release, error) {
	row, err := store.queries.GetRelease(ctx, dbgen.GetReleaseParams{ID: pgUUID(id), RepositoryID: pgUUID(uuid.UUID(repositoryID))})
	if err != nil {
		return Release{}, mapStoreError(err)
	}
	return releaseFromRow(row), nil
}

func (store *PostgresStore) PageReleases(ctx context.Context, repositoryID repository.ID, includeDrafts bool, after *uuid.UUID, limit int) (Page[Release], error) {
	rows, err := store.queries.PageReleases(ctx, dbgen.PageReleasesParams{RepositoryID: pgUUID(uuid.UUID(repositoryID)), IncludeDrafts: includeDrafts, AfterID: optionalUUID(after), PageLimit: int32(limit + 1)})
	if err != nil {
		return Page[Release]{}, fmt.Errorf("page release rows: %w", err)
	}
	page := Page[Release]{Items: make([]Release, min(len(rows), limit))}
	for index := range page.Items {
		page.Items[index] = releaseFromRow(rows[index])
	}
	if len(rows) > limit {
		next := page.Items[len(page.Items)-1].ID
		page.NextCursor = &next
	}
	return page, nil
}

func (store *PostgresStore) UpdateRelease(ctx context.Context, value Release) (Release, error) {
	row, err := store.queries.UpdateRelease(ctx, dbgen.UpdateReleaseParams{
		Name: value.Name, Body: value.Body, State: string(value.State), Prerelease: value.Prerelease,
		UpdatedAt: pgTime(value.UpdatedAt), PublishedAt: optionalTime(value.PublishedAt), ID: pgUUID(value.ID),
		RepositoryID: pgUUID(uuid.UUID(value.RepositoryID)),
	})
	if err != nil {
		return Release{}, mapStoreError(err)
	}
	return releaseFromRow(row), nil
}

func (store *PostgresStore) DeleteRelease(ctx context.Context, repositoryID repository.ID, id uuid.UUID) error {
	rows, err := store.queries.DeleteRelease(ctx, dbgen.DeleteReleaseParams{ID: pgUUID(id), RepositoryID: pgUUID(uuid.UUID(repositoryID))})
	if err != nil {
		return fmt.Errorf("delete release row: %w", err)
	}
	if rows == 0 {
		return ErrNotFound
	}
	return nil
}

func (store *PostgresStore) BeginDeleteRelease(ctx context.Context, repositoryID repository.ID, id uuid.UUID, now time.Time) error {
	tx, err := store.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin release deletion marker: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	queries := dbgen.New(tx)
	if err := queries.LockReleaseAssetQuota(ctx, repositoryID.String()); err != nil {
		return fmt.Errorf("lock release deletion: %w", err)
	}
	if _, err := queries.MarkReleaseDeleting(ctx, dbgen.MarkReleaseDeletingParams{UpdatedAt: pgTime(now), ID: pgUUID(id), RepositoryID: pgUUID(uuid.UUID(repositoryID))}); err != nil {
		return mapStoreError(err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit release deletion marker: %w", err)
	}
	return nil
}

func (store *PostgresStore) CreateAsset(ctx context.Context, value Asset, limits Limits) (Asset, error) {
	tx, err := store.db.Begin(ctx)
	if err != nil {
		return Asset{}, fmt.Errorf("begin release asset quota reservation: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	queries := dbgen.New(tx)
	if err := queries.LockReleaseAssetQuota(ctx, value.RepositoryID.String()); err != nil {
		return Asset{}, fmt.Errorf("lock release asset quota: %w", err)
	}
	usage, err := queries.GetReleaseAssetUsage(ctx, dbgen.GetReleaseAssetUsageParams{ReleaseID: pgUUID(value.ReleaseID), RepositoryID: pgUUID(uuid.UUID(value.RepositoryID))})
	if err != nil {
		return Asset{}, fmt.Errorf("read release asset quota: %w", err)
	}
	releaseRow, err := queries.GetRelease(ctx, dbgen.GetReleaseParams{ID: pgUUID(value.ReleaseID), RepositoryID: pgUUID(uuid.UUID(value.RepositoryID))})
	if err != nil {
		return Asset{}, mapStoreError(err)
	}
	if State(releaseRow.State) == StateDeleting {
		return Asset{}, ErrNotFound
	}
	if exceeds(usage.ReleaseBytes, value.SizeBytes, limits.ReleaseBytes) || exceeds(usage.RepositoryBytes, value.SizeBytes, limits.RepositoryBytes) {
		return Asset{}, ErrQuotaExceeded
	}
	row, err := queries.CreateReleaseAsset(ctx, dbgen.CreateReleaseAssetParams{
		ID: pgUUID(value.ID), ReleaseID: pgUUID(value.ReleaseID), RepositoryID: pgUUID(uuid.UUID(value.RepositoryID)),
		Name: value.Name, ContentType: value.ContentType, SizeBytes: value.SizeBytes, Sha256: value.SHA256,
		StorageKey: value.StorageKey, CreatedAt: pgTime(value.CreatedAt),
	})
	if err != nil {
		return Asset{}, mapStoreError(err)
	}
	if err := tx.Commit(ctx); err != nil {
		return Asset{}, fmt.Errorf("commit release asset quota reservation: %w", err)
	}
	return assetFromRow(row), nil
}

func (store *PostgresStore) GetAsset(ctx context.Context, repositoryID repository.ID, releaseID, id uuid.UUID) (Asset, error) {
	row, err := store.queries.GetReleaseAsset(ctx, dbgen.GetReleaseAssetParams{ID: pgUUID(id), ReleaseID: pgUUID(releaseID), RepositoryID: pgUUID(uuid.UUID(repositoryID))})
	if err != nil {
		return Asset{}, mapStoreError(err)
	}
	return assetFromRow(row), nil
}

func (store *PostgresStore) PageAssets(ctx context.Context, repositoryID repository.ID, releaseID uuid.UUID, after *uuid.UUID, limit int) (Page[Asset], error) {
	rows, err := store.queries.PageReleaseAssets(ctx, dbgen.PageReleaseAssetsParams{ReleaseID: pgUUID(releaseID), RepositoryID: pgUUID(uuid.UUID(repositoryID)), AfterID: optionalUUID(after), PageLimit: int32(limit + 1)})
	if err != nil {
		return Page[Asset]{}, fmt.Errorf("page release asset rows: %w", err)
	}
	page := Page[Asset]{Items: make([]Asset, min(len(rows), limit))}
	for index := range page.Items {
		page.Items[index] = assetFromRow(rows[index])
	}
	if len(rows) > limit {
		next := page.Items[len(page.Items)-1].ID
		page.NextCursor = &next
	}
	return page, nil
}

func (store *PostgresStore) ListAssets(ctx context.Context, repositoryID repository.ID, releaseID uuid.UUID) ([]Asset, error) {
	rows, err := store.queries.ListReleaseAssetsForDeletion(ctx, dbgen.ListReleaseAssetsForDeletionParams{ReleaseID: pgUUID(releaseID), RepositoryID: pgUUID(uuid.UUID(repositoryID))})
	if err != nil {
		return nil, fmt.Errorf("list release asset rows: %w", err)
	}
	assets := make([]Asset, len(rows))
	for index := range rows {
		assets[index] = assetFromRow(rows[index])
	}
	return assets, nil
}

func (store *PostgresStore) DeleteAsset(ctx context.Context, repositoryID repository.ID, releaseID, id uuid.UUID) error {
	rows, err := store.queries.DeleteReleaseAsset(ctx, dbgen.DeleteReleaseAssetParams{ID: pgUUID(id), ReleaseID: pgUUID(releaseID), RepositoryID: pgUUID(uuid.UUID(repositoryID))})
	if err != nil {
		return fmt.Errorf("delete release asset row: %w", err)
	}
	if rows == 0 {
		return ErrNotFound
	}
	return nil
}

func releaseFromRow(row dbgen.CoreRelease) Release {
	value := Release{ID: uuid.UUID(row.ID.Bytes), RepositoryID: repository.ID(row.RepositoryID.Bytes), TagName: row.TagName,
		TargetSHA: row.TargetSha, Name: row.Name, Body: row.Body, State: State(row.State), Prerelease: row.Prerelease,
		CreatedByDID: row.CreatedByDid, CreatedAt: row.CreatedAt.Time, UpdatedAt: row.UpdatedAt.Time}
	if row.PublishedAt.Valid {
		publishedAt := row.PublishedAt.Time
		value.PublishedAt = &publishedAt
	}
	return value
}

func assetFromRow(row dbgen.CoreReleaseAsset) Asset {
	return Asset{ID: uuid.UUID(row.ID.Bytes), ReleaseID: uuid.UUID(row.ReleaseID.Bytes), RepositoryID: repository.ID(row.RepositoryID.Bytes),
		Name: row.Name, ContentType: row.ContentType, SizeBytes: row.SizeBytes, SHA256: row.Sha256, StorageKey: row.StorageKey, CreatedAt: row.CreatedAt.Time}
}

func mapStoreError(err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		return ErrConflict
	}
	return fmt.Errorf("release query: %w", err)
}

func exceeds(current, additional, maximum int64) bool { return current > maximum-additional }
func pgUUID(id uuid.UUID) pgtype.UUID                 { return pgtype.UUID{Bytes: id, Valid: true} }
func optionalUUID(id *uuid.UUID) pgtype.UUID {
	if id == nil {
		return pgtype.UUID{}
	}
	return pgUUID(*id)
}
func pgTime(value time.Time) pgtype.Timestamptz {
	return pgtype.Timestamptz{Time: value.UTC(), Valid: true}
}
func optionalTime(value *time.Time) pgtype.Timestamptz {
	if value == nil {
		return pgtype.Timestamptz{}
	}
	return pgTime(*value)
}
