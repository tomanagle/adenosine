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
	"os"
	"strings"
	"testing"
	"time"
)

func TestProxyOwnsRegisteredShapes(t *testing.T) {
	t.Parallel()
	testCases := []struct {
		name, wantTable, wantColumns, wantWhere, wantActor, wantRecord string
		shape                                                          Shape
	}{
		{name: "repositories", shape: Repositories, wantTable: "network.repositories", wantColumns: "uri,cid,owner_did,slug,name,description,default_branch,git_https,git_ssh,web,forked_from_uri,forked_from_cid,transferred_from_uri,transferred_from_cid,transferred_to_uri,transferred_to_cid,lineage_uri,canonical_uri,record_created_at,record_updated_at,indexed_at,star_count,issue_count,open_issue_count,comment_count,pull_request_count,open_pull_request_count,fork_count", wantWhere: "deleted_at IS NULL AND cid IS NOT NULL", wantActor: "owner_did", wantRecord: "uri"},
		{name: "profiles", shape: Profiles, wantTable: "network.profiles", wantColumns: "did,profile_uri,profile_cid,handle,display_name,bio,avatar_ref,website,location,repository_count,contribution_count,record_created_at,indexed_at", wantWhere: "deleted_at IS NULL AND profile_cid IS NOT NULL", wantActor: "did", wantRecord: "profile_uri"},
		{name: "stars", shape: Stars, wantTable: "network.stars", wantColumns: "uri,cid,author_did,repository_uri,repository_cid,record_created_at,indexed_at", wantWhere: "deleted_at IS NULL AND cid IS NOT NULL", wantActor: "author_did", wantRecord: "uri"},
		{name: "issues", shape: Issues, wantTable: "network.issues", wantColumns: "uri,cid,author_did,repository_uri,repository_cid,title,body,state,status_uri,status_cid,status_updated_at,comment_count,record_created_at,record_updated_at,indexed_at", wantWhere: "deleted_at IS NULL AND cid IS NOT NULL", wantActor: "author_did", wantRecord: "uri"},
		{name: "issue comments", shape: IssueComments, wantTable: "network.issue_comments", wantColumns: "uri,cid,author_did,issue_uri,issue_cid,parent_uri,parent_cid,body,record_created_at,record_updated_at,indexed_at", wantWhere: "deleted_at IS NULL AND cid IS NOT NULL", wantActor: "author_did", wantRecord: "uri"},
		{name: "pull requests", shape: PullRequests, wantTable: "network.pull_requests", wantColumns: "uri,cid,author_did,source_repository_uri,source_repository_cid,source_branch,target_repository_uri,target_repository_cid,target_branch,head_sha,title,body,state,status_uri,status_cid,status_updated_at,merged_commit_sha,review_count,record_created_at,record_updated_at,indexed_at", wantWhere: "deleted_at IS NULL AND cid IS NOT NULL", wantActor: "author_did", wantRecord: "uri"},
		{name: "pull request reviews", shape: PullRequestReviews, wantTable: "network.pull_request_reviews", wantColumns: "uri,cid,author_did,pull_request_uri,pull_request_cid,verdict,body,record_created_at,record_updated_at,indexed_at", wantWhere: "deleted_at IS NULL AND cid IS NOT NULL", wantActor: "author_did", wantRecord: "uri"},
	}
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
			if err := proxy.Forward(response, request, testCase.shape, Policy{}); err != nil {
				t.Fatalf("forward: %v", err)
			}
			upstreamRequest := <-requests
			if upstreamRequest.URL.Path != "/electric/v1/shape" || upstreamRequest.Header.Get("If-None-Match") != `"old-etag"` {
				t.Fatalf("upstream request = %s, headers = %v", upstreamRequest.URL.String(), upstreamRequest.Header)
			}
			query := upstreamRequest.URL.Query()
			assertQueryValue(t, query, "table", testCase.wantTable)
			assertQueryValue(t, query, "columns", testCase.wantColumns)
			assertQueryValue(t, query, "queryable_columns", testCase.wantColumns)
			assertQueryValue(t, query, "where", testCase.wantWhere)
			definition := shapes[testCase.shape]
			if definition.actorColumn != testCase.wantActor || definition.recordColumn != testCase.wantRecord {
				t.Fatalf("moderation columns = %q/%q, want %q/%q", definition.actorColumn, definition.recordColumn, testCase.wantActor, testCase.wantRecord)
			}
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

func TestPolicyWhereUsesRegisteredModerationColumns(t *testing.T) {
	t.Parallel()
	testCases := []struct {
		name, actor, record string
		shape               Shape
	}{
		{name: "repositories", shape: Repositories, actor: "owner_did", record: "uri"},
		{name: "profiles", shape: Profiles, actor: "did", record: "profile_uri"},
		{name: "stars", shape: Stars, actor: "author_did", record: "uri"},
		{name: "issues", shape: Issues, actor: "author_did", record: "uri"},
		{name: "issue comments", shape: IssueComments, actor: "author_did", record: "uri"},
		{name: "pull requests", shape: PullRequests, actor: "author_did", record: "uri"},
		{name: "pull request reviews", shape: PullRequestReviews, actor: "author_did", record: "uri"},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			definition := shapes[testCase.shape]
			where, params := policyWhere(definition, Policy{
				BlockedDIDs:      []string{"did:plc:o'hare", "did:plc:bob"},
				HiddenRecordURIs: []string{"at://did:plc:alice/dev.adenosine.repo/it's-safe"},
			})
			wantRecordPolicy := testCase.record + " <> $3"
			if testCase.shape == Profiles {
				wantRecordPolicy = "(" + testCase.record + " IS NULL OR " + testCase.record + " <> $3)"
			}
			wantWhere := definition.where + " AND " + testCase.actor + " <> $1 AND " + testCase.actor + " <> $2 AND " + wantRecordPolicy
			if where != wantWhere || params["params[1]"] != "did:plc:bob" || params["params[2]"] != "did:plc:o'hare" || params["params[3]"] != "at://did:plc:alice/dev.adenosine.repo/it's-safe" {
				t.Fatalf("where/params = %q/%v, want %q with parameterized policy", where, params, wantWhere)
			}
			for _, value := range params {
				if strings.Contains(where, value) {
					t.Fatalf("policy value %q was interpolated into where clause %q", value, where)
				}
			}
		})
	}
}

