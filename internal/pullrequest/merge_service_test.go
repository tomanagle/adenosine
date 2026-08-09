package pullrequest

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/adenosine-dev/adenosine/internal/event"
	gitservice "github.com/adenosine-dev/adenosine/internal/git"
	"github.com/adenosine-dev/adenosine/internal/repository"
	"github.com/google/uuid"
)

type mergeStore struct {
	targets []mergeTarget
	fetch   fetchTarget
	err     error
	calls   int
}

func (store *mergeStore) GetMergeTarget(context.Context, string) (mergeTarget, error) {
	if store.err != nil {
		return mergeTarget{}, store.err
	}
	index := store.calls
	if index >= len(store.targets) {
		index = len(store.targets) - 1
	}
	store.calls++
	return store.targets[index], nil
}
func (store *mergeStore) GetFetchTarget(context.Context, string) (fetchTarget, error) {
	return store.fetch, store.err
}
func (*mergeStore) List(context.Context, string, int) (Projection, error) { return Projection{}, nil }
func (*mergeStore) Get(context.Context, string) (ProjectedPullRequest, error) {
	return ProjectedPullRequest{}, nil
}
func (*mergeStore) GetRepositoryTargets(context.Context, string, string) (repositoryTargets, error) {
	return repositoryTargets{}, nil
}
func (*mergeStore) ListReviews(context.Context, string, int) ([]ProjectedReview, error) {
	return nil, nil
}
func (*mergeStore) GetReviewTarget(context.Context, string) (StrongRef, error) {
	return StrongRef{}, nil
}
func (*mergeStore) GetStatusTarget(context.Context, string) (statusTarget, error) {
	return statusTarget{}, nil
}

type mergeGit struct {
	commit       gitservice.Commit
	result       gitservice.MergeResult
	mergeErr     error
	mergeCalls   int
	mergeRequest gitservice.MergeRequest
}

func (*mergeGit) ControlledHead(context.Context, repository.ID, string) (string, error) {
	return testSHA1, nil
}
func (*mergeGit) FetchRemote(context.Context, repository.ID, gitservice.RemoteFetch) error {
	return nil
}
func (*mergeGit) MergeBase(context.Context, repository.ID, string, string) (string, error) {
	return testSHA1, nil
}
func (*mergeGit) Diff(context.Context, repository.ID, string, string) (gitservice.Diff, error) {
	return gitservice.Diff{}, nil
}
func (git *mergeGit) Commit(context.Context, repository.ID, string) (gitservice.Commit, error) {
	return git.commit, nil
}
func (git *mergeGit) Merge(_ context.Context, _ repository.ID, request gitservice.MergeRequest) (gitservice.MergeResult, error) {
	git.mergeCalls++
	git.mergeRequest = request
	return git.result, git.mergeErr
}

type mergeAuthorizer struct {
	allowed bool
	err     error
}

func (authorizer mergeAuthorizer) CanWriteRepository(context.Context, string, repository.ID) (bool, error) {
	return authorizer.allowed, authorizer.err
}

type mergeEvents struct {
	calls int
	input event.GitRefsUpdated
	err   error
}

func (events *mergeEvents) GitRefsUpdated(_ context.Context, input event.GitRefsUpdated) error {
	events.calls++
	events.input = input
	return events.err
}

type mergePublisher struct {
	applicationPublisher
	err   error
	calls int
}

func (publisher *mergePublisher) PutPullRequestStatus(ctx context.Context, author string, record StatusRecord) (Status, error) {
	publisher.calls++
	if publisher.err != nil {
		return Status{}, publisher.err
	}
	return publisher.applicationPublisher.PutPullRequestStatus(ctx, author, record)
}

