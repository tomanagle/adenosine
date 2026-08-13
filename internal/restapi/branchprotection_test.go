package restapi

import (
	"context"
	"encoding/json"
	"net/http"
	"reflect"
	"testing"
	"time"

	generated "github.com/adenosine-dev/adenosine/api/generated/go"
	"github.com/adenosine-dev/adenosine/internal/branchprotection"
	"github.com/adenosine-dev/adenosine/internal/repository"
	"github.com/google/uuid"
)

type memoryBranchProtections struct {
	input branchprotection.Input
	value branchprotection.Protection
}

type branchProtectionAuthorizer struct{}

func (branchProtectionAuthorizer) CanReadRepository(context.Context, string, repository.ID) (bool, error) {
	return true, nil
}

func (branchProtectionAuthorizer) CanAdminRepository(context.Context, string, repository.ID) (bool, error) {
	return true, nil
}

func (manager *memoryBranchProtections) Create(_ context.Context, repositoryID repository.ID, input branchprotection.Input, _ time.Time) (branchprotection.Protection, error) {
	manager.input = input
	manager.value.RepositoryID = repositoryID
	manager.value.Pattern = input.Pattern
	manager.value.DenyForcePush = input.DenyForcePush
	manager.value.DenyDeletion = input.DenyDeletion
	manager.value.RequiredApprovals = input.RequiredApprovals
	manager.value.DismissStaleReviews = input.DismissStaleReviews
	manager.value.RequiredStatusChecks = input.RequiredStatusChecks
	manager.value.RequireSignedCommits = input.RequireSignedCommits
	return manager.value, nil
}

func (manager *memoryBranchProtections) Get(context.Context, repository.ID, uuid.UUID) (branchprotection.Protection, error) {
	return manager.value, nil
}

func (manager *memoryBranchProtections) Page(context.Context, repository.ID, *uuid.UUID, int) (branchprotection.Page, error) {
	return branchprotection.Page{Items: []branchprotection.Protection{manager.value}}, nil
}

func (manager *memoryBranchProtections) Update(_ context.Context, _ repository.ID, _ uuid.UUID, input branchprotection.Input, _ time.Time) (branchprotection.Protection, error) {
	manager.input = input
	return manager.value, nil
}

func (*memoryBranchProtections) Delete(context.Context, repository.ID, uuid.UUID) error { return nil }

func TestBranchProtectionEndpoints(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 8, 14, 0, 0, 0, 0, time.UTC)
	repositoryID := repository.ID(uuid.MustParse("0198a851-2a89-7ae2-a370-dc68883e3af1"))
	protectionID := uuid.MustParse("0198a851-2a89-7ae2-a370-dc68883e3af2")
	repo := repository.Repository{ID: repositoryID, OwnerDID: "did:plc:alice", Slug: "project", State: repository.StateActive, Visibility: repository.VisibilityPublic}
	testCases := []struct {
		name       string
		method     string
		path       string
		body       string
		wantStatus int
		assert     func(*testing.T, *memoryBranchProtections, []byte)
	}{
		{
			name: "advanced policy round trips every field", method: http.MethodPost,
			path:       "/api/v1/repositories/alice/project/branch-protections",
			body:       `{"pattern":"release/*","deny_force_push":true,"deny_deletion":true,"required_approvals":2,"dismiss_stale_reviews":true,"required_status_checks":["ci/test","ci/lint"],"require_signed_commits":true}`,
			wantStatus: http.StatusCreated,
			assert: func(t *testing.T, manager *memoryBranchProtections, body []byte) {
				wantChecks := []string{"ci/test", "ci/lint"}
				if manager.input.Pattern != "release/*" || manager.input.RequiredApprovals != 2 || !manager.input.DenyForcePush || !manager.input.DenyDeletion || !manager.input.DismissStaleReviews || !manager.input.RequireSignedCommits || !reflect.DeepEqual(manager.input.RequiredStatusChecks, wantChecks) {
					t.Fatalf("branch protection input = %+v", manager.input)
				}
				var response generated.BranchProtection
				if err := json.Unmarshal(body, &response); err != nil || response.Pattern != "release/*" || response.RequiredApprovals != 2 || !response.RequireSignedCommits || !reflect.DeepEqual(response.RequiredStatusChecks, wantChecks) {
					t.Fatalf("response = %+v, error = %v", response, err)
				}
			},
		},
		{
			name: "list is object wrapped", method: http.MethodGet,
			path:       "/api/v1/repositories/alice/project/branch-protections?limit=10",
			wantStatus: http.StatusOK,
			assert: func(t *testing.T, _ *memoryBranchProtections, body []byte) {
				var response generated.BranchProtectionList
				if err := json.Unmarshal(body, &response); err != nil || len(response.Items) != 1 || response.Page.NextCursor != nil {
					t.Fatalf("response = %+v, error = %v", response, err)
				}
			},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			manager := &memoryBranchProtections{value: branchprotection.Protection{
				ID: protectionID, RepositoryID: repositoryID, Pattern: "main", DenyForcePush: true,
				CreatedAt: now, UpdatedAt: now,
			}}
			server := testAPIServer(t, Dependencies{Repositories: fixedRepositoryManager{repository: repo}, TokenAuth: fakeTokenAuth{}, Authorization: branchProtectionAuthorizer{}, Git: fakeGitReader{}, BranchProtections: manager})
			response := performAPIRequest(server, testCase.method, testCase.path, testCase.body, false, false, "valid-pat")
			if response.Code != testCase.wantStatus {
				t.Fatalf("status = %d, want %d: %s", response.Code, testCase.wantStatus, response.Body.String())
			}
			testCase.assert(t, manager, response.Body.Bytes())
		})
	}
}
