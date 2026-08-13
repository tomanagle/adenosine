package pullrequest

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/adenosine-dev/adenosine/internal/repository"
	"github.com/google/uuid"
)

func TestReviewRequestLifecycle(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 8, 14, 12, 0, 0, 0, time.UTC)
	repositoryID := repository.ID(uuid.MustParse("0198a851-2a89-7ae2-a370-dc68883e3af2"))
	target := statusTarget{
		Subject:          StrongRef{URI: testPullRequestURI, CID: testCID},
		TargetRepository: StrongRef{URI: testTargetRepositoryURI, CID: testCID}, RepositoryID: &repositoryID,
	}
	testCases := []struct {
		name              string
		action            string
		state             State
		triage            bool
		readable          bool
		moderationAllowed bool
		want              error
	}{
		{name: "maintainer requests visible unblocked reviewer", action: "put", state: StateOpen, triage: true, readable: true, moderationAllowed: true},
		{name: "duplicate cancellation is idempotent at publisher", action: "delete", state: StateOpen, triage: true},
		{name: "closed pull request rejects new request", action: "put", state: StateClosed, triage: true, readable: true, moderationAllowed: true, want: ErrConflict},
		{name: "non triager cannot request", action: "put", state: StateOpen, readable: true, moderationAllowed: true, want: ErrPermissionDenied},
		{name: "private repository rejects invisible reviewer", action: "put", state: StateOpen, triage: true, moderationAllowed: true, want: ErrPermissionDenied},
		{name: "block or hidden record rejects request", action: "put", state: StateOpen, triage: true, readable: true, want: ErrPermissionDenied},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			store := &applicationStore{pullRequest: ProjectedPullRequest{State: testCase.state}, status: target, moderationAllowed: testCase.moderationAllowed}
			publisher := &applicationPublisher{}
			authorizer := &applicationAuthorizer{allowed: testCase.triage, readable: testCase.readable}
			service := NewApplicationService(store, nil, publisher, applicationClock{now: now}, authorizer, nil)
			input := ReviewRequestInput{PullRequestURI: testPullRequestURI, ReviewerDID: "did:plc:reviewer"}
			var err error
			if testCase.action == "delete" {
				err = service.DeleteReviewRequest(context.Background(), "did:plc:maintainer", input)
			} else {
				_, err = service.PutReviewRequest(context.Background(), "did:plc:maintainer", input)
			}
			if !errors.Is(err, testCase.want) {
				t.Fatalf("review request error = %v, want %v", err, testCase.want)
			}
			if err == nil && testCase.action == "put" {
				record := publisher.reviewRequestRecord
				if publisher.author != "did:plc:target" || record.Subject != target.Subject || record.TargetRepository != target.TargetRepository || record.ReviewerDID != input.ReviewerDID || record.RequestedByDID != "did:plc:maintainer" || record.CreatedAt != now {
					t.Fatalf("published review request = %q %+v", publisher.author, record)
				}
			}
			if err == nil && testCase.action == "delete" && (publisher.author != "did:plc:target" || publisher.deletedReviewRequest != testPullRequestURI+"\ndid:plc:reviewer") {
				t.Fatalf("cancelled review request = %q %q", publisher.author, publisher.deletedReviewRequest)
			}
		})
	}
}

func TestReviewRequestPagination(t *testing.T) {
	t.Parallel()
	updatedAt := time.Date(2026, 8, 14, 12, 0, 0, 123, time.UTC)
	uri := "at://did:plc:target/" + ReviewRequestCollection + "/aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	items := []ProjectedReviewRequest{
		{ReviewRequest: ReviewRequest{URI: uri, ReviewRequestRecord: ReviewRequestRecord{UpdatedAt: updatedAt}}},
		{ReviewRequest: ReviewRequest{URI: uri + "a", ReviewRequestRecord: ReviewRequestRecord{UpdatedAt: updatedAt.Add(-time.Second)}}},
	}
	testCases := []struct {
		name     string
		cursor   string
		limit    int
		wantErr  error
		wantLen  int
		wantNext bool
	}{
		{name: "bounded keyset page", limit: 1, wantLen: 1, wantNext: true},
		{name: "cursor round trip", cursor: encodeReviewRequestCursor(updatedAt, uri), limit: 10, wantLen: 2},
		{name: "malformed cursor", cursor: "not-a-cursor", limit: 10, wantErr: ErrValidation},
		{name: "limit too high", limit: 101, wantErr: ErrValidation},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			store := &applicationStore{reviewRequests: items}
			service := NewApplicationService(store, nil, &applicationPublisher{}, applicationClock{}, &applicationAuthorizer{}, nil)
			page, err := service.ReviewRequests(context.Background(), testPullRequestURI, "did:plc:viewer", testCase.cursor, testCase.limit)
			if !errors.Is(err, testCase.wantErr) {
				t.Fatalf("ReviewRequests() error = %v, want %v", err, testCase.wantErr)
			}
			if err == nil && (len(page.Items) != testCase.wantLen || (page.NextCursor != "") != testCase.wantNext) {
				t.Fatalf("page = %+v", page)
			}
		})
	}
}
