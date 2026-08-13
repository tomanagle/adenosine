package federation

import (
	"context"
	"strings"
	"testing"

	"github.com/adenosine-dev/adenosine/internal/pullrequest"
)

const testGitSHA = "0123456789abcdef0123456789abcdef01234567"
const testObservedCID = "bafybeibwzif5g2dkjaxtv3p67te2g7a2n5xwbn2khoxalctlp7ch5vmo3a"

type memoryPullRequest struct {
	eventID             int64
	sourceRepositoryURI string
	targetRepositoryURI string
	sourceBranch        string
	targetBranch        string
	title               string
	state               pullrequest.State
	statusEventID       int64
	mergeCommitSHA      string
	reviewCount         int
	deleted             bool
}

type memoryPullRequestStatus struct {
	eventID             int64
	authorDID           string
	pullRequestURI      string
	targetRepositoryURI string
	state               pullrequest.State
	mergeCommitSHA      string
	deleted             bool
}

type memoryPullRequestReview struct {
	eventID        int64
	authorDID      string
	pullRequestURI string
	body           string
	deleted        bool
}

type memoryPullRequestStore struct {
	receipts     map[int64]struct{}
	rawEvents    map[string]int64
	repositories map[string]bool
	pullRequests map[string]memoryPullRequest
	statuses     map[string]memoryPullRequestStatus
	reviews      map[string]memoryPullRequestReview
	totalCount   map[string]int
	openCount    map[string]int
}

func newMemoryPullRequestStore() *memoryPullRequestStore {
	return &memoryPullRequestStore{
		receipts: make(map[int64]struct{}), rawEvents: make(map[string]int64), repositories: make(map[string]bool),
		pullRequests: make(map[string]memoryPullRequest), statuses: make(map[string]memoryPullRequestStatus),
		reviews: make(map[string]memoryPullRequestReview), totalCount: make(map[string]int), openCount: make(map[string]int),
	}
}

func (store *memoryPullRequestStore) Store(_ context.Context, event Event, _ string) (bool, error) {
	if _, exists := store.receipts[event.ID]; exists {
		return true, nil
	}
	store.receipts[event.ID] = struct{}{}
	record := event.Record
	if record == nil || event.ID <= store.rawEvents[record.URI] {
		return false, nil
	}
	store.rawEvents[record.URI] = event.ID
	switch record.Collection {
	case RepositoryCollection:
		store.repositories[record.URI] = record.Action != "delete"
	case PullRequestCollection:
		current, exists := store.pullRequests[record.URI]
		if record.Action == "delete" {
			if exists {
				current.eventID = event.ID
				current.deleted = true
				store.pullRequests[record.URI] = current
			}
			break
		}
		if exists && (current.sourceRepositoryURI != record.PullRequest.SourceRepository.URI || current.sourceBranch != record.PullRequest.SourceBranch || current.targetRepositoryURI != record.PullRequest.TargetRepository.URI || current.targetBranch != record.PullRequest.TargetBranch) {
			break
		}
		store.pullRequests[record.URI] = memoryPullRequest{
			eventID: event.ID, sourceRepositoryURI: record.PullRequest.SourceRepository.URI,
			sourceBranch: record.PullRequest.SourceBranch, targetRepositoryURI: record.PullRequest.TargetRepository.URI,
			targetBranch: record.PullRequest.TargetBranch, title: record.PullRequest.Title,
		}
	case PullRequestStatusCollection:
		current, exists := store.statuses[record.URI]
		if record.Action == "delete" {
			if exists {
				current.eventID = event.ID
				current.deleted = true
				store.statuses[record.URI] = current
			}
			break
		}
		if exists && (current.pullRequestURI != record.PullRequestStatus.Subject.URI || current.targetRepositoryURI != record.PullRequestStatus.TargetRepository.URI) {
			break
		}
		store.statuses[record.URI] = memoryPullRequestStatus{
			eventID: event.ID, authorDID: record.DID, pullRequestURI: record.PullRequestStatus.Subject.URI,
			targetRepositoryURI: record.PullRequestStatus.TargetRepository.URI, state: record.PullRequestStatus.State,
			mergeCommitSHA: record.PullRequestStatus.MergeCommitSHA,
		}
	case PullRequestReviewCollection:
		current, exists := store.reviews[record.URI]
		if record.Action == "delete" {
			if exists {
				current.eventID = event.ID
				current.deleted = true
				store.reviews[record.URI] = current
			}
			break
		}
		if exists && current.pullRequestURI != record.PullRequestReview.Subject.URI {
			break
		}
		store.reviews[record.URI] = memoryPullRequestReview{
			eventID: event.ID, authorDID: record.DID, pullRequestURI: record.PullRequestReview.Subject.URI,
			body: record.PullRequestReview.Body,
		}
	}
	store.recomputePullRequests()
	return false, nil
}

