package auth

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	dbgen "github.com/adenosine-dev/adenosine/internal/database/generated"
	"github.com/adenosine-dev/adenosine/internal/repository"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
)

// PostgresStore persists and resolves authentication credentials and permissions.
type PostgresStore struct {
	queries *dbgen.Queries
}

// NewPostgresStore constructs an authentication store.
func NewPostgresStore(queries *dbgen.Queries) *PostgresStore {
	return &PostgresStore{queries: queries}
}

// UpsertLogin records a successful login and refreshes cached account identity.
func (store *PostgresStore) UpsertLogin(ctx context.Context, did, verifiedHandle string, now time.Time) (Account, error) {
	did = strings.TrimSpace(did)
	verifiedHandle = strings.TrimSpace(verifiedHandle)
	if did == "" {
		return Account{}, fmt.Errorf("%w: account DID must not be empty", ErrValidation)
	}
	now = now.UTC()
	row, err := store.queries.UpsertAccount(ctx, dbgen.UpsertAccountParams{
		Did:         did,
		HandleCache: optionalPGText(verifiedHandle),
		FirstSeenAt: authPGTime(now),
		LastSeenAt:  authPGTime(now),
		LastLoginAt: authPGTime(now),
		CreatedAt:   authPGTime(now),
	})
	if err != nil {
		return Account{}, fmt.Errorf("upsert login account: %w", err)
	}
	return accountFromUpsertRow(row), nil
}

// GetAccount returns the locally cached identity for a DID.
func (store *PostgresStore) GetAccount(ctx context.Context, did string) (Account, error) {
	did = strings.TrimSpace(did)
	if did == "" {
		return Account{}, fmt.Errorf("%w: account DID must not be empty", ErrValidation)
	}
	row, err := store.queries.GetAccount(ctx, did)
	if errors.Is(err, pgx.ErrNoRows) {
		return Account{}, ErrNotFound
	}
	if err != nil {
		return Account{}, fmt.Errorf("get account: %w", err)
	}
	return accountFromRow(row), nil
}

// CreateToken inserts hashed personal access token metadata.
func (store *PostgresStore) CreateToken(ctx context.Context, token AccessToken) (AccessToken, error) {
	row, err := store.queries.CreateAccessToken(ctx, dbgen.CreateAccessTokenParams{
		ID:           authPGUUID(token.ID),
		AccountDid:   token.AccountDID,
		Name:         token.Name,
		TokenPrefix:  token.Prefix,
		TokenHash:    token.Hash,
		Scopes:       token.Scopes,
		RepositoryID: repositoryPGUUID(token.RepositoryID),
		CreatedAt:    authPGTime(token.CreatedAt),
		ExpiresAt:    optionalPGTime(token.ExpiresAt),
	})
	if err != nil {
		return AccessToken{}, fmt.Errorf("create access token: %w", err)
	}
	return tokenFromRow(row), nil
}

// GetActiveTokenByHash resolves an unexpired, unrevoked token hash.
func (store *PostgresStore) GetActiveTokenByHash(ctx context.Context, hash []byte) (AccessToken, error) {
	row, err := store.queries.GetActiveAccessTokenByHash(ctx, hash)
	if errors.Is(err, pgx.ErrNoRows) {
		return AccessToken{}, ErrUnauthorized
	}
	if err != nil {
		return AccessToken{}, fmt.Errorf("get access token: %w", err)
	}
	return tokenFromRow(row), nil
}

// TouchToken records successful credential use.
func (store *PostgresStore) TouchToken(ctx context.Context, id uuid.UUID, usedAt time.Time) error {
	if err := store.queries.TouchAccessToken(ctx, dbgen.TouchAccessTokenParams{
		ID:         authPGUUID(id),
		LastUsedAt: authPGTime(usedAt),
	}); err != nil {
		return fmt.Errorf("touch access token: %w", err)
	}
	return nil
}

// ListTokens returns active token metadata for an account without credential hashes.
func (store *PostgresStore) ListTokens(ctx context.Context, accountDID string, activeAt time.Time) ([]AccessToken, error) {
	rows, err := store.queries.ListActiveAccessTokensByAccountDID(ctx, dbgen.ListActiveAccessTokensByAccountDIDParams{
		AccountDid: accountDID,
		ActiveAt:   authPGTime(activeAt),
	})
	if err != nil {
		return nil, fmt.Errorf("list access tokens: %w", err)
	}
	tokens := make([]AccessToken, len(rows))
	for index, row := range rows {
		tokens[index] = tokenFromListRow(row)
	}
	return tokens, nil
}

