// Package syncproxy exposes server-owned Electric shapes through the public API.
package syncproxy

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const (
	maxSubsetBody = 16 * 1024
	table         = "network.repositories"
	columns       = "uri,cid,owner_did,slug,name,description,default_branch,git_https,git_ssh,web,record_created_at,record_updated_at,indexed_at,star_count,issue_count,open_issue_count,pull_request_count,open_pull_request_count"
	mainWhere     = "deleted_at IS NULL AND cid IS NOT NULL"
)

var (
	ErrDisabled     = errors.New("Electric sync is disabled")
	ErrMalformed    = errors.New("malformed Electric sync request")
	ErrBodyTooLarge = errors.New("Electric sync request body is too large")
	ErrUnavailable  = errors.New("Electric sync is unavailable")
)

var getContinuationParams = map[string]struct{}{
	"cache-buster": {}, "cursor": {}, "expired_handle": {}, "experimental_live_sse": {}, "handle": {},
	"live": {}, "live_sse": {}, "log": {}, "offset": {}, "replica": {},
}

var postContinuationParams = map[string]struct{}{
	"cache-buster": {}, "expired_handle": {}, "handle": {}, "log": {}, "offset": {}, "replica": {},
}

type Proxy struct {
	shapeURL *url.URL
	secret   string
	client   *http.Client
}

// Must constructs the optional Electric proxy or panics during startup.
func Must(baseURL, secret string) *Proxy {
	proxy, err := build(baseURL, secret, nil)
	if err != nil {
		panic(err)
	}
	return proxy
}

func build(baseURL, secret string, transport http.RoundTripper) (*Proxy, error) {
	if baseURL == "" && secret == "" {
		return &Proxy{}, nil
	}
	if baseURL == "" || secret == "" {
		return nil, fmt.Errorf("construct Electric sync proxy: URL and secret must be configured together")
	}
	parsed, err := url.Parse(baseURL)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return nil, fmt.Errorf("construct Electric sync proxy: invalid base URL")
	}
	parsed.Path = strings.TrimSuffix(parsed.Path, "/") + "/v1/shape"
	if transport == nil {
		transport = &http.Transport{
			Proxy:             http.ProxyFromEnvironment,
			DialContext:       (&net.Dialer{Timeout: 5 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
			ForceAttemptHTTP2: true, MaxIdleConns: 100, IdleConnTimeout: 90 * time.Second,
			TLSHandshakeTimeout: 5 * time.Second, ExpectContinueTimeout: time.Second,
		}
	}
	return &Proxy{shapeURL: parsed, secret: secret, client: &http.Client{Transport: transport}}, nil
}

// Forward validates a public shape request and streams Electric's response.
func (proxy *Proxy) Forward(w http.ResponseWriter, r *http.Request) error {
	if proxy.shapeURL == nil {
		return ErrDisabled
	}
	query, err := proxy.upstreamQuery(r.Method, r.URL.Query())
	if err != nil {
		return err
	}
	body, err := subsetBody(r)
	if err != nil {
		return err
	}
	upstreamURL := *proxy.shapeURL
	upstreamURL.RawQuery = query.Encode()
	request, err := http.NewRequestWithContext(r.Context(), r.Method, upstreamURL.String(), body)
	if err != nil {
		return fmt.Errorf("%w: construct request", ErrUnavailable)
	}
	if body.Len() > 0 {
		request.Header.Set("Content-Type", "application/json")
	}
	if etag := r.Header.Get("If-None-Match"); etag != "" {
		request.Header.Set("If-None-Match", etag)
	}
	response, err := proxy.client.Do(request)
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return fmt.Errorf("%w: request canceled", ErrUnavailable)
		}
		return fmt.Errorf("%w: request failed", ErrUnavailable)
	}
	defer response.Body.Close()
	switch {
	case response.StatusCode == http.StatusBadRequest:
		return fmt.Errorf("%w: Electric rejected the request", ErrMalformed)
	case response.StatusCode == http.StatusRequestEntityTooLarge:
		return fmt.Errorf("%w: Electric rejected the body", ErrBodyTooLarge)
	case response.StatusCode == http.StatusUnauthorized, response.StatusCode == http.StatusForbidden,
		response.StatusCode == http.StatusNotFound, response.StatusCode >= http.StatusInternalServerError:
		return fmt.Errorf("%w: Electric returned status %d", ErrUnavailable, response.StatusCode)
	}
	copyResponseHeaders(w.Header(), response)
	w.Header().Set("Vary", mergeVary(w.Header().Get("Vary"), "Cookie", "Authorization"))
	w.WriteHeader(response.StatusCode)
	_, _ = io.Copy(flushingWriter{ResponseWriter: w}, response.Body)
	return nil
}

