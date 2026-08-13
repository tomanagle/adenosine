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

// Quarantine atomically moves a repository out of the live Git namespace.
func (storage *Filesystem) Quarantine(ctx context.Context, id repository.ID) error {
	live, quarantined, err := storage.lifecyclePaths(ctx, id)
	if err != nil {
		return err
	}
	if err := requireDirectory(live); err != nil {
		return fmt.Errorf("inspect live repository: %w", err)
	}
	if _, err := os.Lstat(quarantined); err == nil {
		return fmt.Errorf("quarantine repository target already exists")
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("inspect quarantine target: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(quarantined), 0o750); err != nil {
		return fmt.Errorf("create quarantine shard: %w", err)
	}
	if err := os.Rename(live, quarantined); err != nil {
		return fmt.Errorf("move repository to quarantine: %w", err)
	}
	return nil
}

// Restore atomically returns a quarantined repository to its live path.
func (storage *Filesystem) Restore(ctx context.Context, id repository.ID) error {
	live, quarantined, err := storage.lifecyclePaths(ctx, id)
	if err != nil {
		return err
	}
	if err := requireDirectory(quarantined); err != nil {
		return fmt.Errorf("inspect quarantined repository: %w", err)
	}
	if _, err := os.Lstat(live); err == nil {
		return fmt.Errorf("live repository target already exists")
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("inspect live repository target: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(live), 0o750); err != nil {
		return fmt.Errorf("create live repository shard: %w", err)
	}
	if err := os.Rename(quarantined, live); err != nil {
		return fmt.Errorf("restore repository from quarantine: %w", err)
	}
	return nil
}

// Purge removes one validated quarantined repository after its retention deadline.
func (storage *Filesystem) Purge(ctx context.Context, id repository.ID) error {
	_, quarantined, err := storage.lifecyclePaths(ctx, id)
	if err != nil {
		return err
	}
	if err := requireDirectory(quarantined); os.IsNotExist(err) {
		return nil
	} else if err != nil {
		return fmt.Errorf("inspect quarantined repository: %w", err)
	}
	if err := os.RemoveAll(quarantined); err != nil {
		return fmt.Errorf("purge quarantined repository: %w", err)
	}
	return nil
}

func (storage *Filesystem) lifecyclePaths(ctx context.Context, id repository.ID) (string, string, error) {
	live, err := storage.Path(ctx, id)
	if err != nil {
		return "", "", err
	}
	compact := strings.ReplaceAll(id.String(), "-", "")
	quarantined := filepath.Join(storage.root, ".quarantine", compact[:2], compact[2:4], id.String()+".git")
	for _, candidate := range []string{live, quarantined} {
		relative, relErr := filepath.Rel(storage.root, candidate)
		if relErr != nil || relative == "." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) || filepath.IsAbs(relative) {
			return "", "", fmt.Errorf("repository lifecycle path escapes storage root")
		}
	}
	return live, quarantined, nil
}

func requireDirectory(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return fmt.Errorf("path is not a physical directory")
	}
	return nil
}
