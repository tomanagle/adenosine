package pullrequest

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/adenosine-dev/adenosine/internal/event"
	gitservice "github.com/adenosine-dev/adenosine/internal/git"
	"github.com/adenosine-dev/adenosine/internal/repository"
)

// ErrNotFound indicates that no live projected pull request has a fetchable local target.
var ErrNotFound = errors.New("projected pull request not found")

// ErrProjectionChanged indicates that the projected pull request changed while its Git state was refreshed.
var ErrProjectionChanged = errors.New("projected pull request changed during refresh")

type fetchTarget struct {
	URI          string
	CID          string
	RepositoryID repository.ID
	SourceURL    string
	SourceBranch string
	HeadSHA      string
	TargetBranch string
}

type projectionStore interface {
	GetFetchTarget(context.Context, string) (fetchTarget, error)
	GetMergeTarget(context.Context, string) (mergeTarget, error)
	List(context.Context, string, int) (Projection, error)
	Get(context.Context, string) (ProjectedPullRequest, error)
	GetRepositoryTargets(context.Context, string, string) (repositoryTargets, error)
	ListReviews(context.Context, string, int) ([]ProjectedReview, error)
	GetReviewTarget(context.Context, string) (StrongRef, error)
	GetStatusTarget(context.Context, string) (statusTarget, error)
}

type gitService interface {
	ControlledHead(context.Context, repository.ID, string) (string, error)
	FetchRemote(context.Context, repository.ID, gitservice.RemoteFetch) error
	MergeBase(context.Context, repository.ID, string, string) (string, error)
	Diff(context.Context, repository.ID, string, string) (gitservice.Diff, error)
	Commit(context.Context, repository.ID, string) (gitservice.Commit, error)
	Merge(context.Context, repository.ID, gitservice.MergeRequest) (gitservice.MergeResult, error)
}

type repositoryWriter interface {
	CanWriteRepository(context.Context, string, repository.ID) (bool, error)
}

type repositoryTriager interface {
	CanTriageRepository(context.Context, string, repository.ID) (bool, error)
}

type mergeEventWriter interface {
	GitRefsUpdated(context.Context, event.GitRefsUpdated) error
}

// Publisher writes contributor pull requests, reviewer reviews, and target-authoritative statuses.
type Publisher interface {
	CreatePullRequest(context.Context, string, string, Record) (PullRequest, error)
	CreatePullRequestReview(context.Context, string, string, ReviewRecord) (Review, error)
	PutPullRequestStatus(context.Context, string, StatusRecord) (Status, error)
}

type clock interface{ Now() time.Time }

// Service coordinates projected reads, asynchronous ATProto writes, and verified Git diffs.
type Service struct {
	store      projectionStore
	git        gitService
	publisher  Publisher
	clock      clock
	authorizer repositoryWriter
	events     mergeEventWriter
}

// Result is the local Git state used to review a projected pull request.
type Result struct {
	RepositoryID repository.ID
	HeadRef      string
	MergeBase    string
	Diff         gitservice.Diff
}

// ProjectedPullRequest is the current locally indexed pull request and authoritative state.
type ProjectedPullRequest struct {
	PullRequest
	State           State
	Status          StrongRef
	MergedCommitSHA string
	ReviewCount     int64
	IndexedAt       time.Time
}

// Projection is a bounded repository pull request projection with repository counters.
type Projection struct {
	PullRequestCount     int64
	OpenPullRequestCount int64
	PullRequests         []ProjectedPullRequest
}

// ProjectedReview is a current review of the exact current pull request CID.
type ProjectedReview struct {
	Review
	IndexedAt time.Time
}

// CreateInput contains contributor-authored pull request content.
type CreateInput struct {
	SourceRepositoryURI string
	TargetRepositoryURI string
	SourceBranch        string
	TargetBranch        string
	HeadSHA             string
	Title               string
	Body                string
}

// ReviewInput contains reviewer-authored feedback for a projected pull request.
type ReviewInput struct {
	PullRequestURI string
	Verdict        Verdict
	Body           string
}

