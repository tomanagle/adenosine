package moderation

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/adenosine-dev/adenosine/internal/issue"
)

const testRecordURI = "at://did:plc:bob/dev.adenosine.issueComment/0198a8512a897ae2a370dc68883e3af1"

type serviceStore struct {
	blocks       []BlockedDID
	hidden       []HiddenRecord
	err          error
	operation    string
	accountDID   string
	target       string
	createdAt    time.Time
	mutateCalls  int
	listingCalls int
}

func (store *serviceStore) mutation(operation, accountDID, target string, createdAt time.Time) error {
	store.operation, store.accountDID, store.target, store.createdAt = operation, accountDID, target, createdAt
	store.mutateCalls++
	return store.err
}
func (store *serviceStore) PutBlock(_ context.Context, accountDID, blockedDID string, at time.Time) error {
	return store.mutation("block", accountDID, blockedDID, at)
}
func (store *serviceStore) DeleteBlock(_ context.Context, accountDID, blockedDID string) error {
	return store.mutation("unblock", accountDID, blockedDID, time.Time{})
}
func (store *serviceStore) ListBlocks(_ context.Context, accountDID string) ([]BlockedDID, error) {
	store.operation, store.accountDID = "list-blocks", accountDID
	store.listingCalls++
	return store.blocks, store.err
}
func (store *serviceStore) PutHidden(_ context.Context, accountDID, uri string, at time.Time) error {
	return store.mutation("hide", accountDID, uri, at)
}
func (store *serviceStore) DeleteHidden(_ context.Context, accountDID, uri string) error {
	return store.mutation("unhide", accountDID, uri, time.Time{})
}
func (store *serviceStore) ListHidden(_ context.Context, accountDID string) ([]HiddenRecord, error) {
	store.operation, store.accountDID = "list-hidden", accountDID
	store.listingCalls++
	return store.hidden, store.err
}

type serviceClock struct{ now time.Time }

func (clock serviceClock) Now() time.Time { return clock.now }

func TestServiceScopesIdempotentModerationToAccount(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, time.August, 9, 12, 0, 0, 123, time.FixedZone("offset", 3600))
	testCases := []struct {
		name          string
		operation     string
		store         *serviceStore
		wantOperation string
		wantTarget    string
		wantMutation  int
		wantListing   int
	}{
		{name: "block", operation: "block", store: &serviceStore{}, wantOperation: "block", wantTarget: "did:plc:bob", wantMutation: 1},
		{name: "unblock", operation: "unblock", store: &serviceStore{}, wantOperation: "unblock", wantTarget: "did:plc:bob", wantMutation: 1},
		{name: "list blocks returns nonnil", operation: "list-blocks", store: &serviceStore{}, wantOperation: "list-blocks", wantListing: 1},
		{name: "hide", operation: "hide", store: &serviceStore{}, wantOperation: "hide", wantTarget: testRecordURI, wantMutation: 1},
		{name: "unhide", operation: "unhide", store: &serviceStore{}, wantOperation: "unhide", wantTarget: testRecordURI, wantMutation: 1},
		{name: "list hidden returns nonnil", operation: "list-hidden", store: &serviceStore{}, wantOperation: "list-hidden", wantListing: 1},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			service := NewService(testCase.store, serviceClock{now: now})
			var err error
			switch testCase.operation {
			case "block":
				err = service.Block(context.Background(), "did:plc:alice", "did:plc:bob")
			case "unblock":
				err = service.Unblock(context.Background(), "did:plc:alice", "did:plc:bob")
			case "list-blocks":
				var values []BlockedDID
				values, err = service.ListBlocks(context.Background(), "did:plc:alice")
				if values == nil {
					t.Fatal("blocks is nil")
				}
			case "hide":
				err = service.Hide(context.Background(), "did:plc:alice", testRecordURI)
			case "unhide":
				err = service.Unhide(context.Background(), "did:plc:alice", testRecordURI)
			case "list-hidden":
				var values []HiddenRecord
				values, err = service.ListHidden(context.Background(), "did:plc:alice")
				if values == nil {
					t.Fatal("hidden records is nil")
				}
			}
			if err != nil || testCase.store.operation != testCase.wantOperation || testCase.store.accountDID != "did:plc:alice" || testCase.store.target != testCase.wantTarget || testCase.store.mutateCalls != testCase.wantMutation || testCase.store.listingCalls != testCase.wantListing {
				t.Fatalf("result = err %v store %#v", err, testCase.store)
			}
			if (testCase.operation == "block" || testCase.operation == "hide") && testCase.store.createdAt != now.UTC() {
				t.Fatalf("createdAt = %v, want %v", testCase.store.createdAt, now.UTC())
			}
		})
	}
}

