package federation

import (
	"context"
	"encoding/base64"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"
)

const discoveryTestURI = "at://did:plc:alice/dev.adenosine.repo/project"

type discoveryMemoryStore struct {
	repositories []DiscoveryRepository
	err          error
	limit        int
	cursor       *DiscoveryCursor
	calls        int
}

func (store *discoveryMemoryStore) ListNetworkRepositories(_ context.Context, limit int, cursor *DiscoveryCursor) ([]DiscoveryRepository, error) {
	store.calls++
	store.limit = limit
	store.cursor = cursor
	return store.repositories, store.err
}

func TestDiscoveryCursorRoundTrip(t *testing.T) {
	t.Parallel()
	testCases := []struct {
		name   string
		cursor DiscoveryCursor
		want   DiscoveryCursor
	}{
		{
			name:   "UTC nanoseconds",
			cursor: DiscoveryCursor{IndexedAt: time.Date(2026, 8, 9, 12, 34, 56, 123456789, time.UTC), URI: discoveryTestURI},
			want:   DiscoveryCursor{IndexedAt: time.Date(2026, 8, 9, 12, 34, 56, 123456789, time.UTC), URI: discoveryTestURI},
		},
		{
			name:   "offset normalized to UTC",
			cursor: DiscoveryCursor{IndexedAt: time.Date(2026, 8, 9, 14, 34, 56, 0, time.FixedZone("offset", 2*60*60)), URI: discoveryTestURI},
			want:   DiscoveryCursor{IndexedAt: time.Date(2026, 8, 9, 12, 34, 56, 0, time.UTC), URI: discoveryTestURI},
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			encoded, err := encodeDiscoveryCursor(testCase.cursor)
			if err != nil {
				t.Fatalf("encode cursor: %v", err)
			}
			if strings.Contains(encoded, "=") {
				t.Fatalf("cursor is padded base64url: %q", encoded)
			}
			decoded, err := decodeDiscoveryCursor(encoded)
			if err != nil {
				t.Fatalf("decode cursor: %v", err)
			}
			if !decoded.IndexedAt.Equal(testCase.want.IndexedAt) || decoded.IndexedAt.Location() != time.UTC || decoded.URI != testCase.want.URI {
				t.Fatalf("decoded cursor = %+v, want %+v", decoded, testCase.want)
			}
		})
	}
}

func TestDiscoveryCursorRejectsMalformedValues(t *testing.T) {
	t.Parallel()
	encodePayload := func(value string) string { return base64.RawURLEncoding.EncodeToString([]byte(value)) }
	testCases := []struct {
		name  string
		value string
	}{
		{name: "empty", value: ""},
		{name: "invalid base64url", value: "%%%"},
		{name: "padding", value: base64.URLEncoding.EncodeToString([]byte("v1"))},
		{name: "oversized", value: strings.Repeat("a", maxCursorLength+1)},
		{name: "unsupported version", value: encodePayload("v2\n2026-08-09T12:00:00Z\n" + discoveryTestURI)},
		{name: "missing field", value: encodePayload("v1\n2026-08-09T12:00:00Z")},
		{name: "extra field", value: encodePayload("v1\n2026-08-09T12:00:00Z\n" + discoveryTestURI + "\nextra")},
		{name: "non UTC timestamp", value: encodePayload("v1\n2026-08-09T14:00:00+02:00\n" + discoveryTestURI)},
		{name: "non canonical timestamp", value: encodePayload("v1\n2026-08-09T12:00:00.000Z\n" + discoveryTestURI)},
		{name: "invalid URI", value: encodePayload("v1\n2026-08-09T12:00:00Z\nnot-an-at-uri")},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			_, err := decodeDiscoveryCursor(testCase.value)
			if !errors.Is(err, ErrInvalidCursor) {
				t.Fatalf("error = %v, want ErrInvalidCursor", err)
			}
		})
	}
}

