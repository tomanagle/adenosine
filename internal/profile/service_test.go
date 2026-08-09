package profile

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"
)

const testDID = "did:plc:abcdefghijklmnopqrstuvwx"

type fakeStore struct {
	profile Profile
	err     error
}

func (store *fakeStore) Get(context.Context, string) (Profile, error) {
	if store.err != nil {
		return Profile{}, store.err
	}
	if store.profile.DID == "" {
		return Profile{}, ErrNotFound
	}
	return store.profile, nil
}

func (store *fakeStore) Upsert(_ context.Context, profile Profile) (Profile, error) {
	profile.RepositoryCount = store.profile.RepositoryCount
	profile.ContributionCount = store.profile.ContributionCount
	store.profile = profile
	return profile, nil
}

type sharedRemote struct {
	mu       sync.Mutex
	profile  Profile
	err      error
	getCalls int
}

func (remote *sharedRemote) Get(context.Context, string) (Profile, error) {
	remote.mu.Lock()
	defer remote.mu.Unlock()
	remote.getCalls++
	if remote.err != nil {
		return Profile{}, remote.err
	}
	if remote.profile.DID == "" {
		return Profile{}, ErrNotFound
	}
	return remote.profile, nil
}

func (remote *sharedRemote) Put(_ context.Context, did string, record Record) (Profile, error) {
	remote.mu.Lock()
	defer remote.mu.Unlock()
	if remote.err != nil {
		return Profile{}, remote.err
	}
	digest := sha256.Sum256([]byte(fmt.Sprintf("%s\x00%s\x00%s\x00%s\x00%s\x00%s", did, record.DisplayName, record.Bio, record.Website, record.Location, record.CreatedAt.Format(time.RFC3339Nano))))
	remote.profile = Profile{
		DID:             did,
		URI:             "at://" + did + "/dev.adenosine.profile/self",
		CID:             fmt.Sprintf("bafk%x", digest[:12]),
		DisplayName:     record.DisplayName,
		Bio:             record.Bio,
		Website:         record.Website,
		Location:        record.Location,
		RecordCreatedAt: record.CreatedAt,
	}
	return remote.profile, nil
}

type fixedClock struct{ now time.Time }

func (clock fixedClock) Now() time.Time { return clock.now }

func TestServiceReadsFederationProjection(t *testing.T) {
	t.Parallel()
	testCases := []struct {
		name    string
		profile Profile
	}{
		{name: "indexed profile is returned without a request-time PDS read", profile: Profile{
			DID: testDID, URI: "at://" + testDID + "/dev.adenosine.profile/self", CID: "bafyreiprojected", DisplayName: "Tom Nagle",
		}},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			remote := &sharedRemote{err: errors.New("PDS must not be called")}
			indexed, err := NewService(&fakeStore{profile: tc.profile}, remote, fixedClock{time.Now()}).Get(context.Background(), testDID)
			if err != nil {
				t.Fatalf("get projected profile: %v", err)
			}
			if indexed != tc.profile || remote.getCalls != 0 {
				t.Fatalf("profile/calls = (%+v, %d)", indexed, remote.getCalls)
			}
		})
	}
}

func TestGetProjectionErrors(t *testing.T) {
	t.Parallel()
	storeFailure := errors.New("database unavailable")
	testCases := []struct {
		name      string
		store     *fakeStore
		wantError error
	}{
		{name: "missing projection", store: &fakeStore{}, wantError: ErrNotFound},
		{name: "projection store failure", store: &fakeStore{err: storeFailure}, wantError: storeFailure},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := NewService(tc.store, &sharedRemote{}, fixedClock{time.Now()}).Get(context.Background(), testDID)
			if !errors.Is(err, tc.wantError) {
				t.Fatalf("error = %v, want %v", err, tc.wantError)
			}
		})
	}
}

func TestUpdateValidationAndProviderErrorsAreTyped(t *testing.T) {
	t.Parallel()
	providerFailure := errors.New("write failed")
	testCases := []struct {
		name      string
		input     UpdateInput
		remote    *sharedRemote
		wantError error
		wantField string
		wantCause error
	}{
		{name: "invalid website", input: UpdateInput{Website: "http://example.com"}, remote: &sharedRemote{}, wantError: ErrValidation, wantField: "website"},
		{name: "provider unavailable", input: UpdateInput{}, remote: &sharedRemote{err: providerFailure}, wantError: ErrProvider, wantCause: providerFailure},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			service := NewService(&fakeStore{}, tc.remote, fixedClock{time.Now()})
			_, err := service.Update(context.Background(), testDID, tc.input)
			if !errors.Is(err, tc.wantError) || tc.wantCause != nil && !errors.Is(err, tc.wantCause) {
				t.Fatalf("error = %v", err)
			}
			if tc.wantField != "" {
				var validationError *ValidationError
				if !errors.As(err, &validationError) || validationError.Field != tc.wantField {
					t.Fatalf("validation error = %v", err)
				}
			}
		})
	}
}

func TestUpdatePreservesRecordCreationTime(t *testing.T) {
	t.Parallel()
	testCases := []struct {
		name      string
		createdAt time.Time
	}{
		{name: "existing createdAt remains stable", createdAt: time.Date(2025, 1, 2, 3, 4, 5, 0, time.UTC)},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			remote := &sharedRemote{profile: Profile{
				DID: testDID, URI: "at://" + testDID + "/dev.adenosine.profile/self", CID: "bafyreiexisting",
				DisplayName: "Before", RecordCreatedAt: tc.createdAt,
			}}
			service := NewService(&fakeStore{}, remote, fixedClock{tc.createdAt.Add(24 * time.Hour)})
			updated, err := service.Update(context.Background(), testDID, UpdateInput{DisplayName: "After"})
			if err != nil {
				t.Fatalf("update: %v", err)
			}
			if !updated.RecordCreatedAt.Equal(tc.createdAt) {
				t.Fatalf("record created at = %s, want %s", updated.RecordCreatedAt, tc.createdAt)
			}
		})
	}
}

func TestUpdateRejectsTextBeyondLexiconLimits(t *testing.T) {
	t.Parallel()
	testCases := []struct {
		name  string
		input UpdateInput
	}{
		{name: "display name", input: UpdateInput{DisplayName: strings.Repeat("a", maxDisplayNameRunes+1)}},
		{name: "bio", input: UpdateInput{Bio: strings.Repeat("a", maxBioRunes+1)}},
		{name: "location", input: UpdateInput{Location: strings.Repeat("a", maxLocationRunes+1)}},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			service := NewService(&fakeStore{}, &sharedRemote{}, fixedClock{time.Now()})
			if _, err := service.Update(context.Background(), testDID, tc.input); !errors.Is(err, ErrValidation) {
				t.Fatalf("input %+v error = %v, want validation error", tc.input, err)
			}
		})
	}
}

func TestGetMissingProfileReturnsTypedNotFound(t *testing.T) {
	t.Parallel()
	testCases := []struct {
		name string
		did  string
	}{
		{name: "missing canonical DID", did: testDID},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := NewService(&fakeStore{}, &sharedRemote{}, fixedClock{time.Now()}).Get(context.Background(), tc.did)
			var notFoundError *NotFoundError
			if !errors.Is(err, ErrNotFound) || !errors.As(err, &notFoundError) || notFoundError.DID != tc.did {
				t.Fatalf("not found error = %v", err)
			}
		})
	}
}
