package release

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
)

type blobStorePair struct {
	first  BlobStore
	second BlobStore
}

func TestBlobStoreContract(t *testing.T) {
	t.Parallel()
	testCases := []struct {
		name string
		pair func(*testing.T) blobStorePair
	}{
		{name: BackendFilesystem, pair: newFilesystemPair},
		{name: BackendS3, pair: newS3Pair},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			pair := testCase.pair(t)
			body := "release bytes"
			checksum, err := pair.first.Put(context.Background(), "repo/release/asset", strings.NewReader(body), int64(len(body)))
			if err != nil {
				t.Fatalf("Put(): %v", err)
			}
			if checksum != "ff7a5e6429d2c8511521e4abf41cd54a3e525ef4a1f24f8d1c67ede9d17874dd" {
				t.Fatalf("checksum = %q", checksum)
			}

			reader, err := pair.second.Open(context.Background(), "repo/release/asset")
			if err != nil {
				t.Fatalf("Open() from second node: %v", err)
			}
			got, readErr := io.ReadAll(reader)
			closeErr := reader.Close()
			if readErr != nil || closeErr != nil || string(got) != body {
				t.Fatalf("read body = %q, read error = %v, close error = %v", got, readErr, closeErr)
			}

			if retryChecksum, err := pair.second.Put(context.Background(), "repo/release/asset", strings.NewReader(body), int64(len(body))); err != nil || retryChecksum != checksum {
				t.Fatalf("idempotent Put() = %q, %v", retryChecksum, err)
			}
			if _, err := pair.second.Put(context.Background(), "repo/release/asset", strings.NewReader("other content"), 13); !errors.Is(err, ErrConflict) {
				t.Fatalf("conflicting Put() error = %v, want %v", err, ErrConflict)
			}

			for _, mismatch := range []struct {
				key          string
				body         string
				expectedSize int64
			}{
				{key: "repo/release/short", body: "short", expectedSize: 6},
				{key: "repo/release/long", body: "too long", expectedSize: 3},
			} {
				if _, err := pair.first.Put(context.Background(), mismatch.key, strings.NewReader(mismatch.body), mismatch.expectedSize); !errors.Is(err, ErrSizeMismatch) {
					t.Fatalf("Put(%s) error = %v, want %v", mismatch.key, err, ErrSizeMismatch)
				}
				if _, err := pair.second.Open(context.Background(), mismatch.key); !errors.Is(err, ErrNotFound) {
					t.Fatalf("Open(%s) after mismatch error = %v, want %v", mismatch.key, err, ErrNotFound)
				}
			}

			cancelled, cancel := context.WithCancel(context.Background())
			cancel()
			if _, err := pair.first.Put(cancelled, "repo/release/cancelled", strings.NewReader("x"), 1); !errors.Is(err, context.Canceled) {
				t.Fatalf("cancelled Put() error = %v, want %v", err, context.Canceled)
			}

			if err := pair.second.Delete(context.Background(), "repo/release/asset"); err != nil {
				t.Fatalf("Delete(): %v", err)
			}
			if err := pair.first.Delete(context.Background(), "repo/release/asset"); err != nil {
				t.Fatalf("idempotent Delete(): %v", err)
			}
			if _, err := pair.first.Open(context.Background(), "repo/release/asset"); !errors.Is(err, ErrNotFound) {
				t.Fatalf("Open() after Delete error = %v, want %v", err, ErrNotFound)
			}
		})
	}
}

func TestBlobStoreConcurrentImmutablePut(t *testing.T) {
	t.Parallel()
	testCases := []struct {
		name string
		pair func(*testing.T) blobStorePair
	}{
		{name: BackendFilesystem, pair: newFilesystemPair},
		{name: BackendS3, pair: newS3Pair},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			pair := testCase.pair(t)
			bodies := []string{"node-a", "node-b"}
			errorsByNode := make([]error, len(bodies))
			var wait sync.WaitGroup
			wait.Add(len(bodies))
			for index := range bodies {
				go func() {
					defer wait.Done()
					store := pair.first
					if index == 1 {
						store = pair.second
					}
					_, errorsByNode[index] = store.Put(context.Background(), "repo/release/race", strings.NewReader(bodies[index]), int64(len(bodies[index])))
				}()
			}
			wait.Wait()
			successes, conflicts := 0, 0
			for _, err := range errorsByNode {
				switch {
				case err == nil:
					successes++
				case errors.Is(err, ErrConflict):
					conflicts++
				default:
					t.Fatalf("concurrent Put() error = %v", err)
				}
			}
			if successes != 1 || conflicts != 1 {
				t.Fatalf("concurrent results = %d successes/%d conflicts", successes, conflicts)
			}
			reader, err := pair.first.Open(context.Background(), "repo/release/race")
			if err != nil {
				t.Fatalf("Open(): %v", err)
			}
			body, err := io.ReadAll(reader)
			_ = reader.Close()
			if err != nil || (string(body) != bodies[0] && string(body) != bodies[1]) {
				t.Fatalf("stored body = %q, error = %v", body, err)
			}
		})
	}
}

