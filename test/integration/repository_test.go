package integration_test

import (
	"context"
	"os/exec"
	"testing"
	"time"

	gitservice "github.com/adenosine-dev/adenosine/internal/git"
	"github.com/adenosine-dev/adenosine/internal/repository"
	"github.com/adenosine-dev/adenosine/internal/storage"
	"github.com/google/uuid"
)

type memoryRepositoryStore struct {
	repository repository.Repository
}

func (store *memoryRepositoryStore) Create(_ context.Context, value repository.Repository) (repository.Repository, error) {
	store.repository = value
	return value, nil
}

func (store *memoryRepositoryStore) GetByOwnerSlug(context.Context, string, string) (repository.Repository, error) {
	return store.repository, nil
}

func (store *memoryRepositoryStore) ListByOrganization(context.Context, uuid.UUID) ([]repository.Repository, error) {
	return []repository.Repository{}, nil
}

func (store *memoryRepositoryStore) PageByOrganization(context.Context, uuid.UUID, string, *uuid.UUID, int32) ([]repository.Repository, error) {
	return []repository.Repository{}, nil
}

func (store *memoryRepositoryStore) UpdateState(_ context.Context, id repository.ID, state repository.State, updatedAt time.Time) (repository.Repository, error) {
	store.repository.ID = id
	store.repository.State = state
	store.repository.UpdatedAt = updatedAt
	return store.repository, nil
}

func (store *memoryRepositoryStore) Activate(_ context.Context, id repository.ID, identity *repository.ATIdentity, updatedAt time.Time) (repository.Repository, error) {
	store.repository.ID = id
	store.repository.State = repository.StateActive
	store.repository.UpdatedAt = updatedAt
	if identity != nil {
		store.repository.ATURI, store.repository.ATCID = identity.URI, identity.CID
	}
	return store.repository, nil
}

type integrationClock struct{ now time.Time }

func (clock integrationClock) Now() time.Time { return clock.now }

type integrationIDs struct{ id repository.ID }

func (ids integrationIDs) New() (repository.ID, error) { return ids.id, nil }

type integrationPublisher struct{}

func (integrationPublisher) Publish(context.Context, repository.Publication) (repository.ATIdentity, error) {
	return repository.ATIdentity{}, nil
}

type integrationEndpoints struct{}

func (integrationEndpoints) For(repository.Repository) (string, string, string) { return "", "", "" }

func TestRepositoryServiceCreatesBareRepository(t *testing.T) {
	t.Parallel()
	testCases := []struct {
		name string
	}{
		{name: "public repository"},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			binary, err := exec.LookPath("git")
			if err != nil {
				t.Skip("git executable is unavailable")
			}
			filesystem, err := storage.NewFilesystem(t.TempDir())
			if err != nil {
				t.Fatalf("create filesystem storage: %v", err)
			}
			id := repository.ID(uuid.MustParse("0198a851-2a89-7ae2-a370-dc68883e3af1"))
			store := &memoryRepositoryStore{}
			service := repository.NewService(store, gitservice.NewService(gitservice.NewRunner(binary), filesystem),
				integrationClock{now: time.Date(2026, time.August, 9, 0, 0, 0, 0, time.UTC)}, integrationIDs{id: id}, integrationPublisher{}, integrationEndpoints{})
			created, err := service.Create(context.Background(), repository.CreateInput{OwnerDID: "did:plc:alice", Slug: "hello-world", Visibility: repository.VisibilityPublic, DefaultBranch: "main"})
			if err != nil {
				t.Fatalf("create repository: %v", err)
			}
			if created.State != repository.StateActive {
				t.Fatalf("state = %q, want active", created.State)
			}
			exists, err := filesystem.Exists(context.Background(), id)
			if err != nil || !exists {
				t.Fatalf("repository existence = %v, error = %v", exists, err)
			}
		})
	}
}
