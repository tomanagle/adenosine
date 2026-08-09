package federation

import (
	"context"
	"strings"
	"testing"

	"github.com/adenosine-dev/adenosine/internal/issue"
)

type memoryComment struct {
	eventID  int64
	issueURI string
	parent   string
	body     string
	deleted  bool
}

type memoryCommentStore struct {
	receipts          map[int64]struct{}
	rawEvents         map[string]int64
	repositories      map[string]bool
	issueRepositories map[string]string
	comments          map[string]memoryComment
	issueCount        map[string]int
	repositoryCount   map[string]int
}

func newMemoryCommentStore() *memoryCommentStore {
	return &memoryCommentStore{
		receipts: make(map[int64]struct{}), rawEvents: make(map[string]int64), repositories: make(map[string]bool),
		issueRepositories: make(map[string]string), comments: make(map[string]memoryComment),
		issueCount: make(map[string]int), repositoryCount: make(map[string]int),
	}
}

func (store *memoryCommentStore) Store(_ context.Context, event Event, _ string) (bool, error) {
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
	case IssueCollection:
		if record.Action == "delete" {
			delete(store.issueRepositories, record.URI)
		} else {
			store.issueRepositories[record.URI] = record.Issue.Repository.URI
		}
	case issue.CommentCollection:
		current, exists := store.comments[record.URI]
		if record.Action == "delete" {
			if exists {
				current.eventID = event.ID
				current.deleted = true
				store.comments[record.URI] = current
			}
		} else {
			parent := ""
			if record.IssueComment.Parent != nil {
				parent = record.IssueComment.Parent.URI
			}
			store.comments[record.URI] = memoryComment{
				eventID: event.ID, issueURI: record.IssueComment.Subject.URI, parent: parent, body: record.IssueComment.Body,
			}
		}
	}
	store.recomputeCounts()
	return false, nil
}

func (store *memoryCommentStore) recomputeCounts() {
	clear(store.issueCount)
	clear(store.repositoryCount)
	for _, comment := range store.comments {
		if comment.deleted {
			continue
		}
		repositoryURI, issueExists := store.issueRepositories[comment.issueURI]
		if !issueExists {
			continue
		}
		store.issueCount[comment.issueURI]++
		if store.repositories[repositoryURI] {
			store.repositoryCount[repositoryURI]++
		}
	}
}

func TestDecodeIssueCommentRecords(t *testing.T) {
	t.Parallel()
	issueURI := "at://" + testBobDID + "/" + IssueCollection + "/issue1"
	parentURI := "at://" + testDID + "/" + issue.CommentCollection + "/parent"
	selfURI := "at://" + testDID + "/" + issue.CommentCollection + "/comment1"
	testCases := []struct {
		name       string
		record     string
		wantParent bool
		wantError  bool
	}{
		{name: "top-level comment", record: issueCommentRecord(issueURI, "", "Body")},
		{name: "reply with parent", record: issueCommentRecord(issueURI, parentURI, "Reply"), wantParent: true},
		{name: "subject uses comment collection", record: issueCommentRecord(parentURI, "", "Body"), wantError: true},
		{name: "subject CID is malformed", record: strings.Replace(issueCommentRecord(issueURI, "", "Body"), testCID, "not-a-cid", 1), wantError: true},
		{name: "parent uses issue collection", record: issueCommentRecord(issueURI, issueURI, "Reply"), wantError: true},
		{name: "comment names itself as parent", record: issueCommentRecord(issueURI, selfURI, "Reply"), wantError: true},
		{name: "parent CID is absent", record: strings.Replace(issueCommentRecord(issueURI, parentURI, "Reply"), `"cid":"`+testCID+`"},"body"`, `"cid":""},"body"`, 1), wantError: true},
		{name: "timestamps are reversed", record: strings.Replace(issueCommentRecord(issueURI, "", "Body"), "2026-08-09T13:00:00Z", "2026-08-09T11:00:00Z", 1), wantError: true},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			body := recordEnvelopeForDID(1, testDID, issue.CommentCollection, "comment1", "create", testCase.record)
			event, err := DecodeEvent([]byte(body))
			if (err != nil) != testCase.wantError {
				t.Fatalf("DecodeEvent() error = %v, wantError = %t", err, testCase.wantError)
			}
			if err == nil && (event.Record.IssueComment == nil || (event.Record.IssueComment.Parent != nil) != testCase.wantParent) {
				t.Fatalf("decoded comment = %#v", event.Record.IssueComment)
			}
		})
	}
}

