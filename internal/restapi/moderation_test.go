package restapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	generated "github.com/adenosine-dev/adenosine/api/generated/go"
	"github.com/adenosine-dev/adenosine/internal/issue"
	"github.com/adenosine-dev/adenosine/internal/moderation"
)

type fakeModeration struct {
	blocks      []moderation.BlockedDID
	hidden      []moderation.HiddenRecord
	err         error
	operation   string
	accountDIDs []string
	target      string
	calls       int
}

func (manager *fakeModeration) record(operation, accountDID, target string) error {
	manager.calls++
	manager.operation, manager.target = operation, target
	manager.accountDIDs = append(manager.accountDIDs, accountDID)
	return manager.err
}

func (manager *fakeModeration) Block(_ context.Context, accountDID, blockedDID string) error {
	return manager.record("block", accountDID, blockedDID)
}
func (manager *fakeModeration) Unblock(_ context.Context, accountDID, blockedDID string) error {
	return manager.record("unblock", accountDID, blockedDID)
}
func (manager *fakeModeration) ListBlocks(_ context.Context, accountDID string) ([]moderation.BlockedDID, error) {
	if err := manager.record("list-blocks", accountDID, ""); err != nil {
		return nil, err
	}
	return manager.blocks, nil
}
func (manager *fakeModeration) Hide(_ context.Context, accountDID, recordURI string) error {
	return manager.record("hide", accountDID, recordURI)
}
func (manager *fakeModeration) Unhide(_ context.Context, accountDID, recordURI string) error {
	return manager.record("unhide", accountDID, recordURI)
}
func (manager *fakeModeration) ListHidden(_ context.Context, accountDID string) ([]moderation.HiddenRecord, error) {
	if err := manager.record("list-hidden", accountDID, ""); err != nil {
		return nil, err
	}
	return manager.hidden, nil
}

func TestModerationEndpoints(t *testing.T) {
	t.Parallel()
	testCases := []struct {
		name          string
		method        string
		path          string
		body          string
		session       bool
		pat           bool
		origin        string
		manager       *fakeModeration
		wantStatus    int
		wantCalls     int
		wantOperation string
		wantTarget    string
		wantCode      string
	}{
		{name: "list is session only and owner scoped", method: http.MethodGet, path: "/api/v1/moderation", session: true, manager: &fakeModeration{blocks: []moderation.BlockedDID{{DID: "did:plc:bob"}}, hidden: []moderation.HiddenRecord{{URI: restCommentURI}}}, wantStatus: http.StatusOK, wantCalls: 2, wantOperation: "list-hidden"},
		{name: "list rejects PAT", method: http.MethodGet, path: "/api/v1/moderation", pat: true, manager: &fakeModeration{}, wantStatus: http.StatusForbidden, wantCode: "permission_denied"},
		{name: "block derives owner from session", method: http.MethodPut, path: "/api/v1/moderation/blocked-dids", body: `{"blocked_did":"did:plc:bob"}`, session: true, origin: "http://localhost:8080", manager: &fakeModeration{}, wantStatus: http.StatusNoContent, wantCalls: 1, wantOperation: "block", wantTarget: "did:plc:bob"},
		{name: "unblock derives owner from session", method: http.MethodDelete, path: "/api/v1/moderation/blocked-dids?blocked_did=did:plc:bob", session: true, origin: "http://localhost:8080", manager: &fakeModeration{}, wantStatus: http.StatusNoContent, wantCalls: 1, wantOperation: "unblock", wantTarget: "did:plc:bob"},
		{name: "hide derives owner from session", method: http.MethodPut, path: "/api/v1/moderation/hidden-records", body: `{"record_uri":"` + restCommentURI + `"}`, session: true, origin: "http://localhost:8080", manager: &fakeModeration{}, wantStatus: http.StatusNoContent, wantCalls: 1, wantOperation: "hide", wantTarget: restCommentURI},
		{name: "unhide derives owner from session", method: http.MethodDelete, path: "/api/v1/moderation/hidden-records?record_uri=" + restCommentURI, session: true, origin: "http://localhost:8080", manager: &fakeModeration{}, wantStatus: http.StatusNoContent, wantCalls: 1, wantOperation: "unhide", wantTarget: restCommentURI},
		{name: "mutation rejects PAT", method: http.MethodPut, path: "/api/v1/moderation/blocked-dids", body: `{"blocked_did":"did:plc:bob"}`, pat: true, origin: "http://localhost:8080", manager: &fakeModeration{}, wantStatus: http.StatusForbidden, wantCode: "permission_denied"},
		{name: "mutation requires exact Origin", method: http.MethodPut, path: "/api/v1/moderation/hidden-records", body: `{"record_uri":"` + restCommentURI + `"}`, session: true, origin: "https://localhost:8080", manager: &fakeModeration{}, wantStatus: http.StatusForbidden, wantCode: "permission_denied"},
		{name: "account DID cannot be injected", method: http.MethodPut, path: "/api/v1/moderation/blocked-dids", body: `{"blocked_did":"did:plc:bob","account_did":"did:plc:mallory"}`, session: true, origin: "http://localhost:8080", manager: &fakeModeration{}, wantStatus: http.StatusBadRequest},
		{name: "validation maps without details", method: http.MethodPut, path: "/api/v1/moderation/blocked-dids", body: `{"blocked_did":"did:plc:bob"}`, session: true, origin: "http://localhost:8080", manager: &fakeModeration{err: &issue.ValidationError{Field: "blockedDID", Problem: "secret"}}, wantStatus: http.StatusUnprocessableEntity, wantCalls: 1, wantOperation: "block", wantTarget: "did:plc:bob", wantCode: "validation_failed"},
		{name: "not found maps consistently", method: http.MethodDelete, path: "/api/v1/moderation/hidden-records?record_uri=" + restCommentURI, session: true, origin: "http://localhost:8080", manager: &fakeModeration{err: issue.ErrNotFound}, wantStatus: http.StatusNotFound, wantCalls: 1, wantOperation: "unhide", wantTarget: restCommentURI, wantCode: "not_found"},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			server := testAPIServer(t, Dependencies{Sessions: fakeSessions{}, TokenAuth: fakeTokenAuth{}, Moderation: testCase.manager})
			request := httptest.NewRequest(testCase.method, testCase.path, strings.NewReader(testCase.body))
			if testCase.body != "" {
				request.Header.Set("Content-Type", "application/json")
			}
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
			if response.Code != testCase.wantStatus || testCase.manager.calls != testCase.wantCalls || testCase.manager.operation != testCase.wantOperation || testCase.manager.target != testCase.wantTarget {
				t.Fatalf("status/calls/operation/target = %d/%d/%q/%q, want %d/%d/%q/%q; body=%s", response.Code, testCase.manager.calls, testCase.manager.operation, testCase.manager.target, testCase.wantStatus, testCase.wantCalls, testCase.wantOperation, testCase.wantTarget, response.Body.String())
			}
			for _, accountDID := range testCase.manager.accountDIDs {
				if accountDID != "did:plc:alice" {
					t.Fatalf("moderation account DID = %q", accountDID)
				}
			}
			if testCase.wantStatus == http.StatusOK {
				var body generated.Moderation
				if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil || len(body.BlockedDids) != 1 || body.BlockedDids[0] != "did:plc:bob" || len(body.HiddenRecords) != 1 || body.HiddenRecords[0] != restCommentURI {
					t.Fatalf("moderation response = %#v, %v", body, err)
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
