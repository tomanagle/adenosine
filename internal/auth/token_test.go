package auth

import (
	"context"
	"crypto/sha256"
	"errors"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/adenosine-dev/adenosine/internal/repository"
	"github.com/google/uuid"
)

type memoryTokenStore struct {
	token      AccessToken
	tokens     []AccessToken
	lookupErr  error
	touchErr   error
	listErr    error
	revokeErr  error
	touched    bool
	touchedID  uuid.UUID
	touchedAt  time.Time
	listedDID  string
	listedAt   time.Time
	revokedDID string
	revokedID  uuid.UUID
	revokedAt  time.Time
}

func (store *memoryTokenStore) CreateToken(_ context.Context, token AccessToken) (AccessToken, error) {
	store.token = token
	return token, nil
}

func (store *memoryTokenStore) GetActiveTokenByHash(_ context.Context, hash []byte) (AccessToken, error) {
	if store.lookupErr != nil {
		return AccessToken{}, store.lookupErr
	}
	if string(hash) != string(store.token.Hash) {
		return AccessToken{}, ErrUnauthorized
	}
	return store.token, nil
}

func (store *memoryTokenStore) TouchToken(_ context.Context, id uuid.UUID, at time.Time) error {
	store.touched = true
	store.touchedID = id
	store.touchedAt = at
	return store.touchErr
}

func (store *memoryTokenStore) ListTokens(_ context.Context, accountDID string, activeAt time.Time) ([]AccessToken, error) {
	store.listedDID = accountDID
	store.listedAt = activeAt
	return store.tokens, store.listErr
}

func (store *memoryTokenStore) RevokeToken(_ context.Context, accountDID string, id uuid.UUID, revokedAt time.Time) error {
	store.revokedDID = accountDID
	store.revokedID = id
	store.revokedAt = revokedAt
	return store.revokeErr
}

func TestTokenServiceNormalizesMetadata(t *testing.T) {
	t.Parallel()
	testCases := []struct{ name string }{{name: "whitespace"}}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			store := &memoryTokenStore{}
			service := NewTokenService(store, fixedTokenClock{now: time.Now()}, fixedTokenIDs{id: uuid.New()}, fixedSecrets{plaintext: "adn_pat_secret"})

			token, _, err := service.CreateToken(context.Background(), CreateTokenInput{
				AccountDID: "  did:plc:alice\t",
				Name:       "  laptop  ",
				Scopes:     []string{ScopeRepositoryRead},
			})
			if err != nil {
				t.Fatalf("create token: %v", err)
			}
			if token.AccountDID != "did:plc:alice" || token.Name != "laptop" {
				t.Fatalf("normalized metadata = (%q, %q)", token.AccountDID, token.Name)
			}
		})
	}
}

func TestTokenAuthenticatorHashesTouchesAndReturnsMetadata(t *testing.T) {
	t.Parallel()
	testCases := []struct{ name string }{{name: "valid credential"}}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			plaintext := "adn_pat_secret"
			hash := sha256.Sum256([]byte(plaintext))
			now := time.Date(2026, time.August, 9, 12, 0, 0, 0, time.FixedZone("test", 3600))
			id := uuid.New()
			store := &memoryTokenStore{token: AccessToken{ID: id, AccountDID: "did:plc:alice", Name: "laptop", Hash: hash[:]}}

			token, err := NewTokenAuthenticator(store, fixedTokenClock{now: now}).Authenticate(context.Background(), plaintext)
			if err != nil {
				t.Fatalf("authenticate token: %v", err)
			}
			if token.ID != id || token.AccountDID != "did:plc:alice" {
				t.Fatalf("token metadata = %#v", token)
			}
			if store.touchedID != id || !store.touchedAt.Equal(now.UTC()) {
				t.Fatalf("touch = (%s, %v), want (%s, %v)", store.touchedID, store.touchedAt, id, now.UTC())
			}
		})
	}
}

