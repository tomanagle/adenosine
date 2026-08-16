package restapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	generated "github.com/adenosine-dev/adenosine/api/generated/go"
	localatproto "github.com/adenosine-dev/adenosine/internal/atproto"
	"github.com/adenosine-dev/adenosine/internal/auth"
	gitservice "github.com/adenosine-dev/adenosine/internal/git"
	localidentity "github.com/adenosine-dev/adenosine/internal/identity"
	"github.com/adenosine-dev/adenosine/internal/profile"
	"github.com/adenosine-dev/adenosine/internal/repository"
	"github.com/google/uuid"
)

type fakeReadiness struct {
	err error
}

type recordedRequest struct {
	method   string
	route    string
	status   int
	duration time.Duration
}

type fakeRequestMetrics struct {
	requests []recordedRequest
}

func (metrics *fakeRequestMetrics) RecordHTTPRequest(_ context.Context, method, route string, status int, duration time.Duration) {
	metrics.requests = append(metrics.requests, recordedRequest{method: method, route: route, status: status, duration: duration})
}

type fakeSessions struct{}

func (fakeSessions) Authenticate(_ context.Context, plaintext string) (auth.SessionIdentity, error) {
	if plaintext != "valid-session" {
		return auth.SessionIdentity{}, auth.ErrUnauthorized
	}
	return auth.SessionIdentity{SessionID: uuid.New(), AccountDID: "did:plc:alice"}, nil
}

type fakeLogin struct {
	identifier string
	startErr   error
	result     localidentity.LoginResult
}

func (login *fakeLogin) Start(_ context.Context, identifier, _ string) (string, error) {
	login.identifier = identifier
	if login.startErr != nil {
		return "", login.startErr
	}
	return "https://provider.example/authorize", nil
}

func (login *fakeLogin) Complete(context.Context, url.Values) (localidentity.LoginResult, error) {
	return login.result, nil
}

type fakeLocalSessions struct {
	accountDID string
	sessionID  uuid.UUID
}

func (sessions *fakeLocalSessions) RevokeSession(_ context.Context, accountDID string, sessionID uuid.UUID) error {
	sessions.accountDID = accountDID
	sessions.sessionID = sessionID
	return nil
}

type fakeAccounts struct{ account auth.Account }

func (accounts fakeAccounts) GetAccount(context.Context, string) (auth.Account, error) {
	return accounts.account, nil
}

type fakeOAuthMetadata struct{}

func (fakeOAuthMetadata) ClientMetadata() localatproto.ClientMetadata {
	applicationType := "web"
	return localatproto.ClientMetadata{
		ClientID: "http://localhost:8080", ApplicationType: &applicationType,
		GrantTypes: []string{"authorization_code", "refresh_token"}, Scope: "atproto",
		ResponseTypes: []string{"code"}, RedirectURIs: []string{"http://localhost:8080/oauth/atproto/callback"},
		DPoPBoundAccessTokens: true, TokenEndpointAuthMethod: "none",
	}
}

type fakeProfiles struct {
	value       profile.Profile
	getDID      string
	updateDID   string
	updateInput profile.UpdateInput
	getErr      error
	updateErr   error
}

func (profiles *fakeProfiles) Get(_ context.Context, did string) (profile.Profile, error) {
	profiles.getDID = did
	return profiles.value, profiles.getErr
}

func (profiles *fakeProfiles) Update(_ context.Context, did string, input profile.UpdateInput) (profile.Profile, error) {
	profiles.updateDID = did
	profiles.updateInput = input
	return profiles.value, profiles.updateErr
}

type fakeTokenAuth struct{}

func (fakeTokenAuth) Authenticate(_ context.Context, plaintext string) (auth.AccessToken, error) {
	if plaintext != "valid-pat" {
		return auth.AccessToken{}, auth.ErrUnauthorized
	}
	return auth.AccessToken{AccountDID: "did:plc:alice", Scopes: []string{auth.ScopeRepositoryWrite}}, nil
}

type configuredTokenAuth struct{ token auth.AccessToken }

func (configured configuredTokenAuth) Authenticate(_ context.Context, plaintext string) (auth.AccessToken, error) {
	if plaintext != "valid-pat" {
		return auth.AccessToken{}, auth.ErrUnauthorized
	}
	return configured.token, nil
}

type fakeTokens struct {
	createdFor string
	revokedFor string
	tokens     []auth.AccessToken
}

func (manager *fakeTokens) CreateToken(_ context.Context, input auth.CreateTokenInput) (auth.AccessToken, string, error) {
	manager.createdFor = input.AccountDID
	return auth.AccessToken{
		ID: uuid.MustParse("0198a851-2a89-7ae2-a370-dc68883e3af3"), AccountDID: input.AccountDID,
		Name: input.Name, Prefix: "adn_pat_example", Scopes: input.Scopes, CreatedAt: time.Date(2026, 8, 9, 0, 0, 0, 0, time.UTC),
	}, "adn_pat_one_time_plaintext", nil
}

func (manager *fakeTokens) ListTokens(context.Context, string) ([]auth.AccessToken, error) {
	return manager.tokens, nil
}

func (manager *fakeTokens) RevokeToken(_ context.Context, accountDID string, _ uuid.UUID) error {
	manager.revokedFor = accountDID
	return nil
}

