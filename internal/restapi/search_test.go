package restapi

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	generated "github.com/adenosine-dev/adenosine/api/generated/go"
	"github.com/adenosine-dev/adenosine/internal/federation"
	"github.com/adenosine-dev/adenosine/internal/issue"
	"github.com/adenosine-dev/adenosine/internal/profile"
	"github.com/adenosine-dev/adenosine/internal/pullrequest"
	searchservice "github.com/adenosine-dev/adenosine/internal/search"
	"github.com/adenosine-dev/adenosine/internal/star"
)

type restSearch struct {
	viewerDID string
	calls     int
}

type viewerCollaborationSearch struct {
	restSearch
	viewerDID string
}

func (search *viewerCollaborationSearch) ResolveProfile(_ context.Context, did, viewerDID string) (profile.Profile, error) {
	search.viewerDID = viewerDID
	return profile.Profile{DID: did, RepositoryCount: 1, ContributionCount: 2, IndexedAt: time.Now()}, nil
}
func (search *viewerCollaborationSearch) ListIssues(context.Context, string, string) (issue.Projection, error) {
	return issue.Projection{}, nil
}
func (search *viewerCollaborationSearch) ListStars(context.Context, string, string) (star.Projection, error) {
	return star.Projection{}, nil
}
func (search *viewerCollaborationSearch) ListPullRequests(context.Context, string, string) (pullrequest.Projection, error) {
	return pullrequest.Projection{}, nil
}
func (search *viewerCollaborationSearch) ResolvePullRequest(context.Context, string, string) (pullrequest.ProjectedPullRequest, error) {
	return pullrequest.ProjectedPullRequest{}, nil
}
func (search *viewerCollaborationSearch) ListPullRequestReviews(context.Context, string, string) ([]pullrequest.ProjectedReview, error) {
	return []pullrequest.ProjectedReview{}, nil
}

func (search *restSearch) Repositories(_ context.Context, _ string, _ searchservice.Sort, _ int, _ string, viewerDID string) (searchservice.RepositoryPage, error) {
	search.calls++
	search.viewerDID = viewerDID
	indexedAt := time.Date(2026, 8, 11, 12, 0, 0, 0, time.UTC)
	return searchservice.RepositoryPage{Repositories: []federation.DiscoveryRepository{{
		URI: "at://did:plc:bob/dev.adenosine.repo/forge", CID: "bafyreiforge", OwnerDID: "did:plc:bob", OwnerHandle: "bob.test",
		Slug: "forge", Name: "Forge", DefaultBranch: "main", GitHTTPS: "https://remote.example/bob/forge.git", Web: "https://remote.example/bob/forge",
		CreatedAt: indexedAt, UpdatedAt: indexedAt, IndexedAt: indexedAt,
	}}}, nil
}

func TestCollaborationReadUsesSessionViewer(t *testing.T) {
	t.Parallel()
	testCases := []struct{ name, cookie, wantViewer string }{
		{name: "anonymous"},
		{name: "session", cookie: "valid-session", wantViewer: "did:plc:alice"},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			search := &viewerCollaborationSearch{}
			server := testAPIServer(t, Dependencies{Search: search, Sessions: fakeSessions{}})
			request := httptest.NewRequest(http.MethodGet, "/api/v1/profiles/did:plc:bob", nil)
			if testCase.cookie != "" {
				request.AddCookie(&http.Cookie{Name: "adenosine_session", Value: testCase.cookie})
			}
			response := httptest.NewRecorder()
			server.Handler.ServeHTTP(response, request)
			if response.Code != http.StatusOK || search.viewerDID != testCase.wantViewer || response.Header().Get("Vary") != "Cookie" {
				t.Fatalf("status/viewer/vary = %d/%q/%q", response.Code, search.viewerDID, response.Header().Get("Vary"))
			}
		})
	}
}

func (search *restSearch) Profiles(_ context.Context, _ string, sort searchservice.Sort, _ int, _ string, viewerDID string) (searchservice.ProfilePage, error) {
	search.calls++
	search.viewerDID = viewerDID
	if sort != "" && sort != searchservice.SortRelevance && sort != searchservice.SortRecent {
		return searchservice.ProfilePage{}, searchservice.ErrInvalidSort
	}
	return searchservice.ProfilePage{Profiles: []profile.Profile{{DID: "did:plc:bob", Handle: "bob.test", IndexedAt: time.Date(2026, 8, 11, 12, 0, 0, 0, time.UTC)}}}, nil
}

func TestSearchEndpoints(t *testing.T) {
	t.Parallel()
	testCases := []struct {
		name       string
		path       string
		cookie     string
		wantStatus int
		wantCalls  int
		wantViewer string
		decode     func(*testing.T, []byte)
	}{
		{name: "anonymous repositories", path: "/api/v1/search/repositories?q=forge", wantStatus: http.StatusOK, wantCalls: 1, decode: func(t *testing.T, body []byte) {
			var page generated.RepositorySearchPage
			if err := json.Unmarshal(body, &page); err != nil || len(page.Data) != 1 || page.Data[0].Uri == nil || page.Data[0].Hosting.Local {
				t.Fatalf("repository page = %#v, %v", page, err)
			}
		}},
		{name: "personalized profiles", path: "/api/v1/search/profiles?q=bob&type=ignored", cookie: "valid-session", wantStatus: http.StatusOK, wantCalls: 1, wantViewer: "did:plc:alice", decode: func(t *testing.T, body []byte) {
			var page generated.ProfileSearchPage
			if err := json.Unmarshal(body, &page); err != nil || len(page.Data) != 1 || page.Data[0].Did != "did:plc:bob" {
				t.Fatalf("profile page = %#v, %v", page, err)
			}
		}},
		{name: "invalid session is not anonymous", path: "/api/v1/search/repositories?q=forge", cookie: "invalid", wantStatus: http.StatusUnauthorized},
		{name: "missing query", path: "/api/v1/search/repositories", wantStatus: http.StatusBadRequest},
		{name: "unsupported sort", path: "/api/v1/search/profiles?q=bob&sort=popular", wantStatus: http.StatusBadRequest, wantCalls: 1},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			search := &restSearch{}
			server, err := NewServer(":0", "http://localhost:8080", fakeReadiness{}, slog.New(slog.NewTextHandler(io.Discard, nil)), Dependencies{Sessions: fakeSessions{}, Search: search}, nil)
			if err != nil {
				t.Fatal(err)
			}
			request := httptest.NewRequest(http.MethodGet, testCase.path, nil)
			if testCase.cookie != "" {
				request.AddCookie(&http.Cookie{Name: "adenosine_session", Value: testCase.cookie})
			}
			response := httptest.NewRecorder()
			server.Handler.ServeHTTP(response, request)
			if response.Code != testCase.wantStatus || search.calls != testCase.wantCalls || search.viewerDID != testCase.wantViewer {
				t.Fatalf("status/calls/viewer = %d/%d/%q, want %d/%d/%q; body=%s", response.Code, search.calls, search.viewerDID, testCase.wantStatus, testCase.wantCalls, testCase.wantViewer, response.Body.String())
			}
			if testCase.decode != nil {
				testCase.decode(t, response.Body.Bytes())
			}
		})
	}
}
