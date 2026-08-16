package search

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	dbgen "github.com/adenosine-dev/adenosine/internal/database/generated"
	"github.com/adenosine-dev/adenosine/internal/federation"
	"github.com/adenosine-dev/adenosine/internal/issue"
	"github.com/adenosine-dev/adenosine/internal/profile"
	"github.com/adenosine-dev/adenosine/internal/pullrequest"
	"github.com/adenosine-dev/adenosine/internal/star"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

type PostgresStore struct{ queries *dbgen.Queries }

func NewPostgresStore(queries *dbgen.Queries) *PostgresStore { return &PostgresStore{queries: queries} }

func (store *PostgresStore) ResolveRepository(ctx context.Context, owner, slug, viewerDID string) (federation.DiscoveryRepository, error) {
	row, err := store.queries.ResolveSearchRepository(ctx, dbgen.ResolveSearchRepositoryParams{RepositoryOwner: owner, RepositorySlug: slug, ViewerDid: optionalText(viewerDID)})
	return resolvedRepository(row, err)
}

func (store *PostgresStore) ResolveRepositoryByURI(ctx context.Context, repositoryURI, viewerDID string) (federation.DiscoveryRepository, error) {
	row, err := store.queries.ResolveSearchRepository(ctx, dbgen.ResolveSearchRepositoryParams{RequestedRepositoryUri: optionalText(repositoryURI), ViewerDid: optionalText(viewerDID)})
	return resolvedRepository(row, err)
}

func resolvedRepository(row dbgen.ResolveSearchRepositoryRow, err error) (federation.DiscoveryRepository, error) {
	if errors.Is(err, pgx.ErrNoRows) {
		return federation.DiscoveryRepository{}, ErrNotFound
	}
	if err != nil {
		return federation.DiscoveryRepository{}, fmt.Errorf("resolve repository: %w", err)
	}
	return federation.DiscoveryRepository{URI: row.Uri, CID: row.Cid.String, LocalRepositoryID: optionalUUID(row.LocalRepositoryID), OwnerDID: row.OwnerDid, OwnerHandle: row.OwnerHandle.String, OrganizationSlug: row.OrganizationSlug.String, Slug: row.Slug.String, Name: row.Name.String, Description: row.Description.String, DefaultBranch: row.DefaultBranch.String, GitHTTPS: row.GitHttps.String, GitSSH: row.GitSsh.String, Web: row.Web.String, ForkedFrom: searchStrongRef(row.ForkedFromUri, row.ForkedFromCid), ForkCount: row.ForkCount, CreatedAt: row.RecordCreatedAt.Time, UpdatedAt: row.RecordUpdatedAt.Time, IndexedAt: row.IndexedAt.Time, StarCount: row.StarCount, IssueCount: row.IssueCount, OpenIssueCount: row.OpenIssueCount, CommentCount: row.CommentCount, PullRequestCount: row.PullRequestCount, OpenPullRequestCount: row.OpenPullRequestCount}, nil
}

