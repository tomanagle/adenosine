package pullrequest

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	gitservice "github.com/adenosine-dev/adenosine/internal/git"
	"github.com/adenosine-dev/adenosine/internal/repository"
	"github.com/google/uuid"
)

type stubProjectionStore struct {
	targets []fetchTarget
	target  fetchTarget
	err     error
	calls   int
}

func (store *stubProjectionStore) List(context.Context, string, int) (Projection, error) {
	return Projection{}, store.err
}
func (store *stubProjectionStore) Get(context.Context, string) (ProjectedPullRequest, error) {
	return ProjectedPullRequest{}, store.err
}
func (store *stubProjectionStore) GetRepositoryTargets(context.Context, string, string) (repositoryTargets, error) {
	return repositoryTargets{}, store.err
}
func (store *stubProjectionStore) ListReviews(context.Context, string, int) ([]ProjectedReview, error) {
	return nil, store.err
}
func (store *stubProjectionStore) GetReviewTarget(context.Context, string) (StrongRef, error) {
	return StrongRef{}, store.err
}
func (store *stubProjectionStore) GetStatusTarget(context.Context, string) (statusTarget, error) {
	return statusTarget{}, store.err
}
func (store *stubProjectionStore) GetMergeTarget(context.Context, string) (mergeTarget, error) {
	return mergeTarget{}, store.err
}

func (store *stubProjectionStore) GetFetchTarget(context.Context, string) (fetchTarget, error) {
	if store.err != nil {
		return fetchTarget{}, store.err
	}
	if len(store.targets) == 0 {
		return store.target, nil
	}
	index := store.calls
	if index >= len(store.targets) {
		index = len(store.targets) - 1
	}
	store.calls++
	return store.targets[index], nil
}

type recordingGitService struct {
	controlledHead  string
	controlledHeads []string
	controlledErr   error
	fetchErr        error
	mergeBase       string
	mergeErr        error
	diff            gitservice.Diff
	diffErr         error
	controlled      []controlledCall
	fetches         []fetchCall
	mergeBases      []historyCall
	diffs           []historyCall
}

type controlledCall struct {
	repositoryID repository.ID
	destination  string
}

type fetchCall struct {
	repositoryID repository.ID
	request      gitservice.RemoteFetch
}

type historyCall struct {
	repositoryID repository.ID
	base         string
	head         string
}

func (git *recordingGitService) ControlledHead(_ context.Context, id repository.ID, destination string) (string, error) {
	git.controlled = append(git.controlled, controlledCall{id, destination})
	if len(git.controlledHeads) >= len(git.controlled) {
		return git.controlledHeads[len(git.controlled)-1], git.controlledErr
	}
	return git.controlledHead, git.controlledErr
}

func (git *recordingGitService) FetchRemote(_ context.Context, id repository.ID, request gitservice.RemoteFetch) error {
	git.fetches = append(git.fetches, fetchCall{id, request})
	return git.fetchErr
}

func (git *recordingGitService) MergeBase(_ context.Context, id repository.ID, base, head string) (string, error) {
	git.mergeBases = append(git.mergeBases, historyCall{id, base, head})
	return git.mergeBase, git.mergeErr
}

func (git *recordingGitService) Diff(_ context.Context, id repository.ID, base, head string) (gitservice.Diff, error) {
	git.diffs = append(git.diffs, historyCall{id, base, head})
	return git.diff, git.diffErr
}
func (git *recordingGitService) Commit(context.Context, repository.ID, string) (gitservice.Commit, error) {
	return gitservice.Commit{}, nil
}
func (git *recordingGitService) Merge(context.Context, repository.ID, gitservice.MergeRequest) (gitservice.MergeResult, error) {
	return gitservice.MergeResult{}, nil
}

func TestServiceRefreshOrchestratesProjectedHeadAndDiff(t *testing.T) {
	t.Parallel()
	repositoryID := repository.ID(uuid.MustParse("11111111-2222-3333-4444-555555555555"))
	target := fetchTarget{
		URI: testPullRequestURI, CID: testCID, RepositoryID: repositoryID,
		SourceURL: "https://git.example.com/source/project.git", SourceBranch: "feature/work",
		HeadSHA: testSHA1, TargetBranch: "main",
	}
	destination := "193757451ea39654aee6b5173fff66ec70e96fbbe885175cf65b63cea59f8e99"
	headRef := "refs/adenosine/pull/" + destination + "/head"
	mergeBase := strings.Repeat("b", 40)
	wantDiff := gitservice.Diff{BaseSHA: mergeBase, HeadSHA: testSHA1, Patch: "patch"}
	testCases := []struct {
		name        string
		currentHead string
		wantFetches []fetchCall
	}{
		{
			name: "fetches absent controlled head",
			wantFetches: []fetchCall{{repositoryID, gitservice.RemoteFetch{
				SourceURL: target.SourceURL, SourceBranch: target.SourceBranch, ExpectedHead: target.HeadSHA,
				Destination: destination,
			}}},
		},
		{
			name:        "refreshes stale controlled head with CAS",
			currentHead: strings.Repeat("a", 40),
			wantFetches: []fetchCall{{repositoryID, gitservice.RemoteFetch{
				SourceURL: target.SourceURL, SourceBranch: target.SourceBranch, ExpectedHead: target.HeadSHA,
				Destination: destination, PriorHead: strings.Repeat("a", 40),
			}}},
		},
		{name: "already current avoids fetch", currentHead: target.HeadSHA},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			git := &recordingGitService{controlledHead: testCase.currentHead, mergeBase: mergeBase, diff: wantDiff}
			result, err := NewService(&stubProjectionStore{target: target}, git).Refresh(context.Background(), testPullRequestURI)
			if err != nil {
				t.Fatalf("Refresh() error = %v", err)
			}
			wantResult := Result{RepositoryID: repositoryID, HeadRef: headRef, MergeBase: mergeBase, Diff: wantDiff}
			if !reflect.DeepEqual(result, wantResult) {
				t.Fatalf("Refresh() = %#v, want %#v", result, wantResult)
			}
			if !reflect.DeepEqual(git.controlled, []controlledCall{{repositoryID, destination}}) {
				t.Errorf("ControlledHead calls = %#v", git.controlled)
			}
			if !reflect.DeepEqual(git.fetches, testCase.wantFetches) {
				t.Errorf("FetchRemote calls = %#v, want %#v", git.fetches, testCase.wantFetches)
			}
			if !reflect.DeepEqual(git.mergeBases, []historyCall{{repositoryID, "refs/heads/main", target.HeadSHA}}) {
				t.Errorf("MergeBase calls = %#v", git.mergeBases)
			}
			if !reflect.DeepEqual(git.diffs, []historyCall{{repositoryID, mergeBase, target.HeadSHA}}) {
				t.Errorf("Diff calls = %#v", git.diffs)
			}
		})
	}
}

