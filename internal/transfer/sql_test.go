package transfer

import (
	"os"
	"strings"
	"testing"
)

func TestTransferSQLPreservesRepositoryContinuity(t *testing.T) {
	t.Parallel()
	testCases := []struct {
		name     string
		required []string
	}{
		{
			name: "atomic ownership and route continuity",
			required: []string{
				"-- name: CanCompleteRepositoryTransfer :one",
				"-- name: CompleteRepositoryTransfer :one",
				"-- name: CompletePrivateRepositoryTransfer :one",
				"source_did_alias_id",
				"source_owner_did",
				"source_owner_alias",
				"INSERT INTO core.repository_aliases",
				"UPDATE core.repositories AS repository",
				"DELETE FROM core.organization_team_repositories",
				"transferred_from_uri = available.source_repository_uri",
			},
		},
		{
			name: "bounded keyset history",
			required: []string{
				"-- name: PageRepositoryTransfers :many",
				"(transfer.created_at, transfer.id) <",
				"ORDER BY transfer.created_at DESC, transfer.id DESC",
				"LIMIT sqlc.arg(page_limit)",
			},
		},
		{
			name: "durable acceptance recovery",
			required: []string{
				"-- name: StartRepositoryTransferAcceptance :one",
				"acceptance_started_at = COALESCE",
				"AND acceptance_started_at IS NULL",
				"transfer.acceptance_started_at < transfer.expires_at",
			},
		},
	}

	contents, err := os.ReadFile("../database/queries/transfers.sql")
	if err != nil {
		t.Fatal(err)
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			for _, required := range testCase.required {
				if !strings.Contains(string(contents), required) {
					t.Fatalf("transfer SQL is missing %q", required)
				}
			}
		})
	}
}
