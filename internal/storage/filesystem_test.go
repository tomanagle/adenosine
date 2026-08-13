package storage

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/adenosine-dev/adenosine/internal/repository"
	"github.com/google/uuid"
)

func TestFilesystemPathUsesStableShards(t *testing.T) {
	t.Parallel()
	testCases := []struct{ name string }{{name: "stable shards"}}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			root := t.TempDir()
			storage, err := NewFilesystem(root)
			if err != nil {
				t.Fatalf("create filesystem storage: %v", err)
			}
			id := repository.ID(uuid.MustParse("4a19a8c2-0315-7f2b-9431-a3d1b542ee91"))

			path, err := storage.Path(context.Background(), id)
			if err != nil {
				t.Fatalf("resolve path: %v", err)
			}
			want := filepath.Join(storage.root, "4a", "19", "4a19a8c2-0315-7f2b-9431-a3d1b542ee91.git")
			if path != want {
				t.Fatalf("path = %q, want %q", path, want)
			}

			prepared, err := storage.Prepare(context.Background(), id)
			if err != nil {
				t.Fatalf("prepare path: %v", err)
			}
			if prepared != want {
				t.Fatalf("prepared path = %q, want %q", prepared, want)
			}
			exists, err := storage.Exists(context.Background(), id)
			if err != nil {
				t.Fatalf("check existence: %v", err)
			}
			if exists {
				t.Fatal("repository should not exist before Git initialization")
			}
		})
	}
}

func TestFilesystemQuarantineLifecycle(t *testing.T) {
	t.Parallel()
	testCases := []struct{ name string }{{name: "quarantine restore and purge"}}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			storage, err := NewFilesystem(t.TempDir())
			if err != nil {
				t.Fatalf("NewFilesystem(): %v", err)
			}
			id := repository.ID(uuid.MustParse("4a19a8c2-0315-7f2b-9431-a3d1b542ee91"))
			path, err := storage.Prepare(context.Background(), id)
			if err != nil {
				t.Fatalf("Prepare(): %v", err)
			}
			if err := os.Mkdir(path, 0o750); err != nil {
				t.Fatalf("create repository: %v", err)
			}
			if err := storage.Quarantine(context.Background(), id); err != nil {
				t.Fatalf("Quarantine(): %v", err)
			}
			if exists, _ := storage.Exists(context.Background(), id); exists {
				t.Fatal("live repository exists after quarantine")
			}
			if err := storage.Restore(context.Background(), id); err != nil {
				t.Fatalf("Restore(): %v", err)
			}
			if exists, _ := storage.Exists(context.Background(), id); !exists {
				t.Fatal("live repository missing after restore")
			}
			if err := storage.Quarantine(context.Background(), id); err != nil {
				t.Fatalf("second Quarantine(): %v", err)
			}
			if err := storage.Purge(context.Background(), id); err != nil {
				t.Fatalf("Purge(): %v", err)
			}
			if err := storage.Purge(context.Background(), id); err != nil {
				t.Fatalf("idempotent Purge(): %v", err)
			}
		})
	}
}
