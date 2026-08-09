package pullrequest

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"testing"
	"time"

	dbgen "github.com/adenosine-dev/adenosine/internal/database/generated"
	"github.com/adenosine-dev/adenosine/internal/repository"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
)

type projectedPRDB struct{ row pgx.Row }

func (db projectedPRDB) Exec(context.Context, string, ...any) (pgconn.CommandTag, error) {
	return pgconn.CommandTag{}, errors.New("unexpected Exec")
}

func TestPostgresStoreConvertsExactLiveMergeTarget(t *testing.T) {
	t.Parallel()
	id := uuid.MustParse("11111111-2222-3333-4444-555555555555")
	createdAt := time.Date(2026, 8, 10, 10, 0, 0, 0, time.UTC)
	statusCreatedAt := createdAt.Add(time.Hour)
	row := projectedPRRow{values: []any{
		testPullRequestURI, pgtype.Text{String: testCID, Valid: true}, testSourceRepositoryURI, testCID, "feature", testSHA1,
		testTargetRepositoryURI, testCID, "main", "title", "body", pgtype.Timestamptz{Time: createdAt, Valid: true},
		"did:plc:target", pgtype.UUID{Bytes: id, Valid: true}, "open", pgtype.Timestamptz{Time: statusCreatedAt, Valid: true},
	}}
	want := mergeTarget{
		Subject: StrongRef{URI: testPullRequestURI, CID: testCID}, SourceRepository: StrongRef{URI: testSourceRepositoryURI, CID: testCID}, SourceBranch: "feature", HeadSHA: testSHA1,
		TargetRepository: StrongRef{URI: testTargetRepositoryURI, CID: testCID}, TargetBranch: "main", Title: "title", Body: "body", CreatedAt: createdAt,
		TargetOwnerDID: "did:plc:target", RepositoryID: repository.ID(id), State: StateOpen, StatusCreatedAt: statusCreatedAt,
	}
	testCases := []struct {
		name    string
		row     projectedPRRow
		want    mergeTarget
		wantErr error
	}{
		{name: "exact local live target", row: row, want: want},
		{name: "stale or deleted target excluded", row: projectedPRRow{err: pgx.ErrNoRows}, wantErr: ErrNotFound},
		{name: "query failure", row: projectedPRRow{err: errors.New("query failed")}, wantErr: errors.New("query failed")},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			got, err := NewPostgresStore(dbgen.New(projectedPRDB{row: testCase.row})).GetMergeTarget(context.Background(), testPullRequestURI)
			if testCase.name == "query failure" {
				if err == nil || err.Error() != "query projected pull request merge target: query failed" {
					t.Fatalf("GetMergeTarget() error = %v", err)
				}
				return
			}
			if !errors.Is(err, testCase.wantErr) || !reflect.DeepEqual(got, testCase.want) {
				t.Fatalf("GetMergeTarget() = %#v, %v, want %#v, %v", got, err, testCase.want, testCase.wantErr)
			}
		})
	}
}
func (db projectedPRDB) Query(context.Context, string, ...any) (pgx.Rows, error) {
	return nil, errors.New("unexpected Query")
}
func (db projectedPRDB) QueryRow(context.Context, string, ...any) pgx.Row { return db.row }

type projectedPRRow struct {
	values []any
	err    error
}

func (row projectedPRRow) Scan(destinations ...any) error {
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

func TestPostgresStoreConvertsProjectedFetchTarget(t *testing.T) {
	t.Parallel()
	id := uuid.MustParse("11111111-2222-3333-4444-555555555555")
	row := projectedPRRow{values: []any{
		testPullRequestURI,
		pgtype.Text{String: testCID, Valid: true},
		pgtype.Text{String: "https://git.example.com/source/project.git", Valid: true},
		"feature/work",
		testSHA1,
		"main",
		pgtype.UUID{Bytes: id, Valid: true},
	}}
	want := fetchTarget{
		URI: testPullRequestURI, CID: testCID, RepositoryID: repository.ID(id),
		SourceURL: "https://git.example.com/source/project.git", SourceBranch: "feature/work",
		HeadSHA: testSHA1, TargetBranch: "main",
	}
	testCases := []struct {
		name    string
		row     projectedPRRow
		want    fetchTarget
		wantErr error
	}{
		{name: "live projected row", row: row, want: want},
		{name: "not found", row: projectedPRRow{err: pgx.ErrNoRows}, wantErr: ErrNotFound},
		{name: "query failure", row: projectedPRRow{err: errors.New("query failed")}, wantErr: errors.New("query failed")},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			got, err := NewPostgresStore(dbgen.New(projectedPRDB{row: testCase.row})).GetFetchTarget(context.Background(), testPullRequestURI)
			if testCase.name == "query failure" {
				if err == nil || err.Error() != "query projected pull request fetch target: query failed" {
					t.Fatalf("GetFetchTarget() error = %v", err)
				}
				return
			}
			if !errors.Is(err, testCase.wantErr) || !reflect.DeepEqual(got, testCase.want) {
				t.Fatalf("GetFetchTarget() = %#v, %v, want %#v, %v", got, err, testCase.want, testCase.wantErr)
			}
		})
	}
}
