// Package search provides local AppView search over rebuildable network projections.
package search

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/adenosine-dev/adenosine/internal/federation"
	"github.com/adenosine-dev/adenosine/internal/issue"
	"github.com/adenosine-dev/adenosine/internal/profile"
	"github.com/adenosine-dev/adenosine/internal/pullrequest"
	"github.com/adenosine-dev/adenosine/internal/star"
)

const (
	defaultLimit   = 20
	maxLimit       = 50
	maxQueryBytes  = 200
	maxCursorBytes = 4096
)

var (
	ErrInvalidQuery  = errors.New("search query must contain between 1 and 200 bytes of UTF-8 text")
	ErrInvalidSort   = errors.New("search sort must be relevance or recent")
	ErrInvalidLimit  = errors.New("search limit must be between 1 and 50")
	ErrInvalidCursor = errors.New("invalid search cursor")
	ErrNotFound      = errors.New("search result not found")
)

type Sort string

const (
	SortRelevance Sort = "relevance"
	SortRecent    Sort = "recent"
)

type Cursor struct {
	Score     float64
	IndexedAt time.Time
	Identity  string
}

type RepositoryResult struct {
	Repository federation.DiscoveryRepository
	Score      float64
}

type ProfileResult struct {
	Profile profile.Profile
	Score   float64
}

type RepositoryPage struct {
	Repositories []federation.DiscoveryRepository
	NextCursor   *string
}

type ProfilePage struct {
	Profiles   []profile.Profile
	NextCursor *string
}

type IssuePage struct {
	Projection issue.Projection
	NextCursor *string
}

type StarPage struct {
	Projection star.Projection
	NextCursor *string
}

type PullRequestPage struct {
	Projection pullrequest.Projection
	NextCursor *string
}

type PullRequestReviewPage struct {
	Reviews    []pullrequest.ProjectedReview
	NextCursor *string
}

type Store interface {
	SearchRepositories(context.Context, string, Sort, int, string, *Cursor) ([]RepositoryResult, error)
	SearchProfiles(context.Context, string, Sort, int, string, *Cursor) ([]ProfileResult, error)
}

type repositoryResolverStore interface {
	ResolveRepository(context.Context, string, string, string) (federation.DiscoveryRepository, error)
}

type issueResolverStore interface {
	ResolveIssue(context.Context, string, string, string) (issue.ProjectedIssue, error)
}

type collaborationStore interface {
	ResolveProfile(context.Context, string, string) (profile.Profile, error)
	ListIssues(context.Context, string, string, int, *Cursor) (issue.Projection, error)
	ListStars(context.Context, string, string, int, *Cursor) (star.Projection, error)
	ListPullRequests(context.Context, string, string, int, *Cursor) (pullrequest.Projection, error)
	ResolvePullRequest(context.Context, string, string) (pullrequest.ProjectedPullRequest, error)
	ListPullRequestReviews(context.Context, string, string, int, *Cursor) ([]pullrequest.ProjectedReview, error)
}

type Service struct{ store Store }

func NewService(store Store) *Service { return &Service{store: store} }

// ResolveRepository resolves an exact mutable owner/slug alias from the moderated local AppView.
func (service *Service) ResolveRepository(ctx context.Context, owner, slug, viewerDID string) (federation.DiscoveryRepository, error) {
	resolver, ok := service.store.(repositoryResolverStore)
	if !ok {
		return federation.DiscoveryRepository{}, ErrNotFound
	}
	return resolver.ResolveRepository(ctx, owner, slug, viewerDID)
}

// ResolveIssue resolves one exact issue identity from the moderated local AppView.
func (service *Service) ResolveIssue(ctx context.Context, repositoryURI, issueURI, viewerDID string) (issue.ProjectedIssue, error) {
	resolver, ok := service.store.(issueResolverStore)
	if !ok {
		return issue.ProjectedIssue{}, ErrNotFound
	}
	return resolver.ResolveIssue(ctx, repositoryURI, issueURI, viewerDID)
}