// StatusInput contains target-owner-authoritative open or closed state.
type StatusInput struct {
	PullRequestURI string
	State          State
}

// MergeInput identifies an open pull request and one supported native merge strategy.
type MergeInput struct {
	PullRequestURI string
	Strategy       gitservice.MergeStrategy
}

// MergeResult combines the actual target ref update with its owner-authored status.
type MergeResult struct {
	Git    gitservice.MergeResult
	Status Status
}

type repositoryTargets struct {
	Source StrongRef
	Target StrongRef
}

type statusTarget struct {
	Subject          StrongRef
	TargetRepository StrongRef
	RepositoryID     *repository.ID
	StatusCreatedAt  time.Time
}

type mergeTarget struct {
	Subject          StrongRef
	SourceRepository StrongRef
	SourceBranch     string
	HeadSHA          string
	TargetRepository StrongRef
	TargetBranch     string
	Title            string
	Body             string
	CreatedAt        time.Time
	TargetOwnerDID   string
	RepositoryID     repository.ID
	State            State
	StatusCreatedAt  time.Time
}

// NewService constructs projected pull request Git orchestration.
func NewService(store projectionStore, git gitService) *Service {
	return &Service{store: store, git: git}
}

// NewApplicationService constructs public pull request API orchestration.
func NewApplicationService(store projectionStore, git gitService, publisher Publisher, clock clock, authorizer repositoryWriter, events mergeEventWriter) *Service {
	return &Service{store: store, git: git, publisher: publisher, clock: clock, authorizer: authorizer, events: events}
}

// List returns at most 100 current pull requests targeting a repository.
func (service *Service) List(ctx context.Context, repositoryURI string) (Projection, error) {
	if _, err := RepositoryOwnerDID(repositoryURI); err != nil {
		return Projection{}, err
	}
	projection, err := service.store.List(ctx, repositoryURI, 100)
	if err != nil {
		return Projection{}, projectionError("list projected pull requests", err)
	}
	if projection.PullRequests == nil {
		projection.PullRequests = []ProjectedPullRequest{}
	}
	return projection, nil
}

// Get returns one current projected pull request by canonical URI.
func (service *Service) Get(ctx context.Context, pullRequestURI string) (ProjectedPullRequest, error) {
	if _, err := validateATURI(pullRequestURI, Collection, "pull_request_uri"); err != nil {
		return ProjectedPullRequest{}, err
	}
	value, err := service.store.Get(ctx, pullRequestURI)
	if err != nil {
		return ProjectedPullRequest{}, projectionError("get projected pull request", err)
	}
	return value, nil
}

// Create publishes contributor-authored content against current exact repository refs.
func (service *Service) Create(ctx context.Context, authorDID string, input CreateInput) (PullRequest, error) {
	if _, err := RepositoryOwnerDID(input.SourceRepositoryURI); err != nil {
		return PullRequest{}, err
	}
	if _, err := RepositoryOwnerDID(input.TargetRepositoryURI); err != nil {
		return PullRequest{}, err
	}
	targets, err := service.store.GetRepositoryTargets(ctx, input.SourceRepositoryURI, input.TargetRepositoryURI)
	if err != nil {
		return PullRequest{}, projectionError("get pull request repository targets", err)
	}
	now := service.clock.Now().UTC()
	record := Record{SourceRepository: targets.Source, TargetRepository: targets.Target, SourceBranch: input.SourceBranch,
		TargetBranch: input.TargetBranch, HeadSHA: input.HeadSHA, Title: input.Title, Body: input.Body, CreatedAt: now, UpdatedAt: now}
	if err := record.Validate(); err != nil {
		return PullRequest{}, err
	}
	rkey, err := RandomRecordKey()
	if err != nil {
		return PullRequest{}, fmt.Errorf("create pull request record key: %w", err)
	}
	return service.publisher.CreatePullRequest(ctx, authorDID, rkey, record)
}

