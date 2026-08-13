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
	forkSource  ForkSource
	forkErr     error
}

func (store *fakeStore) GetForkSourceByURI(context.Context, string) (ForkSource, error) {
	return store.forkSource, store.forkErr
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

type fakeGit struct {
	err        error
	forkID     ID
	forkSource ForkSource
	syncID     ID
	syncSource ForkSource
	syncResult ForkSync
	syncErr    error
}

func (git fakeGit) Init(context.Context, ID, string) error { return git.err }

func (git *fakeGit) Fork(_ context.Context, id ID, source ForkSource, _ string) error {
	git.forkID, git.forkSource = id, source
	return git.err
}

func (git *fakeGit) SyncFork(_ context.Context, id ID, source ForkSource, _ string) (ForkSync, error) {
	git.syncID, git.syncSource = id, source
	return git.syncResult, git.syncErr
}

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

func TestServiceCreateFork(t *testing.T) {
	t.Parallel()
	testCases := []struct {
		name string
	}{
		{name: "copies source and publishes portable ancestry"},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			id := ID(uuid.MustParse("0198a851-2a89-7ae2-a370-dc68883e3af1"))
			upstreamID := ID(uuid.MustParse("0198a851-2a89-7ae2-a370-dc68883e3af2"))
			source := &ForkSource{
				URI: "at://did:plc:alice/dev.adenosine.repo/upstream", CID: "bafyupstream",
				LocalRepositoryID: &upstreamID,
			}
			store := &fakeStore{}
			git := &fakeGit{}
			publisher := &fakePublisher{identity: ATIdentity{URI: "at://did:plc:bob/dev.adenosine.repo/fork", CID: "bafyfork"}}
			service := NewService(store, git, fixedClock{time.Now()}, fixedIDs{id}, publisher, fixedEndpoints{})

			created, err := service.Create(context.Background(), CreateInput{
				OwnerDID: "did:plc:bob", Slug: "upstream", Visibility: VisibilityPublic,
				DefaultBranch: "main", ForkedFrom: source,
			})
			if err != nil {
				t.Fatalf("Create() error = %v", err)
			}
			if git.forkID != id || git.forkSource.URI != source.URI {
				t.Fatalf("fork call = %s from %+v", git.forkID, git.forkSource)
			}
			if created.ForkedFrom == nil || created.ForkedFrom.URI != source.URI {
				t.Fatalf("created ancestry = %+v, want %+v", created.ForkedFrom, source)
			}
			if publisher.publication.ForkedFrom == nil || publisher.publication.ForkedFrom.URI != source.URI || publisher.publication.ForkedFrom.CID != source.CID {
				t.Fatalf("published ancestry = %+v, want %s %s", publisher.publication.ForkedFrom, source.URI, source.CID)
			}
		})
	}
}

func TestServiceSyncFork(t *testing.T) {
	t.Parallel()
	testCases := []struct {
		name        string
		localSource bool
	}{
		{name: "uses local source storage", localSource: true},
		{name: "refreshes a federated source endpoint"},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			repositoryID := ID(uuid.MustParse("0198a851-2a89-7ae2-a370-dc68883e3af1"))
			upstreamID := ID(uuid.MustParse("0198a851-2a89-7ae2-a370-dc68883e3af2"))
			ancestry := &ForkSource{URI: "at://did:plc:alice/dev.adenosine.repo/upstream", CID: "bafyold"}
			resolved := ForkSource{URI: ancestry.URI, CID: "bafycurrent", GitHTTPS: "https://git.example/upstream.git"}
			if testCase.localSource {
				ancestry.LocalRepositoryID = &upstreamID
				resolved = *ancestry
			}
			store := &fakeStore{forkSource: resolved}
			git := &fakeGit{syncResult: ForkSync{BeforeSHA: "before", AfterSHA: "after", Updated: true}}
			service := NewService(store, git, fixedClock{}, fixedIDs{}, &fakePublisher{}, fixedEndpoints{})

			result, err := service.SyncFork(context.Background(), Repository{
				ID: repositoryID, DefaultBranch: "main", ForkedFrom: ancestry,
			})
			if err != nil {
				t.Fatalf("SyncFork() error = %v", err)
			}
			if !result.Updated || git.syncID != repositoryID || git.syncSource.CID != resolved.CID {
				t.Fatalf("result = %+v, sync call = %s from %+v", result, git.syncID, git.syncSource)
			}
		})
	}
}

func TestCreateInputValidate(t *testing.T) {
	t.Parallel()
	valid := CreateInput{OwnerDID: "did:plc:alice", Slug: "hello-world", Visibility: VisibilityPublic, DefaultBranch: "main"}
	validFork := ForkSource{URI: "at://did:plc:bob/dev.adenosine.repo/source", CID: "bafyrepo", GitHTTPS: "https://git.example/source.git"}
	testCases := []struct {
		name  string
		input CreateInput
		valid bool
	}{
		{name: "valid", input: valid, valid: true},
		{name: "valid public fork", input: CreateInput{OwnerDID: "did:plc:alice", Slug: "hello", Visibility: VisibilityPublic, DefaultBranch: "main", ForkedFrom: &validFork}, valid: true},
		{name: "private fork", input: CreateInput{OwnerDID: "did:plc:alice", Slug: "hello", Visibility: VisibilityPrivate, DefaultBranch: "main", ForkedFrom: &validFork}},
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
