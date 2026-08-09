package issue

import (
	"context"
	"errors"
	"fmt"

	dbgen "github.com/adenosine-dev/adenosine/internal/database/generated"
	"github.com/jackc/pgx/v5"
)

// PostgresStore reads current repository and issue projections.
type PostgresStore struct {
	queries *dbgen.Queries
}

// NewPostgresStore constructs a projected issue store.
func NewPostgresStore(queries *dbgen.Queries) *PostgresStore {
	return &PostgresStore{queries: queries}
}

func (store *PostgresStore) GetProjection(ctx context.Context, repositoryURI string, limit int) (Projection, error) {
	rows, err := store.queries.GetNetworkIssueProjection(ctx, dbgen.GetNetworkIssueProjectionParams{RepositoryUri: repositoryURI, PageSize: int32(limit)})
	if err != nil {
		return Projection{}, fmt.Errorf("query network issue projection: %w", err)
	}
	if len(rows) == 0 {
		return Projection{}, ErrNotFound
	}
	projection := Projection{IssueCount: rows[0].IssueCount, OpenIssueCount: rows[0].OpenIssueCount, Issues: []ProjectedIssue{}}
	for _, row := range rows {
		if row.IssueUri == "" {
			continue
		}
		projection.Issues = append(projection.Issues, ProjectedIssue{
			Issue: Issue{URI: row.IssueUri, CID: row.IssueCid, AuthorDID: row.AuthorDid, Record: Record{
				Repository: StrongRef{URI: row.RepositoryUri, CID: row.ObservedRepositoryCid}, Title: row.Title, Body: row.Body,
				CreatedAt: row.RecordCreatedAt.Time, UpdatedAt: row.RecordUpdatedAt.Time,
			}},
			State: State(row.State), Status: StrongRef{URI: row.StatusUri, CID: row.StatusCid}, IndexedAt: row.IndexedAt.Time,
		})
	}
	return projection, nil
}

func (store *PostgresStore) GetRepositoryTarget(ctx context.Context, repositoryURI string) (StrongRef, error) {
	row, err := store.queries.GetNetworkIssueRepositoryTarget(ctx, repositoryURI)
	if errors.Is(err, pgx.ErrNoRows) {
		return StrongRef{}, ErrNotFound
	}
	if err != nil {
		return StrongRef{}, fmt.Errorf("query issue repository target: %w", err)
	}
	return StrongRef{URI: row.Uri, CID: row.Cid.String}, nil
}

func (store *PostgresStore) GetStatusTarget(ctx context.Context, issueURI string) (statusTarget, error) {
	row, err := store.queries.GetNetworkIssueStatusWriteTarget(ctx, issueURI)
	if errors.Is(err, pgx.ErrNoRows) {
		return statusTarget{}, ErrNotFound
	}
	if err != nil {
		return statusTarget{}, fmt.Errorf("query issue status target: %w", err)
	}
	return statusTarget{
		Subject: StrongRef{URI: row.IssueUri, CID: row.IssueCid.String}, Repository: StrongRef{URI: row.RepositoryUri, CID: row.RepositoryCid.String},
		StatusCreatedAt: row.StatusCreatedAt.Time,
	}, nil
}
