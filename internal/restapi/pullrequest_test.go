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
	gitservice "github.com/adenosine-dev/adenosine/internal/git"
	"github.com/adenosine-dev/adenosine/internal/pullrequest"
)

const (
	restPullRequestURI = "at://did:plc:contributor/dev.adenosine.pullRequest/pr"
	restPRSourceURI    = "at://did:plc:source/dev.adenosine.repo/source"
	restPRTargetURI    = "at://did:plc:alice/dev.adenosine.repo/target"
)

type fakePullRequests struct {
	projection         pullrequest.Projection
	pullRequest        pullrequest.ProjectedPullRequest
	created            pullrequest.PullRequest
	diff               pullrequest.Result
	reviews            []pullrequest.ProjectedReview
	review             pullrequest.Review
	reviewRequests     pullrequest.ReviewRequestPage
	reviewRequest      pullrequest.ReviewRequest
	status             pullrequest.Status
	merge              pullrequest.MergeResult
	err                error
	operation, author  string
	createInput        pullrequest.CreateInput
	reviewInput        pullrequest.ReviewInput
	reviewRequestInput pullrequest.ReviewRequestInput
	statusInput        pullrequest.StatusInput
	mergeInput         pullrequest.MergeInput
	calls              int
}

func (fake *fakePullRequests) List(context.Context, string) (pullrequest.Projection, error) {
	fake.calls++
	fake.operation = "list"
	return fake.projection, fake.err
}
func (fake *fakePullRequests) Get(context.Context, string) (pullrequest.ProjectedPullRequest, error) {
	fake.calls++
	fake.operation = "get"
	return fake.pullRequest, fake.err
}
func (fake *fakePullRequests) Create(_ context.Context, did string, input pullrequest.CreateInput) (pullrequest.PullRequest, error) {
	fake.calls++
	fake.operation, fake.author, fake.createInput = "create", did, input
	return fake.created, fake.err
}
func (fake *fakePullRequests) Refresh(context.Context, string) (pullrequest.Result, error) {
	fake.calls++
	fake.operation = "diff"
	return fake.diff, fake.err
}
func (fake *fakePullRequests) Reviews(context.Context, string) ([]pullrequest.ProjectedReview, error) {
	fake.calls++
	fake.operation = "reviews"
	return fake.reviews, fake.err
}
func (fake *fakePullRequests) CreateReview(_ context.Context, did string, input pullrequest.ReviewInput) (pullrequest.Review, error) {
	fake.calls++
	fake.operation, fake.author, fake.reviewInput = "review", did, input
	return fake.review, fake.err
}
func (fake *fakePullRequests) ReviewRequests(context.Context, string, string, string, int) (pullrequest.ReviewRequestPage, error) {
	fake.calls++
	fake.operation = "review requests"
	return fake.reviewRequests, fake.err
}
func (fake *fakePullRequests) PutReviewRequest(_ context.Context, did string, input pullrequest.ReviewRequestInput) (pullrequest.ReviewRequest, error) {
	fake.calls++
	fake.operation, fake.author, fake.reviewRequestInput = "put review request", did, input
	return fake.reviewRequest, fake.err
}
func (fake *fakePullRequests) DeleteReviewRequest(_ context.Context, did string, input pullrequest.ReviewRequestInput) error {
	fake.calls++
	fake.operation, fake.author, fake.reviewRequestInput = "delete review request", did, input
	return fake.err
}
func (fake *fakePullRequests) PutStatus(_ context.Context, did string, input pullrequest.StatusInput) (pullrequest.Status, error) {
	fake.calls++
	fake.operation, fake.author, fake.statusInput = "status", did, input
	return fake.status, fake.err
}
func (fake *fakePullRequests) Merge(_ context.Context, did string, input pullrequest.MergeInput) (pullrequest.MergeResult, error) {
	fake.calls++
	fake.operation, fake.author, fake.mergeInput = "merge", did, input
	return fake.merge, fake.err
}

