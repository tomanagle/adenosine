package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/adenosine-dev/adenosine/internal/repository"
	"github.com/google/uuid"
)

const (
	// ScopeRepositoryRead permits authenticated repository reads.
	ScopeRepositoryRead = "repository:read"
	// ScopeRepositoryWrite permits repository ref mutations when local authorization also allows them.
	ScopeRepositoryWrite = "repository:write"

	tokenPrefix = "adn_pat_"
)

// AccessToken is persisted personal access token metadata.
type AccessToken struct {
	ID           uuid.UUID
	AccountDID   string
	Name         string
	Prefix       string
	Hash         []byte
	Scopes       []string
	RepositoryID *repository.ID
	CreatedAt    time.Time
	ExpiresAt    *time.Time
	LastUsedAt   *time.Time
	RevokedAt    *time.Time
}

// CreateTokenInput describes a new personal access token.
type CreateTokenInput struct {
	AccountDID   string
	Name         string
	Scopes       []string
	RepositoryID *repository.ID
	ExpiresAt    *time.Time
}

type accessTokenStore interface {
	GetActiveTokenByHash(context.Context, []byte) (AccessToken, error)
	TouchToken(context.Context, uuid.UUID, time.Time) error
}

type tokenServiceStore interface {
	CreateToken(context.Context, AccessToken) (AccessToken, error)
	ListTokens(context.Context, string, time.Time) ([]AccessToken, error)
	RevokeToken(context.Context, string, uuid.UUID, time.Time) error
}

type tokenClock interface {
	Now() time.Time
}

type tokenIDGenerator interface {
	New() (uuid.UUID, error)
}

type secretGenerator interface {
	New() (string, error)
}

// TokenService creates personal access tokens and returns plaintext once.
type TokenService struct {
	store   tokenServiceStore
	clock   tokenClock
	ids     tokenIDGenerator
	secrets secretGenerator
}

// NewTokenService constructs a personal access token service.
func NewTokenService(store tokenServiceStore, clock tokenClock, ids tokenIDGenerator, secrets secretGenerator) *TokenService {
	return &TokenService{store: store, clock: clock, ids: ids, secrets: secrets}
}

// CreateToken creates hashed token metadata and returns its one-time plaintext value.
func (service *TokenService) CreateToken(ctx context.Context, input CreateTokenInput) (AccessToken, string, error) {
	input.AccountDID = strings.TrimSpace(input.AccountDID)
	input.Name = strings.TrimSpace(input.Name)
	now := service.clock.Now().UTC()
	if err := input.validate(now); err != nil {
		return AccessToken{}, "", fmt.Errorf("%w: %v", ErrValidation, err)
	}
	id, err := service.ids.New()
	if err != nil {
		return AccessToken{}, "", fmt.Errorf("generate access token ID: %w", err)
	}
	plaintext, err := service.secrets.New()
	if err != nil {
		return AccessToken{}, "", fmt.Errorf("generate access token secret: %w", err)
	}
	hash := sha256.Sum256([]byte(plaintext))
	prefixLength := 16
	if len(plaintext) < prefixLength {
		prefixLength = len(plaintext)
	}
	token := AccessToken{
		ID:           id,
		AccountDID:   input.AccountDID,
		Name:         input.Name,
		Prefix:       plaintext[:prefixLength],
		Hash:         hash[:],
		Scopes:       append([]string(nil), input.Scopes...),
		RepositoryID: input.RepositoryID,
		CreatedAt:    now,
		ExpiresAt:    input.ExpiresAt,
	}
	token, err = service.store.CreateToken(ctx, token)
	if err != nil {
		return AccessToken{}, "", fmt.Errorf("store access token: %w", err)
	}
	return token, plaintext, nil
}