func TestMergeOrchestrationStrategiesAndFailures(t *testing.T) {
	t.Parallel()
	target, fetch, oldSHA, newSHA := mergeFixtures()
	testCases := []struct {
		name        string
		strategy    gitservice.MergeStrategy
		state       State
		allowed     bool
		changed     bool
		gitErr      error
		eventErr    error
		publishErr  error
		want        error
		wantMerges  int
		wantEvents  int
		wantPublish int
	}{
		{name: "merge commit", strategy: gitservice.MergeCommit, state: StateOpen, allowed: true, wantMerges: 1, wantEvents: 1, wantPublish: 1},
		{name: "squash", strategy: gitservice.MergeSquash, state: StateOpen, allowed: true, wantMerges: 1, wantEvents: 1, wantPublish: 1},
		{name: "unauthorized", strategy: gitservice.MergeCommit, state: StateOpen, want: ErrPermissionDenied},
		{name: "closed", strategy: gitservice.MergeCommit, state: StateClosed, allowed: true, want: ErrConflict},
		{name: "merged", strategy: gitservice.MergeCommit, state: StateMerged, allowed: true, want: ErrConflict},
		{name: "projection changed", strategy: gitservice.MergeCommit, state: StateOpen, allowed: true, changed: true, want: ErrConflict},
		{name: "content conflict", strategy: gitservice.MergeCommit, state: StateOpen, allowed: true, gitErr: gitservice.ErrMergeConflict, want: ErrConflict, wantMerges: 1},
		{name: "CAS conflict", strategy: gitservice.MergeCommit, state: StateOpen, allowed: true, gitErr: gitservice.ErrMergeRefConflict, want: ErrConflict, wantMerges: 1},
		{name: "event failure prevents publication", strategy: gitservice.MergeCommit, state: StateOpen, allowed: true, eventErr: errors.New("outbox"), want: errors.New("outbox"), wantMerges: 1, wantEvents: 1},
		{name: "publication failure follows event", strategy: gitservice.MergeCommit, state: StateOpen, allowed: true, publishErr: errors.New("provider"), want: errors.New("provider"), wantMerges: 1, wantEvents: 1, wantPublish: 1},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			current := target
			current.State = testCase.state
			targets := []mergeTarget{current, current}
			if testCase.changed {
				changed := current
				changed.Title = "changed"
				targets[1] = changed
			}
			store := &mergeStore{targets: targets, fetch: fetch}
			git := &mergeGit{commit: gitservice.Commit{SHA: oldSHA}, result: gitservice.MergeResult{OldSHA: oldSHA, NewSHA: newSHA, HeadSHA: target.HeadSHA, TargetRef: "refs/heads/main", Strategy: testCase.strategy}, mergeErr: testCase.gitErr}
			events := &mergeEvents{err: testCase.eventErr}
			publisher := &mergePublisher{applicationPublisher: applicationPublisher{status: Status{AuthorDID: target.TargetOwnerDID}}, err: testCase.publishErr}
			result, err := NewApplicationService(store, git, publisher, applicationClock{now: target.CreatedAt.Add(time.Hour)}, mergeAuthorizer{allowed: testCase.allowed}, events).Merge(context.Background(), "did:plc:collaborator", MergeInput{PullRequestURI: target.Subject.URI, Strategy: testCase.strategy})
			if testCase.want != nil && !errors.Is(err, testCase.want) && !strings.Contains(errString(err), testCase.want.Error()) {
				t.Fatalf("Merge() error = %v, want %v", err, testCase.want)
			}
			if testCase.want == nil && err != nil {
				t.Fatalf("Merge() error = %v", err)
			}
			if git.mergeCalls != testCase.wantMerges || events.calls != testCase.wantEvents || publisher.calls != testCase.wantPublish {
				t.Fatalf("merge/event/publish calls = %d/%d/%d, want %d/%d/%d", git.mergeCalls, events.calls, publisher.calls, testCase.wantMerges, testCase.wantEvents, testCase.wantPublish)
			}
			if testCase.want == nil {
				if git.mergeRequest.Strategy != testCase.strategy || result.Git.NewSHA != newSHA || publisher.author != target.TargetOwnerDID || publisher.statusRecord.MergeCommitSHA != newSHA || events.input.ActorDID != "did:plc:collaborator" {
					t.Fatalf("merge result/request/event/status = %#v %#v %#v %#v", result, git.mergeRequest, events.input, publisher.statusRecord)
				}
			}
		})
	}
}

