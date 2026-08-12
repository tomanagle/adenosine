package restapi

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	generated "github.com/adenosine-dev/adenosine/api/generated/go"
	"github.com/adenosine-dev/adenosine/internal/federation"
	"github.com/adenosine-dev/adenosine/internal/repository"
	"github.com/google/uuid"
)

type restDiscoveryStore struct {
	repositories []federation.DiscoveryRepository
	calls        int
}

func (store *restDiscoveryStore) ListNetworkRepositories(context.Context, int, *federation.DiscoveryCursor) ([]federation.DiscoveryRepository, error) {
	store.calls++
	return store.repositories, nil
}

func TestNetworkRepositoryDiscoveryEndpoint(t *testing.T) {
	t.Parallel()
	localID := uuid.MustParse("0198a851-2a89-7ae2-a370-dc68883e3af3")
	indexedAt := time.Date(2026, 8, 9, 12, 0, 0, 0, time.UTC)
	remote := federation.DiscoveryRepository{
		URI: "at://did:plc:alice/dev.adenosine.repo/project", CID: "bafyreiproject", OwnerDID: "did:plc:alice", OwnerHandle: "alice.test",
		Slug: "project", Name: "Project", Description: "Public project", DefaultBranch: "main",
		GitHTTPS: "https://git.example/alice/project.git", GitSSH: "ssh://git@git.example/alice/project.git",
		Web: "https://git.example/alice/project", CreatedAt: indexedAt.Add(-time.Hour), UpdatedAt: indexedAt, IndexedAt: indexedAt,
		StarCount: 12, IssueCount: 9, OpenIssueCount: 5,
	}
	local := remote
	local.URI = "at://did:plc:alice/dev.adenosine.repo/local"
	local.Slug = "local"
	local.LocalRepositoryID = &localID
	testCases := []struct {
		name          string
		path          string
		repositories  []federation.DiscoveryRepository
		wantStatus    int
		wantCount     int
		wantStoreCall int
		assert        func(*testing.T, generated.NetworkRepositoryList)
	}{
		{
			name: "anonymous remote and local repositories", path: "/api/v1/network/repositories?limit=2",
			repositories: []federation.DiscoveryRepository{remote, local}, wantStatus: http.StatusOK, wantCount: 2, wantStoreCall: 1,
			assert: func(t *testing.T, body generated.NetworkRepositoryList) {
				if body.Data[0].Id != nil || body.Data[0].Hosting.Local || body.Data[0].Hosting.SourceBrowsing != generated.CanonicalHost || body.Data[0].Uri == nil || *body.Data[0].Uri != remote.URI || body.Data[0].Cid == nil || *body.Data[0].Cid != remote.CID {
					t.Fatalf("remote repository identity/hosting = %#v", body.Data[0])
				}
				if body.Data[0].Owner.Handle == nil || *body.Data[0].Owner.Handle != "alice.test" || body.Data[0].Visibility != generated.RepositoryVisibilityPublic || body.Data[0].State != generated.Active {
					t.Fatalf("remote repository projection = %#v", body.Data[0])
				}
				if body.Data[0].StarCount != 12 || body.Data[0].IssueCount != 9 || body.Data[0].OpenIssueCount != 5 {
					t.Fatalf("remote repository counts = %d/%d/%d", body.Data[0].StarCount, body.Data[0].IssueCount, body.Data[0].OpenIssueCount)
				}
				if body.Data[1].Id == nil || uuid.UUID(*body.Data[1].Id) != localID || !body.Data[1].Hosting.Local || body.Data[1].Hosting.SourceBrowsing != generated.Local {
					t.Fatalf("local repository identity/hosting = %#v", body.Data[1])
				}
			},
		},
		{name: "empty data is array and cursor is null", path: "/api/v1/network/repositories", repositories: nil, wantStatus: http.StatusOK, wantStoreCall: 1},
		{name: "invalid limit", path: "/api/v1/network/repositories?limit=101", wantStatus: http.StatusBadRequest},
		{name: "invalid cursor", path: "/api/v1/network/repositories?cursor=invalid", wantStatus: http.StatusBadRequest},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			store := &restDiscoveryStore{repositories: testCase.repositories}
			server, err := NewServer(":0", "http://localhost:8080", fakeReadiness{}, slog.New(slog.NewTextHandler(io.Discard, nil)), Dependencies{Discovery: federation.NewDiscoveryService(store)}, nil)
			if err != nil {
				t.Fatalf("create server: %v", err)
			}
			request := httptest.NewRequest(http.MethodGet, testCase.path, nil)
			response := httptest.NewRecorder()
			server.Handler.ServeHTTP(response, request)
			if response.Code != testCase.wantStatus || store.calls != testCase.wantStoreCall {
				t.Fatalf("status/store calls = %d/%d, want %d/%d; body=%s", response.Code, store.calls, testCase.wantStatus, testCase.wantStoreCall, response.Body.String())
			}
			if testCase.wantStatus != http.StatusOK {
				var body generated.ErrorResponse
				if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil || body.Error.Code != "malformed_request" {
					t.Fatalf("error response = %#v, %v", body, err)
				}
				return
			}
			var body generated.NetworkRepositoryList
			if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
				t.Fatalf("decode response: %v", err)
			}
			if body.Data == nil || len(body.Data) != testCase.wantCount || body.Page.NextCursor != nil {
				t.Fatalf("page = %#v, want data length %d and null cursor", body, testCase.wantCount)
			}
			if !strings.Contains(response.Body.String(), `"next_cursor":null`) {
				t.Fatalf("response does not contain a stable null next_cursor: %s", response.Body.String())
			}
			if len(body.Data) > 0 && !strings.Contains(response.Body.String(), `"comment_count":`) {
				t.Fatalf("response does not contain repository comment_count: %s", response.Body.String())
			}
			if testCase.assert != nil {
				testCase.assert(t, body)
			}
		})
	}
}

