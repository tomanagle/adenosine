package atproto

import (
	"context"
	"crypto/rand"
	"errors"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
	"unicode"

	dbgen "github.com/adenosine-dev/adenosine/internal/database/generated"
	localidentity "github.com/adenosine-dev/adenosine/internal/identity"
	"github.com/adenosine-dev/adenosine/internal/issue"
	"github.com/adenosine-dev/adenosine/internal/pullrequest"
	"github.com/adenosine-dev/adenosine/internal/star"
	"github.com/adenosine-dev/adenosine/internal/transfer"
	"github.com/bluesky-social/indigo/atproto/atclient"
	"github.com/bluesky-social/indigo/atproto/auth/oauth"
	indigoidentity "github.com/bluesky-social/indigo/atproto/identity"
	"github.com/bluesky-social/indigo/atproto/syntax"
)

const oauthScope = "atproto"

// publishedCollections are every collection this server writes to an owner's
// repository. Their granular scopes are requested at login because a PDS
// authorizes record writes per collection.
func publishedCollections() []string {
	return []string{
		repositoryCollection,
		transfer.ProposalCollection,
		transfer.AcceptanceCollection,
		profileCollection,
		organizationCollection,
		"dev.adenosine.organizationGrant",
		"dev.adenosine.organizationMembership",
		"dev.adenosine.organizationRevocation",
		issue.Collection,
		issue.CommentCollection,
		issue.StatusCollection,
		pullrequest.Collection,
		pullrequest.ReviewCollection,
		pullrequest.StatusCollection,
		star.Collection,
	}
}

// oauthScopes keeps the delegation least-privilege: the base scope plus one
// granular repository scope per published collection, and never a transitional
// scope that would grant unrelated account access.
func oauthScopes() []string {
	collections := publishedCollections()
	scopes := make([]string, 0, len(collections)+1)
	scopes = append(scopes, oauthScope)
	for _, collection := range collections {
		scopes = append(scopes, "repo:"+collection)
	}
	return scopes
}

type oauthFlow interface {
	StartAuthFlow(context.Context, string) (string, error)
	ProcessCallback(context.Context, url.Values) (*oauth.ClientSessionData, error)
}

type saveResultKey struct{}

type saveResult struct{ err error }

type observingStore struct{ oauth.ClientAuthStore }

func (store observingStore) SaveAuthRequestInfo(ctx context.Context, info oauth.AuthRequestData) error {
	err := store.ClientAuthStore.SaveAuthRequestInfo(ctx, info)
	if result, ok := ctx.Value(saveResultKey{}).(*saveResult); ok {
		result.err = err
	}
	return err
}

type callbackSessionCapture struct {
	session     *oauth.ClientSessionData
	authRequest *oauth.AuthRequestData
}

type callbackSessionCaptureKey struct{}

func (store observingStore) SaveSession(ctx context.Context, session oauth.ClientSessionData) error {
	if capture, ok := ctx.Value(callbackSessionCaptureKey{}).(*callbackSessionCapture); ok {
		clearSession(capture.session)
		capture.session = &session
		return nil
	}
	return store.ClientAuthStore.SaveSession(ctx, session)
}

func (store observingStore) GetAuthRequestInfo(ctx context.Context, state string) (*oauth.AuthRequestData, error) {
	info, err := store.ClientAuthStore.GetAuthRequestInfo(ctx, state)
	if err == nil {
		if capture, ok := ctx.Value(callbackSessionCaptureKey{}).(*callbackSessionCapture); ok {
			capture.authRequest = info
		}
	}
	return info, err
}

type resumableSessionStore interface {
	SaveSession(context.Context, oauth.ClientSessionData) error
	DeleteSession(context.Context, syntax.DID, string) error
	GetLatestSession(context.Context, syntax.DID) (*oauth.ClientSessionData, error)
}

type credentialGrant struct {
	mu        sync.Mutex
	store     resumableSessionStore
	session   *oauth.ClientSessionData
	did       syntax.DID
	sessionID string
	persisted bool
	closed    bool
}

