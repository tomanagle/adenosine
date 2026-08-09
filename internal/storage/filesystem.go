// Package storage owns physical Git repository placement.
package storage

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/adenosine-dev/adenosine/internal/repository"
)

// Filesystem stores bare repositories beneath one POSIX root.
type Filesystem struct {
	root string
}

// NewFilesystem creates and canonicalizes a repository storage root.
func NewFilesystem(root string) (*Filesystem, error) {
	absolute, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("resolve repository root: %w", err)
	}
	if err := os.MkdirAll(absolute, 0o750); err != nil {
		return nil, fmt.Errorf("create repository root: %w", err)
	}
	canonical, err := filepath.EvalSymlinks(absolute)
	if err != nil {
		return nil, fmt.Errorf("canonicalize repository root: %w", err)
	}
	return &Filesystem{root: canonical}, nil
}

// Path returns the deterministic sharded path for a repository ID.
func (storage *Filesystem) Path(ctx context.Context, id repository.ID) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	compact := strings.ReplaceAll(id.String(), "-", "")
	if len(compact) != 32 {
		return "", fmt.Errorf("invalid repository ID %q", id.String())
	}
	return filepath.Join(storage.root, compact[:2], compact[2:4], id.String()+".git"), nil
}

// Prepare creates the private shard directories and returns the repository path.
func (storage *Filesystem) Prepare(ctx context.Context, id repository.ID) (string, error) {
	path, err := storage.Path(ctx, id)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return "", fmt.Errorf("create repository shard: %w", err)
	}
	return path, nil
}

// Exists reports whether the repository path is an existing directory.
func (storage *Filesystem) Exists(ctx context.Context, id repository.ID) (bool, error) {
	path, err := storage.Path(ctx, id)
	if err != nil {
		return false, err
	}
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("inspect repository path: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return false, fmt.Errorf("repository path is not a directory")
	}
	return true, nil
}
