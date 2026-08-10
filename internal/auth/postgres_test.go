package auth

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	dbgen "github.com/adenosine-dev/adenosine/internal/database/generated"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
)

type stubAuthDB struct {
	queryRow func(string, ...any) pgx.Row
}

func (db stubAuthDB) Exec(context.Context, string, ...any) (pgconn.CommandTag, error) {
	return pgconn.CommandTag{}, errors.New("unexpected Exec")
}

func (db stubAuthDB) Query(context.Context, string, ...any) (pgx.Rows, error) {
	return nil, errors.New("unexpected Query")
}

func (db stubAuthDB) QueryRow(_ context.Context, query string, args ...any) pgx.Row {
	return db.queryRow(query, args...)
}

type stubRow struct {
	values []any
	err    error
}

func (row stubRow) Scan(destinations ...any) error {
	if row.err != nil {
		return row.err
	}
	if len(destinations) != len(row.values) {
		return fmt.Errorf("scan destinations = %d, values = %d", len(destinations), len(row.values))
	}
	for index := range destinations {
		reflect.ValueOf(destinations[index]).Elem().Set(reflect.ValueOf(row.values[index]))
	}
	return nil
}

func TestPostgresStoreUpsertLoginNormalizesIdentityAndLoginTimes(t *testing.T) {
	t.Parallel()
	testCases := []struct {
		name string
	}{
		{name: "success"},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			now := time.Date(2026, time.August, 9, 12, 0, 0, 0, time.FixedZone("test", 3600))
			store := NewPostgresStore(dbgen.New(stubAuthDB{queryRow: func(query string, args ...any) pgx.Row {
				if !strings.Contains(query, "ON CONFLICT (did) DO UPDATE") {
					t.Fatalf("unexpected upsert query: %s", query)
				}
				if len(args) != 6 || args[0] != "did:plc:alice" {
					t.Fatalf("upsert arguments = %#v", args)
				}
				handle, ok := args[1].(pgtype.Text)
				if !ok || !handle.Valid || handle.String != "alice.example" {
					t.Fatalf("handle argument = %#v", args[1])
				}
				for _, index := range []int{2, 3, 4, 5} {
					at, ok := args[index].(pgtype.Timestamptz)
					if !ok || !at.Valid || !at.Time.Equal(now.UTC()) || at.Time.Location() != time.UTC {
						t.Fatalf("time argument %d = %#v", index, args[index])
					}
				}
				return accountRow("did:plc:alice", handle)
			}}))

			account, err := store.UpsertLogin(context.Background(), " did:plc:alice ", " alice.example ", now)
			if err != nil {
				t.Fatalf("upsert login: %v", err)
			}
			if account.DID != "did:plc:alice" || account.Handle == nil || *account.Handle != "alice.example" {
				t.Fatalf("account = %#v", account)
			}
		})
	}
}

func TestPostgresStoreGetAccountHandlesOptionalIdentityAndMissingRows(t *testing.T) {
	t.Parallel()
	testCases := []struct {
		name string
		run  func(*testing.T)
	}{
		{name: "optional handle", run: func(t *testing.T) {
			store := NewPostgresStore(dbgen.New(stubAuthDB{queryRow: func(_ string, args ...any) pgx.Row {
				if len(args) != 1 || args[0] != "did:plc:alice" {
					t.Fatalf("get account arguments = %#v", args)
				}
				return accountRow("did:plc:alice", pgtype.Text{})
			}}))
			account, err := store.GetAccount(context.Background(), " did:plc:alice ")
			if err != nil {
				t.Fatalf("get account: %v", err)
			}
			if account.DID != "did:plc:alice" || account.Handle != nil {
				t.Fatalf("account = %#v", account)
			}
		}},
		{name: "missing", run: func(t *testing.T) {
			store := NewPostgresStore(dbgen.New(stubAuthDB{queryRow: func(string, ...any) pgx.Row {
				return stubRow{err: pgx.ErrNoRows}
			}}))
			_, err := store.GetAccount(context.Background(), "did:plc:missing")
			if !errors.Is(err, ErrNotFound) {
				t.Fatalf("error = %v, want ErrNotFound", err)
			}
		}},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, testCase.run)
	}
}

func TestPostgresStoreRevokeSessionScopesOwnershipAndMapsNoRows(t *testing.T) {
	t.Parallel()
	testCases := []struct {
		name string
	}{
		{name: "missing owned session"},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			id := uuid.New()
			now := time.Date(2026, time.August, 9, 12, 0, 0, 0, time.UTC)
			store := NewPostgresStore(dbgen.New(stubAuthDB{queryRow: func(query string, args ...any) pgx.Row {
				if !strings.Contains(query, "account_did = $3") || !strings.Contains(query, "revoked_at IS NULL") || !strings.Contains(query, "expires_at > $1") {
					t.Fatalf("revocation is not ownership-safe: %s", query)
				}
				if len(args) != 3 || args[0] != authPGTime(now) || args[1] != authPGUUID(id) || args[2] != "did:plc:alice" {
					t.Fatalf("revoke arguments = %#v", args)
				}
				return stubRow{err: pgx.ErrNoRows}
			}}))

			err := store.RevokeSession(context.Background(), "did:plc:alice", id, now)
			if !errors.Is(err, ErrNotFound) {
				t.Fatalf("error = %v, want ErrNotFound", err)
			}
		})
	}
}

func accountRow(did string, handle pgtype.Text) pgx.Row {
	at := pgtype.Timestamptz{Time: time.Date(2025, time.January, 1, 0, 0, 0, 0, time.UTC), Valid: true}
	return stubRow{values: []any{did, handle, at, at, at, at}}
}