func (grant *credentialGrant) Persist(ctx context.Context) error {
	grant.mu.Lock()
	defer grant.mu.Unlock()
	if grant.closed || grant.session == nil {
		return errors.New("OAuth credential grant is no longer available")
	}
	if grant.store == nil {
		clearSession(grant.session)
		grant.closed = true
		return errors.New("OAuth credential store is unavailable")
	}
	if err := grant.store.SaveSession(ctx, *grant.session); err != nil {
		cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		_ = grant.store.DeleteSession(cleanupCtx, grant.did, grant.sessionID)
		cancel()
		clearSession(grant.session)
		grant.closed = true
		return err
	}
	clearSession(grant.session)
	grant.session = nil
	grant.persisted = true
	return nil
}

func (grant *credentialGrant) Discard(ctx context.Context) error {
	grant.mu.Lock()
	defer grant.mu.Unlock()
	clearSession(grant.session)
	grant.session = nil
	grant.closed = true
	if grant.persisted && grant.store != nil {
		grant.persisted = false
		cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		return grant.store.DeleteSession(cleanupCtx, grant.did, grant.sessionID)
	}
	return nil
}

type buildOptions struct {
	httpClient   *http.Client
	directory    indigoidentity.Directory
	flow         oauthFlow
	sessionStore resumableSessionStore
	latest       latestCredentialLoader
	apiFactory   func(string, atclient.AuthMethod) profileAPI
	resume       func(context.Context, syntax.DID, string) (*oauth.ClientSession, error)
}

// ClientMetadata is the public OAuth client metadata document.
type ClientMetadata struct {
	ClientID                string   `json:"client_id"`
	ApplicationType         *string  `json:"application_type,omitempty"`
	GrantTypes              []string `json:"grant_types"`
	Scope                   string   `json:"scope"`
	ResponseTypes           []string `json:"response_types"`
	RedirectURIs            []string `json:"redirect_uris"`
	DPoPBoundAccessTokens   bool     `json:"dpop_bound_access_tokens"`
	TokenEndpointAuthMethod string   `json:"token_endpoint_auth_method"`
}

// Client adapts Indigo OAuth to return only a verified DID and current handle.
type Client struct {
	flow              oauthFlow
	app               *oauth.ClientApp
	directory         indigoidentity.Directory
	metadata          ClientMetadata
	observeStateSaves bool
	sessionStore      resumableSessionStore
	httpClient        *http.Client
	apiFactory        func(string, atclient.AuthMethod) profileAPI
	resume            func(context.Context, syntax.DID, string) (*oauth.ClientSession, error)
	operations        sync.Map
}

// Must constructs the required AT Protocol OAuth client and state store or panics at startup.
func Must(baseURL string, queries *dbgen.Queries, stateKey, credentialKey []byte, clock Clock) *Client {
	client, err := build(baseURL, queries, stateKey, credentialKey, clock, buildOptions{})
	if err != nil {
		panic(err)
	}
	return client
}

