package release

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	gitservice "github.com/adenosine-dev/adenosine/internal/git"
	"github.com/adenosine-dev/adenosine/internal/repository"
	"github.com/google/uuid"
)

type memoryStore struct {
	releases       map[uuid.UUID]Release
	assets         map[uuid.UUID]Asset
	createAssetErr error
}

func newMemoryStore() *memoryStore {
	return &memoryStore{releases: map[uuid.UUID]Release{}, assets: map[uuid.UUID]Asset{}}
}
func (store *memoryStore) CreateRelease(_ context.Context, value Release) (Release, error) {
	for _, existing := range store.releases {
		if existing.RepositoryID == value.RepositoryID && existing.TagName == value.TagName {
			return Release{}, ErrConflict
		}
	}
	store.releases[value.ID] = value
	return value, nil
}
func (store *memoryStore) GetRelease(_ context.Context, repositoryID repository.ID, id uuid.UUID) (Release, error) {
	value, ok := store.releases[id]
	if !ok || value.RepositoryID != repositoryID {
		return Release{}, ErrNotFound
	}
	return value, nil
}
func (store *memoryStore) PageReleases(_ context.Context, repositoryID repository.ID, includeDrafts bool, _ *uuid.UUID, limit int) (Page[Release], error) {
	page := Page[Release]{Items: []Release{}}
	for _, value := range store.releases {
		if value.RepositoryID == repositoryID && (includeDrafts || value.State == StatePublished) {
			page.Items = append(page.Items, value)
		}
	}
	if len(page.Items) > limit {
		page.Items = page.Items[:limit]
	}
	return page, nil
}
func (store *memoryStore) UpdateRelease(_ context.Context, value Release) (Release, error) {
	store.releases[value.ID] = value
	return value, nil
}
func (store *memoryStore) BeginDeleteRelease(_ context.Context, repositoryID repository.ID, id uuid.UUID, now time.Time) error {
	value, err := store.GetRelease(context.Background(), repositoryID, id)
	if err != nil {
		return err
	}
	value.State, value.UpdatedAt = StateDeleting, now
	store.releases[id] = value
	return nil
}
func (store *memoryStore) DeleteRelease(_ context.Context, repositoryID repository.ID, id uuid.UUID) error {
	if _, err := store.GetRelease(context.Background(), repositoryID, id); err != nil {
		return err
	}
	delete(store.releases, id)
	return nil
}
func (store *memoryStore) CreateAsset(_ context.Context, value Asset, _ Limits) (Asset, error) {
	if store.createAssetErr != nil {
		return Asset{}, store.createAssetErr
	}
	store.assets[value.ID] = value
	return value, nil
}
func (store *memoryStore) GetAsset(_ context.Context, repositoryID repository.ID, releaseID, id uuid.UUID) (Asset, error) {
	value, ok := store.assets[id]
	if !ok || value.RepositoryID != repositoryID || value.ReleaseID != releaseID {
		return Asset{}, ErrNotFound
	}
	return value, nil
}
func (store *memoryStore) PageAssets(_ context.Context, repositoryID repository.ID, releaseID uuid.UUID, _ *uuid.UUID, limit int) (Page[Asset], error) {
	page := Page[Asset]{Items: []Asset{}}
	for _, value := range store.assets {
		if value.RepositoryID == repositoryID && value.ReleaseID == releaseID {
			page.Items = append(page.Items, value)
		}
	}
	if len(page.Items) > limit {
		page.Items = page.Items[:limit]
	}
	return page, nil
}
func (store *memoryStore) ListAssets(_ context.Context, repositoryID repository.ID, releaseID uuid.UUID) ([]Asset, error) {
	items := []Asset{}
	for _, value := range store.assets {
		if value.RepositoryID == repositoryID && value.ReleaseID == releaseID {
			items = append(items, value)
		}
	}
	return items, nil
}
func (store *memoryStore) DeleteAsset(_ context.Context, repositoryID repository.ID, releaseID, id uuid.UUID) error {
	if _, err := store.GetAsset(context.Background(), repositoryID, releaseID, id); err != nil {
		return err
	}
	delete(store.assets, id)
	return nil
}

type memoryBlobs struct {
	values    map[string][]byte
	deleted   []string
	deleteErr error
}

func (blobs *memoryBlobs) Put(_ context.Context, key string, reader io.Reader, expected int64) (string, error) {
	body, err := io.ReadAll(reader)
	if err != nil {
		return "", err
	}
	if int64(len(body)) != expected {
		return "", ErrSizeMismatch
	}
	blobs.values[key] = body
	return "3a6eb0790f39ac87c94f3856b2dd2c5d110e6811602261a9a923d3bb23adc8b7", nil
}
func (blobs *memoryBlobs) Open(_ context.Context, key string) (io.ReadCloser, error) {
	body, ok := blobs.values[key]
	if !ok {
		return nil, ErrNotFound
	}
	return io.NopCloser(bytes.NewReader(body)), nil
}
func (blobs *memoryBlobs) Delete(_ context.Context, key string) error {
	if blobs.deleteErr != nil {
		return blobs.deleteErr
	}
	delete(blobs.values, key)
	blobs.deleted = append(blobs.deleted, key)
	return nil
}