// ListTokens returns active token metadata owned by an account.
func (service *TokenService) ListTokens(ctx context.Context, accountDID string) ([]AccessToken, error) {
	tokens, err := service.store.ListTokens(ctx, strings.TrimSpace(accountDID), service.clock.Now().UTC())
	if err != nil {
		return nil, fmt.Errorf("list access tokens: %w", err)
	}
	metadata := make([]AccessToken, len(tokens))
	for index, token := range tokens {
		token.Hash = nil
		metadata[index] = token
	}
	return metadata, nil
}

// RevokeToken soft-revokes an active token owned by an account.
func (service *TokenService) RevokeToken(ctx context.Context, accountDID string, id uuid.UUID) error {
	if err := service.store.RevokeToken(ctx, strings.TrimSpace(accountDID), id, service.clock.Now().UTC()); err != nil {
		return fmt.Errorf("revoke access token: %w", err)
	}
	return nil
}

// TokenAuthenticator authenticates plaintext personal access tokens.
type TokenAuthenticator struct {
	store accessTokenStore
	clock tokenClock
}

// NewTokenAuthenticator constructs a personal access token authenticator.
func NewTokenAuthenticator(store accessTokenStore, clock tokenClock) *TokenAuthenticator {
	return &TokenAuthenticator{store: store, clock: clock}
}

// Authenticate resolves active token metadata and records successful use.
func (authenticator *TokenAuthenticator) Authenticate(ctx context.Context, plaintext string) (AccessToken, error) {
	if plaintext == "" {
		return AccessToken{}, ErrUnauthorized
	}
	hash := sha256.Sum256([]byte(plaintext))
	token, err := authenticator.store.GetActiveTokenByHash(ctx, hash[:])
	if err != nil {
		if errors.Is(err, ErrUnauthorized) {
			return AccessToken{}, ErrUnauthorized
		}
		return AccessToken{}, fmt.Errorf("authenticate access token: %w", err)
	}
	if err := authenticator.store.TouchToken(ctx, token.ID, authenticator.clock.Now().UTC()); err != nil {
		return AccessToken{}, fmt.Errorf("record access token use: %w", err)
	}
	return token, nil
}

func (input CreateTokenInput) validate(now time.Time) error {
	if strings.TrimSpace(input.AccountDID) == "" {
		return fmt.Errorf("account DID must not be empty")
	}
	if strings.TrimSpace(input.Name) == "" || len(input.Name) > 255 {
		return fmt.Errorf("name must contain between 1 and 255 characters")
	}
	if len(input.Scopes) == 0 {
		return fmt.Errorf("at least one scope is required")
	}
	seen := make(map[string]struct{}, len(input.Scopes))
	for _, scope := range input.Scopes {
		if scope != ScopeRepositoryRead && scope != ScopeRepositoryWrite {
			return fmt.Errorf("unsupported scope %q", scope)
		}
		if _, exists := seen[scope]; exists {
			return fmt.Errorf("duplicate scope %q", scope)
		}
		seen[scope] = struct{}{}
	}
	if input.ExpiresAt != nil && !input.ExpiresAt.After(now) {
		return fmt.Errorf("expiry must be in the future")
	}
	return nil
}

// RandomSecretGenerator creates high-entropy URL-safe token values.
type RandomSecretGenerator struct{}

// New returns a token containing 256 random bits.
func (RandomSecretGenerator) New() (string, error) {
	value := make([]byte, 32)
	if _, err := rand.Read(value); err != nil {
		return "", fmt.Errorf("read random bytes: %w", err)
	}
	return tokenPrefix + base64.RawURLEncoding.EncodeToString(value), nil
}

// UUIDv7Generator creates time-ordered token IDs.
type UUIDv7Generator struct{}

// New returns a UUIDv7.
func (UUIDv7Generator) New() (uuid.UUID, error) {
	id, err := uuid.NewV7()
	if err != nil {
		return uuid.Nil, fmt.Errorf("generate UUIDv7: %w", err)
	}
	return id, nil
}

// SystemClock provides wall-clock time.
type SystemClock struct{}

// Now returns the current time.
func (SystemClock) Now() time.Time { return time.Now() }