func (store *memoryPullRequestStore) recomputePullRequests() {
	clear(store.totalCount)
	clear(store.openCount)
	for uri, projected := range store.pullRequests {
		projected.state = pullrequest.StateOpen
		projected.statusEventID = 0
		projected.mergeCommitSHA = ""
		projected.reviewCount = 0
		ownerDID := strings.Split(projected.targetRepositoryURI, "/")[2]
		for _, status := range store.statuses {
			if status.deleted || status.pullRequestURI != uri || status.targetRepositoryURI != projected.targetRepositoryURI || status.authorDID != ownerDID {
				continue
			}
			if status.eventID > projected.statusEventID {
				projected.state = status.state
				projected.statusEventID = status.eventID
				projected.mergeCommitSHA = status.mergeCommitSHA
			}
		}
		for _, review := range store.reviews {
			if !review.deleted && review.pullRequestURI == uri {
				projected.reviewCount++
			}
		}
		store.pullRequests[uri] = projected
		if !projected.deleted && store.repositories[projected.targetRepositoryURI] {
			store.totalCount[projected.targetRepositoryURI]++
			if projected.state == pullrequest.StateOpen {
				store.openCount[projected.targetRepositoryURI]++
			}
		}
	}
}

func TestDecodePullRequestStatusAndReviewRecords(t *testing.T) {
	t.Parallel()
	sourceURI := "at://" + testBobDID + "/" + RepositoryCollection + "/fork"
	targetURI := "at://" + testDID + "/" + RepositoryCollection + "/project"
	pullRequestURI := "at://" + testBobDID + "/" + PullRequestCollection + "/pr1"
	statusRKey, err := pullrequest.StatusRecordKey(pullRequestURI)
	if err != nil {
		t.Fatal(err)
	}
	reviewRequestRKey, err := pullrequest.ReviewRequestRecordKey(pullRequestURI, testBobDID)
	if err != nil {
		t.Fatal(err)
	}
	testCases := []struct {
		name       string
		body       string
		wantKind   string
		wantAuthor string
		wantError  bool
	}{
		{name: "pull request", body: recordEnvelopeForDID(1, testBobDID, PullRequestCollection, "pr1", "create", pullRequestRecord(sourceURI, targetURI, "Title")), wantKind: "pull_request", wantAuthor: testBobDID},
		{name: "target owner merged status", body: recordEnvelopeForDID(2, testDID, PullRequestStatusCollection, statusRKey, "create", pullRequestStatusRecord(pullRequestURI, targetURI, "merged", testGitSHA)), wantKind: "status", wantAuthor: testDID},
		{name: "malicious contributor status remains typed", body: recordEnvelopeForDID(3, testBobDID, PullRequestStatusCollection, statusRKey, "create", pullRequestStatusRecord(pullRequestURI, targetURI, "closed", "")), wantKind: "status", wantAuthor: testBobDID},
		{name: "review authorship follows envelope", body: recordEnvelopeForDID(4, testDID, PullRequestReviewCollection, "review1", "create", pullRequestReviewRecord(pullRequestURI, "approve", "Looks good")), wantKind: "review", wantAuthor: testDID},
		{name: "target owner review request", body: recordEnvelopeForDID(10, testDID, PullRequestReviewRequestCollection, reviewRequestRKey, "create", pullRequestReviewRequestRecord(pullRequestURI, targetURI, testBobDID, testDID)), wantKind: "review_request", wantAuthor: testDID},
		{name: "review request rejects non-owner authority", body: recordEnvelopeForDID(11, testBobDID, PullRequestReviewRequestCollection, reviewRequestRKey, "create", pullRequestReviewRequestRecord(pullRequestURI, targetURI, testBobDID, testBobDID)), wantError: true},
		{name: "review request rejects wrong deterministic key", body: recordEnvelopeForDID(12, testDID, PullRequestReviewRequestCollection, strings.Repeat("a", 52), "create", pullRequestReviewRequestRecord(pullRequestURI, targetURI, testBobDID, testDID)), wantError: true},
		{name: "merged status requires commit", body: recordEnvelopeForDID(5, testDID, PullRequestStatusCollection, statusRKey, "create", pullRequestStatusRecord(pullRequestURI, targetURI, "merged", "")), wantError: true},
		{name: "invalid merged commit", body: recordEnvelopeForDID(6, testDID, PullRequestStatusCollection, statusRKey, "create", pullRequestStatusRecord(pullRequestURI, targetURI, "merged", "not-a-sha")), wantError: true},
		{name: "non-merged status rejects commit", body: recordEnvelopeForDID(7, testDID, PullRequestStatusCollection, statusRKey, "create", pullRequestStatusRecord(pullRequestURI, targetURI, "closed", testGitSHA)), wantError: true},
		{name: "wrong status deterministic key", body: recordEnvelopeForDID(8, testDID, PullRequestStatusCollection, strings.Repeat("a", 52), "create", pullRequestStatusRecord(pullRequestURI, targetURI, "closed", "")), wantError: true},
		{name: "review subject wrong collection", body: recordEnvelopeForDID(9, testDID, PullRequestReviewCollection, "review1", "create", pullRequestReviewRecord(targetURI, "approve", "No")), wantError: true},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			event, decodeErr := DecodeEvent([]byte(testCase.body))
			if (decodeErr != nil) != testCase.wantError {
				t.Fatalf("DecodeEvent() error = %v, wantError = %t", decodeErr, testCase.wantError)
			}
			if decodeErr != nil {
				return
			}
			kind := "review"
			if event.Record.PullRequest != nil {
				kind = "pull_request"
			} else if event.Record.PullRequestStatus != nil {
				kind = "status"
			} else if event.Record.PullRequestReviewRequest != nil {
				kind = "review_request"
			}
			if kind != testCase.wantKind || event.Record.DID != testCase.wantAuthor {
				t.Fatalf("kind/author = %s/%s", kind, event.Record.DID)
			}
		})
	}
}

