package restapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	generated "github.com/adenosine-dev/adenosine/api/generated/go"
	localatproto "github.com/adenosine-dev/adenosine/internal/atproto"
	"github.com/adenosine-dev/adenosine/internal/auth"
	"github.com/adenosine-dev/adenosine/internal/comment"
	"github.com/adenosine-dev/adenosine/internal/federation"
	gitservice "github.com/adenosine-dev/adenosine/internal/git"
	localidentity "github.com/adenosine-dev/adenosine/internal/identity"
	"github.com/adenosine-dev/adenosine/internal/issue"
	"github.com/adenosine-dev/adenosine/internal/moderation"
	"github.com/adenosine-dev/adenosine/internal/passkey"
	"github.com/adenosine-dev/adenosine/internal/profile"
	"github.com/adenosine-dev/adenosine/internal/pullrequest"
	"github.com/adenosine-dev/adenosine/internal/repository"
	"github.com/adenosine-dev/adenosine/internal/star"
	"github.com/adenosine-dev/adenosine/internal/syncproxy"
	"github.com/google/uuid"
	openapi_types "github.com/oapi-codegen/runtime/types"
	"go.opentelemetry.io/otel/trace"
)

const (
	maxJSONBody           = 32 * 1024
	maxWebAuthnVerifyBody = 128 * 1024
)

type SessionAuthenticator interface {
	Authenticate(context.Context, string) (auth.SessionIdentity, error)
}

type LoginService interface {
	Start(context.Context, string, string) (string, error)
	Complete(context.Context, url.Values) (localidentity.LoginResult, error)
}

type LocalSessionManager interface {
	RevokeSession(context.Context, string, uuid.UUID) error
}

type PasskeyManager interface {
	BeginRegistration(context.Context, string, uuid.UUID, string) (passkey.BeginResult, error)
	FinishRegistration(context.Context, string, uuid.UUID, string, []byte) (passkey.CredentialSummary, error)
	BeginLogin(context.Context) (passkey.BeginResult, error)
	FinishLogin(context.Context, string, []byte) (passkey.LoginResult, error)
	List(context.Context, string) ([]passkey.CredentialSummary, error)
	Revoke(context.Context, string, uuid.UUID) error
}

type AccountReader interface {
	GetAccount(context.Context, string) (auth.Account, error)
}

type OAuthMetadataProvider interface {
	ClientMetadata() localatproto.ClientMetadata
}

type TokenAuthenticator interface {
	Authenticate(context.Context, string) (auth.AccessToken, error)
}

type TokenManager interface {
	CreateToken(context.Context, auth.CreateTokenInput) (auth.AccessToken, string, error)
	ListTokens(context.Context, string) ([]auth.AccessToken, error)
	RevokeToken(context.Context, string, uuid.UUID) error
}

type SSHKeyManager interface {
	CreateSSHKey(context.Context, auth.CreateSSHKeyInput) (auth.SSHKey, error)
	ListSSHKeys(context.Context, string) ([]auth.SSHKey, error)
	RevokeSSHKey(context.Context, string, uuid.UUID) error
}

type RepositoryManager interface {
	Create(context.Context, repository.CreateInput) (repository.Repository, error)
	GetByOwnerSlug(context.Context, string, string) (repository.Repository, error)
}

type RepositoryEndpointBuilder interface {
	For(repository.Repository) (web, gitHTTPS, gitSSH string)
}

type NetworkRepositoryDiscovery interface {
	ListNetworkRepositories(context.Context, int, string) (federation.DiscoveryPage, error)
}

type StarManager interface {
	Get(context.Context, string) (star.Projection, error)
	Create(context.Context, string, string) (star.Star, error)
	Delete(context.Context, string, string) error
}

type IssueManager interface {
	Get(context.Context, string) (issue.Projection, error)
	Create(context.Context, string, issue.CreateInput) (issue.Issue, error)
	PutStatus(context.Context, string, issue.StatusInput) (issue.Status, error)
}

type PullRequestManager interface {
	List(context.Context, string) (pullrequest.Projection, error)
	Get(context.Context, string) (pullrequest.ProjectedPullRequest, error)
	Create(context.Context, string, pullrequest.CreateInput) (pullrequest.PullRequest, error)
	Refresh(context.Context, string) (pullrequest.Result, error)
	Reviews(context.Context, string) ([]pullrequest.ProjectedReview, error)
	CreateReview(context.Context, string, pullrequest.ReviewInput) (pullrequest.Review, error)
	PutStatus(context.Context, string, pullrequest.StatusInput) (pullrequest.Status, error)
	Merge(context.Context, string, pullrequest.MergeInput) (pullrequest.MergeResult, error)
}

type CommentManager interface {
	Get(context.Context, string, string) (comment.Projection, error)
	Create(context.Context, string, comment.CreateInput) (issue.Comment, error)
	Delete(context.Context, string, string) error
}

type ModerationManager interface {
	Block(context.Context, string, string) error
	Unblock(context.Context, string, string) error
	ListBlocks(context.Context, string) ([]moderation.BlockedDID, error)
	Hide(context.Context, string, string) error
	Unhide(context.Context, string, string) error
	ListHidden(context.Context, string) ([]moderation.HiddenRecord, error)
}

type ProfileManager interface {
	Get(context.Context, string) (profile.Profile, error)
	Update(context.Context, string, profile.UpdateInput) (profile.Profile, error)
}

type RepositoryAuthorizer interface {
	CanReadRepository(context.Context, string, repository.ID) (bool, error)
}

type GitReader interface {
	Branches(context.Context, repository.ID, string) ([]gitservice.Branch, error)
	Tags(context.Context, repository.ID) ([]gitservice.Tag, error)
	Tree(context.Context, repository.ID, string, string) (gitservice.Tree, error)
	BlobMetadata(context.Context, repository.ID, string) (gitservice.BlobMetadata, error)
	StreamBlob(context.Context, repository.ID, string, io.Writer) error
	Commits(context.Context, repository.ID, string, int) ([]gitservice.CommitSummary, error)
	Commit(context.Context, repository.ID, string) (gitservice.Commit, error)
	Diff(context.Context, repository.ID, string, string) (gitservice.Diff, error)
	MergeBase(context.Context, repository.ID, string, string) (string, error)
}

type SyncRepositoryProxy interface {
	Forward(http.ResponseWriter, *http.Request) error
}

// FederationProcessor applies a single event delivered by Tap.
type FederationProcessor interface {
	Process(context.Context, []byte) error
}

// FederationDependencies configures the internal Tap webhook transport.
type FederationDependencies struct {
	Processor        FederationProcessor
	TapAdminPassword string
}

// Dependencies contains the application capabilities exposed by REST.
type Dependencies struct {
	Sessions         SessionAuthenticator
	Login            LoginService
	LocalSessions    LocalSessionManager
	Passkeys         PasskeyManager
	Accounts         AccountReader
	OAuthMetadata    OAuthMetadataProvider
	TokenAuth        TokenAuthenticator
	Tokens           TokenManager
	SSHKeys          SSHKeyManager
	Profiles         ProfileManager
	Repositories     RepositoryManager
	Endpoints        RepositoryEndpointBuilder
	Discovery        NetworkRepositoryDiscovery
	Stars            StarManager
	Issues           IssueManager
	PullRequests     PullRequestManager
	Comments         CommentManager
	Moderation       ModerationManager
	Authorization    RepositoryAuthorizer
	Git              GitReader
	SyncRepositories SyncRepositoryProxy
	Federation       *FederationDependencies
}

type principal struct {
	accountDID   string
	sessionID    uuid.UUID
	session      bool
	scopes       []string
	repositoryID *repository.ID
}

type apiHandler struct {
	readiness    readinessChecker
	logger       *slog.Logger
	baseURL      string
	origin       string
	secureCookie bool
	deps         Dependencies
}

func newAPIHandler(baseURL string, readiness readinessChecker, logger *slog.Logger, deps Dependencies) *apiHandler {
	origin := canonicalOrigin(baseURL)
	parsedBase, _ := url.Parse(baseURL)
	return &apiHandler{
		readiness: readiness, logger: logger, baseURL: strings.TrimSuffix(baseURL, "/"), origin: origin,
		secureCookie: parsedBase != nil && parsedBase.Scheme == "https", deps: deps,
	}
}

func canonicalOrigin(baseURL string) string {
	parsed, err := url.Parse(baseURL)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return baseURL
	}
	scheme := strings.ToLower(parsed.Scheme)
	hostname := strings.ToLower(parsed.Hostname())
	port := parsed.Port()
	if (scheme == "http" && port == "80") || (scheme == "https" && port == "443") {
		port = ""
	}
	host := hostname
	if port != "" {
		host = net.JoinHostPort(hostname, port)
	} else if strings.Contains(hostname, ":") {
		host = "[" + hostname + "]"
	}
	return scheme + "://" + host
}

func (handler *apiHandler) GetLiveness(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, generated.Health{Status: generated.Ok})
}

func (handler *apiHandler) GetReadiness(w http.ResponseWriter, r *http.Request) {
	if err := handler.readiness.Ping(r.Context()); err != nil {
		trace.SpanFromContext(r.Context()).RecordError(err)
		writeJSON(w, http.StatusServiceUnavailable, generated.Health{Status: generated.Unavailable})
		return
	}
	writeJSON(w, http.StatusOK, generated.Health{Status: generated.Ok})
}

