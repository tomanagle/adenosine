package restapi

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/adenosine-dev/adenosine/internal/moderation"
	"github.com/adenosine-dev/adenosine/internal/syncproxy"
)

type failingSyncProxy struct{ err error }

func (proxy failingSyncProxy) Forward(http.ResponseWriter, *http.Request, syncproxy.Shape, syncproxy.Policy) error {
	return proxy.err
}

type recordingSyncProxy struct {
	shape  syncproxy.Shape
	policy syncproxy.Policy
	calls  int
}

func (proxy *recordingSyncProxy) Forward(w http.ResponseWriter, _ *http.Request, shape syncproxy.Shape, policy syncproxy.Policy) error {
	proxy.shape = shape
	proxy.policy = policy
	proxy.calls++
	w.WriteHeader(http.StatusNoContent)
	return nil
}

func TestSyncProxyAppliesOptionalSessionModeration(t *testing.T) {
	t.Parallel()
	testCases := []struct {
		name, path string
		wantShape  syncproxy.Shape
	}{
		{name: "repositories", path: "/api/v1/sync/repositories?offset=-1", wantShape: syncproxy.Repositories},
		{name: "profiles", path: "/api/v1/sync/profiles?offset=-1", wantShape: syncproxy.Profiles},
		{name: "stars", path: "/api/v1/sync/stars?offset=-1", wantShape: syncproxy.Stars},
		{name: "issues", path: "/api/v1/sync/issues?offset=-1", wantShape: syncproxy.Issues},
		{name: "issue comments", path: "/api/v1/sync/issue-comments?offset=-1", wantShape: syncproxy.IssueComments},
		{name: "pull requests", path: "/api/v1/sync/pull-requests?offset=-1", wantShape: syncproxy.PullRequests},
		{name: "pull request reviews", path: "/api/v1/sync/pull-request-reviews?offset=-1", wantShape: syncproxy.PullRequestReviews},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			proxy := &recordingSyncProxy{}
			manager := &fakeModeration{blocks: []moderation.BlockedDID{{DID: "did:plc:bob"}}, hidden: []moderation.HiddenRecord{{URI: restCommentURI}}}
			server := testAPIServer(t, Dependencies{Sessions: fakeSessions{}, Moderation: manager, Sync: proxy})
			request := httptest.NewRequest(http.MethodGet, testCase.path, nil)
			request.AddCookie(&http.Cookie{Name: "adenosine_session", Value: "valid-session"})
			response := httptest.NewRecorder()
			server.Handler.ServeHTTP(response, request)
			if response.Code != http.StatusNoContent || proxy.calls != 1 || proxy.shape != testCase.wantShape || !proxy.policy.BrowserSession || len(proxy.policy.BlockedDIDs) != 1 || proxy.policy.BlockedDIDs[0] != "did:plc:bob" || len(proxy.policy.HiddenRecordURIs) != 1 || proxy.policy.HiddenRecordURIs[0] != restCommentURI || manager.calls != 2 || manager.accountDIDs[0] != "did:plc:alice" || manager.accountDIDs[1] != "did:plc:alice" {
				t.Fatalf("status/proxy/moderation = %d/%+v/%+v; body=%s", response.Code, proxy, manager, response.Body.String())
			}
		})
	}
}

func TestSyncProxyAuthenticationModes(t *testing.T) {
	t.Parallel()
	testCases := []struct {
		name, cookie, authorization string
		wantStatus                  int
		wantCalls                   int
	}{
		{name: "anonymous remains public", wantStatus: http.StatusNoContent, wantCalls: 1},
		{name: "PAT remains public", authorization: "Bearer valid-pat", wantStatus: http.StatusNoContent, wantCalls: 1},
		{name: "invalid presented session is rejected", cookie: "invalid-session", wantStatus: http.StatusUnauthorized},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			proxy := &recordingSyncProxy{}
			server := testAPIServer(t, Dependencies{Sessions: fakeSessions{}, TokenAuth: fakeTokenAuth{}, Moderation: &fakeModeration{}, Sync: proxy})
			request := httptest.NewRequest(http.MethodGet, "/api/v1/sync/issues?offset=-1", nil)
			if testCase.cookie != "" {
				request.AddCookie(&http.Cookie{Name: "adenosine_session", Value: testCase.cookie})
			}
			if testCase.authorization != "" {
				request.Header.Set("Authorization", testCase.authorization)
			}
			response := httptest.NewRecorder()
			server.Handler.ServeHTTP(response, request)
			if response.Code != testCase.wantStatus || proxy.calls != testCase.wantCalls || (proxy.calls == 1 && proxy.policy.BrowserSession) {
				t.Fatalf("status/calls/policy = %d/%d/%+v, want %d/%d/public; body=%s", response.Code, proxy.calls, proxy.policy, testCase.wantStatus, testCase.wantCalls, response.Body.String())
			}
		})
	}
}

