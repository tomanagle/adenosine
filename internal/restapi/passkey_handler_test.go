package restapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/adenosine-dev/adenosine/internal/auth"
	localidentity "github.com/adenosine-dev/adenosine/internal/identity"
	"github.com/adenosine-dev/adenosine/internal/passkey"
	"github.com/google/uuid"
)

var passkeyTestSessionID = uuid.MustParse("0198ad40-00f4-7ee3-9d71-d35f7e5ada01")

type passkeyTestSessions struct{}

func (passkeyTestSessions) Authenticate(_ context.Context, plaintext string) (auth.SessionIdentity, error) {
	if plaintext != "valid-session" {
		return auth.SessionIdentity{}, auth.ErrUnauthorized
	}
	return auth.SessionIdentity{SessionID: passkeyTestSessionID, AccountDID: "did:plc:alice"}, nil
}

type fakePasskeys struct {
	beginResult      passkey.BeginResult
	registration     passkey.CredentialSummary
	login            passkey.LoginResult
	listed           []passkey.CredentialSummary
	err              error
	accountDID       string
	browserSessionID uuid.UUID
	name             string
	token            string
	response         []byte
	revokedID        uuid.UUID
	beginLoginCalls  int
	finishLoginCalls int
}

func (manager *fakePasskeys) BeginRegistration(_ context.Context, did string, sessionID uuid.UUID, name string) (passkey.BeginResult, error) {
	manager.accountDID, manager.browserSessionID, manager.name = did, sessionID, name
	return manager.beginResult, manager.err
}

func (manager *fakePasskeys) FinishRegistration(_ context.Context, did string, sessionID uuid.UUID, token string, response []byte) (passkey.CredentialSummary, error) {
	manager.accountDID, manager.browserSessionID, manager.token = did, sessionID, token
	manager.response = append([]byte(nil), response...)
	return manager.registration, manager.err
}

func (manager *fakePasskeys) BeginLogin(context.Context) (passkey.BeginResult, error) {
	manager.beginLoginCalls++
	return manager.beginResult, manager.err
}

func (manager *fakePasskeys) FinishLogin(_ context.Context, token string, response []byte) (passkey.LoginResult, error) {
	manager.finishLoginCalls++
	manager.token = token
	manager.response = append([]byte(nil), response...)
	return manager.login, manager.err
}

func (manager *fakePasskeys) List(_ context.Context, did string) ([]passkey.CredentialSummary, error) {
	manager.accountDID = did
	return manager.listed, manager.err
}

func (manager *fakePasskeys) Revoke(_ context.Context, did string, id uuid.UUID) error {
	manager.accountDID, manager.revokedID = did, id
	return manager.err
}

func TestPasskeyRegistrationUsesAuthenticatedSession(t *testing.T) {
	t.Parallel()
	credentialID := uuid.MustParse("0198ad40-00f4-7ee3-9d71-d35f7e5ada02")
	testCases := []struct {
		name       string
		path       string
		body       string
		manager    *fakePasskeys
		wantStatus int
		assert     func(*testing.T, *fakePasskeys, string, http.Header)
	}{
		{
			name: "begin derives DID and browser session", path: "/api/v1/passkeys/registration/options", body: `{"name":"Laptop"}`,
			manager: &fakePasskeys{beginResult: passkey.BeginResult{Options: json.RawMessage(`{"publicKey":{"challenge":"unchanged"}}`), Token: "ceremony"}}, wantStatus: http.StatusOK,
			assert: func(t *testing.T, manager *fakePasskeys, body string, _ http.Header) {
				t.Helper()
				if manager.accountDID != "did:plc:alice" || manager.browserSessionID != passkeyTestSessionID || manager.name != "Laptop" {
					t.Fatalf("registration identity = %q %s %q", manager.accountDID, manager.browserSessionID, manager.name)
				}
				if body != "{\"options\":{\"publicKey\":{\"challenge\":\"unchanged\"}},\"ceremony_token\":\"ceremony\"}\n" {
					t.Fatalf("options body = %q", body)
				}
			},
		},
		{
			name: "verify binds ceremony to same session", path: "/api/v1/passkeys/registration/verify", body: `{"ceremony_token":"ceremony","response":{"id":"credential","response":{"attestationObject":"value"}}}`,
			manager: &fakePasskeys{registration: passkey.CredentialSummary{ID: credentialID, Name: "Laptop", CreatedAt: time.Date(2026, 8, 9, 1, 2, 3, 0, time.UTC)}}, wantStatus: http.StatusCreated,
			assert: func(t *testing.T, manager *fakePasskeys, body string, header http.Header) {
				t.Helper()
				if manager.accountDID != "did:plc:alice" || manager.browserSessionID != passkeyTestSessionID || manager.token != "ceremony" || !strings.Contains(string(manager.response), `"attestationObject":"value"`) {
					t.Fatalf("verification binding = %q %s %q %s", manager.accountDID, manager.browserSessionID, manager.token, manager.response)
				}
				if header.Get("Location") != "/api/v1/passkeys/"+credentialID.String() || strings.Contains(body, "did:plc:alice") {
					t.Fatalf("response location/body = %q %q", header.Get("Location"), body)
				}
			},
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			server := testAPIServer(t, Dependencies{Sessions: passkeyTestSessions{}, Passkeys: testCase.manager})
			response := performAPIRequest(server, http.MethodPost, testCase.path, testCase.body, true, true, "")
			if response.Code != testCase.wantStatus {
				t.Fatalf("status = %d: %s", response.Code, response.Body.String())
			}
			testCase.assert(t, testCase.manager, response.Body.String(), response.Header())
		})
	}
}

