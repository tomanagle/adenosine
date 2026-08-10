package api

import (
	"encoding/json"
	"reflect"
	"sort"
	"testing"
)

func TestSyncOpenAPIContract(t *testing.T) {
	var document struct {
		Paths      map[string]map[string]json.RawMessage `json:"paths"`
		Components struct {
			Schemas map[string]struct {
				Properties map[string]json.RawMessage `json:"properties"`
			} `json:"schemas"`
		} `json:"components"`
	}
	if err := json.Unmarshal(OpenAPI, &document); err != nil {
		t.Fatalf("decode OpenAPI: %v", err)
	}
	testCases := []struct {
		name, path, schema string
		wantColumns        []string
	}{
		{name: "repositories", path: "/api/v1/sync/repositories", schema: "SyncRepository", wantColumns: []string{"uri", "cid", "owner_did", "slug", "name", "description", "default_branch", "git_https", "git_ssh", "web", "record_created_at", "record_updated_at", "indexed_at", "star_count", "issue_count", "open_issue_count", "comment_count", "pull_request_count", "open_pull_request_count"}},
		{name: "profiles", path: "/api/v1/sync/profiles", schema: "SyncProfile", wantColumns: []string{"did", "profile_uri", "profile_cid", "handle", "display_name", "bio", "avatar_ref", "website", "location", "repository_count", "contribution_count", "record_created_at", "indexed_at"}},
		{name: "stars", path: "/api/v1/sync/stars", schema: "SyncStar", wantColumns: []string{"uri", "cid", "author_did", "repository_uri", "repository_cid", "record_created_at", "indexed_at"}},
		{name: "issues", path: "/api/v1/sync/issues", schema: "SyncIssue", wantColumns: []string{"uri", "cid", "author_did", "repository_uri", "repository_cid", "title", "body", "state", "status_uri", "status_cid", "status_updated_at", "comment_count", "record_created_at", "record_updated_at", "indexed_at"}},
		{name: "issue comments", path: "/api/v1/sync/issue-comments", schema: "SyncIssueComment", wantColumns: []string{"uri", "cid", "author_did", "issue_uri", "issue_cid", "parent_uri", "parent_cid", "body", "record_created_at", "record_updated_at", "indexed_at"}},
		{name: "pull requests", path: "/api/v1/sync/pull-requests", schema: "SyncPullRequest", wantColumns: []string{"uri", "cid", "author_did", "source_repository_uri", "source_repository_cid", "source_branch", "target_repository_uri", "target_repository_cid", "target_branch", "head_sha", "title", "body", "state", "status_uri", "status_cid", "status_updated_at", "merged_commit_sha", "review_count", "record_created_at", "record_updated_at", "indexed_at"}},
		{name: "pull request reviews", path: "/api/v1/sync/pull-request-reviews", schema: "SyncPullRequestReview", wantColumns: []string{"uri", "cid", "author_did", "pull_request_uri", "pull_request_cid", "verdict", "body", "record_created_at", "record_updated_at", "indexed_at"}},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			path := document.Paths[testCase.path]
			if path["get"] == nil || path["post"] == nil {
				t.Fatalf("path %q does not document GET and POST", testCase.path)
			}
			for _, method := range []string{"get", "post"} {
				var operation struct {
					Responses map[string]json.RawMessage `json:"responses"`
				}
				if err := json.Unmarshal(path[method], &operation); err != nil || operation.Responses["401"] == nil {
					t.Fatalf("%s %s does not document invalid-session rejection: %v", method, testCase.path, err)
				}
			}
			properties := document.Components.Schemas[testCase.schema].Properties
			gotColumns := make([]string, 0, len(properties))
			for column := range properties {
				gotColumns = append(gotColumns, column)
			}
			sort.Strings(gotColumns)
			sort.Strings(testCase.wantColumns)
			if !reflect.DeepEqual(gotColumns, testCase.wantColumns) {
				t.Fatalf("%s columns = %v, want %v", testCase.schema, gotColumns, testCase.wantColumns)
			}
		})
	}
}
