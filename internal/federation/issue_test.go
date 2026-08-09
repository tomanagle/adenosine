package federation

import (
	"context"
	"strings"
	"testing"

	"github.com/adenosine-dev/adenosine/internal/issue"
)

const testBobDID = "did:plc:ar7c4by46qjdydhdevvrndac"

type memoryIssue struct {
	eventID       int64
	repositoryURI string
	title         string
	state         issue.State
	statusEventID int64
	deleted       bool
}

type memoryIssueStatus struct {
	eventID       int64
	authorDID     string
	issueURI      string
	repositoryURI string
	state         issue.State
	deleted       bool
}

type memoryIssueStore struct {
	receipts     map[int64]struct{}
	rawEvents    map[string]int64
	repositories map[string]memoryRecord
	issues       map[string]memoryIssue
	statuses     map[string]memoryIssueStatus
	issueCount   map[string]int
	openCount    map[string]int
}

func newMemoryIssueStore() *memoryIssueStore {
	return &memoryIssueStore{
		receipts: make(map[int64]struct{}), rawEvents: make(map[string]int64),
		repositories: make(map[string]memoryRecord), issues: make(map[string]memoryIssue),
		statuses: make(map[string]memoryIssueStatus), issueCount: make(map[string]int), openCount: make(map[string]int),
	}
}

func (store *memoryIssueStore) Store(_ context.Context, event Event, _ string) (bool, error) {
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
		store.repositories[record.URI] = memoryRecord{eventID: event.ID, value: record.Repository, deleted: record.Action == "delete"}
		store.recomputeIssueCounts(record.URI)
	case IssueCollection:
		current, exists := store.issues[record.URI]
		if record.Action == "delete" {
			if exists {
				current.eventID = event.ID
				current.deleted = true
				store.issues[record.URI] = current
				store.recomputeIssueCounts(current.repositoryURI)
			}
			break
		}
		store.issues[record.URI] = memoryIssue{
			eventID: event.ID, repositoryURI: record.Issue.Repository.URI,
			title: record.Issue.Title, state: issue.StateOpen,
		}
		store.recomputeIssueState(record.URI)
		store.recomputeIssueCounts(record.Issue.Repository.URI)
	case IssueStatusCollection:
		current, exists := store.statuses[record.URI]
		if record.Action == "delete" {
			if exists {
				current.eventID = event.ID
				current.deleted = true
				store.statuses[record.URI] = current
				store.recomputeIssueState(current.issueURI)
			}
			break
		}
		store.statuses[record.URI] = memoryIssueStatus{
			eventID: event.ID, authorDID: record.DID, issueURI: record.IssueStatus.Subject.URI,
			repositoryURI: record.IssueStatus.Repository.URI, state: record.IssueStatus.State,
		}
		store.recomputeIssueState(record.IssueStatus.Subject.URI)
	}
	return false, nil
}

func (store *memoryIssueStore) recomputeIssueState(issueURI string) {
	projected, exists := store.issues[issueURI]
	if !exists {
		return
	}
	projected.state = issue.StateOpen
	projected.statusEventID = 0
	ownerDID := strings.Split(projected.repositoryURI, "/")[2]
	for _, status := range store.statuses {
		if status.deleted || status.issueURI != issueURI || status.repositoryURI != projected.repositoryURI || status.authorDID != ownerDID {
			continue
		}
		if status.eventID > projected.statusEventID {
			projected.state = status.state
			projected.statusEventID = status.eventID
		}
	}
	store.issues[issueURI] = projected
	store.recomputeIssueCounts(projected.repositoryURI)
}

func (store *memoryIssueStore) recomputeIssueCounts(repositoryURI string) {
	repository, exists := store.repositories[repositoryURI]
	if !exists || repository.deleted {
		return
	}
	total, open := 0, 0
	for _, projected := range store.issues {
		if projected.repositoryURI != repositoryURI || projected.deleted {
			continue
		}
		total++
		if projected.state == issue.StateOpen {
			open++
		}
	}
	store.issueCount[repositoryURI] = total
	store.openCount[repositoryURI] = open
}