func TestProcessorPullRequestAuthorityOrderingImmutabilityAndCounters(t *testing.T) {
	t.Parallel()
	sourceURI := "at://" + testBobDID + "/" + RepositoryCollection + "/fork"
	targetURI := "at://" + testDID + "/" + RepositoryCollection + "/project"
	otherTargetURI := "at://" + testDID + "/" + RepositoryCollection + "/other"
	pullRequestURI := "at://" + testBobDID + "/" + PullRequestCollection + "/pr1"
	statusRKey, err := pullrequest.StatusRecordKey(pullRequestURI)
	if err != nil {
		t.Fatal(err)
	}
	reviewURI := "at://" + testDID + "/" + PullRequestReviewCollection + "/review1"
	testCases := []struct {
		name              string
		events            []string
		wantState         pullrequest.State
		wantStatusEventID int64
		wantMergeSHA      string
		wantTitle         string
		wantReviews       int
		wantTotal         int
		wantOpen          int
		wantDeleted       bool
		wantDuplicate     bool
	}{
		{
			name: "status review and target repository arrive before pull request with different observed CIDs",
			events: []string{
				recordEnvelopeForDID(100, testDID, PullRequestStatusCollection, statusRKey, "create", strings.ReplaceAll(pullRequestStatusRecord(pullRequestURI, targetURI, "closed", ""), testCID, testObservedCID)),
				recordEnvelopeForDID(101, testDID, PullRequestReviewCollection, "review1", "create", strings.ReplaceAll(pullRequestReviewRecord(pullRequestURI, "approve", "First"), testCID, testObservedCID)),
				recordEnvelopeForDID(102, testDID, RepositoryCollection, "project", "create", repositoryRecord("Project")),
				recordEnvelopeForDID(103, testBobDID, PullRequestCollection, "pr1", "create", pullRequestRecord(sourceURI, targetURI, "Late PR")),
			},
			wantState: pullrequest.StateClosed, wantStatusEventID: 100, wantTitle: "Late PR", wantReviews: 1, wantTotal: 1,
		},
		{
			name: "malicious close ignored target owner closes then merges",
			events: []string{
				recordEnvelopeForDID(200, testDID, RepositoryCollection, "project", "create", repositoryRecord("Project")),
				recordEnvelopeForDID(201, testBobDID, PullRequestCollection, "pr1", "create", pullRequestRecord(sourceURI, targetURI, "PR")),
				recordEnvelopeForDID(202, testBobDID, PullRequestStatusCollection, statusRKey, "create", pullRequestStatusRecord(pullRequestURI, targetURI, "closed", "")),
				recordEnvelopeForDID(203, testDID, PullRequestStatusCollection, statusRKey, "create", pullRequestStatusRecord(pullRequestURI, targetURI, "closed", "")),
				recordEnvelopeForDID(204, testDID, PullRequestStatusCollection, statusRKey, "update", pullRequestStatusRecord(pullRequestURI, targetURI, "merged", testGitSHA)),
			},
			wantState: pullrequest.StateMerged, wantStatusEventID: 204, wantMergeSHA: testGitSHA, wantTitle: "PR", wantTotal: 1,
		},
		{
			name: "retarget and review subject attempts are ignored",
			events: []string{
				recordEnvelopeForDID(300, testDID, RepositoryCollection, "project", "create", repositoryRecord("Project")),
				recordEnvelopeForDID(301, testBobDID, PullRequestCollection, "pr1", "create", pullRequestRecord(sourceURI, targetURI, "Original")),
				recordEnvelopeForDID(302, testBobDID, PullRequestCollection, "pr1", "update", pullRequestRecord(sourceURI, otherTargetURI, "Retargeted")),
				recordEnvelopeForDID(305, testBobDID, PullRequestCollection, "pr1", "update", strings.Replace(pullRequestRecord(sourceURI, targetURI, "Branch retargeted"), `"targetBranch":"main"`, `"targetBranch":"release"`, 1)),
				recordEnvelopeForDID(303, testDID, PullRequestReviewCollection, "review1", "create", pullRequestReviewRecord(pullRequestURI, "comment", "Original review")),
				recordEnvelopeForDID(304, testDID, PullRequestReviewCollection, "review1", "update", pullRequestReviewRecord("at://"+testBobDID+"/"+PullRequestCollection+"/other", "approve", "Moved")),
			},
			wantState: pullrequest.StateOpen, wantTitle: "Original", wantReviews: 1, wantTotal: 1, wantOpen: 1,
		},
		{
			name: "stale replay review delete status delete pull request delete and unknown delete",
			events: []string{
				recordEnvelopeForDID(400, testDID, RepositoryCollection, "project", "create", repositoryRecord("Project")),
				recordEnvelopeForDID(402, testBobDID, PullRequestCollection, "pr1", "update", pullRequestRecord(sourceURI, targetURI, "Current")),
				recordEnvelopeForDID(401, testBobDID, PullRequestCollection, "pr1", "create", pullRequestRecord(sourceURI, targetURI, "Stale")),
				recordEnvelopeForDID(403, testDID, PullRequestReviewCollection, "review1", "create", pullRequestReviewRecord(pullRequestURI, "approve", "Review")),
				recordEnvelopeForDID(404, testDID, PullRequestReviewCollection, "review1", "delete", ""),
				recordEnvelopeForDID(405, testDID, PullRequestStatusCollection, statusRKey, "create", pullRequestStatusRecord(pullRequestURI, targetURI, "closed", "")),
				recordEnvelopeForDID(406, testDID, PullRequestStatusCollection, statusRKey, "delete", ""),
				recordEnvelopeForDID(407, testBobDID, PullRequestCollection, "pr1", "delete", ""),
				recordEnvelopeForDID(407, testBobDID, PullRequestCollection, "pr1", "delete", ""),
				recordEnvelopeForDID(408, testBobDID, PullRequestReviewCollection, "unknown", "delete", ""),
			},
			wantState: pullrequest.StateOpen, wantTitle: "Current", wantDeleted: true, wantDuplicate: true,
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			store := newMemoryPullRequestStore()
			processor := &Processor{store: store}
			duplicate := false
			for _, body := range testCase.events {
				result, processErr := processor.Process(context.Background(), []byte(body))
				if processErr != nil {
					t.Fatal(processErr)
				}
				duplicate = duplicate || result.Duplicate
			}
			projected := store.pullRequests[pullRequestURI]
			if projected.state != testCase.wantState || projected.statusEventID != testCase.wantStatusEventID || projected.mergeCommitSHA != testCase.wantMergeSHA || projected.title != testCase.wantTitle || projected.reviewCount != testCase.wantReviews || projected.deleted != testCase.wantDeleted {
				t.Fatalf("pull request = %#v", projected)
			}
			if store.totalCount[targetURI] != testCase.wantTotal || store.openCount[targetURI] != testCase.wantOpen {
				t.Fatalf("total/open counts = %d/%d", store.totalCount[targetURI], store.openCount[targetURI])
			}
			if review, exists := store.reviews[reviewURI]; exists && review.authorDID != testDID {
				t.Fatalf("review author = %q", review.authorDID)
			}
			if duplicate != testCase.wantDuplicate {
				t.Fatalf("duplicate = %t, want %t", duplicate, testCase.wantDuplicate)
			}
		})
	}
}

