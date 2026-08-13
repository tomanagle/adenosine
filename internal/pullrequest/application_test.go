package pullrequest

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/adenosine-dev/adenosine/internal/repository"
	"github.com/google/uuid"
)

type applicationStore struct {
	projection   Projection
	pullRequest  ProjectedPullRequest
	targets      repositoryTargets
	reviews      []ProjectedReview
	reviewTarget StrongRef
	status       statusTarget
	err          error
	limit        int
}

func (store *applicationStore) GetFetchTarget(context.Context, string) (fetchTarget, error) {
	return fetchTarget{}, store.err
}
func (store *applicationStore) GetMergeTarget(context.Context, string) (mergeTarget, error) {
	return mergeTarget{}, store.err
}
func (store *applicationStore) List(_ context.Context, _ string, limit int) (Projection, error) {
	store.limit = limit
	return store.projection, store.err
}
func (store *applicationStore) Get(context.Context, string) (ProjectedPullRequest, error) {
	return store.pullRequest, store.err
}
func (store *applicationStore) GetRepositoryTargets(context.Context, string, string) (repositoryTargets, error) {
	return store.targets, store.err
}
func (store *applicationStore) ListReviews(_ context.Context, _ string, limit int) ([]ProjectedReview, error) {
	store.limit = limit
	return store.reviews, store.err
}
func (store *applicationStore) GetReviewTarget(context.Context, string) (StrongRef, error) {
	return store.reviewTarget, store.err
}
func (store *applicationStore) GetStatusTarget(context.Context, string) (statusTarget, error) {
	return store.status, store.err
}

type applicationPublisher struct {
	pullRequest  PullRequest
	review       Review
	status       Status
	record       Record
	reviewRecord ReviewRecord
	statusRecord StatusRecord
	author       string
}

func (publisher *applicationPublisher) CreatePullRequest(_ context.Context, author, _ string, record Record) (PullRequest, error) {
	publisher.author, publisher.record = author, record
	return publisher.pullRequest, nil
}
func (publisher *applicationPublisher) CreatePullRequestReview(_ context.Context, author, _ string, record ReviewRecord) (Review, error) {
	publisher.author, publisher.reviewRecord = author, record
	return publisher.review, nil
}
func (publisher *applicationPublisher) PutPullRequestStatus(_ context.Context, author string, record StatusRecord) (Status, error) {
	publisher.author, publisher.statusRecord = author, record
	return publisher.status, nil
}

type applicationClock struct{ now time.Time }

func (clock applicationClock) Now() time.Time { return clock.now }

type applicationAuthorizer struct {
	allowed bool
	calls   int
}

func (*applicationAuthorizer) CanWriteRepository(context.Context, string, repository.ID) (bool, error) {
	return false, nil
}

func (authorizer *applicationAuthorizer) CanTriageRepository(context.Context, string, repository.ID) (bool, error) {
	authorizer.calls++
	return authorizer.allowed, nil
}

