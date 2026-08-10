package syncproxy

import (
	"bufio"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestProxyOwnsRepositoryShape(t *testing.T) {
	t.Parallel()
	testCases := []struct{ name string }{{name: "owns repository shape"}}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			requests := make(chan *http.Request, 1)
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests <- r.Clone(r.Context())
				w.Header().Set("Electric-Handle", "new-handle")
				w.Header().Set("Electric-Offset", "4_0")
				w.Header().Set("Electric-Cursor", "cursor-2")
				w.Header().Set("Electric-Schema", `{"uri":{"type":"text"}}`)
				w.Header().Set("Cache-Control", "public, max-age=5")
				w.Header().Set("ETag", `"shape-etag"`)
				w.Header().Set("Set-Cookie", "upstream=must-not-escape")
				w.WriteHeader(http.StatusConflict)
				_, _ = w.Write([]byte(`[{"headers":{"control":"must-refetch"}}]`))
			}))
			defer upstream.Close()

			proxy, err := build(upstream.URL+"/electric", "server-secret", nil)
			if err != nil {
				t.Fatalf("build proxy: %v", err)
			}
			request := httptest.NewRequest(http.MethodGet, "/api/v1/sync/repositories?offset=3_0&handle=old&cursor=cursor-1&cache-buster=recovery-1&expired_handle=older&live=true&live_sse=false&replica=full&log=changes_only", nil)
			request.Header.Set("If-None-Match", `"old-etag"`)
			response := httptest.NewRecorder()
			if err := proxy.Forward(response, request); err != nil {
				t.Fatalf("forward: %v", err)
			}
			upstreamRequest := <-requests
			if upstreamRequest.URL.Path != "/electric/v1/shape" || upstreamRequest.Header.Get("If-None-Match") != `"old-etag"` {
				t.Fatalf("upstream request = %s, headers = %v", upstreamRequest.URL.String(), upstreamRequest.Header)
			}
			query := upstreamRequest.URL.Query()
			assertQueryValue(t, query, "table", table)
			assertQueryValue(t, query, "columns", columns)
			assertQueryValue(t, query, "queryable_columns", columns)
			assertQueryValue(t, query, "where", mainWhere)
			assertQueryValue(t, query, "secret", "server-secret")
			for _, key := range []string{"offset", "handle", "cursor", "cache-buster", "expired_handle", "live", "live_sse", "replica", "log"} {
				if query.Get(key) == "" {
					t.Fatalf("continuation parameter %q was not forwarded: %v", key, query)
				}
			}
			if response.Code != http.StatusConflict || response.Header().Get("Electric-Handle") != "new-handle" || response.Header().Get("Electric-Offset") != "4_0" || response.Header().Get("ETag") != `"shape-etag"` {
				t.Fatalf("response = %d, headers = %v", response.Code, response.Header())
			}
			if response.Header().Get("Set-Cookie") != "" || response.Header().Get("Vary") != "Cookie, Authorization" {
				t.Fatalf("unsafe or missing response headers: %v", response.Header())
			}
		})
	}
}

