package api

import (
	"encoding/json"
	"reflect"
	"sort"
	"strings"
	"testing"
)

const requestIDHeaderReference = "#/components/headers/RequestID"

type openAPIResponse struct {
	Reference string `json:"$ref"`
	Headers   map[string]struct {
		Reference string `json:"$ref"`
	} `json:"headers"`
}

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
		{name: "repositories", path: "/api/v1/sync/repositories", schema: "SyncRepository", wantColumns: []string{"uri", "cid", "owner_did", "slug", "name", "description", "default_branch", "git_https", "git_ssh", "web", "forked_from_uri", "forked_from_cid", "record_created_at", "record_updated_at", "indexed_at", "fork_count", "star_count", "issue_count", "open_issue_count", "comment_count", "pull_request_count", "open_pull_request_count"}},
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

func TestOpenAPIResponsesIncludeRequestID(t *testing.T) {
	var document struct {
		Paths map[string]map[string]struct {
			Responses map[string]openAPIResponse `json:"responses"`
		} `json:"paths"`
		Components struct {
			Responses map[string]openAPIResponse `json:"responses"`
		} `json:"components"`
	}
	if err := json.Unmarshal(OpenAPI, &document); err != nil {
		t.Fatalf("decode OpenAPI: %v", err)
	}

	testCases := []struct {
		name         string
		pathPrefixes []string
	}{
		{name: "public health and API operations", pathPrefixes: []string{"/health/", "/api/v1/"}},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			for path, pathItem := range document.Paths {
				if !hasAnyPrefix(path, testCase.pathPrefixes) {
					continue
				}
				for method, operation := range pathItem {
					for status, response := range operation.Responses {
						if response.Reference != "" {
							const responseReferencePrefix = "#/components/responses/"
							name, found := strings.CutPrefix(response.Reference, responseReferencePrefix)
							if !found {
								t.Errorf("%s %s response %s has unsupported reference %q", strings.ToUpper(method), path, status, response.Reference)
								continue
							}
							var exists bool
							response, exists = document.Components.Responses[name]
							if !exists {
								t.Errorf("%s %s response %s references missing response %q", strings.ToUpper(method), path, status, name)
								continue
							}
						}
						if got := response.Headers["X-Request-ID"].Reference; got != requestIDHeaderReference {
							t.Errorf("%s %s response %s X-Request-ID reference = %q, want %q", strings.ToUpper(method), path, status, got, requestIDHeaderReference)
						}
					}
				}
			}
		})
	}
}

func TestViewerPersonalizedReadsDeclareOptionalSessionSecurity(t *testing.T) {
	var document struct {
		Paths map[string]map[string]struct {
			Security []map[string][]string `json:"security"`
		} `json:"paths"`
	}
	if err := json.Unmarshal(OpenAPI, &document); err != nil {
		t.Fatalf("decode OpenAPI: %v", err)
	}
	testCases := []struct {
		name string
		path string
	}{
		{name: "issue comments", path: "/api/v1/issues/comments"},
		{name: "pull request diff", path: "/api/v1/pull-requests/diff"},
		{name: "repository", path: "/api/v1/repositories/{owner}/{repo}"},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			security := document.Paths[testCase.path]["get"].Security
			var anonymous, session bool
			for _, requirement := range security {
				if len(requirement) == 0 {
					anonymous = true
				}
				if _, found := requirement["sessionCookie"]; found {
					session = true
				}
			}
			if !anonymous || !session {
				t.Fatalf("GET %s security = %#v, want anonymous and session alternatives", testCase.path, security)
			}
		})
	}
}

func TestCollectionAPIContract(t *testing.T) {
	var document struct {
		Paths map[string]map[string]struct {
			Parameters []struct {
				Reference string `json:"$ref"`
			} `json:"parameters"`
		} `json:"paths"`
		Components struct {
			Schemas map[string]struct {
				Type       string                     `json:"type"`
				Properties map[string]json.RawMessage `json:"properties"`
			} `json:"schemas"`
		} `json:"components"`
	}
	if err := json.Unmarshal(OpenAPI, &document); err != nil {
		t.Fatalf("decode OpenAPI: %v", err)
	}

	testCases := []struct {
		name      string
		path      string
		limitRef  string
		cursorRef string
	}{
		{name: "passkeys", path: "/api/v1/passkeys"},
		{name: "organizations", path: "/api/v1/organizations"},
		{name: "organization invitations", path: "/api/v1/organization-invitations"},
		{name: "owner organization invitations", path: "/api/v1/organizations/{organization}/invitations"},
		{name: "organization members", path: "/api/v1/organizations/{organization}/members"},
		{name: "organization teams", path: "/api/v1/organizations/{organization}/teams"},
		{name: "organization team members", path: "/api/v1/organizations/{organization}/teams/{team}/members"},
		{name: "organization repositories", path: "/api/v1/organizations/{organization}/repositories"},
		{name: "organization team repositories", path: "/api/v1/organizations/{organization}/teams/{team}/repositories"},
		{name: "stars", path: "/api/v1/stars"},
		{name: "issues", path: "/api/v1/issues"},
		{name: "comments", path: "/api/v1/issues/comments"},
		{name: "pull requests", path: "/api/v1/pull-requests"},
		{name: "pull request reviews", path: "/api/v1/pull-requests/reviews"},
		{name: "network repositories", path: "/api/v1/network/repositories"},
		{name: "repository search", path: "/api/v1/search/repositories", limitRef: "#/components/parameters/SearchLimit", cursorRef: "#/components/parameters/SearchCursor"},
		{name: "profile search", path: "/api/v1/search/profiles", limitRef: "#/components/parameters/SearchLimit", cursorRef: "#/components/parameters/SearchCursor"},
		{name: "branches", path: "/api/v1/repositories/{owner}/{repo}/branches"},
		{name: "tags", path: "/api/v1/repositories/{owner}/{repo}/tags"},
		{name: "commits", path: "/api/v1/repositories/{owner}/{repo}/commits"},
		{name: "access tokens", path: "/api/v1/tokens"},
		{name: "SSH keys", path: "/api/v1/ssh-keys"},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			parameters := document.Paths[testCase.path]["get"].Parameters
			found := map[string]bool{}
			for _, parameter := range parameters {
				found[parameter.Reference] = true
			}
			limitRef := testCase.limitRef
			if limitRef == "" {
				limitRef = "#/components/parameters/Limit"
			}
			cursorRef := testCase.cursorRef
			if cursorRef == "" {
				cursorRef = "#/components/parameters/Cursor"
			}
			for _, reference := range []string{limitRef, cursorRef} {
				if !found[reference] {
					t.Errorf("GET %s is missing %s", testCase.path, reference)
				}
			}
		})
	}

	for name, schema := range document.Components.Schemas {
		if schema.Type == "array" && name != "ElectricSubsetResponse" {
			t.Errorf("schema %s is a top-level array", name)
		}
		if _, collection := schema.Properties["items"]; collection {
			if _, paginated := schema.Properties["page"]; !paginated {
				t.Errorf("collection schema %s has items without page", name)
			}
		}
		if _, legacy := schema.Properties["data"]; legacy && name != "ElectricSubsetResponse" {
			t.Errorf("schema %s uses legacy top-level data field", name)
		}
	}
}

func hasAnyPrefix(value string, prefixes []string) bool {
	for _, prefix := range prefixes {
		if strings.HasPrefix(value, prefix) {
			return true
		}
	}
	return false
}
