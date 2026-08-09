package star

import (
	"context"
	"errors"
	"testing"
	"time"
)

type serviceStore struct {
	target          Target
	count           int64
	stars           []Star
	targetErr       error
	projectionErr   error
	targetURI       string
	projectionURI   string
	projectionLimit int
	targetCalls     int
	projectionCalls int
}

func (store *serviceStore) GetTarget(_ context.Context, uri string) (Target, int64, error) {
	store.targetCalls++
	store.targetURI = uri
	return store.target, store.count, store.targetErr
}

func (store *serviceStore) GetProjection(_ context.Context, uri string, limit int) (Projection, error) {
	store.projectionCalls++
	store.projectionURI, store.projectionLimit = uri, limit
	return Projection{StarCount: store.count, Stars: store.stars}, store.projectionErr
}

type servicePublisher struct {
	result      Star
	err         error
	authorDID   string
	target      Target
	createdAt   time.Time
	createCalls int
	deleteCalls int
}

func (publisher *servicePublisher) CreateStar(_ context.Context, did string, target Target, createdAt time.Time) (Star, error) {
	publisher.createCalls++
	publisher.authorDID, publisher.target, publisher.createdAt = did, target, createdAt
	return publisher.result, publisher.err
}

func (publisher *servicePublisher) DeleteStar(_ context.Context, did string, target Target) error {
	publisher.deleteCalls++
	publisher.authorDID, publisher.target = did, target
	return publisher.err
}

type serviceClock struct{ now time.Time }

func (clock serviceClock) Now() time.Time { return clock.now }

func TestServiceProjectedReadsAndAsynchronousMutations(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 8, 9, 12, 0, 0, 123, time.FixedZone("offset", 3600))
	target := Target{URI: testRepositoryURI, CID: testCID}
	projected := Star{URI: "at://did:plc:alice/dev.adenosine.star/key", CID: testCID, AuthorDID: "did:plc:alice", Target: target, CreatedAt: now, IndexedAt: now}
	testCases := []struct {
		name                string
		operation           string
		uri                 string
		store               *serviceStore
		publisher           *servicePublisher
		wantErr             error
		wantTargetCalls     int
		wantProjectionCalls int
		wantCreateCalls     int
		wantDeleteCalls     int
	}{
		{name: "public projection is bounded and atomic", operation: "get", uri: testRepositoryURI, store: &serviceStore{target: target, count: 7, stars: []Star{projected}}, publisher: &servicePublisher{}, wantProjectionCalls: 1},
		{name: "create uses current projected CID and clock", operation: "create", uri: testRepositoryURI, store: &serviceStore{target: target}, publisher: &servicePublisher{result: projected}, wantTargetCalls: 1, wantCreateCalls: 1},
		{name: "delete uses current projected CID", operation: "delete", uri: testRepositoryURI, store: &serviceStore{target: target}, publisher: &servicePublisher{}, wantTargetCalls: 1, wantDeleteCalls: 1},
		{name: "invalid URI fails before lookup", operation: "create", uri: "https://example.test/repo", store: &serviceStore{}, publisher: &servicePublisher{}, wantErr: ErrValidation},
		{name: "deleted target is not found", operation: "delete", uri: testRepositoryURI, store: &serviceStore{targetErr: ErrNotFound}, publisher: &servicePublisher{}, wantErr: ErrNotFound, wantTargetCalls: 1},
		{name: "publisher error remains typed", operation: "create", uri: testRepositoryURI, store: &serviceStore{target: target}, publisher: &servicePublisher{err: &ProviderError{Operation: "create", Err: errors.New("secret")}}, wantErr: ErrProvider, wantTargetCalls: 1, wantCreateCalls: 1},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			service := NewService(testCase.store, testCase.publisher, serviceClock{now: now})
			var err error
			switch testCase.operation {
			case "get":
				var projection Projection
				projection, err = service.Get(context.Background(), testCase.uri)
				if err == nil && (projection.StarCount != 7 || len(projection.Stars) != 1 || testCase.store.projectionLimit != 100) {
					t.Fatalf("projection = %#v, limit = %d", projection, testCase.store.projectionLimit)
				}
			case "create":
				_, err = service.Create(context.Background(), "did:plc:alice", testCase.uri)
			case "delete":
				err = service.Delete(context.Background(), "did:plc:alice", testCase.uri)
			}
			if !errors.Is(err, testCase.wantErr) || testCase.store.targetCalls != testCase.wantTargetCalls || testCase.store.projectionCalls != testCase.wantProjectionCalls || testCase.publisher.createCalls != testCase.wantCreateCalls || testCase.publisher.deleteCalls != testCase.wantDeleteCalls {
				t.Fatalf("err/calls = %v target=%d projection=%d create=%d delete=%d", err, testCase.store.targetCalls, testCase.store.projectionCalls, testCase.publisher.createCalls, testCase.publisher.deleteCalls)
			}
			if testCase.wantTargetCalls > 0 && testCase.store.targetURI != testCase.uri {
				t.Fatalf("target lookup URI = %q, want %q", testCase.store.targetURI, testCase.uri)
			}
			if testCase.wantProjectionCalls > 0 && testCase.store.projectionURI != testCase.uri {
				t.Fatalf("star projection URI = %q, want %q", testCase.store.projectionURI, testCase.uri)
			}
			if testCase.wantCreateCalls+testCase.wantDeleteCalls > 0 && testCase.publisher.err == nil {
				if testCase.publisher.authorDID != "did:plc:alice" || testCase.publisher.target != target {
					t.Fatalf("publisher identity/target = %q/%#v", testCase.publisher.authorDID, testCase.publisher.target)
				}
			}
			if testCase.wantCreateCalls > 0 && testCase.publisher.createdAt != now.UTC() {
				t.Fatalf("createdAt = %v, want %v", testCase.publisher.createdAt, now.UTC())
			}
		})
	}
}