func (store *PostgresStore) ListForks(ctx context.Context, repositoryURI, viewerDID string, limit int, cursor *Cursor) ([]federation.DiscoveryRepository, int64, error) {
	count, err := store.queries.CountSearchRepositoryForks(ctx, dbgen.CountSearchRepositoryForksParams{RepositoryUri: repositoryURI, ViewerDid: optionalText(viewerDID)})
	if err != nil {
		return nil, 0, fmt.Errorf("count repository forks: %w", err)
	}
	params := dbgen.ListSearchRepositoryForksParams{RepositoryUri: repositoryURI, ViewerDid: optionalText(viewerDID), ResultLimit: int32(limit)}
	if cursor != nil {
		params.CursorUri = optionalText(cursor.Identity)
		params.CursorIndexedAt = pgtype.Timestamptz{Time: cursor.IndexedAt, Valid: true}
	}
	rows, err := store.queries.ListSearchRepositoryForks(ctx, params)
	if err != nil {
		return nil, 0, fmt.Errorf("list repository forks: %w", err)
	}
	result := make([]federation.DiscoveryRepository, len(rows))
	for index, row := range rows {
		result[index] = federation.DiscoveryRepository{
			URI: row.Uri, CID: row.Cid.String, LocalRepositoryID: optionalUUID(row.LocalRepositoryID), OwnerDID: row.OwnerDid,
			OwnerHandle: row.OwnerHandle.String, OrganizationSlug: row.OrganizationSlug.String, Slug: row.Slug.String, Name: row.Name.String,
			Description: row.Description.String, DefaultBranch: row.DefaultBranch.String, GitHTTPS: row.GitHttps.String, GitSSH: row.GitSsh.String, Web: row.Web.String,
			ForkedFrom: searchStrongRef(row.ForkedFromUri, row.ForkedFromCid), ForkCount: row.ForkCount,
			StarCount: row.StarCount, IssueCount: row.IssueCount, OpenIssueCount: row.OpenIssueCount, CommentCount: row.CommentCount,
			PullRequestCount: row.PullRequestCount, OpenPullRequestCount: row.OpenPullRequestCount,
			CreatedAt: row.RecordCreatedAt.Time, UpdatedAt: row.RecordUpdatedAt.Time, IndexedAt: row.IndexedAt.Time,
		}
	}
	return result, count, nil
}

func (store *PostgresStore) ResolveIssue(ctx context.Context, repositoryURI, issueURI, viewerDID string) (issue.ProjectedIssue, error) {
	row, err := store.queries.ResolveSearchIssue(ctx, dbgen.ResolveSearchIssueParams{IssueUri: issueURI, RepositoryUri: repositoryURI, ViewerDid: optionalText(viewerDID)})
	if errors.Is(err, pgx.ErrNoRows) {
		return issue.ProjectedIssue{}, ErrNotFound
	}
	if err != nil {
		return issue.ProjectedIssue{}, fmt.Errorf("resolve issue: %w", err)
	}
	return issue.ProjectedIssue{Issue: issue.Issue{URI: row.Uri, CID: row.Cid.String, AuthorDID: row.AuthorDid, Record: issue.Record{Repository: issue.StrongRef{URI: row.RepositoryUri, CID: row.RepositoryCid}, Title: row.Title, Body: row.Body, CreatedAt: row.RecordCreatedAt.Time, UpdatedAt: row.RecordUpdatedAt.Time}}, State: issue.State(row.State), Status: issue.StrongRef{URI: row.StatusUri.String, CID: row.StatusCid.String}, CommentCount: row.VisibleCommentCount, IndexedAt: row.IndexedAt.Time}, nil
}

func (store *PostgresStore) ResolveProfile(ctx context.Context, did, viewerDID string) (profile.Profile, error) {
	row, err := store.queries.ResolveSearchProfile(ctx, dbgen.ResolveSearchProfileParams{ProfileDid: did, ViewerDid: optionalText(viewerDID)})
	if errors.Is(err, pgx.ErrNoRows) {
		return profile.Profile{}, ErrNotFound
	}
	if err != nil {
		return profile.Profile{}, fmt.Errorf("resolve profile: %w", err)
	}
	return profile.Profile{DID: row.Did, URI: row.ProfileUri.String, CID: row.ProfileCid.String, Handle: row.Handle.String, DisplayName: row.DisplayName.String, Bio: row.Bio.String, AvatarRef: row.AvatarRef.String, Website: row.Website.String, Location: row.Location.String, RepositoryCount: row.VisibleRepositoryCount, ContributionCount: int64(row.VisibleContributionCount), RecordCreatedAt: row.RecordCreatedAt.Time, IndexedAt: row.IndexedAt.Time}, nil
}

