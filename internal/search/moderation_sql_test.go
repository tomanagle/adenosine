package search

import (
	"os"
	"strings"
	"testing"
)

func TestCollaborationModerationSQL(t *testing.T) {
	t.Parallel()
	testCases := []struct {
		name     string
		path     string
		required []string
	}{
		{name: "collaboration and profile projections", path: "../database/queries/search.sql", required: []string{"ResolveSearchProfile", "ListSearchIssues", "ListSearchStars", "ListSearchPullRequests", "ResolveSearchPullRequest", "ListSearchPullRequestReviews", "repository.owner_did", "visible_comment_count", "visible_review_count", "visible_repository_count", "visible_contribution_count", "blocked_did IN (repository.owner_did, issue.author_did, comment.author_did)", "hidden.record_uri IN (repository.uri, issue.uri, comment.uri)", "blocked_did IN (repository.owner_did, pull.author_did, review.author_did)", "hidden.record_uri IN (repository.uri, pull.uri, review.uri)"}},
		{name: "thread target inherits issue repository moderation", path: "../database/queries/federation.sql", required: []string{"JOIN network.repositories repository ON repository.uri = issue.repository_uri", "blocked_did IN (repository.owner_did, issue.author_did)", "hidden.record_uri IN (repository.uri, issue.uri)"}},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			contents, err := os.ReadFile(testCase.path)
			if err != nil {
				t.Fatal(err)
			}
			for _, required := range testCase.required {
				if !strings.Contains(string(contents), required) {
					t.Fatalf("%s does not contain %q", testCase.path, required)
				}
			}
		})
	}
}