func TestApplicationServiceUsesCurrentTargetsAndBoundsReads(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)
	store := &applicationStore{targets: repositoryTargets{Source: StrongRef{URI: testSourceRepositoryURI, CID: testCID}, Target: StrongRef{URI: testTargetRepositoryURI, CID: testCID}}, reviewTarget: StrongRef{URI: testPullRequestURI, CID: testCID}}
	publisher := &applicationPublisher{}
	service := NewApplicationService(store, nil, publisher, applicationClock{now: now}, nil, nil)
	testCases := []struct {
		name   string
		invoke func() error
		check  func(*testing.T)
	}{
		{name: "list bounded to 100", invoke: func() error { _, err := service.List(context.Background(), testTargetRepositoryURI); return err }, check: func(t *testing.T) {
			if store.limit != 100 {
				t.Fatalf("limit = %d", store.limit)
			}
		}},
		{name: "create uses exact live refs", invoke: func() error {
			_, err := service.Create(context.Background(), "did:plc:contributor", CreateInput{SourceRepositoryURI: testSourceRepositoryURI, TargetRepositoryURI: testTargetRepositoryURI, SourceBranch: "feature", TargetBranch: "main", HeadSHA: testSHA1, Title: "title", Body: "body"})
			return err
		}, check: func(t *testing.T) {
			if publisher.record.SourceRepository != store.targets.Source || publisher.record.TargetRepository != store.targets.Target || publisher.record.CreatedAt != now || publisher.author != "did:plc:contributor" {
				t.Fatalf("published record = %#v", publisher.record)
			}
		}},
		{name: "reviews bounded and exact", invoke: func() error { _, err := service.Reviews(context.Background(), testPullRequestURI); return err }, check: func(t *testing.T) {
			if store.limit != 100 {
				t.Fatalf("limit = %d", store.limit)
			}
		}},
		{name: "review uses current PR CID", invoke: func() error {
			_, err := service.CreateReview(context.Background(), "did:plc:reviewer", ReviewInput{PullRequestURI: testPullRequestURI, Verdict: VerdictApprove, Body: "ok"})
			return err
		}, check: func(t *testing.T) {
			if publisher.reviewRecord.Subject != store.reviewTarget {
				t.Fatalf("review subject = %#v", publisher.reviewRecord.Subject)
			}
		}},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			if err := testCase.invoke(); err != nil {
				t.Fatalf("operation error = %v", err)
			}
			testCase.check(t)
		})
	}
}

func TestApplicationStatusRequiresOwnerPreservesCreatedAtAndRejectsMerged(t *testing.T) {
	t.Parallel()
	createdAt := time.Date(2026, 8, 9, 12, 0, 0, 0, time.UTC)
	now := createdAt.Add(time.Hour)
	repositoryID := repository.ID(uuid.MustParse("0198a851-2a89-7ae2-a370-dc68883e3af2"))
	testCases := []struct {
		name, author string
		state        State
		local        bool
		allowed      bool
		want         error
		wantCalls    bool
		wantTriage   int
	}{
		{name: "owner closes", author: "did:plc:target", state: StateClosed, wantCalls: true},
		{name: "triager closes through target authority", author: "did:plc:other", state: StateClosed, local: true, allowed: true, wantCalls: true, wantTriage: 1},
		{name: "non-owner without triage rejected", author: "did:plc:other", state: StateClosed, local: true, want: ErrAuthorization, wantTriage: 1},
		{name: "merged belongs to merge endpoint", author: "did:plc:target", state: StateMerged, want: ErrValidation},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			target := statusTarget{Subject: StrongRef{URI: testPullRequestURI, CID: testCID}, TargetRepository: StrongRef{URI: testTargetRepositoryURI, CID: testCID}, StatusCreatedAt: createdAt}
			if testCase.local {
				target.RepositoryID = &repositoryID
			}
			store := &applicationStore{status: target}
			publisher := &applicationPublisher{}
			authorizer := &applicationAuthorizer{allowed: testCase.allowed}
			_, err := NewApplicationService(store, nil, publisher, applicationClock{now: now}, authorizer, nil).PutStatus(context.Background(), testCase.author, StatusInput{PullRequestURI: testPullRequestURI, State: testCase.state})
			if !errors.Is(err, testCase.want) {
				t.Fatalf("PutStatus() error = %v, want %v", err, testCase.want)
			}
			if testCase.wantCalls && (publisher.author != "did:plc:target" || publisher.statusRecord.CreatedAt != createdAt || publisher.statusRecord.UpdatedAt != now || publisher.statusRecord.MergeCommitSHA != "") {
				t.Fatalf("status author/record = %q/%#v", publisher.author, publisher.statusRecord)
			}
			if authorizer.calls != testCase.wantTriage {
				t.Fatalf("triage calls = %d, want %d", authorizer.calls, testCase.wantTriage)
			}
		})
	}
}
