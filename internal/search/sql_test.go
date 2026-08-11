package search

import (
	"os"
	"strings"
	"testing"
)

func TestSearchSQLBoundaries(t *testing.T) {
	t.Parallel()
	testCases := []struct {
		name      string
		path      string
		required  []string
		forbidden []string
	}{
		{
			name: "PostgreSQL full text and trigram indexes", path: "../../migrations/000012_search.sql",
			required: []string{"CREATE EXTENSION IF NOT EXISTS pg_trgm", "to_tsvector('simple'", "gin_trgm_ops", "network_identities_handle_trgm_idx"},
		},
		{
			name: "local projection visibility moderation and neutral rank", path: "../database/queries/search.sql",
			required:  []string{"FROM network.repositories", "FROM network.profiles", "local_repository.visibility = 'public'", "moderation.blocked_dids", "moderation.hidden_records", "websearch_to_tsquery", "ORDER BY", "repository.local_repository_id"},
			forbidden: []string{"http://", "https://", "local_repository_id IS NOT NULL THEN", "topics"},
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			contents, err := os.ReadFile(testCase.path)
			if err != nil {
				t.Fatal(err)
			}
			text := string(contents)
			for _, required := range testCase.required {
				if !strings.Contains(text, required) {
					t.Fatalf("%s does not contain %q", testCase.path, required)
				}
			}
			for _, forbidden := range testCase.forbidden {
				if strings.Contains(text, forbidden) {
					t.Fatalf("%s unexpectedly contains %q", testCase.path, forbidden)
				}
			}
		})
	}
}