func (store *PostgresStore) ListIssues(ctx context.Context, repositoryURI, viewerDID string, limit int, cursor *Cursor) (issue.Projection, error) {
	counts, err := store.queries.CountSearchIssues(ctx, dbgen.CountSearchIssuesParams{RepositoryUri: repositoryURI, ViewerDid: optionalText(viewerDID)})
	if err != nil {
		return issue.Projection{}, fmt.Errorf("count issues: %w", err)
	}
	params := dbgen.ListSearchIssuesParams{RepositoryUri: repositoryURI, ViewerDid: optionalText(viewerDID), ResultLimit: int32(limit)}
	applyCollectionCursor(&params.CursorUri, &params.CursorCreatedAt, cursor)
	rows, err := store.queries.ListSearchIssues(ctx, params)
	if err != nil {
		return issue.Projection{}, fmt.Errorf("list issues: %w", err)
	}
	result := issue.Projection{IssueCount: counts.VisibleIssueCount, OpenIssueCount: counts.VisibleOpenIssueCount, Issues: []issue.ProjectedIssue{}}
	for _, row := range rows {
		result.Issues = append(result.Issues, projectedIssue(row.Uri, row.Cid.String, row.AuthorDid, row.RepositoryUri, row.RepositoryCid, row.Title, row.Body, row.State, row.StatusUri.String, row.StatusCid.String, row.VisibleCommentCount, row.RecordCreatedAt.Time, row.RecordUpdatedAt.Time, row.IndexedAt.Time))
	}
	return result, nil
}

func (store *PostgresStore) ListStars(ctx context.Context, repositoryURI, viewerDID string, limit int, cursor *Cursor) (star.Projection, error) {
	count, err := store.queries.CountSearchStars(ctx, dbgen.CountSearchStarsParams{RepositoryUri: repositoryURI, ViewerDid: optionalText(viewerDID)})
	if err != nil {
		return star.Projection{}, fmt.Errorf("count stars: %w", err)
	}
	params := dbgen.ListSearchStarsParams{RepositoryUri: repositoryURI, ViewerDid: optionalText(viewerDID), ResultLimit: int32(limit)}
	applyCollectionCursor(&params.CursorUri, &params.CursorCreatedAt, cursor)
	rows, err := store.queries.ListSearchStars(ctx, params)
	if err != nil {
		return star.Projection{}, fmt.Errorf("list stars: %w", err)
	}
	result := star.Projection{StarCount: count, Stars: []star.Star{}}
	for _, row := range rows {
		result.Stars = append(result.Stars, star.Star{URI: row.Uri, CID: row.Cid.String, AuthorDID: row.AuthorDid, Target: star.Target{URI: row.RepositoryUri, CID: row.RepositoryCid}, CreatedAt: row.RecordCreatedAt.Time, IndexedAt: row.IndexedAt.Time})
	}
	return result, nil
}

func (store *PostgresStore) ListPullRequests(ctx context.Context, repositoryURI, viewerDID string, limit int, cursor *Cursor) (pullrequest.Projection, error) {
	counts, err := store.queries.CountSearchPullRequests(ctx, dbgen.CountSearchPullRequestsParams{RepositoryUri: repositoryURI, ViewerDid: optionalText(viewerDID)})
	if err != nil {
		return pullrequest.Projection{}, fmt.Errorf("count pull requests: %w", err)
	}
	params := dbgen.ListSearchPullRequestsParams{RepositoryUri: repositoryURI, ViewerDid: optionalText(viewerDID), ResultLimit: int32(limit)}
	applyCollectionCursor(&params.CursorUri, &params.CursorCreatedAt, cursor)
	rows, err := store.queries.ListSearchPullRequests(ctx, params)
	if err != nil {
		return pullrequest.Projection{}, fmt.Errorf("list pull requests: %w", err)
	}
	result := pullrequest.Projection{PullRequestCount: counts.VisiblePullRequestCount, OpenPullRequestCount: counts.VisibleOpenPullRequestCount, PullRequests: []pullrequest.ProjectedPullRequest{}}
	for _, row := range rows {
		result.PullRequests = append(result.PullRequests, projectedPull(row.Uri, row.Cid.String, row.AuthorDid, row.SourceRepositoryUri, row.SourceRepositoryCid, row.SourceBranch, row.TargetRepositoryUri, row.TargetRepositoryCid, row.TargetBranch, row.HeadSha, row.Title, row.Body, row.State, row.StatusUri.String, row.StatusCid.String, row.MergedCommitSha.String, row.VisibleReviewCount, row.RecordCreatedAt.Time, row.RecordUpdatedAt.Time, row.IndexedAt.Time))
	}
	return result, nil
}

