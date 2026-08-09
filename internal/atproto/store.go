package atproto

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"time"

	dbgen "github.com/adenosine-dev/adenosine/internal/database/generated"
	"github.com/bluesky-social/indigo/atproto/auth/oauth"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

const authRequestLifetime = 10 * time.Minute

// Clock supplies the current time for OAuth state expiry.
type Clock interface {
	Now() time.Time
}

// SystemClock supplies UTC wall-clock time.
type SystemClock struct{}

func (SystemClock) Now() time.Time { return time.Now() }

// PostgresClientAuthStore encrypts OAuth request state and resumable credentials.
type PostgresClientAuthStore struct {
	queries        *dbgen.Queries
	latest         latestCredentialLoader
	stateAEAD      cipher.AEAD
	credentialAEAD cipher.AEAD
	clock          Clock
	random         io.Reader
}

type latestCredentialLoader interface {
	GetLatestOAuthCredential(context.Context, string) ([]byte, []byte, error)
}

type generatedLatestCredentialLoader struct{ queries *dbgen.Queries }

func (loader generatedLatestCredentialLoader) GetLatestOAuthCredential(ctx context.Context, did string) ([]byte, []byte, error) {
	row, err := loader.queries.GetLatestOAuthCredential(ctx, did)
	return row.SessionIDHash, row.EncryptedPayload, err
}

func buildPostgresClientAuthStore(queries *dbgen.Queries, stateKey, credentialKey []byte, clock Clock, random io.Reader, latest ...latestCredentialLoader) (*PostgresClientAuthStore, error) {
	if queries == nil {
		return nil, errors.New("OAuth queries are required")
	}
	if len(stateKey) != 32 {
		return nil, errors.New("OAuth state encryption key must be 32 bytes")
	}
	if len(credentialKey) != 32 {
		return nil, errors.New("OAuth credential encryption key must be 32 bytes")
	}
	if clock == nil {
		return nil, errors.New("OAuth state clock is required")
	}
	if random == nil {
		return nil, errors.New("OAuth state randomness source is required")
	}
	stateAEAD, err := newAEAD(stateKey)
	if err != nil {
		return nil, fmt.Errorf("construct OAuth state cipher: %w", err)
	}
	credentialAEAD, err := newAEAD(credentialKey)
	if err != nil {
		return nil, fmt.Errorf("construct OAuth credential cipher: %w", err)
	}
	var loader latestCredentialLoader = generatedLatestCredentialLoader{queries: queries}
	if len(latest) > 0 {
		loader = latest[0]
	}
	return &PostgresClientAuthStore{queries: queries, latest: loader, stateAEAD: stateAEAD, credentialAEAD: credentialAEAD, clock: clock, random: random}, nil
}