func (handler *apiHandler) GetCurrentIdentity(w http.ResponseWriter, r *http.Request) {
	identity, err := handler.authenticate(r)
	if err != nil {
		handler.writeError(w, r, err)
		return
	}
	response := generated.CurrentIdentity{Did: identity.accountDID}
	if handler.deps.Accounts != nil {
		account, err := handler.deps.Accounts.GetAccount(r.Context(), identity.accountDID)
		if err != nil {
			handler.writeError(w, r, err)
			return
		}
		response.Handle = account.Handle
	}
	writeJSON(w, http.StatusOK, response)
}

func (handler *apiHandler) StartATProtoLogin(w http.ResponseWriter, r *http.Request) {
	var request generated.StartATProtoLoginRequest
	if err := decodeJSON(w, r, &request); err != nil {
		handler.writeMalformed(w, r, err)
		return
	}
	authorizationURL, err := handler.deps.Login.Start(r.Context(), request.Identifier, "")
	if err != nil {
		handler.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, generated.StartATProtoLoginResponse{AuthorizationUrl: authorizationURL})
}

func (handler *apiHandler) CompleteATProtoLogin(w http.ResponseWriter, r *http.Request, _ generated.CompleteATProtoLoginParams) {
	result, err := handler.deps.Login.Complete(r.Context(), r.URL.Query())
	if err != nil {
		handler.writeError(w, r, err)
		return
	}
	handler.setSessionCookie(w, result.SessionPlaintext, result.SessionExpiresAt)
	http.Redirect(w, r, "/api/v1/me", http.StatusSeeOther)
}

func (handler *apiHandler) BeginPasskeyRegistration(w http.ResponseWriter, r *http.Request, _ generated.BeginPasskeyRegistrationParams) {
	identity, err := handler.requireSession(r, true)
	if err != nil {
		handler.writeError(w, r, err)
		return
	}
	var request generated.BeginPasskeyRegistrationRequest
	if err := decodeJSON(w, r, &request); err != nil {
		handler.writeMalformed(w, r, err)
		return
	}
	result, err := handler.deps.Passkeys.BeginRegistration(r.Context(), identity.accountDID, identity.sessionID, request.Name)
	if err != nil {
		handler.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, passkeyOptionsResponse{Options: result.Options, CeremonyToken: result.Token})
}

func (handler *apiHandler) VerifyPasskeyRegistration(w http.ResponseWriter, r *http.Request, _ generated.VerifyPasskeyRegistrationParams) {
	identity, err := handler.requireSession(r, true)
	if err != nil {
		handler.writeError(w, r, err)
		return
	}
	request, err := decodePasskeyVerification(w, r)
	if err != nil {
		handler.writeMalformed(w, r, err)
		return
	}
	credential, err := handler.deps.Passkeys.FinishRegistration(r.Context(), identity.accountDID, identity.sessionID, request.CeremonyToken, request.Response)
	if err != nil {
		handler.writeError(w, r, err)
		return
	}
	w.Header().Set("Location", "/api/v1/passkeys/"+credential.ID.String())
	writeJSON(w, http.StatusCreated, passkeyResponse(credential))
}

func (handler *apiHandler) BeginPasskeyLogin(w http.ResponseWriter, r *http.Request, _ generated.BeginPasskeyLoginParams) {
	if !handler.validOrigin(r) {
		handler.writeError(w, r, auth.ErrForbidden)
		return
	}
	result, err := handler.deps.Passkeys.BeginLogin(r.Context())
	if err != nil {
		handler.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, passkeyOptionsResponse{Options: result.Options, CeremonyToken: result.Token})
}

func (handler *apiHandler) VerifyPasskeyLogin(w http.ResponseWriter, r *http.Request, _ generated.VerifyPasskeyLoginParams) {
	if !handler.validOrigin(r) {
		handler.writeError(w, r, auth.ErrForbidden)
		return
	}
	request, err := decodePasskeyVerification(w, r)
	if err != nil {
		handler.writeMalformed(w, r, err)
		return
	}
	result, err := handler.deps.Passkeys.FinishLogin(r.Context(), request.CeremonyToken, request.Response)
	if err != nil {
		handler.writeError(w, r, err)
		return
	}
	handler.setSessionCookie(w, result.SessionPlaintext, result.SessionExpiresAt)
	w.WriteHeader(http.StatusNoContent)
}

func (handler *apiHandler) ListPasskeys(w http.ResponseWriter, r *http.Request) {
	identity, err := handler.requireSession(r, false)
	if err != nil {
		handler.writeError(w, r, err)
		return
	}
	credentials, err := handler.deps.Passkeys.List(r.Context(), identity.accountDID)
	if err != nil {
		handler.writeError(w, r, err)
		return
	}
	data := make([]generated.Passkey, len(credentials))
	for index, credential := range credentials {
		data[index] = passkeyResponse(credential)
	}
	writeJSON(w, http.StatusOK, generated.PasskeyList{Data: data})
}

