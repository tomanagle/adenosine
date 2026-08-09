package issue

import (
	"context"
	"errors"
	"testing"
	"time"
)

const (
	serviceAliceRepositoryURI = "at://did:plc:alice/dev.adenosine.repo/project"
	serviceBobIssueURI        = "at://did:plc:bob/dev.adenosine.issue/0198a8512a897ae2a370dc68883e3af1"
)

type issueServiceStore struct {
	projection      Projection
	repository      StrongRef
	status          statusTarget
	err             error
	projectionURI   string
	repositoryURI   string
	issueURI        string
	projectionLimit int
	projectionCalls int
	repositoryCalls int
	statusCalls     int
}

func (store *issueServiceStore) GetProjection(_ context.Context, uri string, limit int) (Projection, error) {
	store.projectionCalls++
	store.projectionURI, store.projectionLimit = uri, limit
	return store.projection, store.err
}

func (store *issueServiceStore) GetRepositoryTarget(_ context.Context, uri string) (StrongRef, error) {
	store.repositoryCalls++
	store.repositoryURI = uri
	return store.repository, store.err
}

func (store *issueServiceStore) GetStatusTarget(_ context.Context, uri string) (statusTarget, error) {
	store.statusCalls++
	store.issueURI = uri
	return store.status, store.err
}

type issueServicePublisher struct {
	issue        Issue
	status       Status
	err          error
	authorDID    string
	rkey         string
	record       Record
	statusRecord StatusRecord
	createCalls  int
	statusCalls  int
}

func (publisher *issueServicePublisher) CreateIssue(_ context.Context, did, rkey string, record Record) (Issue, error) {
	publisher.createCalls++
	publisher.authorDID, publisher.rkey, publisher.record = did, rkey, record
	return publisher.issue, publisher.err
}

func (publisher *issueServicePublisher) PutIssueStatus(_ context.Context, did string, record StatusRecord) (Status, error) {
	publisher.statusCalls++
	publisher.authorDID, publisher.statusRecord = did, record
	return publisher.status, publisher.err
}

type issueServiceClock struct{ now time.Time }

func (clock issueServiceClock) Now() time.Time { return clock.now }