func TestPullRequestEndpoints(t *testing.T) {
	t.Parallel()
	createdAt := time.Date(2026, 8, 10, 10, 0, 0, 0, time.UTC)
	base := pullrequest.PullRequest{URI: restPullRequestURI, CID: restIssueCID, AuthorDID: "did:plc:contributor", Record: pullrequest.Record{
		SourceRepository: pullrequest.StrongRef{URI: restPRSourceURI, CID: restIssueCID}, TargetRepository: pullrequest.StrongRef{URI: restPRTargetURI, CID: restIssueCID},
		SourceBranch: "feature", TargetBranch: "main", HeadSHA: strings.Repeat("a", 40), Title: "title", Body: "body", CreatedAt: createdAt, UpdatedAt: createdAt,
	}}
	projected := pullrequest.ProjectedPullRequest{PullRequest: base, State: pullrequest.StateOpen, ReviewCount: 1, IndexedAt: createdAt.Add(time.Minute)}
	review := pullrequest.Review{URI: "at://did:plc:alice/dev.adenosine.pullRequestReview/review", CID: restIssueCID, AuthorDID: "did:plc:alice", ReviewRecord: pullrequest.ReviewRecord{Subject: pullrequest.StrongRef{URI: restPullRequestURI, CID: restIssueCID}, Verdict: pullrequest.VerdictApprove, Body: "ok", CreatedAt: createdAt, UpdatedAt: createdAt}}
	reviewRequestRKey, err := pullrequest.ReviewRequestRecordKey(restPullRequestURI, "did:plc:reviewer")
	if err != nil {
		t.Fatal(err)
	}
	reviewRequest := pullrequest.ReviewRequest{
		URI: "at://did:plc:alice/" + pullrequest.ReviewRequestCollection + "/" + reviewRequestRKey, CID: restIssueCID, AuthorDID: "did:plc:alice",
		ReviewRequestRecord: pullrequest.ReviewRequestRecord{
			Subject: pullrequest.StrongRef{URI: restPullRequestURI, CID: restIssueCID}, TargetRepository: base.TargetRepository,
			ReviewerDID: "did:plc:reviewer", RequestedByDID: "did:plc:alice", CreatedAt: createdAt, UpdatedAt: createdAt,
		},
	}
	status := pullrequest.Status{URI: "at://did:plc:alice/dev.adenosine.pullRequestStatus/status", CID: restIssueCID, AuthorDID: "did:plc:alice", StatusRecord: pullrequest.StatusRecord{Subject: pullrequest.StrongRef{URI: restPullRequestURI, CID: restIssueCID}, TargetRepository: base.TargetRepository, State: pullrequest.StateClosed, CreatedAt: createdAt, UpdatedAt: createdAt}}
	diff := pullrequest.Result{HeadRef: "refs/adenosine/pull/key/head", MergeBase: strings.Repeat("b", 40), Diff: gitservice.Diff{BaseSHA: strings.Repeat("b", 40), HeadSHA: strings.Repeat("a", 40), Patch: "patch", Files: []gitservice.DiffFile{}}}
	createBody := `{"source_repository_uri":"` + restPRSourceURI + `","target_repository_uri":"` + restPRTargetURI + `","source_branch":"feature","target_branch":"main","head_sha":"` + strings.Repeat("a", 40) + `","title":"title","body":"body"}`
	testCases := []struct {
		name, method, path, body, operation, wantCode string
		manager                                       *fakePullRequests
		session, pat                                  bool
		wantStatus, wantCalls                         int
	}{
		{name: "anonymous list", method: http.MethodGet, path: "/api/v1/pull-requests?repository_uri=" + restPRTargetURI, operation: "list", manager: &fakePullRequests{projection: pullrequest.Projection{PullRequestCount: 5, OpenPullRequestCount: 3, PullRequests: []pullrequest.ProjectedPullRequest{projected}}}, wantStatus: http.StatusOK, wantCalls: 1},
		{name: "anonymous detail", method: http.MethodGet, path: "/api/v1/pull-requests/detail?pull_request_uri=" + restPullRequestURI, operation: "get", manager: &fakePullRequests{pullRequest: projected}, wantStatus: http.StatusOK, wantCalls: 1},
		{name: "anonymous diff", method: http.MethodGet, path: "/api/v1/pull-requests/diff?pull_request_uri=" + restPullRequestURI, operation: "diff", manager: &fakePullRequests{diff: diff}, wantStatus: http.StatusOK, wantCalls: 1},
		{name: "anonymous reviews", method: http.MethodGet, path: "/api/v1/pull-requests/reviews?pull_request_uri=" + restPullRequestURI, operation: "reviews", manager: &fakePullRequests{reviews: []pullrequest.ProjectedReview{{Review: review, IndexedAt: createdAt.Add(time.Minute)}}}, wantStatus: http.StatusOK, wantCalls: 1},
		{name: "anonymous review requests", method: http.MethodGet, path: "/api/v1/pull-requests/review-requests?pull_request_uri=" + restPullRequestURI, operation: "review requests", manager: &fakePullRequests{reviewRequests: pullrequest.ReviewRequestPage{Items: []pullrequest.ProjectedReviewRequest{{ReviewRequest: reviewRequest, IndexedAt: createdAt.Add(time.Minute)}}}}, wantStatus: http.StatusOK, wantCalls: 1},
		{name: "create derives session author", method: http.MethodPost, path: "/api/v1/pull-requests", body: createBody, operation: "create", session: true, manager: &fakePullRequests{created: base}, wantStatus: http.StatusAccepted, wantCalls: 1},
		{name: "review derives session author", method: http.MethodPost, path: "/api/v1/pull-requests/reviews", body: `{"pull_request_uri":"` + restPullRequestURI + `","verdict":"approve","body":"ok"}`, operation: "review", session: true, manager: &fakePullRequests{review: review}, wantStatus: http.StatusAccepted, wantCalls: 1},
		{name: "review request derives session author and path reviewer", method: http.MethodPut, path: "/api/v1/pull-requests/review-requests/did:plc:reviewer", body: `{"pull_request_uri":"` + restPullRequestURI + `"}`, operation: "put review request", session: true, manager: &fakePullRequests{reviewRequest: reviewRequest}, wantStatus: http.StatusAccepted, wantCalls: 1},
		{name: "review request cancellation derives session author and path reviewer", method: http.MethodDelete, path: "/api/v1/pull-requests/review-requests/did:plc:reviewer?pull_request_uri=" + restPullRequestURI, operation: "delete review request", session: true, manager: &fakePullRequests{}, wantStatus: http.StatusAccepted, wantCalls: 1},
		{name: "status derives target owner", method: http.MethodPut, path: "/api/v1/pull-requests/status", body: `{"pull_request_uri":"` + restPullRequestURI + `","state":"closed"}`, operation: "status", session: true, manager: &fakePullRequests{status: status}, wantStatus: http.StatusAccepted, wantCalls: 1},
		{name: "merged generic status is rejected", method: http.MethodPut, path: "/api/v1/pull-requests/status", body: `{"pull_request_uri":"` + restPullRequestURI + `","state":"merged"}`, session: true, manager: &fakePullRequests{}, wantStatus: http.StatusUnprocessableEntity},
		{name: "mutation rejects PAT", method: http.MethodPost, path: "/api/v1/pull-requests", body: createBody, pat: true, manager: &fakePullRequests{}, wantStatus: http.StatusForbidden, wantCode: "permission_denied"},
		{name: "not found maps stably", method: http.MethodGet, path: "/api/v1/pull-requests/detail?pull_request_uri=" + restPullRequestURI, operation: "get", manager: &fakePullRequests{err: pullrequest.ErrNotFound}, wantStatus: http.StatusNotFound, wantCalls: 1, wantCode: "not_found"},
		{name: "provider error is redacted", method: http.MethodPost, path: "/api/v1/pull-requests", body: createBody, operation: "create", session: true, manager: &fakePullRequests{err: &pullrequest.ProviderError{Operation: "secret", Err: errors.New("provider-secret")}}, wantStatus: http.StatusBadGateway, wantCalls: 1, wantCode: "pull_request_provider_unavailable"},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			server := testAPIServer(t, Dependencies{Sessions: fakeSessions{}, TokenAuth: fakeTokenAuth{}, PullRequests: testCase.manager})
			request := httptest.NewRequest(testCase.method, testCase.path, strings.NewReader(testCase.body))
			if testCase.body != "" {
				request.Header.Set("Content-Type", "application/json")
			}
			if testCase.session {
				request.AddCookie(&http.Cookie{Name: "adenosine_session", Value: "valid-session"})
				request.Header.Set("Origin", "http://localhost:8080")
			}
			if testCase.pat {
				request.Header.Set("Authorization", "Bearer valid-pat")
				request.Header.Set("Origin", "http://localhost:8080")
			}
			response := httptest.NewRecorder()
			server.Handler.ServeHTTP(response, request)
			if response.Code != testCase.wantStatus || testCase.manager.calls != testCase.wantCalls || testCase.manager.operation != testCase.operation {
				t.Fatalf("status/calls/operation = %d/%d/%q, want %d/%d/%q body=%s", response.Code, testCase.manager.calls, testCase.manager.operation, testCase.wantStatus, testCase.wantCalls, testCase.operation, response.Body.String())
			}
			if testCase.method == http.MethodGet && testCase.wantStatus == http.StatusOK && response.Header().Get("Vary") != "Cookie" {
				t.Fatalf("Vary = %q, want Cookie", response.Header().Get("Vary"))
			}
			if testCase.wantStatus == http.StatusAccepted && testCase.manager.author != "did:plc:alice" {
				t.Fatalf("mutation response/author = %s/%q", response.Body.String(), testCase.manager.author)
			}
			if testCase.wantStatus == http.StatusAccepted && response.Body.Len() > 0 && !strings.Contains(response.Body.String(), `"projected":false`) {
				t.Fatalf("mutation response = %s", response.Body.String())
			}
			if testCase.name == "anonymous list" {
				var body generated.PullRequestList
				if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil || body.PullRequestCount != 5 || body.OpenPullRequestCount != 3 || len(body.Items) != 1 || body.Items[0].ReviewCount != 1 {
					t.Fatalf("list response = %#v, %v", body, err)
				}
			}
			if testCase.name == "anonymous diff" {
				var body generated.PullRequestDiff
				if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil || body.MergeBase != diff.MergeBase || body.HeadRef != diff.HeadRef || body.Diff.Patch != "patch" {
					t.Fatalf("diff response = %#v, %v", body, err)
				}
			}
			if testCase.name == "anonymous review requests" {
				var body generated.PullRequestReviewRequestList
				if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil || len(body.Items) != 1 || body.Items[0].ReviewerDid != "did:plc:reviewer" || body.Items[0].RequestedByDid != "did:plc:alice" {
					t.Fatalf("review request response = %#v, %v", body, err)
				}
			}
			if strings.Contains(testCase.name, "review request derives") || strings.Contains(testCase.name, "review request cancellation") {
				if testCase.manager.reviewRequestInput.PullRequestURI != restPullRequestURI || testCase.manager.reviewRequestInput.ReviewerDID != "did:plc:reviewer" {
					t.Fatalf("review request input = %#v", testCase.manager.reviewRequestInput)
				}
			}
			if testCase.wantCode != "" {
				var body generated.ErrorResponse
				_ = json.Unmarshal(response.Body.Bytes(), &body)
				if body.Error.Code != testCase.wantCode || strings.Contains(response.Body.String(), "secret") {
					t.Fatalf("error response = %s", response.Body.String())
				}
			}
		})
	}
}