func (proxy *Proxy) upstreamQuery(method string, client url.Values) (url.Values, error) {
	allowed := getContinuationParams
	if method == http.MethodPost {
		allowed = postContinuationParams
	}
	query := make(url.Values)
	for key, values := range client {
		if _, ok := allowed[key]; !ok || len(values) != 1 {
			return nil, fmt.Errorf("%w: unsupported or repeated query parameter", ErrMalformed)
		}
		if len(values[0]) > 255 || strings.ContainsRune(values[0], 0) {
			return nil, fmt.Errorf("%w: invalid query parameter", ErrMalformed)
		}
		query.Set(key, values[0])
	}
	if query.Get("offset") == "" {
		return nil, fmt.Errorf("%w: offset is required", ErrMalformed)
	}
	if value := query.Get("live"); value != "" && value != "true" && value != "false" {
		return nil, fmt.Errorf("%w: invalid live value", ErrMalformed)
	}
	if value := query.Get("live_sse"); value != "" && value != "true" && value != "false" {
		return nil, fmt.Errorf("%w: invalid live_sse value", ErrMalformed)
	}
	if value := query.Get("experimental_live_sse"); value != "" && value != "true" && value != "false" {
		return nil, fmt.Errorf("%w: invalid experimental_live_sse value", ErrMalformed)
	}
	if value := query.Get("replica"); value != "" && value != "default" && value != "full" {
		return nil, fmt.Errorf("%w: invalid replica value", ErrMalformed)
	}
	if value := query.Get("log"); value != "" && value != "full" && value != "changes_only" {
		return nil, fmt.Errorf("%w: invalid log value", ErrMalformed)
	}
	query.Set("table", table)
	query.Set("columns", columns)
	query.Set("queryable_columns", columns)
	query.Set("where", mainWhere)
	query.Set("secret", proxy.secret)
	return query, nil
}

type subsetRequest struct {
	Where   *string           `json:"where,omitempty"`
	Params  map[string]string `json:"params,omitempty"`
	Limit   *int              `json:"limit,omitempty"`
	Offset  *int              `json:"offset,omitempty"`
	OrderBy *string           `json:"order_by,omitempty"`
}

func subsetBody(r *http.Request) (*bytes.Reader, error) {
	if r.Method != http.MethodPost {
		return bytes.NewReader(nil), nil
	}
	raw, err := io.ReadAll(io.LimitReader(r.Body, maxSubsetBody+1))
	if err != nil {
		return nil, fmt.Errorf("%w: read body", ErrMalformed)
	}
	if len(raw) > maxSubsetBody {
		return nil, fmt.Errorf("%w: limit is %d bytes", ErrBodyTooLarge, maxSubsetBody)
	}
	if len(bytes.TrimSpace(raw)) == 0 {
		return bytes.NewReader(nil), nil
	}
	if mediaType := strings.ToLower(strings.TrimSpace(strings.Split(r.Header.Get("Content-Type"), ";")[0])); mediaType != "application/json" {
		return nil, fmt.Errorf("%w: content type must be application/json", ErrMalformed)
	}
	var subset subsetRequest
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&subset); err != nil {
		return nil, fmt.Errorf("%w: invalid JSON", ErrMalformed)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("%w: body must contain one JSON value", ErrMalformed)
	}
	if err := validateSubset(subset); err != nil {
		return nil, err
	}
	// Electric applies POST subsets to the server-owned base shape, so this
	// predicate can only narrow the base where clause set in upstreamQuery.
	return bytes.NewReader(raw), nil
}

func validateSubset(subset subsetRequest) error {
	if subset.Where != nil && (len(*subset.Where) == 0 || len(*subset.Where) > 4096 || strings.ContainsRune(*subset.Where, 0)) {
		return fmt.Errorf("%w: invalid subset where", ErrMalformed)
	}
	if subset.OrderBy != nil && (len(*subset.OrderBy) == 0 || len(*subset.OrderBy) > 1024 || strings.ContainsRune(*subset.OrderBy, 0)) {
		return fmt.Errorf("%w: invalid subset order_by", ErrMalformed)
	}
	if subset.Limit != nil && (*subset.Limit < 1 || *subset.Limit > 1000) {
		return fmt.Errorf("%w: subset limit must be between 1 and 1000", ErrMalformed)
	}
	if subset.Offset != nil && (*subset.Offset < 0 || *subset.Offset > 1_000_000) {
		return fmt.Errorf("%w: invalid subset offset", ErrMalformed)
	}
	if (subset.Limit != nil || subset.Offset != nil) && subset.OrderBy == nil {
		return fmt.Errorf("%w: subset order_by is required for pagination", ErrMalformed)
	}
	if len(subset.Params) > 100 {
		return fmt.Errorf("%w: too many subset parameters", ErrMalformed)
	}
	for key, value := range subset.Params {
		index, err := strconv.Atoi(key)
		if err != nil || index < 1 || len(value) > 4096 || strings.ContainsRune(value, 0) {
			return fmt.Errorf("%w: invalid subset parameter", ErrMalformed)
		}
	}
	return nil
}

func copyResponseHeaders(destination http.Header, response *http.Response) {
	for _, name := range []string{
		"Age", "Cache-Control", "Content-Encoding", "Content-Length", "Content-Type", "Date", "ETag", "Expires",
		"Electric-Cursor", "Electric-Handle", "Electric-Offset", "Electric-Schema", "Electric-Up-To-Date", "Last-Modified", "Retry-After", "Vary",
	} {
		for _, value := range response.Header.Values(name) {
			destination.Add(name, value)
		}
	}
	if response.Uncompressed {
		destination.Del("Content-Encoding")
		destination.Del("Content-Length")
	}
}

func mergeVary(existing string, values ...string) string {
	seen := make(map[string]bool)
	merged := make([]string, 0, len(values)+2)
	for _, value := range append(strings.Split(existing, ","), values...) {
		value = strings.TrimSpace(value)
		if value != "" && !seen[strings.ToLower(value)] {
			seen[strings.ToLower(value)] = true
			merged = append(merged, value)
		}
	}
	return strings.Join(merged, ", ")
}

type flushingWriter struct{ http.ResponseWriter }

func (writer flushingWriter) Write(body []byte) (int, error) {
	written, err := writer.ResponseWriter.Write(body)
	if err == nil {
		_ = http.NewResponseController(writer.ResponseWriter).Flush()
	}
	return written, err
}
