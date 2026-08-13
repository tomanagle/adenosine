package pullrequest

import (
	"context"
	"errors"
	"fmt"
	"time"

	dbgen "github.com/adenosine-dev/adenosine/internal/database/generated"
	"github.com/adenosine-dev/adenosine/internal/repository"
	"github.com/jackc/pgx/v5"
)

// PostgresStore reads live projected pull request fetch targets.
type PostgresStore struct{ queries *dbgen.Queries }

// NewPostgresStore constructs a projected pull request store.
func NewPostgresStore(queries *dbgen.Queries) *PostgresStore {
	return &PostgresStore{queries: queries}
}

func (store *PostgresStore) List(ctx context.Context, repositoryURI string, limit int) (Projection, error) {
	counts, err := store.queries.GetProjectedPullRequestCounts(ctx, repositoryURI)
	if errors.Is(err, pgx.ErrNoRows) {
		return Projection{}, ErrNotFound
	}
	if err != nil {
		return Projection{}, fmt.Errorf("query projected pull request counts: %w", err)
	}
	rows, err := store.queries.ListProjectedPullRequests(ctx, dbgen.ListProjectedPullRequestsParams{RepositoryUri: repositoryURI, ResultLimit: int32(limit)})
	if err != nil {
		return Projection{}, fmt.Errorf("query projected pull requests: %w", err)
	}
	values := make([]ProjectedPullRequest, len(rows))
	for index, row := range rows {
		values[index] = projectedPullRequest(row.Uri, row.Cid.String, row.AuthorDid, row.SourceRepositoryUri, row.SourceRepositoryCid,
			row.SourceBranch, row.TargetRepositoryUri, row.TargetRepositoryCid, row.TargetBranch, row.HeadSha, row.Title, row.Body,
			row.State, row.StatusUri.String, row.StatusCid.String, row.MergedCommitSha.String, row.ReviewCount,
			row.RecordCreatedAt.Time, row.RecordUpdatedAt.Time, row.IndexedAt.Time)
	}
	return Projection{PullRequestCount: counts.PullRequestCount, OpenPullRequestCount: counts.OpenPullRequestCount, PullRequests: values}, nil
}

func (store *PostgresStore) Get(ctx context.Context, pullRequestURI string) (ProjectedPullRequest, error) {
	row, err := store.queries.GetProjectedPullRequest(ctx, pullRequestURI)
	if errors.Is(err, pgx.ErrNoRows) {
		return ProjectedPullRequest{}, ErrNotFound
	}
	if err != nil {
		return ProjectedPullRequest{}, fmt.Errorf("query projected pull request: %w", err)
	}
	return projectedPullRequest(row.Uri, row.Cid.String, row.AuthorDid, row.SourceRepositoryUri, row.SourceRepositoryCid,
		row.SourceBranch, row.TargetRepositoryUri, row.TargetRepositoryCid, row.TargetBranch, row.HeadSha, row.Title, row.Body,
		row.State, row.StatusUri.String, row.StatusCid.String, row.MergedCommitSha.String, row.ReviewCount,
		row.RecordCreatedAt.Time, row.RecordUpdatedAt.Time, row.IndexedAt.Time), nil
}

func (store *PostgresStore) GetRepositoryTargets(ctx context.Context, sourceURI, targetURI string) (repositoryTargets, error) {
	row, err := store.queries.GetProjectedPullRequestRepositoryTargets(ctx, dbgen.GetProjectedPullRequestRepositoryTargetsParams{SourceRepositoryUri: sourceURI, TargetRepositoryUri: targetURI})
	if errors.Is(err, pgx.ErrNoRows) {
		return repositoryTargets{}, ErrNotFound
	}
	if err != nil {
		return repositoryTargets{}, fmt.Errorf("query projected pull request repository targets: %w", err)
	}
	return repositoryTargets{Source: StrongRef{URI: row.SourceUri, CID: row.SourceCid.String}, Target: StrongRef{URI: row.TargetUri, CID: row.TargetCid.String}}, nil
}

func (store *PostgresStore) ListReviews(ctx context.Context, pullRequestURI string, limit int) ([]ProjectedReview, error) {
	rows, err := store.queries.ListProjectedPullRequestReviews(ctx, dbgen.ListProjectedPullRequestReviewsParams{PullRequestUri: pullRequestURI, ResultLimit: int32(limit)})
	if err != nil {
		return nil, fmt.Errorf("query projected pull request reviews: %w", err)
	}
	values := make([]ProjectedReview, len(rows))
	for index, row := range rows {
		values[index] = ProjectedReview{Review: Review{URI: row.Uri, CID: row.Cid.String, AuthorDID: row.AuthorDid, ReviewRecord: ReviewRecord{
			Subject: StrongRef{URI: row.PullRequestUri, CID: row.PullRequestCid}, Verdict: Verdict(row.Verdict), Body: row.Body,
			CreatedAt: row.RecordCreatedAt.Time, UpdatedAt: row.RecordUpdatedAt.Time}}, IndexedAt: row.IndexedAt.Time}
	}
	return values, nil
}