func TestS3Configuration(t *testing.T) {
	t.Parallel()
	valid := S3Config{
		Endpoint: "https://objects.example.com", Region: "us-east-1", Bucket: "release-assets",
		AccessKeyID: "access-key", SecretAccessKey: "secret-key",
	}
	testCases := []struct {
		name   string
		mutate func(*S3Config)
	}{
		{name: "valid"},
		{name: "invalid endpoint", mutate: func(cfg *S3Config) { cfg.Endpoint = "/objects" }},
		{name: "endpoint credentials", mutate: func(cfg *S3Config) { cfg.Endpoint = "https://user@objects.example.com" }},
		{name: "empty region", mutate: func(cfg *S3Config) { cfg.Region = "" }},
		{name: "invalid bucket", mutate: func(cfg *S3Config) { cfg.Bucket = "bucket/path" }},
		{name: "empty access key", mutate: func(cfg *S3Config) { cfg.AccessKeyID = "" }},
		{name: "empty secret key", mutate: func(cfg *S3Config) { cfg.SecretAccessKey = "" }},
		{name: "session token whitespace", mutate: func(cfg *S3Config) { cfg.SessionToken = " token" }},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			cfg := valid
			if testCase.mutate != nil {
				testCase.mutate(&cfg)
			}
			err := cfg.Validate()
			if testCase.mutate == nil && err != nil {
				t.Fatalf("Validate(): %v", err)
			}
			if testCase.mutate != nil && err == nil {
				t.Fatal("Validate() accepted invalid configuration")
			}
		})
	}
}

func TestS3StreamingIntegrityAndRetry(t *testing.T) {
	t.Parallel()
	testCases := []struct {
		name       string
		failPuts   int
		corruptGet bool
		wantErr    error
		wantPuts   int
	}{
		{name: "transient put is retried", failPuts: 1, wantPuts: 2},
		{name: "corrupt read is rejected", corruptGet: true, wantErr: ErrChecksumMismatch, wantPuts: 1},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			server := newFakeS3Server(t)
			server.failPuts = testCase.failPuts
			storage, err := NewS3(context.Background(), server.config())
			if err != nil {
				t.Fatalf("NewS3(): %v", err)
			}
			if _, err := storage.Put(context.Background(), "repo/release/integrity", strings.NewReader("verified"), 8); err != nil {
				t.Fatalf("Put(): %v", err)
			}
			if server.putCount != testCase.wantPuts {
				t.Fatalf("PutObject calls = %d, want %d", server.putCount, testCase.wantPuts)
			}
			server.corruptGet = testCase.corruptGet
			reader, err := storage.Open(context.Background(), "repo/release/integrity")
			if err != nil {
				t.Fatalf("Open(): %v", err)
			}
			_, err = io.ReadAll(reader)
			_ = reader.Close()
			if !errors.Is(err, testCase.wantErr) {
				t.Fatalf("Read() error = %v, want %v", err, testCase.wantErr)
			}
		})
	}
}

func TestS3StartupBucketValidation(t *testing.T) {
	t.Parallel()
	testCases := []struct{ name string }{{name: "missing bucket fails startup"}}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			server := newFakeS3Server(t)
			cfg := server.config()
			cfg.Bucket = "missing"
			if _, err := NewS3(context.Background(), cfg); err == nil || !strings.Contains(err.Error(), "verify S3 bucket access") {
				t.Fatalf("NewS3() error = %v", err)
			}
		})
	}
}

func TestBlobStoreSelection(t *testing.T) {
	t.Parallel()
	server := newFakeS3Server(t)
	testCases := []struct {
		name    string
		config  BlobStoreConfig
		wantErr bool
	}{
		{name: BackendFilesystem, config: BlobStoreConfig{Backend: BackendFilesystem, FilesystemRoot: t.TempDir()}},
		{name: BackendS3, config: BlobStoreConfig{Backend: BackendS3, S3: server.config()}},
		{name: "unsupported", config: BlobStoreConfig{Backend: "database"}, wantErr: true},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			store, err := buildBlobStore(context.Background(), testCase.config)
			if testCase.wantErr && err == nil {
				t.Fatal("buildBlobStore() accepted an unsupported backend")
			}
			if !testCase.wantErr && (err != nil || store == nil) {
				t.Fatalf("buildBlobStore() = %#v, %v", store, err)
			}
		})
	}
}

func newFilesystemPair(t *testing.T) blobStorePair {
	t.Helper()
	root := t.TempDir()
	first, err := NewFilesystem(root)
	if err != nil {
		t.Fatalf("NewFilesystem(first): %v", err)
	}
	second, err := NewFilesystem(root)
	if err != nil {
		t.Fatalf("NewFilesystem(second): %v", err)
	}
	return blobStorePair{first: first, second: second}
}

func newS3Pair(t *testing.T) blobStorePair {
	t.Helper()
	server := newFakeS3Server(t)
	first, err := NewS3(context.Background(), server.config())
	if err != nil {
		t.Fatalf("NewS3(first): %v", err)
	}
	second, err := NewS3(context.Background(), server.config())
	if err != nil {
		t.Fatalf("NewS3(second): %v", err)
	}
	return blobStorePair{first: first, second: second}
}