func TestMergeRetryRecognizesBothStrategiesWithoutDuplicateCommit(t *testing.T) {
	t.Parallel()
	target, fetch, oldSHA, newSHA := mergeFixtures()
	for _, strategy := range []gitservice.MergeStrategy{gitservice.MergeCommit, gitservice.MergeSquash} {
		t.Run(string(strategy), func(t *testing.T) {
			parents := []string{oldSHA}
			if strategy == gitservice.MergeCommit {
				parents = append(parents, target.HeadSHA)
			}
			git := &mergeGit{commit: gitservice.Commit{SHA: newSHA, Parents: parents, Message: mergeMessage(target, mergeTrailers(target, strategy))}}
			events := &mergeEvents{}
			publisher := &mergePublisher{applicationPublisher: applicationPublisher{status: Status{AuthorDID: target.TargetOwnerDID}}}
			result, err := NewApplicationService(&mergeStore{targets: []mergeTarget{target, target}, fetch: fetch}, git, publisher, applicationClock{now: target.CreatedAt.Add(2 * time.Hour)}, mergeAuthorizer{allowed: true}, events).Merge(context.Background(), "did:plc:collaborator", MergeInput{PullRequestURI: target.Subject.URI, Strategy: strategy})
			if err != nil || git.mergeCalls != 0 || result.Git.NewSHA != newSHA || result.Git.OldSHA != oldSHA || events.calls != 1 || publisher.calls != 1 {
				t.Fatalf("retry = %#v, %v; merge/event/publish = %d/%d/%d", result, err, git.mergeCalls, events.calls, publisher.calls)
			}
		})
	}
}

func TestMergeValidatesURIAndStrategyBeforeDependencies(t *testing.T) {
	t.Parallel()
	testCases := []struct {
		name  string
		input MergeInput
	}{
		{name: "non-canonical URI", input: MergeInput{PullRequestURI: "not-an-at-uri", Strategy: gitservice.MergeCommit}},
		{name: "unsupported strategy", input: MergeInput{PullRequestURI: testPullRequestURI, Strategy: "rebase"}},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			_, err := (&Service{}).Merge(context.Background(), "did:plc:actor", testCase.input)
			if !errors.Is(err, ErrValidation) {
				t.Fatalf("Merge() error = %v, want ErrValidation", err)
			}
		})
	}
}

func mergeFixtures() (mergeTarget, fetchTarget, string, string) {
	id := repository.ID(uuid.MustParse("11111111-2222-3333-4444-555555555555"))
	createdAt := time.Date(2026, 8, 10, 10, 0, 0, 0, time.UTC)
	oldSHA, newSHA := strings.Repeat("b", 40), strings.Repeat("c", 40)
	target := mergeTarget{Subject: StrongRef{URI: testPullRequestURI, CID: testCID}, SourceRepository: StrongRef{URI: testSourceRepositoryURI, CID: testCID}, SourceBranch: "feature", HeadSHA: testSHA1, TargetRepository: StrongRef{URI: testTargetRepositoryURI, CID: testCID}, TargetBranch: "main", Title: "Merge title", Body: "body", CreatedAt: createdAt, TargetOwnerDID: "did:plc:target", RepositoryID: id, State: StateOpen, StatusCreatedAt: createdAt.Add(-time.Hour)}
	fetch := fetchTarget{URI: target.Subject.URI, CID: target.Subject.CID, RepositoryID: id, SourceURL: "https://git.example/source.git", SourceBranch: target.SourceBranch, HeadSHA: target.HeadSHA, TargetBranch: target.TargetBranch}
	return target, fetch, oldSHA, newSHA
}

func errString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