func (store *PostgresStore) GetReviewTarget(ctx context.Context, pullRequestURI string) (StrongRef, error) {
	row, err := store.queries.GetProjectedPullRequestReviewTarget(ctx, pullRequestURI)
	if errors.Is(err, pgx.ErrNoRows) {
		return StrongRef{}, ErrNotFound
	}
	if err != nil {
		return StrongRef{}, fmt.Errorf("query projected pull request review target: %w", err)
	}
	return StrongRef{URI: row.Uri, CID: row.Cid.String}, nil
}

func (store *PostgresStore) GetStatusTarget(ctx context.Context, pullRequestURI string) (statusTarget, error) {
	row, err := store.queries.GetProjectedPullRequestStatusTarget(ctx, pullRequestURI)
	if errors.Is(err, pgx.ErrNoRows) {
		return statusTarget{}, ErrNotFound
	}
	if err != nil {
		return statusTarget{}, fmt.Errorf("query projected pull request status target: %w", err)
	}
	value := statusTarget{Subject: StrongRef{URI: row.Uri, CID: row.Cid.String},
		TargetRepository: StrongRef{URI: row.TargetRepositoryUri, CID: row.TargetRepositoryCid}, StatusCreatedAt: row.StatusCreatedAt.Time}
	if row.LocalRepositoryID.Valid {
		id := repository.ID(row.LocalRepositoryID.Bytes)
		value.RepositoryID = &id
	}
	return value, nil
}

func projectedPullRequest(uri, cid, authorDID, sourceURI, sourceCID, sourceBranch, targetURI, targetCID, targetBranch,
	headSHA, title, body, state, statusURI, statusCID, mergedSHA string, reviewCount int64, createdAt, updatedAt, indexedAt time.Time,
) ProjectedPullRequest {
	return ProjectedPullRequest{PullRequest: PullRequest{URI: uri, CID: cid, AuthorDID: authorDID, Record: Record{
		SourceRepository: StrongRef{URI: sourceURI, CID: sourceCID}, TargetRepository: StrongRef{URI: targetURI, CID: targetCID},
		SourceBranch: sourceBranch, TargetBranch: targetBranch, HeadSHA: headSHA, Title: title, Body: body, CreatedAt: createdAt, UpdatedAt: updatedAt}},
		State: State(state), Status: StrongRef{URI: statusURI, CID: statusCID}, MergedCommitSHA: mergedSHA, ReviewCount: reviewCount, IndexedAt: indexedAt}
}

func (store *PostgresStore) GetFetchTarget(ctx context.Context, pullRequestURI string) (fetchTarget, error) {
	row, err := store.queries.GetProjectedPullRequestFetchTarget(ctx, pullRequestURI)
	if errors.Is(err, pgx.ErrNoRows) {
		return fetchTarget{}, ErrNotFound
	}
	if err != nil {
		return fetchTarget{}, fmt.Errorf("query projected pull request fetch target: %w", err)
	}
	return fetchTarget{
		URI:          row.Uri,
		CID:          row.Cid.String,
		RepositoryID: repository.ID(row.LocalRepositoryID.Bytes),
		SourceURL:    row.GitHttps.String,
		SourceBranch: row.SourceBranch,
		HeadSHA:      row.HeadSha,
		TargetBranch: row.TargetBranch,
	}, nil
}

func (store *PostgresStore) GetMergeTarget(ctx context.Context, pullRequestURI string) (mergeTarget, error) {
	row, err := store.queries.GetProjectedPullRequestMergeTarget(ctx, pullRequestURI)
	if errors.Is(err, pgx.ErrNoRows) {
		return mergeTarget{}, ErrNotFound
	}
	if err != nil {
		return mergeTarget{}, fmt.Errorf("query projected pull request merge target: %w", err)
	}
	return mergeTarget{
		Subject:          StrongRef{URI: row.Uri, CID: row.Cid.String},
		SourceRepository: StrongRef{URI: row.SourceRepositoryUri, CID: row.SourceRepositoryCid}, SourceBranch: row.SourceBranch, HeadSHA: row.HeadSha,
		TargetRepository: StrongRef{URI: row.TargetRepositoryUri, CID: row.TargetRepositoryCid}, TargetBranch: row.TargetBranch,
		Title: row.Title, Body: row.Body, CreatedAt: row.RecordCreatedAt.Time, TargetOwnerDID: row.TargetOwnerDid,
		RepositoryID: repository.ID(row.LocalRepositoryID.Bytes), State: State(row.State), StatusCreatedAt: row.StatusCreatedAt.Time,
	}, nil
}