func TestSyncProxyModerationFailureIsRedacted(t *testing.T) {
	t.Parallel()
	testCases := []struct{ name string }{{name: "moderation storage details are hidden"}}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			proxy := &recordingSyncProxy{}
			server := testAPIServer(t, Dependencies{Sessions: fakeSessions{}, Moderation: &fakeModeration{err: errors.New("database secret")}, Sync: proxy})
			request := httptest.NewRequest(http.MethodGet, "/api/v1/sync/issues?offset=-1", nil)
			request.AddCookie(&http.Cookie{Name: "adenosine_session", Value: "valid-session"})
			response := httptest.NewRecorder()
			server.Handler.ServeHTTP(response, request)
			assertAPIError(t, response, http.StatusBadGateway, "sync_unavailable")
			if proxy.calls != 0 || strings.Contains(response.Body.String(), "database") || strings.Contains(response.Body.String(), "secret") {
				t.Fatalf("unsafe moderation failure: calls=%d body=%s", proxy.calls, response.Body.String())
			}
		})
	}
}

func TestSyncProxyRoutes(t *testing.T) {
	t.Parallel()
	testCases := []struct {
		name, method, path string
		wantShape          syncproxy.Shape
	}{
		{name: "GET repositories", method: http.MethodGet, path: "/api/v1/sync/repositories?offset=-1", wantShape: syncproxy.Repositories},
		{name: "POST repositories", method: http.MethodPost, path: "/api/v1/sync/repositories?offset=-1", wantShape: syncproxy.Repositories},
		{name: "GET profiles", method: http.MethodGet, path: "/api/v1/sync/profiles?offset=-1", wantShape: syncproxy.Profiles},
		{name: "POST profiles", method: http.MethodPost, path: "/api/v1/sync/profiles?offset=-1", wantShape: syncproxy.Profiles},
		{name: "GET stars", method: http.MethodGet, path: "/api/v1/sync/stars?offset=-1", wantShape: syncproxy.Stars},
		{name: "POST stars", method: http.MethodPost, path: "/api/v1/sync/stars?offset=-1", wantShape: syncproxy.Stars},
		{name: "GET issues", method: http.MethodGet, path: "/api/v1/sync/issues?offset=-1", wantShape: syncproxy.Issues},
		{name: "POST issues", method: http.MethodPost, path: "/api/v1/sync/issues?offset=-1", wantShape: syncproxy.Issues},
		{name: "GET issue comments", method: http.MethodGet, path: "/api/v1/sync/issue-comments?offset=-1", wantShape: syncproxy.IssueComments},
		{name: "POST issue comments", method: http.MethodPost, path: "/api/v1/sync/issue-comments?offset=-1", wantShape: syncproxy.IssueComments},
		{name: "GET pull requests", method: http.MethodGet, path: "/api/v1/sync/pull-requests?offset=-1", wantShape: syncproxy.PullRequests},
		{name: "POST pull requests", method: http.MethodPost, path: "/api/v1/sync/pull-requests?offset=-1", wantShape: syncproxy.PullRequests},
		{name: "GET pull request reviews", method: http.MethodGet, path: "/api/v1/sync/pull-request-reviews?offset=-1", wantShape: syncproxy.PullRequestReviews},
		{name: "POST pull request reviews", method: http.MethodPost, path: "/api/v1/sync/pull-request-reviews?offset=-1", wantShape: syncproxy.PullRequestReviews},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			proxy := &recordingSyncProxy{}
			server := testAPIServer(t, Dependencies{Sync: proxy})
			request := httptest.NewRequest(testCase.method, testCase.path, nil)
			response := httptest.NewRecorder()
			server.Handler.ServeHTTP(response, request)
			if response.Code != http.StatusNoContent || proxy.calls != 1 || proxy.shape != testCase.wantShape {
				t.Fatalf("status/calls/shape = %d/%d/%q, want %d/1/%q; body=%s", response.Code, proxy.calls, proxy.shape, http.StatusNoContent, testCase.wantShape, response.Body.String())
			}
		})
	}
}

func TestSyncProxyAPIErrors(t *testing.T) {
	t.Parallel()
	testCases := []struct {
		name       string
		dependency SyncProxy
		wantStatus int
		wantCode   string
	}{
		{name: "missing dependency", wantStatus: http.StatusServiceUnavailable, wantCode: "sync_disabled"},
		{name: "disabled", dependency: failingSyncProxy{err: syncproxy.ErrDisabled}, wantStatus: http.StatusServiceUnavailable, wantCode: "sync_disabled"},
		{name: "malformed", dependency: failingSyncProxy{err: syncproxy.ErrMalformed}, wantStatus: http.StatusBadRequest, wantCode: "malformed_request"},
		{name: "too large", dependency: failingSyncProxy{err: syncproxy.ErrBodyTooLarge}, wantStatus: http.StatusRequestEntityTooLarge, wantCode: "request_body_too_large"},
		{name: "unavailable", dependency: failingSyncProxy{err: errors.New("upstream https://private.invalid?secret=secret")}, wantStatus: http.StatusBadGateway, wantCode: "sync_unavailable"},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			server := testAPIServer(t, Dependencies{Sync: testCase.dependency})
			request := httptest.NewRequest(http.MethodGet, "/api/v1/sync/repositories?offset=-1", nil)
			response := httptest.NewRecorder()
			server.Handler.ServeHTTP(response, request)
			assertAPIError(t, response, testCase.wantStatus, testCase.wantCode)
			if response.Header().Get("Vary") != "Cookie, Authorization" || strings.Contains(response.Body.String(), "private.invalid") || strings.Contains(response.Body.String(), "secret") {
				t.Fatalf("unsafe sync error response: headers=%v body=%s", response.Header(), response.Body.String())
			}
		})
	}
}
