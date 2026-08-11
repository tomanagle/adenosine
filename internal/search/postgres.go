package search

import (
	"context"
	"fmt"

	dbgen "github.com/adenosine-dev/adenosine/internal/database/generated"
	"github.com/adenosine-dev/adenosine/internal/federation"
	"github.com/adenosine-dev/adenosine/internal/profile"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
)

type PostgresStore struct{ queries *dbgen.Queries }

func NewPostgresStore(queries *dbgen.Queries) *PostgresStore { return &PostgresStore{queries: queries} }

func (store *PostgresStore) SearchRepositories(ctx context.Context, query string, sort Sort, limit int, viewerDID string, cursor *Cursor) ([]RepositoryResult, error) {
	params := dbgen.SearchRepositoriesParams{SearchQuery: query, SearchSort: string(sort), PageSize: int32(limit), ViewerDid: optionalText(viewerDID)}
	if cursor != nil {
		params.CursorUri = optionalText(cursor.Identity)
		params.CursorScore = pgtype.Float8{Float64: cursor.Score, Valid: true}
		params.CursorIndexedAt = pgtype.Timestamptz{Time: cursor.IndexedAt, Valid: true}
	}
	rows, err := store.queries.SearchRepositories(ctx, params)
	if err != nil {
		return nil, fmt.Errorf("query repositories: %w", err)
	}
	results := make([]RepositoryResult, len(rows))
	for index, row := range rows {
		var localID *uuid.UUID
		if row.LocalRepositoryID.Valid {
			value := uuid.UUID(row.LocalRepositoryID.Bytes)
			localID = &value
		}
		results[index] = RepositoryResult{Score: row.Score, Repository: federation.DiscoveryRepository{
			URI: row.Uri, CID: row.Cid.String, LocalRepositoryID: localID, OwnerDID: row.OwnerDid, OwnerHandle: row.OwnerHandle.String,
			Slug: row.Slug.String, Name: row.Name.String, Description: row.Description.String, DefaultBranch: row.DefaultBranch.String,
			GitHTTPS: row.GitHttps.String, GitSSH: row.GitSsh.String, Web: row.Web.String, CreatedAt: row.RecordCreatedAt.Time,
			UpdatedAt: row.RecordUpdatedAt.Time, IndexedAt: row.IndexedAt.Time, StarCount: row.StarCount, IssueCount: row.IssueCount,
			OpenIssueCount: row.OpenIssueCount, CommentCount: row.CommentCount, PullRequestCount: row.PullRequestCount, OpenPullRequestCount: row.OpenPullRequestCount,
		}}
	}
	return results, nil
}

func (store *PostgresStore) SearchProfiles(ctx context.Context, query string, sort Sort, limit int, viewerDID string, cursor *Cursor) ([]ProfileResult, error) {
	params := dbgen.SearchProfilesParams{SearchQuery: query, SearchSort: string(sort), PageSize: int32(limit), ViewerDid: optionalText(viewerDID)}
	if cursor != nil {
		params.CursorDid = optionalText(cursor.Identity)
		params.CursorScore = pgtype.Float8{Float64: cursor.Score, Valid: true}
		params.CursorIndexedAt = pgtype.Timestamptz{Time: cursor.IndexedAt, Valid: true}
	}
	rows, err := store.queries.SearchProfiles(ctx, params)
	if err != nil {
		return nil, fmt.Errorf("query profiles: %w", err)
	}
	results := make([]ProfileResult, len(rows))
	for index, row := range rows {
		results[index] = ProfileResult{Score: row.Score, Profile: profile.Profile{
			DID: row.Did, URI: row.ProfileUri.String, CID: row.ProfileCid.String, Handle: row.Handle.String,
			DisplayName: row.DisplayName.String, Bio: row.Bio.String, AvatarRef: row.AvatarRef.String, Website: row.Website.String,
			Location: row.Location.String, RepositoryCount: row.RepositoryCount, ContributionCount: row.ContributionCount,
			RecordCreatedAt: row.RecordCreatedAt.Time, IndexedAt: row.IndexedAt.Time,
		}}
	}
	return results, nil
}

func optionalText(value string) pgtype.Text { return pgtype.Text{String: value, Valid: value != ""} }