func TestProxyMarksBrowserSessionResponsesPrivate(t *testing.T) {
	t.Parallel()
	testCases := []struct{ name string }{{name: "valid session policy is not shared"}}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Query().Get("params[1]") != "did:plc:bob" || r.URL.Query().Get("params[2]") != "at://did:plc:bob/dev.adenosine.issue/hidden" {
					t.Errorf("upstream moderation parameters = %v", r.URL.Query())
				}
				w.Header().Set("Cache-Control", "public, max-age=60")
				w.Header().Set("Expires", "tomorrow")
				_, _ = w.Write([]byte(`[]`))
			}))
			defer upstream.Close()
			proxy, err := build(upstream.URL, "secret", nil)
			if err != nil {
				t.Fatalf("build proxy: %v", err)
			}
			response := httptest.NewRecorder()
			err = proxy.Forward(response, httptest.NewRequest(http.MethodGet, "/?offset=-1", nil), Issues, Policy{
				BrowserSession: true, BlockedDIDs: []string{"did:plc:bob"}, HiddenRecordURIs: []string{"at://did:plc:bob/dev.adenosine.issue/hidden"},
			})
			if err != nil || response.Header().Get("Cache-Control") != "private, no-store" || response.Header().Get("Expires") != "" || response.Header().Get("Vary") != "Cookie, Authorization" {
				t.Fatalf("error/headers = %v/%v", err, response.Header())
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
			if err := proxy.Forward(httptest.NewRecorder(), request, Repositories, Policy{}); !errors.Is(err, ErrMalformed) {
				t.Fatalf("error = %v, want malformed", err)
			}
		})
	}
}

func TestProxyRejectsUnknownServerShape(t *testing.T) {
	t.Parallel()
	testCases := []struct {
		name  string
		shape Shape
	}{
		{name: "empty shape", shape: ""},
		{name: "unregistered table name", shape: "core.accounts"},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			proxy, err := build("https://electric.example", "secret", roundTripFunc(func(*http.Request) (*http.Response, error) {
				t.Fatal("unknown shape reached upstream")
				return nil, nil
			}))
			if err != nil {
				t.Fatalf("build proxy: %v", err)
			}
			err = proxy.Forward(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/?offset=-1", nil), testCase.shape, Policy{})
			if !errors.Is(err, ErrMalformed) {
				t.Fatalf("error = %v, want malformed", err)
			}
		})
	}
}

func TestDevelopmentElectricAccessMatchesRegistry(t *testing.T) {
	t.Parallel()
	testCases := []struct {
		name, prefix, suffix string
	}{
		{name: "select grant", prefix: "GRANT SELECT ON TABLE", suffix: " TO electric"},
		{name: "publication", prefix: "ALTER PUBLICATION electric_publication_default SET TABLE"},
	}
	contents, err := os.ReadFile("../../dev/entrypoint.sh")
	if err != nil {
		t.Fatalf("read development entrypoint: %v", err)
	}
	normalized := strings.Join(strings.Fields(string(contents)), " ")
	tables := "network.repositories, network.profiles, network.stars, network.issues, network.issue_comments, network.pull_requests, network.pull_request_reviews"
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			clause := testCase.prefix + " " + tables + testCase.suffix + ";"
			if strings.Count(normalized, testCase.prefix) != 1 || !strings.Contains(normalized, clause) {
				t.Fatalf("development Electric %s does not contain exactly %q", testCase.name, clause)
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
				definition := shapes[Repositories]
				if r.Method != http.MethodPost || r.URL.Query().Get("where") != definition.where || r.URL.Query().Get("table") != definition.table {
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
			if err := proxy.Forward(httptest.NewRecorder(), valid, Repositories, Policy{}); err != nil {
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
			if err := proxy.Forward(httptest.NewRecorder(), empty, Repositories, Policy{}); err != nil || upstreamBody != "" {
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
					if err := proxy.Forward(httptest.NewRecorder(), request, Repositories, Policy{}); !errors.Is(err, validationCase.want) {
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
			if err := proxy.Forward(response, httptest.NewRequest(http.MethodGet, "/?offset=-1", nil), Repositories, Policy{}); err != nil {
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
				_ = proxy.Forward(w, r, Repositories, Policy{})
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
			err := proxy.Forward(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/?offset=-1", nil), Repositories, Policy{})
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
