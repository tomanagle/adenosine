package release

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	gitservice "github.com/adenosine-dev/adenosine/internal/git"
	"github.com/adenosine-dev/adenosine/internal/repository"
	"github.com/google/uuid"
)

type store interface {
	CreateRelease(context.Context, Release) (Release, error)
	GetRelease(context.Context, repository.ID, uuid.UUID) (Release, error)
	PageReleases(context.Context, repository.ID, bool, *uuid.UUID, int) (Page[Release], error)
	UpdateRelease(context.Context, Release) (Release, error)
	BeginDeleteRelease(context.Context, repository.ID, uuid.UUID, time.Time) error
	DeleteRelease(context.Context, repository.ID, uuid.UUID) error
	CreateAsset(context.Context, Asset, Limits) (Asset, error)
	GetAsset(context.Context, repository.ID, uuid.UUID, uuid.UUID) (Asset, error)
	PageAssets(context.Context, repository.ID, uuid.UUID, *uuid.UUID, int) (Page[Asset], error)
	ListAssets(context.Context, repository.ID, uuid.UUID) ([]Asset, error)
	DeleteAsset(context.Context, repository.ID, uuid.UUID, uuid.UUID) error
}

// BlobStore persists immutable release asset bytes behind opaque keys.
type BlobStore interface {
	Put(context.Context, string, io.Reader, int64) (string, error)
	Open(context.Context, string) (io.ReadCloser, error)
	Delete(context.Context, string) error
}

type tagReader interface {
	Tags(context.Context, repository.ID) ([]gitservice.Tag, error)
}

type clock interface{ Now() time.Time }
type idGenerator interface{ New() (uuid.UUID, error) }

type Service struct {
	store  store
	blobs  BlobStore
	tags   tagReader
	clock  clock
	ids    idGenerator
	limits Limits
}

func NewService(store store, blobs BlobStore, tags tagReader, clock clock, ids idGenerator, limits Limits) (*Service, error) {
	if store == nil || blobs == nil || tags == nil || clock == nil || ids == nil {
		return nil, fmt.Errorf("construct release service: dependencies must not be nil")
	}
	if err := limits.Validate(); err != nil {
		return nil, err
	}
	return &Service{store: store, blobs: blobs, tags: tags, clock: clock, ids: ids, limits: limits}, nil
}

func (service *Service) Create(ctx context.Context, repositoryID repository.ID, input CreateInput) (Release, error) {
	if err := input.Validate(); err != nil {
		return Release{}, err
	}
	tags, err := service.tags.Tags(ctx, repositoryID)
	if err != nil {
		return Release{}, fmt.Errorf("list release tag targets: %w", err)
	}
	targetSHA := ""
	for _, tag := range tags {
		if tag.Name == input.TagName {
			targetSHA = tag.PeeledSHA
			break
		}
	}
	if targetSHA == "" {
		return Release{}, fmt.Errorf("%w: tag does not exist", ErrValidation)
	}
	id, err := service.ids.New()
	if err != nil {
		return Release{}, fmt.Errorf("generate release ID: %w", err)
	}
	now := service.clock.Now().UTC()
	state, publishedAt := releaseState(input.Draft, now)
	created, err := service.store.CreateRelease(ctx, Release{
		ID: id, RepositoryID: repositoryID, TagName: input.TagName, TargetSHA: targetSHA,
		Name: input.Name, Body: input.Body, State: state, Prerelease: input.Prerelease,
		CreatedByDID: input.CreatedByDID, CreatedAt: now, UpdatedAt: now, PublishedAt: publishedAt,
	})
	if err != nil {
		return Release{}, fmt.Errorf("create release: %w", err)
	}
	return created, nil
}

func (service *Service) Get(ctx context.Context, repositoryID repository.ID, id uuid.UUID) (Release, error) {
	value, err := service.store.GetRelease(ctx, repositoryID, id)
	if err != nil {
		return Release{}, fmt.Errorf("get release: %w", err)
	}
	if value.State == StateDeleting {
		return Release{}, ErrNotFound
	}
	return value, nil
}

func (service *Service) Page(ctx context.Context, repositoryID repository.ID, includeDrafts bool, after *uuid.UUID, limit int) (Page[Release], error) {
	if limit < 1 || limit > 100 {
		return Page[Release]{}, fmt.Errorf("%w: limit must be between 1 and 100", ErrValidation)
	}
	page, err := service.store.PageReleases(ctx, repositoryID, includeDrafts, after, limit)
	if err != nil {
		return Page[Release]{}, fmt.Errorf("page releases: %w", err)
	}
	if page.Items == nil {
		page.Items = []Release{}
	}
	return page, nil
}

func (service *Service) Update(ctx context.Context, repositoryID repository.ID, id uuid.UUID, input UpdateInput) (Release, error) {
	if err := input.Validate(); err != nil {
		return Release{}, err
	}
	current, err := service.store.GetRelease(ctx, repositoryID, id)
	if err != nil {
		return Release{}, fmt.Errorf("get release for update: %w", err)
	}
	if current.State == StateDeleting {
		return Release{}, ErrNotFound
	}
	now := service.clock.Now().UTC()
	current.Name, current.Body, current.Prerelease, current.UpdatedAt = input.Name, input.Body, input.Prerelease, now
	if input.Draft {
		current.State, current.PublishedAt = StateDraft, nil
	} else {
		if current.State != StatePublished || current.PublishedAt == nil {
			current.PublishedAt = &now
		}
		current.State = StatePublished
	}
	updated, err := service.store.UpdateRelease(ctx, current)
	if err != nil {
		return Release{}, fmt.Errorf("update release: %w", err)
	}
	return updated, nil
}

