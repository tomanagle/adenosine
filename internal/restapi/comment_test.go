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
	"github.com/adenosine-dev/adenosine/internal/comment"
	"github.com/adenosine-dev/adenosine/internal/issue"
)

const restCommentURI = "at://did:plc:alice/dev.adenosine.issueComment/0198a8512a897ae2a370dc68883e3af2"

type fakeComments struct {
	projection comment.Projection
	created    issue.Comment
	err        error
	operation  string
	viewerDID  string
	authorDID  string
	issueURI   string
	commentURI string
	input      comment.CreateInput
	calls      int
}

func (manager *fakeComments) Get(_ context.Context, issueURI, viewerDID string) (comment.Projection, error) {
	manager.calls++
	manager.operation, manager.issueURI, manager.viewerDID = "get", issueURI, viewerDID
	return manager.projection, manager.err
}

func (manager *fakeComments) Create(_ context.Context, did string, input comment.CreateInput) (issue.Comment, error) {
	manager.calls++
	manager.operation, manager.authorDID, manager.input = "create", did, input
	return manager.created, manager.err
}

func (manager *fakeComments) Delete(_ context.Context, did, commentURI string) error {
	manager.calls++
	manager.operation, manager.authorDID, manager.commentURI = "delete", did, commentURI
	return manager.err
}