func TestPasskeyLoginIsAnonymousAndRequiresExactOrigin(t *testing.T) {
	t.Parallel()
	testCases := []struct {
		name       string
		origin     string
		manager    *fakePasskeys
		wantStatus int
		wantCalls  int
	}{
		{name: "exact origin starts anonymous login", origin: "http://localhost:8080", manager: &fakePasskeys{beginResult: passkey.BeginResult{Options: json.RawMessage(`{"publicKey":{}}`), Token: "token"}}, wantStatus: http.StatusOK, wantCalls: 1},
		{name: "missing origin is forbidden", manager: &fakePasskeys{}, wantStatus: http.StatusForbidden},
		{name: "different origin is forbidden", origin: "http://localhost:8081", manager: &fakePasskeys{}, wantStatus: http.StatusForbidden},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			server := testAPIServer(t, Dependencies{Passkeys: testCase.manager})
			request := newPasskeyRequest(http.MethodPost, "/api/v1/passkeys/login/options", "", testCase.origin)
			response := performPasskeyRequest(server, request)
			if response.Code != testCase.wantStatus || testCase.manager.beginLoginCalls != testCase.wantCalls {
				t.Fatalf("status/calls = %d/%d: %s", response.Code, testCase.manager.beginLoginCalls, response.Body.String())
			}
		})
	}
}

func TestPasskeySessionOnlyEndpointsRejectPAT(t *testing.T) {
	t.Parallel()
	testCases := []struct {
		name   string
		method string
		path   string
		body   string
	}{
		{name: "registration", method: http.MethodPost, path: "/api/v1/passkeys/registration/options", body: `{"name":"Laptop"}`},
		{name: "list", method: http.MethodGet, path: "/api/v1/passkeys"},
		{name: "delete", method: http.MethodDelete, path: "/api/v1/passkeys/0198ad40-00f4-7ee3-9d71-d35f7e5ada02"},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			server := testAPIServer(t, Dependencies{TokenAuth: fakeTokenAuth{}, Passkeys: &fakePasskeys{}})
			response := performAPIRequest(server, testCase.method, testCase.path, testCase.body, false, true, "valid-pat")
			assertAPIError(t, response, http.StatusForbidden, "permission_denied")
		})
	}
}

func TestPasskeyVerifyBodyValidationAndErrorMapping(t *testing.T) {
	t.Parallel()
	largeResponse := `{"ceremony_token":"token","response":{"padding":"` + strings.Repeat("a", 40*1024) + `"}}`
	testCases := []struct {
		name        string
		path        string
		body        string
		contentType string
		manager     *fakePasskeys
		wantStatus  int
		wantCode    string
	}{
		{name: "response must be object", path: "/api/v1/passkeys/login/verify", body: `{"ceremony_token":"token","response":[]}`, contentType: "application/json", manager: &fakePasskeys{}, wantStatus: http.StatusBadRequest, wantCode: "malformed_request"},
		{name: "unknown outer field rejected", path: "/api/v1/passkeys/login/verify", body: `{"ceremony_token":"token","response":{},"did":"did:plc:mallory"}`, contentType: "application/json", manager: &fakePasskeys{}, wantStatus: http.StatusBadRequest, wantCode: "malformed_request"},
		{name: "one JSON value required", path: "/api/v1/passkeys/login/verify", body: `{"ceremony_token":"token","response":{}} {}`, contentType: "application/json", manager: &fakePasskeys{}, wantStatus: http.StatusBadRequest, wantCode: "malformed_request"},
		{name: "content type required", path: "/api/v1/passkeys/login/verify", body: `{"ceremony_token":"token","response":{}}`, contentType: "text/plain", manager: &fakePasskeys{}, wantStatus: http.StatusBadRequest, wantCode: "malformed_request"},
		{name: "verify permits valid body over 32KiB", path: "/api/v1/passkeys/login/verify", body: largeResponse, contentType: "application/json", manager: &fakePasskeys{login: passkey.LoginResult{SessionPlaintext: "session", SessionExpiresAt: time.Now().Add(time.Hour)}}, wantStatus: http.StatusNoContent},
		{name: "registration protocol validation is 422", path: "/api/v1/passkeys/registration/verify", body: `{"ceremony_token":"token","response":{}}`, contentType: "application/json", manager: &fakePasskeys{err: auth.ErrValidation}, wantStatus: http.StatusUnprocessableEntity, wantCode: "validation_failed"},
		{name: "login failure is generic 401", path: "/api/v1/passkeys/login/verify", body: `{"ceremony_token":"token","response":{}}`, contentType: "application/json", manager: &fakePasskeys{err: auth.ErrUnauthorized}, wantStatus: http.StatusUnauthorized, wantCode: "authentication_required"},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			server := testAPIServer(t, Dependencies{Sessions: passkeyTestSessions{}, Passkeys: testCase.manager})
			request := newPasskeyRequest(http.MethodPost, testCase.path, testCase.body, "http://localhost:8080")
			request.Header.Set("Content-Type", testCase.contentType)
			if strings.Contains(testCase.path, "/registration/") {
				request.AddCookie(&http.Cookie{Name: "adenosine_session", Value: "valid-session"})
			}
			response := performPasskeyRequest(server, request)
			if testCase.wantCode != "" {
				assertAPIError(t, response, testCase.wantStatus, testCase.wantCode)
			} else if response.Code != testCase.wantStatus {
				t.Fatalf("status = %d, want %d: %s", response.Code, testCase.wantStatus, response.Body.String())
			}
		})
	}
}