func TestDecodeIssueAndStatusRecords(t *testing.T) {
	t.Parallel()
	repositoryURI := "at://" + testDID + "/" + RepositoryCollection + "/project"
	issueURI := "at://" + testBobDID + "/" + IssueCollection + "/issue1"
	statusRKey, err := issue.StatusRecordKey(issueURI)
	if err != nil {
		t.Fatal(err)
	}
	testCases := []struct {
		name       string
		body       string
		wantIssue  bool
		wantStatus bool
		wantError  bool
	}{
		{name: "Bob issue against Alice repository", body: recordEnvelopeForDID(1, testBobDID, IssueCollection, "issue1", "create", issueRecord(repositoryURI, "Title")), wantIssue: true},
		{name: "Alice authoritative status", body: recordEnvelopeForDID(2, testDID, IssueStatusCollection, statusRKey, "create", issueStatusRecord(issueURI, repositoryURI, "closed")), wantStatus: true},
		{name: "status wrong deterministic key", body: recordEnvelopeForDID(3, testDID, IssueStatusCollection, strings.Repeat("a", 52), "create", issueStatusRecord(issueURI, repositoryURI, "closed")), wantError: true},
		{name: "issue wrong repository collection", body: recordEnvelopeForDID(4, testBobDID, IssueCollection, "issue1", "create", issueRecord(issueURI, "Title")), wantError: true},
		{name: "status unknown state", body: recordEnvelopeForDID(5, testDID, IssueStatusCollection, statusRKey, "create", issueStatusRecord(issueURI, repositoryURI, "triaged")), wantError: true},
		{name: "issue reversed timestamps", body: recordEnvelopeForDID(6, testBobDID, IssueCollection, "issue1", "create", strings.Replace(issueRecord(repositoryURI, "Title"), "2026-08-09T13:00:00Z", "2026-08-09T11:00:00Z", 1)), wantError: true},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			event, err := DecodeEvent([]byte(testCase.body))
			if (err != nil) != testCase.wantError {
				t.Fatalf("DecodeEvent() error = %v, wantError = %t", err, testCase.wantError)
			}
			if err == nil && ((event.Record.Issue != nil) != testCase.wantIssue || (event.Record.IssueStatus != nil) != testCase.wantStatus) {
				t.Fatalf("decoded record = %#v", event.Record)
			}
		})
	}
}

