package restapi

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	generated "github.com/adenosine-dev/adenosine/api/generated/go"
	"github.com/adenosine-dev/adenosine/internal/federation"
	"github.com/adenosine-dev/adenosine/internal/repository"
	searchservice "github.com/adenosine-dev/adenosine/internal/search"
	"github.com/google/uuid"
)

type forkRepositoryManager struct {
	source      repository.Repository
	created     repository.Repository
	createInput repository.CreateInput
	syncResult  repository.ForkSync
}

func (manager *forkRepositoryManager) Create(_ context.Context, input repository.CreateInput) (repository.Repository, error) {
	manager.createInput = input
	manager.created.ForkedFrom = input.ForkedFrom
	return manager.created, nil
}

func (manager *forkRepositoryManager) GetByOwnerSlug(_ context.Context, owner, slug string) (repository.Repository, error) {
	if owner == "alice" && slug == manager.source.Slug {
		return manager.source, nil
	}
	if owner == "bob" && slug == manager.created.Slug {
		return manager.created, nil
	}
	return repository.Repository{}, repository.ErrNotFound
}

func (*forkRepositoryManager) ListByOrganization(context.Context, uuid.UUID) ([]repository.Repository, error) {
	return nil, nil
}

func (manager *forkRepositoryManager) SyncFork(context.Context, repository.Repository) (repository.ForkSync, error) {
	return manager.syncResult, nil
}

type forkSearch struct {
	restSearch
	page searchservice.ForkPage
}

func (search *forkSearch) PageForks(context.Context, string, string, int, string) (searchservice.ForkPage, error) {
	return search.page, nil
}

type forkAuthorization struct{ fakeAuthorization }

func (forkAuthorization) CanWriteRepository(context.Context, string, repository.ID) (bool, error) {
	return true, nil
}

func TestRepositoryForkEndpoints(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 8, 13, 12, 0, 0, 0, time.UTC)
	upstreamID := repository.ID(uuid.MustParse("0198a851-2a89-7ae2-a370-dc68883e3af1"))
	forkID := repository.ID(uuid.MustParse("0198a851-2a89-7ae2-a370-dc68883e3af2"))
	upstream := repository.Repository{
		ID: upstreamID, OwnerDID: "did:plc:alice", Slug: "project", DisplayName: "Project",
		Description: "Portable project", Visibility: repository.VisibilityPublic, State: repository.StateActive,
		DefaultBranch: "main", ATURI: "at://did:plc:alice/dev.adenosine.repo/project", ATCID: "bafyupstream",
		CreatedAt: now, UpdatedAt: now,
	}
	created := repository.Repository{
		ID: forkID, OwnerDID: "did:plc:alice", Slug: "project-copy", Visibility: repository.VisibilityPublic,
		State: repository.StateActive, DefaultBranch: "main", ATURI: "at://did:plc:alice/dev.adenosine.repo/project-copy",
		ATCID: "bafyfork", CreatedAt: now, UpdatedAt: now,
	}
	projectedFork := federation.DiscoveryRepository{
		URI: "at://did:plc:bob/dev.adenosine.repo/project", CID: "bafybobfork", OwnerDID: "did:plc:bob",
		OwnerHandle: "bob", Slug: "project", Name: "Project", DefaultBranch: "main",
		GitHTTPS: "https://git.example/bob/project.git", Web: "https://git.example/bob/project",
		CreatedAt: now, UpdatedAt: now, IndexedAt: now,
	}
	testCases := []struct {
		name       string
		method     string
		path       string
		body       string
		session    bool
		origin     bool
		wantStatus int
		assert     func(*testing.T, *forkRepositoryManager, []byte)
	}{
		{
			name: "creates a personal fork", method: http.MethodPost, path: "/api/v1/repositories/alice/project/forks",
			body: `{"slug":"project-copy"}`, session: true, origin: true, wantStatus: http.StatusCreated,
			assert: func(t *testing.T, manager *forkRepositoryManager, body []byte) {
				if manager.createInput.ForkedFrom == nil || manager.createInput.ForkedFrom.URI != upstream.ATURI || manager.createInput.OwnerDID != "did:plc:alice" || manager.createInput.Slug != "project-copy" {
					t.Fatalf("create input = %+v", manager.createInput)
				}
				var response generated.Repository
				if err := json.Unmarshal(body, &response); err != nil || response.ForkedFrom == nil || response.ForkedFrom.Uri != upstream.ATURI {
					t.Fatalf("response = %+v, error = %v", response, err)
				}
			},
		},
		{
			name: "lists direct forks in an envelope", method: http.MethodGet,
			path: "/api/v1/repositories/alice/project/forks?limit=20", wantStatus: http.StatusOK,
			assert: func(t *testing.T, _ *forkRepositoryManager, body []byte) {
				var response generated.RepositoryForkList
				if err := json.Unmarshal(body, &response); err != nil || len(response.Items) != 1 || response.ForkCount != 1 || response.Page.NextCursor != nil {
					t.Fatalf("response = %+v, error = %v", response, err)
				}
			},
		},
		{
			name: "syncs an authorized fork", method: http.MethodPost,
			path: "/api/v1/repositories/bob/project-copy/sync-fork", session: true, origin: true, wantStatus: http.StatusOK,
			assert: func(t *testing.T, _ *forkRepositoryManager, body []byte) {
				var response generated.RepositoryForkSync
				if err := json.Unmarshal(body, &response); err != nil || !response.Updated || response.AfterSha != "after" {
					t.Fatalf("response = %+v, error = %v", response, err)
				}
			},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			manager := &forkRepositoryManager{source: upstream, created: created, syncResult: repository.ForkSync{BeforeSHA: "before", AfterSHA: "after", Updated: true}}
			search := &forkSearch{page: searchservice.ForkPage{Repositories: []federation.DiscoveryRepository{projectedFork}, ForkCount: 1}}
			server := testAPIServer(t, Dependencies{
				Repositories: manager, Search: search, Sessions: fakeSessions{}, Authorization: forkAuthorization{},
			})
			response := performAPIRequest(server, testCase.method, testCase.path, testCase.body, testCase.session, testCase.origin, "")
			if response.Code != testCase.wantStatus {
				t.Fatalf("status = %d, want %d: %s", response.Code, testCase.wantStatus, response.Body.String())
			}
			testCase.assert(t, manager, response.Body.Bytes())
		})
	}
}