func TestServiceRefreshWrapsOperationalErrors(t *testing.T) {
	t.Parallel()
	cause := errors.New("failed")
	target := fetchTarget{URI: testPullRequestURI, RepositoryID: repository.ID(uuid.New()), HeadSHA: testSHA1, TargetBranch: "main"}
	testCases := []struct {
		name        string
		store       *stubProjectionStore
		configure   func(*recordingGitService)
		wantContext string
	}{
		{name: "load", store: &stubProjectionStore{err: cause}, wantContext: "load projected pull request fetch target"},
		{name: "inspect", store: &stubProjectionStore{target: target}, configure: func(git *recordingGitService) { git.controlledErr = cause }, wantContext: "inspect projected pull request head"},
		{name: "fetch", store: &stubProjectionStore{target: target}, configure: func(git *recordingGitService) { git.fetchErr = cause }, wantContext: "fetch projected pull request head"},
		{name: "merge base", store: &stubProjectionStore{target: target}, configure: func(git *recordingGitService) { git.mergeErr = cause }, wantContext: "compute projected pull request merge base"},
		{name: "diff", store: &stubProjectionStore{target: target}, configure: func(git *recordingGitService) { git.mergeBase = testSHA1; git.diffErr = cause }, wantContext: "compute projected pull request diff"},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			git := &recordingGitService{mergeBase: testSHA1}
			if testCase.configure != nil {
				testCase.configure(git)
			}
			_, err := NewService(testCase.store, git).Refresh(context.Background(), testPullRequestURI)
			if !errors.Is(err, cause) || !strings.Contains(err.Error(), testCase.wantContext) {
				t.Fatalf("Refresh() error = %v, want wrapped %q", err, testCase.wantContext)
			}
		})
	}
}

func TestServiceRefreshHandlesConcurrentChanges(t *testing.T) {
	t.Parallel()
	repositoryID := repository.ID(uuid.MustParse("11111111-2222-3333-4444-555555555555"))
	target := fetchTarget{URI: testPullRequestURI, CID: testCID, RepositoryID: repositoryID, SourceURL: "https://git.example.com/source.git", SourceBranch: "feature", HeadSHA: testSHA1, TargetBranch: "main"}
	changed := target
	changed.CID = "bafyreiaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	testCases := []struct {
		name      string
		store     *stubProjectionStore
		git       *recordingGitService
		wantError error
	}{
		{
			name:  "accepts concurrent fetch of same head",
			store: &stubProjectionStore{target: target},
			git:   &recordingGitService{controlledHeads: []string{strings.Repeat("a", 40), testSHA1}, fetchErr: gitservice.ErrRefConflict, mergeBase: testSHA1},
		},
		{
			name:      "rejects projection changed during refresh",
			store:     &stubProjectionStore{targets: []fetchTarget{target, changed}},
			git:       &recordingGitService{controlledHead: testSHA1, mergeBase: testSHA1},
			wantError: ErrProjectionChanged,
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			_, err := NewService(testCase.store, testCase.git).Refresh(context.Background(), testPullRequestURI)
			if !errors.Is(err, testCase.wantError) {
				t.Fatalf("Refresh() error = %v, want %v", err, testCase.wantError)
			}
		})
	}
}

func TestDestinationKeyIsDeterministicAndRefSafe(t *testing.T) {
	t.Parallel()
	testCases := []struct {
		name string
		uri  string
		want string
	}{
		{name: "projected URI", uri: testPullRequestURI, want: "193757451ea39654aee6b5173fff66ec70e96fbbe885175cf65b63cea59f8e99"},
		{name: "raw ref syntax is not exposed", uri: "refs/heads/main:../../owned", want: "51ccbcf3ed11278f92b3a6990fe31a7fbe0513675e30db3da6241704d5e2f432"},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			got := destinationKey(testCase.uri)
			if got != testCase.want || len(got) != 64 || strings.Trim(got, "0123456789abcdef") != "" {
				t.Fatalf("destinationKey(%q) = %q, want %q", testCase.uri, got, testCase.want)
			}
			if got != destinationKey(testCase.uri) || strings.Contains(got, testCase.uri) {
				t.Fatalf("destination key is unstable or exposes URI: %q", got)
			}
		})
	}
}