func TestProxyRejectsShapeOverridesAndInvalidContinuation(t *testing.T) {
	t.Parallel()
	proxy, err := build("https://electric.example", "secret", roundTripFunc(func(*http.Request) (*http.Response, error) {
		t.Fatal("invalid request reached upstream")
		return nil, nil
	}))
	if err != nil {
		t.Fatalf("build proxy: %v", err)
	}
	testCases := []struct {
		name, method, rawQuery string
	}{
		{name: "table override", method: http.MethodGet, rawQuery: "offset=-1&table=core.accounts"},
		{name: "columns override", method: http.MethodGet, rawQuery: "offset=-1&columns=uri%2Cpassword_hash"},
		{name: "where override", method: http.MethodGet, rawQuery: "offset=-1&where=TRUE"},
		{name: "secret override", method: http.MethodGet, rawQuery: "offset=-1&secret=stolen"},
		{name: "subset where query", method: http.MethodGet, rawQuery: "offset=-1&subset__where=TRUE"},
		{name: "duplicate offset", method: http.MethodGet, rawQuery: "offset=-1&offset=0_0"},
		{name: "invalid live", method: http.MethodGet, rawQuery: "offset=-1&live=1"},
		{name: "invalid replica", method: http.MethodGet, rawQuery: "offset=-1&replica=everything"},
		{name: "invalid log", method: http.MethodGet, rawQuery: "offset=-1&log=history"},
		{name: "long cache buster", method: http.MethodGet, rawQuery: "offset=-1&cache-buster=" + strings.Repeat("x", 256)},
		{name: "missing offset", method: http.MethodGet, rawQuery: "handle=missing-offset"},
		{name: "POST-only allowlist", method: http.MethodPost, rawQuery: "offset=-1&live=true"},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			request := httptest.NewRequest(testCase.method, "/api/v1/sync/repositories?"+testCase.rawQuery, nil)
			if err := proxy.Forward(httptest.NewRecorder(), request); !errors.Is(err, ErrMalformed) {
				t.Fatalf("error = %v, want malformed", err)
			}
		})
	}
}

