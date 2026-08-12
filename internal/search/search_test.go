package search

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/adenosine-dev/adenosine/internal/federation"
	"github.com/adenosine-dev/adenosine/internal/profile"
)

type memoryStore struct {
	repositories []RepositoryResult
	profiles     []ProfileResult
	query        string
	sort         Sort
	limit        int
	viewerDID    string
	cursor       *Cursor
	calls        int
}

func (store *memoryStore) SearchRepositories(_ context.Context, query string, sort Sort, limit int, viewerDID string, cursor *Cursor) ([]RepositoryResult, error) {
	store.query, store.sort, store.limit, store.viewerDID, store.cursor = query, sort, limit, viewerDID, cursor
	store.calls++
	return store.repositories, nil
}

func (store *memoryStore) SearchProfiles(_ context.Context, query string, sort Sort, limit int, viewerDID string, cursor *Cursor) ([]ProfileResult, error) {
	store.query, store.sort, store.limit, store.viewerDID, store.cursor = query, sort, limit, viewerDID, cursor
	store.calls++
	return store.profiles, nil
}

func TestRepositorySearch(t *testing.T) {
	t.Parallel()
	indexedAt := time.Date(2026, 8, 11, 12, 0, 0, 0, time.UTC)
	results := []RepositoryResult{
		{Repository: federation.DiscoveryRepository{URI: "at://did:plc:alice/dev.adenosine.repo/one", IndexedAt: indexedAt}, Score: 0.8},
		{Repository: federation.DiscoveryRepository{URI: "at://did:plc:bob/dev.adenosine.repo/two", IndexedAt: indexedAt.Add(-time.Minute)}, Score: 0.7},
	}
	testCases := []struct {
		name           string
		query          string
		sort           Sort
		limit          int
		cursor         string
		results        []RepositoryResult
		wantErr        error
		wantCalls      int
		wantCount      int
		wantStoreLimit int
		wantCursor     bool
	}{
		{name: "defaults and trims query", query: "  forge ", results: nil, wantCalls: 1, wantStoreLimit: 21},
		{name: "bounded page emits cursor", query: "forge", limit: 1, results: results, wantCalls: 1, wantCount: 1, wantStoreLimit: 2, wantCursor: true},
		{name: "empty query", query: " ", wantErr: ErrInvalidQuery},
		{name: "wildcard only query", query: "%_", wantErr: ErrInvalidQuery},
		{name: "punctuation only query", query: "-*\\", wantErr: ErrInvalidQuery},
		{name: "invalid sort", query: "forge", sort: "popular", wantErr: ErrInvalidSort},
		{name: "invalid limit", query: "forge", limit: 51, wantErr: ErrInvalidLimit},
		{name: "invalid cursor", query: "forge", cursor: "invalid", wantErr: ErrInvalidCursor},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			store := &memoryStore{repositories: testCase.results}
			page, err := NewService(store).Repositories(context.Background(), testCase.query, testCase.sort, testCase.limit, testCase.cursor, "did:plc:viewer")
			if !errors.Is(err, testCase.wantErr) {
				t.Fatalf("error = %v, want %v", err, testCase.wantErr)
			}
			if store.calls != testCase.wantCalls || store.limit != testCase.wantStoreLimit {
				t.Fatalf("store calls/limit = %d/%d, want %d/%d", store.calls, store.limit, testCase.wantCalls, testCase.wantStoreLimit)
			}
			if err != nil {
				return
			}
			if len(page.Repositories) != testCase.wantCount || (page.NextCursor != nil) != testCase.wantCursor {
				t.Fatalf("page = %#v", page)
			}
			if store.query != "forge" || store.sort != SortRelevance || store.viewerDID != "did:plc:viewer" {
				t.Fatalf("store input = %q/%q/%q", store.query, store.sort, store.viewerDID)
			}
		})
	}
}

func TestEscapeLike(t *testing.T) {
	t.Parallel()
	testCases := []struct {
		name  string
		value string
		want  string
	}{
		{name: "plain text", value: "forge", want: "forge"},
		{name: "percent", value: "100%", want: `100\%`},
		{name: "underscore", value: "foo_bar", want: `foo\_bar`},
		{name: "escape character", value: `foo\bar`, want: `foo\\bar`},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			if got := escapeLike(testCase.value); got != testCase.want {
				t.Fatalf("escapeLike(%q) = %q, want %q", testCase.value, got, testCase.want)
			}
		})
	}
}

func TestSearchCursorBinding(t *testing.T) {
	t.Parallel()
	indexedAt := time.Date(2026, 8, 11, 12, 0, 0, 0, time.UTC)
	cursor := encodeCursor("profile", "alice", SortRelevance, Cursor{Score: 0.5, IndexedAt: indexedAt, Identity: "did:plc:alice"})
	testCases := []struct {
		name    string
		query   string
		sort    Sort
		cursor  string
		wantErr error
	}{
		{name: "matching profile cursor", query: "alice", sort: SortRelevance, cursor: cursor},
		{name: "query mismatch", query: "bob", sort: SortRelevance, cursor: cursor, wantErr: ErrInvalidCursor},
		{name: "sort mismatch", query: "alice", sort: SortRecent, cursor: cursor, wantErr: ErrInvalidCursor},
		{name: "kind mismatch", query: "alice", sort: SortRelevance, cursor: encodeCursor("repository", "alice", SortRelevance, Cursor{Score: 0.5, IndexedAt: indexedAt, Identity: "uri"}), wantErr: ErrInvalidCursor},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			store := &memoryStore{profiles: []ProfileResult{{Profile: profile.Profile{DID: "did:plc:alice", IndexedAt: indexedAt}}}}
			_, err := NewService(store).Profiles(context.Background(), testCase.query, testCase.sort, 20, testCase.cursor, "")
			if !errors.Is(err, testCase.wantErr) {
				t.Fatalf("error = %v, want %v", err, testCase.wantErr)
			}
			if testCase.wantErr == nil && (store.cursor == nil || store.cursor.Identity != "did:plc:alice") {
				t.Fatalf("decoded cursor = %#v", store.cursor)
			}
		})
	}
}