func build(baseURL string, queries *dbgen.Queries, stateKey, credentialKey []byte, clock Clock, options buildOptions) (*Client, error) {
	base, err := parseBaseURL(baseURL)
	if err != nil {
		return nil, err
	}
	callbackURL := base + "/oauth/atproto/callback"
	var config oauth.ClientConfig
	parsedBase, _ := url.Parse(base)
	if parsedBase.Scheme == "http" && isLoopbackIP(parsedBase.Hostname()) {
		config = oauth.NewLocalhostConfig(callbackURL, oauthScopes())
	} else {
		config = oauth.NewPublicConfig(base+"/oauth/client-metadata.json", callbackURL, oauthScopes())
	}

	httpClient := options.httpClient
	if httpClient == nil {
		httpClient = newPublicHTTPClient(nil)
	}
	directory := options.directory
	if directory == nil {
		directory = &indigoidentity.BaseDirectory{
			PLCURL:                indigoidentity.DefaultPLCURL,
			HTTPClient:            *httpClient,
			Resolver:              net.Resolver{},
			TryAuthoritativeDNS:   true,
			SkipDNSDomainSuffixes: []string{".bsky.social"},
			UserAgent:             config.UserAgent,
		}
	}
	flow := options.flow
	sessionStore := options.sessionStore
	var app *oauth.ClientApp
	if flow == nil {
		store, err := buildPostgresClientAuthStore(queries, stateKey, credentialKey, clock, rand.Reader, options.latest)
		if err != nil {
			return nil, err
		}
		app = oauth.NewClientApp(&config, observingStore{ClientAuthStore: store})
		app.Client = httpClient
		app.Resolver = &oauth.Resolver{Client: httpClient, UserAgent: config.UserAgent}
		app.Dir = directory
		flow = app
		sessionStore = store
	}
	indigoMetadata := config.ClientMetadata()
	metadata := ClientMetadata{
		ClientID:                indigoMetadata.ClientID,
		ApplicationType:         indigoMetadata.ApplicationType,
		GrantTypes:              append([]string(nil), indigoMetadata.GrantTypes...),
		Scope:                   indigoMetadata.Scope,
		ResponseTypes:           append([]string(nil), indigoMetadata.ResponseTypes...),
		RedirectURIs:            append([]string(nil), indigoMetadata.RedirectURIs...),
		DPoPBoundAccessTokens:   indigoMetadata.DPoPBoundAccessTokens,
		TokenEndpointAuthMethod: indigoMetadata.TokenEndpointAuthMethod,
	}
	apiFactory := options.apiFactory
	if apiFactory == nil {
		apiFactory = func(host string, auth atclient.AuthMethod) profileAPI {
			api := atclient.NewAPIClient(host)
			api.Client = httpClient
			api.Auth = auth
			return api
		}
	}
	return &Client{flow: flow, app: app, directory: directory, metadata: metadata, observeStateSaves: options.flow == nil, sessionStore: sessionStore, httpClient: httpClient, apiFactory: apiFactory, resume: options.resume}, nil
}

// Start validates a handle or DID before any provider network access.
func (client *Client) Start(ctx context.Context, identifier string) (string, error) {
	identifier, err := validateIdentifier(identifier)
	if err != nil {
		return "", err
	}
	flowContext := ctx
	result := &saveResult{}
	if client.observeStateSaves {
		flowContext = context.WithValue(ctx, saveResultKey{}, result)
	}
	redirectURL, err := client.flow.StartAuthFlow(flowContext, identifier)
	if err != nil {
		return "", &ProviderError{Operation: "start", Err: err}
	}
	if result.err != nil {
		return "", &ProviderError{Operation: "state persistence", Err: result.err}
	}
	return redirectURL, nil
}

