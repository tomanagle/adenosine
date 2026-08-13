package atproto

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/bluesky-social/indigo/atproto/auth/oauth"
	indigoidentity "github.com/bluesky-social/indigo/atproto/identity"
	"github.com/bluesky-social/indigo/atproto/syntax"
)

const canonicalDID = "did:plc:abcdefghijklmnopqrstuvwx"

type fakeOAuthFlow struct {
	starts      int
	input       string
	start       string
	startErr    error
	session     *oauth.ClientSessionData
	callbackErr error
	persistErr  error
}

func (flow *fakeOAuthFlow) StartAuthFlow(ctx context.Context, identifier string) (string, error) {
	flow.starts++
	flow.input = identifier
	if result, ok := ctx.Value(saveResultKey{}).(*saveResult); ok {
		result.err = flow.persistErr
	}
	return flow.start, flow.startErr
}

func (flow *fakeOAuthFlow) ProcessCallback(ctx context.Context, _ url.Values) (*oauth.ClientSessionData, error) {
	if flow.callbackErr == nil && flow.session != nil {
		if capture, ok := ctx.Value(callbackSessionCaptureKey{}).(*callbackSessionCapture); ok {
			staged := *flow.session
			capture.session = &staged
		}
	}
	return flow.session, flow.callbackErr
}

type fakeDirectory struct {
	identity *indigoidentity.Identity
	err      error
}

type fakeResumableSessionStore struct {
	saved       oauth.ClientSessionData
	saveCalls   int
	deleteCalls int
	saveErr     error
}

func (store *fakeResumableSessionStore) SaveSession(_ context.Context, session oauth.ClientSessionData) error {
	store.saveCalls++
	store.saved = session
	return store.saveErr
}

func (store *fakeResumableSessionStore) DeleteSession(context.Context, syntax.DID, string) error {
	store.deleteCalls++
	return nil
}

func (store *fakeResumableSessionStore) GetLatestSession(context.Context, syntax.DID) (*oauth.ClientSessionData, error) {
	session := store.saved
	return &session, nil
}

func (directory fakeDirectory) LookupDID(context.Context, syntax.DID) (*indigoidentity.Identity, error) {
	return directory.identity, directory.err
}

func (directory fakeDirectory) LookupHandle(context.Context, syntax.Handle) (*indigoidentity.Identity, error) {
	return directory.identity, directory.err
}

func (directory fakeDirectory) Lookup(context.Context, syntax.AtIdentifier) (*indigoidentity.Identity, error) {
	return directory.identity, directory.err
}

func (fakeDirectory) Purge(context.Context, syntax.AtIdentifier) error { return nil }

func parsedDID(t *testing.T) syntax.DID {
	t.Helper()
	did, err := syntax.ParseDID(canonicalDID)
	if err != nil {
		t.Fatal(err)
	}
	return did
}

func TestBuildConfiguresLocalhostAndPublicMetadata(t *testing.T) {
	t.Parallel()
	testCases := []struct {
		name     string
		baseURL  string
		clientID string
		callback string
	}{
		{
			name:    "loopback",
			baseURL: "http://127.0.0.1:3000",
			// A loopback client advertises its scopes inside the client ID.
			clientID: "http://localhost?redirect_uri=http%3A%2F%2F127.0.0.1%3A3000%2Foauth%2Fatproto%2Fcallback" +
				"&scope=atproto+repo%3Adev.adenosine.repo+repo%3Adev.adenosine.profile+repo%3Adev.adenosine.organization" +
				"+repo%3Adev.adenosine.organizationGrant+repo%3Adev.adenosine.organizationMembership" +
				"+repo%3Adev.adenosine.organizationRevocation+repo%3Adev.adenosine.issue" +
				"+repo%3Adev.adenosine.issueComment+repo%3Adev.adenosine.issueStatus+repo%3Adev.adenosine.pullRequest" +
				"+repo%3Adev.adenosine.pullRequestReview+repo%3Adev.adenosine.pullRequestStatus+repo%3Adev.adenosine.star",
			callback: "http://127.0.0.1:3000/oauth/atproto/callback",
		},
		{
			name:     "public",
			baseURL:  "https://git.example/app/",
			clientID: "https://git.example/app/oauth/client-metadata.json",
			callback: "https://git.example/app/oauth/atproto/callback",
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			client, err := build(testCase.baseURL, nil, nil, nil, nil, buildOptions{flow: &fakeOAuthFlow{}, directory: fakeDirectory{}})
			if err != nil {
				t.Fatalf("build: %v", err)
			}
			metadata := client.ClientMetadata()
			if metadata.ClientID != testCase.clientID || len(metadata.RedirectURIs) != 1 || metadata.RedirectURIs[0] != testCase.callback {
				t.Fatalf("metadata = %#v", metadata)
			}
			if metadata.Scope != strings.Join(oauthScopes(), " ") || !metadata.DPoPBoundAccessTokens {
				t.Fatalf("metadata scope/config = %#v", metadata)
			}
			if strings.Contains(metadata.Scope, "transition") {
				t.Fatalf("metadata requests a transitional scope = %q", metadata.Scope)
			}
		})
	}
}