// Reviews returns at most 100 reviews attached to the exact current PR CID.
func (service *Service) Reviews(ctx context.Context, pullRequestURI string) ([]ProjectedReview, error) {
	if _, err := validateATURI(pullRequestURI, Collection, "pull_request_uri"); err != nil {
		return nil, err
	}
	if _, err := service.store.GetReviewTarget(ctx, pullRequestURI); err != nil {
		return nil, projectionError("get pull request review target", err)
	}
	values, err := service.store.ListReviews(ctx, pullRequestURI, 100)
	if err != nil {
		return nil, projectionError("list projected pull request reviews", err)
	}
	if values == nil {
		values = []ProjectedReview{}
	}
	return values, nil
}

// CreateReview publishes reviewer-owned feedback against the exact current PR CID.
func (service *Service) CreateReview(ctx context.Context, authorDID string, input ReviewInput) (Review, error) {
	if _, err := validateATURI(input.PullRequestURI, Collection, "pull_request_uri"); err != nil {
		return Review{}, err
	}
	subject, err := service.store.GetReviewTarget(ctx, input.PullRequestURI)
	if err != nil {
		return Review{}, projectionError("get pull request review target", err)
	}
	now := service.clock.Now().UTC()
	record := ReviewRecord{Subject: subject, Verdict: input.Verdict, Body: input.Body, CreatedAt: now, UpdatedAt: now}
	if err := record.Validate(); err != nil {
		return Review{}, err
	}
	rkey, err := RandomRecordKey()
	if err != nil {
		return Review{}, fmt.Errorf("create pull request review record key: %w", err)
	}
	return service.publisher.CreatePullRequestReview(ctx, authorDID, rkey, record)
}

// PutStatus publishes target-owner-authoritative open or closed state.
func (service *Service) PutStatus(ctx context.Context, authorDID string, input StatusInput) (Status, error) {
	if _, err := validateATURI(input.PullRequestURI, Collection, "pull_request_uri"); err != nil {
		return Status{}, err
	}
	if input.State != StateOpen && input.State != StateClosed {
		return Status{}, &ValidationError{Field: "state", Problem: "must be open or closed"}
	}
	target, err := service.store.GetStatusTarget(ctx, input.PullRequestURI)
	if err != nil {
		return Status{}, projectionError("get pull request status target", err)
	}
	ownerDID, err := RepositoryOwnerDID(target.TargetRepository.URI)
	if err != nil {
		return Status{}, err
	}
	if authorDID != ownerDID {
		triager, ok := service.authorizer.(repositoryTriager)
		if !ok || target.RepositoryID == nil {
			return Status{}, &AuthorizationError{Err: errors.New("status actor lacks repository triage permission")}
		}
		allowed, authorizeErr := triager.CanTriageRepository(ctx, authorDID, *target.RepositoryID)
		if authorizeErr != nil {
			return Status{}, fmt.Errorf("authorize pull request status: %w", authorizeErr)
		}
		if !allowed {
			return Status{}, &AuthorizationError{Err: errors.New("status actor lacks repository triage permission")}
		}
	}
	now := service.clock.Now().UTC()
	createdAt := target.StatusCreatedAt
	if createdAt.IsZero() {
		createdAt = now
	}
	record := StatusRecord{Subject: target.Subject, TargetRepository: target.TargetRepository, State: input.State, CreatedAt: createdAt, UpdatedAt: now}
	return service.publisher.PutPullRequestStatus(ctx, ownerDID, record)
}