func TestIssueServiceProjectedReadsAndAsynchronousWrites(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 8, 9, 12, 0, 0, 123000000, time.FixedZone("offset", 3600))
	statusCreatedAt := time.Date(2026, 8, 8, 10, 0, 0, 0, time.UTC)
	repository := StrongRef{URI: serviceAliceRepositoryURI, CID: testCID}
	subject := StrongRef{URI: serviceBobIssueURI, CID: testCID}
	projected := ProjectedIssue{Issue: Issue{URI: serviceBobIssueURI, CID: testCID, AuthorDID: "did:plc:bob", Record: Record{Repository: repository, Title: "title", Body: "body", CreatedAt: now, UpdatedAt: now}}, State: StateOpen, IndexedAt: now}
	testCases := []struct {
		name                string
		operation           string
		authorDID           string
		store               *issueServiceStore
		publisher           *issueServicePublisher
		wantErr             error
		wantProjectionCalls int
		wantRepositoryCalls int
		wantStatusCalls     int
		wantCreateCalls     int
		wantPublishStatus   int
	}{
		{name: "public read is bounded and returns atomic counts", operation: "get", store: &issueServiceStore{projection: Projection{IssueCount: 8, OpenIssueCount: 3, Issues: []ProjectedIssue{projected}}}, publisher: &issueServicePublisher{}, wantProjectionCalls: 1},
		{name: "Bob creates against Alice current repository projection", operation: "create", authorDID: "did:plc:bob", store: &issueServiceStore{repository: repository}, publisher: &issueServicePublisher{issue: projected.Issue}, wantRepositoryCalls: 1, wantCreateCalls: 1},
		{name: "Bob cannot publish Alice repository status", operation: "status", authorDID: "did:plc:bob", store: &issueServiceStore{status: statusTarget{Subject: subject, Repository: repository, StatusCreatedAt: statusCreatedAt}}, publisher: &issueServicePublisher{}, wantErr: ErrAuthorization, wantStatusCalls: 1},
		{name: "Alice status uses exact current refs and authoritative creation time", operation: "status", authorDID: "did:plc:alice", store: &issueServiceStore{status: statusTarget{Subject: subject, Repository: repository, StatusCreatedAt: statusCreatedAt}}, publisher: &issueServicePublisher{}, wantStatusCalls: 1, wantPublishStatus: 1},
		{name: "first Alice status uses current time for creation", operation: "status", authorDID: "did:plc:alice", store: &issueServiceStore{status: statusTarget{Subject: subject, Repository: repository}}, publisher: &issueServicePublisher{}, wantStatusCalls: 1, wantPublishStatus: 1},
		{name: "missing projected repository is not found", operation: "create", authorDID: "did:plc:bob", store: &issueServiceStore{err: ErrNotFound}, publisher: &issueServicePublisher{}, wantErr: ErrNotFound, wantRepositoryCalls: 1},
		{name: "invalid repository fails before projection lookup", operation: "create", authorDID: "did:plc:bob", store: &issueServiceStore{}, publisher: &issueServicePublisher{}, wantErr: ErrValidation},
		{name: "publisher provider error remains typed", operation: "create", authorDID: "did:plc:bob", store: &issueServiceStore{repository: repository}, publisher: &issueServicePublisher{err: &ProviderError{Operation: "create", Err: errors.New("secret")}}, wantErr: ErrProvider, wantRepositoryCalls: 1, wantCreateCalls: 1},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			service := NewService(testCase.store, testCase.publisher, issueServiceClock{now: now})
			var err error
			switch testCase.operation {
			case "get":
				var projection Projection
				projection, err = service.Get(context.Background(), serviceAliceRepositoryURI)
				if err == nil && (projection.IssueCount != 8 || projection.OpenIssueCount != 3 || len(projection.Issues) != 1 || testCase.store.projectionLimit != 100) {
					t.Fatalf("projection = %#v, limit = %d", projection, testCase.store.projectionLimit)
				}
			case "create":
				uri := serviceAliceRepositoryURI
				if testCase.wantErr == ErrValidation {
					uri = "invalid"
				}
				_, err = service.Create(context.Background(), testCase.authorDID, CreateInput{RepositoryURI: uri, Title: "title", Body: "body"})
			case "status":
				_, err = service.PutStatus(context.Background(), testCase.authorDID, StatusInput{IssueURI: serviceBobIssueURI, State: StateClosed})
			}
			if !errors.Is(err, testCase.wantErr) || testCase.store.projectionCalls != testCase.wantProjectionCalls || testCase.store.repositoryCalls != testCase.wantRepositoryCalls || testCase.store.statusCalls != testCase.wantStatusCalls || testCase.publisher.createCalls != testCase.wantCreateCalls || testCase.publisher.statusCalls != testCase.wantPublishStatus {
				t.Fatalf("err/calls = %v projection=%d repository=%d status=%d create=%d publish-status=%d", err, testCase.store.projectionCalls, testCase.store.repositoryCalls, testCase.store.statusCalls, testCase.publisher.createCalls, testCase.publisher.statusCalls)
			}
			if testCase.wantCreateCalls > 0 && testCase.publisher.err == nil {
				if testCase.publisher.authorDID != "did:plc:bob" || ValidateRecordKey(testCase.publisher.rkey) != nil || testCase.publisher.record.Repository != repository || testCase.publisher.record.CreatedAt != now.UTC() || testCase.publisher.record.UpdatedAt != now.UTC() {
					t.Fatalf("create publication = did=%q rkey=%q record=%#v", testCase.publisher.authorDID, testCase.publisher.rkey, testCase.publisher.record)
				}
			}
			if testCase.wantPublishStatus > 0 {
				wantCreatedAt := statusCreatedAt
				if testCase.store.status.StatusCreatedAt.IsZero() {
					wantCreatedAt = now.UTC()
				}
				if testCase.publisher.authorDID != "did:plc:alice" || testCase.publisher.statusRecord.Subject != subject || testCase.publisher.statusRecord.Repository != repository || testCase.publisher.statusRecord.State != StateClosed || testCase.publisher.statusRecord.CreatedAt != wantCreatedAt || testCase.publisher.statusRecord.UpdatedAt != now.UTC() {
					t.Fatalf("status publication = did=%q record=%#v", testCase.publisher.authorDID, testCase.publisher.statusRecord)
				}
			}
		})
	}
}