func TestBuildRejectsUnsafeBaseURLs(t *testing.T) {
	t.Parallel()
	testCases := []struct {
		name    string
		baseURL string
	}{
		{name: "empty"},
		{name: "leading whitespace", baseURL: " http://localhost"},
		{name: "localhost hostname", baseURL: "http://localhost:3000"},
		{name: "public HTTP", baseURL: "http://example.com"},
		{name: "userinfo", baseURL: "https://user:password@example.com"},
		{name: "query", baseURL: "https://example.com?secret=value"},
		{name: "fragment", baseURL: "https://example.com/#fragment"},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			if _, err := build(testCase.baseURL, nil, nil, nil, nil, buildOptions{flow: &fakeOAuthFlow{}, directory: fakeDirectory{}}); err == nil {
				t.Fatalf("unsafe base URL accepted: %q", testCase.baseURL)
			}
		})
	}
}

func TestStartValidatesIdentifierBeforeNetwork(t *testing.T) {
	t.Parallel()
	testCases := []struct {
		name       string
		identifier string
		valid      bool
	}{
		{name: "empty"},
		{name: "URL", identifier: "https://bsky.social"},
		{name: "host and port", identifier: "bsky.social:443"},
		{name: "IP address", identifier: "127.0.0.1"},
		{name: "single label", identifier: "server"},
		{name: "localhost", identifier: "localhost"},
		{name: "invalid TLD", identifier: "handle.invalid"},
		{name: "localhost subdomain", identifier: "alice.localhost"},
		{name: "example TLD", identifier: "alice.example"},
		{name: "leading whitespace", identifier: " alice.test"},
		{name: "trailing newline", identifier: "alice.test\n"},
		{name: "too long", identifier: strings.Repeat("a", 2049)},
		{name: "valid handle and DID", identifier: "Alice.Test", valid: true},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			if testCase.valid {
				flow := &fakeOAuthFlow{start: "https://auth.example/authorize"}
				client := &Client{flow: flow}
				redirect, err := client.Start(context.Background(), testCase.identifier)
				if err != nil {
					t.Fatalf("start handle: %v", err)
				}
				if redirect != flow.start || flow.input != "alice.test" {
					t.Fatalf("redirect/input = %q, %q", redirect, flow.input)
				}
				if _, err := client.Start(context.Background(), canonicalDID); err != nil {
					t.Fatalf("start DID: %v", err)
				}
				return
			}

			flow := &fakeOAuthFlow{}
			client := &Client{flow: flow}
			_, err := client.Start(context.Background(), testCase.identifier)
			if !errors.Is(err, ErrInvalidIdentifier) {
				t.Fatalf("identifier %q error = %v", testCase.identifier, err)
			}
			if flow.starts != 0 {
				t.Fatalf("identifier %q reached provider", testCase.identifier)
			}
		})
	}
}

func TestStartWrapsProviderFailureWithoutSecretDetails(t *testing.T) {
	t.Parallel()
	testCases := []struct{ name string }{{name: "provider failure"}}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			flow := &fakeOAuthFlow{startErr: errors.New("provider included secret-code")}
			_, err := (&Client{flow: flow}).Start(context.Background(), "alice.test")
			if !errors.Is(err, ErrProviderFailure) {
				t.Fatalf("error = %v", err)
			}
			var typed *ProviderError
			if !errors.As(err, &typed) {
				t.Fatalf("error type = %T", err)
			}
			if strings.Contains(err.Error(), "secret-code") {
				t.Fatalf("provider secret rendered in error: %v", err)
			}
		})
	}
}

