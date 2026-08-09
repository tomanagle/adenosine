package profile

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// Remote reads and writes the authenticated DID's canonical self profile record.
type Remote interface {
	Get(context.Context, string) (Profile, error)
	Put(context.Context, string, Record) (Profile, error)
}

type projectionStore interface {
	Get(context.Context, string) (Profile, error)
}

type clock interface{ Now() time.Time }

// Service coordinates the canonical profile record with a refreshable local projection.
type Service struct {
	profiles projectionStore
	remote   Remote
	clock    clock
}

// NewService constructs the shared profile service.
func NewService(profiles projectionStore, remote Remote, clock clock) *Service {
	return &Service{profiles: profiles, remote: remote, clock: clock}
}

// Get returns the locally indexed network projection.
func (service *Service) Get(ctx context.Context, did string) (Profile, error) {
	did, err := validateDID(did)
	if err != nil {
		return Profile{}, err
	}
	projected, err := service.profiles.Get(ctx, did)
	if errors.Is(err, ErrNotFound) {
		return Profile{}, &NotFoundError{DID: did}
	}
	if err != nil {
		return Profile{}, fmt.Errorf("get profile projection: %w", err)
	}
	return projected, nil
}

// Update publishes and projects a profile for the authenticated DID.
func (service *Service) Update(ctx context.Context, authenticatedDID string, input UpdateInput) (Profile, error) {
	did, err := validateDID(authenticatedDID)
	if err != nil {
		return Profile{}, err
	}
	if err := input.Validate(); err != nil {
		return Profile{}, err
	}
	now := service.clock.Now().UTC()
	createdAt := now
	existing, err := service.remote.Get(ctx, did)
	if err == nil {
		if err := validateProviderProfile(existing, did); err != nil {
			return Profile{}, &ProviderError{Operation: "get before update", Err: err}
		}
		createdAt = existing.RecordCreatedAt
	} else if !errors.Is(err, ErrNotFound) {
		return Profile{}, &ProviderError{Operation: "get before update", Err: err}
	}
	remoteProfile, err := service.remote.Put(ctx, did, Record{
		DisplayName: input.DisplayName,
		Bio:         input.Bio,
		Website:     input.Website,
		Location:    input.Location,
		CreatedAt:   createdAt,
	})
	if err != nil {
		return Profile{}, &ProviderError{Operation: "put", Err: err}
	}
	if err := validateProviderProfile(remoteProfile, did); err != nil {
		return Profile{}, &ProviderError{Operation: "put", Err: err}
	}
	remoteProfile.IndexedAt = now
	return remoteProfile, nil
}