type fakeSSHKeys struct {
	createdFor string
	revokedFor string
	createErr  error
}

func (manager *fakeSSHKeys) CreateSSHKey(_ context.Context, input auth.CreateSSHKeyInput) (auth.SSHKey, error) {
	manager.createdFor = input.AccountDID
	if manager.createErr != nil {
		return auth.SSHKey{}, manager.createErr
	}
	return auth.SSHKey{ID: uuid.MustParse("0198a851-2a89-7ae2-a370-dc68883e3af4"), AccountDID: input.AccountDID,
		Name: input.Name, Algorithm: "ssh-ed25519", PublicKey: input.AuthorizedKey,
		Fingerprint: "SHA256:example", CreatedAt: time.Date(2026, 8, 9, 0, 0, 0, 0, time.UTC)}, nil
}

func (*fakeSSHKeys) ListSSHKeys(context.Context, string) ([]auth.SSHKey, error) { return nil, nil }

func (manager *fakeSSHKeys) RevokeSSHKey(_ context.Context, accountDID string, _ uuid.UUID) error {
	manager.revokedFor = accountDID
	return nil
}

type fakeRepositories struct{}

func (fakeRepositories) Create(context.Context, repository.CreateInput) (repository.Repository, error) {
	return repository.Repository{}, nil
}

func (fakeRepositories) GetByOwnerSlug(context.Context, string, string) (repository.Repository, error) {
	return repository.Repository{}, repository.ErrNotFound
}

func (fakeRepositories) ListByOrganization(context.Context, uuid.UUID) ([]repository.Repository, error) {
	return nil, nil
}

type fixedRepositoryManager struct{ repository repository.Repository }

func (manager fixedRepositoryManager) Create(context.Context, repository.CreateInput) (repository.Repository, error) {
	return manager.repository, nil
}

func (manager fixedRepositoryManager) GetByOwnerSlug(_ context.Context, owner, slug string) (repository.Repository, error) {
	if owner != "alice" || slug != manager.repository.Slug {
		return repository.Repository{}, repository.ErrNotFound
	}
	return manager.repository, nil
}

func (manager fixedRepositoryManager) ListByOrganization(context.Context, uuid.UUID) ([]repository.Repository, error) {
	return nil, nil
}

type fakeAuthorization struct{}

func (fakeAuthorization) CanReadRepository(context.Context, string, repository.ID) (bool, error) {
	return true, nil
}

func (fakeAuthorization) CanWriteRepository(context.Context, string, repository.ID) (bool, error) {
	return true, nil
}

func (fakeAuthorization) CanAdminRepository(context.Context, string, repository.ID) (bool, error) {
	return true, nil
}

type fakeGitReader struct {
	blob []byte
}

func (reader fakeGitReader) Branches(context.Context, repository.ID, string) ([]gitservice.Branch, error) {
	return []gitservice.Branch{{Name: "main", SHA: strings.Repeat("a", 40), Default: true}}, nil
}

func (reader fakeGitReader) Tags(context.Context, repository.ID) ([]gitservice.Tag, error) {
	return []gitservice.Tag{{Name: "v1.0.0", SHA: strings.Repeat("b", 40), ObjectType: "commit", PeeledSHA: strings.Repeat("b", 40), PeeledType: "commit"}}, nil
}

func (reader fakeGitReader) Tree(_ context.Context, _ repository.ID, revision, treePath string) (gitservice.Tree, error) {
	if revision == "bad" {
		return gitservice.Tree{}, gitservice.ErrInvalidInput
	}
	return gitservice.Tree{CommitSHA: strings.Repeat("a", 40), Path: treePath, Entries: []gitservice.TreeEntry{
		{Name: "docs", Path: "docs", Mode: "040000", Type: "tree", SHA: strings.Repeat("c", 40), Size: -1},
		{Name: "README.md", Path: "README.md", Mode: "100644", Type: "blob", SHA: strings.Repeat("d", 40), Size: 7},
	}}, nil
}

func (reader fakeGitReader) BlobMetadata(context.Context, repository.ID, string) (gitservice.BlobMetadata, error) {
	return gitservice.BlobMetadata{SHA: strings.Repeat("d", 40), Type: "blob", Size: int64(len(reader.blob))}, nil
}

func (reader fakeGitReader) StreamBlob(_ context.Context, _ repository.ID, _ string, output io.Writer) error {
	_, err := output.Write(reader.blob)
	return err
}

func (fakeGitReader) Commits(context.Context, repository.ID, string, int) ([]gitservice.CommitSummary, error) {
	identity := gitservice.CommitIdentity{Name: "Alice", Email: "alice@example.com", Time: time.Date(2026, 8, 9, 1, 2, 3, 0, time.UTC)}
	return []gitservice.CommitSummary{{SHA: strings.Repeat("e", 40), Parents: []string{strings.Repeat("a", 40)}, Author: identity, Committer: identity, Summary: "Update README"}}, nil
}

func (fakeGitReader) Commit(context.Context, repository.ID, string) (gitservice.Commit, error) {
	identity := gitservice.CommitIdentity{Name: "Alice", Email: "alice@example.com", Time: time.Date(2026, 8, 9, 1, 2, 3, 0, time.UTC)}
	return gitservice.Commit{SHA: strings.Repeat("e", 40), Parents: []string{strings.Repeat("a", 40)}, Author: identity, Committer: identity, Summary: "Update README", Message: "Update README\n\nDetails.\n"}, nil
}