// RevokeToken soft-revokes an active token owned by an account.
func (store *PostgresStore) RevokeToken(ctx context.Context, accountDID string, id uuid.UUID, revokedAt time.Time) error {
	_, err := store.queries.RevokeAccessToken(ctx, dbgen.RevokeAccessTokenParams{
		RevokedAt:  authPGTime(revokedAt),
		ID:         authPGUUID(id),
		AccountDid: accountDID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("revoke access token: %w", err)
	}
	return nil
}

// CreateSSHKey inserts user SSH public-key metadata.
func (store *PostgresStore) CreateSSHKey(ctx context.Context, key SSHKey) (SSHKey, error) {
	row, err := store.queries.CreateSSHKey(ctx, dbgen.CreateSSHKeyParams{
		ID:          authPGUUID(key.ID),
		AccountDid:  key.AccountDID,
		Name:        key.Name,
		Algorithm:   key.Algorithm,
		PublicKey:   key.PublicKey,
		Fingerprint: key.Fingerprint,
		CreatedAt:   authPGTime(key.CreatedAt),
	})
	if err != nil {
		var postgresError *pgconn.PgError
		if errors.As(err, &postgresError) && postgresError.Code == "23505" {
			return SSHKey{}, ErrConflict
		}
		return SSHKey{}, fmt.Errorf("create SSH key: %w", err)
	}
	return sshKeyFromRow(row), nil
}

// GetActiveSSHKeyByFingerprint resolves an unrevoked SSH public key.
func (store *PostgresStore) GetActiveSSHKeyByFingerprint(ctx context.Context, fingerprint string) (SSHKey, error) {
	row, err := store.queries.GetActiveSSHKeyByFingerprint(ctx, fingerprint)
	if errors.Is(err, pgx.ErrNoRows) {
		return SSHKey{}, ErrUnauthorized
	}
	if err != nil {
		return SSHKey{}, fmt.Errorf("get SSH key: %w", err)
	}
	return sshKeyFromRow(row), nil
}

// TouchSSHKey records successful SSH public-key use.
func (store *PostgresStore) TouchSSHKey(ctx context.Context, id uuid.UUID, usedAt time.Time) error {
	if err := store.queries.TouchSSHKey(ctx, dbgen.TouchSSHKeyParams{
		ID:         authPGUUID(id),
		LastUsedAt: authPGTime(usedAt),
	}); err != nil {
		return fmt.Errorf("touch SSH key: %w", err)
	}
	return nil
}

// ListSSHKeys returns active SSH credentials for an account.
func (store *PostgresStore) ListSSHKeys(ctx context.Context, accountDID string) ([]SSHKey, error) {
	rows, err := store.queries.ListActiveSSHKeysByAccountDID(ctx, accountDID)
	if err != nil {
		return nil, fmt.Errorf("list SSH keys: %w", err)
	}
	keys := make([]SSHKey, len(rows))
	for index, row := range rows {
		keys[index] = sshKeyFromRow(row)
	}
	return keys, nil
}

// RevokeSSHKey soft-revokes an active SSH credential owned by an account.
func (store *PostgresStore) RevokeSSHKey(ctx context.Context, accountDID string, id uuid.UUID, revokedAt time.Time) error {
	_, err := store.queries.RevokeSSHKey(ctx, dbgen.RevokeSSHKeyParams{
		RevokedAt:  authPGTime(revokedAt),
		ID:         authPGUUID(id),
		AccountDid: accountDID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("revoke SSH key: %w", err)
	}
	return nil
}

// CreateSession inserts hashed browser-session metadata.
func (store *PostgresStore) CreateSession(ctx context.Context, session Session) (Session, error) {
	row, err := store.queries.CreateSession(ctx, dbgen.CreateSessionParams{
		ID:         authPGUUID(session.ID),
		AccountDid: session.AccountDID,
		TokenHash:  session.Hash,
		CreatedAt:  authPGTime(session.CreatedAt),
		ExpiresAt:  authPGTime(session.ExpiresAt),
	})
	if err != nil {
		return Session{}, fmt.Errorf("create session: %w", err)
	}
	return sessionFromRow(row), nil
}

// RevokeSession soft-revokes an active session owned by an account.
func (store *PostgresStore) RevokeSession(ctx context.Context, accountDID string, id uuid.UUID, revokedAt time.Time) error {
	_, err := store.queries.RevokeSession(ctx, dbgen.RevokeSessionParams{
		RevokedAt:  authPGTime(revokedAt),
		ID:         authPGUUID(id),
		AccountDid: accountDID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("revoke session: %w", err)
	}
	return nil
}

// AuthenticateSession atomically resolves an active session and records successful use.
func (store *PostgresStore) AuthenticateSession(ctx context.Context, hash []byte, seenAt time.Time) (SessionIdentity, error) {
	row, err := store.queries.AuthenticateSession(ctx, dbgen.AuthenticateSessionParams{
		SeenAt:    authPGTime(seenAt),
		TokenHash: hash,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return SessionIdentity{}, ErrUnauthorized
	}
	if err != nil {
		return SessionIdentity{}, fmt.Errorf("authenticate session: %w", err)
	}
	return SessionIdentity{SessionID: uuid.UUID(row.ID.Bytes), AccountDID: row.AccountDid}, nil
}

// CanWriteRepository checks owner and collaborator write permissions.
func (store *PostgresStore) CanWriteRepository(ctx context.Context, accountDID string, repositoryID repository.ID) (bool, error) {
	allowed, err := store.queries.CanWriteRepository(ctx, dbgen.CanWriteRepositoryParams{
		AccountDid:   accountDID,
		RepositoryID: pgtype.UUID{Bytes: [16]byte(repositoryID), Valid: true},
	})
	if err != nil {
		return false, fmt.Errorf("check repository write permission: %w", err)
	}
	return allowed, nil
}

// CanAdminRepository checks repository owner and explicit admin permissions.
func (store *PostgresStore) CanAdminRepository(ctx context.Context, accountDID string, repositoryID repository.ID) (bool, error) {
	allowed, err := store.queries.CanAdminRepository(ctx, dbgen.CanAdminRepositoryParams{
		AccountDid: accountDID, RepositoryID: pgtype.UUID{Bytes: [16]byte(repositoryID), Valid: true},
	})
	if err != nil {
		return false, fmt.Errorf("check repository admin permission: %w", err)
	}
	return allowed, nil
}

// CanTriageRepository checks issue and pull-request management permissions.
func (store *PostgresStore) CanTriageRepository(ctx context.Context, accountDID string, repositoryID repository.ID) (bool, error) {
	allowed, err := store.queries.CanTriageRepository(ctx, dbgen.CanTriageRepositoryParams{
		AccountDid:   accountDID,
		RepositoryID: pgtype.UUID{Bytes: [16]byte(repositoryID), Valid: true},
	})
	if err != nil {
		return false, fmt.Errorf("check repository triage permission: %w", err)
	}
	return allowed, nil
}

// CanReadRepository checks public visibility, ownership, and collaborator access.
func (store *PostgresStore) CanReadRepository(ctx context.Context, accountDID string, repositoryID repository.ID) (bool, error) {
	allowed, err := store.queries.CanReadRepository(ctx, dbgen.CanReadRepositoryParams{
		AccountDid:   accountDID,
		RepositoryID: pgtype.UUID{Bytes: [16]byte(repositoryID), Valid: true},
	})
	if err != nil {
		return false, fmt.Errorf("check repository read permission: %w", err)
	}
	return allowed, nil
}

func tokenFromRow(row dbgen.AuthAccessToken) AccessToken {
	token := AccessToken{
		ID:         uuid.UUID(row.ID.Bytes),
		AccountDID: row.AccountDid,
		Name:       row.Name,
		Prefix:     row.TokenPrefix,
		Hash:       append([]byte(nil), row.TokenHash...),
		Scopes:     append([]string(nil), row.Scopes...),
		CreatedAt:  row.CreatedAt.Time,
	}
	if row.RepositoryID.Valid {
		id := repository.ID(row.RepositoryID.Bytes)
		token.RepositoryID = &id
	}
	if row.ExpiresAt.Valid {
		expiresAt := row.ExpiresAt.Time
		token.ExpiresAt = &expiresAt
	}
	if row.LastUsedAt.Valid {
		lastUsedAt := row.LastUsedAt.Time
		token.LastUsedAt = &lastUsedAt
	}
	if row.RevokedAt.Valid {
		revokedAt := row.RevokedAt.Time
		token.RevokedAt = &revokedAt
	}
	return token
}

func tokenFromListRow(row dbgen.ListActiveAccessTokensByAccountDIDRow) AccessToken {
	token := AccessToken{
		ID:         uuid.UUID(row.ID.Bytes),
		AccountDID: row.AccountDid,
		Name:       row.Name,
		Prefix:     row.TokenPrefix,
		Scopes:     append([]string(nil), row.Scopes...),
		CreatedAt:  row.CreatedAt.Time,
	}
	if row.RepositoryID.Valid {
		id := repository.ID(row.RepositoryID.Bytes)
		token.RepositoryID = &id
	}
	if row.ExpiresAt.Valid {
		expiresAt := row.ExpiresAt.Time
		token.ExpiresAt = &expiresAt
	}
	if row.LastUsedAt.Valid {
		lastUsedAt := row.LastUsedAt.Time
		token.LastUsedAt = &lastUsedAt
	}
	if row.RevokedAt.Valid {
		revokedAt := row.RevokedAt.Time
		token.RevokedAt = &revokedAt
	}
	return token
}

func sshKeyFromRow(row dbgen.AuthSshKey) SSHKey {
	key := SSHKey{
		ID:          uuid.UUID(row.ID.Bytes),
		AccountDID:  row.AccountDid,
		Name:        row.Name,
		Algorithm:   row.Algorithm,
		PublicKey:   row.PublicKey,
		Fingerprint: row.Fingerprint,
		CreatedAt:   row.CreatedAt.Time,
	}
	if row.LastUsedAt.Valid {
		lastUsedAt := row.LastUsedAt.Time
		key.LastUsedAt = &lastUsedAt
	}
	if row.RevokedAt.Valid {
		revokedAt := row.RevokedAt.Time
		key.RevokedAt = &revokedAt
	}
	return key
}

func sessionFromRow(row dbgen.AuthSession) Session {
	session := Session{
		ID:         uuid.UUID(row.ID.Bytes),
		AccountDID: row.AccountDid,
		Hash:       append([]byte(nil), row.TokenHash...),
		CreatedAt:  row.CreatedAt.Time,
		ExpiresAt:  row.ExpiresAt.Time,
	}
	if row.LastSeenAt.Valid {
		lastSeenAt := row.LastSeenAt.Time
		session.LastSeenAt = &lastSeenAt
	}
	if row.RevokedAt.Valid {
		revokedAt := row.RevokedAt.Time
		session.RevokedAt = &revokedAt
	}
	return session
}

func accountFromRow(row dbgen.CoreAccount) Account {
	account := Account{DID: row.Did}
	if row.HandleCache.Valid {
		handle := row.HandleCache.String
		account.Handle = &handle
	}
	return account
}

func accountFromUpsertRow(row dbgen.UpsertAccountRow) Account {
	account := Account{DID: row.Did}
	if row.HandleCache.Valid {
		handle := row.HandleCache.String
		account.Handle = &handle
	}
	return account
}

func authPGUUID(id uuid.UUID) pgtype.UUID {
	return pgtype.UUID{Bytes: [16]byte(id), Valid: true}
}

func repositoryPGUUID(id *repository.ID) pgtype.UUID {
	if id == nil {
		return pgtype.UUID{}
	}
	return pgtype.UUID{Bytes: [16]byte(*id), Valid: true}
}

func authPGTime(value time.Time) pgtype.Timestamptz {
	return pgtype.Timestamptz{Time: value, Valid: true}
}

func optionalPGTime(value *time.Time) pgtype.Timestamptz {
	if value == nil {
		return pgtype.Timestamptz{}
	}
	return authPGTime(*value)
}

func optionalPGText(value string) pgtype.Text {
	return pgtype.Text{String: value, Valid: value != ""}
}