type fakeS3Object struct {
	body     []byte
	checksum string
}

type fakeS3 struct {
	t          *testing.T
	server     *httptest.Server
	mu         sync.Mutex
	objects    map[string]fakeS3Object
	failPuts   int
	putCount   int
	corruptGet bool
}

func newFakeS3Server(t *testing.T) *fakeS3 {
	t.Helper()
	backend := &fakeS3{t: t, objects: map[string]fakeS3Object{}}
	backend.server = httptest.NewServer(http.HandlerFunc(backend.serveHTTP))
	t.Cleanup(backend.server.Close)
	return backend
}

func (server *fakeS3) config() S3Config {
	return S3Config{
		Endpoint: server.server.URL, Region: "us-east-1", Bucket: "release-assets",
		AccessKeyID: "test-access-key", SecretAccessKey: "test-secret-key", PathStyle: true,
	}
}

func (server *fakeS3) serveHTTP(response http.ResponseWriter, request *http.Request) {
	bucket, key, err := fakeS3Path(request.URL)
	if err != nil || bucket != "release-assets" {
		writeS3Error(response, http.StatusNotFound, "NoSuchBucket")
		return
	}
	if key == "" {
		if request.Method == http.MethodHead {
			response.WriteHeader(http.StatusOK)
			return
		}
		writeS3Error(response, http.StatusMethodNotAllowed, "MethodNotAllowed")
		return
	}

	switch request.Method {
	case http.MethodPut:
		server.put(response, request, key)
	case http.MethodHead:
		server.writeObject(response, key, true)
	case http.MethodGet:
		server.writeObject(response, key, false)
	case http.MethodDelete:
		server.mu.Lock()
		delete(server.objects, key)
		server.mu.Unlock()
		response.WriteHeader(http.StatusNoContent)
	default:
		writeS3Error(response, http.StatusMethodNotAllowed, "MethodNotAllowed")
	}
}

func (server *fakeS3) put(response http.ResponseWriter, request *http.Request, key string) {
	server.mu.Lock()
	server.putCount++
	if server.failPuts > 0 {
		server.failPuts--
		server.mu.Unlock()
		writeS3Error(response, http.StatusInternalServerError, "InternalError")
		return
	}
	server.mu.Unlock()
	body, err := io.ReadAll(request.Body)
	if err != nil {
		server.t.Errorf("read fake S3 request: %v", err)
		writeS3Error(response, http.StatusInternalServerError, "InternalError")
		return
	}
	checksum := request.Header.Get("X-Amz-Meta-" + checksumMetadataKey)
	digest := sha256.Sum256(body)
	wantEncoded := base64.StdEncoding.EncodeToString(digest[:])
	if checksum == "" || request.Header.Get("X-Amz-Checksum-Sha256") != wantEncoded {
		writeS3Error(response, http.StatusBadRequest, "BadDigest")
		return
	}
	server.mu.Lock()
	defer server.mu.Unlock()
	if _, exists := server.objects[key]; exists && request.Header.Get("If-None-Match") == "*" {
		writeS3Error(response, http.StatusPreconditionFailed, "PreconditionFailed")
		return
	}
	server.objects[key] = fakeS3Object{body: append([]byte(nil), body...), checksum: checksum}
	response.Header().Set("X-Amz-Checksum-Sha256", wantEncoded)
	response.WriteHeader(http.StatusOK)
}

func (server *fakeS3) writeObject(response http.ResponseWriter, key string, head bool) {
	server.mu.Lock()
	object, exists := server.objects[key]
	corrupt := server.corruptGet
	server.mu.Unlock()
	if !exists {
		writeS3Error(response, http.StatusNotFound, "NoSuchKey")
		return
	}
	response.Header().Set("Content-Length", fmt.Sprint(len(object.body)))
	response.Header().Set("X-Amz-Meta-"+checksumMetadataKey, object.checksum)
	body := object.body
	if corrupt && !head && len(body) > 0 {
		body = append([]byte(nil), body...)
		body[0] ^= 0xff
	}
	response.WriteHeader(http.StatusOK)
	if !head {
		_, _ = response.Write(body)
	}
}

func fakeS3Path(value *url.URL) (string, string, error) {
	decoded, err := url.PathUnescape(value.EscapedPath())
	if err != nil {
		return "", "", err
	}
	parts := strings.SplitN(strings.TrimPrefix(decoded, "/"), "/", 2)
	if len(parts) == 0 || parts[0] == "" {
		return "", "", fmt.Errorf("bucket is missing")
	}
	if len(parts) == 1 {
		return parts[0], "", nil
	}
	return parts[0], parts[1], nil
}

func writeS3Error(response http.ResponseWriter, status int, code string) {
	response.Header().Set("Content-Type", "application/xml")
	response.WriteHeader(status)
	_, _ = fmt.Fprintf(response, "<Error><Code>%s</Code><Message>%s</Message><RequestId>test-request</RequestId></Error>", code, code)
}