func (fakeGitReader) Diff(context.Context, repository.ID, string, string) (gitservice.Diff, error) {
	additions, deletions := 2, 1
	return gitservice.Diff{BaseSHA: strings.Repeat("a", 40), HeadSHA: strings.Repeat("e", 40), Patch: "diff --git a/README.md b/README.md\n", Files: []gitservice.DiffFile{
		{Status: "M", OldPath: "README.md", NewPath: "README.md", Additions: &additions, Deletions: &deletions},
	}}, nil
}

func (fakeGitReader) MergeBase(context.Context, repository.ID, string, string) (string, error) {
	return strings.Repeat("a", 40), nil
}

func TestAPIDocumentation(t *testing.T) {
	t.Parallel()
	server, err := NewServer(":0", "http://localhost:8080", fakeReadiness{}, slog.New(slog.NewTextHandler(io.Discard, nil)), Observability{}, Dependencies{}, nil)
	if err != nil {
		t.Fatalf("create server: %v", err)
	}

	testCases := []struct {
		name           string
		path           string
		contentType    string
		verifyDocument bool
	}{
		{name: "API documentation", path: "/docs/api", contentType: "text/html; charset=utf-8"},
		{name: "OpenAPI JSON", path: "/openapi.json", contentType: "application/json"},
		{name: "OpenAPI YAML", path: "/openapi.yaml", contentType: "application/yaml"},
		{name: "OpenAPI JSON document", path: "/openapi.json", verifyDocument: true},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodGet, testCase.path, nil)
			response := httptest.NewRecorder()
			server.Handler.ServeHTTP(response, request)
			if testCase.verifyDocument {
				var document map[string]any
				if err := json.Unmarshal(response.Body.Bytes(), &document); err != nil {
					t.Fatalf("OpenAPI response is not JSON: %v", err)
				}
				if document["openapi"] != "3.0.3" {
					t.Fatalf("OpenAPI version = %v, want 3.0.3", document["openapi"])
				}
				return
			}
			if response.Code != http.StatusOK {
				t.Fatalf("status = %d, want %d", response.Code, http.StatusOK)
			}
			if got := response.Header().Get("Content-Type"); got != testCase.contentType {
				t.Fatalf("content type = %q, want %q", got, testCase.contentType)
			}
		})
	}
}

func TestPrometheusEndpointUsesInternalServerMiddleware(t *testing.T) {
	testCases := []struct{ name string }{{name: "serves scrape through shared middleware"}}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			metrics := &fakeRequestMetrics{}
			prometheus := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				_, _ = w.Write([]byte("# EOF\n"))
			})
			server, err := NewServer(":0", "http://localhost:8080", fakeReadiness{}, slog.New(slog.NewTextHandler(io.Discard, nil)), Observability{Requests: metrics, Prometheus: prometheus}, Dependencies{}, nil)
			if err != nil {
				t.Fatalf("create server: %v", err)
			}
			request := httptest.NewRequest(http.MethodGet, "/metrics", nil)
			response := httptest.NewRecorder()
			server.Handler.ServeHTTP(response, request)
			if response.Code != http.StatusOK || response.Body.String() != "# EOF\n" {
				t.Fatalf("scrape response = %d %q", response.Code, response.Body.String())
			}
			if len(metrics.requests) != 1 || metrics.requests[0].route != "GET /metrics" {
				t.Fatalf("recorded requests = %#v", metrics.requests)
			}
		})
	}
}

func (f fakeReadiness) Ping(context.Context) error { return f.err }

func TestHealthEndpoints(t *testing.T) {
	t.Parallel()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	testCases := []struct {
		name      string
		path      string
		readiness fakeReadiness
		want      int
	}{
		{name: "live", path: "/health/live", want: http.StatusOK},
		{name: "ready", path: "/health/ready", want: http.StatusOK},
		{name: "database unavailable", path: "/health/ready", readiness: fakeReadiness{err: errors.New("unavailable")}, want: http.StatusServiceUnavailable},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			server, err := NewServer(":0", "http://localhost:8080", testCase.readiness, logger, Observability{}, Dependencies{}, nil)
			if err != nil {
				t.Fatalf("create server: %v", err)
			}
			request := httptest.NewRequest(http.MethodGet, testCase.path, nil)
			response := httptest.NewRecorder()
			server.Handler.ServeHTTP(response, request)
			if response.Code != testCase.want {
				t.Fatalf("status = %d, want %d", response.Code, testCase.want)
			}
			if response.Header().Get("X-Request-ID") == "" {
				t.Fatal("missing request ID")
			}
		})
	}
}