func TestTokenAuthenticatorDoesNotTouchRejectedCredential(t *testing.T) {
	t.Parallel()
	testCases := []struct{ name string }{{name: "rejected credential"}}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			store := &memoryTokenStore{lookupErr: ErrUnauthorized}
			_, err := NewTokenAuthenticator(store, fixedTokenClock{now: time.Now()}).Authenticate(context.Background(), "unknown")
			if !errors.Is(err, ErrUnauthorized) {
				t.Fatalf("error = %v, want ErrUnauthorized", err)
			}
			if store.touched {
				t.Fatal("rejected token was touched")
			}
		})
	}
}

func TestTokenServiceListsOwnedMetadataWithoutHash(t *testing.T) {
	t.Parallel()
	testCases := []struct{ name string }{{name: "owned metadata"}}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			now := time.Date(2026, time.August, 9, 12, 0, 0, 0, time.UTC)
			store := &memoryTokenStore{tokens: []AccessToken{{ID: uuid.New(), AccountDID: "did:plc:alice", Hash: []byte("secret hash")}}}
			service := NewTokenService(store, fixedTokenClock{now: now}, fixedTokenIDs{}, fixedSecrets{})

			tokens, err := service.ListTokens(context.Background(), "  did:plc:alice ")
			if err != nil {
				t.Fatalf("list tokens: %v", err)
			}
			if store.listedDID != "did:plc:alice" || !store.listedAt.Equal(now) {
				t.Fatalf("list scope = (%q, %v)", store.listedDID, store.listedAt)
			}
			if len(tokens) != 1 || tokens[0].Hash != nil {
				t.Fatalf("listed tokens exposed hash: %#v", tokens)
			}
			if store.tokens[0].Hash == nil {
				t.Fatal("listing mutated persisted domain metadata")
			}
		})
	}
}

func TestTokenServiceRevokesByOwnerAndMapsNotFound(t *testing.T) {
	t.Parallel()
	testCases := []struct{ name string }{{name: "not found"}}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			now := time.Date(2026, time.August, 9, 12, 0, 0, 0, time.UTC)
			id := uuid.New()
			store := &memoryTokenStore{revokeErr: ErrNotFound}
			service := NewTokenService(store, fixedTokenClock{now: now}, fixedTokenIDs{}, fixedSecrets{})

			err := service.RevokeToken(context.Background(), " did:plc:alice ", id)
			if !errors.Is(err, ErrNotFound) {
				t.Fatalf("error = %v, want ErrNotFound", err)
			}
			if store.revokedDID != "did:plc:alice" || store.revokedID != id || !store.revokedAt.Equal(now) {
				t.Fatalf("revoke scope = (%q, %s, %v)", store.revokedDID, store.revokedID, store.revokedAt)
			}
		})
	}
}

type fixedTokenClock struct{ now time.Time }

func (clock fixedTokenClock) Now() time.Time { return clock.now }

type fixedTokenIDs struct{ id uuid.UUID }

func (ids fixedTokenIDs) New() (uuid.UUID, error) { return ids.id, nil }

type fixedSecrets struct{ plaintext string }

func (secrets fixedSecrets) New() (string, error) { return secrets.plaintext, nil }

type fixedPermissions struct{ allowed bool }

func (permissions fixedPermissions) CanWriteRepository(context.Context, string, repository.ID) (bool, error) {
	return permissions.allowed, nil
}

func TestTokenServiceStoresOnlyHash(t *testing.T) {
	t.Parallel()
	testCases := []struct{ name string }{{name: "created token"}}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			store := &memoryTokenStore{}
			plaintext := "adn_pat_0123456789abcdefghijklmnopqrstuvwxyz"
			now := time.Date(2026, time.August, 9, 0, 0, 0, 0, time.UTC)
			service := NewTokenService(
				store,
				fixedTokenClock{now: now},
				fixedTokenIDs{id: uuid.MustParse("0198a851-2a89-7ae2-a370-dc68883e3af3")},
				fixedSecrets{plaintext: plaintext},
			)

			token, revealed, err := service.CreateToken(context.Background(), CreateTokenInput{
				AccountDID: "did:plc:alice",
				Name:       "laptop",
				Scopes:     []string{ScopeRepositoryWrite},
			})
			if err != nil {
				t.Fatalf("create token: %v", err)
			}
			if revealed != plaintext {
				t.Fatalf("revealed token = %q", revealed)
			}
			wantHash := sha256.Sum256([]byte(plaintext))
			if string(token.Hash) != string(wantHash[:]) {
				t.Fatal("stored hash does not match SHA-256 token hash")
			}
			if string(token.Hash) == plaintext {
				t.Fatal("plaintext token was stored")
			}
			if token.Prefix != plaintext[:16] {
				t.Fatalf("prefix = %q, want %q", token.Prefix, plaintext[:16])
			}
		})
	}
}