func TestDiscoveryServicePagination(t *testing.T) {
	t.Parallel()
	indexedAt := time.Date(2026, 8, 9, 12, 0, 0, 0, time.UTC)
	cursor, err := encodeDiscoveryCursor(DiscoveryCursor{IndexedAt: indexedAt.Add(time.Minute), URI: discoveryTestURI})
	if err != nil {
		t.Fatal(err)
	}
	testCases := []struct {
		name             string
		limit            int
		cursor           string
		repositories     []DiscoveryRepository
		storeErr         error
		wantErr          error
		wantStoreCalls   int
		wantStoreLimit   int
		wantRepositories int
		wantNextCursor   bool
		wantCursor       bool
	}{
		{name: "default limit and empty non nil page", limit: 0, repositories: nil, wantStoreCalls: 1, wantStoreLimit: 31},
		{name: "minimum limit", limit: 1, repositories: []DiscoveryRepository{{URI: discoveryTestURI, IndexedAt: indexedAt}}, wantStoreCalls: 1, wantStoreLimit: 2, wantRepositories: 1},
		{name: "maximum limit", limit: 100, repositories: []DiscoveryRepository{}, wantStoreCalls: 1, wantStoreLimit: 101},
		{name: "next cursor from last returned row", limit: 1, repositories: []DiscoveryRepository{{URI: discoveryTestURI, IndexedAt: indexedAt}, {URI: "at://did:plc:bob/dev.adenosine.repo/other", IndexedAt: indexedAt.Add(-time.Second)}}, wantStoreCalls: 1, wantStoreLimit: 2, wantRepositories: 1, wantNextCursor: true},
		{name: "validated cursor passed to store", limit: 10, cursor: cursor, repositories: []DiscoveryRepository{}, wantStoreCalls: 1, wantStoreLimit: 11, wantCursor: true},
		{name: "limit below range", limit: -1, wantErr: ErrInvalidLimit},
		{name: "limit above range", limit: 101, wantErr: ErrInvalidLimit},
		{name: "invalid cursor", limit: 10, cursor: "invalid", wantErr: ErrInvalidCursor},
		{name: "store failure wrapped", limit: 10, storeErr: errors.New("database unavailable"), wantStoreCalls: 1, wantStoreLimit: 11, wantErr: errors.New("database unavailable")},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			store := &discoveryMemoryStore{repositories: testCase.repositories, err: testCase.storeErr}
			page, err := NewDiscoveryService(store).ListNetworkRepositories(context.Background(), testCase.limit, testCase.cursor)
			if testCase.wantErr != nil {
				if testCase.storeErr != nil {
					if err == nil || !strings.Contains(err.Error(), testCase.wantErr.Error()) {
						t.Fatalf("error = %v, want wrapped %v", err, testCase.wantErr)
					}
				} else if !errors.Is(err, testCase.wantErr) {
					t.Fatalf("error = %v, want %v", err, testCase.wantErr)
				}
			} else if err != nil {
				t.Fatalf("list repositories: %v", err)
			}
			if store.calls != testCase.wantStoreCalls || store.limit != testCase.wantStoreLimit {
				t.Fatalf("store calls/limit = %d/%d, want %d/%d", store.calls, store.limit, testCase.wantStoreCalls, testCase.wantStoreLimit)
			}
			if testCase.wantErr != nil {
				return
			}
			if page.Repositories == nil || len(page.Repositories) != testCase.wantRepositories {
				t.Fatalf("repositories = %#v, want non-nil length %d", page.Repositories, testCase.wantRepositories)
			}
			if (page.NextCursor != nil) != testCase.wantNextCursor || (store.cursor != nil) != testCase.wantCursor {
				t.Fatalf("next/store cursor presence = %t/%t, want %t/%t", page.NextCursor != nil, store.cursor != nil, testCase.wantNextCursor, testCase.wantCursor)
			}
			if page.NextCursor != nil {
				decoded, decodeErr := decodeDiscoveryCursor(*page.NextCursor)
				want := DiscoveryCursor{IndexedAt: indexedAt, URI: discoveryTestURI}
				if decodeErr != nil || !reflect.DeepEqual(decoded, want) {
					t.Fatalf("next cursor = %+v, %v, want %+v", decoded, decodeErr, want)
				}
			}
		})
	}
}
