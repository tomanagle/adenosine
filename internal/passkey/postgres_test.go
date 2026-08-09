package passkey

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	dbgen "github.com/adenosine-dev/adenosine/internal/database/generated"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

type stubPasskeyDB struct {
	queryRow func(string, ...any) pgx.Row
}

func (db stubPasskeyDB) Exec(context.Context, string, ...any) (pgconn.CommandTag, error) {
	return pgconn.CommandTag{}, errors.New("unexpected Exec")
}

func (db stubPasskeyDB) Query(context.Context, string, ...any) (pgx.Rows, error) {
	return nil, errors.New("unexpected Query")
}

func (db stubPasskeyDB) QueryRow(_ context.Context, query string, args ...any) pgx.Row {
	return db.queryRow(query, args...)
}

type errorPasskeyRow struct {
	err error
}

func (row errorPasskeyRow) Scan(...any) error {
	return row.err
}

func TestPostgresStoreSecureErrorMappingsAndQueryScopes(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, time.August, 9, 12, 0, 0, 0, time.UTC)
	id := uuid.New()

	testCases := []struct {
		name          string
		requiredQuery []string
		rowError      error
		operation     func(*PostgresStore) error
		wantError     error
	}{
		{
			name:          "unknown login user is unauthorized",
			requiredQuery: []string{"auth.webauthn_users", "rp_id = $1", "account_did = $2"},
			rowError:      pgx.ErrNoRows,
			operation: func(store *PostgresStore) error {
				_, err := store.GetUser(context.Background(), "example.com", "did:plc:missing")
				return err
			},
			wantError: ErrUnauthorized,
		},
		{
			name:          "unknown credential is unauthorized",
			requiredQuery: []string{"credential_id = $2", "revoked_at IS NULL"},
			rowError:      pgx.ErrNoRows,
			operation: func(store *PostgresStore) error {
				_, err := store.GetCredentialByCredentialID(context.Background(), "example.com", []byte("missing"))
				return err
			},
			wantError: ErrUnauthorized,
		},
		{
			name:          "expired or consumed ceremony is unauthorized",
			requiredQuery: []string{"DELETE FROM auth.passkey_ceremonies", "expires_at > $2", "RETURNING"},
			rowError:      pgx.ErrNoRows,
			operation: func(store *PostgresStore) error {
				_, err := store.ConsumeCeremony(context.Background(), []byte("hash"), now)
				return err
			},
			wantError: ErrUnauthorized,
		},
		{
			name:          "unknown account touch is unauthorized",
			requiredQuery: []string{"SET last_seen_at = $2", "last_login_at = $2", "WHERE did = $1"},
			rowError:      pgx.ErrNoRows,
			operation: func(store *PostgresStore) error {
				return store.TouchAccountLogin(context.Background(), "did:plc:missing", now)
			},
			wantError: ErrUnauthorized,
		},
		{
			name:          "owned revoke miss is not found",
			requiredQuery: []string{"id = $2", "rp_id = $3", "account_did = $4", "revoked_at IS NULL"},
			rowError:      pgx.ErrNoRows,
			operation: func(store *PostgresStore) error {
				return store.RevokeCredential(context.Background(), "example.com", "did:plc:alice", id, now)
			},
			wantError: ErrNotFound,
		},
		{
			name:          "duplicate credential is conflict",
			requiredQuery: []string{"INSERT INTO auth.passkey_credentials"},
			rowError:      &pgconn.PgError{Code: "23505"},
			operation: func(store *PostgresStore) error {
				_, err := store.CreateCredential(context.Background(), "example.com", Credential{ID: id})
				return err
			},
			wantError: ErrConflict,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			store := NewPostgresStore(dbgen.New(stubPasskeyDB{queryRow: func(query string, _ ...any) pgx.Row {
				for _, required := range tc.requiredQuery {
					if !strings.Contains(query, required) {
						t.Fatalf("query does not contain %q: %s", required, query)
					}
				}
				return errorPasskeyRow{err: tc.rowError}
			}}))

			if err := tc.operation(store); !errors.Is(err, tc.wantError) {
				t.Fatalf("error = %v, want %v", err, tc.wantError)
			}
		})
	}
}

func TestPostgresStoreCredentialUpdateIsMonotonicAndOwnerScoped(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name     string
		required []string
	}{
		{
			name: "credential security state",
			required: []string{
				"sign_count = GREATEST(sign_count, $1)",
				"flags = $2",
				"clone_warning = clone_warning",
				"sign_count <> 0 AND $1 <= sign_count",
				"GREATEST(last_used_at, $4)",
				"id = $5",
				"rp_id = $6",
				"account_did = $7",
				"revoked_at IS NULL",
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			store := NewPostgresStore(dbgen.New(stubPasskeyDB{queryRow: func(query string, _ ...any) pgx.Row {
				for _, required := range tc.required {
					if !strings.Contains(query, required) {
						t.Fatalf("query does not contain %q: %s", required, query)
					}
				}
				return errorPasskeyRow{err: pgx.ErrNoRows}
			}}))

			_, err := store.UpdateCredential(context.Background(), "example.com", Credential{
				ID: uuid.New(), AccountDID: "did:plc:alice", SignCount: 42, Flags: 29, CloneWarning: true,
			}, time.Now())
			if !errors.Is(err, ErrNotFound) {
				t.Fatalf("error = %v, want ErrNotFound", err)
			}
		})
	}
}