func TestRepositoryWritePrincipal(t *testing.T) {
	repositoryID := repository.ID(uuid.MustParse("0198a851-2a89-7ae2-a370-dc68883e3af3"))
	testCases := []struct {
		name    string
		token   auth.AccessToken
		wantErr error
	}{
		{name: "account wide write token", token: auth.AccessToken{AccountDID: "did:plc:alice", Scopes: []string{auth.ScopeRepositoryWrite}}},
		{name: "read token", token: auth.AccessToken{AccountDID: "did:plc:alice", Scopes: []string{auth.ScopeRepositoryRead}}, wantErr: auth.ErrForbidden},
		{name: "repository scoped token", token: auth.AccessToken{AccountDID: "did:plc:alice", Scopes: []string{auth.ScopeRepositoryWrite}, RepositoryID: &repositoryID}, wantErr: auth.ErrForbidden},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			handler := newAPIHandler("http://localhost:8080", fakeReadiness{}, slog.New(slog.NewTextHandler(io.Discard, nil)), Dependencies{TokenAuth: configuredTokenAuth{token: testCase.token}})
			request := httptest.NewRequest(http.MethodPost, "/api/v1/issues", nil)
			request.Header.Set("Authorization", "Bearer valid-pat")
			identity, err := handler.requireRepositoryWrite(request)
			if !errors.Is(err, testCase.wantErr) {
				t.Fatalf("requireRepositoryWrite() error = %v, want %v", err, testCase.wantErr)
			}
			if testCase.wantErr == nil && identity.accountDID != testCase.token.AccountDID {
				t.Errorf("account DID = %q, want %q", identity.accountDID, testCase.token.AccountDID)
			}
		})
	}
}

func TestRequestObservabilityUsesMatchedRoute(t *testing.T) {
	testCases := []struct {
		name        string
		path        string
		deps        Dependencies
		wantStatus  int
		wantRoute   string
		identifiers []string
	}{
		{
			name: "parameterized API route",
			path: "/api/v1/profiles/did:plc:concreteidentifier",
			deps: Dependencies{Profiles: &fakeProfiles{value: profile.Profile{
				DID: "did:plc:concreteidentifier", Handle: "alice.example", IndexedAt: time.Now(),
			}}},
			wantStatus:  http.StatusOK,
			wantRoute:   "GET /api/v1/profiles/{did}",
			identifiers: []string{"did:plc:concreteidentifier"},
		},
		{
			name:        "unmatched route",
			path:        "/concrete-unmatched-identifier?token=concrete-query-identifier",
			wantStatus:  http.StatusNotFound,
			wantRoute:   "unmatched",
			identifiers: []string{"concrete-unmatched-identifier", "concrete-query-identifier"},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			var logs bytes.Buffer
			logger := slog.New(slog.NewJSONHandler(&logs, nil))
			metrics := &fakeRequestMetrics{}
			server, err := NewServer(":0", "http://localhost:8080", fakeReadiness{}, logger, Observability{Requests: metrics}, testCase.deps, nil)
			if err != nil {
				t.Fatalf("create server: %v", err)
			}
			httpServer := httptest.NewServer(server.Handler)
			t.Cleanup(httpServer.Close)

			response, err := http.Get(httpServer.URL + testCase.path)
			if err != nil {
				t.Fatalf("request server: %v", err)
			}
			defer response.Body.Close()
			if response.StatusCode != testCase.wantStatus {
				t.Fatalf("status = %d, want %d", response.StatusCode, testCase.wantStatus)
			}
			requestID := response.Header.Get("X-Request-ID")
			if requestID == "" {
				t.Fatal("missing request ID")
			}

			if len(metrics.requests) != 1 {
				t.Fatalf("recorded requests = %d, want 1", len(metrics.requests))
			}
			recorded := metrics.requests[0]
			if recorded.method != http.MethodGet || recorded.route != testCase.wantRoute || recorded.status != testCase.wantStatus || recorded.duration <= 0 {
				t.Errorf("recorded request = %#v", recorded)
			}
			for _, identifier := range testCase.identifiers {
				if strings.Contains(recorded.route, identifier) {
					t.Errorf("metric route contains identifier %q", identifier)
				}
			}

			var event map[string]any
			if err := json.Unmarshal(logs.Bytes(), &event); err != nil {
				t.Fatalf("decode request log: %v", err)
			}
			if event["route"] != testCase.wantRoute || event["request_id"] != requestID {
				t.Errorf("request log route/request ID = %q, %q; want %q, %q", event["route"], event["request_id"], testCase.wantRoute, requestID)
			}
			for _, identifier := range testCase.identifiers {
				if strings.Contains(logs.String(), identifier) {
					t.Errorf("request log contains identifier %q", identifier)
				}
			}
		})
	}
}

