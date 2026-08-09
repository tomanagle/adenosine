package comment

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/adenosine-dev/adenosine/internal/issue"
)

const (
	testIssueURI      = "at://did:plc:alice/dev.adenosine.issue/0198a8512a897ae2a370dc68883e3af1"
	testOtherIssueURI = "at://did:plc:alice/dev.adenosine.issue/0198a8512a897ae2a370dc68883e3af2"
	testParentURI     = "at://did:plc:bob/dev.adenosine.issueComment/0198a8512a897ae2a370dc68883e3af3"
	testCommentURI    = "at://did:plc:bob/dev.adenosine.issueComment/0198a8512a897ae2a370dc68883e3af4"
	testCID           = "bafybeigdyrzt5sfp7udm7hu76uh7y26nf3efuylqabf3oclgtqy55fbzdi"
)

type serviceStore struct {
	projection      Projection
	issueTarget     issue.StrongRef
	parent          parentTarget
	err             error
	projectionCalls int
	issueCalls      int
	parentCalls     int
	viewerDID       string
	limit           int
}

func (store *serviceStore) GetProjection(_ context.Context, _ string, viewerDID string, limit int) (Projection, error) {
	store.projectionCalls++
	store.viewerDID, store.limit = viewerDID, limit
	return store.projection, store.err
}

func (store *serviceStore) GetIssueTarget(context.Context, string) (issue.StrongRef, error) {
	store.issueCalls++
	return store.issueTarget, store.err
}

func (store *serviceStore) GetParentTarget(context.Context, string) (parentTarget, error) {
	store.parentCalls++
	return store.parent, store.err
}

type servicePublisher struct {
	comment     issue.Comment
	err         error
	authorDID   string
	rkey        string
	record      issue.CommentRecord
	commentURI  string
	createCalls int
	deleteCalls int
}

func (publisher *servicePublisher) CreateIssueComment(_ context.Context, did, rkey string, record issue.CommentRecord) (issue.Comment, error) {
	publisher.createCalls++
	publisher.authorDID, publisher.rkey, publisher.record = did, rkey, record
	return publisher.comment, publisher.err
}

func (publisher *servicePublisher) DeleteIssueComment(_ context.Context, did, uri string) error {
	publisher.deleteCalls++
	publisher.authorDID, publisher.commentURI = did, uri
	return publisher.err
}

type serviceClock struct{ now time.Time }

func (clock serviceClock) Now() time.Time { return clock.now }

