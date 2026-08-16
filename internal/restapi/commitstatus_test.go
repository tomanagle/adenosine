package restapi

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	generated "github.com/adenosine-dev/adenosine/api/generated/go"
	"github.com/adenosine-dev/adenosine/internal/auth"
	"github.com/adenosine-dev/adenosine/internal/commitstatus"
	"github.com/adenosine-dev/adenosine/internal/repository"
	"github.com/google/uuid"
)

type memoryCommitStatuses struct {
	repositoryID repository.ID
	creatorDID   string
	statusInput  commitstatus.StatusInput
	status       commitstatus.CommitStatus
	checkInput   commitstatus.CheckRunInput
	check        commitstatus.CheckRun
}

func (manager *memoryCommitStatuses) CreateStatus(_ context.Context, repositoryID repository.ID, creatorDID, sha string, input commitstatus.StatusInput, _ time.Time) (commitstatus.CommitStatus, bool, error) {
	manager.repositoryID, manager.creatorDID, manager.statusInput = repositoryID, creatorDID, input
	manager.status.CommitSHA, manager.status.Context, manager.status.State = sha, input.Context, input.State
	return manager.status, true, nil
}

func (manager *memoryCommitStatuses) PageStatuses(context.Context, repository.ID, string, *uuid.UUID, int) (commitstatus.Page[commitstatus.CommitStatus], error) {
	return commitstatus.Page[commitstatus.CommitStatus]{Items: []commitstatus.CommitStatus{manager.status}}, nil
}

func (manager *memoryCommitStatuses) Combined(context.Context, repository.ID, string) (commitstatus.Combined, error) {
	return commitstatus.Combined{SHA: manager.status.CommitSHA, State: manager.status.State, Items: []commitstatus.CommitStatus{manager.status}}, nil
}

func (manager *memoryCommitStatuses) CreateCheckRun(_ context.Context, repositoryID repository.ID, creatorDID string, input commitstatus.CheckRunInput, _ time.Time) (commitstatus.CheckRun, bool, error) {
	manager.repositoryID, manager.creatorDID, manager.checkInput = repositoryID, creatorDID, input
	manager.check.CommitSHA, manager.check.Name, manager.check.Status = input.CommitSHA, input.Name, input.Status
	return manager.check, true, nil
}

func (manager *memoryCommitStatuses) GetCheckRun(context.Context, repository.ID, uuid.UUID) (commitstatus.CheckRun, error) {
	return manager.check, nil
}

func (manager *memoryCommitStatuses) PageCheckRuns(context.Context, repository.ID, string, *uuid.UUID, int) (commitstatus.Page[commitstatus.CheckRun], error) {
	return commitstatus.Page[commitstatus.CheckRun]{Items: []commitstatus.CheckRun{manager.check}}, nil
}

func (manager *memoryCommitStatuses) UpdateCheckRun(_ context.Context, repositoryID repository.ID, creatorDID string, _ uuid.UUID, input commitstatus.CheckRunUpdate, _ time.Time) (commitstatus.CheckRun, bool, error) {
	manager.repositoryID, manager.creatorDID = repositoryID, creatorDID
	manager.check.Status, manager.check.Version = input.Status, input.ExpectedVersion+1
	return manager.check, true, nil
}

