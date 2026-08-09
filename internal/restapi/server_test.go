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

type fakeAuthorization struct{}

func (fakeAuthorization) CanReadRepository(context.Context, string, repository.ID) (bool, error) {
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
	server, err := NewServer(":0", "http://localhost:8080", fakeReadiness{}, slog.New(slog.NewTextHandler(io.Discard, nil)), Dependencies{}, nil)
	if err != nil {
		t.Fatalf("create server: %v", err)
	}

	tests := []struct {
		path        string
		contentType string
	}{
		{path: "/docs/api", contentType: "text/html; charset=utf-8"},
		{path: "/openapi.json", contentType: "application/json"},
		{path: "/openapi.yaml", contentType: "application/yaml"},
	}
	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodGet, tt.path, nil)
			response := httptest.NewRecorder()
			server.Handler.ServeHTTP(response, request)
			if response.Code != http.StatusOK {
				t.Fatalf("status = %d, want %d", response.Code, http.StatusOK)
			}
			if got := response.Header().Get("Content-Type"); got != tt.contentType {
				t.Fatalf("content type = %q, want %q", got, tt.contentType)
			}
		})
	}

	request := httptest.NewRequest(http.MethodGet, "/openapi.json", nil)
	response := httptest.NewRecorder()
	server.Handler.ServeHTTP(response, request)
	var document map[string]any
	if err := json.Unmarshal(response.Body.Bytes(), &document); err != nil {
		t.Fatalf("OpenAPI response is not JSON: %v", err)
	}
	if document["openapi"] != "3.0.3" {
		t.Fatalf("OpenAPI version = %v, want 3.0.3", document["openapi"])
	}
}

func (f fakeReadiness) Ping(context.Context) error { return f.err }