func (store *PostgresStore) ResolvePullRequest(ctx context.Context, uri, viewerDID string) (pullrequest.ProjectedPullRequest, error) {
	row, err := store.queries.ResolveSearchPullRequest(ctx, dbgen.ResolveSearchPullRequestParams{PullRequestUri: uri, ViewerDid: optionalText(viewerDID)})
	if errors.Is(err, pgx.ErrNoRows) {
		return pullrequest.ProjectedPullRequest{}, ErrNotFound
	}
	if err != nil {
		return pullrequest.ProjectedPullRequest{}, fmt.Errorf("resolve pull request: %w", err)
	}
	return projectedPull(row.Uri, row.Cid.String, row.AuthorDid, row.SourceRepositoryUri, row.SourceRepositoryCid, row.SourceBranch, row.TargetRepositoryUri, row.TargetRepositoryCid, row.TargetBranch, row.HeadSha, row.Title, row.Body, row.State, row.StatusUri.String, row.StatusCid.String, row.MergedCommitSha.String, row.VisibleReviewCount, row.RecordCreatedAt.Time, row.RecordUpdatedAt.Time, row.IndexedAt.Time), nil
}

func (store *PostgresStore) ListPullRequestReviews(ctx context.Context, uri, viewerDID string, limit int, cursor *Cursor) ([]pullrequest.ProjectedReview, error) {
	params := dbgen.ListSearchPullRequestReviewsParams{PullRequestUri: uri, ViewerDid: optionalText(viewerDID), ResultLimit: int32(limit)}
	applyCollectionCursor(&params.CursorUri, &params.CursorCreatedAt, cursor)
	rows, err := store.queries.ListSearchPullRequestReviews(ctx, params)
	if err != nil {
		return nil, fmt.Errorf("list pull request reviews: %w", err)
	}
	result := make([]pullrequest.ProjectedReview, len(rows))
	for index, row := range rows {
		result[index] = pullrequest.ProjectedReview{Review: pullrequest.Review{URI: row.Uri, CID: row.Cid.String, AuthorDID: row.AuthorDid, ReviewRecord: pullrequest.ReviewRecord{Subject: pullrequest.StrongRef{URI: row.PullRequestUri, CID: row.PullRequestCid}, Verdict: pullrequest.Verdict(row.Verdict), Body: row.Body, CreatedAt: row.RecordCreatedAt.Time, UpdatedAt: row.RecordUpdatedAt.Time}}, IndexedAt: row.IndexedAt.Time}
	}
	return result, nil
}

func applyCollectionCursor(uri *pgtype.Text, createdAt *pgtype.Timestamptz, cursor *Cursor) {
	if cursor == nil {
		return
	}
	*uri = optionalText(cursor.Identity)
	*createdAt = pgtype.Timestamptz{Time: cursor.IndexedAt, Valid: true}
}

