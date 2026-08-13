package restapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/adenosine-dev/adenosine/internal/owner"
)

type fakeOwnerResolver struct {
	value owner.Owner
	err   error
}

func (resolver fakeOwnerResolver) Resolve(context.Context, string) (owner.Owner, error) {
	return resolver.value, resolver.err
}

func TestGetOwner(t *testing.T) {
	testCases := []struct {
		name       string
		owner      string
		resolver   OwnerResolver
		wantStatus int
		wantBody   map[string]any
	}{
		{
			name: "account", owner: "alice.example", wantStatus: http.StatusOK,
			resolver: fakeOwnerResolver{value: owner.Owner{Kind: owner.KindAccount, CanonicalName: "alice.example", AccountDID: "did:plc:alice"}},
			wantBody: map[string]any{"kind": "account", "canonical_name": "alice.example", "account_did": "did:plc:alice"},
		},
		{
			name: "organization", owner: "adenosine", wantStatus: http.StatusOK,
			resolver: fakeOwnerResolver{value: owner.Owner{Kind: owner.KindOrganization, CanonicalName: "adenosine", OrganizationSlug: "adenosine"}},
			wantBody: map[string]any{"kind": "organization", "canonical_name": "adenosine", "organization_slug": "adenosine"},
		},
		{
			name: "missing", owner: "missing", wantStatus: http.StatusNotFound,
			resolver: fakeOwnerResolver{err: owner.ErrNotFound},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			server := testAPIServer(t, Dependencies{Owners: testCase.resolver})
			request := httptest.NewRequest(http.MethodGet, "/api/v1/owners/"+testCase.owner, nil)
			response := httptest.NewRecorder()
			server.Handler.ServeHTTP(response, request)
			if response.Code != testCase.wantStatus {
				t.Fatalf("status = %d, want %d; body = %s", response.Code, testCase.wantStatus, response.Body.String())
			}
			if testCase.wantBody == nil {
				return
			}
			var body map[string]any
			if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
				t.Fatalf("decode response: %v", err)
			}
			if !mapsEqual(body, testCase.wantBody) {
				t.Fatalf("body = %#v, want %#v", body, testCase.wantBody)
			}
		})
	}
}

func mapsEqual(left, right map[string]any) bool {
	if len(left) != len(right) {
		return false
	}
	for key, value := range left {
		if right[key] != value {
			return false
		}
	}
	return true
}
