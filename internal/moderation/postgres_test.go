package moderation

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	dbgen "github.com/adenosine-dev/adenosine/internal/database/generated"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
)

type postgresDB struct {
	exec  func(string, ...any) error
	query func(string, ...any) (pgx.Rows, error)
}

func (db postgresDB) Exec(_ context.Context, query string, args ...any) (pgconn.CommandTag, error) {
	return pgconn.CommandTag{}, db.exec(query, args...)
}
func (db postgresDB) Query(_ context.Context, query string, args ...any) (pgx.Rows, error) {
	return db.query(query, args...)
}
func (db postgresDB) QueryRow(context.Context, string, ...any) pgx.Row {
	return postgresRow{err: errors.New("unexpected QueryRow")}
}

type postgresRow struct{ err error }

func (row postgresRow) Scan(...any) error { return row.err }

type postgresRows struct {
	values [][]any
	index  int
	err    error
}

func (rows *postgresRows) Close()                                       {}
func (rows *postgresRows) Err() error                                   { return rows.err }
func (rows *postgresRows) CommandTag() pgconn.CommandTag                { return pgconn.CommandTag{} }
func (rows *postgresRows) FieldDescriptions() []pgconn.FieldDescription { return nil }
func (rows *postgresRows) Next() bool {
	if rows.index >= len(rows.values) {
		return false
	}
	rows.index++
	return true
}
func (rows *postgresRows) Scan(destinations ...any) error {
	values := rows.values[rows.index-1]
	if len(destinations) != len(values) {
		return fmt.Errorf("destinations = %d, values = %d", len(destinations), len(values))
	}
	for index := range destinations {
		reflect.ValueOf(destinations[index]).Elem().Set(reflect.ValueOf(values[index]))
	}
	return nil
}
func (rows *postgresRows) Values() ([]any, error) { return rows.values[rows.index-1], nil }
func (rows *postgresRows) RawValues() [][]byte    { return nil }
func (rows *postgresRows) Conn() *pgx.Conn        { return nil }

func TestPostgresStoreUsesOwnerScopedIdempotentMutations(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, time.August, 9, 12, 0, 0, 0, time.FixedZone("offset", 3600))
	testCases := []struct {
		name       string
		operation  func(*PostgresStore) error
		required   []string
		wantTarget string
		wantTime   bool
	}{
		{name: "block uses idempotent insert", operation: func(store *PostgresStore) error {
			return store.PutBlock(context.Background(), "did:plc:alice", "did:plc:bob", now)
		}, required: []string{"moderation.blocked_dids", "ON CONFLICT (account_did, blocked_did) DO NOTHING"}, wantTarget: "did:plc:bob", wantTime: true},
		{name: "unblock is owner scoped", operation: func(store *PostgresStore) error {
			return store.DeleteBlock(context.Background(), "did:plc:alice", "did:plc:bob")
		}, required: []string{"DELETE FROM moderation.blocked_dids", "account_did = $1 AND blocked_did = $2"}, wantTarget: "did:plc:bob"},
		{name: "hide uses idempotent insert", operation: func(store *PostgresStore) error {
			return store.PutHidden(context.Background(), "did:plc:alice", testRecordURI, now)
		}, required: []string{"moderation.hidden_records", "ON CONFLICT (account_did, record_uri) DO NOTHING"}, wantTarget: testRecordURI, wantTime: true},
		{name: "unhide is owner scoped", operation: func(store *PostgresStore) error {
			return store.DeleteHidden(context.Background(), "did:plc:alice", testRecordURI)
		}, required: []string{"DELETE FROM moderation.hidden_records", "account_did = $1 AND record_uri = $2"}, wantTarget: testRecordURI},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			calls := 0
			db := postgresDB{exec: func(query string, args ...any) error {
				calls++
				for _, required := range testCase.required {
					if !strings.Contains(query, required) {
						t.Fatalf("query does not contain %q: %s", required, query)
					}
				}
				if len(args) < 2 || args[0] != "did:plc:alice" || args[1] != testCase.wantTarget {
					t.Fatalf("arguments = %#v", args)
				}
				if testCase.wantTime {
					value, ok := args[2].(pgtype.Timestamptz)
					if !ok || !value.Valid || !value.Time.Equal(now.UTC()) || value.Time.Location() != time.UTC {
						t.Fatalf("time = %#v", args[2])
					}
				}
				return nil
			}, query: func(string, ...any) (pgx.Rows, error) { return nil, errors.New("unexpected Query") }}
			if err := testCase.operation(NewPostgresStore(dbgen.New(db))); err != nil || calls != 1 {
				t.Fatalf("error/calls = %v/%d", err, calls)
			}
		})
	}
}

