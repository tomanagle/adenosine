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
	"unicode/utf8"

	"github.com/adenosine-dev/adenosine/internal/federation"
	"github.com/adenosine-dev/adenosine/internal/profile"
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

type Store interface {
	SearchRepositories(context.Context, string, Sort, int, string, *Cursor) ([]RepositoryResult, error)
	SearchProfiles(context.Context, string, Sort, int, string, *Cursor) ([]ProfileResult, error)
}

type Service struct{ store Store }

func NewService(store Store) *Service { return &Service{store: store} }

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
	if query == "" || len(query) > maxQueryBytes || !utf8.ValidString(query) {
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