func TestCommitStatusEndpoints(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 8, 14, 0, 0, 0, 0, time.UTC)
	repositoryID := repository.ID(uuid.MustParse("0198a851-2a89-7ae2-a370-dc68883e3af1"))
	statusID := uuid.MustParse("0198a851-2a89-7ae2-a370-dc68883e3af2")
	checkID := uuid.MustParse("0198a851-2a89-7ae2-a370-dc68883e3af3")
	sha := strings.Repeat("e", 40)
	repo := repository.Repository{ID: repositoryID, OwnerDID: "did:plc:alice", Slug: "project", State: repository.StateActive, Visibility: repository.VisibilityPublic}
	testCases := []struct {
		name       string
		method     string
		path       string
		body       string
		token      auth.AccessToken
		wantStatus int
		assert     func(*testing.T, *memoryCommitStatuses, []byte)
	}{
		{name: "repository-scoped CI reports status", method: http.MethodPost, path: "/api/v1/repositories/alice/project/commits/" + sha + "/statuses", body: `{"context":"ci/test","state":"success","external_id":"build-42"}`, token: auth.AccessToken{AccountDID: "did:plc:ci", Scopes: []string{auth.ScopeRepositoryStatus}, RepositoryID: &repositoryID}, wantStatus: http.StatusCreated, assert: func(t *testing.T, manager *memoryCommitStatuses, body []byte) {
			if manager.repositoryID != repositoryID || manager.creatorDID != "did:plc:ci" || manager.statusInput.ExternalID != "build-42" {
				t.Fatalf("status write = %s %q %+v", manager.repositoryID, manager.creatorDID, manager.statusInput)
			}
			var response generated.CommitStatus
			if err := json.Unmarshal(body, &response); err != nil || response.Context != "ci/test" {
				t.Fatalf("response = %+v, error = %v", response, err)
			}
		}},
		{name: "unscoped writer is forbidden", method: http.MethodPost, path: "/api/v1/repositories/alice/project/commits/" + sha + "/statuses", body: `{"context":"ci/test","state":"success","external_id":"build-42"}`, token: auth.AccessToken{AccountDID: "did:plc:ci", Scopes: []string{auth.ScopeRepositoryStatus}}, wantStatus: http.StatusForbidden, assert: func(*testing.T, *memoryCommitStatuses, []byte) {}},
		{name: "status history is object wrapped", method: http.MethodGet, path: "/api/v1/repositories/alice/project/commits/" + sha + "/statuses?limit=10", wantStatus: http.StatusOK, assert: func(t *testing.T, _ *memoryCommitStatuses, body []byte) {
			var response generated.CommitStatusList
			if err := json.Unmarshal(body, &response); err != nil || len(response.Items) != 1 || response.Page.NextCursor != nil {
				t.Fatalf("response = %+v, error = %v", response, err)
			}
		}},
		{name: "repository-scoped CI creates check run", method: http.MethodPost, path: "/api/v1/repositories/alice/project/commits/" + sha + "/check-runs", body: `{"name":"test","external_id":"check-42"}`, token: auth.AccessToken{AccountDID: "did:plc:ci", Scopes: []string{auth.ScopeRepositoryStatus}, RepositoryID: &repositoryID}, wantStatus: http.StatusCreated, assert: func(t *testing.T, manager *memoryCommitStatuses, body []byte) {
			if manager.checkInput.Name != "test" || manager.checkInput.Status != commitstatus.CheckQueued {
				t.Fatalf("check input = %+v", manager.checkInput)
			}
			var response generated.CheckRun
			if err := json.Unmarshal(body, &response); err != nil || response.Name != "test" {
				t.Fatalf("response = %+v, error = %v", response, err)
			}
		}},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			manager := &memoryCommitStatuses{
				status: commitstatus.CommitStatus{ID: statusID, RepositoryID: repositoryID, CommitSHA: sha, Context: "ci/test", State: commitstatus.StateSuccess, CreatorDID: "did:plc:ci", ExternalID: "build-42", CreatedAt: now},
				check:  commitstatus.CheckRun{ID: checkID, RepositoryID: repositoryID, CommitSHA: sha, Name: "test", ExternalID: "check-42", CreatorDID: "did:plc:ci", Status: commitstatus.CheckQueued, Version: 1, CreatedAt: now, UpdatedAt: now},
			}
			server := testAPIServer(t, Dependencies{Repositories: fixedRepositoryManager{repository: repo}, TokenAuth: configuredTokenAuth{token: testCase.token}, Git: fakeGitReader{}, CommitStatuses: manager})
			response := performAPIRequest(server, testCase.method, testCase.path, testCase.body, false, false, "valid-pat")
			if response.Code != testCase.wantStatus {
				t.Fatalf("status = %d, want %d: %s", response.Code, testCase.wantStatus, response.Body.String())
			}
			testCase.assert(t, manager, response.Body.Bytes())
		})
	}
}
