package release

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFilesystemRoundTrip(t *testing.T) {
	t.Parallel()
	testCases := []struct {
		name         string
		body         string
		expectedSize int64
		wantErr      error
	}{
		{name: "exact body", body: "release bytes", expectedSize: 13},
		{name: "body too short", body: "short", expectedSize: 6, wantErr: ErrSizeMismatch},
		{name: "body too long", body: "too long", expectedSize: 3, wantErr: ErrSizeMismatch},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			storage, err := NewFilesystem(t.TempDir())
			if err != nil {
				t.Fatalf("NewFilesystem(): %v", err)
			}
			checksum, err := storage.Put(context.Background(), "repo/release/asset", strings.NewReader(testCase.body), testCase.expectedSize)
			if !errors.Is(err, testCase.wantErr) {
				t.Fatalf("Put() error = %v, want %v", err, testCase.wantErr)
			}
			if testCase.wantErr != nil {
				return
			}
			if checksum != "ff7a5e6429d2c8511521e4abf41cd54a3e525ef4a1f24f8d1c67ede9d17874dd" {
				t.Fatalf("checksum = %q", checksum)
			}
			reader, err := storage.Open(context.Background(), "repo/release/asset")
			if err != nil {
				t.Fatalf("Open(): %v", err)
			}
			body, err := io.ReadAll(reader)
			_ = reader.Close()
			if err != nil || string(body) != testCase.body {
				t.Fatalf("read body = %q, error = %v", body, err)
			}
			if err := storage.Delete(context.Background(), "repo/release/asset"); err != nil {
				t.Fatalf("Delete(): %v", err)
			}
			if _, err := storage.Open(context.Background(), "repo/release/asset"); !errors.Is(err, ErrNotFound) {
				t.Fatalf("Open() after Delete error = %v, want %v", err, ErrNotFound)
			}
		})
	}
}

func TestFilesystemRejectsUnsafePaths(t *testing.T) {
	t.Parallel()
	testCases := []struct{ name, key string }{
		{name: "empty", key: ""},
		{name: "parent", key: "../asset"},
		{name: "absolute", key: "/asset"},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			storage, err := NewFilesystem(t.TempDir())
			if err != nil {
				t.Fatalf("NewFilesystem(): %v", err)
			}
			if _, err := storage.Put(context.Background(), testCase.key, strings.NewReader("x"), 1); err == nil {
				t.Fatal("Put() accepted unsafe key")
			}
		})
	}
}

func TestFilesystemRejectsSymlinkDirectory(t *testing.T) {
	t.Parallel()
	testCases := []struct{ name string }{{name: "symlinked shard"}}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			root, outside := t.TempDir(), t.TempDir()
			storage, err := NewFilesystem(root)
			if err != nil {
				t.Fatalf("NewFilesystem(): %v", err)
			}
			if err := os.Symlink(outside, filepath.Join(root, "repo")); err != nil {
				t.Fatalf("Symlink(): %v", err)
			}
			if _, err := storage.Put(context.Background(), "repo/release/asset", strings.NewReader("x"), 1); err == nil {
				t.Fatal("Put() followed symlinked directory")
			}
		})
	}
}
