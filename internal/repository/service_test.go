package repository

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

type fakeStore struct {
	created     Repository
	states      []State
	identity    *ATIdentity
	activateErr error
}

func (store *fakeStore) Create(_ context.Context, repository Repository) (Repository, error) {
	store.created = repository
	return repository, nil
}

func (store *fakeStore) GetByOwnerSlug(context.Context, string, string) (Repository, error) {
	return store.created, nil
}

func (store *fakeStore) ListByOrganization(context.Context, uuid.UUID) ([]Repository, error) {
	return nil, nil
}
func (store *fakeStore) PageByOrganization(context.Context, uuid.UUID, string, *uuid.UUID, int32) ([]Repository, error) {
	return nil, nil
}

func (store *fakeStore) UpdateState(_ context.Context, id ID, state State, updatedAt time.Time) (Repository, error) {
	store.states = append(store.states, state)
	store.created.ID, store.created.State, store.created.UpdatedAt = id, state, updatedAt
	return store.created, nil
}

func (store *fakeStore) Activate(_ context.Context, id ID, identity *ATIdentity, updatedAt time.Time) (Repository, error) {
	if store.activateErr != nil {
		return Repository{}, store.activateErr
	}
	store.identity = identity
	store.created.ID, store.created.State, store.created.UpdatedAt = id, StateActive, updatedAt
	if identity != nil {
		store.created.ATURI, store.created.ATCID = identity.URI, identity.CID
	}
	return store.created, nil
}

type fakeGit struct{ err error }

func (git fakeGit) Init(context.Context, ID, string) error { return git.err }

type fixedClock struct{ now time.Time }

func (clock fixedClock) Now() time.Time { return clock.now }

type fixedIDs struct{ id ID }

func (ids fixedIDs) New() (ID, error) { return ids.id, nil }

type fakePublisher struct {
	publication Publication
	identity    ATIdentity
	err         error
	calls       int
}

func (publisher *fakePublisher) Publish(_ context.Context, publication Publication) (ATIdentity, error) {
	publisher.calls++
	publisher.publication = publication
	return publisher.identity, publisher.err
}

type fixedEndpoints struct{}

func (fixedEndpoints) For(Repository) (string, string, string) {
	return "https://code.test/did:plc:alice/hello-world", "https://code.test/did:plc:alice/hello-world.git", "ssh://git@code.test/did:plc:alice/hello-world.git"
}