func TestCredentialEndpointsRequireSessionAndOrigin(t *testing.T) {
	t.Parallel()
	tokens := &fakeTokens{}
	keys := &fakeSSHKeys{}
	server := testAPIServer(t, Dependencies{
		Sessions: fakeSessions{}, TokenAuth: fakeTokenAuth{}, Tokens: tokens, SSHKeys: keys,
		Repositories: fakeRepositories{}, Authorization: fakeAuthorization{},
	})

	testCases := []struct {
		name string
		run  func(*testing.T)
	}{
		{name: "create token returns plaintext once", run: func(t *testing.T) {
			response := performAPIRequest(server, http.MethodPost, "/api/v1/tokens", `{"name":"laptop","scopes":["repository:write"]}`, true, true, "")
			if response.Code != http.StatusCreated {
				t.Fatalf("status = %d, want 201: %s", response.Code, response.Body.String())
			}
			if tokens.createdFor != "did:plc:alice" {
				t.Fatalf("created for %q", tokens.createdFor)
			}
			var body generated.CreatedAccessToken
			if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
				t.Fatalf("decode token: %v", err)
			}
			if body.Token != "adn_pat_one_time_plaintext" || response.Header().Get("Location") == "" {
				t.Fatalf("created token response = %#v", body)
			}
		}},

		{name: "missing origin is forbidden", run: func(t *testing.T) {
			response := performAPIRequest(server, http.MethodPost, "/api/v1/tokens", `{"name":"laptop","scopes":["repository:write"]}`, true, false, "")
			assertAPIError(t, response, http.StatusForbidden, "permission_denied")
		}},

		{name: "PAT cannot administer credentials", run: func(t *testing.T) {
			response := performAPIRequest(server, http.MethodGet, "/api/v1/tokens", "", false, false, "valid-pat")
			assertAPIError(t, response, http.StatusForbidden, "permission_denied")
		}},

		{name: "missing authentication", run: func(t *testing.T) {
			response := performAPIRequest(server, http.MethodGet, "/api/v1/ssh-keys", "", false, false, "")
			assertAPIError(t, response, http.StatusUnauthorized, "authentication_required")
		}},

		{name: "unknown JSON field", run: func(t *testing.T) {
			response := performAPIRequest(server, http.MethodPost, "/api/v1/ssh-keys", `{"name":"laptop","public_key":"ssh-ed25519 AAAA","account_did":"did:plc:mallory"}`, true, true, "")
			assertAPIError(t, response, http.StatusBadRequest, "malformed_request")
		}},

		{name: "duplicate SSH key conflict", run: func(t *testing.T) {
			keys.createErr = auth.ErrConflict
			response := performAPIRequest(server, http.MethodPost, "/api/v1/ssh-keys", `{"name":"laptop","public_key":"ssh-ed25519 AAAA"}`, true, true, "")
			assertAPIError(t, response, http.StatusConflict, "conflict")
			keys.createErr = nil
		}},

		{name: "revoke derives account from session", run: func(t *testing.T) {
			response := performAPIRequest(server, http.MethodDelete, "/api/v1/tokens/0198a851-2a89-7ae2-a370-dc68883e3af3", "", true, true, "")
			if response.Code != http.StatusNoContent {
				t.Fatalf("status = %d, want 204: %s", response.Code, response.Body.String())
			}
			if tokens.revokedFor != "did:plc:alice" {
				t.Fatalf("revoked for %q", tokens.revokedFor)
			}
		}},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, testCase.run)
	}
}

func TestDeveloperProfileEndpoints(t *testing.T) {
	t.Parallel()
	indexedAt := time.Date(2026, 8, 9, 1, 2, 3, 0, time.UTC)
	recordCreatedAt := indexedAt.Add(-time.Hour)
	did := "did:plc:abcdefghijklmnopqrstuvwx"
	value := profile.Profile{
		DID: did, URI: "at://" + did + "/dev.adenosine.profile/self", CID: "bafyreiprofile",
		Handle: "alice.example", DisplayName: "Alice", Bio: "Builds things", Website: "https://alice.example",
		Location: "Earth", RepositoryCount: 4, ContributionCount: 12,
		RecordCreatedAt: recordCreatedAt, IndexedAt: indexedAt,
	}
	profiles := &fakeProfiles{value: value}
	server := testAPIServer(t, Dependencies{Sessions: fakeSessions{}, TokenAuth: fakeTokenAuth{}, Profiles: profiles})
	testCases := []struct {
		name, method, path, body, bearer string
		session, origin                  bool
		status                           int
		code                             string
		verify                           func(*testing.T, *httptest.ResponseRecorder)
	}{
		{name: "public GET", method: http.MethodGet, path: "/api/v1/profiles/" + did, status: http.StatusOK, verify: func(t *testing.T, response *httptest.ResponseRecorder) {
			var body generated.DeveloperProfile
			if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
				t.Fatalf("decode profile: %v", err)
			}
			if profiles.getDID != did || body.Did != did || body.Uri == nil || body.Cid == nil || body.RepositoryCount != 4 || body.ContributionCount != 12 || body.RecordCreatedAt == nil || !body.IndexedAt.Equal(indexedAt) {
				t.Fatalf("profile = %#v, requested DID = %q", body, profiles.getDID)
			}
		}},
		{name: "session PUT derives identity", method: http.MethodPut, path: "/api/v1/profile", body: `{"display_name":"Updated","bio":"New bio","website":null,"location":"Moon"}`, session: true, origin: true, status: http.StatusOK, verify: func(t *testing.T, _ *httptest.ResponseRecorder) {
			if profiles.updateDID != "did:plc:alice" || profiles.updateInput != (profile.UpdateInput{DisplayName: "Updated", Bio: "New bio", Location: "Moon"}) {
				t.Fatalf("updated DID/input = %q, %#v", profiles.updateDID, profiles.updateInput)
			}
		}},
		{name: "PAT is forbidden", method: http.MethodPut, path: "/api/v1/profile", body: `{}`, origin: true, bearer: "valid-pat", status: http.StatusForbidden, code: "permission_denied"},
		{name: "missing origin is forbidden", method: http.MethodPut, path: "/api/v1/profile", body: `{}`, session: true, status: http.StatusForbidden, code: "permission_denied"},
		{name: "unknown JSON field", method: http.MethodPut, path: "/api/v1/profile", body: `{"display_name":"Mallory","did":"did:plc:mallory"}`, session: true, origin: true, status: http.StatusBadRequest, code: "malformed_request"},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			response := performAPIRequest(server, tc.method, tc.path, tc.body, tc.session, tc.origin, tc.bearer)
			if tc.code != "" {
				assertAPIError(t, response, tc.status, tc.code)
			} else if response.Code != tc.status {
				t.Fatalf("status = %d, want %d: %s", response.Code, tc.status, response.Body.String())
			}
			if tc.verify != nil {
				tc.verify(t, response)
			}
		})
	}
}

