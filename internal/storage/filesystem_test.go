package storage

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/adenosine-dev/adenosine/internal/repository"
	"github.com/google/uuid"
)

func TestFilesystemPathUsesStableShards(t *testing.T) {
	t.Parallel()
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
}