func TestServiceCreatePublicationLifecycle(t *testing.T) {
	t.Parallel()
	id := ID(uuid.MustParse("0198a851-2a89-7ae2-a370-dc68883e3af1"))
	now := time.Date(2026, time.August, 9, 0, 0, 0, 0, time.UTC)
	published := ATIdentity{URI: "at://did:plc:alice/dev.adenosine.repo/0198a8512a897ae2a370dc68883e3af1", CID: "bafyrepo"}
	testCases := []struct {
		name             string
		visibility       Visibility
		publisherErr     error
		activateErr      error
		wantErr          string
		wantPublishCalls int
		wantStates       []State
		wantIdentity     *ATIdentity
	}{
		{name: "public publishes and persists identity", visibility: VisibilityPublic, wantPublishCalls: 1, wantIdentity: &published},
		{name: "private skips publication and persists null identity", visibility: VisibilityPrivate, wantPublishCalls: 0},
		{name: "provider failure marks failed", visibility: VisibilityPublic, publisherErr: errors.New("provider failed"), wantErr: "publish repository record", wantPublishCalls: 1, wantStates: []State{StateFailed}},
		{name: "activation failure marks failed", visibility: VisibilityPublic, activateErr: errors.New("database failed"), wantErr: "activate repository", wantPublishCalls: 1, wantStates: []State{StateFailed}},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			store := &fakeStore{activateErr: testCase.activateErr}
			publisher := &fakePublisher{identity: published, err: testCase.publisherErr}
			service := NewService(store, fakeGit{}, fixedClock{now}, fixedIDs{id}, publisher, fixedEndpoints{})
			result, err := service.Create(context.Background(), CreateInput{
				OwnerDID: "did:plc:alice", Slug: "hello-world", DisplayName: "Hello World", Visibility: testCase.visibility, DefaultBranch: "main",
			})
			if testCase.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), testCase.wantErr) {
					t.Fatalf("error = %v, want containing %q", err, testCase.wantErr)
				}
			} else if err != nil {
				t.Fatalf("create repository: %v", err)
			}
			if publisher.calls != testCase.wantPublishCalls {
				t.Fatalf("publication calls = %d, want %d", publisher.calls, testCase.wantPublishCalls)
			}
			if len(testCase.wantStates) != len(store.states) || (len(store.states) > 0 && store.states[0] != testCase.wantStates[0]) {
				t.Fatalf("state transitions = %v, want %v", store.states, testCase.wantStates)
			}
			if testCase.wantIdentity == nil && store.identity != nil {
				t.Fatalf("identity = %#v, want nil", store.identity)
			}
			if testCase.wantIdentity != nil && (store.identity == nil || *store.identity != *testCase.wantIdentity) {
				t.Fatalf("identity = %#v, want %#v", store.identity, testCase.wantIdentity)
			}
			if testCase.wantErr == "" && result.State != StateActive {
				t.Fatalf("state = %q, want active", result.State)
			}
			if publisher.calls == 1 && (publisher.publication.ID != id || publisher.publication.Slug != "hello-world" || publisher.publication.Name != "Hello World") {
				t.Fatalf("published ID/slug/name = %s, %q, %q", publisher.publication.ID.String(), publisher.publication.Slug, publisher.publication.Name)
			}
		})
	}
}

func TestServiceCreateMarksGitFailure(t *testing.T) {
	t.Parallel()
	testCases := []struct {
		name string
	}{
		{name: "initialization failure"},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			store := &fakeStore{}
			service := NewService(store, fakeGit{err: errors.New("git failed")}, fixedClock{time.Now()}, fixedIDs{ID(uuid.MustParse("0198a851-2a89-7ae2-a370-dc68883e3af1"))}, &fakePublisher{}, fixedEndpoints{})
			_, err := service.Create(context.Background(), CreateInput{OwnerDID: "did:plc:alice", Slug: "hello-world", Visibility: VisibilityPublic, DefaultBranch: "main"})
			if err == nil || len(store.states) != 1 || store.states[0] != StateFailed {
				t.Fatalf("error = %v, state transitions = %v", err, store.states)
			}
		})
	}
}

func TestCreateInputValidate(t *testing.T) {
	t.Parallel()
	valid := CreateInput{OwnerDID: "did:plc:alice", Slug: "hello-world", Visibility: VisibilityPublic, DefaultBranch: "main"}
	testCases := []struct {
		name  string
		input CreateInput
		valid bool
	}{
		{name: "valid", input: valid, valid: true},
		{name: "missing owner", input: CreateInput{Slug: "hello", Visibility: VisibilityPublic, DefaultBranch: "main"}},
		{name: "invalid slug", input: CreateInput{OwnerDID: "did:plc:alice", Slug: "../escape", Visibility: VisibilityPublic, DefaultBranch: "main"}},
		{name: "invalid visibility", input: CreateInput{OwnerDID: "did:plc:alice", Slug: "hello", Visibility: "internal", DefaultBranch: "main"}},
		{name: "option branch", input: CreateInput{OwnerDID: "did:plc:alice", Slug: "hello", Visibility: VisibilityPublic, DefaultBranch: "-upload-pack"}},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			err := testCase.input.Validate()
			if testCase.valid && err != nil {
				t.Fatalf("valid input rejected: %v", err)
			}
			if !testCase.valid && err == nil {
				t.Fatalf("invalid input accepted: %+v", testCase.input)
			}
		})
	}
}
