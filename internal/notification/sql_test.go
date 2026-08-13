package notification

import (
	"os"
	"strings"
	"testing"
)

func TestReviewRequestNotificationProjection(t *testing.T) {
	t.Parallel()
	testCases := []struct {
		name      string
		required  []string
		forbidden []string
	}{
		{
			name: "derives current visible reviewer request without canonical notification records",
			required: []string{
				"'pull_request_review_request'::text",
				"request.reviewer_did = sqlc.arg(account_did)",
				"request.requested_by_did <> sqlc.arg(account_did)",
				"pull.cid = request.pull_request_cid",
				"request.deleted_at IS NULL AND request.cid IS NOT NULL",
				"FROM moderation.blocked_dids AS block",
				"FROM moderation.hidden_records AS hidden",
				"(activity.occurred_at, activity.id) <",
			},
			forbidden: []string{"CREATE TABLE notifications", "INSERT INTO notifications"},
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			contents, err := os.ReadFile("../database/queries/notifications.sql")
			if err != nil {
				t.Fatal(err)
			}
			for _, required := range testCase.required {
				if !strings.Contains(string(contents), required) {
					t.Fatalf("notification query does not contain %q", required)
				}
			}
			for _, forbidden := range testCase.forbidden {
				if strings.Contains(string(contents), forbidden) {
					t.Fatalf("notification query contains forbidden %q", forbidden)
				}
			}
		})
	}
}