func TestServiceDeleteResumesAfterBlobFailure(t *testing.T) {
	t.Parallel()
	testCases := []struct{ name string }{{name: "deleting marker blocks exposure until retry completes"}}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			repositoryID := repository.ID(uuid.MustParse("0198aaaa-0000-7000-8000-000000000010"))
			releaseID := uuid.MustParse("0198aaaa-0000-7000-8000-000000000001")
			assetID := uuid.MustParse("0198aaaa-0000-7000-8000-000000000002")
			key := assetStorageKey(repositoryID, releaseID, assetID)
			store := newMemoryStore()
			store.releases[releaseID] = Release{ID: releaseID, RepositoryID: repositoryID, State: StatePublished}
			store.assets[assetID] = Asset{ID: assetID, ReleaseID: releaseID, RepositoryID: repositoryID, StorageKey: key}
			blobs := &memoryBlobs{values: map[string][]byte{key: []byte("data")}, deleteErr: errors.New("disk unavailable")}
			service, err := NewService(store, blobs, fixedTags{}, fixedClock(time.Now()), &sequenceIDs{}, Limits{AssetBytes: 10, ReleaseBytes: 20, RepositoryBytes: 30})
			if err != nil {
				t.Fatalf("NewService(): %v", err)
			}
			if err := service.Delete(context.Background(), repositoryID, releaseID); err == nil {
				t.Fatal("Delete() succeeded while blob storage failed")
			}
			if _, err := service.Get(context.Background(), repositoryID, releaseID); !errors.Is(err, ErrNotFound) {
				t.Fatalf("Get() error during deletion = %v, want %v", err, ErrNotFound)
			}
			blobs.deleteErr = nil
			if err := service.Delete(context.Background(), repositoryID, releaseID); err != nil {
				t.Fatalf("retry Delete(): %v", err)
			}
			if _, ok := store.releases[releaseID]; ok {
				t.Fatal("release remains after successful retry")
			}
		})
	}
}

type fixedTags []gitservice.Tag

func (tags fixedTags) Tags(context.Context, repository.ID) ([]gitservice.Tag, error) {
	return tags, nil
}

type fixedClock time.Time

func (clock fixedClock) Now() time.Time { return time.Time(clock) }

type sequenceIDs struct {
	values []uuid.UUID
	index  int
}

func (ids *sequenceIDs) New() (uuid.UUID, error) {
	value := ids.values[ids.index]
	ids.index++
	return value, nil
}

func TestServiceReleaseLifecycle(t *testing.T) {
	t.Parallel()
	testCases := []struct {
		name       string
		tags       fixedTags
		input      CreateInput
		wantErr    error
		wantState  State
		wantTarget string
	}{
		{name: "published release snapshots peeled target", tags: fixedTags{{Name: "v1", PeeledSHA: strings.Repeat("a", 40)}}, input: CreateInput{TagName: "v1", Name: "Version 1", CreatedByDID: "did:plc:alice"}, wantState: StatePublished, wantTarget: strings.Repeat("a", 40)},
		{name: "draft release", tags: fixedTags{{Name: "v2", PeeledSHA: strings.Repeat("b", 40)}}, input: CreateInput{TagName: "v2", Name: "Version 2", Draft: true, CreatedByDID: "did:plc:alice"}, wantState: StateDraft, wantTarget: strings.Repeat("b", 40)},
		{name: "missing tag", tags: fixedTags{}, input: CreateInput{TagName: "missing", Name: "Missing", CreatedByDID: "did:plc:alice"}, wantErr: ErrValidation},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			store, blobs := newMemoryStore(), &memoryBlobs{values: map[string][]byte{}}
			ids := &sequenceIDs{values: []uuid.UUID{uuid.MustParse("0198aaaa-0000-7000-8000-000000000001")}}
			service, err := NewService(store, blobs, testCase.tags, fixedClock(time.Date(2026, 8, 16, 1, 2, 3, 0, time.UTC)), ids, Limits{AssetBytes: 10, ReleaseBytes: 20, RepositoryBytes: 30})
			if err != nil {
				t.Fatalf("NewService(): %v", err)
			}
			created, err := service.Create(context.Background(), repository.ID(uuid.MustParse("0198aaaa-0000-7000-8000-000000000010")), testCase.input)
			if !errors.Is(err, testCase.wantErr) {
				t.Fatalf("Create() error = %v, want %v", err, testCase.wantErr)
			}
			if testCase.wantErr != nil {
				return
			}
			if created.State != testCase.wantState || created.TargetSHA != testCase.wantTarget {
				t.Fatalf("release = %#v", created)
			}
			if (created.PublishedAt == nil) != (created.State == StateDraft) {
				t.Fatalf("PublishedAt = %v for state %q", created.PublishedAt, created.State)
			}
			updated, err := service.Update(context.Background(), created.RepositoryID, created.ID, UpdateInput{Name: "Updated", Body: "notes", Draft: false, Prerelease: true})
			if err != nil || updated.State != StatePublished || updated.PublishedAt == nil || !updated.Prerelease {
				t.Fatalf("Update() = %#v, %v", updated, err)
			}
		})
	}
}