func TestStartFailsWhenIndigoIgnoresStatePersistenceError(t *testing.T) {
	t.Parallel()
	testCases := []struct{ name string }{{name: "persistence failure"}}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			flow := &fakeOAuthFlow{start: "https://provider.example/authorize", persistErr: errors.New("database included secret-state")}
			client := &Client{flow: flow, observeStateSaves: true}
			redirectURL, err := client.Start(context.Background(), "alice.test")
			if redirectURL != "" || !errors.Is(err, ErrProviderFailure) {
				t.Fatalf("redirect/error = %q, %v", redirectURL, err)
			}
			if strings.Contains(err.Error(), "secret-state") {
				t.Fatalf("state persistence details rendered in error: %v", err)
			}
		})
	}
}

func TestCompleteRequiresAtprotoScopeAndDropsCredentials(t *testing.T) {
	t.Parallel()
	testCases := []struct{ name string }{{name: "missing atproto scope"}}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			did := parsedDID(t)
			session := &oauth.ClientSessionData{
				AccountDID:              did,
				SessionID:               "browser-session",
				Scopes:                  []string{"repo:read"},
				AccessToken:             "access-secret",
				RefreshToken:            "refresh-secret",
				DPoPPrivateKeyMultibase: "private-secret",
			}
			client := &Client{
				flow:      &fakeOAuthFlow{session: session},
				directory: fakeDirectory{identity: &indigoidentity.Identity{DID: did, Handle: syntax.Handle("alice.test")}},
			}
			_, _, err := client.Complete(context.Background(), nil)
			if !errors.Is(err, ErrCallbackFailure) {
				t.Fatalf("scope error = %v", err)
			}
			if !reflectZeroSession(*session) {
				t.Fatalf("OAuth session credentials retained: %#v", session)
			}
		})
	}
}

func TestCompleteReturnsCanonicalDIDAndCurrentlyVerifiedHandle(t *testing.T) {
	t.Parallel()
	did := parsedDID(t)
	testCases := []struct {
		name   string
		handle syntax.Handle
		want   string
	}{
		{name: "verified", handle: syntax.Handle("Alice.Test"), want: "alice.test"},
		{name: "invalid omitted", handle: syntax.HandleInvalid, want: ""},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			session := &oauth.ClientSessionData{AccountDID: did, SessionID: "browser-session", Scopes: []string{"atproto"}, AccessToken: "secret"}
			client := &Client{
				flow:      &fakeOAuthFlow{session: session},
				directory: fakeDirectory{identity: &indigoidentity.Identity{DID: did, Handle: testCase.handle}},
			}
			callbackValues := url.Values{"code": {"callback-secret"}, "state": {"state-secret"}}
			result, grant, err := client.Complete(context.Background(), callbackValues)
			if err != nil {
				t.Fatalf("complete: %v", err)
			}
			if result.DID != canonicalDID || result.Handle != testCase.want {
				t.Fatalf("identity = %#v", result)
			}
			if grant == nil {
				t.Fatal("credential grant is nil")
			}
			if len(callbackValues) != 0 {
				t.Fatalf("callback values retained: %#v", callbackValues)
			}
			if err := grant.Discard(context.Background()); err != nil {
				t.Fatalf("discard credential grant: %v", err)
			}
			if !reflectZeroSession(*session) {
				t.Fatalf("OAuth session retained: %#v", session)
			}
		})
	}
}

func TestCompleteCredentialGrantPersistsOnceAndDeletesOnDownstreamFailure(t *testing.T) {
	t.Parallel()
	testCases := []struct{ name string }{{name: "persist once then discard"}}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			did := parsedDID(t)
			session := &oauth.ClientSessionData{
				AccountDID: did, SessionID: "browser-session", Scopes: []string{"atproto"},
				AccessToken: "access-secret", RefreshToken: "refresh-secret", DPoPPrivateKeyMultibase: "private-secret",
			}
			store := &fakeResumableSessionStore{}
			client := &Client{
				flow:         &fakeOAuthFlow{session: session},
				directory:    fakeDirectory{identity: &indigoidentity.Identity{DID: did, Handle: syntax.Handle("alice.test")}},
				sessionStore: store,
			}
			_, grant, err := client.Complete(context.Background(), nil)
			if err != nil {
				t.Fatal(err)
			}
			if !reflectZeroSession(*session) {
				t.Fatalf("callback session retained: %#v", session)
			}
			if err := grant.Persist(context.Background()); err != nil {
				t.Fatal(err)
			}
			if store.saveCalls != 1 || store.saved.AccessToken != "access-secret" || store.saved.SessionID != "browser-session" {
				t.Fatalf("saved credentials = %#v, calls %d", store.saved, store.saveCalls)
			}
			if err := grant.Persist(context.Background()); err == nil {
				t.Fatal("credential grant persisted twice")
			}
			if err := grant.Discard(context.Background()); err != nil {
				t.Fatal(err)
			}
			if store.deleteCalls != 1 {
				t.Fatalf("credential deletes = %d, want 1", store.deleteCalls)
			}
		})
	}
}

