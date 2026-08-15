package federation

import (
	"os"
	"strings"
	"testing"
)

func TestFederationSQLGuardsReplayAndAtomicStateShape(t *testing.T) {
	t.Parallel()
	testCases := []struct {
		name      string
		path      string
		required  []string
		forbidden []string
	}{
		{
			name: "generated query source",
			path: "../database/queries/federation.sql",
			required: []string{
				"GREATEST(ops.federation_cursors.event_id, EXCLUDED.event_id)",
				"WHERE network.records.source_event_id < EXCLUDED.source_event_id",
				"WHERE network.repositories.source_event_id < EXCLUDED.source_event_id",
				"FROM core.repository_transfers AS transfer",
				"candidate_edges AS", "valid_edges AS", "overlong_nodes AS", "invalid_nodes AS",
				"SELECT count(*) FROM network.repositories",
			},
		},
		{
			name: "migration tables",
			path: "../../migrations/000005_federation.sql",
			required: []string{
				"CREATE TABLE network.records", "CREATE TABLE network.identities",
				"CREATE TABLE network.repositories", "ADD COLUMN deleted_at",
				"CREATE TABLE ops.federation_cursors", "CREATE TABLE ops.federation_receipts",
				"PRIMARY KEY (consumer, event_id)",
			},
		},
		{
			name: "star migration",
			path: "../../migrations/000007_stars.sql",
			required: []string{
				"ADD COLUMN star_count BIGINT NOT NULL DEFAULT 0",
				"CHECK (star_count >= 0)",
				"CREATE INDEX network_repositories_star_count_idx",
				"CREATE TABLE network.stars",
				"repository_uri      TEXT NOT NULL",
				"repository_cid      TEXT",
				"source_event_id     BIGINT NOT NULL",
				"ON network.stars (author_did, repository_uri)",
				"WHERE deleted_at IS NULL",
			},
			forbidden: []string{"REFERENCES network.repositories"},
		},
		{
			name: "star projection source guards",
			path: "../database/queries/federation.sql",
			required: []string{
				"-- name: UpsertFederationStar :one",
				"source_record.source_event_id = $9",
				"current_record.source_event_id = EXCLUDED.source_event_id",
				"-- name: TombstoneFederationStar :one",
				"current_record.source_event_id = $3",
				"RETURNING repository_uri",
				"-- name: RecomputeFederationStarCount :exec",
				"count(DISTINCT star.author_did)",
				"-- name: LockFederationRepositoryStars :exec",
				"pg_advisory_xact_lock",
				"-- name: GetFederationStarRepositoryURI :one",
			},
		},
		{
			name: "issue migration",
			path: "../../migrations/000009_issues.sql",
			required: []string{
				"ADD COLUMN issue_count BIGINT NOT NULL DEFAULT 0",
				"ADD COLUMN open_issue_count BIGINT NOT NULL DEFAULT 0",
				"network_repositories_open_issue_count_idx",
				"CREATE TABLE network.issues",
				"repository_cid         TEXT NOT NULL",
				"status_source_event_id BIGINT",
				"CREATE TABLE network.issue_statuses",
				"issue_cid           TEXT NOT NULL",
				"source_event_id     BIGINT NOT NULL",
			},
			forbidden: []string{"REFERENCES network.repositories", "REFERENCES network.issues"},
		},
		{
			name: "issue projection source authority and locking",
			path: "../database/queries/federation.sql",
			required: []string{
				"-- name: UpsertFederationIssue :one",
				"source_record.source_event_id = $12",
				"current_record.source_event_id = EXCLUDED.source_event_id",
				"-- name: UpsertFederationIssueStatus :one",
				"source_record.source_event_id = $13",
				"authority_repository.uri = source_repository.canonical_uri",
				"candidate.author_did = authority_repository.owner_did",
				"ORDER BY candidate.source_event_id DESC, candidate.uri DESC",
				"-- name: LockFederationRepositoryIssues :exec",
				"-- name: RecomputeFederationIssueCounts :exec",
			},
		},
		{
			name: "comment and moderation migration",
			path: "../../migrations/000010_comments_moderation.sql",
			required: []string{
				"CREATE SCHEMA moderation",
				"ADD COLUMN comment_count BIGINT NOT NULL DEFAULT 0",
				"network_issues_comment_count_nonnegative CHECK (comment_count >= 0)",
				"CREATE TABLE network.issue_comments",
				"issue_cid           TEXT NOT NULL",
				"parent_uri          TEXT",
				"parent_cid          TEXT",
				"network_issue_comments_issue_idx",
				"network_issue_comments_parent_idx",
				"network_issue_comments_author_idx",
				"CREATE TABLE moderation.blocked_dids",
				"CREATE TABLE moderation.hidden_records",
				"PRIMARY KEY (account_did, blocked_did)",
				"PRIMARY KEY (account_did, record_uri)",
				"account_did TEXT NOT NULL REFERENCES core.accounts(did) ON DELETE CASCADE",
			},
			forbidden: []string{"REFERENCES network.issues", "REFERENCES network.issue_comments", "blocked_did TEXT NOT NULL REFERENCES"},
		},
		{
			name: "comment projection source guards counts and filtered read",
			path: "../database/queries/federation.sql",
			required: []string{
				"-- name: UpsertFederationIssueComment :one",
				"source_record.source_event_id = $13",
				"network.issue_comments.source_event_id < EXCLUDED.source_event_id",
				"current_record.source_event_id = EXCLUDED.source_event_id",
				"-- name: TombstoneFederationIssueComment :one",
				"-- name: LockFederationIssueComments :exec",
				"-- name: RecomputeFederationIssueCommentCount :exec",
				"UPDATE network.issues AS issue SET comment_count",
				"-- name: RecomputeFederationRepositoryCommentCount :exec",
				"network.issue_comments.issue_uri = EXCLUDED.issue_uri",
				"network.issue_comments.parent_uri IS NOT DISTINCT FROM EXCLUDED.parent_uri",
				"parent.issue_uri = comment.issue_uri",
				"-- name: ListFederationCommentChildIssueURIs :many",
				"-- name: ListNetworkIssueComments :many",
				"FROM moderation.blocked_dids AS blocked",
				"FROM moderation.hidden_records AS hidden",
			},
		},
		{
			name: "owner-scoped moderation queries",
			path: "../database/queries/moderation.sql",
			required: []string{
				"-- name: BlockDID :exec", "-- name: UnblockDID :exec", "-- name: ListBlockedDIDs :many", "-- name: IsDIDBlocked :one",
				"-- name: HideRecord :exec", "-- name: UnhideRecord :exec", "-- name: ListHiddenRecords :many", "-- name: IsRecordHidden :one",
				"WHERE account_did = $1 AND blocked_did = $2",
				"WHERE account_did = $1 AND record_uri = $2",
			},
		},
		{
			name: "pull request migration",
			path: "../../migrations/000011_pull_requests.sql",
			required: []string{
				"ADD COLUMN pull_request_count BIGINT NOT NULL DEFAULT 0",
				"ADD COLUMN open_pull_request_count BIGINT NOT NULL DEFAULT 0",
				"CREATE TABLE network.pull_requests",
				"source_repository_uri  TEXT NOT NULL",
				"target_repository_uri  TEXT NOT NULL",
				"status_source_event_id BIGINT",
				"review_count           BIGINT NOT NULL DEFAULT 0",
				"CREATE TABLE network.pull_request_statuses",
				"CREATE TABLE network.pull_request_reviews",
				"merged_commit_sha",
			},
			forbidden: []string{"REFERENCES network.repositories", "REFERENCES network.pull_requests"},
		},
		{
			name: "pull request projection guards authority counts and locks",
			path: "../database/queries/federation.sql",
			required: []string{
				"-- name: UpsertFederationPullRequest :one",
				"source_record.source_event_id = $17",
				"network.pull_requests.source_repository_uri = EXCLUDED.source_repository_uri",
				"network.pull_requests.source_branch = EXCLUDED.source_branch",
				"network.pull_requests.target_repository_uri = EXCLUDED.target_repository_uri",
				"network.pull_requests.target_branch = EXCLUDED.target_branch",
				"-- name: UpsertFederationPullRequestStatus :one",
				"network.pull_request_statuses.pull_request_uri = EXCLUDED.pull_request_uri",
				"network.pull_request_statuses.target_repository_uri = EXCLUDED.target_repository_uri",
				"authority_repository.uri = source_target_repository.canonical_uri",
				"candidate.author_did = authority_repository.owner_did",
				"candidate.pull_request_cid = pull_request.cid",
				"ORDER BY candidate.source_event_id DESC, candidate.uri DESC",
				"-- name: UpsertFederationPullRequestReview :one",
				"network.pull_request_reviews.pull_request_uri = EXCLUDED.pull_request_uri",
				"-- name: RecomputeFederationPullRequestReviewCount :exec",
				"review.pull_request_cid = network.pull_requests.cid",
				"-- name: RecomputeFederationPullRequestCounts :exec",
				"-- name: LockFederationPullRequest :exec",
				"-- name: LockFederationRepositoryPullRequests :exec",
			},
		},
		{
			name: "pull request public reads use exact live observations",
			path: "../database/queries/federation.sql",
			required: []string{
				"-- name: GetProjectedPullRequestRepositoryTargets :one",
				"source.deleted_at IS NULL AND source.cid IS NOT NULL",
				"target.deleted_at IS NULL AND target.cid IS NOT NULL",
				"-- name: ListProjectedPullRequests :many",
				"status.pull_request_cid = pull_request.cid",
				"review.pull_request_cid = pull_request.cid",
				"-- name: ListProjectedPullRequestReviews :many",
				"pull_request.cid = review.pull_request_cid",
				"-- name: GetProjectedPullRequestStatusTarget :one",
			},
		},
		{
			name: "repository transfer migration",
			path: "../../migrations/000021_repository_transfers.sql",
			required: []string{
				"CREATE TABLE core.repository_transfers", "accepted_by_did TEXT REFERENCES core.accounts(did)",
				"CREATE UNIQUE INDEX repository_transfers_pending_uidx", "CREATE TABLE network.repository_transfers",
				"CREATE TABLE network.repository_transfer_acceptances", "ADD COLUMN transferred_from_uri TEXT",
				"acceptance_started_at TIMESTAMPTZ", "repository_transfers_acceptance_window CHECK",
				"ADD COLUMN lineage_uri TEXT", "ALTER COLUMN canonical_uri SET NOT NULL",
			},
		},
		{
			name: "repository transfer projection requires a bilateral acyclic chain",
			path: "../database/queries/federation.sql",
			required: []string{
				"-- name: UpsertFederationRepositoryTransfer :exec", "-- name: UpsertFederationRepositoryTransferAcceptance :exec",
				"acceptance.proposal_cid = proposal.cid", "proposal.author_did = source.owner_did",
				"proposal.repository_cid = source.cid", "acceptance.repository_cid = successor.cid",
				"source.transferred_to_uri = acceptance.repository_uri", "successor.transferred_from_uri = proposal.repository_uri",
				"acceptance.author_did = proposal.destination_did", "cyclic_nodes AS",
				"-- name: LockFederationRepositoryTransferLineages :exec", "-- name: RecomputeFederationRepositoryLineageCounts :exec",
				"count(DISTINCT star.author_did)", "repository.uri = requested_repository.canonical_uri",
			},
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
			for _, forbidden := range testCase.forbidden {
				if strings.Contains(string(contents), forbidden) {
					t.Fatalf("%s unexpectedly contains %q", testCase.path, forbidden)
				}
			}
		})
	}
}
