package database

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

type recordedDatabaseCall struct {
	caller    string
	operation string
	duration  time.Duration
	err       error
}

type fakeCallMetrics struct {
	calls []recordedDatabaseCall
}

func (metrics *fakeCallMetrics) RecordDatabaseCall(_ context.Context, caller, operation string, duration time.Duration, err error) {
	metrics.calls = append(metrics.calls, recordedDatabaseCall{caller: caller, operation: operation, duration: duration, err: err})
}

type sequenceClock struct {
	times []time.Time
	next  int
}

func (clock *sequenceClock) Now() time.Time {
	value := clock.times[clock.next]
	clock.next++
	return value
}

type fakeDatabaseExecutor struct {
	execErr  error
	queryErr error
	rowErr   error
	rows     pgx.Rows
}

func (database fakeDatabaseExecutor) Exec(context.Context, string, ...any) (pgconn.CommandTag, error) {
	return pgconn.NewCommandTag("UPDATE 1"), database.execErr
}

func (database fakeDatabaseExecutor) Query(context.Context, string, ...any) (pgx.Rows, error) {
	return database.rows, database.queryErr
}

func (database fakeDatabaseExecutor) QueryRow(context.Context, string, ...any) pgx.Row {
	return fakeDatabaseRow{err: database.rowErr}
}

type fakeDatabaseRow struct{ err error }

func (row fakeDatabaseRow) Scan(...any) error { return row.err }

type fakeDatabaseRows struct {
	err    error
	closed bool
}

func (rows *fakeDatabaseRows) Close()                                  { rows.closed = true }
func (rows *fakeDatabaseRows) Err() error                              { return rows.err }
func (*fakeDatabaseRows) CommandTag() pgconn.CommandTag                { return pgconn.CommandTag{} }
func (*fakeDatabaseRows) FieldDescriptions() []pgconn.FieldDescription { return nil }
func (*fakeDatabaseRows) Next() bool                                   { return false }
func (*fakeDatabaseRows) Scan(...any) error                            { return nil }
func (*fakeDatabaseRows) Values() ([]any, error)                       { return nil, nil }
func (*fakeDatabaseRows) RawValues() [][]byte                          { return nil }
func (*fakeDatabaseRows) Conn() *pgx.Conn                              { return nil }

func TestInstrumentedDBTXRecordsCompletedCalls(t *testing.T) {
	cause := errors.New("database failure")
	statement := "-- name: UpdateProfile :exec\nUPDATE network.profiles SET handle = $1"
	testCases := []struct {
		name       string
		database   fakeDatabaseExecutor
		invoke     func(instrumentedDBTX) error
		wantErr    error
		wantCaller string
		wantOp     string
	}{
		{name: "exec success", invoke: func(db instrumentedDBTX) error { _, err := db.Exec(context.Background(), statement); return err }, wantCaller: "UpdateProfile", wantOp: "update"},
		{name: "query start error", database: fakeDatabaseExecutor{queryErr: cause}, invoke: func(db instrumentedDBTX) error { _, err := db.Query(context.Background(), statement); return err }, wantErr: cause, wantCaller: "UpdateProfile", wantOp: "update"},
		{name: "query iteration error", database: fakeDatabaseExecutor{rows: &fakeDatabaseRows{err: cause}}, invoke: func(db instrumentedDBTX) error {
			rows, err := db.Query(context.Background(), statement)
			if err != nil {
				return err
			}
			rows.Close()
			return rows.Err()
		}, wantErr: cause, wantCaller: "UpdateProfile", wantOp: "update"},
		{name: "query row scan error", database: fakeDatabaseExecutor{rowErr: cause}, invoke: func(db instrumentedDBTX) error { return db.QueryRow(context.Background(), statement).Scan() }, wantErr: cause, wantCaller: "UpdateProfile", wantOp: "update"},
		{name: "raw SQL uses bounded fallback", invoke: func(db instrumentedDBTX) error {
			_, err := db.Exec(context.Background(), "DELETE FROM network.profiles")
			return err
		}, wantCaller: "Unmapped", wantOp: "delete"},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			metrics := &fakeCallMetrics{}
			clock := &sequenceClock{times: []time.Time{time.Unix(0, 0), time.Unix(0, int64(25*time.Millisecond))}}
			database := testCase.database
			if database.rows == nil && database.queryErr == nil {
				database.rows = &fakeDatabaseRows{}
			}
			db := instrumentedDBTX{db: database, metrics: metrics, clock: clock}
			err := testCase.invoke(db)
			if !errors.Is(err, testCase.wantErr) {
				t.Fatalf("call error = %v, want %v", err, testCase.wantErr)
			}
			if len(metrics.calls) != 1 {
				t.Fatalf("metric calls = %d, want 1", len(metrics.calls))
			}
			call := metrics.calls[0]
			if call.caller != testCase.wantCaller || call.operation != testCase.wantOp || call.duration != 25*time.Millisecond || !errors.Is(call.err, testCase.wantErr) {
				t.Fatalf("recorded call = %#v", call)
			}
		})
	}
}

func TestDatabaseCaller(t *testing.T) {
	testCases := []struct {
		name      string
		statement string
		want      string
	}{
		{name: "sqlc query", statement: "-- name: GetProfile :one\nSELECT * FROM network.profiles", want: "GetProfile"},
		{name: "raw query", statement: "SELECT 1", want: "Unmapped"},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			if got := databaseCaller(testCase.statement); got != testCase.want {
				t.Fatalf("databaseCaller() = %q, want %q", got, testCase.want)
			}
		})
	}
}