func (handler *apiHandler) DeletePasskey(w http.ResponseWriter, r *http.Request, id openapi_types.UUID, _ generated.DeletePasskeyParams) {
	identity, err := handler.requireSession(r, true)
	if err != nil {
		handler.writeError(w, r, err)
		return
	}
	if err := handler.deps.Passkeys.Revoke(r.Context(), identity.accountDID, uuid.UUID(id)); err != nil {
		handler.writeError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (handler *apiHandler) setSessionCookie(w http.ResponseWriter, plaintext string, expiresAt time.Time) {
	http.SetCookie(w, &http.Cookie{
		Name: "adenosine_session", Value: plaintext, Path: "/", Expires: expiresAt,
		HttpOnly: true, Secure: handler.secureCookie, SameSite: http.SameSiteLaxMode,
	})
}

func (handler *apiHandler) GetOAuthClientMetadata(w http.ResponseWriter, _ *http.Request) {
	metadata := handler.deps.OAuthMetadata.ClientMetadata()
	writeJSON(w, http.StatusOK, generated.OAuthClientMetadata{
		ClientId: metadata.ClientID, ApplicationType: metadata.ApplicationType, GrantTypes: metadata.GrantTypes,
		Scope: metadata.Scope, ResponseTypes: metadata.ResponseTypes, RedirectUris: metadata.RedirectURIs,
		DpopBoundAccessTokens: metadata.DPoPBoundAccessTokens, TokenEndpointAuthMethod: metadata.TokenEndpointAuthMethod,
	})
}

func (handler *apiHandler) Logout(w http.ResponseWriter, r *http.Request) {
	identity, err := handler.requireSession(r, true)
	if err != nil {
		handler.writeError(w, r, err)
		return
	}
	if err := handler.deps.LocalSessions.RevokeSession(r.Context(), identity.accountDID, identity.sessionID); err != nil {
		handler.writeError(w, r, err)
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name: "adenosine_session", Path: "/", MaxAge: -1, Expires: time.Unix(1, 0),
		HttpOnly: true, Secure: handler.secureCookie, SameSite: http.SameSiteLaxMode,
	})
	w.WriteHeader(http.StatusNoContent)
}

func (handler *apiHandler) GetDeveloperProfile(w http.ResponseWriter, r *http.Request, did string) {
	developerProfile, err := handler.deps.Profiles.Get(r.Context(), did)
	if err != nil {
		handler.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, developerProfileResponse(developerProfile))
}

func (handler *apiHandler) UpdateDeveloperProfile(w http.ResponseWriter, r *http.Request) {
	identity, err := handler.requireSession(r, true)
	if err != nil {
		handler.writeError(w, r, err)
		return
	}
	var request generated.UpdateDeveloperProfileRequest
	if err := decodeJSON(w, r, &request); err != nil {
		handler.writeMalformed(w, r, err)
		return
	}
	developerProfile, err := handler.deps.Profiles.Update(r.Context(), identity.accountDID, profile.UpdateInput{
		DisplayName: optionalString(request.DisplayName), Bio: optionalString(request.Bio),
		Website: optionalString(request.Website), Location: optionalString(request.Location),
	})
	if err != nil {
		handler.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, developerProfileResponse(developerProfile))
}

func (handler *apiHandler) ListAccessTokens(w http.ResponseWriter, r *http.Request) {
	identity, err := handler.requireSession(r, false)
	if err != nil {
		handler.writeError(w, r, err)
		return
	}
	tokens, err := handler.deps.Tokens.ListTokens(r.Context(), identity.accountDID)
	if err != nil {
		handler.writeError(w, r, err)
		return
	}
	data := make([]generated.AccessToken, len(tokens))
	for index, token := range tokens {
		data[index] = accessTokenResponse(token)
	}
	writeJSON(w, http.StatusOK, generated.AccessTokenList{Data: data, Page: generated.Page{}})
}

func (handler *apiHandler) CreateAccessToken(w http.ResponseWriter, r *http.Request) {
	identity, err := handler.requireSession(r, true)
	if err != nil {
		handler.writeError(w, r, err)
		return
	}
	var request generated.CreateAccessTokenRequest
	if err := decodeJSON(w, r, &request); err != nil {
		handler.writeMalformed(w, r, err)
		return
	}
	scopes := make([]string, len(request.Scopes))
	for index, scope := range request.Scopes {
		scopes[index] = string(scope)
	}
	var repositoryID *repository.ID
	if request.RepositoryId != nil {
		value := repository.ID(*request.RepositoryId)
		repositoryID = &value
	}
	token, plaintext, err := handler.deps.Tokens.CreateToken(r.Context(), auth.CreateTokenInput{
		AccountDID: identity.accountDID, Name: request.Name, Scopes: scopes,
		RepositoryID: repositoryID, ExpiresAt: request.ExpiresAt,
	})
	if err != nil {
		handler.writeError(w, r, err)
		return
	}
	response := createdAccessTokenResponse(token, plaintext)
	w.Header().Set("Location", "/api/v1/tokens/"+token.ID.String())
	writeJSON(w, http.StatusCreated, response)
}

func (handler *apiHandler) RevokeAccessToken(w http.ResponseWriter, r *http.Request, id openapi_types.UUID) {
	identity, err := handler.requireSession(r, true)
	if err != nil {
		handler.writeError(w, r, err)
		return
	}
	if err := handler.deps.Tokens.RevokeToken(r.Context(), identity.accountDID, uuid.UUID(id)); err != nil {
		handler.writeError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (handler *apiHandler) ListSSHKeys(w http.ResponseWriter, r *http.Request) {
	identity, err := handler.requireSession(r, false)
	if err != nil {
		handler.writeError(w, r, err)
		return
	}
	keys, err := handler.deps.SSHKeys.ListSSHKeys(r.Context(), identity.accountDID)
	if err != nil {
		handler.writeError(w, r, err)
		return
	}
	data := make([]generated.SSHKey, len(keys))
	for index, key := range keys {
		data[index] = sshKeyResponse(key)
	}
	writeJSON(w, http.StatusOK, generated.SSHKeyList{Data: data, Page: generated.Page{}})
}

func (handler *apiHandler) CreateSSHKey(w http.ResponseWriter, r *http.Request) {
	identity, err := handler.requireSession(r, true)
	if err != nil {
		handler.writeError(w, r, err)
		return
	}
	var request generated.CreateSSHKeyRequest
	if err := decodeJSON(w, r, &request); err != nil {
		handler.writeMalformed(w, r, err)
		return
	}
	key, err := handler.deps.SSHKeys.CreateSSHKey(r.Context(), auth.CreateSSHKeyInput{
		AccountDID: identity.accountDID, Name: request.Name, AuthorizedKey: request.PublicKey,
	})
	if err != nil {
		handler.writeError(w, r, err)
		return
	}
	w.Header().Set("Location", "/api/v1/ssh-keys/"+key.ID.String())
	writeJSON(w, http.StatusCreated, sshKeyResponse(key))
}

func (handler *apiHandler) RevokeSSHKey(w http.ResponseWriter, r *http.Request, id openapi_types.UUID) {
	identity, err := handler.requireSession(r, true)
	if err != nil {
		handler.writeError(w, r, err)
		return
	}
	if err := handler.deps.SSHKeys.RevokeSSHKey(r.Context(), identity.accountDID, uuid.UUID(id)); err != nil {
		handler.writeError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (handler *apiHandler) CreateRepository(w http.ResponseWriter, r *http.Request, _ generated.CreateRepositoryParams) {
	identity, err := handler.authenticate(r)
	if err != nil {
		handler.writeError(w, r, err)
		return
	}
	if !identity.session && (!slices.Contains(identity.scopes, auth.ScopeRepositoryWrite) || identity.repositoryID != nil) {
		handler.writeError(w, r, auth.ErrForbidden)
		return
	}
	if identity.session && !handler.validOrigin(r) {
		handler.writeError(w, r, auth.ErrForbidden)
		return
	}
	var request generated.CreateRepositoryRequest
	if err := decodeJSON(w, r, &request); err != nil {
		handler.writeMalformed(w, r, err)
		return
	}
	visibility := repository.VisibilityPublic
	if request.Visibility != nil {
		visibility = repository.Visibility(*request.Visibility)
	}
	defaultBranch := "main"
	if request.DefaultBranch != nil {
		defaultBranch = *request.DefaultBranch
	}
	repo, err := handler.deps.Repositories.Create(r.Context(), repository.CreateInput{
		OwnerDID: identity.accountDID, Slug: request.Slug, DisplayName: optionalString(request.DisplayName),
		Description: optionalString(request.Description), Visibility: visibility, DefaultBranch: defaultBranch,
	})
	if err != nil {
		handler.writeError(w, r, err)
		return
	}
	w.Header().Set("Location", "/api/v1/repositories/"+repo.OwnerDID+"/"+repo.Slug)
	writeJSON(w, http.StatusCreated, handler.repositoryResponse(repo))
}

func (handler *apiHandler) GetRepository(w http.ResponseWriter, r *http.Request, owner string, slug generated.RepositorySlug) {
	repo, err := handler.readableRepository(r, owner, slug)
	if err != nil {
		handler.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, handler.repositoryResponse(repo))
}

func (handler *apiHandler) ListNetworkRepositories(w http.ResponseWriter, r *http.Request, params generated.ListNetworkRepositoriesParams) {
	limit := 0
	if params.Limit != nil {
		limit = *params.Limit
	}
	cursor := ""
	if params.Cursor != nil {
		cursor = *params.Cursor
	}
	page, err := handler.deps.Discovery.ListNetworkRepositories(r.Context(), limit, cursor)
	if err != nil {
		if errors.Is(err, federation.ErrInvalidCursor) || errors.Is(err, federation.ErrInvalidLimit) {
			handler.writeMalformed(w, r, err)
			return
		}
		handler.writeError(w, r, err)
		return
	}
	data := make([]generated.Repository, len(page.Repositories))
	for index, repo := range page.Repositories {
		data[index] = networkRepositoryResponse(repo)
	}
	writeJSON(w, http.StatusOK, generated.NetworkRepositoryList{Data: data, Page: generated.Page{NextCursor: page.NextCursor}})
}

func (handler *apiHandler) GetSyncRepositories(w http.ResponseWriter, r *http.Request, _ generated.GetSyncRepositoriesParams) {
	handler.syncRepositories(w, r)
}

func (handler *apiHandler) PostSyncRepositories(w http.ResponseWriter, r *http.Request, _ generated.PostSyncRepositoriesParams) {
	handler.syncRepositories(w, r)
}

func (handler *apiHandler) syncRepositories(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Vary", "Cookie, Authorization")
	if handler.deps.SyncRepositories == nil {
		handler.writeAPIError(w, r, http.StatusServiceUnavailable, "sync_disabled", "Realtime sync is not configured", syncproxy.ErrDisabled)
		return
	}
	if err := handler.deps.SyncRepositories.Forward(w, r); err != nil {
		if r.Context().Err() != nil {
			return
		}
		switch {
		case errors.Is(err, syncproxy.ErrMalformed):
			handler.writeAPIError(w, r, http.StatusBadRequest, "malformed_request", "The sync request is malformed", err)
		case errors.Is(err, syncproxy.ErrBodyTooLarge):
			handler.writeAPIError(w, r, http.StatusRequestEntityTooLarge, "request_body_too_large", "The sync request body is too large", err)
		case errors.Is(err, syncproxy.ErrDisabled):
			handler.writeAPIError(w, r, http.StatusServiceUnavailable, "sync_disabled", "Realtime sync is not configured", err)
		default:
			handler.writeAPIError(w, r, http.StatusBadGateway, "sync_unavailable", "Realtime sync is unavailable", err)
		}
	}
}

func (handler *apiHandler) GetStars(w http.ResponseWriter, r *http.Request, params generated.GetStarsParams) {
	projection, err := handler.deps.Stars.Get(r.Context(), params.RepositoryUri)
	if err != nil {
		handler.writeError(w, r, err)
		return
	}
	data := make([]generated.Star, len(projection.Stars))
	for index, value := range projection.Stars {
		data[index] = projectedStarResponse(value)
	}
	writeJSON(w, http.StatusOK, generated.StarList{StarCount: projection.StarCount, Data: data})
}

func (handler *apiHandler) PutStar(w http.ResponseWriter, r *http.Request, params generated.PutStarParams) {
	identity, err := handler.requireSession(r, true)
	if err != nil {
		handler.writeError(w, r, err)
		return
	}
	value, err := handler.deps.Stars.Create(r.Context(), identity.accountDID, params.RepositoryUri)
	if err != nil {
		handler.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusAccepted, generated.StarMutation{Star: starEnvelopeResponse(value), Projected: false})
}

func (handler *apiHandler) DeleteStar(w http.ResponseWriter, r *http.Request, params generated.DeleteStarParams) {
	identity, err := handler.requireSession(r, true)
	if err != nil {
		handler.writeError(w, r, err)
		return
	}
	if err := handler.deps.Stars.Delete(r.Context(), identity.accountDID, params.RepositoryUri); err != nil {
		handler.writeError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusAccepted)
}

func (handler *apiHandler) GetIssues(w http.ResponseWriter, r *http.Request, params generated.GetIssuesParams) {
	projection, err := handler.deps.Issues.Get(r.Context(), params.RepositoryUri)
	if err != nil {
		handler.writeError(w, r, err)
		return
	}
	data := make([]projectedIssueJSON, len(projection.Issues))
	for index, value := range projection.Issues {
		data[index] = projectedIssueResponse(value)
	}
	writeJSON(w, http.StatusOK, issueListJSON{IssueCount: projection.IssueCount, OpenIssueCount: projection.OpenIssueCount, Data: data})
}

func (handler *apiHandler) CreateIssue(w http.ResponseWriter, r *http.Request, _ generated.CreateIssueParams) {
	identity, err := handler.requireSession(r, true)
	if err != nil {
		handler.writeError(w, r, err)
		return
	}
	var request generated.CreateIssueRequest
	if err := decodeJSON(w, r, &request); err != nil {
		handler.writeMalformed(w, r, err)
		return
	}
	value, err := handler.deps.Issues.Create(r.Context(), identity.accountDID, issue.CreateInput{
		RepositoryURI: request.RepositoryUri, Title: request.Title, Body: request.Body,
	})
	if err != nil {
		handler.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusAccepted, generated.IssueMutation{Issue: issueEnvelopeResponse(value), Projected: false})
}

func (handler *apiHandler) PutIssueStatus(w http.ResponseWriter, r *http.Request, _ generated.PutIssueStatusParams) {
	identity, err := handler.requireSession(r, true)
	if err != nil {
		handler.writeError(w, r, err)
		return
	}
	var request generated.PutIssueStatusRequest
	if err := decodeJSON(w, r, &request); err != nil {
		handler.writeMalformed(w, r, err)
		return
	}
	value, err := handler.deps.Issues.PutStatus(r.Context(), identity.accountDID, issue.StatusInput{IssueURI: request.IssueUri, State: issue.State(request.State)})
	if err != nil {
		handler.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusAccepted, generated.IssueStatusMutation{Status: issueStatusEnvelopeResponse(value), Projected: false})
}

func (handler *apiHandler) ListPullRequests(w http.ResponseWriter, r *http.Request, params generated.ListPullRequestsParams) {
	projection, err := handler.deps.PullRequests.List(r.Context(), params.RepositoryUri)
	if err != nil {
		handler.writeError(w, r, err)
		return
	}
	data := make([]generated.PullRequest, len(projection.PullRequests))
	for index, value := range projection.PullRequests {
		data[index] = projectedPullRequestResponse(value)
	}
	writeJSON(w, http.StatusOK, generated.PullRequestList{PullRequestCount: projection.PullRequestCount, OpenPullRequestCount: projection.OpenPullRequestCount, Data: data})
}

func (handler *apiHandler) GetPullRequest(w http.ResponseWriter, r *http.Request, params generated.GetPullRequestParams) {
	value, err := handler.deps.PullRequests.Get(r.Context(), params.PullRequestUri)
	if err != nil {
		handler.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, projectedPullRequestResponse(value))
}

func (handler *apiHandler) CreatePullRequest(w http.ResponseWriter, r *http.Request, _ generated.CreatePullRequestParams) {
	identity, err := handler.requireSession(r, true)
	if err != nil {
		handler.writeError(w, r, err)
		return
	}
	var request generated.CreatePullRequestRequest
	if err := decodeJSON(w, r, &request); err != nil {
		handler.writeMalformed(w, r, err)
		return
	}
	value, err := handler.deps.PullRequests.Create(r.Context(), identity.accountDID, pullrequest.CreateInput{
		SourceRepositoryURI: request.SourceRepositoryUri, TargetRepositoryURI: request.TargetRepositoryUri,
		SourceBranch: request.SourceBranch, TargetBranch: request.TargetBranch, HeadSHA: request.HeadSha, Title: request.Title, Body: request.Body,
	})
	if err != nil {
		handler.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusAccepted, generated.PullRequestMutation{PullRequest: pullRequestEnvelopeResponse(value), Projected: false})
}

func (handler *apiHandler) GetPullRequestDiff(w http.ResponseWriter, r *http.Request, params generated.GetPullRequestDiffParams) {
	result, err := handler.deps.PullRequests.Refresh(r.Context(), params.PullRequestUri)
	if err != nil {
		handler.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, generated.PullRequestDiff{MergeBase: result.MergeBase, HeadRef: result.HeadRef, Diff: diffResponse(result.Diff)})
}

func (handler *apiHandler) ListPullRequestReviews(w http.ResponseWriter, r *http.Request, params generated.ListPullRequestReviewsParams) {
	values, err := handler.deps.PullRequests.Reviews(r.Context(), params.PullRequestUri)
	if err != nil {
		handler.writeError(w, r, err)
		return
	}
	data := make([]generated.PullRequestReview, len(values))
	for index, value := range values {
		data[index] = projectedPullRequestReviewResponse(value)
	}
	writeJSON(w, http.StatusOK, generated.PullRequestReviewList{Data: data})
}

func (handler *apiHandler) CreatePullRequestReview(w http.ResponseWriter, r *http.Request, _ generated.CreatePullRequestReviewParams) {
	identity, err := handler.requireSession(r, true)
	if err != nil {
		handler.writeError(w, r, err)
		return
	}
	var request generated.CreatePullRequestReviewRequest
	if err := decodeJSON(w, r, &request); err != nil {
		handler.writeMalformed(w, r, err)
		return
	}
	value, err := handler.deps.PullRequests.CreateReview(r.Context(), identity.accountDID, pullrequest.ReviewInput{PullRequestURI: request.PullRequestUri, Verdict: pullrequest.Verdict(request.Verdict), Body: request.Body})
	if err != nil {
		handler.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusAccepted, generated.PullRequestReviewMutation{Review: pullRequestReviewEnvelopeResponse(value), Projected: false})
}

func (handler *apiHandler) PutPullRequestStatus(w http.ResponseWriter, r *http.Request, _ generated.PutPullRequestStatusParams) {
	identity, err := handler.requireSession(r, true)
	if err != nil {
		handler.writeError(w, r, err)
		return
	}
	var request generated.PutPullRequestStatusRequest
	if err := decodeJSON(w, r, &request); err != nil {
		handler.writeMalformed(w, r, err)
		return
	}
	if request.State != generated.PutPullRequestStatusRequestState("open") && request.State != generated.PutPullRequestStatusRequestState("closed") {
		handler.writeError(w, r, &pullrequest.ValidationError{Field: "state", Problem: "must be open or closed"})
		return
	}
	value, err := handler.deps.PullRequests.PutStatus(r.Context(), identity.accountDID, pullrequest.StatusInput{PullRequestURI: request.PullRequestUri, State: pullrequest.State(request.State)})
	if err != nil {
		handler.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusAccepted, generated.PullRequestStatusMutation{Status: pullRequestStatusEnvelopeResponse(value), Projected: false})
}

func (handler *apiHandler) MergePullRequest(w http.ResponseWriter, r *http.Request, _ generated.MergePullRequestParams) {
	identity, err := handler.requireSession(r, true)
	if err != nil {
		handler.writeError(w, r, err)
		return
	}
	var request generated.MergePullRequestRequest
	if err := decodeJSON(w, r, &request); err != nil {
		handler.writeMalformed(w, r, err)
		return
	}
	if request.Strategy != generated.MergePullRequestRequestStrategyMergeCommit && request.Strategy != generated.MergePullRequestRequestStrategySquash {
		handler.writeError(w, r, &pullrequest.ValidationError{Field: "strategy", Problem: "must be merge-commit or squash"})
		return
	}
	result, err := handler.deps.PullRequests.Merge(r.Context(), identity.accountDID, pullrequest.MergeInput{
		PullRequestURI: request.PullRequestUri, Strategy: gitservice.MergeStrategy(request.Strategy),
	})
	if err != nil {
		handler.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, generated.PullRequestMerge{
		MergeCommitSha: result.Git.NewSHA, OldSha: result.Git.OldSHA, HeadSha: result.Git.HeadSHA,
		TargetRef: result.Git.TargetRef, Strategy: generated.PullRequestMergeStrategy(result.Git.Strategy),
		Status: pullRequestStatusEnvelopeResponse(result.Status),
	})
}

func (handler *apiHandler) GetIssueComments(w http.ResponseWriter, r *http.Request, params generated.GetIssueCommentsParams) {
	viewerDID, err := handler.optionalSessionViewer(r)
	if err != nil {
		handler.writeError(w, r, err)
		return
	}
	projection, err := handler.deps.Comments.Get(r.Context(), params.IssueUri, viewerDID)
	if err != nil {
		handler.writeError(w, r, err)
		return
	}
	data := make([]generated.Comment, len(projection.Comments))
	for index, value := range projection.Comments {
		data[index] = projectedCommentResponse(value)
	}
	writeJSON(w, http.StatusOK, generated.CommentList{CommentCount: projection.CommentCount, Data: data})
}

func (handler *apiHandler) CreateIssueComment(w http.ResponseWriter, r *http.Request, _ generated.CreateIssueCommentParams) {
	identity, err := handler.requireSession(r, true)
	if err != nil {
		handler.writeError(w, r, err)
		return
	}
	var request generated.CreateCommentRequest
	if err := decodeJSON(w, r, &request); err != nil {
		handler.writeMalformed(w, r, err)
		return
	}
	value, err := handler.deps.Comments.Create(r.Context(), identity.accountDID, comment.CreateInput{
		IssueURI: request.IssueUri, ParentURI: optionalString(request.ParentUri), Body: request.Body,
	})
	if err != nil {
		handler.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusAccepted, generated.CommentMutation{Comment: commentEnvelopeResponse(value), Projected: false})
}

func (handler *apiHandler) DeleteIssueComment(w http.ResponseWriter, r *http.Request, params generated.DeleteIssueCommentParams) {
	identity, err := handler.requireSession(r, true)
	if err != nil {
		handler.writeError(w, r, err)
		return
	}
	if err := handler.deps.Comments.Delete(r.Context(), identity.accountDID, params.CommentUri); err != nil {
		handler.writeError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusAccepted)
}

func (handler *apiHandler) GetModeration(w http.ResponseWriter, r *http.Request) {
	identity, err := handler.requireSession(r, false)
	if err != nil {
		handler.writeError(w, r, err)
		return
	}
	blocks, err := handler.deps.Moderation.ListBlocks(r.Context(), identity.accountDID)
	if err != nil {
		handler.writeError(w, r, err)
		return
	}
	hidden, err := handler.deps.Moderation.ListHidden(r.Context(), identity.accountDID)
	if err != nil {
		handler.writeError(w, r, err)
		return
	}
	blockedDIDs := make([]string, len(blocks))
	for index, value := range blocks {
		blockedDIDs[index] = value.DID
	}
	hiddenRecords := make([]string, len(hidden))
	for index, value := range hidden {
		hiddenRecords[index] = value.URI
	}
	writeJSON(w, http.StatusOK, generated.Moderation{BlockedDids: blockedDIDs, HiddenRecords: hiddenRecords})
}

func (handler *apiHandler) PutBlockedDID(w http.ResponseWriter, r *http.Request, _ generated.PutBlockedDIDParams) {
	identity, err := handler.requireSession(r, true)
	if err != nil {
		handler.writeError(w, r, err)
		return
	}
	var request generated.PutBlockedDIDRequest
	if err := decodeJSON(w, r, &request); err != nil {
		handler.writeMalformed(w, r, err)
		return
	}
	if err := handler.deps.Moderation.Block(r.Context(), identity.accountDID, request.BlockedDid); err != nil {
		handler.writeError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (handler *apiHandler) DeleteBlockedDID(w http.ResponseWriter, r *http.Request, params generated.DeleteBlockedDIDParams) {
	identity, err := handler.requireSession(r, true)
	if err != nil {
		handler.writeError(w, r, err)
		return
	}
	if err := handler.deps.Moderation.Unblock(r.Context(), identity.accountDID, params.BlockedDid); err != nil {
		handler.writeError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (handler *apiHandler) PutHiddenRecord(w http.ResponseWriter, r *http.Request, _ generated.PutHiddenRecordParams) {
	identity, err := handler.requireSession(r, true)
	if err != nil {
		handler.writeError(w, r, err)
		return
	}
	var request generated.PutHiddenRecordRequest
	if err := decodeJSON(w, r, &request); err != nil {
		handler.writeMalformed(w, r, err)
		return
	}
	if err := handler.deps.Moderation.Hide(r.Context(), identity.accountDID, request.RecordUri); err != nil {
		handler.writeError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (handler *apiHandler) DeleteHiddenRecord(w http.ResponseWriter, r *http.Request, params generated.DeleteHiddenRecordParams) {
	identity, err := handler.requireSession(r, true)
	if err != nil {
		handler.writeError(w, r, err)
		return
	}
	if err := handler.deps.Moderation.Unhide(r.Context(), identity.accountDID, params.RecordUri); err != nil {
		handler.writeError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (handler *apiHandler) ListRepositoryBranches(w http.ResponseWriter, r *http.Request, owner string, slug generated.RepositorySlug) {
	repo, err := handler.readableRepository(r, owner, slug)
	if err != nil {
		handler.writeError(w, r, err)
		return
	}
	branches, err := handler.deps.Git.Branches(r.Context(), repo.ID, repo.DefaultBranch)
	if err != nil {
		handler.writeError(w, r, err)
		return
	}
	data := make([]generated.Branch, len(branches))
	for index, branch := range branches {
		data[index] = generated.Branch{Name: branch.Name, Sha: branch.SHA, Default: branch.Default}
	}
	writeJSON(w, http.StatusOK, generated.BranchList{Data: data})
}

func (handler *apiHandler) ListRepositoryTags(w http.ResponseWriter, r *http.Request, owner string, slug generated.RepositorySlug) {
	repo, err := handler.readableRepository(r, owner, slug)
	if err != nil {
		handler.writeError(w, r, err)
		return
	}
	tags, err := handler.deps.Git.Tags(r.Context(), repo.ID)
	if err != nil {
		handler.writeError(w, r, err)
		return
	}
	data := make([]generated.Tag, len(tags))
	for index, tag := range tags {
		data[index] = generated.Tag{Name: tag.Name, Sha: tag.SHA, ObjectType: tag.ObjectType, TargetSha: tag.PeeledSHA, TargetType: tag.PeeledType}
	}
	writeJSON(w, http.StatusOK, generated.TagList{Data: data})
}

func (handler *apiHandler) GetRepositoryTree(w http.ResponseWriter, r *http.Request, owner string, slug generated.RepositorySlug, params generated.GetRepositoryTreeParams) {
	repo, err := handler.readableRepository(r, owner, slug)
	if err != nil {
		handler.writeError(w, r, err)
		return
	}
	revision := repo.DefaultBranch
	if params.Rev != nil {
		revision = *params.Rev
	}
	treePath := ""
	if params.Path != nil {
		treePath = *params.Path
	}
	tree, err := handler.deps.Git.Tree(r.Context(), repo.ID, revision, treePath)
	if err != nil {
		handler.writeError(w, r, err)
		return
	}
	entries := make([]generated.TreeEntry, len(tree.Entries))
	for index, entry := range tree.Entries {
		var size *int64
		if entry.Size >= 0 {
			value := entry.Size
			size = &value
		}
		entries[index] = generated.TreeEntry{Name: entry.Name, Path: entry.Path, Mode: entry.Mode,
			Type: generated.TreeEntryType(entry.Type), Sha: entry.SHA, Size: size}
	}
	writeJSON(w, http.StatusOK, generated.Tree{Revision: revision, CommitSha: tree.CommitSHA, Path: tree.Path, Entries: entries})
}

func (handler *apiHandler) GetRepositoryBlob(w http.ResponseWriter, r *http.Request, owner string, slug generated.RepositorySlug, sha string) {
	repo, err := handler.readableRepository(r, owner, slug)
	if err != nil {
		handler.writeError(w, r, err)
		return
	}
	metadata, err := handler.deps.Git.BlobMetadata(r.Context(), repo.ID, sha)
	if err != nil {
		handler.writeError(w, r, err)
		return
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Length", strconv.FormatInt(metadata.Size, 10))
	w.Header().Set("ETag", `"`+metadata.SHA+`"`)
	if repo.Visibility == repository.VisibilityPublic {
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	} else {
		w.Header().Set("Cache-Control", "private, max-age=31536000, immutable")
	}
	w.WriteHeader(http.StatusOK)
	if err := handler.deps.Git.StreamBlob(r.Context(), repo.ID, sha, w); err != nil {
		handler.logger.ErrorContext(r.Context(), "stream repository blob", "request_id", requestIDFromContext(r.Context()), "error", err)
		trace.SpanFromContext(r.Context()).RecordError(err)
	}
}

func (handler *apiHandler) ListRepositoryCommits(w http.ResponseWriter, r *http.Request, owner string, slug generated.RepositorySlug, params generated.ListRepositoryCommitsParams) {
	repo, err := handler.readableRepository(r, owner, slug)
	if err != nil {
		handler.writeError(w, r, err)
		return
	}
	ref := repo.DefaultBranch
	if params.Ref != nil {
		ref = *params.Ref
	}
	limit := 30
	if params.Limit != nil {
		limit = *params.Limit
	}
	commits, err := handler.deps.Git.Commits(r.Context(), repo.ID, ref, limit)
	if err != nil {
		handler.writeError(w, r, err)
		return
	}
	data := make([]generated.CommitSummary, len(commits))
	for index, commit := range commits {
		data[index] = commitSummaryResponse(commit)
	}
	writeJSON(w, http.StatusOK, generated.CommitList{Data: data})
}

func (handler *apiHandler) GetRepositoryCommit(w http.ResponseWriter, r *http.Request, owner string, slug generated.RepositorySlug, revision string) {
	repo, err := handler.readableRepository(r, owner, slug)
	if err != nil {
		handler.writeError(w, r, err)
		return
	}
	commit, err := handler.deps.Git.Commit(r.Context(), repo.ID, revision)
	if err != nil {
		handler.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, commitResponse(commit))
}

func (handler *apiHandler) GetRepositoryDiff(w http.ResponseWriter, r *http.Request, owner string, slug generated.RepositorySlug, params generated.GetRepositoryDiffParams) {
	repo, err := handler.readableRepository(r, owner, slug)
	if err != nil {
		handler.writeError(w, r, err)
		return
	}
	diff, err := handler.deps.Git.Diff(r.Context(), repo.ID, params.Base, params.Head)
	if err != nil {
		handler.writeError(w, r, err)
		return
	}
	files := make([]generated.DiffFile, len(diff.Files))
	for index, file := range diff.Files {
		files[index] = generated.DiffFile{Status: file.Status, OldPath: file.OldPath, NewPath: file.NewPath,
			Additions: file.Additions, Deletions: file.Deletions}
	}
	writeJSON(w, http.StatusOK, generated.Diff{BaseSha: diff.BaseSHA, HeadSha: diff.HeadSHA, Files: files, Patch: diff.Patch})
}

func (handler *apiHandler) GetRepositoryMergeBase(w http.ResponseWriter, r *http.Request, owner string, slug generated.RepositorySlug, params generated.GetRepositoryMergeBaseParams) {
	repo, err := handler.readableRepository(r, owner, slug)
	if err != nil {
		handler.writeError(w, r, err)
		return
	}
	sha, err := handler.deps.Git.MergeBase(r.Context(), repo.ID, params.A, params.B)
	if err != nil {
		handler.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, generated.MergeBase{Sha: sha})
}

func (handler *apiHandler) readableRepository(r *http.Request, owner, slug string) (repository.Repository, error) {
	repo, err := handler.deps.Repositories.GetByOwnerSlug(r.Context(), owner, slug)
	if err != nil || repo.State != repository.StateActive {
		return repository.Repository{}, repository.ErrNotFound
	}
	if repo.Visibility == repository.VisibilityPublic {
		return repo, nil
	}
	identity, err := handler.authenticate(r)
	if err != nil {
		return repository.Repository{}, err
	}
	if !identity.session {
		if (!slices.Contains(identity.scopes, auth.ScopeRepositoryRead) && !slices.Contains(identity.scopes, auth.ScopeRepositoryWrite)) ||
			(identity.repositoryID != nil && *identity.repositoryID != repo.ID) {
			return repository.Repository{}, repository.ErrNotFound
		}
	}
	allowed, err := handler.deps.Authorization.CanReadRepository(r.Context(), identity.accountDID, repo.ID)
	if err != nil {
		return repository.Repository{}, fmt.Errorf("authorize repository read: %w", err)
	}
	if !allowed {
		return repository.Repository{}, repository.ErrNotFound
	}
	return repo, nil
}

func (handler *apiHandler) authenticate(r *http.Request) (principal, error) {
	if cookie, err := r.Cookie("adenosine_session"); err == nil {
		if handler.deps.Sessions == nil {
			return principal{}, auth.ErrUnauthorized
		}
		identity, err := handler.deps.Sessions.Authenticate(r.Context(), cookie.Value)
		if err != nil {
			return principal{}, err
		}
		return principal{accountDID: identity.AccountDID, sessionID: identity.SessionID, session: true}, nil
	}
	plaintext, ok := bearerToken(r)
	if !ok || handler.deps.TokenAuth == nil {
		return principal{}, auth.ErrUnauthorized
	}
	token, err := handler.deps.TokenAuth.Authenticate(r.Context(), plaintext)
	if err != nil {
		return principal{}, err
	}
	return principal{accountDID: token.AccountDID, scopes: token.Scopes, repositoryID: token.RepositoryID}, nil
}

func (handler *apiHandler) optionalSessionViewer(r *http.Request) (string, error) {
	cookie, err := r.Cookie("adenosine_session")
	if errors.Is(err, http.ErrNoCookie) {
		return "", nil
	}
	if err != nil || handler.deps.Sessions == nil {
		return "", auth.ErrUnauthorized
	}
	identity, err := handler.deps.Sessions.Authenticate(r.Context(), cookie.Value)
	if err != nil {
		return "", err
	}
	return identity.AccountDID, nil
}

func (handler *apiHandler) requireSession(r *http.Request, mutation bool) (principal, error) {
	identity, err := handler.authenticate(r)
	if err != nil {
		return principal{}, err
	}
	if !identity.session {
		return principal{}, auth.ErrForbidden
	}
	if mutation && !handler.validOrigin(r) {
		return principal{}, auth.ErrForbidden
	}
	return identity, nil
}

func (handler *apiHandler) validOrigin(r *http.Request) bool {
	return r.Header.Get("Origin") == handler.origin
}

func bearerToken(r *http.Request) (string, bool) {
	values := r.Header.Values("Authorization")
	if len(values) != 1 || !strings.HasPrefix(values[0], "Bearer ") {
		return "", false
	}
	value := strings.TrimPrefix(values[0], "Bearer ")
	return value, value != "" && !strings.ContainsAny(value, " \t\r\n")
}

func decodeJSON(w http.ResponseWriter, r *http.Request, destination any) error {
	return decodeJSONLimit(w, r, destination, maxJSONBody)
}

func decodeJSONLimit(w http.ResponseWriter, r *http.Request, destination any, limit int64) error {
	if !strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
		return fmt.Errorf("content type must be application/json")
	}
	r.Body = http.MaxBytesReader(w, r.Body, limit)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return fmt.Errorf("request body must contain one JSON value")
	}
	return nil
}

type passkeyVerificationRequest struct {
	CeremonyToken string          `json:"ceremony_token"`
	Response      json.RawMessage `json:"response"`
}

type passkeyOptionsResponse struct {
	Options       json.RawMessage `json:"options"`
	CeremonyToken string          `json:"ceremony_token"`
}

func decodePasskeyVerification(w http.ResponseWriter, r *http.Request) (passkeyVerificationRequest, error) {
	var request passkeyVerificationRequest
	if err := decodeJSONLimit(w, r, &request, maxWebAuthnVerifyBody); err != nil {
		return passkeyVerificationRequest{}, err
	}
	var object map[string]json.RawMessage
	if len(request.Response) == 0 || json.Unmarshal(request.Response, &object) != nil || object == nil {
		return passkeyVerificationRequest{}, errors.New("response must be a JSON object")
	}
	return request, nil
}

func (handler *apiHandler) writeMalformed(w http.ResponseWriter, r *http.Request, err error) {
	handler.writeAPIError(w, r, http.StatusBadRequest, "malformed_request", "The request is malformed", err)
}

func (handler *apiHandler) writeError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, auth.ErrUnauthorized):
		handler.writeAPIError(w, r, http.StatusUnauthorized, "authentication_required", "Authentication is required", err)
	case errors.Is(err, auth.ErrForbidden):
		handler.writeAPIError(w, r, http.StatusForbidden, "permission_denied", "Permission denied", err)
	case errors.Is(err, auth.ErrNotFound), errors.Is(err, repository.ErrNotFound):
		handler.writeAPIError(w, r, http.StatusNotFound, "not_found", "The requested resource was not found", err)
	case errors.Is(err, auth.ErrConflict), errors.Is(err, repository.ErrAlreadyExists):
		handler.writeAPIError(w, r, http.StatusConflict, "conflict", "The request conflicts with existing state", err)
	case errors.Is(err, auth.ErrValidation), errors.Is(err, repository.ErrValidation):
		handler.writeAPIError(w, r, http.StatusUnprocessableEntity, "validation_failed", "The request is invalid", err)
	case errors.Is(err, profile.ErrProvider):
		handler.writeAPIError(w, r, http.StatusBadGateway, "profile_provider_unavailable", "The profile provider is unavailable", err)
	case errors.Is(err, profile.ErrAuthorization):
		handler.writeAPIError(w, r, http.StatusConflict, "atproto_authorization_required", "AT Protocol authorization is required", err)
	case errors.Is(err, profile.ErrValidation):
		handler.writeAPIError(w, r, http.StatusUnprocessableEntity, "validation_failed", "The profile is invalid", err)
	case errors.Is(err, profile.ErrNotFound):
		handler.writeAPIError(w, r, http.StatusNotFound, "not_found", "The requested profile was not found", err)
	case errors.Is(err, star.ErrNotFound):
		handler.writeAPIError(w, r, http.StatusNotFound, "not_found", "The requested repository was not found", err)
	case errors.Is(err, star.ErrValidation):
		handler.writeAPIError(w, r, http.StatusUnprocessableEntity, "validation_failed", "The star request is invalid", err)
	case errors.Is(err, star.ErrAuthorization):
		handler.writeAPIError(w, r, http.StatusConflict, "atproto_authorization_required", "AT Protocol authorization is required", err)
	case errors.Is(err, star.ErrConflict):
		handler.writeAPIError(w, r, http.StatusConflict, "star_conflict", "The star conflicts with existing state", err)
	case errors.Is(err, star.ErrProvider):
		handler.writeAPIError(w, r, http.StatusBadGateway, "star_provider_unavailable", "The star provider is unavailable", err)
	case errors.Is(err, issue.ErrNotFound):
		handler.writeAPIError(w, r, http.StatusNotFound, "not_found", "The requested issue or repository was not found", err)
	case errors.Is(err, issue.ErrValidation):
		handler.writeAPIError(w, r, http.StatusUnprocessableEntity, "validation_failed", "The issue request is invalid", err)
	case errors.Is(err, issue.ErrAuthorization):
		handler.writeAPIError(w, r, http.StatusConflict, "atproto_authorization_required", "AT Protocol authorization is required", err)
	case errors.Is(err, issue.ErrConflict):
		handler.writeAPIError(w, r, http.StatusConflict, "issue_conflict", "The issue conflicts with existing state", err)
	case errors.Is(err, issue.ErrProvider):
		handler.writeAPIError(w, r, http.StatusBadGateway, "issue_provider_unavailable", "The issue provider is unavailable", err)
	case errors.Is(err, pullrequest.ErrNotFound):
		handler.writeAPIError(w, r, http.StatusNotFound, "not_found", "The requested pull request or repository was not found", err)
	case errors.Is(err, pullrequest.ErrValidation):
		handler.writeAPIError(w, r, http.StatusUnprocessableEntity, "validation_failed", "The pull request is invalid", err)
	case errors.Is(err, pullrequest.ErrPermissionDenied):
		handler.writeAPIError(w, r, http.StatusForbidden, "permission_denied", "Permission denied", err)
	case errors.Is(err, pullrequest.ErrAuthorization):
		handler.writeAPIError(w, r, http.StatusConflict, "atproto_authorization_required", "AT Protocol authorization is required", err)
	case errors.Is(err, pullrequest.ErrConflict), errors.Is(err, pullrequest.ErrProjectionChanged):
		handler.writeAPIError(w, r, http.StatusConflict, "pull_request_conflict", "The pull request conflicts with existing state", err)
	case errors.Is(err, pullrequest.ErrProvider):
		handler.writeAPIError(w, r, http.StatusBadGateway, "pull_request_provider_unavailable", "The pull request provider is unavailable", err)
	case errors.Is(err, gitservice.ErrInvalidInput):
		handler.writeAPIError(w, r, http.StatusBadRequest, "malformed_request", "The Git revision, path, or object ID is invalid", err)
	case errors.Is(err, gitservice.ErrObjectNotFound):
		handler.writeAPIError(w, r, http.StatusNotFound, "not_found", "The requested Git object was not found", err)
	case errors.Is(err, gitservice.ErrUnsupportedObject):
		handler.writeAPIError(w, r, http.StatusUnprocessableEntity, "unsupported_git_object", "The Git object type is unsupported for this operation", err)
	case errors.Is(err, gitservice.ErrOutputLimit):
		handler.writeAPIError(w, r, http.StatusRequestEntityTooLarge, "git_output_too_large", "The repository output exceeds the supported limit", err)
	case errors.Is(err, localatproto.ErrInvalidIdentifier):
		handler.writeAPIError(w, r, http.StatusUnprocessableEntity, "invalid_atproto_identifier", "The AT Protocol handle or DID is invalid", err)
	case errors.Is(err, localatproto.ErrProviderFailure):
		handler.writeAPIError(w, r, http.StatusBadGateway, "oauth_provider_unavailable", "The AT Protocol OAuth provider is unavailable", err)
	case errors.Is(err, localatproto.ErrCallbackFailure):
		handler.writeAPIError(w, r, http.StatusBadRequest, "oauth_callback_failed", "The AT Protocol OAuth callback could not be verified", err)
	default:
		handler.writeAPIError(w, r, http.StatusInternalServerError, "internal_error", "An internal error occurred", err)
	}
}

func (handler *apiHandler) writeAPIError(w http.ResponseWriter, r *http.Request, status int, code, message string, err error) {
	if status >= 500 {
		handler.logger.ErrorContext(r.Context(), "REST request failed", "request_id", requestIDFromContext(r.Context()), "error", err)
	}
	writeJSON(w, status, generated.ErrorResponse{Error: generated.Error{
		Code: code, Message: message, RequestId: requestIDFromContext(r.Context()),
	}})
}

func accessTokenResponse(token auth.AccessToken) generated.AccessToken {
	scopes := make([]generated.AccessTokenScopes, len(token.Scopes))
	for index, scope := range token.Scopes {
		scopes[index] = generated.AccessTokenScopes(scope)
	}
	var repositoryID *openapi_types.UUID
	if token.RepositoryID != nil {
		value := openapi_types.UUID(*token.RepositoryID)
		repositoryID = &value
	}
	return generated.AccessToken{Id: openapi_types.UUID(token.ID), Name: token.Name, Prefix: token.Prefix,
		Scopes: scopes, RepositoryId: repositoryID, CreatedAt: token.CreatedAt,
		ExpiresAt: token.ExpiresAt, LastUsedAt: token.LastUsedAt}
}

func createdAccessTokenResponse(token auth.AccessToken, plaintext string) generated.CreatedAccessToken {
	scopes := make([]generated.CreatedAccessTokenScopes, len(token.Scopes))
	for index, scope := range token.Scopes {
		scopes[index] = generated.CreatedAccessTokenScopes(scope)
	}
	var repositoryID *openapi_types.UUID
	if token.RepositoryID != nil {
		value := openapi_types.UUID(*token.RepositoryID)
		repositoryID = &value
	}
	return generated.CreatedAccessToken{Id: openapi_types.UUID(token.ID), Name: token.Name, Prefix: token.Prefix,
		Token: plaintext, Scopes: scopes, RepositoryId: repositoryID, CreatedAt: token.CreatedAt,
		ExpiresAt: token.ExpiresAt, LastUsedAt: token.LastUsedAt}
}

func sshKeyResponse(key auth.SSHKey) generated.SSHKey {
	return generated.SSHKey{Id: openapi_types.UUID(key.ID), Name: key.Name, Algorithm: key.Algorithm,
		PublicKey: key.PublicKey, Fingerprint: key.Fingerprint, CreatedAt: key.CreatedAt, LastUsedAt: key.LastUsedAt}
}

func passkeyResponse(credential passkey.CredentialSummary) generated.Passkey {
	return generated.Passkey{Id: openapi_types.UUID(credential.ID), Name: credential.Name, CreatedAt: credential.CreatedAt, LastUsedAt: credential.LastUsedAt}
}

func developerProfileResponse(value profile.Profile) generated.DeveloperProfile {
	var recordCreatedAt *time.Time
	if !value.RecordCreatedAt.IsZero() {
		recordCreatedAt = &value.RecordCreatedAt
	}
	return generated.DeveloperProfile{
		Did: value.DID, Uri: pointerUnlessEmpty(value.URI), Cid: pointerUnlessEmpty(value.CID),
		Handle: pointerUnlessEmpty(value.Handle), DisplayName: pointerUnlessEmpty(value.DisplayName),
		Bio: pointerUnlessEmpty(value.Bio), AvatarRef: pointerUnlessEmpty(value.AvatarRef),
		Website: pointerUnlessEmpty(value.Website), Location: pointerUnlessEmpty(value.Location),
		RepositoryCount: value.RepositoryCount, ContributionCount: value.ContributionCount,
		RecordCreatedAt: recordCreatedAt, IndexedAt: value.IndexedAt,
	}
}

func commitIdentityResponse(identity gitservice.CommitIdentity) generated.CommitIdentity {
	return generated.CommitIdentity{Name: identity.Name, Email: identity.Email, Date: identity.Time}
}

func commitSummaryResponse(commit gitservice.CommitSummary) generated.CommitSummary {
	return generated.CommitSummary{Sha: commit.SHA, Parents: commit.Parents, Summary: commit.Summary,
		Author: commitIdentityResponse(commit.Author), Committer: commitIdentityResponse(commit.Committer)}
}

func commitResponse(commit gitservice.Commit) generated.Commit {
	return generated.Commit{Sha: commit.SHA, Parents: commit.Parents, Summary: commit.Summary, Message: commit.Message,
		Author: commitIdentityResponse(commit.Author), Committer: commitIdentityResponse(commit.Committer)}
}

func diffResponse(diff gitservice.Diff) generated.Diff {
	files := make([]generated.DiffFile, len(diff.Files))
	for index, file := range diff.Files {
		files[index] = generated.DiffFile{Status: file.Status, OldPath: file.OldPath, NewPath: file.NewPath, Additions: file.Additions, Deletions: file.Deletions}
	}
	return generated.Diff{BaseSha: diff.BaseSHA, HeadSha: diff.HeadSHA, Files: files, Patch: diff.Patch}
}

func (handler *apiHandler) repositoryResponse(repo repository.Repository) generated.Repository {
	webURL := handler.baseURL + "/" + repo.OwnerDID + "/" + repo.Slug
	gitHTTPSURL := webURL + ".git"
	gitSSHURL := ""
	if handler.deps.Endpoints != nil {
		webURL, gitHTTPSURL, gitSSHURL = handler.deps.Endpoints.For(repo)
	}
	id := openapi_types.UUID(repo.ID)
	return generated.Repository{
		Id: &id, Uri: pointerUnlessEmpty(repo.ATURI), Cid: pointerUnlessEmpty(repo.ATCID), Slug: repo.Slug, DisplayName: pointerUnlessEmpty(repo.DisplayName),
		Description: pointerUnlessEmpty(repo.Description), Visibility: generated.RepositoryVisibility(repo.Visibility),
		State: generated.RepositoryState(repo.State), DefaultBranch: repo.DefaultBranch,
		Owner: generated.RepositoryOwner{Did: repo.OwnerDID}, CreatedAt: repo.CreatedAt, UpdatedAt: repo.UpdatedAt,
		Hosting: generated.RepositoryHosting{Local: true, WebUrl: webURL, GitHttpsUrl: gitHTTPSURL, GitSshUrl: pointerUnlessEmpty(gitSSHURL)},
	}
}

func networkRepositoryResponse(repo federation.DiscoveryRepository) generated.Repository {
	var id *openapi_types.UUID
	if repo.LocalRepositoryID != nil {
		value := openapi_types.UUID(*repo.LocalRepositoryID)
		id = &value
	}
	return generated.Repository{
		Id: id, Uri: &repo.URI, Cid: pointerUnlessEmpty(repo.CID), Slug: repo.Slug, DisplayName: pointerUnlessEmpty(repo.Name),
		Description: pointerUnlessEmpty(repo.Description), Visibility: generated.RepositoryVisibilityPublic,
		State: generated.Active, DefaultBranch: repo.DefaultBranch,
		Owner:     generated.RepositoryOwner{Did: repo.OwnerDID, Handle: pointerUnlessEmpty(repo.OwnerHandle)},
		StarCount: repo.StarCount, IssueCount: repo.IssueCount, OpenIssueCount: repo.OpenIssueCount,
		Hosting: generated.RepositoryHosting{Local: repo.LocalRepositoryID != nil, WebUrl: repo.Web,
			GitHttpsUrl: repo.GitHTTPS, GitSshUrl: pointerUnlessEmpty(repo.GitSSH)},
		CreatedAt: repo.CreatedAt, UpdatedAt: repo.UpdatedAt,
	}
}

func projectedStarResponse(value star.Star) generated.Star {
	return generated.Star{
		Uri: value.URI, Cid: value.CID, AuthorDid: value.AuthorDID,
		RepositoryUri: value.Target.URI, RepositoryCid: value.Target.CID,
		CreatedAt: value.CreatedAt, IndexedAt: value.IndexedAt,
	}
}

func starEnvelopeResponse(value star.Star) generated.StarEnvelope {
	return generated.StarEnvelope{
		Uri: value.URI, Cid: value.CID, AuthorDid: value.AuthorDID,
		RepositoryUri: value.Target.URI, RepositoryCid: value.Target.CID, CreatedAt: value.CreatedAt,
	}
}

type issueListJSON struct {
	IssueCount     int64                `json:"issue_count"`
	OpenIssueCount int64                `json:"open_issue_count"`
	Data           []projectedIssueJSON `json:"data"`
}

type projectedIssueJSON struct {
	URI           string               `json:"uri"`
	CID           string               `json:"cid"`
	AuthorDID     string               `json:"author_did"`
	RepositoryURI string               `json:"repository_uri"`
	RepositoryCID string               `json:"repository_cid"`
	Title         string               `json:"title"`
	Body          string               `json:"body"`
	State         generated.IssueState `json:"state"`
	StatusURI     *string              `json:"status_uri"`
	StatusCID     *string              `json:"status_cid"`
	CommentCount  int64                `json:"comment_count"`
	CreatedAt     time.Time            `json:"created_at"`
	UpdatedAt     time.Time            `json:"updated_at"`
	IndexedAt     time.Time            `json:"indexed_at"`
}

func projectedIssueResponse(value issue.ProjectedIssue) projectedIssueJSON {
	return projectedIssueJSON{
		URI: value.URI, CID: value.CID, AuthorDID: value.AuthorDID,
		RepositoryURI: value.Repository.URI, RepositoryCID: value.Repository.CID,
		Title: value.Title, Body: value.Body, State: generated.IssueState(value.State),
		StatusURI: pointerUnlessEmpty(value.Status.URI), StatusCID: pointerUnlessEmpty(value.Status.CID),
		CreatedAt: value.CreatedAt, UpdatedAt: value.UpdatedAt, IndexedAt: value.IndexedAt,
	}
}

func issueEnvelopeResponse(value issue.Issue) generated.IssueEnvelope {
	return generated.IssueEnvelope{
		Uri: value.URI, Cid: value.CID, AuthorDid: value.AuthorDID,
		RepositoryUri: value.Repository.URI, RepositoryCid: value.Repository.CID,
		Title: value.Title, Body: value.Body, CreatedAt: value.CreatedAt, UpdatedAt: value.UpdatedAt,
	}
}

func issueStatusEnvelopeResponse(value issue.Status) generated.IssueStatusEnvelope {
	return generated.IssueStatusEnvelope{
		Uri: value.URI, Cid: value.CID, AuthorDid: value.AuthorDID,
		IssueUri: value.Subject.URI, IssueCid: value.Subject.CID,
		RepositoryUri: value.Repository.URI, RepositoryCid: value.Repository.CID,
		State: generated.IssueStatusEnvelopeState(value.State), CreatedAt: value.CreatedAt, UpdatedAt: value.UpdatedAt,
	}
}

func projectedPullRequestResponse(value pullrequest.ProjectedPullRequest) generated.PullRequest {
	return generated.PullRequest{Uri: value.URI, Cid: value.CID, AuthorDid: value.AuthorDID,
		SourceRepositoryUri: value.SourceRepository.URI, SourceRepositoryCid: value.SourceRepository.CID, SourceBranch: value.SourceBranch,
		TargetRepositoryUri: value.TargetRepository.URI, TargetRepositoryCid: value.TargetRepository.CID, TargetBranch: value.TargetBranch,
		HeadSha: value.HeadSHA, Title: value.Title, Body: value.Body, State: generated.PullRequestState(value.State),
		StatusUri: pointerUnlessEmpty(value.Status.URI), StatusCid: pointerUnlessEmpty(value.Status.CID), MergedCommitSha: pointerUnlessEmpty(value.MergedCommitSHA),
		ReviewCount: value.ReviewCount, CreatedAt: value.CreatedAt, UpdatedAt: value.UpdatedAt, IndexedAt: value.IndexedAt}
}

func pullRequestEnvelopeResponse(value pullrequest.PullRequest) generated.PullRequestEnvelope {
	return generated.PullRequestEnvelope{Uri: value.URI, Cid: value.CID, AuthorDid: value.AuthorDID,
		SourceRepositoryUri: value.SourceRepository.URI, SourceRepositoryCid: value.SourceRepository.CID, SourceBranch: value.SourceBranch,
		TargetRepositoryUri: value.TargetRepository.URI, TargetRepositoryCid: value.TargetRepository.CID, TargetBranch: value.TargetBranch,
		HeadSha: value.HeadSHA, Title: value.Title, Body: value.Body, CreatedAt: value.CreatedAt, UpdatedAt: value.UpdatedAt}
}

func projectedPullRequestReviewResponse(value pullrequest.ProjectedReview) generated.PullRequestReview {
	return generated.PullRequestReview{Uri: value.URI, Cid: value.CID, AuthorDid: value.AuthorDID, PullRequestUri: value.Subject.URI,
		PullRequestCid: value.Subject.CID, Verdict: generated.PullRequestReviewVerdict(value.Verdict), Body: value.Body,
		CreatedAt: value.CreatedAt, UpdatedAt: value.UpdatedAt, IndexedAt: value.IndexedAt}
}

func pullRequestReviewEnvelopeResponse(value pullrequest.Review) generated.PullRequestReviewEnvelope {
	return generated.PullRequestReviewEnvelope{Uri: value.URI, Cid: value.CID, AuthorDid: value.AuthorDID, PullRequestUri: value.Subject.URI,
		PullRequestCid: value.Subject.CID, Verdict: generated.PullRequestReviewEnvelopeVerdict(value.Verdict), Body: value.Body,
		CreatedAt: value.CreatedAt, UpdatedAt: value.UpdatedAt}
}

func pullRequestStatusEnvelopeResponse(value pullrequest.Status) generated.PullRequestStatusEnvelope {
	return generated.PullRequestStatusEnvelope{Uri: value.URI, Cid: value.CID, AuthorDid: value.AuthorDID, PullRequestUri: value.Subject.URI,
		PullRequestCid: value.Subject.CID, TargetRepositoryUri: value.TargetRepository.URI, TargetRepositoryCid: value.TargetRepository.CID,
		State: generated.PullRequestStatusEnvelopeState(value.State), MergeCommitSha: pointerUnlessEmpty(value.MergeCommitSHA), CreatedAt: value.CreatedAt, UpdatedAt: value.UpdatedAt}
}

func projectedCommentResponse(value comment.ProjectedComment) generated.Comment {
	var parentURI, parentCID *string
	if value.Parent != nil {
		parentURI, parentCID = &value.Parent.URI, &value.Parent.CID
	}
	return generated.Comment{
		Uri: value.URI, Cid: value.CID, AuthorDid: value.AuthorDID,
		IssueUri: value.Subject.URI, IssueCid: value.Subject.CID, ParentUri: parentURI, ParentCid: parentCID,
		Body: value.Body, CreatedAt: value.CreatedAt, UpdatedAt: value.UpdatedAt, IndexedAt: value.IndexedAt,
	}
}

func commentEnvelopeResponse(value issue.Comment) generated.CommentEnvelope {
	var parentURI, parentCID *string
	if value.Parent != nil {
		parentURI, parentCID = &value.Parent.URI, &value.Parent.CID
	}
	return generated.CommentEnvelope{
		Uri: value.URI, Cid: value.CID, AuthorDid: value.AuthorDID,
		IssueUri: value.Subject.URI, IssueCid: value.Subject.CID, ParentUri: parentURI, ParentCid: parentCID,
		Body: value.Body, CreatedAt: value.CreatedAt, UpdatedAt: value.UpdatedAt,
	}
}

func optionalString(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func pointerUnlessEmpty(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}
