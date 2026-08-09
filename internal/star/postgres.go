package star

import (
	"context"
	"errors"
	"fmt"

	dbgen "github.com/adenosine-dev/adenosine/internal/database/generated"
	"github.com/jackc/pgx/v5"
)

// PostgresStore reads live star and repository projections.
type PostgresStore struct {
	queries *dbgen.Queries
}

// NewPostgresStore constructs a projected star store.
func NewPostgresStore(queries *dbgen.Queries) *PostgresStore {
	return &PostgresStore{queries: queries}
}

func (store *PostgresStore) GetTarget(ctx context.Context, repositoryURI string) (Target, int64, error) {
	row, err := store.queries.GetNetworkRepositoryStarTarget(ctx, repositoryURI)
	if errors.Is(err, pgx.ErrNoRows) {
		return Target{}, 0, ErrNotFound
	}
	if err != nil {
		return Target{}, 0, fmt.Errorf("query repository star target: %w", err)
	}
	return Target{URI: row.Uri, CID: row.Cid.String}, row.StarCount, nil
}

func (store *PostgresStore) GetProjection(ctx context.Context, repositoryURI string, limit int) (Projection, error) {
	rows, err := store.queries.GetNetworkStarProjection(ctx, dbgen.GetNetworkStarProjectionParams{RepositoryUri: repositoryURI, PageSize: int32(limit)})
	if err != nil {
		return Projection{}, fmt.Errorf("query network star projection: %w", err)
	}
	if len(rows) == 0 {
		return Projection{}, ErrNotFound
	}
	projection := Projection{StarCount: rows[0].StarCount, Stars: []Star{}}
	for _, row := range rows {
		if row.StarUri == "" {
			continue
		}
		projection.Stars = append(projection.Stars, Star{
			URI: row.StarUri, CID: row.StarCid, AuthorDID: row.AuthorDid,
			Target:    Target{URI: row.RepositoryUri, CID: row.ObservedRepositoryCid},
			CreatedAt: row.RecordCreatedAt.Time, IndexedAt: row.IndexedAt.Time,
		})
	}
	return projection, nil
}