func TestProxyPOSTSubsetValidation(t *testing.T) {
	t.Parallel()
	testCases := []struct{ name string }{{name: "validates POST subsets"}}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			var upstreamBody string
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				body, _ := io.ReadAll(r.Body)
				upstreamBody = string(body)
				if r.Method != http.MethodPost || r.URL.Query().Get("where") != mainWhere || r.URL.Query().Get("table") != table {
					t.Errorf("upstream method/query = %s %v", r.Method, r.URL.Query())
				}
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"data":[],"metadata":{}}`))
			}))
			defer upstream.Close()
			proxy, err := build(upstream.URL, "secret", nil)
			if err != nil {
				t.Fatalf("build proxy: %v", err)
			}

			validBody := `{"where":"owner_did = $1","params":{"1":"did:plc:alice"},"order_by":"indexed_at DESC","limit":20,"offset":0}`
			valid := httptest.NewRequest(http.MethodPost, "/api/v1/sync/repositories?offset=now&log=changes_only", strings.NewReader(validBody))
			valid.Header.Set("Content-Type", "application/json; charset=utf-8")
			if err := proxy.Forward(httptest.NewRecorder(), valid); err != nil {
				t.Fatalf("valid POST error = %v, body = %q", err, upstreamBody)
			}
			var forwarded subsetRequest
			if err := json.Unmarshal([]byte(upstreamBody), &forwarded); err != nil {
				t.Fatalf("decode forwarded subset: %v", err)
			}
			if forwarded.Where == nil || *forwarded.Where != "owner_did = $1" || forwarded.Params["1"] != "did:plc:alice" {
				t.Fatalf("forwarded subset = %+v, want narrowing predicate", forwarded)
			}

			empty := httptest.NewRequest(http.MethodPost, "/api/v1/sync/repositories?offset=-1", nil)
			if err := proxy.Forward(httptest.NewRecorder(), empty); err != nil || upstreamBody != "" {
				t.Fatalf("empty POST error = %v, body = %q", err, upstreamBody)
			}

			validationCases := []struct {
				name, body, contentType string
				want                    error
			}{
				{name: "unknown field", body: `{"table":"core.accounts"}`, contentType: "application/json", want: ErrMalformed},
				{name: "bad param key", body: `{"where":"uri = $1","params":{"zero":"x"}}`, contentType: "application/json", want: ErrMalformed},
				{name: "pagination without order", body: `{"limit":10}`, contentType: "application/json", want: ErrMalformed},
				{name: "limit too large", body: `{"order_by":"uri","limit":1001}`, contentType: "application/json", want: ErrMalformed},
				{name: "wrong content type", body: `{}`, contentType: "text/plain", want: ErrMalformed},
				{name: "body too large", body: strings.Repeat("x", maxSubsetBody+1), contentType: "application/json", want: ErrBodyTooLarge},
			}
			for _, validationCase := range validationCases {
				t.Run(validationCase.name, func(t *testing.T) {
					request := httptest.NewRequest(http.MethodPost, "/api/v1/sync/repositories?offset=-1", strings.NewReader(validationCase.body))
					request.Header.Set("Content-Type", validationCase.contentType)
					if err := proxy.Forward(httptest.NewRecorder(), request); !errors.Is(err, validationCase.want) {
						t.Fatalf("error = %v, want %v", err, validationCase.want)
					}
				})
			}
		})
	}
}

func TestProxyDecompressesAndRemovesStaleHeaders(t *testing.T) {
	t.Parallel()
	testCases := []struct{ name string }{{name: "decompresses response"}}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Encoding", "gzip")
				w.Header().Set("Content-Type", "application/json")
				writer := gzip.NewWriter(w)
				_, _ = writer.Write([]byte(`[]`))
				_ = writer.Close()
			}))
			defer upstream.Close()
			proxy, _ := build(upstream.URL, "secret", nil)
			response := httptest.NewRecorder()
			if err := proxy.Forward(response, httptest.NewRequest(http.MethodGet, "/?offset=-1", nil)); err != nil {
				t.Fatalf("forward: %v", err)
			}
			if response.Body.String() != `[]` || response.Header().Get("Content-Encoding") != "" || response.Header().Get("Content-Length") != "" {
				t.Fatalf("body = %q, headers = %v", response.Body.String(), response.Header())
			}
		})
	}
}

func TestProxyStreamsAndCancelsUpstream(t *testing.T) {
	t.Parallel()
	testCases := []struct{ name string }{{name: "streams and cancels upstream"}}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			release := make(chan struct{})
			canceled := make(chan struct{})
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "text/event-stream")
				_, _ = w.Write([]byte("data: first\n\n"))
				w.(http.Flusher).Flush()
				select {
				case <-release:
					_, _ = w.Write([]byte("data: second\n\n"))
				case <-r.Context().Done():
					close(canceled)
				}
			}))
			defer upstream.Close()
			proxy, _ := build(upstream.URL, "secret", nil)
			public := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_ = proxy.Forward(w, r)
			}))
			defer public.Close()

			ctx, cancel := context.WithCancel(context.Background())
			request, _ := http.NewRequestWithContext(ctx, http.MethodGet, public.URL+"/?offset=-1&live=true&live_sse=true", nil)
			response, err := public.Client().Do(request)
			if err != nil {
				t.Fatalf("request stream: %v", err)
			}
			reader := bufio.NewReader(response.Body)
			line, err := reader.ReadString('\n')
			if err != nil || line != "data: first\n" {
				t.Fatalf("first streamed line = %q, error = %v", line, err)
			}
			cancel()
			_ = response.Body.Close()
			select {
			case <-canceled:
			case <-time.After(2 * time.Second):
				t.Fatal("upstream request was not canceled")
			}
		})
	}
}

func TestProxyUnavailableDoesNotLeakDetails(t *testing.T) {
	t.Parallel()
	testCases := []struct{ name string }{{name: "redacts upstream details"}}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			proxy, _ := build("https://electric.internal.example/private", "super-secret", roundTripFunc(func(*http.Request) (*http.Response, error) {
				return nil, errors.New("dial https://electric.internal.example/private?secret=super-secret")
			}))
			err := proxy.Forward(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/?offset=-1", nil))
			if !errors.Is(err, ErrUnavailable) || strings.Contains(err.Error(), "internal.example") || strings.Contains(err.Error(), "super-secret") {
				t.Fatalf("unsafe unavailable error: %v", err)
			}
		})
	}
}

func assertQueryValue(t *testing.T, query url.Values, key, want string) {
	t.Helper()
	if values := query[key]; len(values) != 1 || values[0] != want {
		t.Fatalf("query[%q] = %v, want %q", key, values, want)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (function roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return function(request)
}