func TestDeveloperProfileErrors(t *testing.T) {
	t.Parallel()
	testCases := []struct {
		name   string
		method string
		path   string
		body   string
		err    error
		status int
		code   string
	}{
		{name: "validation", method: http.MethodPut, path: "/api/v1/profile", body: `{}`, err: &profile.ValidationError{Field: "website", Problem: "secret provider detail"}, status: http.StatusUnprocessableEntity, code: "validation_failed"},
		{name: "not found", method: http.MethodGet, path: "/api/v1/profiles/did:plc:abcdefghijklmnopqrstuvwx", err: &profile.NotFoundError{DID: "did:plc:abcdefghijklmnopqrstuvwx"}, status: http.StatusNotFound, code: "not_found"},
		{name: "provider", method: http.MethodGet, path: "/api/v1/profiles/did:plc:abcdefghijklmnopqrstuvwx", err: &profile.ProviderError{Operation: "get", Err: errors.New("secret provider detail")}, status: http.StatusBadGateway, code: "profile_provider_unavailable"},
		{name: "invalid provider data", method: http.MethodGet, path: "/api/v1/profiles/did:plc:abcdefghijklmnopqrstuvwx", err: &profile.ProviderError{Operation: "get", Err: &profile.ValidationError{Field: "CID", Problem: "secret provider detail"}}, status: http.StatusBadGateway, code: "profile_provider_unavailable"},
		{name: "authorization required", method: http.MethodPut, path: "/api/v1/profile", body: `{}`, err: &profile.AuthorizationError{Err: errors.New("secret provider detail")}, status: http.StatusConflict, code: "atproto_authorization_required"},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			profiles := &fakeProfiles{}
			if testCase.method == http.MethodPut {
				profiles.updateErr = testCase.err
			} else {
				profiles.getErr = testCase.err
			}
			server := testAPIServer(t, Dependencies{Sessions: fakeSessions{}, Profiles: profiles})
			response := performAPIRequest(server, testCase.method, testCase.path, testCase.body, testCase.method == http.MethodPut, testCase.method == http.MethodPut, "")
			assertAPIError(t, response, testCase.status, testCase.code)
			if strings.Contains(response.Body.String(), "secret provider detail") {
				t.Fatalf("error leaked internal detail: %s", response.Body.String())
			}
		})
	}
}

