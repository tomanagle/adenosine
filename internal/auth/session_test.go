package auth

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
)

type memorySessionStore struct {
	identity    SessionIdentity
	session     Session
	err         error
	createErr   error
	revokeErr   error
	hash        []byte
	seenAt      time.Time
	calls       int
	createCalls int
	revokedDID  string
	revokedID   uuid.UUID
	revokedAt   time.Time
}

func (store *memorySessionStore) CreateSession(_ context.Context, session Session) (Session, error) {
	store.createCalls++
	store.session = session
	return session, store.createErr
}

func (store *memorySessionStore) RevokeSession(_ context.Context, accountDID string, id uuid.UUID, revokedAt time.Time) error {
	store.revokedDID = accountDID
	store.revokedID = id
	store.revokedAt = revokedAt
	return store.revokeErr
}

func (store *memorySessionStore) AuthenticateSession(_ context.Context, hash []byte, seenAt time.Time) (SessionIdentity, error) {
	store.calls++
	store.hash = append([]byte(nil), hash...)
	store.seenAt = seenAt
	return store.identity, store.err
}

func TestSessionAuthenticatorHashesCredentialAndReturnsIdentity(t *testing.T) {
	t.Parallel()
	plaintext := "session-secret"
	wantHash := sha256.Sum256([]byte(plaintext))
	now := time.Date(2026, time.August, 9, 12, 0, 0, 0, time.FixedZone("test", -3600))
	want := SessionIdentity{SessionID: uuid.New(), AccountDID: "did:plc:alice"}
	store := &memorySessionStore{identity: want}

	identity, err := NewSessionAuthenticator(store, fixedTokenClock{now: now}).Authenticate(context.Background(), plaintext)
	if err != nil {
		t.Fatalf("authenticate session: %v", err)
	}
	if identity != want {
		t.Fatalf("identity = %#v, want %#v", identity, want)
	}
	if string(store.hash) != string(wantHash[:]) {
		t.Fatal("session lookup did not use SHA-256 hash")
	}
	if !store.seenAt.Equal(now.UTC()) || store.seenAt.Location() != time.UTC {
		t.Fatalf("seen at = %v, want %v", store.seenAt, now.UTC())
	}
}

func TestSessionAuthenticatorRejectsUnknownWithoutSeparateTouch(t *testing.T) {
	t.Parallel()
	store := &memorySessionStore{err: ErrUnauthorized}
	_, err := NewSessionAuthenticator(store, fixedTokenClock{now: time.Now()}).Authenticate(context.Background(), "unknown")
	if !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("error = %v, want ErrUnauthorized", err)
	}
	if store.calls != 1 {
		t.Fatalf("store calls = %d, want one atomic authentication call", store.calls)
	}
}

func TestSessionAuthenticatorRejectsEmptyCredentialWithoutLookup(t *testing.T) {
	t.Parallel()
	store := &memorySessionStore{}
	_, err := NewSessionAuthenticator(store, fixedTokenClock{}).Authenticate(context.Background(), "")
	if !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("error = %v, want ErrUnauthorized", err)
	}
	if store.calls != 0 {
		t.Fatal("empty credential reached the store")
	}
}

func TestSessionServiceStoresHashAndReturnsPlaintextOnce(t *testing.T) {
	t.Parallel()
	plaintext := "dGVzdC1zZXNzaW9uLXNlY3JldA"
	wantHash := sha256.Sum256([]byte(plaintext))
	now := time.Date(2026, time.August, 9, 12, 0, 0, 0, time.FixedZone("test", 3600))
	id := uuid.MustParse("0198a851-2a89-7ae2-a370-dc68883e3af3")
	store := &memorySessionStore{}
	service := NewSessionService(store, fixedTokenClock{now: now}, fixedTokenIDs{id: id}, fixedSecrets{plaintext: plaintext}, 24*time.Hour)

	session, revealed, err := service.CreateSession(context.Background(), "  did:plc:alice ")
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	if revealed != plaintext {
		t.Fatalf("revealed credential = %q, want %q", revealed, plaintext)
	}
	if session.ID != id || session.AccountDID != "did:plc:alice" {
		t.Fatalf("session identity = %#v", session)
	}
	if string(store.session.Hash) != string(wantHash[:]) || string(store.session.Hash) == plaintext {
		t.Fatal("session store did not receive only the SHA-256 hash")
	}
	if !session.CreatedAt.Equal(now.UTC()) || !session.ExpiresAt.Equal(now.UTC().Add(24*time.Hour)) {
		t.Fatalf("session lifetime = %v through %v", session.CreatedAt, session.ExpiresAt)
	}
}

func TestRandomSessionSecretGeneratorReturns256URLSafeBits(t *testing.T) {
	t.Parallel()
	plaintext, err := (RandomSessionSecretGenerator{}).New()
	if err != nil {
		t.Fatalf("generate session secret: %v", err)
	}
	decoded, err := base64.RawURLEncoding.DecodeString(plaintext)
	if err != nil {
		t.Fatalf("decode URL-safe session secret: %v", err)
	}
	if len(decoded) != 32 {
		t.Fatalf("random secret bytes = %d, want 32", len(decoded))
	}
}

func TestSessionServiceValidatesDIDAndLifetimeBeforeIssuing(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		did      string
		lifetime time.Duration
	}{
		{name: "empty DID", did: " \t", lifetime: time.Hour},
		{name: "zero lifetime", did: "did:plc:alice", lifetime: 0},
		{name: "negative lifetime", did: "did:plc:alice", lifetime: -time.Hour},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			store := &memorySessionStore{}
			service := NewSessionService(store, fixedTokenClock{}, fixedTokenIDs{}, fixedSecrets{}, test.lifetime)
			_, plaintext, err := service.CreateSession(context.Background(), test.did)
			if !errors.Is(err, ErrValidation) || plaintext != "" {
				t.Fatalf("result = (%q, %v), want empty plaintext and ErrValidation", plaintext, err)
			}
			if store.createCalls != 0 {
				t.Fatal("invalid session reached the store")
			}
		})
	}
}

func TestSessionServiceRevokesByOwnerAndPreservesNotFound(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, time.August, 9, 12, 0, 0, 0, time.UTC)
	id := uuid.New()
	store := &memorySessionStore{revokeErr: ErrNotFound}
	service := NewSessionService(store, fixedTokenClock{now: now}, fixedTokenIDs{}, fixedSecrets{}, time.Hour)

	err := service.RevokeSession(context.Background(), " did:plc:alice ", id)
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("error = %v, want ErrNotFound", err)
	}
	if store.revokedDID != "did:plc:alice" || store.revokedID != id || !store.revokedAt.Equal(now) {
		t.Fatalf("revoke scope = (%q, %s, %v)", store.revokedDID, store.revokedID, store.revokedAt)
	}
}