func TestTokenServiceAcceptsStatusOnlyScope(t *testing.T) {
	t.Parallel()
	repositoryID := repository.ID(uuid.New())
	testCases := []struct {
		name         string
		repositoryID *repository.ID
		wantErr      error
	}{
		{name: "repository status", repositoryID: &repositoryID},
		{name: "repository status must be scoped", wantErr: ErrValidation},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			store := &memoryTokenStore{}
			service := NewTokenService(store, fixedTokenClock{now: time.Now()}, fixedTokenIDs{id: uuid.New()}, fixedSecrets{plaintext: "adn_pat_secret"})
			token, _, err := service.CreateToken(context.Background(), CreateTokenInput{
				AccountDID: "did:plc:ci", Name: "external CI", Scopes: []string{ScopeRepositoryStatus}, RepositoryID: testCase.repositoryID,
			})
			if !errors.Is(err, testCase.wantErr) {
				t.Fatalf("CreateToken() error = %v, want %v", err, testCase.wantErr)
			}
			if err == nil && (len(token.Scopes) != 1 || token.Scopes[0] != ScopeRepositoryStatus || token.RepositoryID == nil || *token.RepositoryID != repositoryID) {
				t.Fatalf("token = %+v", token)
			}
		})
	}
}

func TestGitAuthorizer(t *testing.T) {
	t.Parallel()
	testCases := []struct{ name string }{{name: "authorized write"}}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			plaintext := "adn_pat_secret"
			hash := sha256.Sum256([]byte(plaintext))
			repositoryID := repository.ID(uuid.MustParse("0198a851-2a89-7ae2-a370-dc68883e3af2"))
			store := &memoryTokenStore{token: AccessToken{
				ID:         uuid.MustParse("0198a851-2a89-7ae2-a370-dc68883e3af3"),
				AccountDID: "did:plc:alice",
				Hash:       hash[:],
				Scopes:     []string{ScopeRepositoryWrite},
			}}
			authorizer := NewGitAuthorizer(store, fixedPermissions{allowed: true}, fixedTokenClock{now: time.Now()})
			request := httptest.NewRequest("POST", "/alice/repo.git/git-receive-pack", nil)
			request.SetBasicAuth("alice", plaintext)

			if err := authorizer.AuthorizeWrite(context.Background(), request, repository.Repository{ID: repositoryID}); err != nil {
				t.Fatalf("authorize write: %v", err)
			}
			if !store.touched {
				t.Fatal("successful authorization did not update last-used time")
			}
		})
	}
}

func TestGitAuthorizerRejectsMissingScope(t *testing.T) {
	t.Parallel()
	testCases := []struct{ name string }{{name: "read-only token"}}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			plaintext := "adn_pat_secret"
			hash := sha256.Sum256([]byte(plaintext))
			store := &memoryTokenStore{token: AccessToken{
				ID:         uuid.New(),
				AccountDID: "did:plc:alice",
				Hash:       hash[:],
				Scopes:     []string{ScopeRepositoryRead},
			}}
			authorizer := NewGitAuthorizer(store, fixedPermissions{allowed: true}, fixedTokenClock{now: time.Now()})
			request := httptest.NewRequest("POST", "/alice/repo.git/git-receive-pack", nil)
			request.SetBasicAuth("alice", plaintext)

			if err := authorizer.AuthorizeWrite(context.Background(), request, repository.Repository{ID: repository.ID(uuid.New())}); err != ErrForbidden {
				t.Fatalf("error = %v, want ErrForbidden", err)
			}
		})
	}
}