func (service *Service) ResolveProfile(ctx context.Context, did, viewerDID string) (profile.Profile, error) {
	store, ok := service.store.(collaborationStore)
	if !ok {
		return profile.Profile{}, ErrNotFound
	}
	return store.ResolveProfile(ctx, did, viewerDID)
}

func (service *Service) ListIssues(ctx context.Context, repositoryURI, viewerDID string) (issue.Projection, error) {
	store, ok := service.store.(collaborationStore)
	if !ok {
		return issue.Projection{}, ErrNotFound
	}
	return store.ListIssues(ctx, repositoryURI, viewerDID, 100, nil)
}

func (service *Service) ListStars(ctx context.Context, repositoryURI, viewerDID string) (star.Projection, error) {
	store, ok := service.store.(collaborationStore)
	if !ok {
		return star.Projection{}, ErrNotFound
	}
	return store.ListStars(ctx, repositoryURI, viewerDID, 100, nil)
}

func (service *Service) ListPullRequests(ctx context.Context, repositoryURI, viewerDID string) (pullrequest.Projection, error) {
	store, ok := service.store.(collaborationStore)
	if !ok {
		return pullrequest.Projection{}, ErrNotFound
	}
	return store.ListPullRequests(ctx, repositoryURI, viewerDID, 100, nil)
}

func (service *Service) ResolvePullRequest(ctx context.Context, uri, viewerDID string) (pullrequest.ProjectedPullRequest, error) {
	store, ok := service.store.(collaborationStore)
	if !ok {
		return pullrequest.ProjectedPullRequest{}, ErrNotFound
	}
	return store.ResolvePullRequest(ctx, uri, viewerDID)
}

func (service *Service) ListPullRequestReviews(ctx context.Context, uri, viewerDID string) ([]pullrequest.ProjectedReview, error) {
	store, ok := service.store.(collaborationStore)
	if !ok {
		return nil, ErrNotFound
	}
	return store.ListPullRequestReviews(ctx, uri, viewerDID, 100, nil)
}

func (service *Service) PageIssues(ctx context.Context, repositoryURI, viewerDID string, limit int, encodedCursor string) (IssuePage, error) {
	store, ok := service.store.(collaborationStore)
	if !ok {
		return IssuePage{}, ErrNotFound
	}
	cursor, err := validateCollectionCursor(encodedCursor, "issue", repositoryURI, limit)
	if err != nil {
		return IssuePage{}, err
	}
	projection, err := store.ListIssues(ctx, repositoryURI, viewerDID, limit+1, cursor)
	if err != nil {
		return IssuePage{}, err
	}
	page := IssuePage{Projection: projection}
	if len(page.Projection.Issues) > limit {
		last := page.Projection.Issues[limit-1]
		page.Projection.Issues = page.Projection.Issues[:limit]
		next := encodeCursor("issue", repositoryURI, SortRecent, Cursor{IndexedAt: last.CreatedAt, Identity: last.URI})
		page.NextCursor = &next
	}
	return page, nil
}

func (service *Service) PageStars(ctx context.Context, repositoryURI, viewerDID string, limit int, encodedCursor string) (StarPage, error) {
	store, ok := service.store.(collaborationStore)
	if !ok {
		return StarPage{}, ErrNotFound
	}
	cursor, err := validateCollectionCursor(encodedCursor, "star", repositoryURI, limit)
	if err != nil {
		return StarPage{}, err
	}
	projection, err := store.ListStars(ctx, repositoryURI, viewerDID, limit+1, cursor)
	if err != nil {
		return StarPage{}, err
	}
	page := StarPage{Projection: projection}
	if len(page.Projection.Stars) > limit {
		last := page.Projection.Stars[limit-1]
		page.Projection.Stars = page.Projection.Stars[:limit]
		next := encodeCursor("star", repositoryURI, SortRecent, Cursor{IndexedAt: last.CreatedAt, Identity: last.URI})
		page.NextCursor = &next
	}
	return page, nil
}

