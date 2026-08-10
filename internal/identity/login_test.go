package identity

import (
	"context"
	"errors"
	"net/url"
	"reflect"
	"testing"
	"time"

	"github.com/adenosine-dev/adenosine/internal/auth"
	"github.com/google/uuid"
)

type fakeProvider struct {
	startCalls    int
	identifier    string
	redirectURL   string
	startErr      error
	completeCalls int
	identity      OAuthIdentity
	grant         OAuthCredentialGrant
	completeErr   error
}

func (provider *fakeProvider) Start(_ context.Context, identifier string) (string, error) {
	provider.startCalls++
	provider.identifier = identifier
	return provider.redirectURL, provider.startErr
}

func (provider *fakeProvider) Complete(context.Context, url.Values) (OAuthIdentity, OAuthCredentialGrant, error) {
	provider.completeCalls++
	return provider.identity, provider.grant, provider.completeErr
}

type fakeCredentialGrant struct {
	persistCalls int
	discardCalls int
	persistErr   error
	discardErr   error
	order        *[]string
}

func (grant *fakeCredentialGrant) Persist(context.Context) error {
	grant.persistCalls++
	if grant.order != nil {
		*grant.order = append(*grant.order, "credentials")
	}
	return grant.persistErr
}

func (grant *fakeCredentialGrant) Discard(context.Context) error {
	grant.discardCalls++
	return grant.discardErr
}

type fakeAccounts struct {
	calls  int
	did    string
	handle string
	at     time.Time
	err    error
	order  *[]string
}

func (accounts *fakeAccounts) UpsertLogin(_ context.Context, did, handle string, at time.Time) (auth.Account, error) {
	accounts.calls++
	accounts.did = did
	accounts.handle = handle
	accounts.at = at
	if accounts.order != nil {
		*accounts.order = append(*accounts.order, "account")
	}
	return auth.Account{DID: did}, accounts.err
}

type fakeSessions struct {
	calls     int
	did       string
	session   auth.Session
	plaintext string
	err       error
	order     *[]string
}

func (sessions *fakeSessions) CreateSession(_ context.Context, did string) (auth.Session, string, error) {
	sessions.calls++
	sessions.did = did
	if sessions.order != nil {
		*sessions.order = append(*sessions.order, "browser-session")
	}
	return sessions.session, sessions.plaintext, sessions.err
}

type loginFixedClock struct{ now time.Time }

func (clock loginFixedClock) Now() time.Time { return clock.now }

func TestLoginServiceStartRejectsReturnTargetBeforeDelegating(t *testing.T) {
	t.Parallel()
	testCases := []struct{ name string }{{name: "invalid then default return target"}}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			provider := &fakeProvider{redirectURL: "https://provider.example/authorize"}
			service := NewLoginService(provider, &fakeAccounts{}, &fakeSessions{}, loginFixedClock{})
			if _, err := service.Start(context.Background(), "alice.test", "https://evil.example"); !errors.Is(err, ErrInvalidReturnTo) {
				t.Fatalf("return target error = %v", err)
			}
			if provider.startCalls != 0 {
				t.Fatalf("provider called for unsupported return target")
			}
			redirectURL, err := service.Start(context.Background(), "alice.test", "")
			if err != nil {
				t.Fatalf("start: %v", err)
			}
			if redirectURL != provider.redirectURL || provider.identifier != "alice.test" {
				t.Fatalf("redirect/identifier = %q, %q", redirectURL, provider.identifier)
			}
		})
	}
}