func pullRequestRecord(sourceURI, targetURI, title string) string {
	return `{"$type":"` + PullRequestCollection + `","sourceRepository":{"uri":"` + sourceURI + `","cid":"` + testCID + `"},"targetRepository":{"uri":"` + targetURI + `","cid":"` + testCID + `"},"sourceBranch":"feature","targetBranch":"main","headSHA":"` + testGitSHA + `","title":"` + title + `","body":"Body","createdAt":"2026-08-09T12:00:00Z","updatedAt":"2026-08-09T13:00:00Z"}`
}

func pullRequestStatusRecord(pullRequestURI, targetURI, state, mergeCommitSHA string) string {
	mergeCommit := ""
	if mergeCommitSHA != "" {
		mergeCommit = `,"mergeCommitSHA":"` + mergeCommitSHA + `"`
	}
	return `{"$type":"` + PullRequestStatusCollection + `","subject":{"uri":"` + pullRequestURI + `","cid":"` + testCID + `"},"targetRepository":{"uri":"` + targetURI + `","cid":"` + testCID + `"},"state":"` + state + `"` + mergeCommit + `,"createdAt":"2026-08-09T12:00:00Z","updatedAt":"2026-08-09T13:00:00Z"}`
}

func pullRequestReviewRecord(pullRequestURI, verdict, body string) string {
	return `{"$type":"` + PullRequestReviewCollection + `","subject":{"uri":"` + pullRequestURI + `","cid":"` + testCID + `"},"verdict":"` + verdict + `","body":"` + body + `","createdAt":"2026-08-09T12:00:00Z","updatedAt":"2026-08-09T13:00:00Z"}`
}

func pullRequestReviewRequestRecord(pullRequestURI, targetURI, reviewerDID, requestedByDID string) string {
	return `{"$type":"` + PullRequestReviewRequestCollection + `","subject":{"uri":"` + pullRequestURI + `","cid":"` + testCID + `"},"targetRepository":{"uri":"` + targetURI + `","cid":"` + testCID + `"},"reviewer":"` + reviewerDID + `","requestedBy":"` + requestedByDID + `","createdAt":"2026-08-09T12:00:00Z","updatedAt":"2026-08-09T13:00:00Z"}`
}
