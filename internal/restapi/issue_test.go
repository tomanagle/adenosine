package restapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	generated "github.com/adenosine-dev/adenosine/api/generated/go"
	"github.com/adenosine-dev/adenosine/internal/issue"
)

const (
	restIssueRepositoryURI = "at://did:plc:alice/dev.adenosine.repo/project"
	restIssueURI           = "at://did:plc:bob/dev.adenosine.issue/0198a8512a897ae2a370dc68883e3af1"
	restIssueCID           = "bafybeigdyrzt5sfp7udm7hu76uh7y26nf3efuylqabf3oclgtqy55fbzdi"
)

type fakeIssues struct {
	projection    issue.Projection
	created       issue.Issue
	status        issue.Status
	err           error
	operation     string
	authorDID     string
	repositoryURI string
	createInput   issue.CreateInput
	statusInput   issue.StatusInput
	calls         int
}

func (manager *fakeIssues) Get(_ context.Context, uri string) (issue.Projection, error) {
	manager.calls++
	manager.operation, manager.repositoryURI = "get", uri
	return manager.projection, manager.err
}

func (manager *fakeIssues) Create(_ context.Context, did string, input issue.CreateInput) (issue.Issue, error) {
	manager.calls++
	manager.operation, manager.authorDID, manager.createInput = "create", did, input
	return manager.created, manager.err
}

func (manager *fakeIssues) PutStatus(_ context.Context, did string, input issue.StatusInput) (issue.Status, error) {
	manager.calls++
	manager.operation, manager.authorDID, manager.statusInput = "status", did, input
	return manager.status, manager.err
}