func TestServiceUpdatePublicationTime(t *testing.T) {
	t.Parallel()
	publishedAt := time.Date(2026, 8, 15, 1, 2, 3, 0, time.UTC)
	updatedAt := time.Date(2026, 8, 16, 1, 2, 3, 0, time.UTC)
	testCases := []struct {
		name             string
		currentState     State
		currentPublished *time.Time
		draft            bool
		wantPublished    *time.Time
	}{
		{name: "published edit preserves publication time", currentState: StatePublished, currentPublished: &publishedAt, wantPublished: &publishedAt},
		{name: "publishing a draft records publication time", currentState: StateDraft, wantPublished: &updatedAt},
		{name: "returning to draft clears publication time", currentState: StatePublished, currentPublished: &publishedAt, draft: true},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			repositoryID := repository.ID(uuid.MustParse("0198aaaa-0000-7000-8000-000000000010"))
			releaseID := uuid.MustParse("0198aaaa-0000-7000-8000-000000000001")
			store := newMemoryStore()
			store.releases[releaseID] = Release{ID: releaseID, RepositoryID: repositoryID, State: testCase.currentState, PublishedAt: testCase.currentPublished}
			service, err := NewService(store, &memoryBlobs{values: map[string][]byte{}}, fixedTags{}, fixedClock(updatedAt), &sequenceIDs{}, Limits{AssetBytes: 10, ReleaseBytes: 20, RepositoryBytes: 30})
			if err != nil {
				t.Fatalf("NewService(): %v", err)
			}
			updated, err := service.Update(context.Background(), repositoryID, releaseID, UpdateInput{Name: "Updated", Draft: testCase.draft})
			if err != nil {
				t.Fatalf("Update(): %v", err)
			}
			if testCase.wantPublished == nil {
				if updated.PublishedAt != nil {
					t.Fatalf("PublishedAt = %v, want nil", updated.PublishedAt)
				}
				return
			}
			if updated.PublishedAt == nil || !updated.PublishedAt.Equal(*testCase.wantPublished) {
				t.Fatalf("PublishedAt = %v, want %v", updated.PublishedAt, testCase.wantPublished)
			}
		})
	}
}

func TestServiceAssetLifecycle(t *testing.T) {
	t.Parallel()
	testCases := []struct {
		name           string
		createAssetErr error
		wantErr        error
		wantStored     bool
	}{
		{name: "upload and delete", wantStored: true},
		{name: "quota rollback deletes blob", createAssetErr: ErrQuotaExceeded, wantErr: ErrQuotaExceeded},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			repositoryID := repository.ID(uuid.MustParse("0198aaaa-0000-7000-8000-000000000010"))
			releaseID := uuid.MustParse("0198aaaa-0000-7000-8000-000000000001")
			assetID := uuid.MustParse("0198aaaa-0000-7000-8000-000000000002")
			store := newMemoryStore()
			store.createAssetErr = testCase.createAssetErr
			store.releases[releaseID] = Release{ID: releaseID, RepositoryID: repositoryID}
			blobs := &memoryBlobs{values: map[string][]byte{}}
			ids := &sequenceIDs{values: []uuid.UUID{assetID}}
			service, err := NewService(store, blobs, fixedTags{}, fixedClock(time.Now()), ids, Limits{AssetBytes: 10, ReleaseBytes: 20, RepositoryBytes: 30})
			if err != nil {
				t.Fatalf("NewService(): %v", err)
			}
			asset, err := service.UploadAsset(context.Background(), repositoryID, releaseID, AssetInput{Name: "asset.bin", ContentType: "application/octet-stream", SizeBytes: 4, Body: strings.NewReader("data")})
			if !errors.Is(err, testCase.wantErr) {
				t.Fatalf("UploadAsset() error = %v, want %v", err, testCase.wantErr)
			}
			key := assetStorageKey(repositoryID, releaseID, assetID)
			_, stored := blobs.values[key]
			if stored != testCase.wantStored {
				t.Fatalf("blob stored = %t, want %t", stored, testCase.wantStored)
			}
			if testCase.wantErr != nil {
				if len(blobs.deleted) != 1 {
					t.Fatalf("deleted keys = %v", blobs.deleted)
				}
				return
			}
			if asset.SHA256 == "" {
				t.Fatal("asset checksum is empty")
			}
			if err := service.DeleteAsset(context.Background(), repositoryID, releaseID, assetID); err != nil {
				t.Fatalf("DeleteAsset(): %v", err)
			}
			if _, ok := blobs.values[key]; ok {
				t.Fatal("blob remains after DeleteAsset")
			}
		})
	}
}
