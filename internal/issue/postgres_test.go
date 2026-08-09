package issue

import (
	"os"
	"strings"
	"testing"
)

func TestIssueProjectionSQL(t *testing.T) {
	t.Parallel()
	testCases := []struct {
		name     string
		required []string
	}{
		{name: "public list and counts use one bounded current projection query", required: []string{"-- name: GetNetworkIssueProjection :many", "repository.issue_count", "repository.open_issue_count", "issue.deleted_at IS NULL", "issue.cid IS NOT NULL", "ORDER BY issue.record_created_at DESC, issue.uri DESC", "LIMIT sqlc.arg(page_size)"}},
		{name: "status target resolves active current strong refs and creation time", required: []string{"-- name: GetNetworkIssueStatusWriteTarget :one", "repository.cid AS repository_cid", "status.record_created_at AS status_created_at", "issue.deleted_at IS NULL", "repository.deleted_at IS NULL"}},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			contents, err := os.ReadFile("../database/queries/federation.sql")
			if err != nil {
				t.Fatal(err)
			}
			for _, required := range testCase.required {
				if !strings.Contains(string(contents), required) {
					t.Fatalf("federation.sql does not contain %q", required)
				}
			}
		})
	}
}
