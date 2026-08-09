package atproto

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"reflect"
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
	tests := []struct {
		name     string
		baseURL  string
		clientID string
		callback string
	}{
		{
			name:     "loopback",
			baseURL:  "http://127.0.0.1:3000",
			clientID: "http://localhost?redirect_uri=http%3A%2F%2F127.0.0.1%3A3000%2Foauth%2Fatproto%2Fcallback&scope=atproto",
			callback: "http://127.0.0.1:3000/oauth/atproto/callback",
		},
		{
			name:     "public",
			baseURL:  "https://git.example/app/",
			clientID: "https://git.example/app/oauth/client-metadata.json",
			callback: "https://git.example/app/oauth/atproto/callback",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			client, err := build(test.baseURL, nil, nil, nil, nil, buildOptions{flow: &fakeOAuthFlow{}, directory: fakeDirectory{}})
			if err != nil {
				t.Fatalf("build: %v", err)
			}
			metadata := client.ClientMetadata()
			if metadata.ClientID != test.clientID || len(metadata.RedirectURIs) != 1 || metadata.RedirectURIs[0] != test.callback {
				t.Fatalf("metadata = %#v", metadata)
			}
			if metadata.Scope != "atproto" || strings.Contains(metadata.Scope, "transition") || !metadata.DPoPBoundAccessTokens {
				t.Fatalf("metadata scope/config = %#v", metadata)
			}
		})
	}
}

func TestBuildRejectsUnsafeBaseURLs(t *testing.T) {
	t.Parallel()
	for _, baseURL := range []string{
		"", " http://localhost", "http://localhost:3000", "http://example.com",
		"https://user:password@example.com", "https://example.com?secret=value", "https://example.com/#fragment",
	} {
		if _, err := build(baseURL, nil, nil, nil, nil, buildOptions{flow: &fakeOAuthFlow{}, directory: fakeDirectory{}}); err == nil {
			t.Fatalf("unsafe base URL accepted: %q", baseURL)
		}
	}
}

func TestStartValidatesIdentifierBeforeNetwork(t *testing.T) {
	t.Parallel()
	for _, identifier := range []string{
		"", "https://bsky.social", "bsky.social:443", "127.0.0.1", "server", "localhost",
		"handle.invalid", "alice.localhost", "alice.example", " alice.test", "alice.test\n",
		strings.Repeat("a", 2049),
	} {
		flow := &fakeOAuthFlow{}
		client := &Client{flow: flow}
		_, err := client.Start(context.Background(), identifier)
		if !errors.Is(err, ErrInvalidIdentifier) {
			t.Fatalf("identifier %q error = %v", identifier, err)
		}
		if flow.starts != 0 {
			t.Fatalf("identifier %q reached provider", identifier)
		}
	}

	flow := &fakeOAuthFlow{start: "https://auth.example/authorize"}
	client := &Client{flow: flow}
	redirect, err := client.Start(context.Background(), "Alice.Test")
	if err != nil {
		t.Fatalf("start handle: %v", err)
	}
	if redirect != flow.start || flow.input != "alice.test" {
		t.Fatalf("redirect/input = %q, %q", redirect, flow.input)
	}
	if _, err := client.Start(context.Background(), canonicalDID); err != nil {
		t.Fatalf("start DID: %v", err)
	}
}

func TestStartWrapsProviderFailureWithoutSecretDetails(t *testing.T) {
	t.Parallel()
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
}

func TestStartFailsWhenIndigoIgnoresStatePersistenceError(t *testing.T) {
	t.Parallel()
	flow := &fakeOAuthFlow{start: "https://provider.example/authorize", persistErr: errors.New("database included secret-state")}
	client := &Client{flow: flow, observeStateSaves: true}
	redirectURL, err := client.Start(context.Background(), "alice.test")
	if redirectURL != "" || !errors.Is(err, ErrProviderFailure) {
		t.Fatalf("redirect/error = %q, %v", redirectURL, err)
	}
	if strings.Contains(err.Error(), "secret-state") {
		t.Fatalf("state persistence details rendered in error: %v", err)
	}
}

func TestCompleteRequiresAtprotoScopeAndDropsCredentials(t *testing.T) {
	t.Parallel()
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
}

func TestCompleteReturnsCanonicalDIDAndCurrentlyVerifiedHandle(t *testing.T) {
	t.Parallel()
	did := parsedDID(t)
	for _, test := range []struct {
		name   string
		handle syntax.Handle
		want   string
	}{
		{name: "verified", handle: syntax.Handle("Alice.Test"), want: "alice.test"},
		{name: "invalid omitted", handle: syntax.HandleInvalid, want: ""},
	} {
		t.Run(test.name, func(t *testing.T) {
			session := &oauth.ClientSessionData{AccountDID: did, SessionID: "browser-session", Scopes: []string{"atproto"}, AccessToken: "secret"}
			client := &Client{
				flow:      &fakeOAuthFlow{session: session},
				directory: fakeDirectory{identity: &indigoidentity.Identity{DID: did, Handle: test.handle}},
			}
			callbackValues := url.Values{"code": {"callback-secret"}, "state": {"state-secret"}}
			result, grant, err := client.Complete(context.Background(), callbackValues)
			if err != nil {
				t.Fatalf("complete: %v", err)
			}
			if result.DID != canonicalDID || result.Handle != test.want {
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
}

func reflectZeroSession(session oauth.ClientSessionData) bool {
	return reflect.DeepEqual(session, oauth.ClientSessionData{})
}

func TestPublicIPPolicy(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		address string
		public  bool
	}{
		{address: "8.8.8.8", public: true},
		{address: "2606:4700:4700::1111", public: true},
		{address: "127.0.0.1"}, {address: "10.0.0.1"}, {address: "172.16.0.1"},
		{address: "192.168.0.1"}, {address: "169.254.1.1"}, {address: "0.0.0.0"},
		{address: "224.0.0.1"}, {address: "::1"}, {address: "fc00::1"}, {address: "fe80::1"}, {address: "ff02::1"},
	} {
		address := netip.MustParseAddr(test.address)
		if got := isPublicIP(address); got != test.public {
			t.Fatalf("isPublicIP(%s) = %v", address, got)
		}
	}
}

func TestPublicDialRejectsPrivateOrMixedDNSAnswers(t *testing.T) {
	t.Parallel()
	for _, addresses := range [][]netip.Addr{
		{netip.MustParseAddr("127.0.0.1")},
		{netip.MustParseAddr("8.8.8.8"), netip.MustParseAddr("10.0.0.1")},
	} {
		dial := publicDialContext(&net.Dialer{}, func(context.Context, string) ([]netip.Addr, error) { return addresses, nil })
		if _, err := dial(context.Background(), "tcp", "provider.example:443"); !errors.Is(err, errorsNoPublicAddress) {
			t.Fatalf("addresses %v error = %v", addresses, err)
		}
	}
}

func TestPublicHTTPClientRejectsUnsafeRedirects(t *testing.T) {
	t.Parallel()
	client := newPublicHTTPClient(func(context.Context, string) ([]netip.Addr, error) {
		return []netip.Addr{netip.MustParseAddr("8.8.8.8")}, nil
	})
	for _, rawURL := range []string{"http://provider.example/callback", "https://user:secret@provider.example/callback"} {
		requestURL, err := url.Parse(rawURL)
		if err != nil {
			t.Fatal(err)
		}
		request := &http.Request{URL: requestURL}
		if err := client.CheckRedirect(request, nil); !errors.Is(err, errorsUnsafeRedirect) {
			t.Fatalf("redirect %q error = %v", rawURL, err)
		}
	}
}
