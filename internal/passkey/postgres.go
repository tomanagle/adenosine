// Package passkey persists WebAuthn users, credentials, and ceremonies.
package passkey

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/adenosine-dev/adenosine/internal/auth"
	dbgen "github.com/adenosine-dev/adenosine/internal/database/generated"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
)

var (
	// ErrUnauthorized deliberately hides whether a login user or credential exists.
	ErrUnauthorized = auth.ErrUnauthorized
	// ErrNotFound indicates that an owned active credential was not found.
	ErrNotFound = auth.ErrNotFound
	// ErrConflict indicates that credential material or a ceremony token already exists.
	ErrConflict = auth.ErrConflict
)

// User is the WebAuthn user identity for one account and relying party.
type User struct {
	AccountDID  string
	Handle      []byte
	Name        string
	DisplayName string
	Credentials []Credential
}

// Credential is the complete persisted WebAuthn credential state.
type Credential struct {
	ID                            uuid.UUID
	AccountDID                    string
	Name                          string
	CredentialID                  []byte
	PublicKey                     []byte
	AttestationType               string
	Transports                    []string
	Flags                         byte
	AAGUID                        []byte
	SignCount                     uint32
	CloneWarning                  bool
	Attachment                    string
	AttestationClientDataJSON     []byte
	AttestationClientDataHash     []byte
	AttestationAuthenticatorData  []byte
	AttestationPublicKeyAlgorithm int64
	AttestationObject             []byte
	CreatedAt                     time.Time
	LastUsedAt                    *time.Time
}

// Ceremony is single-use WebAuthn session state addressed by a hashed token.
type Ceremony struct {
	TokenHash        []byte
	Kind             string
	RPID             string
	AccountDID       string
	BrowserSessionID *uuid.UUID
	SessionData      []byte
	CreatedAt        time.Time
	ExpiresAt        time.Time
}

// PostgresStore persists passkey state with generated sqlc queries.
type PostgresStore struct {
	queries *dbgen.Queries
}

// NewPostgresStore constructs a passkey persistence store.
func NewPostgresStore(queries *dbgen.Queries) *PostgresStore {
	return &PostgresStore{queries: queries}
}

// CreateUser creates a database-generated WebAuthn handle or returns the concurrent winner.
func (store *PostgresStore) CreateUser(ctx context.Context, rpID string, user User, createdAt time.Time) (User, error) {
	row, err := store.queries.CreateWebAuthnUser(ctx, dbgen.CreateWebAuthnUserParams{
		RpID:        rpID,
		AccountDid:  user.AccountDID,
		Name:        user.Name,
		DisplayName: user.DisplayName,
		CreatedAt:   passkeyPGTime(createdAt),
	})
	if isUniqueViolation(err) {
		return User{}, ErrConflict
	}
	if err != nil {
		return User{}, fmt.Errorf("create WebAuthn user: %w", err)
	}
	return userFromRow(row, nil), nil
}

// GetUser resolves a WebAuthn login user and all active credentials.
func (store *PostgresStore) GetUser(ctx context.Context, rpID, accountDID string) (User, error) {
	row, err := store.queries.GetWebAuthnUser(ctx, dbgen.GetWebAuthnUserParams{RpID: rpID, AccountDid: accountDID})
	if errors.Is(err, pgx.ErrNoRows) {
		return User{}, ErrUnauthorized
	}
	if err != nil {
		return User{}, fmt.Errorf("get WebAuthn user: %w", err)
	}
	credentials, err := store.ListCredentials(ctx, rpID, accountDID)
	if err != nil {
		return User{}, err
	}
	return userFromRow(row, credentials), nil
}

// CreateCredential inserts an active WebAuthn credential.
func (store *PostgresStore) CreateCredential(ctx context.Context, rpID string, credential Credential) (Credential, error) {
	row, err := store.queries.CreatePasskeyCredential(ctx, dbgen.CreatePasskeyCredentialParams{
		ID:                            passkeyPGUUID(credential.ID),
		RpID:                          rpID,
		AccountDid:                    credential.AccountDID,
		Name:                          credential.Name,
		CredentialID:                  credential.CredentialID,
		PublicKey:                     credential.PublicKey,
		AttestationType:               credential.AttestationType,
		Transports:                    credential.Transports,
		Flags:                         int16(credential.Flags),
		Aaguid:                        credential.AAGUID,
		SignCount:                     int64(credential.SignCount),
		CloneWarning:                  credential.CloneWarning,
		Attachment:                    credential.Attachment,
		AttestationClientDataJson:     credential.AttestationClientDataJSON,
		AttestationClientDataHash:     credential.AttestationClientDataHash,
		AttestationAuthenticatorData:  credential.AttestationAuthenticatorData,
		AttestationPublicKeyAlgorithm: credential.AttestationPublicKeyAlgorithm,
		AttestationObject:             credential.AttestationObject,
		CreatedAt:                     passkeyPGTime(credential.CreatedAt),
	})
	if isUniqueViolation(err) {
		return Credential{}, ErrConflict
	}
	if err != nil {
		return Credential{}, fmt.Errorf("create passkey credential: %w", err)
	}
	return credentialFromRow(row), nil
}