func TestRepositoryReadEndpoints(t *testing.T) {
	t.Parallel()
	repositoryID := repository.ID(uuid.MustParse("0198a851-2a89-7ae2-a370-dc68883e3af2"))
	repo := repository.Repository{ID: repositoryID, OwnerDID: "did:plc:alice", Slug: "project", Visibility: repository.VisibilityPublic, State: repository.StateActive, DefaultBranch: "main"}
	blob := []byte("hello\x00world")
	server := testAPIServer(t, Dependencies{
		Sessions: fakeSessions{}, TokenAuth: fakeTokenAuth{},
		Repositories: fixedRepositoryManager{repository: repo}, Authorization: fakeAuthorization{}, Git: fakeGitReader{blob: blob},
	})

	testCases := []struct {
		name string
		run  func(*testing.T)
	}{
		{name: "anonymous branches", run: func(t *testing.T) {
			response := performAPIRequest(server, http.MethodGet, "/api/v1/repositories/alice/project/branches", "", false, false, "")
			if response.Code != http.StatusOK {
				t.Fatalf("status = %d: %s", response.Code, response.Body.String())
			}
			var body generated.BranchList
			if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil || len(body.Items) != 1 || !body.Items[0].Default {
				t.Fatalf("branches = %#v, error = %v", body, err)
			}
		}},

		{name: "default tree", run: func(t *testing.T) {
			response := performAPIRequest(server, http.MethodGet, "/api/v1/repositories/alice/project/tree?path=", "", false, false, "")
			if response.Code != http.StatusOK {
				t.Fatalf("status = %d: %s", response.Code, response.Body.String())
			}
			var body generated.Tree
			if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil || body.Revision != "main" || len(body.Entries) != 2 || body.Entries[0].Size != nil || body.Entries[1].Size == nil {
				t.Fatalf("tree = %#v, error = %v", body, err)
			}
		}},

		{name: "invalid tree revision", run: func(t *testing.T) {
			response := performAPIRequest(server, http.MethodGet, "/api/v1/repositories/alice/project/tree?rev=bad", "", false, false, "")
			assertAPIError(t, response, http.StatusBadRequest, "malformed_request")
		}},

		{name: "raw blob", run: func(t *testing.T) {
			sha := strings.Repeat("d", 40)
			response := performAPIRequest(server, http.MethodGet, "/api/v1/repositories/alice/project/blobs/"+sha, "", false, false, "")
			if response.Code != http.StatusOK || !bytes.Equal(response.Body.Bytes(), blob) {
				t.Fatalf("status = %d, blob = %v", response.Code, response.Body.Bytes())
			}
			if response.Header().Get("Content-Type") != "application/octet-stream" || response.Header().Get("ETag") != `"`+sha+`"` || response.Header().Get("Cache-Control") != "public, max-age=31536000, immutable" {
				t.Fatalf("blob headers = %v", response.Header())
			}
		}},

		{name: "commit history and detail", run: func(t *testing.T) {
			response := performAPIRequest(server, http.MethodGet, "/api/v1/repositories/alice/project/commits?ref=main&limit=10", "", false, false, "")
			if response.Code != http.StatusOK {
				t.Fatalf("history status = %d: %s", response.Code, response.Body.String())
			}
			var history generated.CommitList
			if err := json.Unmarshal(response.Body.Bytes(), &history); err != nil || len(history.Items) != 1 || history.Items[0].Summary != "Update README" {
				t.Fatalf("history = %#v, error = %v", history, err)
			}
			response = performAPIRequest(server, http.MethodGet, "/api/v1/repositories/alice/project/commits/main", "", false, false, "")
			var detail generated.Commit
			if err := json.Unmarshal(response.Body.Bytes(), &detail); response.Code != http.StatusOK || err != nil || !strings.Contains(detail.Message, "Details.") {
				t.Fatalf("detail status = %d, body = %#v, error = %v", response.Code, detail, err)
			}
		}},

		{name: "diff and merge base", run: func(t *testing.T) {
			response := performAPIRequest(server, http.MethodGet, "/api/v1/repositories/alice/project/diff?base=main~1&head=main", "", false, false, "")
			var diff generated.Diff
			if err := json.Unmarshal(response.Body.Bytes(), &diff); response.Code != http.StatusOK || err != nil || len(diff.Files) != 1 || diff.Files[0].Additions == nil {
				t.Fatalf("diff status = %d, body = %#v, error = %v", response.Code, diff, err)
			}
			response = performAPIRequest(server, http.MethodGet, "/api/v1/repositories/alice/project/merge-base?a=main~1&b=main", "", false, false, "")
			var mergeBase generated.MergeBase
			if err := json.Unmarshal(response.Body.Bytes(), &mergeBase); response.Code != http.StatusOK || err != nil || mergeBase.Sha != strings.Repeat("a", 40) {
				t.Fatalf("merge base status = %d, body = %#v, error = %v", response.Code, mergeBase, err)
			}
		}},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, testCase.run)
	}
}

func TestPrivateRepositoryReadRequiresScopedAuthorization(t *testing.T) {
	t.Parallel()
	testCases := []struct {
		name string
	}{
		{name: "requires matching scoped authorization"},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			repositoryID := repository.ID(uuid.MustParse("0198a851-2a89-7ae2-a370-dc68883e3af2"))
			repo := repository.Repository{ID: repositoryID, OwnerDID: "did:plc:alice", Slug: "private", Visibility: repository.VisibilityPrivate, State: repository.StateActive, DefaultBranch: "main"}
			dependencies := Dependencies{
				Sessions: fakeSessions{}, TokenAuth: fakeTokenAuth{},
				Repositories: fixedRepositoryManager{repository: repo}, Authorization: fakeAuthorization{}, Git: fakeGitReader{},
			}
			server := testAPIServer(t, dependencies)

			response := performAPIRequest(server, http.MethodGet, "/api/v1/repositories/alice/private/branches", "", false, false, "")
			assertAPIError(t, response, http.StatusUnauthorized, "authentication_required")

			response = performAPIRequest(server, http.MethodGet, "/api/v1/repositories/alice/private/branches", "", false, false, "valid-pat")
			if response.Code != http.StatusOK {
				t.Fatalf("authorized status = %d: %s", response.Code, response.Body.String())
			}

			otherRepository := repository.ID(uuid.MustParse("0198a851-2a89-7ae2-a370-dc68883e3af9"))
			dependencies.TokenAuth = configuredTokenAuth{token: auth.AccessToken{
				AccountDID: "did:plc:alice", Scopes: []string{auth.ScopeRepositoryRead}, RepositoryID: &otherRepository,
			}}
			server = testAPIServer(t, dependencies)
			response = performAPIRequest(server, http.MethodGet, "/api/v1/repositories/alice/private/branches", "", false, false, "valid-pat")
			assertAPIError(t, response, http.StatusNotFound, "not_found")
		})
	}
}

