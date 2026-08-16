package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

type memoryCredentialStore struct{ config credentialFile }

func (store *memoryCredentialStore) Load() (credentialFile, error) { return store.config, nil }
func (store *memoryCredentialStore) Save(config credentialFile) error {
	store.config = config
	return nil
}

type recordingGit struct{ calls [][]string }

func (git *recordingGit) Run(_ context.Context, args ...string) error {
	git.calls = append(git.calls, append([]string(nil), args...))
	return nil
}

func TestCLIWorkflowsUseGeneratedPublicClient(t *testing.T) {
	head := strings.Repeat("a", 40)
	repositoryURI := "at://did:plc:alice/dev.adenosine.repo/project"
	pullRequestURI := "at://did:plc:alice/dev.adenosine.pullRequest/pr"
	testCases := []struct {
		name         string
		args         []string
		wantRequests []string
		wantGit      [][]string
		wantOutput   string
		wantCursor   string
	}{
		{name: "repository view", args: []string{"repo", "view", "--json", "alice/project"}, wantRequests: []string{"GET /api/v1/repositories/alice/project"}, wantOutput: `"slug": "project"`},
		{name: "issue create resolves repository", args: []string{"issue", "create", "--repo", "alice/project", "--title", "Bug", "--body", "Details"}, wantRequests: []string{"GET /api/v1/repositories/alice/project", "POST /api/v1/issues"}, wantOutput: "dev.adenosine.issue/issue"},
		{name: "issue list forwards opaque cursor", args: []string{"issue", "list", "--json", "--repo", "alice/project", "--cursor", "opaque.cursor:value"}, wantRequests: []string{"GET /api/v1/repositories/alice/project", "GET /api/v1/issues"}, wantOutput: `"next_cursor": "next.opaque:value"`, wantCursor: "opaque.cursor:value"},
		{name: "pull request create resolves both repositories", args: []string{"pr", "create", "--source-repo", "alice/source", "--target-repo", "alice/project", "--source-branch", "feature", "--head", head, "--title", "Change"}, wantRequests: []string{"GET /api/v1/repositories/alice/source", "GET /api/v1/repositories/alice/project", "POST /api/v1/pull-requests"}, wantOutput: pullRequestURI},
		{name: "pull request view JSON", args: []string{"pr", "view", "--json", pullRequestURI}, wantRequests: []string{"GET /api/v1/pull-requests/detail"}, wantOutput: `"title": "Change"`},
		{name: "pull request merge", args: []string{"pr", "merge", "--strategy", "squash", pullRequestURI}, wantRequests: []string{"POST /api/v1/pull-requests/merge"}, wantOutput: strings.Repeat("c", 40)},
		{name: "pull request checkout uses Git without credentials", args: []string{"pr", "checkout", "--branch", "review-pr", pullRequestURI}, wantRequests: []string{"GET /api/v1/pull-requests/checkout"}, wantGit: [][]string{{"fetch", "--no-tags", "https://git.example/source.git", head}, {"checkout", "-B", "review-pr", "FETCH_HEAD"}}},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			var requests []string
			var receivedCursor string
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
				if request.Header.Get("Authorization") != "Bearer secret-token" {
					t.Errorf("authorization header = %q", request.Header.Get("Authorization"))
				}
				requests = append(requests, request.Method+" "+request.URL.Path)
				if request.Method == http.MethodGet && request.URL.Path == "/api/v1/issues" {
					receivedCursor = request.URL.Query().Get("cursor")
				}
				serveCLIResponse(t, w, request, repositoryURI, pullRequestURI, head)
			}))
			t.Cleanup(server.Close)
			credentials := &memoryCredentialStore{config: credentialFile{DefaultHost: server.URL, Hosts: map[string]hostCredential{server.URL: {Token: "secret-token"}}}}
			git := &recordingGit{}
			var output bytes.Buffer
			runner := &runner{stdin: strings.NewReader(""), stdout: &output, stderr: io.Discard, credentials: credentials, git: git, newClient: newAPIClient}
			if err := runner.run(context.Background(), testCase.args); err != nil {
				t.Fatalf("run() error = %v", err)
			}
			if !reflect.DeepEqual(requests, testCase.wantRequests) {
				t.Errorf("requests = %#v, want %#v", requests, testCase.wantRequests)
			}
			if receivedCursor != testCase.wantCursor {
				t.Errorf("cursor = %q, want %q", receivedCursor, testCase.wantCursor)
			}
			if !reflect.DeepEqual(git.calls, testCase.wantGit) {
				t.Errorf("Git calls = %#v, want %#v", git.calls, testCase.wantGit)
			}
			if testCase.wantOutput != "" && !strings.Contains(output.String(), testCase.wantOutput) {
				t.Errorf("output = %q, want fragment %q", output.String(), testCase.wantOutput)
			}
		})
	}
}

func TestLoginReadsTokenFromStdinAndSecuresCredentialFile(t *testing.T) {
	testCases := []struct{ name string }{{name: "valid token"}}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
				if request.Header.Get("Authorization") != "Bearer stdin-token" {
					t.Fatalf("authorization header = %q", request.Header.Get("Authorization"))
				}
				writeTestJSON(w, http.StatusOK, map[string]any{"did": "did:plc:alice"})
			}))
			t.Cleanup(server.Close)
			directory := t.TempDir()
			t.Setenv("ADENOSINE_CONFIG_DIR", directory)
			var output bytes.Buffer
			if err := Run(context.Background(), []string{"login", "--host", server.URL, "--token-stdin"}, strings.NewReader("stdin-token\n"), &output, io.Discard); err != nil {
				t.Fatalf("Run() error = %v", err)
			}
			info, err := os.Stat(filepath.Join(directory, "hosts.json"))
			if err != nil {
				t.Fatalf("stat credentials: %v", err)
			}
			if info.Mode().Perm() != 0o600 {
				t.Errorf("credential permissions = %o, want 600", info.Mode().Perm())
			}
			data, err := os.ReadFile(filepath.Join(directory, "hosts.json"))
			if err != nil || !bytes.Contains(data, []byte("stdin-token")) || strings.Contains(output.String(), "stdin-token") {
				t.Fatalf("credential file/output = %q/%q, %v", data, output.String(), err)
			}
		})
	}
}

