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
	"github.com/adenosine-dev/adenosine/internal/star"
)

const restStarRepositoryURI = "at://did:plc:ewvi7nxzyoun6zhxrhs64oiz/dev.adenosine.repo/0198a8512a897ae2a370dc68883e3af1"

type fakeStars struct {
	projection    star.Projection
	result        star.Star
	err           error
	operation     string
	authorDID     string
	repositoryURI string
	calls         int
}

func (manager *fakeStars) Get(_ context.Context, uri string) (star.Projection, error) {
	manager.calls++
	manager.operation, manager.repositoryURI = "get", uri
	return manager.projection, manager.err
}

func (manager *fakeStars) Create(_ context.Context, did, uri string) (star.Star, error) {
	manager.calls++
	manager.operation, manager.authorDID, manager.repositoryURI = "create", did, uri
	return manager.result, manager.err
}

func (manager *fakeStars) Delete(_ context.Context, did, uri string) error {
	manager.calls++
	manager.operation, manager.authorDID, manager.repositoryURI = "delete", did, uri
	return manager.err
}

func TestStarEndpoints(t *testing.T) {
	t.Parallel()
	createdAt := time.Date(2026, 8, 9, 11, 0, 0, 0, time.UTC)
	indexedAt := createdAt.Add(time.Minute)
	target := star.Target{URI: restStarRepositoryURI, CID: "bafybeigdyrzt5sfp7udm7hu76uh7y26nf3efuylqabf3oclgtqy55fbzdi"}
	value := star.Star{URI: "at://did:plc:alice/dev.adenosine.star/key", CID: target.CID, AuthorDID: "did:plc:alice", Target: target, CreatedAt: createdAt, IndexedAt: indexedAt}
	testCases := []struct {
		name          string
		method        string
		path          string
		session       bool
		pat           bool
		origin        string
		manager       *fakeStars
		wantStatus    int
		wantCalls     int
		wantOperation string
		wantCode      string
	}{
		{name: "anonymous projected read", method: http.MethodGet, path: "/api/v1/stars?repository_uri=" + restStarRepositoryURI, manager: &fakeStars{projection: star.Projection{StarCount: 4, Stars: []star.Star{value}}}, wantStatus: http.StatusOK, wantCalls: 1, wantOperation: "get"},
		{name: "repository URI is required", method: http.MethodGet, path: "/api/v1/stars", manager: &fakeStars{}, wantStatus: http.StatusBadRequest},
		{name: "put requires authentication", method: http.MethodPut, path: "/api/v1/stars?repository_uri=" + restStarRepositoryURI, manager: &fakeStars{}, wantStatus: http.StatusUnauthorized, wantCode: "authentication_required"},
		{name: "put rejects PAT", method: http.MethodPut, path: "/api/v1/stars?repository_uri=" + restStarRepositoryURI, pat: true, origin: "http://localhost:8080", manager: &fakeStars{}, wantStatus: http.StatusForbidden, wantCode: "permission_denied"},
		{name: "put requires exact origin", method: http.MethodPut, path: "/api/v1/stars?repository_uri=" + restStarRepositoryURI, session: true, origin: "http://evil.example", manager: &fakeStars{}, wantStatus: http.StatusForbidden, wantCode: "permission_denied"},
		{name: "put publishes as session DID", method: http.MethodPut, path: "/api/v1/stars?repository_uri=" + restStarRepositoryURI, session: true, origin: "http://localhost:8080", manager: &fakeStars{result: value}, wantStatus: http.StatusAccepted, wantCalls: 1, wantOperation: "create"},
		{name: "delete accepted asynchronously", method: http.MethodDelete, path: "/api/v1/stars?repository_uri=" + restStarRepositoryURI, session: true, origin: "http://localhost:8080", manager: &fakeStars{}, wantStatus: http.StatusAccepted, wantCalls: 1, wantOperation: "delete"},
		{name: "unknown projected target", method: http.MethodPut, path: "/api/v1/stars?repository_uri=" + restStarRepositoryURI, session: true, origin: "http://localhost:8080", manager: &fakeStars{err: star.ErrNotFound}, wantStatus: http.StatusNotFound, wantCalls: 1, wantOperation: "create", wantCode: "not_found"},
		{name: "validation is stable", method: http.MethodGet, path: "/api/v1/stars?repository_uri=invalid", manager: &fakeStars{err: &star.ValidationError{Field: "subject.uri", Problem: "secret detail"}}, wantStatus: http.StatusUnprocessableEntity, wantCalls: 1, wantOperation: "get", wantCode: "validation_failed"},
		{name: "authorization conflict is stable", method: http.MethodPut, path: "/api/v1/stars?repository_uri=" + restStarRepositoryURI, session: true, origin: "http://localhost:8080", manager: &fakeStars{err: &star.AuthorizationError{Err: errors.New("secret credential")}}, wantStatus: http.StatusConflict, wantCalls: 1, wantOperation: "create", wantCode: "atproto_authorization_required"},
		{name: "record conflict is stable", method: http.MethodPut, path: "/api/v1/stars?repository_uri=" + restStarRepositoryURI, session: true, origin: "http://localhost:8080", manager: &fakeStars{err: &star.ConflictError{Err: errors.New("secret provider response")}}, wantStatus: http.StatusConflict, wantCalls: 1, wantOperation: "create", wantCode: "star_conflict"},
		{name: "provider failure is redacted", method: http.MethodDelete, path: "/api/v1/stars?repository_uri=" + restStarRepositoryURI, session: true, origin: "http://localhost:8080", manager: &fakeStars{err: &star.ProviderError{Operation: "delete", Err: errors.New("secret credential")}}, wantStatus: http.StatusBadGateway, wantCalls: 1, wantOperation: "delete", wantCode: "star_provider_unavailable"},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			server := testAPIServer(t, Dependencies{Sessions: fakeSessions{}, TokenAuth: fakeTokenAuth{}, Stars: testCase.manager})
			request := httptest.NewRequest(testCase.method, testCase.path, nil)
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
			if testCase.wantCalls > 0 && testCase.manager.repositoryURI != strings.TrimPrefix(testCase.path, "/api/v1/stars?repository_uri=") {
				t.Fatalf("repository URI = %q", testCase.manager.repositoryURI)
			}
			if testCase.wantStatus == http.StatusOK {
				var body generated.StarList
				if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil || body.StarCount != 4 || len(body.Data) != 1 || body.Data[0].IndexedAt != indexedAt {
					t.Fatalf("GET response = %#v, %v", body, err)
				}
			}
			if testCase.wantStatus == http.StatusAccepted && testCase.method == http.MethodPut {
				var body generated.StarMutation
				if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil || bool(body.Projected) || body.Star.RepositoryCid != target.CID {
					t.Fatalf("PUT response = %#v, %v", body, err)
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

func TestCanonicalOriginMatchesBrowserSerialization(t *testing.T) {
	t.Parallel()
	testCases := []struct {
		name    string
		baseURL string
		want    string
	}{
		{name: "lowercase hostname", baseURL: "https://FORGE.Example/path", want: "https://forge.example"},
		{name: "strip default HTTPS port", baseURL: "https://forge.example:443", want: "https://forge.example"},
		{name: "retain nondefault port", baseURL: "http://FORGE.Example:58080", want: "http://forge.example:58080"},
		{name: "canonical IPv6", baseURL: "http://[2001:DB8::1]:80", want: "http://[2001:db8::1]"},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			if got := canonicalOrigin(testCase.baseURL); got != testCase.want {
				t.Fatalf("canonicalOrigin(%q) = %q, want %q", testCase.baseURL, got, testCase.want)
			}
		})
	}
}