func reflectZeroSession(session oauth.ClientSessionData) bool {
	return reflect.DeepEqual(session, oauth.ClientSessionData{})
}

func TestPublicIPPolicy(t *testing.T) {
	t.Parallel()
	testCases := []struct {
		name    string
		address string
		public  bool
	}{
		{name: "public IPv4", address: "8.8.8.8", public: true},
		{name: "public IPv6", address: "2606:4700:4700::1111", public: true},
		{name: "loopback IPv4", address: "127.0.0.1"}, {name: "private 10", address: "10.0.0.1"}, {name: "private 172", address: "172.16.0.1"},
		{name: "private 192", address: "192.168.0.1"}, {name: "link-local IPv4", address: "169.254.1.1"}, {name: "unspecified IPv4", address: "0.0.0.0"},
		{name: "multicast IPv4", address: "224.0.0.1"}, {name: "loopback IPv6", address: "::1"}, {name: "private IPv6", address: "fc00::1"}, {name: "link-local IPv6", address: "fe80::1"}, {name: "multicast IPv6", address: "ff02::1"},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			address := netip.MustParseAddr(testCase.address)
			if got := isPublicIP(address); got != testCase.public {
				t.Fatalf("isPublicIP(%s) = %v", address, got)
			}
		})
	}
}

func TestPublicDialRejectsPrivateOrMixedDNSAnswers(t *testing.T) {
	t.Parallel()
	testCases := []struct {
		name      string
		addresses []netip.Addr
	}{
		{name: "private", addresses: []netip.Addr{netip.MustParseAddr("127.0.0.1")}},
		{name: "mixed", addresses: []netip.Addr{netip.MustParseAddr("8.8.8.8"), netip.MustParseAddr("10.0.0.1")}},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			dial := publicDialContext(&net.Dialer{}, func(context.Context, string) ([]netip.Addr, error) { return testCase.addresses, nil })
			if _, err := dial(context.Background(), "tcp", "provider.example:443"); !errors.Is(err, errorsNoPublicAddress) {
				t.Fatalf("addresses %v error = %v", testCase.addresses, err)
			}
		})
	}
}

func TestPublicHTTPClientRejectsUnsafeRedirects(t *testing.T) {
	t.Parallel()
	testCases := []struct {
		name   string
		rawURL string
	}{
		{name: "HTTP", rawURL: "http://provider.example/callback"},
		{name: "userinfo", rawURL: "https://user:secret@provider.example/callback"},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			client := newPublicHTTPClient(func(context.Context, string) ([]netip.Addr, error) {
				return []netip.Addr{netip.MustParseAddr("8.8.8.8")}, nil
			})
			requestURL, err := url.Parse(testCase.rawURL)
			if err != nil {
				t.Fatal(err)
			}
			request := &http.Request{URL: requestURL}
			if err := client.CheckRedirect(request, nil); !errors.Is(err, errorsUnsafeRedirect) {
				t.Fatalf("redirect %q error = %v", testCase.rawURL, err)
			}
		})
	}
}

// A PDS authorizes record writes per collection, so login must request a
// granular scope for every collection this server publishes. Adding a
// published record type without its scope breaks publication at runtime.
func TestOAuthScopesRequestGranularCollectionWrites(t *testing.T) {
	t.Parallel()
	testCases := []struct {
		name string
		want []string
	}{
		{
			name: "base scope and every published collection",
			want: []string{
				"atproto",
				"repo:dev.adenosine.repo",
				"repo:dev.adenosine.profile",
				"repo:dev.adenosine.organization",
				"repo:dev.adenosine.organizationGrant",
				"repo:dev.adenosine.organizationMembership",
				"repo:dev.adenosine.organizationRevocation",
				"repo:dev.adenosine.issue",
				"repo:dev.adenosine.issueComment",
				"repo:dev.adenosine.issueStatus",
				"repo:dev.adenosine.pullRequest",
				"repo:dev.adenosine.pullRequestReview",
				"repo:dev.adenosine.pullRequestStatus",
				"repo:dev.adenosine.star",
			},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			got := oauthScopes()
			if !slices.Equal(got, testCase.want) {
				t.Fatalf("requested scopes = %q, want %q", got, testCase.want)
			}
			for _, scope := range got {
				if strings.Contains(scope, "transition") {
					t.Fatalf("transitional scope requested = %q", scope)
				}
			}
		})
	}
}