func TestProcessorIssueAuthorityLifecycleOrderingAndCounters(t *testing.T) {
	t.Parallel()
	repositoryURI := "at://" + testDID + "/" + RepositoryCollection + "/project"
	issueURI := "at://" + testBobDID + "/" + IssueCollection + "/issue1"
	statusRKey, err := issue.StatusRecordKey(issueURI)
	if err != nil {
		t.Fatal(err)
	}
	testCases := []struct {
		name              string
		events            []string
		wantState         issue.State
		wantStatusEventID int64
		wantTitle         string
		wantIssues        int
		wantOpen          int
		wantStatuses      int
		wantDuplicate     bool
	}{
		{
			name: "Bob issue malicious Bob status Alice close reopen delete replay and unknown delete",
			events: []string{
				recordEnvelopeForDID(100, testDID, RepositoryCollection, "project", "create", repositoryRecord("Project")),
				recordEnvelopeForDID(101, testBobDID, IssueCollection, "issue1", "create", issueRecord(repositoryURI, "Original")),
				recordEnvelopeForDID(102, testBobDID, IssueStatusCollection, statusRKey, "create", issueStatusRecord(issueURI, repositoryURI, "closed")),
				recordEnvelopeForDID(103, testDID, IssueStatusCollection, statusRKey, "create", issueStatusRecord(issueURI, repositoryURI, "closed")),
				recordEnvelopeForDID(104, testDID, IssueStatusCollection, statusRKey, "update", issueStatusRecord(issueURI, repositoryURI, "open")),
				recordEnvelopeForDID(99, testDID, IssueStatusCollection, statusRKey, "update", issueStatusRecord(issueURI, repositoryURI, "closed")),
				recordEnvelopeForDID(105, testDID, IssueStatusCollection, statusRKey, "delete", ""),
				recordEnvelopeForDID(105, testDID, IssueStatusCollection, statusRKey, "delete", ""),
				recordEnvelopeForDID(106, testDID, IssueStatusCollection, strings.Repeat("b", 52), "delete", ""),
			},
			wantState: issue.StateOpen, wantTitle: "Original", wantIssues: 1, wantOpen: 1, wantStatuses: 2, wantDuplicate: true,
		},
		{
			name: "Alice status arrives before Bob issue and Alice repository",
			events: []string{
				recordEnvelopeForDID(200, testDID, IssueStatusCollection, statusRKey, "create", issueStatusRecord(issueURI, repositoryURI, "closed")),
				recordEnvelopeForDID(201, testBobDID, IssueCollection, "issue1", "create", issueRecord(repositoryURI, "Before repository")),
				recordEnvelopeForDID(202, testDID, RepositoryCollection, "project", "create", repositoryRecord("Project")),
			},
			wantState: issue.StateClosed, wantStatusEventID: 200, wantTitle: "Before repository", wantIssues: 1, wantStatuses: 1,
		},
		{
			name: "stale issue and status updates cannot replace current typed state",
			events: []string{
				recordEnvelopeForDID(300, testDID, RepositoryCollection, "project", "create", repositoryRecord("Project")),
				recordEnvelopeForDID(302, testBobDID, IssueCollection, "issue1", "update", issueRecord(repositoryURI, "Current")),
				recordEnvelopeForDID(301, testBobDID, IssueCollection, "issue1", "create", issueRecord(repositoryURI, "Stale")),
				recordEnvelopeForDID(304, testDID, IssueStatusCollection, statusRKey, "update", issueStatusRecord(issueURI, repositoryURI, "closed")),
				recordEnvelopeForDID(303, testDID, IssueStatusCollection, statusRKey, "update", issueStatusRecord(issueURI, repositoryURI, "open")),
			},
			wantState: issue.StateClosed, wantStatusEventID: 304, wantTitle: "Current", wantIssues: 1, wantStatuses: 1,
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			store := newMemoryIssueStore()
			processor := &Processor{store: store}
			duplicate := false
			for _, body := range testCase.events {
				result, processErr := processor.Process(context.Background(), []byte(body))
				if processErr != nil {
					t.Fatal(processErr)
				}
				duplicate = duplicate || result.Duplicate
			}
			projected := store.issues[issueURI]
			if projected.state != testCase.wantState || projected.statusEventID != testCase.wantStatusEventID || projected.title != testCase.wantTitle {
				t.Fatalf("issue = %#v", projected)
			}
			if store.issueCount[repositoryURI] != testCase.wantIssues || store.openCount[repositoryURI] != testCase.wantOpen || len(store.statuses) != testCase.wantStatuses {
				t.Fatalf("issue/open/status counts = %d/%d/%d", store.issueCount[repositoryURI], store.openCount[repositoryURI], len(store.statuses))
			}
			if duplicate != testCase.wantDuplicate {
				t.Fatalf("duplicate = %t, want %t", duplicate, testCase.wantDuplicate)
			}
			if testCase.wantStatusEventID == 0 && projected.state == issue.StateClosed {
				t.Fatal("deleted authoritative status remained selected")
			}
		})
	}
}

func recordEnvelopeForDID(id int64, did, collection, rkey, action, record string) string {
	cidAndRecord := `,"cid":"` + testCID + `","record":` + record
	if action == "delete" {
		cidAndRecord = ""
	}
	return `{"id":` + integerString(id) + `,"type":"record","record":{"live":true,"rev":"3kzfcijpj2z2a","did":"` + did + `","collection":"` + collection + `","rkey":"` + rkey + `","action":"` + action + `"` + cidAndRecord + `}}`
}

func issueRecord(repositoryURI, title string) string {
	return `{"$type":"` + IssueCollection + `","repository":{"uri":"` + repositoryURI + `","cid":"` + testCID + `"},"title":"` + title + `","body":"Body","createdAt":"2026-08-09T12:00:00Z","updatedAt":"2026-08-09T13:00:00Z"}`
}

func issueStatusRecord(issueURI, repositoryURI, state string) string {
	return `{"$type":"` + IssueStatusCollection + `","subject":{"uri":"` + issueURI + `","cid":"` + testCID + `"},"repository":{"uri":"` + repositoryURI + `","cid":"` + testCID + `"},"state":"` + state + `","createdAt":"2026-08-09T12:00:00Z","updatedAt":"2026-08-09T13:00:00Z"}`
}
