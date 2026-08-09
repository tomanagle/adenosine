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

	"github.com/google/uuid"
)

// Session is persisted browser-session metadata. Hash contains only the
// SHA-256 digest of the one-time plaintext credential.
type Session struct {
	ID         uuid.UUID
	AccountDID string
	Hash       []byte
	CreatedAt  time.Time
	ExpiresAt  time.Time
	LastSeenAt *time.Time
	RevokedAt  *time.Time
}

// SessionIdentity identifies an account and its authenticated session.
type SessionIdentity struct {
	SessionID  uuid.UUID
	AccountDID string
}

type sessionStore interface {
	AuthenticateSession(context.Context, []byte, time.Time) (SessionIdentity, error)
}

type sessionServiceStore interface {
	CreateSession(context.Context, Session) (Session, error)
	RevokeSession(context.Context, string, uuid.UUID, time.Time) error
}

// SessionService issues and revokes browser sessions.
type SessionService struct {
	store    sessionServiceStore
	clock    tokenClock
	ids      tokenIDGenerator
	secrets  secretGenerator
	lifetime time.Duration
}

// RandomSessionSecretGenerator creates high-entropy URL-safe session credentials.
type RandomSessionSecretGenerator struct{}

// New returns a session credential containing 256 random bits.
func (RandomSessionSecretGenerator) New() (string, error) {
	value := make([]byte, 32)
	if _, err := rand.Read(value); err != nil {
		return "", fmt.Errorf("read random bytes: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(value), nil
}

// NewSessionService constructs a browser-session service with an injected lifetime.
func NewSessionService(store sessionServiceStore, clock tokenClock, ids tokenIDGenerator, secrets secretGenerator, lifetime time.Duration) *SessionService {
	return &SessionService{store: store, clock: clock, ids: ids, secrets: secrets, lifetime: lifetime}
}

// CreateSession stores a session hash and returns its plaintext credential once.
func (service *SessionService) CreateSession(ctx context.Context, accountDID string) (Session, string, error) {
	accountDID = strings.TrimSpace(accountDID)
	if accountDID == "" {
		return Session{}, "", fmt.Errorf("%w: account DID must not be empty", ErrValidation)
	}
	if service.lifetime <= 0 {
		return Session{}, "", fmt.Errorf("%w: session lifetime must be positive", ErrValidation)
	}
	id, err := service.ids.New()
	if err != nil {
		return Session{}, "", fmt.Errorf("generate session ID: %w", err)
	}
	plaintext, err := service.secrets.New()
	if err != nil {
		return Session{}, "", fmt.Errorf("generate session secret: %w", err)
	}
	hash := sha256.Sum256([]byte(plaintext))
	now := service.clock.Now().UTC()
	session := Session{
		ID:         id,
		AccountDID: accountDID,
		Hash:       hash[:],
		CreatedAt:  now,
		ExpiresAt:  now.Add(service.lifetime),
	}
	session, err = service.store.CreateSession(ctx, session)
	if err != nil {
		return Session{}, "", fmt.Errorf("store session: %w", err)
	}
	return session, plaintext, nil
}

// RevokeSession soft-revokes an active session owned by an account.
func (service *SessionService) RevokeSession(ctx context.Context, accountDID string, sessionID uuid.UUID) error {
	accountDID = strings.TrimSpace(accountDID)
	if accountDID == "" {
		return fmt.Errorf("%w: account DID must not be empty", ErrValidation)
	}
	if err := service.store.RevokeSession(ctx, accountDID, sessionID, service.clock.Now().UTC()); err != nil {
		return fmt.Errorf("revoke session: %w", err)
	}
	return nil
}

// SessionAuthenticator authenticates existing session credentials.
type SessionAuthenticator struct {
	store sessionStore
	clock tokenClock
}

// NewSessionAuthenticator constructs an existing-session authenticator.
func NewSessionAuthenticator(store sessionStore, clock tokenClock) *SessionAuthenticator {
	return &SessionAuthenticator{store: store, clock: clock}
}

// Authenticate hashes a plaintext session credential and atomically records successful use.
func (authenticator *SessionAuthenticator) Authenticate(ctx context.Context, plaintext string) (SessionIdentity, error) {
	if plaintext == "" {
		return SessionIdentity{}, ErrUnauthorized
	}
	hash := sha256.Sum256([]byte(plaintext))
	identity, err := authenticator.store.AuthenticateSession(ctx, hash[:], authenticator.clock.Now().UTC())
	if err != nil {
		if errors.Is(err, ErrUnauthorized) {
			return SessionIdentity{}, ErrUnauthorized
		}
		return SessionIdentity{}, fmt.Errorf("authenticate session: %w", err)
	}
	return identity, nil
}