func TestATProtoLoginStartAndCallback(t *testing.T) {
	t.Parallel()
	testCases := []struct{ name string }{{name: "starts login and completes callback"}}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			expiresAt := time.Date(2026, 8, 10, 1, 2, 3, 0, time.UTC)
			login := &fakeLogin{result: localidentity.LoginResult{
				DID: "did:plc:alice", Handle: "alice.example", SessionPlaintext: "new-session", SessionExpiresAt: expiresAt,
			}}
			server := testAPIServer(t, Dependencies{Login: login})

			response := performAPIRequest(server, http.MethodPost, "/api/v1/auth/atproto/start", `{"identifier":"alice.example"}`, false, false, "")
			if response.Code != http.StatusOK || login.identifier != "alice.example" {
				t.Fatalf("start response = %d %q, identifier = %q", response.Code, response.Body.String(), login.identifier)
			}
			var started generated.ATProtoLoginStart
			if err := json.Unmarshal(response.Body.Bytes(), &started); err != nil || started.AuthorizationUrl != "https://provider.example/authorize" {
				t.Fatalf("start body = %#v, error = %v", started, err)
			}

			response = performAPIRequest(server, http.MethodGet, "/oauth/atproto/callback?state=state&iss=https%3A%2F%2Fissuer.example&code=code", "", false, false, "")
			if response.Code != http.StatusSeeOther || response.Header().Get("Location") != "/" {
				t.Fatalf("callback response = %d, location = %q", response.Code, response.Header().Get("Location"))
			}
			cookies := response.Result().Cookies()
			if len(cookies) != 1 || cookies[0].Name != "adenosine_session" || cookies[0].Value != "new-session" || !cookies[0].HttpOnly || cookies[0].Secure || cookies[0].SameSite != http.SameSiteLaxMode {
				t.Fatalf("session cookie = %#v", cookies)
			}
		})
	}
}

func TestLogoutRevokesSessionAndClearsCookie(t *testing.T) {
	t.Parallel()
	testCases := []struct{ name string }{{name: "revokes session and clears cookie"}}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			sessions := &fakeLocalSessions{}
			server := testAPIServer(t, Dependencies{Sessions: fakeSessions{}, LocalSessions: sessions})

			response := performAPIRequest(server, http.MethodPost, "/api/v1/auth/logout", "", true, true, "")
			if response.Code != http.StatusNoContent || sessions.accountDID != "did:plc:alice" || sessions.sessionID == uuid.Nil {
				t.Fatalf("logout response = %d, account = %q, session = %s", response.Code, sessions.accountDID, sessions.sessionID)
			}
			cookies := response.Result().Cookies()
			if len(cookies) != 1 || cookies[0].Name != "adenosine_session" || cookies[0].MaxAge != -1 || !cookies[0].HttpOnly {
				t.Fatalf("cleared cookie = %#v", cookies)
			}
		})
	}
}

func TestCurrentIdentityIncludesVerifiedHandle(t *testing.T) {
	t.Parallel()
	testCases := []struct{ name string }{{name: "includes verified handle"}}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			handle := "alice.example"
			server := testAPIServer(t, Dependencies{
				Sessions: fakeSessions{}, Accounts: fakeAccounts{account: auth.Account{DID: "did:plc:alice", Handle: &handle}},
			})

			response := performAPIRequest(server, http.MethodGet, "/api/v1/me", "", true, false, "")
			if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"handle":"alice.example"`) {
				t.Fatalf("identity response = %d %s", response.Code, response.Body.String())
			}
		})
	}
}

func TestOAuthClientMetadata(t *testing.T) {
	t.Parallel()
	testCases := []struct{ name string }{{name: "returns client metadata"}}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			server := testAPIServer(t, Dependencies{OAuthMetadata: fakeOAuthMetadata{}})

			response := performAPIRequest(server, http.MethodGet, "/oauth/client-metadata.json", "", false, false, "")
			if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"scope":"atproto"`) || !strings.Contains(response.Body.String(), `"dpop_bound_access_tokens":true`) {
				t.Fatalf("metadata response = %d %s", response.Code, response.Body.String())
			}
		})
	}
}

func testAPIServer(t *testing.T, dependencies Dependencies) *http.Server {
	t.Helper()
	server, err := NewServer(":0", "http://localhost:8080", fakeReadiness{}, slog.New(slog.NewTextHandler(io.Discard, nil)), Observability{}, dependencies, nil)
	if err != nil {
		t.Fatalf("create server: %v", err)
	}
	return server
}

func performAPIRequest(server *http.Server, method, path, body string, session, origin bool, bearer string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(method, path, bytes.NewBufferString(body))
	if body != "" {
		request.Header.Set("Content-Type", "application/json")
	}
	if session {
		request.AddCookie(&http.Cookie{Name: "adenosine_session", Value: "valid-session"})
	}
	if origin {
		request.Header.Set("Origin", "http://localhost:8080")
	}
	if bearer != "" {
		request.Header.Set("Authorization", "Bearer "+bearer)
	}
	response := httptest.NewRecorder()
	server.Handler.ServeHTTP(response, request)
	return response
}

func assertAPIError(t *testing.T, response *httptest.ResponseRecorder, status int, code string) {
	t.Helper()
	if response.Code != status {
		t.Fatalf("status = %d, want %d: %s", response.Code, status, response.Body.String())
	}
	var body generated.ErrorResponse
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode error: %v", err)
	}
	if body.Error.Code != code || body.Error.RequestId == "" || body.Error.RequestId != response.Header().Get("X-Request-ID") {
		t.Fatalf("error response = %#v, request ID header = %q", body, response.Header().Get("X-Request-ID"))
	}
}