func TestCommentEndpoints(t *testing.T) {
	t.Parallel()
	createdAt := time.Date(2026, 8, 9, 12, 0, 0, 0, time.UTC)
	updatedAt := createdAt.Add(time.Minute)
	indexedAt := updatedAt.Add(time.Minute)
	value := issue.Comment{URI: restCommentURI, CID: restIssueCID, AuthorDID: "did:plc:alice", CommentRecord: issue.CommentRecord{
		Subject: issue.StrongRef{URI: restIssueURI, CID: restIssueCID}, Body: "comment body", CreatedAt: createdAt, UpdatedAt: updatedAt,
	}}
	projection := comment.Projection{CommentCount: 3, Comments: []comment.ProjectedComment{{Comment: value, IndexedAt: indexedAt}}}
	testCases := []struct {
		name          string
		method        string
		path          string
		body          string
		cookie        string
		pat           bool
		origin        string
		manager       *fakeComments
		wantStatus    int
		wantCalls     int
		wantOperation string
		wantViewer    string
		wantCode      string
	}{
		{name: "anonymous read is public", method: http.MethodGet, path: "/api/v1/issues/comments?issue_uri=" + restIssueURI, manager: &fakeComments{projection: projection}, wantStatus: http.StatusOK, wantCalls: 1, wantOperation: "get"},
		{name: "session read passes viewer identity", method: http.MethodGet, path: "/api/v1/issues/comments?issue_uri=" + restIssueURI, cookie: "valid-session", manager: &fakeComments{projection: projection}, wantStatus: http.StatusOK, wantCalls: 1, wantOperation: "get", wantViewer: "did:plc:alice"},
		{name: "PAT read stays unpersonalized", method: http.MethodGet, path: "/api/v1/issues/comments?issue_uri=" + restIssueURI, pat: true, manager: &fakeComments{projection: projection}, wantStatus: http.StatusOK, wantCalls: 1, wantOperation: "get"},
		{name: "invalid present cookie is unauthorized", method: http.MethodGet, path: "/api/v1/issues/comments?issue_uri=" + restIssueURI, cookie: "invalid-session", pat: true, manager: &fakeComments{}, wantStatus: http.StatusUnauthorized, wantCode: "authentication_required"},
		{name: "issue URI is required", method: http.MethodGet, path: "/api/v1/issues/comments", manager: &fakeComments{}, wantStatus: http.StatusBadRequest},
		{name: "create rejects PAT", method: http.MethodPost, path: "/api/v1/issues/comments", body: `{"issue_uri":"` + restIssueURI + `","parent_uri":null,"body":"comment body"}`, pat: true, origin: "http://localhost:8080", manager: &fakeComments{}, wantStatus: http.StatusForbidden, wantCode: "permission_denied"},
		{name: "create requires exact Origin", method: http.MethodPost, path: "/api/v1/issues/comments", body: `{"issue_uri":"` + restIssueURI + `","body":"comment body"}`, cookie: "valid-session", origin: "http://localhost:8080/", manager: &fakeComments{}, wantStatus: http.StatusForbidden, wantCode: "permission_denied"},
		{name: "author DID cannot be injected", method: http.MethodPost, path: "/api/v1/issues/comments", body: `{"issue_uri":"` + restIssueURI + `","body":"comment body","author_did":"did:plc:mallory"}`, cookie: "valid-session", origin: "http://localhost:8080", manager: &fakeComments{}, wantStatus: http.StatusBadRequest},
		{name: "create derives author from session", method: http.MethodPost, path: "/api/v1/issues/comments", body: `{"issue_uri":"` + restIssueURI + `","parent_uri":null,"body":"comment body"}`, cookie: "valid-session", origin: "http://localhost:8080", manager: &fakeComments{created: value}, wantStatus: http.StatusAccepted, wantCalls: 1, wantOperation: "create"},
		{name: "delete derives author from session", method: http.MethodDelete, path: "/api/v1/issues/comments?comment_uri=" + restCommentURI, cookie: "valid-session", origin: "http://localhost:8080", manager: &fakeComments{}, wantStatus: http.StatusAccepted, wantCalls: 1, wantOperation: "delete"},
		{name: "delete rejects PAT", method: http.MethodDelete, path: "/api/v1/issues/comments?comment_uri=" + restCommentURI, pat: true, origin: "http://localhost:8080", manager: &fakeComments{}, wantStatus: http.StatusForbidden, wantCode: "permission_denied"},
		{name: "cross authority error is redacted", method: http.MethodDelete, path: "/api/v1/issues/comments?comment_uri=" + restCommentURI, cookie: "valid-session", origin: "http://localhost:8080", manager: &fakeComments{err: &issue.AuthorizationError{Err: errors.New("record belongs to did:plc:secret")}}, wantStatus: http.StatusConflict, wantCalls: 1, wantOperation: "delete", wantCode: "atproto_authorization_required"},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			server := testAPIServer(t, Dependencies{Sessions: fakeSessions{}, TokenAuth: fakeTokenAuth{}, Comments: testCase.manager})
			request := httptest.NewRequest(testCase.method, testCase.path, strings.NewReader(testCase.body))
			if testCase.body != "" {
				request.Header.Set("Content-Type", "application/json")
			}
			if testCase.cookie != "" {
				request.AddCookie(&http.Cookie{Name: "adenosine_session", Value: testCase.cookie})
			}
			if testCase.pat {
				request.Header.Set("Authorization", "Bearer valid-pat")
			}
			if testCase.origin != "" {
				request.Header.Set("Origin", testCase.origin)
			}
			response := httptest.NewRecorder()
			server.Handler.ServeHTTP(response, request)
			if response.Code != testCase.wantStatus || testCase.manager.calls != testCase.wantCalls || testCase.manager.operation != testCase.wantOperation || testCase.manager.viewerDID != testCase.wantViewer {
				t.Fatalf("status/calls/operation/viewer = %d/%d/%q/%q, want %d/%d/%q/%q; body=%s", response.Code, testCase.manager.calls, testCase.manager.operation, testCase.manager.viewerDID, testCase.wantStatus, testCase.wantCalls, testCase.wantOperation, testCase.wantViewer, response.Body.String())
			}
			if testCase.wantCalls > 0 && testCase.wantOperation != "get" && testCase.manager.authorDID != "did:plc:alice" {
				t.Fatalf("mutation author DID = %q", testCase.manager.authorDID)
			}
			if testCase.wantOperation == "create" && (testCase.manager.input.IssueURI != restIssueURI || testCase.manager.input.ParentURI != "" || testCase.manager.input.Body != "comment body") {
				t.Fatalf("create input = %#v", testCase.manager.input)
			}
			if testCase.wantOperation == "delete" && testCase.manager.commentURI != restCommentURI {
				t.Fatalf("deleted comment URI = %q", testCase.manager.commentURI)
			}
			if testCase.wantStatus == http.StatusOK {
				var body generated.CommentList
				if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil || body.CommentCount != 3 || len(body.Data) != 1 || body.Data[0].Uri != restCommentURI || body.Data[0].IssueUri != restIssueURI || body.Data[0].ParentUri != nil || body.Data[0].ParentCid != nil || body.Data[0].IndexedAt != indexedAt {
					t.Fatalf("GET response = %#v, %v", body, err)
				}
				if !strings.Contains(response.Body.String(), `"parent_uri":null`) || !strings.Contains(response.Body.String(), `"parent_cid":null`) {
					t.Fatalf("GET response omits nullable parent: %s", response.Body.String())
				}
			}
			if testCase.wantStatus == http.StatusAccepted && testCase.wantOperation == "create" && !strings.Contains(response.Body.String(), `"projected":false`) {
				t.Fatalf("create response is not asynchronous: %s", response.Body.String())
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