func TestServiceReadsAndPublishesWithoutProjectionWrites(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, time.August, 9, 12, 0, 0, 123, time.FixedZone("offset", 3600))
	subject := issue.StrongRef{URI: testIssueURI, CID: testCID}
	parentRef := issue.StrongRef{URI: testParentURI, CID: testCID}
	testCases := []struct {
		name           string
		operation      string
		issueURI       string
		viewerDID      string
		parentURI      string
		store          *serviceStore
		publisher      *servicePublisher
		wantErr        error
		wantProjection int
		wantIssue      int
		wantParent     int
		wantCreate     int
		wantDelete     int
	}{
		{name: "anonymous projection is bounded", operation: "get", issueURI: testIssueURI, store: &serviceStore{projection: Projection{CommentCount: 2}}, publisher: &servicePublisher{}, wantProjection: 1},
		{name: "authenticated projection passes viewer", operation: "get", issueURI: testIssueURI, viewerDID: "did:plc:alice", store: &serviceStore{projection: Projection{Comments: []ProjectedComment{}}}, publisher: &servicePublisher{}, wantProjection: 1},
		{name: "missing projection remains typed", operation: "get", issueURI: testIssueURI, store: &serviceStore{err: issue.ErrNotFound}, publisher: &servicePublisher{}, wantErr: issue.ErrNotFound, wantProjection: 1},
		{name: "top level comment uses current issue CID", operation: "create", issueURI: testIssueURI, store: &serviceStore{issueTarget: subject}, publisher: &servicePublisher{}, wantIssue: 1, wantCreate: 1},
		{name: "reply uses current parent CID", operation: "create", issueURI: testIssueURI, parentURI: testParentURI, store: &serviceStore{issueTarget: subject, parent: parentTarget{Ref: parentRef, IssueURI: testIssueURI}}, publisher: &servicePublisher{}, wantIssue: 1, wantParent: 1, wantCreate: 1},
		{name: "cross issue reply is rejected", operation: "create", issueURI: testIssueURI, parentURI: testParentURI, store: &serviceStore{issueTarget: subject, parent: parentTarget{Ref: parentRef, IssueURI: testOtherIssueURI}}, publisher: &servicePublisher{}, wantErr: issue.ErrValidation, wantIssue: 1, wantParent: 1},
		{name: "missing parent remains typed", operation: "create", issueURI: testIssueURI, parentURI: testParentURI, store: &serviceStore{issueTarget: subject, err: issue.ErrNotFound}, publisher: &servicePublisher{}, wantErr: issue.ErrNotFound, wantIssue: 1},
		{name: "malformed parent fails before lookups", operation: "create", issueURI: testIssueURI, parentURI: "invalid", store: &serviceStore{}, publisher: &servicePublisher{}, wantErr: issue.ErrValidation},
		{name: "delete delegates authority to publisher", operation: "delete", issueURI: testIssueURI, store: &serviceStore{}, publisher: &servicePublisher{}, wantDelete: 1},
		{name: "publisher error remains typed", operation: "delete", issueURI: testIssueURI, store: &serviceStore{}, publisher: &servicePublisher{err: &issue.AuthorizationError{}}, wantErr: issue.ErrAuthorization, wantDelete: 1},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			service := NewService(testCase.store, testCase.publisher, serviceClock{now: now})
			var err error
			switch testCase.operation {
			case "get":
				var projection Projection
				projection, err = service.Get(context.Background(), testCase.issueURI, testCase.viewerDID)
				if err == nil && projection.Comments == nil {
					t.Fatal("Comments is nil")
				}
			case "create":
				_, err = service.Create(context.Background(), "did:plc:bob", CreateInput{IssueURI: testCase.issueURI, ParentURI: testCase.parentURI, Body: "body"})
			case "delete":
				err = service.Delete(context.Background(), "did:plc:bob", testCommentURI)
			}
			if !errors.Is(err, testCase.wantErr) {
				t.Fatalf("error = %v, want %v", err, testCase.wantErr)
			}
			if testCase.store.projectionCalls != testCase.wantProjection || testCase.store.issueCalls != testCase.wantIssue || testCase.store.parentCalls != testCase.wantParent || testCase.publisher.createCalls != testCase.wantCreate || testCase.publisher.deleteCalls != testCase.wantDelete {
				t.Fatalf("calls = projection %d issue %d parent %d create %d delete %d", testCase.store.projectionCalls, testCase.store.issueCalls, testCase.store.parentCalls, testCase.publisher.createCalls, testCase.publisher.deleteCalls)
			}
			if testCase.wantProjection > 0 && (testCase.store.viewerDID != testCase.viewerDID || testCase.store.limit != 100) {
				t.Fatalf("projection viewer/limit = %q/%d", testCase.store.viewerDID, testCase.store.limit)
			}
			if testCase.wantCreate > 0 {
				if testCase.publisher.authorDID != "did:plc:bob" || issue.ValidateRecordKey(testCase.publisher.rkey) != nil || testCase.publisher.record.Subject != subject || testCase.publisher.record.CreatedAt != now.UTC() || testCase.publisher.record.UpdatedAt != now.UTC() {
					t.Fatalf("publication = did %q rkey %q record %#v", testCase.publisher.authorDID, testCase.publisher.rkey, testCase.publisher.record)
				}
				if testCase.parentURI == "" && testCase.publisher.record.Parent != nil {
					t.Fatalf("parent = %#v, want nil", testCase.publisher.record.Parent)
				}
				if testCase.parentURI != "" && (testCase.publisher.record.Parent == nil || *testCase.publisher.record.Parent != parentRef) {
					t.Fatalf("parent = %#v, want %#v", testCase.publisher.record.Parent, parentRef)
				}
			}
		})
	}
}

func TestServiceRejectsNoncanonicalIdentifiersBeforeDependencies(t *testing.T) {
	t.Parallel()
	testCases := []struct {
		name      string
		operation func(*Service) error
	}{
		{name: "invalid get issue", operation: func(service *Service) error { _, err := service.Get(context.Background(), "invalid", ""); return err }},
		{name: "invalid viewer", operation: func(service *Service) error {
			_, err := service.Get(context.Background(), testIssueURI, "INVALID")
			return err
		}},
		{name: "invalid create author", operation: func(service *Service) error {
			_, err := service.Create(context.Background(), "INVALID", CreateInput{IssueURI: testIssueURI})
			return err
		}},
		{name: "invalid delete URI", operation: func(service *Service) error { return service.Delete(context.Background(), "did:plc:bob", testIssueURI) }},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			store, publisher := &serviceStore{}, &servicePublisher{}
			err := testCase.operation(NewService(store, publisher, serviceClock{now: time.Now()}))
			if !errors.Is(err, issue.ErrValidation) || store.projectionCalls+store.issueCalls+store.parentCalls != 0 || publisher.createCalls+publisher.deleteCalls != 0 {
				t.Fatalf("error/dependency calls = %v/%#v/%#v", err, store, publisher)
			}
		})
	}
}
