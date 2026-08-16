package release

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

type Filesystem struct{ root string }

func NewFilesystem(root string) (*Filesystem, error) {
	absolute, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("resolve release asset root: %w", err)
	}
	if err := os.MkdirAll(absolute, 0o750); err != nil {
		return nil, fmt.Errorf("create release asset root: %w", err)
	}
	canonical, err := filepath.EvalSymlinks(absolute)
	if err != nil {
		return nil, fmt.Errorf("canonicalize release asset root: %w", err)
	}
	return &Filesystem{root: canonical}, nil
}

func (storage *Filesystem) Put(ctx context.Context, key string, source io.Reader, expectedSize int64) (checksum string, returnedErr error) {
	if expectedSize < 0 || source == nil {
		return "", ErrSizeMismatch
	}
	path, err := storage.path(key)
	if err != nil {
		return "", err
	}
	if err := storage.ensureDirectory(filepath.Dir(path)); err != nil {
		return "", err
	}
	temporary, err := os.CreateTemp(filepath.Dir(path), ".upload-*")
	if err != nil {
		return "", fmt.Errorf("create release asset temporary file: %w", err)
	}
	temporaryPath := temporary.Name()
	defer func() {
		if returnedErr != nil {
			_ = temporary.Close()
			_ = os.Remove(temporaryPath)
		}
	}()
	if err := temporary.Chmod(0o640); err != nil {
		return "", fmt.Errorf("protect release asset temporary file: %w", err)
	}
	hash := sha256.New()
	read := &contextReader{ctx: ctx, reader: io.LimitReader(source, expectedSize+1)}
	written, err := io.Copy(io.MultiWriter(temporary, hash), read)
	if err != nil {
		return "", fmt.Errorf("write release asset: %w", err)
	}
	if written != expectedSize {
		return "", ErrSizeMismatch
	}
	if err := temporary.Sync(); err != nil {
		return "", fmt.Errorf("sync release asset: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return "", fmt.Errorf("close release asset: %w", err)
	}
	if _, err := os.Lstat(path); err == nil {
		return "", ErrConflict
	} else if !os.IsNotExist(err) {
		return "", fmt.Errorf("inspect release asset target: %w", err)
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		return "", fmt.Errorf("publish release asset: %w", err)
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func (storage *Filesystem) Open(ctx context.Context, key string) (io.ReadCloser, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	path, err := storage.path(key)
	if err != nil {
		return nil, err
	}
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("inspect release asset: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return nil, fmt.Errorf("release asset is not a physical file")
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open release asset: %w", err)
	}
	return file, nil
}

func (storage *Filesystem) Delete(ctx context.Context, key string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	path, err := storage.path(key)
	if err != nil {
		return err
	}
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("inspect release asset for deletion: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return fmt.Errorf("release asset is not a physical file")
	}
	if err := os.Remove(path); err != nil {
		return fmt.Errorf("delete release asset: %w", err)
	}
	return nil
}

func (storage *Filesystem) path(key string) (string, error) {
	if key == "" || filepath.IsAbs(key) || strings.ContainsRune(key, 0) {
		return "", fmt.Errorf("invalid release asset key")
	}
	clean := filepath.Clean(filepath.FromSlash(key))
	if clean == "." || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("release asset key escapes storage root")
	}
	path := filepath.Join(storage.root, clean)
	relative, err := filepath.Rel(storage.root, path)
	if err != nil || relative == "." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) || filepath.IsAbs(relative) {
		return "", fmt.Errorf("release asset key escapes storage root")
	}
	return path, nil
}

func (storage *Filesystem) ensureDirectory(directory string) error {
	relative, err := filepath.Rel(storage.root, directory)
	if err != nil || relative == "." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) || filepath.IsAbs(relative) {
		return fmt.Errorf("release asset directory escapes storage root")
	}
	current := storage.root
	for _, part := range strings.Split(relative, string(filepath.Separator)) {
		current = filepath.Join(current, part)
		info, statErr := os.Lstat(current)
		if os.IsNotExist(statErr) {
			if err := os.Mkdir(current, 0o750); err != nil && !os.IsExist(err) {
				return fmt.Errorf("create release asset directory: %w", err)
			}
			info, statErr = os.Lstat(current)
		}
		if statErr != nil {
			return fmt.Errorf("inspect release asset directory: %w", statErr)
		}
		if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			return fmt.Errorf("release asset directory is not a physical directory")
		}
	}
	return nil
}

type contextReader struct {
	ctx    context.Context
	reader io.Reader
}

func (reader *contextReader) Read(buffer []byte) (int, error) {
	if err := reader.ctx.Err(); err != nil {
		return 0, err
	}
	return reader.reader.Read(buffer)
}