// Complete verifies the callback and returns credentials through an opaque grant.
func (client *Client) Complete(ctx context.Context, values url.Values) (localidentity.OAuthIdentity, localidentity.OAuthCredentialGrant, error) {
	capture := &callbackSessionCapture{}
	defer clearValues(values)
	defer func() {
		if capture.authRequest != nil {
			*capture.authRequest = oauth.AuthRequestData{}
		}
	}()
	session, err := client.flow.ProcessCallback(context.WithValue(ctx, callbackSessionCaptureKey{}, capture), values)
	if err != nil {
		clearSession(capture.session)
		return localidentity.OAuthIdentity{}, nil, &CallbackError{Operation: "exchange", Err: err}
	}
	if session == nil {
		clearSession(capture.session)
		return localidentity.OAuthIdentity{}, nil, &CallbackError{Operation: "exchange", Err: errors.New("empty OAuth session")}
	}
	defer clearSession(session)
	if capture.session == nil {
		return localidentity.OAuthIdentity{}, nil, &CallbackError{Operation: "credential capture", Err: errors.New("OAuth session was not staged")}
	}
	grant := &credentialGrant{
		store: client.sessionStore, session: capture.session,
		did: capture.session.AccountDID, sessionID: capture.session.SessionID,
	}
	discard := func(operation string, cause error) (localidentity.OAuthIdentity, localidentity.OAuthCredentialGrant, error) {
		_ = grant.Discard(ctx)
		return localidentity.OAuthIdentity{}, nil, &CallbackError{Operation: operation, Err: cause}
	}
	didValue := session.AccountDID.String()
	grantedAtproto := false
	for _, scope := range session.Scopes {
		if scope == oauthScope {
			grantedAtproto = true
			break
		}
	}
	if !grantedAtproto {
		return discard("scope verification", errors.New("required scope not granted"))
	}
	did, err := syntax.ParseDID(didValue)
	if err != nil {
		return discard("DID verification", err)
	}
	if capture.session.AccountDID != did || capture.session.SessionID == "" {
		return discard("credential verification", errors.New("staged OAuth session identity mismatch"))
	}
	resolved, err := client.directory.LookupDID(ctx, did)
	if err != nil {
		return discard("identity verification", err)
	}
	if resolved.DID != did {
		return discard("identity verification", errors.New("resolved DID mismatch"))
	}
	handle := ""
	if !resolved.Handle.IsInvalidHandle() {
		handle = resolved.Handle.Normalize().String()
	}
	return localidentity.OAuthIdentity{DID: did.String(), Handle: handle}, grant, nil
}

func clearValues(values url.Values) {
	for key, entries := range values {
		for index := range entries {
			entries[index] = ""
		}
		delete(values, key)
	}
}

// ClientMetadata returns a copy of the public metadata document.
func (client *Client) ClientMetadata() ClientMetadata {
	metadata := client.metadata
	metadata.GrantTypes = append([]string(nil), metadata.GrantTypes...)
	metadata.ResponseTypes = append([]string(nil), metadata.ResponseTypes...)
	metadata.RedirectURIs = append([]string(nil), metadata.RedirectURIs...)
	return metadata
}

func validateIdentifier(identifier string) (string, error) {
	if identifier == "" || len(identifier) > 2048 || strings.TrimSpace(identifier) != identifier {
		return "", ErrInvalidIdentifier
	}
	for _, character := range identifier {
		if unicode.IsControl(character) {
			return "", ErrInvalidIdentifier
		}
	}
	if strings.Contains(identifier, "://") || net.ParseIP(identifier) != nil {
		return "", ErrInvalidIdentifier
	}
	atIdentifier, err := syntax.ParseAtIdentifier(identifier)
	if err != nil {
		return "", ErrInvalidIdentifier
	}
	if handle, err := atIdentifier.AsHandle(); err == nil {
		if !handle.AllowedTLD() || handle.IsInvalidHandle() {
			return "", ErrInvalidIdentifier
		}
		return handle.Normalize().String(), nil
	}
	if _, err := atIdentifier.AsDID(); err != nil {
		return "", ErrInvalidIdentifier
	}
	return atIdentifier.String(), nil
}

func parseBaseURL(raw string) (string, error) {
	if raw == "" || strings.TrimSpace(raw) != raw {
		return "", errors.New("AT Protocol OAuth base URL is invalid")
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Hostname() == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return "", errors.New("AT Protocol OAuth base URL is invalid")
	}
	if parsed.Scheme != "https" && !(parsed.Scheme == "http" && isLoopbackIP(parsed.Hostname())) {
		return "", errors.New("AT Protocol OAuth base URL must use HTTPS except on a loopback IP")
	}
	if parsed.Path != "" && parsed.Path != "/" && strings.HasSuffix(parsed.Path, "/") {
		parsed.Path = strings.TrimSuffix(parsed.Path, "/")
	}
	return strings.TrimSuffix(parsed.String(), "/"), nil
}

func isLoopbackIP(host string) bool {
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