func TestLoginServiceCompleteUpdatesHandleAndIssuesLocalSession(t *testing.T) {
	t.Parallel()
	testCases := []struct{ name string }{{name: "successful callback"}}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			now := time.Date(2026, time.August, 9, 12, 0, 0, 0, time.FixedZone("test", 3600))
			expiresAt := now.UTC().Add(24 * time.Hour)
			sessionID := uuid.New()
			order := []string{}
			grant := &fakeCredentialGrant{order: &order}
			provider := &fakeProvider{identity: OAuthIdentity{DID: "did:plc:abcdefghijklmnopqrstuvwx", Handle: "new-handle.test"}, grant: grant}
			accounts := &fakeAccounts{order: &order}
			sessions := &fakeSessions{
				session: auth.Session{
					ID:         sessionID,
					AccountDID: provider.identity.DID,
					Hash:       []byte("local-hash-not-oauth"),
					ExpiresAt:  expiresAt,
				},
				plaintext: "local-session-plaintext",
				order:     &order,
			}
			service := NewLoginService(provider, accounts, sessions, loginFixedClock{now: now})
			result, err := service.Complete(context.Background(), url.Values{
				"code":  {"oauth-code-secret"},
				"state": {"oauth-state-secret"},
			})
			if err != nil {
				t.Fatalf("complete: %v", err)
			}
			if accounts.calls != 1 || accounts.did != provider.identity.DID || accounts.handle != "new-handle.test" || !accounts.at.Equal(now.UTC()) {
				t.Fatalf("account upsert = %#v", accounts)
			}
			if sessions.calls != 1 || sessions.did != provider.identity.DID {
				t.Fatalf("session issuance = %#v", sessions)
			}
			if !reflect.DeepEqual(order, []string{"account", "credentials", "browser-session"}) {
				t.Fatalf("callback order = %v", order)
			}
			if grant.persistCalls != 1 || grant.discardCalls != 0 {
				t.Fatalf("credential grant calls = persist %d, discard %d", grant.persistCalls, grant.discardCalls)
			}
			want := LoginResult{
				DID:              provider.identity.DID,
				Handle:           provider.identity.Handle,
				SessionID:        sessionID,
				SessionExpiresAt: expiresAt,
				SessionPlaintext: "local-session-plaintext",
			}
			if !reflect.DeepEqual(result, want) {
				t.Fatalf("result = %#v, want %#v", result, want)
			}

			resultValue := reflect.ValueOf(result)
			resultType := resultValue.Type()
			for index := 0; index < resultType.NumField(); index++ {
				name := resultType.Field(index).Name
				if name == "AccessToken" || name == "RefreshToken" || name == "DPoPPrivateKey" || name == "OAuthSession" {
					t.Fatalf("login result exposes OAuth credential field %s", name)
				}
			}
		})
	}
}

func TestLoginServiceCompleteStopsBeforeSessionWhenAccountFails(t *testing.T) {
	t.Parallel()
	testCases := []struct{ name string }{{name: "account failure"}}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			grant := &fakeCredentialGrant{}
			provider := &fakeProvider{identity: OAuthIdentity{DID: "did:plc:abcdefghijklmnopqrstuvwx"}, grant: grant}
			accounts := &fakeAccounts{err: errors.New("database unavailable")}
			sessions := &fakeSessions{}
			_, err := NewLoginService(provider, accounts, sessions, loginFixedClock{}).Complete(context.Background(), nil)
			if err == nil || sessions.calls != 0 {
				t.Fatalf("error = %v, session calls = %d", err, sessions.calls)
			}
			if grant.persistCalls != 0 || grant.discardCalls != 1 {
				t.Fatalf("credential cleanup calls = persist %d, discard %d", grant.persistCalls, grant.discardCalls)
			}
		})
	}
}

func TestLoginServiceCompleteCleansUpCredentialFailures(t *testing.T) {
	t.Parallel()
	testCases := []struct {
		name            string
		persistErr      error
		localSessionErr error
		wantSession     int
	}{
		{name: "credential persistence", persistErr: errors.New("credential database unavailable")},
		{name: "local session issuance", localSessionErr: errors.New("session database unavailable"), wantSession: 1},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			grant := &fakeCredentialGrant{persistErr: testCase.persistErr}
			provider := &fakeProvider{
				identity: OAuthIdentity{DID: "did:plc:abcdefghijklmnopqrstuvwx"},
				grant:    grant,
			}
			sessions := &fakeSessions{err: testCase.localSessionErr}
			_, err := NewLoginService(provider, &fakeAccounts{}, sessions, loginFixedClock{}).Complete(context.Background(), nil)
			if err == nil {
				t.Fatal("expected completion failure")
			}
			if grant.persistCalls != 1 || grant.discardCalls != 1 || sessions.calls != testCase.wantSession {
				t.Fatalf("calls = persist %d, discard %d, session %d", grant.persistCalls, grant.discardCalls, sessions.calls)
			}
		})
	}
}
