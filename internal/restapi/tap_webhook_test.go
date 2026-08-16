package restapi

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type fakeFederationProcessor struct {
	body  []byte
	err   error
	calls int
}

func (processor *fakeFederationProcessor) Process(_ context.Context, body []byte) error {
	processor.calls++
	processor.body = append([]byte(nil), body...)
	return processor.err
}

func TestTapWebhookHandler(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name                    string
		method                  string
		body                    string
		contentType             string
		username                string
		password                string
		emptyConfiguredPassword bool
		processorErr            error
		wantStatus              int
		wantCalls               int
		wantAllow               string
		wantChallenge           bool
	}{
		{name: "applied event", method: http.MethodPost, body: `{"type":"record"}`, contentType: "application/json", username: "admin", password: "tap-secret", wantStatus: http.StatusNoContent, wantCalls: 1},
		{name: "duplicate event", method: http.MethodPost, body: `{"type":"record","id":1}`, contentType: "application/json; charset=utf-8", username: "admin", password: "tap-secret", wantStatus: http.StatusNoContent, wantCalls: 1},
		{name: "rejected event", method: http.MethodPost, body: `{"type":"unknown"}`, contentType: "application/json", username: "admin", password: "tap-secret", wantStatus: http.StatusNoContent, wantCalls: 1},
		{name: "transient processor failure", method: http.MethodPost, body: `{"private":"raw-payload"}`, contentType: "application/json", username: "admin", password: "tap-secret", processorErr: errors.New("secret processor detail"), wantStatus: http.StatusServiceUnavailable, wantCalls: 1},
		{name: "wrong username", method: http.MethodPost, body: `{}`, contentType: "application/json", username: "operator", password: "tap-secret", wantStatus: http.StatusUnauthorized, wantChallenge: true},
		{name: "wrong password", method: http.MethodPost, body: `{}`, contentType: "application/json", username: "admin", password: "wrong", wantStatus: http.StatusUnauthorized, wantChallenge: true},
		{name: "missing authorization", method: http.MethodPost, body: `{}`, contentType: "application/json", wantStatus: http.StatusUnauthorized, wantChallenge: true},
		{name: "empty configured password", method: http.MethodPost, body: `{}`, contentType: "application/json", username: "admin", emptyConfiguredPassword: true, wantStatus: http.StatusUnauthorized, wantChallenge: true},
		{name: "non JSON content type", method: http.MethodPost, body: `{}`, contentType: "text/plain", username: "admin", password: "tap-secret", wantStatus: http.StatusUnsupportedMediaType},
		{name: "malformed JSON", method: http.MethodPost, body: `{"type":`, contentType: "application/json", username: "admin", password: "tap-secret", wantStatus: http.StatusBadRequest},
		{name: "body too large", method: http.MethodPost, body: `"` + strings.Repeat("x", maxTapWebhookBody) + `"`, contentType: "application/json", username: "admin", password: "tap-secret", wantStatus: http.StatusRequestEntityTooLarge},
		{name: "non POST method", method: http.MethodGet, contentType: "application/json", username: "admin", password: "tap-secret", wantStatus: http.StatusMethodNotAllowed, wantAllow: http.MethodPost},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			configuredPassword := "tap-secret"
			if testCase.emptyConfiguredPassword {
				configuredPassword = ""
			}
			processor := &fakeFederationProcessor{err: testCase.processorErr}
			server, err := NewServer(":0", "http://localhost:8080", fakeReadiness{}, slog.New(slog.NewTextHandler(io.Discard, nil)), Observability{}, Dependencies{
				Federation: &FederationDependencies{Processor: processor, TapAdminPassword: configuredPassword},
			}, nil)
			if err != nil {
				t.Fatalf("create server: %v", err)
			}

			request := httptest.NewRequest(testCase.method, "/internal/federation/tap", strings.NewReader(testCase.body))
			if testCase.contentType != "" {
				request.Header.Set("Content-Type", testCase.contentType)
			}
			if testCase.username != "" || testCase.password != "" {
				request.SetBasicAuth(testCase.username, testCase.password)
			}
			response := httptest.NewRecorder()
			server.Handler.ServeHTTP(response, request)

			if response.Code != testCase.wantStatus {
				t.Fatalf("status = %d, want %d", response.Code, testCase.wantStatus)
			}
			if processor.calls != testCase.wantCalls {
				t.Fatalf("processor calls = %d, want %d", processor.calls, testCase.wantCalls)
			}
			if testCase.wantCalls == 1 && string(processor.body) != testCase.body {
				t.Fatalf("processor body = %q, want %q", processor.body, testCase.body)
			}
			if response.Header().Get("Allow") != testCase.wantAllow {
				t.Fatalf("Allow = %q, want %q", response.Header().Get("Allow"), testCase.wantAllow)
			}
			if got := response.Header().Get("WWW-Authenticate") != ""; got != testCase.wantChallenge {
				t.Fatalf("authentication challenge present = %v, want %v", got, testCase.wantChallenge)
			}
			if strings.Contains(response.Body.String(), "secret processor detail") || strings.Contains(response.Body.String(), "raw-payload") {
				t.Fatalf("response leaked internal detail: %s", response.Body.String())
			}
		})
	}
}