func TestHealthEndpoints(t *testing.T) {
	t.Parallel()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	tests := []struct {
		name      string
		path      string
		readiness fakeReadiness
		want      int
	}{
		{name: "live", path: "/health/live", want: http.StatusOK},
		{name: "ready", path: "/health/ready", want: http.StatusOK},
		{name: "database unavailable", path: "/health/ready", readiness: fakeReadiness{err: errors.New("unavailable")}, want: http.StatusServiceUnavailable},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server, err := NewServer(":0", "http://localhost:8080", tt.readiness, logger, Dependencies{}, nil)
			if err != nil {
				t.Fatalf("create server: %v", err)
			}
			request := httptest.NewRequest(http.MethodGet, tt.path, nil)
			response := httptest.NewRecorder()
			server.Handler.ServeHTTP(response, request)
			if response.Code != tt.want {
				t.Fatalf("status = %d, want %d", response.Code, tt.want)
			}
			if response.Header().Get("X-Request-ID") == "" {
				t.Fatal("missing request ID")
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

	t.Run("create token returns plaintext once", func(t *testing.T) {
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
	})

	t.Run("missing origin is forbidden", func(t *testing.T) {
		response := performAPIRequest(server, http.MethodPost, "/api/v1/tokens", `{"name":"laptop","scopes":["repository:write"]}`, true, false, "")
		assertAPIError(t, response, http.StatusForbidden, "permission_denied")
	})

	t.Run("PAT cannot administer credentials", func(t *testing.T) {
		response := performAPIRequest(server, http.MethodGet, "/api/v1/tokens", "", false, false, "valid-pat")
		assertAPIError(t, response, http.StatusForbidden, "permission_denied")
	})

	t.Run("missing authentication", func(t *testing.T) {
		response := performAPIRequest(server, http.MethodGet, "/api/v1/ssh-keys", "", false, false, "")
		assertAPIError(t, response, http.StatusUnauthorized, "authentication_required")
	})

	t.Run("unknown JSON field", func(t *testing.T) {
		response := performAPIRequest(server, http.MethodPost, "/api/v1/ssh-keys", `{"name":"laptop","public_key":"ssh-ed25519 AAAA","account_did":"did:plc:mallory"}`, true, true, "")
		assertAPIError(t, response, http.StatusBadRequest, "malformed_request")
	})

	t.Run("duplicate SSH key conflict", func(t *testing.T) {
		keys.createErr = auth.ErrConflict
		response := performAPIRequest(server, http.MethodPost, "/api/v1/ssh-keys", `{"name":"laptop","public_key":"ssh-ed25519 AAAA"}`, true, true, "")
		assertAPIError(t, response, http.StatusConflict, "conflict")
		keys.createErr = nil
	})

	t.Run("revoke derives account from session", func(t *testing.T) {
		response := performAPIRequest(server, http.MethodDelete, "/api/v1/tokens/0198a851-2a89-7ae2-a370-dc68883e3af3", "", true, true, "")
		if response.Code != http.StatusNoContent {
			t.Fatalf("status = %d, want 204: %s", response.Code, response.Body.String())
		}
		if tokens.revokedFor != "did:plc:alice" {
			t.Fatalf("revoked for %q", tokens.revokedFor)
		}
	})
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
	tests := []struct {
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
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			profiles := &fakeProfiles{}
			if tt.method == http.MethodPut {
				profiles.updateErr = tt.err
			} else {
				profiles.getErr = tt.err
			}
			server := testAPIServer(t, Dependencies{Sessions: fakeSessions{}, Profiles: profiles})
			response := performAPIRequest(server, tt.method, tt.path, tt.body, tt.method == http.MethodPut, tt.method == http.MethodPut, "")
			assertAPIError(t, response, tt.status, tt.code)
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

	t.Run("anonymous branches", func(t *testing.T) {
		response := performAPIRequest(server, http.MethodGet, "/api/v1/repositories/alice/project/branches", "", false, false, "")
		if response.Code != http.StatusOK {
			t.Fatalf("status = %d: %s", response.Code, response.Body.String())
		}
		var body generated.BranchList
		if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil || len(body.Data) != 1 || !body.Data[0].Default {
			t.Fatalf("branches = %#v, error = %v", body, err)
		}
	})

	t.Run("default tree", func(t *testing.T) {
		response := performAPIRequest(server, http.MethodGet, "/api/v1/repositories/alice/project/tree?path=", "", false, false, "")
		if response.Code != http.StatusOK {
			t.Fatalf("status = %d: %s", response.Code, response.Body.String())
		}
		var body generated.Tree
		if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil || body.Revision != "main" || len(body.Entries) != 2 || body.Entries[0].Size != nil || body.Entries[1].Size == nil {
			t.Fatalf("tree = %#v, error = %v", body, err)
		}
	})

	t.Run("invalid tree revision", func(t *testing.T) {
		response := performAPIRequest(server, http.MethodGet, "/api/v1/repositories/alice/project/tree?rev=bad", "", false, false, "")
		assertAPIError(t, response, http.StatusBadRequest, "malformed_request")
	})

	t.Run("raw blob", func(t *testing.T) {
		sha := strings.Repeat("d", 40)
		response := performAPIRequest(server, http.MethodGet, "/api/v1/repositories/alice/project/blobs/"+sha, "", false, false, "")
		if response.Code != http.StatusOK || !bytes.Equal(response.Body.Bytes(), blob) {
			t.Fatalf("status = %d, blob = %v", response.Code, response.Body.Bytes())
		}
		if response.Header().Get("Content-Type") != "application/octet-stream" || response.Header().Get("ETag") != `"`+sha+`"` || response.Header().Get("Cache-Control") != "public, max-age=31536000, immutable" {
			t.Fatalf("blob headers = %v", response.Header())
		}
	})

	t.Run("commit history and detail", func(t *testing.T) {
		response := performAPIRequest(server, http.MethodGet, "/api/v1/repositories/alice/project/commits?ref=main&limit=10", "", false, false, "")
		if response.Code != http.StatusOK {
			t.Fatalf("history status = %d: %s", response.Code, response.Body.String())
		}
		var history generated.CommitList
		if err := json.Unmarshal(response.Body.Bytes(), &history); err != nil || len(history.Data) != 1 || history.Data[0].Summary != "Update README" {
			t.Fatalf("history = %#v, error = %v", history, err)
		}
		response = performAPIRequest(server, http.MethodGet, "/api/v1/repositories/alice/project/commits/main", "", false, false, "")
		var detail generated.Commit
		if err := json.Unmarshal(response.Body.Bytes(), &detail); response.Code != http.StatusOK || err != nil || !strings.Contains(detail.Message, "Details.") {
			t.Fatalf("detail status = %d, body = %#v, error = %v", response.Code, detail, err)
		}
	})

	t.Run("diff and merge base", func(t *testing.T) {
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
	})
}

func TestPrivateRepositoryReadRequiresScopedAuthorization(t *testing.T) {
	t.Parallel()
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
}

func TestATProtoLoginStartAndCallback(t *testing.T) {
	t.Parallel()
	expiresAt := time.Date(2026, 8, 10, 1, 2, 3, 0, time.UTC)
	login := &fakeLogin{result: localidentity.LoginResult{
		DID: "did:plc:alice", Handle: "alice.example", SessionPlaintext: "new-session", SessionExpiresAt: expiresAt,
	}}
	server := testAPIServer(t, Dependencies{Login: login})

	response := performAPIRequest(server, http.MethodPost, "/api/v1/auth/atproto/start", `{"identifier":"alice.example"}`, false, false, "")
	if response.Code != http.StatusOK || login.identifier != "alice.example" {
		t.Fatalf("start response = %d %q, identifier = %q", response.Code, response.Body.String(), login.identifier)
	}
	var started generated.StartATProtoLoginResponse
	if err := json.Unmarshal(response.Body.Bytes(), &started); err != nil || started.AuthorizationUrl != "https://provider.example/authorize" {
		t.Fatalf("start body = %#v, error = %v", started, err)
	}

	response = performAPIRequest(server, http.MethodGet, "/oauth/atproto/callback?state=state&iss=https%3A%2F%2Fissuer.example&code=code", "", false, false, "")
	if response.Code != http.StatusSeeOther || response.Header().Get("Location") != "/api/v1/me" {
		t.Fatalf("callback response = %d, location = %q", response.Code, response.Header().Get("Location"))
	}
	cookies := response.Result().Cookies()
	if len(cookies) != 1 || cookies[0].Name != "adenosine_session" || cookies[0].Value != "new-session" || !cookies[0].HttpOnly || cookies[0].Secure || cookies[0].SameSite != http.SameSiteLaxMode {
		t.Fatalf("session cookie = %#v", cookies)
	}
}

func TestLogoutRevokesSessionAndClearsCookie(t *testing.T) {
	t.Parallel()
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
}

func TestCurrentIdentityIncludesVerifiedHandle(t *testing.T) {
	t.Parallel()
	handle := "alice.example"
	server := testAPIServer(t, Dependencies{
		Sessions: fakeSessions{}, Accounts: fakeAccounts{account: auth.Account{DID: "did:plc:alice", Handle: &handle}},
	})

	response := performAPIRequest(server, http.MethodGet, "/api/v1/me", "", true, false, "")
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"handle":"alice.example"`) {
		t.Fatalf("identity response = %d %s", response.Code, response.Body.String())
	}
}

func TestOAuthClientMetadata(t *testing.T) {
	t.Parallel()
	server := testAPIServer(t, Dependencies{OAuthMetadata: fakeOAuthMetadata{}})

	response := performAPIRequest(server, http.MethodGet, "/oauth/client-metadata.json", "", false, false, "")
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"scope":"atproto"`) || !strings.Contains(response.Body.String(), `"dpop_bound_access_tokens":true`) {
		t.Fatalf("metadata response = %d %s", response.Code, response.Body.String())
	}
}

func testAPIServer(t *testing.T, dependencies Dependencies) *http.Server {
	t.Helper()
	server, err := NewServer(":0", "http://localhost:8080", fakeReadiness{}, slog.New(slog.NewTextHandler(io.Discard, nil)), dependencies, nil)
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