func TestPostgresStoreMapsModerationLists(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, time.August, 9, 12, 0, 0, 0, time.UTC)
	testCases := []struct {
		name      string
		operation func(*PostgresStore) (string, time.Time, error)
		rowValue  string
		required  string
	}{
		{name: "blocked DIDs", rowValue: "did:plc:bob", required: "WHERE account_did = $1", operation: func(store *PostgresStore) (string, time.Time, error) {
			values, err := store.ListBlocks(context.Background(), "did:plc:alice")
			if err != nil || len(values) != 1 {
				return "", time.Time{}, err
			}
			return values[0].DID, values[0].CreatedAt, nil
		}},
		{name: "hidden records", rowValue: testRecordURI, required: "WHERE account_did = $1", operation: func(store *PostgresStore) (string, time.Time, error) {
			values, err := store.ListHidden(context.Background(), "did:plc:alice")
			if err != nil || len(values) != 1 {
				return "", time.Time{}, err
			}
			return values[0].URI, values[0].CreatedAt, nil
		}},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			db := postgresDB{exec: func(string, ...any) error { return errors.New("unexpected Exec") }, query: func(query string, args ...any) (pgx.Rows, error) {
				if !strings.Contains(query, testCase.required) || len(args) != 1 || args[0] != "did:plc:alice" {
					t.Fatalf("query/arguments = %s/%#v", query, args)
				}
				return &postgresRows{values: [][]any{{testCase.rowValue, pgtype.Timestamptz{Time: now, Valid: true}}}}, nil
			}}
			value, createdAt, err := testCase.operation(NewPostgresStore(dbgen.New(db)))
			if err != nil || value != testCase.rowValue || createdAt != now {
				t.Fatalf("result = %q %v %v", value, createdAt, err)
			}
		})
	}
}

func TestPostgresStoreWrapsDatabaseErrors(t *testing.T) {
	t.Parallel()
	cause := errors.New("database unavailable")
	testCases := []struct {
		name      string
		operation func(*PostgresStore) error
	}{
		{name: "block", operation: func(store *PostgresStore) error {
			return store.PutBlock(context.Background(), "did:plc:alice", "did:plc:bob", time.Now())
		}},
		{name: "unblock", operation: func(store *PostgresStore) error {
			return store.DeleteBlock(context.Background(), "did:plc:alice", "did:plc:bob")
		}},
		{name: "list blocks", operation: func(store *PostgresStore) error {
			_, err := store.ListBlocks(context.Background(), "did:plc:alice")
			return err
		}},
		{name: "hide", operation: func(store *PostgresStore) error {
			return store.PutHidden(context.Background(), "did:plc:alice", testRecordURI, time.Now())
		}},
		{name: "unhide", operation: func(store *PostgresStore) error {
			return store.DeleteHidden(context.Background(), "did:plc:alice", testRecordURI)
		}},
		{name: "list hidden", operation: func(store *PostgresStore) error {
			_, err := store.ListHidden(context.Background(), "did:plc:alice")
			return err
		}},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			db := postgresDB{exec: func(string, ...any) error { return cause }, query: func(string, ...any) (pgx.Rows, error) { return nil, cause }}
			if err := testCase.operation(NewPostgresStore(dbgen.New(db))); !errors.Is(err, cause) {
				t.Fatalf("error = %v, want cause", err)
			}
		})
	}
}
