package federation

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/google/uuid"
)

const (
	defaultDiscoveryLimit = 30
	maxDiscoveryLimit     = 100
	maxCursorLength       = 4096
	cursorVersion         = "v1"
)

var (
	// ErrInvalidCursor is returned for every unsupported or malformed discovery cursor.
	ErrInvalidCursor = errors.New("invalid network repository cursor")
	// ErrInvalidLimit indicates that a discovery page size is outside the public bounds.
	ErrInvalidLimit = errors.New("network repository limit must be between 1 and 100")
)

// DiscoveryRepository is public repository metadata projected from the network index.
type DiscoveryRepository struct {
	URI               string
	CID               string
	LocalRepositoryID *uuid.UUID
	OwnerDID          string
	OwnerHandle       string
	Slug              string
	Name              string
	Description       string
	DefaultBranch     string
	GitHTTPS          string
	GitSSH            string
	Web               string
	CreatedAt         time.Time
	UpdatedAt         time.Time
	IndexedAt         time.Time
	StarCount         int64
	IssueCount        int64
	OpenIssueCount    int64
}

// DiscoveryPage contains one stable page of network repositories.
type DiscoveryPage struct {
	Repositories []DiscoveryRepository
	NextCursor   *string
}

// DiscoveryStore lists projected repositories after an optional validated keyset.
type DiscoveryStore interface {
	ListNetworkRepositories(context.Context, int, *DiscoveryCursor) ([]DiscoveryRepository, error)
}

// DiscoveryCursor is the validated position passed to a discovery store.
type DiscoveryCursor struct {
	IndexedAt time.Time
	URI       string
}

// DiscoveryService provides bounded keyset pagination over the local network index.
type DiscoveryService struct {
	store DiscoveryStore
}

// NewDiscoveryService constructs the network repository discovery service.
func NewDiscoveryService(store DiscoveryStore) *DiscoveryService {
	return &DiscoveryService{store: store}
}

// ListNetworkRepositories returns a page without contacting remote services.
func (service *DiscoveryService) ListNetworkRepositories(ctx context.Context, limit int, encodedCursor string) (DiscoveryPage, error) {
	if limit == 0 {
		limit = defaultDiscoveryLimit
	}
	if limit < 1 || limit > maxDiscoveryLimit {
		return DiscoveryPage{}, ErrInvalidLimit
	}

	var cursor *DiscoveryCursor
	if encodedCursor != "" {
		decoded, err := decodeDiscoveryCursor(encodedCursor)
		if err != nil {
			return DiscoveryPage{}, err
		}
		cursor = &decoded
	}
	repositories, err := service.store.ListNetworkRepositories(ctx, limit+1, cursor)
	if err != nil {
		return DiscoveryPage{}, fmt.Errorf("list network repositories: %w", err)
	}
	if repositories == nil {
		repositories = []DiscoveryRepository{}
	}

	page := DiscoveryPage{Repositories: repositories}
	if len(repositories) > limit {
		page.Repositories = repositories[:limit]
		last := page.Repositories[len(page.Repositories)-1]
		nextCursor, err := encodeDiscoveryCursor(DiscoveryCursor{IndexedAt: last.IndexedAt, URI: last.URI})
		if err != nil {
			return DiscoveryPage{}, fmt.Errorf("encode network repository cursor: %w", err)
		}
		page.NextCursor = &nextCursor
	}
	return page, nil
}

func encodeDiscoveryCursor(cursor DiscoveryCursor) (string, error) {
	indexedAt := cursor.IndexedAt.UTC()
	if cursor.IndexedAt.IsZero() || !canonicalATURI(cursor.URI) {
		return "", ErrInvalidCursor
	}
	payload := strings.Join([]string{cursorVersion, indexedAt.Format(time.RFC3339Nano), cursor.URI}, "\n")
	encoded := base64.RawURLEncoding.EncodeToString([]byte(payload))
	if len(encoded) > maxCursorLength {
		return "", ErrInvalidCursor
	}
	return encoded, nil
}

func decodeDiscoveryCursor(encoded string) (DiscoveryCursor, error) {
	if encoded == "" || len(encoded) > maxCursorLength {
		return DiscoveryCursor{}, ErrInvalidCursor
	}
	payload, err := base64.RawURLEncoding.Strict().DecodeString(encoded)
	if err != nil || len(payload) > maxCursorLength {
		return DiscoveryCursor{}, ErrInvalidCursor
	}
	parts := strings.Split(string(payload), "\n")
	if len(parts) != 3 || parts[0] != cursorVersion {
		return DiscoveryCursor{}, ErrInvalidCursor
	}
	indexedAt, err := time.Parse(time.RFC3339Nano, parts[1])
	if err != nil || !strings.HasSuffix(parts[1], "Z") || parts[1] != indexedAt.UTC().Format(time.RFC3339Nano) {
		return DiscoveryCursor{}, ErrInvalidCursor
	}
	if !canonicalATURI(parts[2]) {
		return DiscoveryCursor{}, ErrInvalidCursor
	}
	return DiscoveryCursor{IndexedAt: indexedAt.UTC(), URI: parts[2]}, nil
}

func canonicalATURI(value string) bool {
	uri, err := syntax.ParseATURI(value)
	return err == nil && uri.String() == value
}
