package restapi

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/adenosine-dev/adenosine/internal/syncproxy"
)

type failingSyncProxy struct{ err error }

func (proxy failingSyncProxy) Forward(http.ResponseWriter, *http.Request) error { return proxy.err }

func TestSyncProxyAPIErrors(t *testing.T) {
	t.Parallel()
	testCases := []struct {
		name       string
		dependency SyncRepositoryProxy
		wantStatus int
		wantCode   string
	}{
		{name: "missing dependency", wantStatus: http.StatusServiceUnavailable, wantCode: "sync_disabled"},
		{name: "disabled", dependency: failingSyncProxy{err: syncproxy.ErrDisabled}, wantStatus: http.StatusServiceUnavailable, wantCode: "sync_disabled"},
		{name: "malformed", dependency: failingSyncProxy{err: syncproxy.ErrMalformed}, wantStatus: http.StatusBadRequest, wantCode: "malformed_request"},
		{name: "too large", dependency: failingSyncProxy{err: syncproxy.ErrBodyTooLarge}, wantStatus: http.StatusRequestEntityTooLarge, wantCode: "request_body_too_large"},
		{name: "unavailable", dependency: failingSyncProxy{err: errors.New("upstream https://private.invalid?secret=secret")}, wantStatus: http.StatusBadGateway, wantCode: "sync_unavailable"},
	}
	for _, tt := range testCases {
		t.Run(tt.name, func(t *testing.T) {
			server := testAPIServer(t, Dependencies{SyncRepositories: tt.dependency})
			request := httptest.NewRequest(http.MethodGet, "/api/v1/sync/repositories?offset=-1", nil)
			response := httptest.NewRecorder()
			server.Handler.ServeHTTP(response, request)
			assertAPIError(t, response, tt.wantStatus, tt.wantCode)
			if response.Header().Get("Vary") != "Cookie, Authorization" || strings.Contains(response.Body.String(), "private.invalid") || strings.Contains(response.Body.String(), "secret") {
				t.Fatalf("unsafe sync error response: headers=%v body=%s", response.Header(), response.Body.String())
			}
		})
	}
}