func TestServiceValidatesModerationIdentifiersBeforeStore(t *testing.T) {
	t.Parallel()
	testCases := []struct {
		name      string
		operation func(*Service) error
	}{
		{name: "invalid account block", operation: func(service *Service) error { return service.Block(context.Background(), "INVALID", "did:plc:bob") }},
		{name: "invalid blocked DID", operation: func(service *Service) error { return service.Block(context.Background(), "did:plc:alice", "INVALID") }},
		{name: "self block", operation: func(service *Service) error {
			return service.Block(context.Background(), "did:plc:alice", "did:plc:alice")
		}},
		{name: "invalid unblock account", operation: func(service *Service) error { return service.Unblock(context.Background(), "INVALID", "did:plc:bob") }},
		{name: "invalid hidden URI", operation: func(service *Service) error { return service.Hide(context.Background(), "did:plc:alice", "invalid") }},
		{name: "invalid unhidden URI", operation: func(service *Service) error {
			return service.Unhide(context.Background(), "did:plc:alice", "at://handle.example/dev.adenosine.issue/key")
		}},
		{name: "invalid list blocks account", operation: func(service *Service) error {
			_, err := service.ListBlocks(context.Background(), "INVALID")
			return err
		}},
		{name: "invalid list hidden account", operation: func(service *Service) error {
			_, err := service.ListHidden(context.Background(), "INVALID")
			return err
		}},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			store := &serviceStore{}
			err := testCase.operation(NewService(store, serviceClock{now: time.Now()}))
			if !errors.Is(err, issue.ErrValidation) || store.mutateCalls+store.listingCalls != 0 {
				t.Fatalf("error/calls = %v/%#v", err, store)
			}
		})
	}
}

func TestServicePreservesStoreErrors(t *testing.T) {
	t.Parallel()
	cause := errors.New("database unavailable")
	testCases := []struct {
		name      string
		operation func(*Service) error
	}{
		{name: "block", operation: func(service *Service) error {
			return service.Block(context.Background(), "did:plc:alice", "did:plc:bob")
		}},
		{name: "unblock", operation: func(service *Service) error {
			return service.Unblock(context.Background(), "did:plc:alice", "did:plc:bob")
		}},
		{name: "list blocks", operation: func(service *Service) error {
			_, err := service.ListBlocks(context.Background(), "did:plc:alice")
			return err
		}},
		{name: "hide", operation: func(service *Service) error {
			return service.Hide(context.Background(), "did:plc:alice", testRecordURI)
		}},
		{name: "unhide", operation: func(service *Service) error {
			return service.Unhide(context.Background(), "did:plc:alice", testRecordURI)
		}},
		{name: "list hidden", operation: func(service *Service) error {
			_, err := service.ListHidden(context.Background(), "did:plc:alice")
			return err
		}},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			err := testCase.operation(NewService(&serviceStore{err: cause}, serviceClock{now: time.Now()}))
			if !errors.Is(err, cause) {
				t.Fatalf("error = %v, want cause", err)
			}
		})
	}
}