func TestProcessorIssueCommentLifecycleOrderingAndCounters(t *testing.T) {
	t.Parallel()
	repositoryURI := "at://" + testDID + "/" + RepositoryCollection + "/project"
	issueURI := "at://" + testBobDID + "/" + IssueCollection + "/issue1"
	parentURI := "at://" + testDID + "/" + issue.CommentCollection + "/parent"
	replyURI := "at://" + testBobDID + "/" + issue.CommentCollection + "/reply"
	unknownURI := "at://" + testBobDID + "/" + issue.CommentCollection + "/unknown"
	testCases := []struct {
		name                string
		events              []string
		commentURI          string
		wantBody            string
		wantParent          string
		wantDeleted         bool
		wantIssueComments   int
		wantRepoComments    int
		wantRawTombstoneURI string
		wantRawEvent        int64
		wantDuplicate       bool
	}{
		{
			name: "reply and parent arrive before issue and repository",
			events: []string{
				recordEnvelopeForDID(100, testBobDID, issue.CommentCollection, "reply", "create", issueCommentRecord(issueURI, parentURI, "Reply first")),
				recordEnvelopeForDID(101, testDID, issue.CommentCollection, "parent", "create", issueCommentRecord(issueURI, "", "Parent second")),
				recordEnvelopeForDID(102, testBobDID, IssueCollection, "issue1", "create", issueRecord(repositoryURI, "Issue third")),
				recordEnvelopeForDID(103, testDID, RepositoryCollection, "project", "create", repositoryRecord("Repository fourth")),
			},
			commentURI: replyURI, wantBody: "Reply first", wantParent: parentURI, wantIssueComments: 2, wantRepoComments: 2,
		},
		{
			name: "update delete stale replay and unknown delete",
			events: []string{
				recordEnvelopeForDID(200, testDID, RepositoryCollection, "project", "create", repositoryRecord("Project")),
				recordEnvelopeForDID(201, testBobDID, IssueCollection, "issue1", "create", issueRecord(repositoryURI, "Issue")),
				recordEnvelopeForDID(202, testBobDID, issue.CommentCollection, "reply", "create", issueCommentRecord(issueURI, "", "Original")),
				recordEnvelopeForDID(204, testBobDID, issue.CommentCollection, "reply", "update", issueCommentRecord(issueURI, "", "Current")),
				recordEnvelopeForDID(203, testBobDID, issue.CommentCollection, "reply", "update", issueCommentRecord(issueURI, "", "Stale")),
				recordEnvelopeForDID(205, testBobDID, issue.CommentCollection, "reply", "delete", ""),
				recordEnvelopeForDID(205, testBobDID, issue.CommentCollection, "reply", "delete", ""),
				recordEnvelopeForDID(206, testBobDID, issue.CommentCollection, "unknown", "delete", ""),
			},
			commentURI: replyURI, wantBody: "Current", wantDeleted: true, wantRawTombstoneURI: unknownURI, wantRawEvent: 206, wantDuplicate: true,
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			store := newMemoryCommentStore()
			processor := &Processor{store: store}
			duplicate := false
			for _, body := range testCase.events {
				result, err := processor.Process(context.Background(), []byte(body))
				if err != nil {
					t.Fatal(err)
				}
				duplicate = duplicate || result.Duplicate
			}
			comment := store.comments[testCase.commentURI]
			if comment.body != testCase.wantBody || comment.parent != testCase.wantParent || comment.deleted != testCase.wantDeleted {
				t.Fatalf("comment = %#v", comment)
			}
			if store.issueCount[issueURI] != testCase.wantIssueComments || store.repositoryCount[repositoryURI] != testCase.wantRepoComments {
				t.Fatalf("issue/repository comment counts = %d/%d", store.issueCount[issueURI], store.repositoryCount[repositoryURI])
			}
			if testCase.wantRawTombstoneURI != "" && store.rawEvents[testCase.wantRawTombstoneURI] != testCase.wantRawEvent {
				t.Fatalf("raw tombstone event = %d, want %d", store.rawEvents[testCase.wantRawTombstoneURI], testCase.wantRawEvent)
			}
			if duplicate != testCase.wantDuplicate {
				t.Fatalf("duplicate = %t, want %t", duplicate, testCase.wantDuplicate)
			}
		})
	}
}

func issueCommentRecord(issueURI, parentURI, body string) string {
	parent := ""
	if parentURI != "" {
		parent = `,"parent":{"uri":"` + parentURI + `","cid":"` + testCID + `"}`
	}
	return `{"$type":"` + issue.CommentCollection + `","subject":{"uri":"` + issueURI + `","cid":"` + testCID + `"}` + parent + `,"body":"` + body + `","createdAt":"2026-08-09T12:00:00Z","updatedAt":"2026-08-09T13:00:00Z"}`
}