func serveCLIResponse(t *testing.T, w http.ResponseWriter, request *http.Request, repositoryURI, pullRequestURI, head string) {
	t.Helper()
	switch request.Method + " " + request.URL.Path {
	case "GET /api/v1/repositories/alice/project", "GET /api/v1/repositories/alice/source":
		slug := strings.TrimPrefix(request.URL.Path, "/api/v1/repositories/alice/")
		uri := strings.Replace(repositoryURI, "project", slug, 1)
		writeTestJSON(w, http.StatusOK, testRepository(uri, slug))
	case "POST /api/v1/issues":
		writeTestJSON(w, http.StatusAccepted, map[string]any{"projected": false, "issue": testIssueEnvelope(repositoryURI)})
	case "GET /api/v1/issues":
		writeTestJSON(w, http.StatusOK, map[string]any{"issue_count": 1, "open_issue_count": 1, "items": []any{testIssue(repositoryURI)}, "page": map[string]any{"next_cursor": "next.opaque:value"}})
	case "POST /api/v1/pull-requests":
		writeTestJSON(w, http.StatusAccepted, map[string]any{"projected": false, "pull_request": testPullRequestEnvelope(repositoryURI, pullRequestURI, head)})
	case "GET /api/v1/pull-requests/detail":
		writeTestJSON(w, http.StatusOK, testPullRequest(repositoryURI, pullRequestURI, head))
	case "POST /api/v1/pull-requests/merge":
		writeTestJSON(w, http.StatusOK, testPullRequestMerge(pullRequestURI, head))
	case "GET /api/v1/pull-requests/checkout":
		writeTestJSON(w, http.StatusOK, map[string]any{"git_https_url": "https://git.example/source.git", "source_branch": "feature", "head_sha": head})
	default:
		http.NotFound(w, request)
	}
}

func writeTestJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func testRepository(uri, slug string) map[string]any {
	now := time.Date(2026, 8, 14, 0, 0, 0, 0, time.UTC)
	return map[string]any{"uri": uri, "slug": slug, "visibility": "public", "state": "active", "default_branch": "main", "archived": false, "owner": map[string]any{"did": "did:plc:alice", "handle": "alice"}, "hosting": map[string]any{"local": true, "web_url": "https://forge.example/alice/" + slug, "git_https_url": "https://forge.example/alice/" + slug + ".git", "source_browsing": "local"}, "star_count": 0, "issue_count": 0, "open_issue_count": 0, "comment_count": 0, "pull_request_count": 0, "open_pull_request_count": 0, "fork_count": 0, "created_at": now, "updated_at": now}
}

func testIssueEnvelope(repositoryURI string) map[string]any {
	now := time.Date(2026, 8, 14, 0, 0, 0, 0, time.UTC)
	return map[string]any{"uri": "at://did:plc:alice/dev.adenosine.issue/issue", "cid": "bafyissue", "author_did": "did:plc:alice", "repository_uri": repositoryURI, "repository_cid": "bafyrepo", "title": "Bug", "body": "Details", "created_at": now, "updated_at": now}
}

func testIssue(repositoryURI string) map[string]any {
	value := testIssueEnvelope(repositoryURI)
	value["state"], value["status_uri"], value["status_cid"] = "open", nil, nil
	value["comment_count"], value["indexed_at"] = 0, time.Date(2026, 8, 14, 0, 0, 0, 0, time.UTC)
	return value
}

func testPullRequestEnvelope(repositoryURI, pullRequestURI, head string) map[string]any {
	now := time.Date(2026, 8, 14, 0, 0, 0, 0, time.UTC)
	return map[string]any{"uri": pullRequestURI, "cid": "bafypr", "author_did": "did:plc:alice", "source_repository_uri": strings.Replace(repositoryURI, "project", "source", 1), "source_repository_cid": "bafysource", "source_branch": "feature", "target_repository_uri": repositoryURI, "target_repository_cid": "bafytarget", "target_branch": "main", "head_sha": head, "title": "Change", "body": "", "created_at": now, "updated_at": now}
}

func testPullRequest(repositoryURI, pullRequestURI, head string) map[string]any {
	value := testPullRequestEnvelope(repositoryURI, pullRequestURI, head)
	value["state"], value["status_uri"], value["status_cid"], value["merged_commit_sha"] = "open", nil, nil, nil
	value["review_count"], value["indexed_at"] = 0, time.Date(2026, 8, 14, 0, 0, 0, 0, time.UTC)
	return value
}

func testPullRequestMerge(pullRequestURI, head string) map[string]any {
	now := time.Date(2026, 8, 14, 0, 0, 0, 0, time.UTC)
	merge := strings.Repeat("c", 40)
	return map[string]any{"merge_commit_sha": merge, "old_sha": strings.Repeat("b", 40), "head_sha": head, "target_ref": "refs/heads/main", "strategy": "squash", "status": map[string]any{"uri": pullRequestURI + "/status", "cid": "bafystatus", "author_did": "did:plc:alice", "pull_request_uri": pullRequestURI, "pull_request_cid": "bafypr", "target_repository_uri": "at://did:plc:alice/dev.adenosine.repo/project", "target_repository_cid": "bafyrepo", "state": "merged", "merge_commit_sha": merge, "created_at": now, "updated_at": now}}
}