func (service *Service) Delete(ctx context.Context, repositoryID repository.ID, id uuid.UUID) error {
	if err := service.store.BeginDeleteRelease(ctx, repositoryID, id, service.clock.Now().UTC()); err != nil {
		return fmt.Errorf("begin release deletion: %w", err)
	}
	assets, err := service.store.ListAssets(ctx, repositoryID, id)
	if err != nil {
		return fmt.Errorf("list release assets for deletion: %w", err)
	}
	for _, asset := range assets {
		if err := service.DeleteAsset(ctx, repositoryID, id, asset.ID); err != nil {
			return err
		}
	}
	if err := service.store.DeleteRelease(ctx, repositoryID, id); err != nil {
		return fmt.Errorf("delete release: %w", err)
	}
	return nil
}

func (service *Service) UploadAsset(ctx context.Context, repositoryID repository.ID, releaseID uuid.UUID, input AssetInput) (Asset, error) {
	contentType, err := input.Validate(service.limits.AssetBytes)
	if err != nil {
		return Asset{}, err
	}
	current, err := service.store.GetRelease(ctx, repositoryID, releaseID)
	if err != nil {
		return Asset{}, fmt.Errorf("get release for asset upload: %w", err)
	}
	if current.State == StateDeleting {
		return Asset{}, ErrNotFound
	}
	id, err := service.ids.New()
	if err != nil {
		return Asset{}, fmt.Errorf("generate release asset ID: %w", err)
	}
	storageKey := assetStorageKey(repositoryID, releaseID, id)
	checksum, err := service.blobs.Put(ctx, storageKey, input.Body, input.SizeBytes)
	if err != nil {
		return Asset{}, fmt.Errorf("store release asset: %w", err)
	}
	asset := Asset{ID: id, ReleaseID: releaseID, RepositoryID: repositoryID, Name: input.Name,
		ContentType: contentType, SizeBytes: input.SizeBytes, SHA256: checksum,
		StorageKey: storageKey, CreatedAt: service.clock.Now().UTC()}
	created, err := service.store.CreateAsset(ctx, asset, service.limits)
	if err != nil {
		cleanupErr := service.blobs.Delete(context.WithoutCancel(ctx), storageKey)
		if cleanupErr != nil {
			return Asset{}, fmt.Errorf("create release asset metadata: %w; clean up blob: %v", err, cleanupErr)
		}
		return Asset{}, fmt.Errorf("create release asset metadata: %w", err)
	}
	return created, nil
}

func (service *Service) GetAsset(ctx context.Context, repositoryID repository.ID, releaseID, assetID uuid.UUID) (Asset, error) {
	asset, err := service.store.GetAsset(ctx, repositoryID, releaseID, assetID)
	if err != nil {
		return Asset{}, fmt.Errorf("get release asset: %w", err)
	}
	return asset, nil
}

func (service *Service) PageAssets(ctx context.Context, repositoryID repository.ID, releaseID uuid.UUID, after *uuid.UUID, limit int) (Page[Asset], error) {
	if limit < 1 || limit > 100 {
		return Page[Asset]{}, fmt.Errorf("%w: limit must be between 1 and 100", ErrValidation)
	}
	page, err := service.store.PageAssets(ctx, repositoryID, releaseID, after, limit)
	if err != nil {
		return Page[Asset]{}, fmt.Errorf("page release assets: %w", err)
	}
	if page.Items == nil {
		page.Items = []Asset{}
	}
	return page, nil
}

func (service *Service) OpenAsset(ctx context.Context, asset Asset) (io.ReadCloser, error) {
	reader, err := service.blobs.Open(ctx, asset.StorageKey)
	if err != nil {
		return nil, fmt.Errorf("open release asset: %w", err)
	}
	return reader, nil
}

func (service *Service) DeleteAsset(ctx context.Context, repositoryID repository.ID, releaseID, assetID uuid.UUID) error {
	asset, err := service.store.GetAsset(ctx, repositoryID, releaseID, assetID)
	if err != nil {
		return fmt.Errorf("get release asset for deletion: %w", err)
	}
	if err := service.blobs.Delete(ctx, asset.StorageKey); err != nil {
		return fmt.Errorf("delete release asset blob: %w", err)
	}
	if err := service.store.DeleteAsset(ctx, repositoryID, releaseID, assetID); err != nil {
		return fmt.Errorf("delete release asset metadata: %w", err)
	}
	return nil
}

func releaseState(draft bool, now time.Time) (State, *time.Time) {
	if draft {
		return StateDraft, nil
	}
	return StatePublished, &now
}

func assetStorageKey(repositoryID repository.ID, releaseID, assetID uuid.UUID) string {
	return strings.Join([]string{repositoryID.String(), releaseID.String(), assetID.String()}, "/")
}

type SystemClock struct{}

func (SystemClock) Now() time.Time { return time.Now() }

type UUIDv7Generator struct{}

func (UUIDv7Generator) New() (uuid.UUID, error) { return uuid.NewV7() }

func IsPublic(value Release) bool { return value.State == StatePublished }

func IsExpectedError(err error) bool {
	return errors.Is(err, ErrNotFound) || errors.Is(err, ErrConflict) || errors.Is(err, ErrValidation) ||
		errors.Is(err, ErrQuotaExceeded) || errors.Is(err, ErrSizeMismatch)
}