func TestIssueEndpoints(t *testing.T) {
	t.Parallel()
	createdAt := time.Date(2026, 8, 9, 11, 0, 0, 0, time.UTC)
	updatedAt := createdAt.Add(time.Minute)
	indexedAt := updatedAt.Add(time.Minute)
	repository := issue.StrongRef{URI: restIssueRepositoryURI, CID: restIssueCID}
	created := issue.Issue{URI: restIssueURI, CID: restIssueCID, AuthorDID: "did:plc:alice", Record: issue.Record{Repository: repository, Title: "title", Body: "body", CreatedAt: createdAt, UpdatedAt: updatedAt}}
	projected := issue.ProjectedIssue{Issue: created, State: issue.StateOpen, Status: issue.StrongRef{}, IndexedAt: indexedAt}
	status := issue.Status{URI: "at://did:plc:alice/dev.adenosine.issueStatus/key", CID: restIssueCID, AuthorDID: "did:plc:alice", StatusRecord: issue.StatusRecord{Subject: issue.StrongRef{URI: restIssueURI, CID: restIssueCID}, Repository: repository, State: issue.StateClosed, CreatedAt: createdAt, UpdatedAt: updatedAt}}
	testCases := []struct {
		name          string
		method        string
		path          string
		body          string
		session       bool
		pat           bool
		origin        string
		manager       *fakeIssues
		wantStatus    int
		wantCalls     int
		wantOperation string
		wantCode      string
	}{
		{name: "anonymous projected read returns exact issue and counts", method: http.MethodGet, path: "/api/v1/issues?repository_uri=" + restIssueRepositoryURI, manager: &fakeIssues{projection: issue.Projection{IssueCount: 7, OpenIssueCount: 4, Issues: []issue.ProjectedIssue{projected}}}, wantStatus: http.StatusOK, wantCalls: 1, wantOperation: "get"},
		{name: "repository URI is required", method: http.MethodGet, path: "/api/v1/issues", manager: &fakeIssues{}, wantStatus: http.StatusBadRequest},
		{name: "create requires session", method: http.MethodPost, path: "/api/v1/issues", body: `{"repository_uri":"` + restIssueRepositoryURI + `","title":"title","body":"body"}`, manager: &fakeIssues{}, wantStatus: http.StatusUnauthorized, wantCode: "authentication_required"},
		{name: "create rejects PAT", method: http.MethodPost, path: "/api/v1/issues", body: `{"repository_uri":"` + restIssueRepositoryURI + `","title":"title","body":"body"}`, pat: true, origin: "http://localhost:8080", manager: &fakeIssues{}, wantStatus: http.StatusForbidden, wantCode: "permission_denied"},
		{name: "create requires exact Origin", method: http.MethodPost, path: "/api/v1/issues", body: `{"repository_uri":"` + restIssueRepositoryURI + `","title":"title","body":"body"}`, session: true, origin: "http://evil.example", manager: &fakeIssues{}, wantStatus: http.StatusForbidden, wantCode: "permission_denied"},
		{name: "reporter DID cannot be injected", method: http.MethodPost, path: "/api/v1/issues", body: `{"repository_uri":"` + restIssueRepositoryURI + `","title":"title","body":"body","author_did":"did:plc:mallory"}`, session: true, origin: "http://localhost:8080", manager: &fakeIssues{}, wantStatus: http.StatusBadRequest},
		{name: "create derives reporter from session", method: http.MethodPost, path: "/api/v1/issues", body: `{"repository_uri":"` + restIssueRepositoryURI + `","title":"title","body":"body"}`, session: true, origin: "http://localhost:8080", manager: &fakeIssues{created: created}, wantStatus: http.StatusAccepted, wantCalls: 1, wantOperation: "create"},
		{name: "status derives owner from session", method: http.MethodPut, path: "/api/v1/issues", body: `{"issue_uri":"` + restIssueURI + `","state":"closed"}`, session: true, origin: "http://localhost:8080", manager: &fakeIssues{status: status}, wantStatus: http.StatusAccepted, wantCalls: 1, wantOperation: "status"},
		{name: "projected target not found", method: http.MethodPut, path: "/api/v1/issues", body: `{"issue_uri":"` + restIssueURI + `","state":"closed"}`, session: true, origin: "http://localhost:8080", manager: &fakeIssues{err: issue.ErrNotFound}, wantStatus: http.StatusNotFound, wantCalls: 1, wantOperation: "status", wantCode: "not_found"},
		{name: "validation is redacted", method: http.MethodPost, path: "/api/v1/issues", body: `{"repository_uri":"` + restIssueRepositoryURI + `","title":"title","body":"body"}`, session: true, origin: "http://localhost:8080", manager: &fakeIssues{err: &issue.ValidationError{Field: "title", Problem: "secret detail"}}, wantStatus: http.StatusUnprocessableEntity, wantCalls: 1, wantOperation: "create", wantCode: "validation_failed"},
		{name: "authorization maps to delegation conflict", method: http.MethodPut, path: "/api/v1/issues", body: `{"issue_uri":"` + restIssueURI + `","state":"closed"}`, session: true, origin: "http://localhost:8080", manager: &fakeIssues{err: &issue.AuthorizationError{Err: errors.New("secret credential")}}, wantStatus: http.StatusConflict, wantCalls: 1, wantOperation: "status", wantCode: "atproto_authorization_required"},
		{name: "record conflict is redacted", method: http.MethodPut, path: "/api/v1/issues", body: `{"issue_uri":"` + restIssueURI + `","state":"closed"}`, session: true, origin: "http://localhost:8080", manager: &fakeIssues{err: &issue.ConflictError{Err: errors.New("secret provider response")}}, wantStatus: http.StatusConflict, wantCalls: 1, wantOperation: "status", wantCode: "issue_conflict"},
		{name: "provider failure is redacted", method: http.MethodPost, path: "/api/v1/issues", body: `{"repository_uri":"` + restIssueRepositoryURI + `","title":"title","body":"body"}`, session: true, origin: "http://localhost:8080", manager: &fakeIssues{err: &issue.ProviderError{Operation: "create", Err: errors.New("secret provider")}}, wantStatus: http.StatusBadGateway, wantCalls: 1, wantOperation: "create", wantCode: "issue_provider_unavailable"},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			server := testAPIServer(t, Dependencies{Sessions: fakeSessions{}, TokenAuth: fakeTokenAuth{}, Issues: testCase.manager})
			request := httptest.NewRequest(testCase.method, testCase.path, strings.NewReader(testCase.body))
			if testCase.body != "" {
				request.Header.Set("Content-Type", "application/json")
			}
			if testCase.session {
				request.AddCookie(&http.Cookie{Name: "adenosine_session", Value: "valid-session"})
			}
			if testCase.pat {
				request.Header.Set("Authorization", "Bearer valid-pat")
			}
			if testCase.origin != "" {
				request.Header.Set("Origin", testCase.origin)
			}
			response := httptest.NewRecorder()
			server.Handler.ServeHTTP(response, request)
			if response.Code != testCase.wantStatus || testCase.manager.calls != testCase.wantCalls || testCase.manager.operation != testCase.wantOperation {
				t.Fatalf("status/calls/operation = %d/%d/%q, want %d/%d/%q; body=%s", response.Code, testCase.manager.calls, testCase.manager.operation, testCase.wantStatus, testCase.wantCalls, testCase.wantOperation, response.Body.String())
			}
			if testCase.wantCalls > 0 && testCase.wantOperation != "get" && testCase.manager.authorDID != "did:plc:alice" {
				t.Fatalf("mutation author DID = %q", testCase.manager.authorDID)
			}
			if testCase.wantOperation == "create" && testCase.wantStatus == http.StatusAccepted && (testCase.manager.createInput.RepositoryURI != restIssueRepositoryURI || testCase.manager.createInput.Title != "title" || testCase.manager.createInput.Body != "body") {
				t.Fatalf("create input = %#v", testCase.manager.createInput)
			}
			if testCase.wantOperation == "status" && testCase.wantStatus == http.StatusAccepted && (testCase.manager.statusInput.IssueURI != restIssueURI || testCase.manager.statusInput.State != issue.StateClosed) {
				t.Fatalf("status input = %#v", testCase.manager.statusInput)
			}
			if testCase.wantStatus == http.StatusOK {
				var body generated.IssueList
				if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil || body.IssueCount != 7 || body.OpenIssueCount != 4 || len(body.Data) != 1 || body.Data[0].Uri != restIssueURI || body.Data[0].Cid != restIssueCID || body.Data[0].AuthorDid != "did:plc:alice" || body.Data[0].RepositoryUri != restIssueRepositoryURI || body.Data[0].RepositoryCid != restIssueCID || body.Data[0].Title != "title" || body.Data[0].Body != "body" || body.Data[0].State != generated.IssueState(issue.StateOpen) || body.Data[0].StatusUri != nil || body.Data[0].StatusCid != nil || body.Data[0].CreatedAt != createdAt || body.Data[0].UpdatedAt != updatedAt || body.Data[0].IndexedAt != indexedAt {
					t.Fatalf("GET response = %#v, %v", body, err)
				}
				if !strings.Contains(response.Body.String(), `"status_uri":null`) || !strings.Contains(response.Body.String(), `"status_cid":null`) || !strings.Contains(response.Body.String(), `"comment_count":0`) {
					t.Fatalf("GET response omits nullable status identity: %s", response.Body.String())
				}
			}
			if testCase.wantStatus == http.StatusAccepted {
				if !strings.Contains(response.Body.String(), `"projected":false`) {
					t.Fatalf("mutation response is not asynchronous: %s", response.Body.String())
				}
			}
			if testCase.wantCode != "" {
				var body generated.ErrorResponse
				if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil || body.Error.Code != testCase.wantCode || strings.Contains(response.Body.String(), "secret") {
					t.Fatalf("error response = %#v, %v; raw=%s", body, err, response.Body.String())
				}
			}
		})
	}
}
