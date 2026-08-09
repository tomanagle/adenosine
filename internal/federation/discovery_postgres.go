package federation

import (
	"context"
	"fmt"

	dbgen "github.com/adenosine-dev/adenosine/internal/database/generated"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
)

// PostgresDiscoveryStore reads repository projections from the local network index.
type PostgresDiscoveryStore struct {
	queries *dbgen.Queries
}

// NewPostgresDiscoveryStore constructs a network discovery store.
func NewPostgresDiscoveryStore(queries *dbgen.Queries) *PostgresDiscoveryStore {
	return &PostgresDiscoveryStore{queries: queries}
}

// ListNetworkRepositories reads one descending keyset page of live projections.
func (store *PostgresDiscoveryStore) ListNetworkRepositories(ctx context.Context, limit int, cursor *DiscoveryCursor) ([]DiscoveryRepository, error) {
	params := dbgen.ListNetworkRepositoriesParams{PageSize: int32(limit)}
	if cursor != nil {
		params.CursorIndexedAt = pgtype.Timestamptz{Time: cursor.IndexedAt, Valid: true}
		params.CursorUri = pgtype.Text{String: cursor.URI, Valid: true}
	}
	rows, err := store.queries.ListNetworkRepositories(ctx, params)
	if err != nil {
		return nil, fmt.Errorf("query network repositories: %w", err)
	}
	repositories := make([]DiscoveryRepository, len(rows))
	for index, row := range rows {
		repositories[index] = DiscoveryRepository{
			URI: row.Uri, CID: row.Cid.String, LocalRepositoryID: optionalUUID(row.LocalRepositoryID), OwnerDID: row.OwnerDid,
			OwnerHandle: row.Handle.String, Slug: row.Slug.String, Name: row.Name.String,
			Description: row.Description.String, DefaultBranch: row.DefaultBranch.String,
			GitHTTPS: row.GitHttps.String, GitSSH: row.GitSsh.String, Web: row.Web.String,
			CreatedAt: row.RecordCreatedAt.Time, UpdatedAt: row.RecordUpdatedAt.Time, IndexedAt: row.IndexedAt.Time,
			StarCount: row.StarCount, IssueCount: row.IssueCount, OpenIssueCount: row.OpenIssueCount,
		}
	}
	return repositories, nil
}

func optionalUUID(value pgtype.UUID) *uuid.UUID {
	if !value.Valid {
		return nil
	}
	id := uuid.UUID(value.Bytes)
	return &id
}