func TestPasskeyListReturnsMetadataOnlyAndDeleteUsesOwner(t *testing.T) {
	t.Parallel()
	id := uuid.MustParse("0198ad40-00f4-7ee3-9d71-d35f7e5ada02")
	testCases := []struct {
		name       string
		method     string
		path       string
		manager    *fakePasskeys
		wantStatus int
		assert     func(*testing.T, *fakePasskeys, string)
	}{
		{name: "list redacts credential material", method: http.MethodGet, path: "/api/v1/passkeys", manager: &fakePasskeys{listed: []passkey.CredentialSummary{{ID: id, Name: "Laptop", CreatedAt: time.Date(2026, 8, 9, 1, 2, 3, 0, time.UTC)}}}, wantStatus: http.StatusOK, assert: func(t *testing.T, manager *fakePasskeys, body string) {
			t.Helper()
			if manager.accountDID != "did:plc:alice" || !strings.Contains(body, `"name":"Laptop"`) || strings.Contains(body, "credential_id") || strings.Contains(body, "public_key") || strings.Contains(body, "account_did") {
				t.Fatalf("list DID/body = %q %s", manager.accountDID, body)
			}
		}},
		{name: "delete scopes revocation to owner", method: http.MethodDelete, path: "/api/v1/passkeys/" + id.String(), manager: &fakePasskeys{}, wantStatus: http.StatusNoContent, assert: func(t *testing.T, manager *fakePasskeys, _ string) {
			t.Helper()
			if manager.accountDID != "did:plc:alice" || manager.revokedID != id {
				t.Fatalf("revocation = %q %s", manager.accountDID, manager.revokedID)
			}
		}},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			server := testAPIServer(t, Dependencies{Sessions: passkeyTestSessions{}, Passkeys: testCase.manager})
			response := performAPIRequest(server, testCase.method, testCase.path, "", true, true, "")
			if response.Code != testCase.wantStatus {
				t.Fatalf("status = %d: %s", response.Code, response.Body.String())
			}
			testCase.assert(t, testCase.manager, response.Body.String())
		})
	}
}

func TestOAuthAndPasskeySessionCookiesAreIdentical(t *testing.T) {
	t.Parallel()
	expiresAt := time.Date(2026, 8, 10, 1, 2, 3, 0, time.UTC)
	testCases := []struct {
		name string
		path string
		body string
		deps Dependencies
	}{
		{name: "OAuth", path: "/oauth/atproto/callback?state=state&code=code", deps: Dependencies{Login: &fakeLogin{result: localidentity.LoginResult{SessionPlaintext: "new-session", SessionExpiresAt: expiresAt}}}},
		{name: "passkey", path: "/api/v1/passkeys/login/verify", body: `{"ceremony_token":"token","response":{}}`, deps: Dependencies{Passkeys: &fakePasskeys{login: passkey.LoginResult{SessionPlaintext: "new-session", SessionExpiresAt: expiresAt}}}},
	}
	cookies := make([]*http.Cookie, 0, len(testCases))
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			server := testAPIServer(t, testCase.deps)
			method := http.MethodGet
			if testCase.body != "" {
				method = http.MethodPost
			}
			request := newPasskeyRequest(method, testCase.path, testCase.body, "http://localhost:8080")
			response := performPasskeyRequest(server, request)
			resultCookies := response.Result().Cookies()
			if len(resultCookies) != 1 {
				t.Fatalf("cookies = %#v", resultCookies)
			}
			cookies = append(cookies, resultCookies[0])
		})
	}
	if !reflect.DeepEqual(cookies[0], cookies[1]) {
		t.Fatalf("OAuth cookie = %#v, passkey cookie = %#v", cookies[0], cookies[1])
	}
}

func newPasskeyRequest(method, path, body, origin string) *http.Request {
	request, _ := http.NewRequest(method, path, strings.NewReader(body))
	if body != "" {
		request.Header.Set("Content-Type", "application/json")
	}
	if origin != "" {
		request.Header.Set("Origin", origin)
	}
	return request
}

func performPasskeyRequest(server *http.Server, request *http.Request) *httptest.ResponseRecorder {
	response := httptest.NewRecorder()
	server.Handler.ServeHTTP(response, request)
	return response
}