func projectedIssue(uri, cid, author, repositoryURI, repositoryCID, title, body, state, statusURI, statusCID string, comments int64, created, updated, indexed time.Time) issue.ProjectedIssue {
	return issue.ProjectedIssue{Issue: issue.Issue{URI: uri, CID: cid, AuthorDID: author, Record: issue.Record{Repository: issue.StrongRef{URI: repositoryURI, CID: repositoryCID}, Title: title, Body: body, CreatedAt: created, UpdatedAt: updated}}, State: issue.State(state), Status: issue.StrongRef{URI: statusURI, CID: statusCID}, CommentCount: comments, IndexedAt: indexed}
}
func projectedPull(uri, cid, author, sourceURI, sourceCID, sourceBranch, targetURI, targetCID, targetBranch, head, title, body, state, statusURI, statusCID, merged string, reviews int64, created, updated, indexed time.Time) pullrequest.ProjectedPullRequest {
	return pullrequest.ProjectedPullRequest{PullRequest: pullrequest.PullRequest{URI: uri, CID: cid, AuthorDID: author, Record: pullrequest.Record{SourceRepository: pullrequest.StrongRef{URI: sourceURI, CID: sourceCID}, TargetRepository: pullrequest.StrongRef{URI: targetURI, CID: targetCID}, SourceBranch: sourceBranch, TargetBranch: targetBranch, HeadSHA: head, Title: title, Body: body, CreatedAt: created, UpdatedAt: updated}}, State: pullrequest.State(state), Status: pullrequest.StrongRef{URI: statusURI, CID: statusCID}, MergedCommitSHA: merged, ReviewCount: reviews, IndexedAt: indexed}
}

func optionalUUID(value pgtype.UUID) *uuid.UUID {
	if !value.Valid {
		return nil
	}
	id := uuid.UUID(value.Bytes)
	return &id
}

func (store *PostgresStore) SearchRepositories(ctx context.Context, query string, sort Sort, limit int, viewerDID string, cursor *Cursor) ([]RepositoryResult, error) {
	params := dbgen.SearchRepositoriesParams{SearchQuery: query, SearchPattern: escapeLike(query), SearchSort: string(sort), PageSize: int32(limit), ViewerDid: optionalText(viewerDID)}
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
			URI: row.Uri, CID: row.Cid.String, LocalRepositoryID: localID, OwnerDID: row.OwnerDid, OwnerHandle: row.OwnerHandle.String, OrganizationSlug: row.OrganizationSlug.String,
			Slug: row.Slug.String, Name: row.Name.String, Description: row.Description.String, DefaultBranch: row.DefaultBranch.String,
			ForkedFrom: searchStrongRef(row.ForkedFromUri, row.ForkedFromCid), ForkCount: row.ForkCount,
			GitHTTPS: row.GitHttps.String, GitSSH: row.GitSsh.String, Web: row.Web.String, CreatedAt: row.RecordCreatedAt.Time,
			UpdatedAt: row.RecordUpdatedAt.Time, IndexedAt: row.IndexedAt.Time, StarCount: row.StarCount, IssueCount: row.IssueCount,
			OpenIssueCount: row.OpenIssueCount, CommentCount: row.CommentCount, PullRequestCount: row.PullRequestCount, OpenPullRequestCount: row.OpenPullRequestCount,
		}}
	}
	return results, nil
}

func searchStrongRef(uri, cid pgtype.Text) *federation.StrongRef {
	if !uri.Valid || !cid.Valid {
		return nil
	}
	return &federation.StrongRef{URI: uri.String, CID: cid.String}
}

func (store *PostgresStore) SearchProfiles(ctx context.Context, query string, sort Sort, limit int, viewerDID string, cursor *Cursor) ([]ProfileResult, error) {
	params := dbgen.SearchProfilesParams{SearchQuery: query, SearchPattern: escapeLike(query), SearchSort: string(sort), PageSize: int32(limit), ViewerDid: optionalText(viewerDID)}
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
			Location: row.Location.String, RepositoryCount: row.VisibleRepositoryCount, ContributionCount: int64(row.VisibleContributionCount),
			RecordCreatedAt: row.RecordCreatedAt.Time, IndexedAt: row.IndexedAt.Time,
		}}
	}
	return results, nil
}

func optionalText(value string) pgtype.Text { return pgtype.Text{String: value, Valid: value != ""} }

func escapeLike(value string) string {
	value = strings.ReplaceAll(value, `\`, `\\`)
	value = strings.ReplaceAll(value, `%`, `\%`)
	return strings.ReplaceAll(value, `_`, `\_`)
}