func (service *Service) PagePullRequests(ctx context.Context, repositoryURI, viewerDID string, limit int, encodedCursor string) (PullRequestPage, error) {
	store, ok := service.store.(collaborationStore)
	if !ok {
		return PullRequestPage{}, ErrNotFound
	}
	cursor, err := validateCollectionCursor(encodedCursor, "pull-request", repositoryURI, limit)
	if err != nil {
		return PullRequestPage{}, err
	}
	projection, err := store.ListPullRequests(ctx, repositoryURI, viewerDID, limit+1, cursor)
	if err != nil {
		return PullRequestPage{}, err
	}
	page := PullRequestPage{Projection: projection}
	if len(page.Projection.PullRequests) > limit {
		last := page.Projection.PullRequests[limit-1]
		page.Projection.PullRequests = page.Projection.PullRequests[:limit]
		next := encodeCursor("pull-request", repositoryURI, SortRecent, Cursor{IndexedAt: last.CreatedAt, Identity: last.URI})
		page.NextCursor = &next
	}
	return page, nil
}

func (service *Service) PagePullRequestReviews(ctx context.Context, uri, viewerDID string, limit int, encodedCursor string) (PullRequestReviewPage, error) {
	store, ok := service.store.(collaborationStore)
	if !ok {
		return PullRequestReviewPage{}, ErrNotFound
	}
	cursor, err := validateCollectionCursor(encodedCursor, "pull-request-review", uri, limit)
	if err != nil {
		return PullRequestReviewPage{}, err
	}
	reviews, err := store.ListPullRequestReviews(ctx, uri, viewerDID, limit+1, cursor)
	if err != nil {
		return PullRequestReviewPage{}, err
	}
	page := PullRequestReviewPage{Reviews: reviews}
	if len(page.Reviews) > limit {
		last := page.Reviews[limit-1]
		page.Reviews = page.Reviews[:limit]
		next := encodeCursor("pull-request-review", uri, SortRecent, Cursor{IndexedAt: last.CreatedAt, Identity: last.URI})
		page.NextCursor = &next
	}
	return page, nil
}

func validateCollectionCursor(encoded, kind, identity string, limit int) (*Cursor, error) {
	if limit < 1 || limit > 100 {
		return nil, ErrInvalidLimit
	}
	if encoded == "" {
		return nil, nil
	}
	cursor, err := decodeCursor(encoded, kind, identity, SortRecent)
	if err != nil {
		return nil, err
	}
	return &cursor, nil
}

func (service *Service) Repositories(ctx context.Context, query string, sort Sort, limit int, cursor, viewerDID string) (RepositoryPage, error) {
	query, sort, limit, decoded, err := validate(query, sort, limit, cursor, "repository")
	if err != nil {
		return RepositoryPage{}, err
	}
	results, err := service.store.SearchRepositories(ctx, query, sort, limit+1, viewerDID, decoded)
	if err != nil {
		return RepositoryPage{}, fmt.Errorf("search repositories: %w", err)
	}
	page := RepositoryPage{Repositories: make([]federation.DiscoveryRepository, 0, min(len(results), limit))}
	for _, result := range results[:min(len(results), limit)] {
		page.Repositories = append(page.Repositories, result.Repository)
	}
	if len(results) > limit {
		last := results[limit-1]
		next := encodeCursor("repository", query, sort, Cursor{Score: last.Score, IndexedAt: last.Repository.IndexedAt, Identity: last.Repository.URI})
		page.NextCursor = &next
	}
	return page, nil
}