// Merge refreshes and atomically merges one exact open projection, then records and publishes the result.
func (service *Service) Merge(ctx context.Context, actorDID string, input MergeInput) (MergeResult, error) {
	if _, err := validateATURI(input.PullRequestURI, Collection, "pull_request_uri"); err != nil {
		return MergeResult{}, err
	}
	if input.Strategy != gitservice.MergeCommit && input.Strategy != gitservice.MergeSquash {
		return MergeResult{}, &ValidationError{Field: "strategy", Problem: "must be merge-commit or squash"}
	}
	target, err := service.store.GetMergeTarget(ctx, input.PullRequestURI)
	if err != nil {
		return MergeResult{}, projectionError("get pull request merge target", err)
	}
	if target.State != StateOpen {
		return MergeResult{}, &ConflictError{Err: fmt.Errorf("pull request state is %q", target.State)}
	}
	allowed, err := service.authorizer.CanWriteRepository(ctx, actorDID, target.RepositoryID)
	if err != nil {
		return MergeResult{}, fmt.Errorf("authorize pull request merge: %w", err)
	}
	if !allowed {
		return MergeResult{}, ErrPermissionDenied
	}
	if _, err := service.Refresh(ctx, input.PullRequestURI); err != nil {
		return MergeResult{}, err
	}
	current, err := service.store.GetMergeTarget(ctx, input.PullRequestURI)
	if err != nil {
		return MergeResult{}, projectionError("revalidate pull request merge target", err)
	}
	if current != target || current.State != StateOpen {
		return MergeResult{}, &ConflictError{Err: ErrProjectionChanged}
	}

	targetRef := "refs/heads/" + target.TargetBranch
	commit, err := service.git.Commit(ctx, target.RepositoryID, targetRef)
	if err != nil {
		return MergeResult{}, fmt.Errorf("read pull request merge target ref: %w", err)
	}
	trailers := mergeTrailers(target, input.Strategy)
	gitResult, reused := recognizedMerge(commit, target, input.Strategy, trailers)
	if !reused {
		now := service.clock.Now().UTC()
		identity := gitservice.MergeIdentity{Name: actorDID, Email: mergeEmail(actorDID), Time: now}
		gitResult, err = service.git.Merge(ctx, target.RepositoryID, gitservice.MergeRequest{
			TargetBranch: target.TargetBranch, ExpectedTargetSHA: commit.SHA, HeadSHA: target.HeadSHA,
			Strategy: input.Strategy, Message: mergeMessage(target, trailers), Author: identity, Committer: identity,
		})
		if err != nil {
			if errors.Is(err, gitservice.ErrMergeConflict) || errors.Is(err, gitservice.ErrMergeRefConflict) {
				return MergeResult{}, &ConflictError{Err: err}
			}
			return MergeResult{}, fmt.Errorf("merge pull request Git ref: %w", err)
		}
	}
	if err := service.events.GitRefsUpdated(ctx, event.GitRefsUpdated{
		RepositoryID: target.RepositoryID, Ref: gitResult.TargetRef, OldSHA: gitResult.OldSHA, NewSHA: gitResult.NewSHA,
		HeadSHA: target.HeadSHA, ActorDID: actorDID, PullRequestURI: target.Subject.URI, PullRequestCID: target.Subject.CID, Strategy: string(input.Strategy),
	}); err != nil {
		return MergeResult{}, fmt.Errorf("record pull request merge event: %w", err)
	}
	now := service.clock.Now().UTC()
	createdAt := target.StatusCreatedAt
	if createdAt.IsZero() {
		createdAt = now
	}
	status, err := service.publisher.PutPullRequestStatus(ctx, target.TargetOwnerDID, StatusRecord{
		Subject: target.Subject, TargetRepository: target.TargetRepository, State: StateMerged,
		MergeCommitSHA: gitResult.NewSHA, CreatedAt: createdAt, UpdatedAt: now,
	})
	if err != nil {
		return MergeResult{}, err
	}
	return MergeResult{Git: gitResult, Status: status}, nil
}

func mergeTrailers(target mergeTarget, strategy gitservice.MergeStrategy) string {
	return "Adenosine-Pull-Request: " + target.Subject.URI + "\n" +
		"Adenosine-Pull-Request-CID: " + target.Subject.CID + "\n" +
		"Adenosine-Pull-Request-Head: " + target.HeadSHA + "\n" +
		"Adenosine-Merge-Strategy: " + string(strategy)
}

func mergeMessage(target mergeTarget, trailers string) string {
	message := target.Title
	if target.Body != "" {
		message += "\n\n" + target.Body
	}
	return message + "\n\n" + trailers + "\n"
}