func newAEAD(key []byte) (cipher.AEAD, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

// SaveAuthRequestInfo JSON-encodes and encrypts create-only request state.
func (store *PostgresClientAuthStore) SaveAuthRequestInfo(ctx context.Context, info oauth.AuthRequestData) error {
	if info.State == "" {
		return fmt.Errorf("save OAuth request state: %w", ErrStateInvalid)
	}
	defer func() { info = oauth.AuthRequestData{} }()
	payload, err := json.Marshal(info)
	if err != nil {
		return fmt.Errorf("encode OAuth request state: %w", err)
	}
	hash := sha256.Sum256([]byte(info.State))
	defer clearBytes(payload)
	nonce := make([]byte, store.stateAEAD.NonceSize())
	if _, err := io.ReadFull(store.random, nonce); err != nil {
		return fmt.Errorf("generate OAuth request nonce: %w", err)
	}
	encrypted := store.stateAEAD.Seal(nonce, nonce, payload, hash[:])
	now := store.clock.Now().UTC()
	if err := store.queries.CreateOAuthState(ctx, dbgen.CreateOAuthStateParams{
		StateHash:        hash[:],
		EncryptedPayload: encrypted,
		CreatedAt:        pgTime(now),
		ExpiresAt:        pgTime(now.Add(authRequestLifetime)),
	}); err != nil {
		return fmt.Errorf("store OAuth request state: %w", err)
	}
	return nil
}

// GetAuthRequestInfo atomically consumes and decrypts unexpired request state.
func (store *PostgresClientAuthStore) GetAuthRequestInfo(ctx context.Context, state string) (*oauth.AuthRequestData, error) {
	if state == "" {
		return nil, ErrStateNotFound
	}
	hash := sha256.Sum256([]byte(state))
	encrypted, err := store.queries.ConsumeOAuthState(ctx, dbgen.ConsumeOAuthStateParams{
		StateHash: hash[:],
		ExpiresAt: pgTime(store.clock.Now().UTC()),
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrStateNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("consume OAuth request state: %w", err)
	}
	if len(encrypted) < store.stateAEAD.NonceSize()+store.stateAEAD.Overhead() {
		return nil, ErrStateInvalid
	}
	nonce, ciphertext := encrypted[:store.stateAEAD.NonceSize()], encrypted[store.stateAEAD.NonceSize():]
	payload, err := store.stateAEAD.Open(nil, nonce, ciphertext, hash[:])
	if err != nil {
		return nil, ErrStateInvalid
	}
	defer clearBytes(payload)
	var info oauth.AuthRequestData
	if err := json.Unmarshal(payload, &info); err != nil {
		info = oauth.AuthRequestData{}
		return nil, ErrStateInvalid
	}
	if info.State != state {
		info = oauth.AuthRequestData{}
		return nil, ErrStateInvalid
	}
	return &info, nil
}

// DeleteAuthRequestInfo idempotently deletes request state by its digest.
func (store *PostgresClientAuthStore) DeleteAuthRequestInfo(ctx context.Context, state string) error {
	hash := sha256.Sum256([]byte(state))
	if err := store.queries.DeleteOAuthState(ctx, hash[:]); err != nil {
		return fmt.Errorf("delete OAuth request state: %w", err)
	}
	return nil
}

// SaveSession upserts encrypted credentials using a fresh nonce.
func (store *PostgresClientAuthStore) SaveSession(ctx context.Context, session oauth.ClientSessionData) error {
	if session.AccountDID.String() == "" || session.SessionID == "" {
		return ErrSessionInvalid
	}
	defer clearSession(&session)
	payload, err := json.Marshal(session)
	if err != nil {
		return fmt.Errorf("encode OAuth session: %w", err)
	}
	defer clearBytes(payload)
	nonce := make([]byte, store.credentialAEAD.NonceSize())
	if _, err := io.ReadFull(store.random, nonce); err != nil {
		return fmt.Errorf("generate OAuth credential nonce: %w", err)
	}
	did := session.AccountDID.String()
	hash := sha256.Sum256([]byte(session.SessionID))
	encrypted := store.credentialAEAD.Seal(nonce, nonce, payload, sessionAAD(did, hash[:]))
	now := store.clock.Now().UTC()
	if err := store.queries.UpsertOAuthCredential(ctx, dbgen.UpsertOAuthCredentialParams{
		AccountDid:       did,
		SessionIDHash:    hash[:],
		EncryptedPayload: encrypted,
		CreatedAt:        pgTime(now),
		UpdatedAt:        pgTime(now),
	}); err != nil {
		return fmt.Errorf("store OAuth credentials: %w", err)
	}
	return nil
}

// GetSession authenticates and decrypts credentials for the requested identity.
func (store *PostgresClientAuthStore) GetSession(ctx context.Context, did syntax.DID, sessionID string) (*oauth.ClientSessionData, error) {
	if did.String() == "" || sessionID == "" {
		return nil, ErrSessionNotFound
	}
	hash := sha256.Sum256([]byte(sessionID))
	encrypted, err := store.queries.GetOAuthCredential(ctx, dbgen.GetOAuthCredentialParams{
		AccountDid:    did.String(),
		SessionIDHash: hash[:],
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrSessionNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("load OAuth credentials: %w", err)
	}
	if len(encrypted) < store.credentialAEAD.NonceSize()+store.credentialAEAD.Overhead() {
		return nil, ErrSessionInvalid
	}
	nonce, ciphertext := encrypted[:store.credentialAEAD.NonceSize()], encrypted[store.credentialAEAD.NonceSize():]
	payload, err := store.credentialAEAD.Open(nil, nonce, ciphertext, sessionAAD(did.String(), hash[:]))
	if err != nil {
		return nil, ErrSessionInvalid
	}
	defer clearBytes(payload)
	var session oauth.ClientSessionData
	if err := json.Unmarshal(payload, &session); err != nil {
		return nil, ErrSessionInvalid
	}
	if session.AccountDID != did || session.SessionID != sessionID {
		clearSession(&session)
		return nil, ErrSessionInvalid
	}
	return &session, nil
}

// GetLatestSession decrypts the most recently updated credential for a DID.
func (store *PostgresClientAuthStore) GetLatestSession(ctx context.Context, did syntax.DID) (*oauth.ClientSessionData, error) {
	if did.String() == "" || store.latest == nil {
		return nil, ErrSessionNotFound
	}
	hash, encrypted, err := store.latest.GetLatestOAuthCredential(ctx, did.String())
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrSessionNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("load latest OAuth credentials: %w", err)
	}
	if len(hash) != sha256.Size || len(encrypted) < store.credentialAEAD.NonceSize()+store.credentialAEAD.Overhead() {
		return nil, ErrSessionInvalid
	}
	nonce, ciphertext := encrypted[:store.credentialAEAD.NonceSize()], encrypted[store.credentialAEAD.NonceSize():]
	payload, err := store.credentialAEAD.Open(nil, nonce, ciphertext, sessionAAD(did.String(), hash))
	if err != nil {
		return nil, ErrSessionInvalid
	}
	defer clearBytes(payload)
	var session oauth.ClientSessionData
	if err := json.Unmarshal(payload, &session); err != nil {
		return nil, ErrSessionInvalid
	}
	sessionHash := sha256.Sum256([]byte(session.SessionID))
	if session.AccountDID != did || session.SessionID == "" || !equalBytes(sessionHash[:], hash) {
		clearSession(&session)
		return nil, ErrSessionInvalid
	}
	return &session, nil
}

// DeleteSession idempotently removes credentials for one Indigo session.
func (store *PostgresClientAuthStore) DeleteSession(ctx context.Context, did syntax.DID, sessionID string) error {
	hash := sha256.Sum256([]byte(sessionID))
	if err := store.queries.DeleteOAuthCredential(ctx, dbgen.DeleteOAuthCredentialParams{
		AccountDid:    did.String(),
		SessionIDHash: hash[:],
	}); err != nil {
		return fmt.Errorf("delete OAuth credentials: %w", err)
	}
	return nil
}

func sessionAAD(did string, sessionIDHash []byte) []byte {
	aad := make([]byte, len(did)+1+len(sessionIDHash))
	copy(aad, did)
	copy(aad[len(did)+1:], sessionIDHash)
	return aad
}

func equalBytes(left, right []byte) bool {
	return len(left) == len(right) && subtle.ConstantTimeCompare(left, right) == 1
}

func clearSession(session *oauth.ClientSessionData) {
	if session != nil {
		*session = oauth.ClientSessionData{}
	}
}

func clearBytes(value []byte) {
	for index := range value {
		value[index] = 0
	}
}

func pgTime(value time.Time) pgtype.Timestamptz {
	return pgtype.Timestamptz{Time: value, Valid: true}
}

var _ oauth.ClientAuthStore = (*PostgresClientAuthStore)(nil)