func (service *Service) Profiles(ctx context.Context, query string, sort Sort, limit int, cursor, viewerDID string) (ProfilePage, error) {
	query, sort, limit, decoded, err := validate(query, sort, limit, cursor, "profile")
	if err != nil {
		return ProfilePage{}, err
	}
	results, err := service.store.SearchProfiles(ctx, query, sort, limit+1, viewerDID, decoded)
	if err != nil {
		return ProfilePage{}, fmt.Errorf("search profiles: %w", err)
	}
	page := ProfilePage{Profiles: make([]profile.Profile, 0, min(len(results), limit))}
	for _, result := range results[:min(len(results), limit)] {
		page.Profiles = append(page.Profiles, result.Profile)
	}
	if len(results) > limit {
		last := results[limit-1]
		next := encodeCursor("profile", query, sort, Cursor{Score: last.Score, IndexedAt: last.Profile.IndexedAt, Identity: last.Profile.DID})
		page.NextCursor = &next
	}
	return page, nil
}

func validate(query string, sort Sort, limit int, encodedCursor, kind string) (string, Sort, int, *Cursor, error) {
	query = strings.TrimSpace(query)
	query = strings.Join(strings.Fields(query), " ")
	if query == "" || len(query) > maxQueryBytes || !utf8.ValidString(query) || !containsSearchText(query) {
		return "", "", 0, nil, ErrInvalidQuery
	}
	if sort == "" {
		sort = SortRelevance
	}
	if sort != SortRelevance && sort != SortRecent {
		return "", "", 0, nil, ErrInvalidSort
	}
	if limit == 0 {
		limit = defaultLimit
	}
	if limit < 1 || limit > maxLimit {
		return "", "", 0, nil, ErrInvalidLimit
	}
	if encodedCursor == "" {
		return query, sort, limit, nil, nil
	}
	cursor, err := decodeCursor(encodedCursor, kind, query, sort)
	if err != nil {
		return "", "", 0, nil, err
	}
	return query, sort, limit, &cursor, nil
}

func containsSearchText(value string) bool {
	for _, character := range value {
		if unicode.IsLetter(character) || unicode.IsNumber(character) {
			return true
		}
	}
	return false
}

func encodeCursor(kind, query string, sort Sort, cursor Cursor) string {
	payload := strings.Join([]string{"v1", kind, string(sort), queryHash(query), strconv.FormatFloat(cursor.Score, 'g', 17, 64), cursor.IndexedAt.UTC().Format(time.RFC3339Nano), cursor.Identity}, "\n")
	return base64.RawURLEncoding.EncodeToString([]byte(payload))
}

func decodeCursor(encoded, kind, query string, sort Sort) (Cursor, error) {
	if len(encoded) > maxCursorBytes {
		return Cursor{}, ErrInvalidCursor
	}
	payload, err := base64.RawURLEncoding.Strict().DecodeString(encoded)
	if err != nil || len(payload) > maxCursorBytes {
		return Cursor{}, ErrInvalidCursor
	}
	parts := strings.Split(string(payload), "\n")
	if len(parts) != 7 || parts[0] != "v1" || parts[1] != kind || parts[2] != string(sort) || parts[3] != queryHash(query) || parts[6] == "" {
		return Cursor{}, ErrInvalidCursor
	}
	score, scoreErr := strconv.ParseFloat(parts[4], 64)
	indexedAt, timeErr := time.Parse(time.RFC3339Nano, parts[5])
	if scoreErr != nil || math.IsNaN(score) || math.IsInf(score, 0) || timeErr != nil || !strings.HasSuffix(parts[5], "Z") || parts[5] != indexedAt.UTC().Format(time.RFC3339Nano) {
		return Cursor{}, ErrInvalidCursor
	}
	return Cursor{Score: score, IndexedAt: indexedAt.UTC(), Identity: parts[6]}, nil
}

func queryHash(query string) string {
	digest := sha256.Sum256([]byte(query))
	return base64.RawURLEncoding.EncodeToString(digest[:])
}