func mergeEmail(actorDID string) string {
	digest := sha256.Sum256([]byte(actorDID))
	return hex.EncodeToString(digest[:16]) + "@users.noreply.adenosine.local"
}

func recognizedMerge(commit gitservice.Commit, target mergeTarget, strategy gitservice.MergeStrategy, trailers string) (gitservice.MergeResult, bool) {
	if !strings.HasSuffix(strings.TrimSpace(commit.Message), trailers) || len(commit.Parents) == 0 {
		return gitservice.MergeResult{}, false
	}
	if strategy == gitservice.MergeCommit && (len(commit.Parents) != 2 || commit.Parents[1] != target.HeadSHA) {
		return gitservice.MergeResult{}, false
	}
	if strategy == gitservice.MergeSquash && len(commit.Parents) != 1 {
		return gitservice.MergeResult{}, false
	}
	return gitservice.MergeResult{OldSHA: commit.Parents[0], NewSHA: commit.SHA, HeadSHA: target.HeadSHA,
		TargetRef: "refs/heads/" + target.TargetBranch, Strategy: strategy}, true
}

func projectionError(operation string, err error) error {
	if errors.Is(err, ErrNotFound) {
		return ErrNotFound
	}
	return fmt.Errorf("%s: %w", operation, err)
}

// Refresh fetches the projected source head when needed and computes its target-branch diff.
func (service *Service) Refresh(ctx context.Context, pullRequestURI string) (Result, error) {
	target, err := service.store.GetFetchTarget(ctx, pullRequestURI)
	if err != nil {
		return Result{}, fmt.Errorf("load projected pull request fetch target: %w", err)
	}
	destination := destinationKey(target.URI)
	headRef := "refs/adenosine/pull/" + destination + "/head"
	priorHead, err := service.git.ControlledHead(ctx, target.RepositoryID, destination)
	if err != nil {
		return Result{}, fmt.Errorf("inspect projected pull request head: %w", err)
	}
	if priorHead != target.HeadSHA {
		request := gitservice.RemoteFetch{
			SourceURL:    target.SourceURL,
			SourceBranch: target.SourceBranch,
			ExpectedHead: target.HeadSHA,
			Destination:  destination,
			PriorHead:    priorHead,
		}
		if err := service.git.FetchRemote(ctx, target.RepositoryID, request); err != nil {
			if !errors.Is(err, gitservice.ErrRefConflict) {
				return Result{}, fmt.Errorf("fetch projected pull request head: %w", err)
			}
			currentHead, inspectErr := service.git.ControlledHead(ctx, target.RepositoryID, destination)
			if inspectErr != nil {
				return Result{}, fmt.Errorf("inspect concurrently refreshed pull request head: %w", inspectErr)
			}
			if currentHead != target.HeadSHA {
				return Result{}, fmt.Errorf("fetch projected pull request head: %w", err)
			}
		}
	}
	targetRef := "refs/heads/" + target.TargetBranch
	mergeBase, err := service.git.MergeBase(ctx, target.RepositoryID, targetRef, target.HeadSHA)
	if err != nil {
		return Result{}, fmt.Errorf("compute projected pull request merge base: %w", err)
	}
	diff, err := service.git.Diff(ctx, target.RepositoryID, mergeBase, target.HeadSHA)
	if err != nil {
		return Result{}, fmt.Errorf("compute projected pull request diff: %w", err)
	}
	current, err := service.store.GetFetchTarget(ctx, pullRequestURI)
	if err != nil {
		return Result{}, fmt.Errorf("revalidate projected pull request fetch target: %w", err)
	}
	if current != target {
		return Result{}, ErrProjectionChanged
	}
	return Result{RepositoryID: target.RepositoryID, HeadRef: headRef, MergeBase: mergeBase, Diff: diff}, nil
}

func destinationKey(pullRequestURI string) string {
	digest := sha256.Sum256([]byte(pullRequestURI))
	return hex.EncodeToString(digest[:])
}