func TestMergePullRequestEndpoint(t *testing.T) {
	t.Parallel()
	createdAt := time.Date(2026, 8, 10, 10, 0, 0, 0, time.UTC)
	oldSHA, headSHA, mergeSHA := strings.Repeat("a", 40), strings.Repeat("b", 40), strings.Repeat("c", 40)
	status := pullrequest.Status{URI: "at://did:plc:alice/dev.adenosine.pullRequestStatus/status", CID: restIssueCID, AuthorDID: "did:plc:alice", StatusRecord: pullrequest.StatusRecord{
		Subject: pullrequest.StrongRef{URI: restPullRequestURI, CID: restIssueCID}, TargetRepository: pullrequest.StrongRef{URI: restPRTargetURI, CID: restIssueCID},
		State: pullrequest.StateMerged, MergeCommitSHA: mergeSHA, CreatedAt: createdAt, UpdatedAt: createdAt,
	}}
	result := pullrequest.MergeResult{Git: gitservice.MergeResult{OldSHA: oldSHA, NewSHA: mergeSHA, HeadSHA: headSHA, TargetRef: "refs/heads/main", Strategy: gitservice.MergeCommit}, Status: status}
	testCases := []struct {
		name, body, origin, authorization, wantCode string
		session                                     bool
		err                                         error
		wantStatus, wantCalls                       int
	}{
		{name: "session merge", body: `{"pull_request_uri":"` + restPullRequestURI + `","strategy":"merge-commit"}`, session: true, origin: "http://localhost:8080", wantStatus: http.StatusOK, wantCalls: 1},
		{name: "anonymous denied", body: `{}`, wantStatus: http.StatusUnauthorized, wantCode: "authentication_required"},
		{name: "PAT denied", body: `{}`, authorization: "Bearer valid-pat", origin: "http://localhost:8080", wantStatus: http.StatusForbidden, wantCode: "permission_denied"},
		{name: "missing origin denied", body: `{}`, session: true, wantStatus: http.StatusForbidden, wantCode: "permission_denied"},
		{name: "wrong origin denied", body: `{}`, session: true, origin: "http://evil.example", wantStatus: http.StatusForbidden, wantCode: "permission_denied"},
		{name: "invalid strategy", body: `{"pull_request_uri":"` + restPullRequestURI + `","strategy":"rebase"}`, session: true, origin: "http://localhost:8080", wantStatus: http.StatusUnprocessableEntity, wantCode: "validation_failed"},
		{name: "malformed", body: `{"pull_request_uri":`, session: true, origin: "http://localhost:8080", wantStatus: http.StatusBadRequest, wantCode: "malformed_request"},
		{name: "permission", body: `{"pull_request_uri":"` + restPullRequestURI + `","strategy":"merge-commit"}`, session: true, origin: "http://localhost:8080", err: pullrequest.ErrPermissionDenied, wantStatus: http.StatusForbidden, wantCode: "permission_denied", wantCalls: 1},
		{name: "not found", body: `{"pull_request_uri":"` + restPullRequestURI + `","strategy":"merge-commit"}`, session: true, origin: "http://localhost:8080", err: pullrequest.ErrNotFound, wantStatus: http.StatusNotFound, wantCode: "not_found", wantCalls: 1},
		{name: "conflict redacted", body: `{"pull_request_uri":"` + restPullRequestURI + `","strategy":"merge-commit"}`, session: true, origin: "http://localhost:8080", err: &pullrequest.ConflictError{Err: errors.New("git secret")}, wantStatus: http.StatusConflict, wantCode: "pull_request_conflict", wantCalls: 1},
		{name: "provider redacted", body: `{"pull_request_uri":"` + restPullRequestURI + `","strategy":"merge-commit"}`, session: true, origin: "http://localhost:8080", err: &pullrequest.ProviderError{Operation: "publish", Err: errors.New("provider secret")}, wantStatus: http.StatusBadGateway, wantCode: "pull_request_provider_unavailable", wantCalls: 1},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			manager := &fakePullRequests{merge: result, err: testCase.err}
			server := testAPIServer(t, Dependencies{Sessions: fakeSessions{}, TokenAuth: fakeTokenAuth{}, PullRequests: manager})
			request := httptest.NewRequest(http.MethodPost, "/api/v1/pull-requests/merge", strings.NewReader(testCase.body))
			request.Header.Set("Content-Type", "application/json")
			request.Header.Set("Origin", testCase.origin)
			if testCase.session {
				request.AddCookie(&http.Cookie{Name: "adenosine_session", Value: "valid-session"})
			}
			if testCase.authorization != "" {
				request.Header.Set("Authorization", testCase.authorization)
			}
			response := httptest.NewRecorder()
			server.Handler.ServeHTTP(response, request)
			if response.Code != testCase.wantStatus || manager.calls != testCase.wantCalls {
				t.Fatalf("status/calls = %d/%d, want %d/%d: %s", response.Code, manager.calls, testCase.wantStatus, testCase.wantCalls, response.Body.String())
			}
			if testCase.wantStatus == http.StatusOK {
				var body generated.PullRequestMerge
				if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil || body.MergeCommitSha != mergeSHA || body.OldSha != oldSHA || body.HeadSha != headSHA || body.TargetRef != "refs/heads/main" || body.Status.AuthorDid != "did:plc:alice" || body.Status.MergeCommitSha == nil || *body.Status.MergeCommitSha != mergeSHA {
					t.Fatalf("merge response = %#v, %v", body, err)
				}
				if manager.author != "did:plc:alice" || manager.mergeInput.Strategy != gitservice.MergeCommit {
					t.Fatalf("merge actor/input = %q/%#v", manager.author, manager.mergeInput)
				}
			}
			if testCase.wantCode != "" {
				var body generated.ErrorResponse
				_ = json.Unmarshal(response.Body.Bytes(), &body)
				if body.Error.Code != testCase.wantCode || strings.Contains(response.Body.String(), "secret") {
					t.Fatalf("error response = %s", response.Body.String())
				}
			}
		})
	}
}
