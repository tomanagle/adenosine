package federation

import (
	"os"
	"strings"
	"testing"
)

func TestNetworkRepositoryDiscoverySQL(t *testing.T) {
	t.Parallel()
	testCases := []struct {
		name     string
		path     string
		required []string
	}{
		{
			name:     "partial descending index",
			path:     "../../migrations/000006_network_repository_discovery.sql",
			required: []string{"(indexed_at DESC, uri DESC)", "WHERE deleted_at IS NULL"},
		},
		{
			name:     "live joined keyset query",
			path:     "../database/queries/federation.sql",
			required: []string{"-- name: ListNetworkRepositories :many", "LEFT JOIN network.identities", "repository.deleted_at IS NULL", "repository.indexed_at < sqlc.narg(cursor_indexed_at)", "repository.uri < sqlc.narg(cursor_uri)::text", "ORDER BY repository.indexed_at DESC, repository.uri DESC", "LIMIT sqlc.arg(page_size)"},
		},
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