// GetCredentialByCredentialID resolves an active credential without disclosing misses.
func (store *PostgresStore) GetCredentialByCredentialID(ctx context.Context, rpID string, credentialID []byte) (Credential, error) {
	row, err := store.queries.GetActivePasskeyCredentialByCredentialID(ctx, dbgen.GetActivePasskeyCredentialByCredentialIDParams{
		RpID: rpID, CredentialID: credentialID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return Credential{}, ErrUnauthorized
	}
	if err != nil {
		return Credential{}, fmt.Errorf("get passkey credential: %w", err)
	}
	return credentialFromRow(row), nil
}

// UpdateCredential records successful credential use without decreasing its signature counter.
func (store *PostgresStore) UpdateCredential(ctx context.Context, rpID string, credential Credential, usedAt time.Time) (Credential, error) {
	row, err := store.queries.UpdatePasskeyCredential(ctx, dbgen.UpdatePasskeyCredentialParams{
		SignCount:    int64(credential.SignCount),
		Flags:        int16(credential.Flags),
		CloneWarning: credential.CloneWarning,
		LastUsedAt:   passkeyPGTime(usedAt),
		ID:           passkeyPGUUID(credential.ID),
		RpID:         rpID,
		AccountDid:   credential.AccountDID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return Credential{}, ErrNotFound
	}
	if err != nil {
		return Credential{}, fmt.Errorf("update passkey credential: %w", err)
	}
	return credentialFromRow(row), nil
}

// ListCredentials returns active credentials owned by an account for a relying party.
func (store *PostgresStore) ListCredentials(ctx context.Context, rpID, accountDID string) ([]Credential, error) {
	rows, err := store.queries.ListActivePasskeyCredentials(ctx, dbgen.ListActivePasskeyCredentialsParams{
		RpID: rpID, AccountDid: accountDID,
	})
	if err != nil {
		return nil, fmt.Errorf("list passkey credentials: %w", err)
	}
	credentials := make([]Credential, len(rows))
	for index, row := range rows {
		credentials[index] = credentialFromRow(row)
	}
	return credentials, nil
}

// RevokeCredential revokes an active credential only when it belongs to the account and RP.
func (store *PostgresStore) RevokeCredential(ctx context.Context, rpID, accountDID string, id uuid.UUID, revokedAt time.Time) error {
	_, err := store.queries.RevokePasskeyCredential(ctx, dbgen.RevokePasskeyCredentialParams{
		RevokedAt: passkeyPGTime(revokedAt), ID: passkeyPGUUID(id), RpID: rpID, AccountDid: accountDID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("revoke passkey credential: %w", err)
	}
	return nil
}

// CreateCeremony stores hashed, expiring ceremony state.
func (store *PostgresStore) CreateCeremony(ctx context.Context, ceremony Ceremony) (Ceremony, error) {
	row, err := store.queries.CreatePasskeyCeremony(ctx, dbgen.CreatePasskeyCeremonyParams{
		TokenHash:        ceremony.TokenHash,
		Kind:             ceremony.Kind,
		RpID:             ceremony.RPID,
		AccountDid:       ceremony.AccountDID,
		BrowserSessionID: optionalPasskeyPGUUID(ceremony.BrowserSessionID),
		SessionData:      ceremony.SessionData,
		CreatedAt:        passkeyPGTime(ceremony.CreatedAt),
		ExpiresAt:        passkeyPGTime(ceremony.ExpiresAt),
	})
	if isUniqueViolation(err) {
		return Ceremony{}, ErrConflict
	}
	if err != nil {
		return Ceremony{}, fmt.Errorf("create passkey ceremony: %w", err)
	}
	return ceremonyFromValues(
		row.TokenHash, row.Kind, row.RpID, row.AccountDid, row.BrowserSessionID,
		row.SessionData, row.CreatedAt, row.ExpiresAt,
	), nil
}

// ConsumeCeremony atomically deletes and returns unexpired ceremony state.
func (store *PostgresStore) ConsumeCeremony(ctx context.Context, tokenHash []byte, now time.Time) (Ceremony, error) {
	row, err := store.queries.ConsumePasskeyCeremony(ctx, dbgen.ConsumePasskeyCeremonyParams{
		TokenHash: tokenHash, ExpiresAt: passkeyPGTime(now),
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return Ceremony{}, ErrUnauthorized
	}
	if err != nil {
		return Ceremony{}, fmt.Errorf("consume passkey ceremony: %w", err)
	}
	return ceremonyFromValues(
		row.TokenHash, row.Kind, row.RpID, row.AccountDid, row.BrowserSessionID,
		row.SessionData, row.CreatedAt, row.ExpiresAt,
	), nil
}

// PurgeExpiredCeremonies bounds durable state created by abandoned browser ceremonies.
func (store *PostgresStore) PurgeExpiredCeremonies(ctx context.Context, now time.Time) error {
	if _, err := store.queries.PurgeExpiredPasskeyCeremonies(ctx, passkeyPGTime(now)); err != nil {
		return fmt.Errorf("purge expired passkey ceremonies: %w", err)
	}
	return nil
}

// TouchAccountLogin records a successful passkey login without changing the cached handle.
func (store *PostgresStore) TouchAccountLogin(ctx context.Context, accountDID string, loggedInAt time.Time) error {
	_, err := store.queries.TouchAccountLogin(ctx, dbgen.TouchAccountLoginParams{
		Did: accountDID, LastSeenAt: passkeyPGTime(loggedInAt),
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrUnauthorized
	}
	if err != nil {
		return fmt.Errorf("touch passkey account login: %w", err)
	}
	return nil
}

func userFromRow(row dbgen.AuthWebauthnUser, credentials []Credential) User {
	return User{
		AccountDID:  row.AccountDid,
		Handle:      append([]byte(nil), row.Handle...),
		Name:        row.Name,
		DisplayName: row.DisplayName,
		Credentials: credentials,
	}
}

func credentialFromRow(row dbgen.AuthPasskeyCredential) Credential {
	credential := Credential{
		ID:                            uuid.UUID(row.ID.Bytes),
		AccountDID:                    row.AccountDid,
		Name:                          row.Name,
		CredentialID:                  append([]byte(nil), row.CredentialID...),
		PublicKey:                     append([]byte(nil), row.PublicKey...),
		AttestationType:               row.AttestationType,
		Transports:                    append([]string(nil), row.Transports...),
		Flags:                         byte(row.Flags),
		AAGUID:                        append([]byte(nil), row.Aaguid...),
		SignCount:                     uint32(row.SignCount),
		CloneWarning:                  row.CloneWarning,
		Attachment:                    row.Attachment,
		AttestationClientDataJSON:     append([]byte(nil), row.AttestationClientDataJson...),
		AttestationClientDataHash:     append([]byte(nil), row.AttestationClientDataHash...),
		AttestationAuthenticatorData:  append([]byte(nil), row.AttestationAuthenticatorData...),
		AttestationPublicKeyAlgorithm: row.AttestationPublicKeyAlgorithm,
		AttestationObject:             append([]byte(nil), row.AttestationObject...),
		CreatedAt:                     row.CreatedAt.Time,
	}
	if row.LastUsedAt.Valid {
		lastUsedAt := row.LastUsedAt.Time
		credential.LastUsedAt = &lastUsedAt
	}
	return credential
}

func ceremonyFromValues(
	tokenHash []byte,
	kind string,
	rpID string,
	accountDID string,
	browserSessionID pgtype.UUID,
	sessionData []byte,
	createdAt pgtype.Timestamptz,
	expiresAt pgtype.Timestamptz,
) Ceremony {
	ceremony := Ceremony{
		TokenHash:   append([]byte(nil), tokenHash...),
		Kind:        kind,
		RPID:        rpID,
		AccountDID:  accountDID,
		SessionData: append([]byte(nil), sessionData...),
		CreatedAt:   createdAt.Time,
		ExpiresAt:   expiresAt.Time,
	}
	if browserSessionID.Valid {
		id := uuid.UUID(browserSessionID.Bytes)
		ceremony.BrowserSessionID = &id
	}
	return ceremony
}

func isUniqueViolation(err error) bool {
	var postgresError *pgconn.PgError
	return errors.As(err, &postgresError) && postgresError.Code == "23505"
}

func passkeyPGUUID(id uuid.UUID) pgtype.UUID {
	return pgtype.UUID{Bytes: [16]byte(id), Valid: true}
}

func optionalPasskeyPGUUID(id *uuid.UUID) pgtype.UUID {
	if id == nil {
		return pgtype.UUID{}
	}
	return passkeyPGUUID(*id)
}

func passkeyPGTime(value time.Time) pgtype.Timestamptz {
	return pgtype.Timestamptz{Time: value.UTC(), Valid: true}
}
