package release

import (
	"context"
	"fmt"
)

const (
	BackendFilesystem = "filesystem"
	BackendS3         = "s3"
)

// BlobStoreConfig selects the process-wide release asset backend.
type BlobStoreConfig struct {
	Backend        string
	FilesystemRoot string
	S3             S3Config
}

// MustBlobStore constructs the required release asset backend or panics during startup.
func MustBlobStore(ctx context.Context, cfg BlobStoreConfig) BlobStore {
	value, err := buildBlobStore(ctx, cfg)
	if err != nil {
		panic(err)
	}
	return value
}

func buildBlobStore(ctx context.Context, cfg BlobStoreConfig) (BlobStore, error) {
	switch cfg.Backend {
	case BackendFilesystem:
		value, err := NewFilesystem(cfg.FilesystemRoot)
		if err != nil {
			return nil, fmt.Errorf("open filesystem release asset storage: %w", err)
		}
		return value, nil
	case BackendS3:
		value, err := NewS3(ctx, cfg.S3)
		if err != nil {
			return nil, fmt.Errorf("open S3 release asset storage: %w", err)
		}
		return value, nil
	default:
		return nil, fmt.Errorf("open release asset storage: unsupported backend %q", cfg.Backend)
	}
}