type resolvingRESTSearch struct {
	restSearch
	repository federation.DiscoveryRepository
	viewerDID  string
}

func (search *resolvingRESTSearch) ResolveRepository(_ context.Context, owner, slug, viewerDID string) (federation.DiscoveryRepository, error) {
	search.viewerDID = viewerDID
	if owner != "alice.test" || slug != search.repository.Slug {
		return federation.DiscoveryRepository{}, repository.ErrNotFound
	}
	return search.repository, nil
}

func TestRemoteRepositoryMetadataEndpoint(t *testing.T) {
	t.Parallel()
	testCases := []struct {
		name       string
		cookie     string
		wantStatus int
		wantViewer string
	}{
		{name: "anonymous", wantStatus: http.StatusOK},
		{name: "moderated session viewer", cookie: "valid-session", wantStatus: http.StatusOK, wantViewer: "did:plc:alice"},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			search := &resolvingRESTSearch{repository: federation.DiscoveryRepository{URI: "at://did:plc:alice/dev.adenosine.repo/project", OwnerDID: "did:plc:alice", OwnerHandle: "alice.test", Slug: "project", Web: "https://remote.example/alice/project", GitHTTPS: "https://remote.example/alice/project.git"}}
			server := testAPIServer(t, Dependencies{Repositories: fakeRepositories{}, Search: search, Sessions: fakeSessions{}})
			request := httptest.NewRequest(http.MethodGet, "/api/v1/repositories/alice.test/project", nil)
			if testCase.cookie != "" {
				request.AddCookie(&http.Cookie{Name: "adenosine_session", Value: testCase.cookie})
			}
			response := httptest.NewRecorder()
			server.Handler.ServeHTTP(response, request)
			if response.Code != testCase.wantStatus || search.viewerDID != testCase.wantViewer {
				t.Fatalf("status/viewer = %d/%q, want %d/%q: %s", response.Code, search.viewerDID, testCase.wantStatus, testCase.wantViewer, response.Body.String())
			}
			var body generated.Repository
			if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil || body.Hosting.SourceBrowsing != generated.CanonicalHost || body.Hosting.Local {
				t.Fatalf("repository = %#v, %v", body, err)
			}
		})
	}
}

func TestLocalRepositoryResponseIdentity(t *testing.T) {
	t.Parallel()
	id := repository.ID(uuid.MustParse("0198a851-2a89-7ae2-a370-dc68883e3af4"))
	testCases := []struct {
		name      string
		repo      repository.Repository
		endpoints RepositoryEndpointBuilder
	}{
		{name: "pointer ID and AT identity", repo: repository.Repository{ID: id, OwnerDID: "did:plc:alice", Slug: "project", ATURI: "at://did:plc:alice/dev.adenosine.repo/project", ATCID: "bafyreiproject"}},
		{name: "configured hosting endpoints", repo: repository.Repository{ID: id, OwnerDID: "did:plc:alice", Slug: "project"}, endpoints: fixedRESTEndpoints{}},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			response := (&apiHandler{baseURL: "https://git.example", deps: Dependencies{Endpoints: testCase.endpoints}}).repositoryResponse(testCase.repo)
			if response.Id == nil || uuid.UUID(*response.Id) != uuid.UUID(id) || response.Uri == nil || *response.Uri != testCase.repo.ATURI || response.Cid == nil || *response.Cid != testCase.repo.ATCID {
				if testCase.endpoints == nil {
					t.Fatalf("repository identity = %#v", response)
				}
			}
			if testCase.endpoints != nil && (response.Hosting.GitHttpsUrl != "https://host.example/project.git" || response.Hosting.GitSshUrl == nil || *response.Hosting.GitSshUrl != "ssh://git@host.example/project.git") {
				t.Fatalf("repository hosting = %#v", response.Hosting)
			}
		})
	}
}

type fixedRESTEndpoints struct{}

func (fixedRESTEndpoints) For(repository.Repository) (string, string, string) {
	return "https://host.example/project", "https://host.example/project.git", "ssh://git@host.example/project.git"
}
