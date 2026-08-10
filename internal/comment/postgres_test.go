package comment

import (
	"context"
	"errors"
	"fmt"
	"os"
	"reflect"
	"strings"
	"testing"

	dbgen "github.com/adenosine-dev/adenosine/internal/database/generated"
	"github.com/adenosine-dev/adenosine/internal/issue"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
)

type postgresDB struct{ row pgx.Row }

func (db postgresDB) Exec(context.Context, string, ...any) (pgconn.CommandTag, error) {
	return pgconn.CommandTag{}, errors.New("unexpected Exec")
}
func (db postgresDB) Query(context.Context, string, ...any) (pgx.Rows, error) {
	return nil, errors.New("unexpected Query")
}
func (db postgresDB) QueryRow(context.Context, string, ...any) pgx.Row { return db.row }

type postgresRow struct {
	values []any
	err    error
}

func (row postgresRow) Scan(destinations ...any) error {
	if row.err != nil {
		return row.err
	}
	if len(destinations) != len(row.values) {
		return fmt.Errorf("destinations = %d, values = %d", len(destinations), len(row.values))
	}
	for index := range destinations {
		reflect.ValueOf(destinations[index]).Elem().Set(reflect.ValueOf(row.values[index]))
	}
	return nil
}

func TestPostgresStoreMapsCurrentTargetsAndMissingRows(t *testing.T) {
	t.Parallel()
	testCases := []struct {
		name      string
		row       postgresRow
		operation func(*PostgresStore) (issue.StrongRef, string, error)
		wantRef   issue.StrongRef
		wantIssue string
		wantErr   error
	}{
		{name: "issue target", row: postgresRow{values: []any{testIssueURI, pgtype.Text{String: testCID, Valid: true}}}, operation: func(store *PostgresStore) (issue.StrongRef, string, error) {
			value, err := store.GetIssueTarget(context.Background(), testIssueURI)
			return value, "", err
		}, wantRef: issue.StrongRef{URI: testIssueURI, CID: testCID}},
		{name: "parent target", row: postgresRow{values: []any{testParentURI, pgtype.Text{String: testCID, Valid: true}, testIssueURI}}, operation: func(store *PostgresStore) (issue.StrongRef, string, error) {
			value, err := store.GetParentTarget(context.Background(), testParentURI)
			return value.Ref, value.IssueURI, err
		}, wantRef: issue.StrongRef{URI: testParentURI, CID: testCID}, wantIssue: testIssueURI},
		{name: "missing issue", row: postgresRow{err: pgx.ErrNoRows}, operation: func(store *PostgresStore) (issue.StrongRef, string, error) {
			value, err := store.GetIssueTarget(context.Background(), testIssueURI)
			return value, "", err
		}, wantErr: issue.ErrNotFound},
		{name: "missing parent", row: postgresRow{err: pgx.ErrNoRows}, operation: func(store *PostgresStore) (issue.StrongRef, string, error) {
			value, err := store.GetParentTarget(context.Background(), testParentURI)
			return value.Ref, value.IssueURI, err
		}, wantErr: issue.ErrNotFound},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			store := NewPostgresStore(dbgen.New(postgresDB{row: testCase.row}))
			ref, issueURI, err := testCase.operation(store)
			if !errors.Is(err, testCase.wantErr) || ref != testCase.wantRef || issueURI != testCase.wantIssue {
				t.Fatalf("result = %#v %q %v", ref, issueURI, err)
			}
		})
	}
}

func TestPostgresCommentQueriesAreAtomicBoundedAndViewerFiltered(t *testing.T) {
	t.Parallel()
	testCases := []struct {
		name     string
		required []string
	}{
		{name: "projection roots visibility and count in active issue", required: []string{"-- name: ListNetworkIssueComments :many", "WITH target AS", "issue.deleted_at IS NULL", "issue.cid IS NOT NULL", "visible AS MATERIALIZED", "(SELECT count(*) FROM visible) AS comment_count"}},
		{name: "authenticated filtering is owner scoped", required: []string{"sqlc.narg(account_did)::text IS NULL", "blocked.account_did = sqlc.narg(account_did)", "blocked.blocked_did = comment.author_did", "hidden.account_did = sqlc.narg(account_did)", "hidden.record_uri = comment.uri"}},
		{name: "list is chronological and bounded", required: []string{"ORDER BY visible.record_created_at, visible.uri", "LIMIT sqlc.arg(page_size)"}},
		{name: "parent target requires active parent and issue", required: []string{"-- name: GetNetworkIssueCommentParentTarget :one", "JOIN network.issues AS issue", "comment.deleted_at IS NULL", "comment.cid IS NOT NULL"}},
		{name: "repository and issue rows expose their own comment counts", required: []string{"repository.open_issue_count,\n\trepository.comment_count", "issue.comment_count", "COALESCE(projected_issue.comment_count, 0) AS comment_count"}},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			contents, err := os.ReadFile("../database/queries/federation.sql")
			if err != nil {
				t.Fatal(err)
			}
			for _, required := range testCase.required {
				if !strings.Contains(string(contents), required) {
					t.Fatalf("federation.sql does not contain %q", required)
				}
			}
		})
	}
}
