package database

import (
	"context"
	"errors"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// CallMetrics is implemented by the process telemetry dependency and by small
// fakes in database tests.
type CallMetrics interface {
	RecordDatabaseCall(context.Context, string, string, time.Duration, error)
}

type clock interface {
	Now() time.Time
}

type systemClock struct{}

func (systemClock) Now() time.Time { return time.Now() }

type databaseExecutor interface {
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
	Query(context.Context, string, ...any) (pgx.Rows, error)
	QueryRow(context.Context, string, ...any) pgx.Row
}

type instrumentedDBTX struct {
	db      databaseExecutor
	metrics CallMetrics
	clock   clock
}

func (db instrumentedDBTX) Exec(ctx context.Context, statement string, args ...any) (pgconn.CommandTag, error) {
	started := db.clock.Now()
	result, err := db.db.Exec(ctx, statement, args...)
	db.record(ctx, statement, started, err)
	return result, err
}

func (db instrumentedDBTX) Query(ctx context.Context, statement string, args ...any) (pgx.Rows, error) {
	started := db.clock.Now()
	rows, err := db.db.Query(ctx, statement, args...)
	if err != nil {
		db.record(ctx, statement, started, err)
		return nil, err
	}
	return &instrumentedRows{Rows: rows, record: func(callErr error) {
		db.record(ctx, statement, started, callErr)
	}}, nil
}

func (db instrumentedDBTX) QueryRow(ctx context.Context, statement string, args ...any) pgx.Row {
	started := db.clock.Now()
	return instrumentedRow{Row: db.db.QueryRow(ctx, statement, args...), record: func(callErr error) {
		db.record(ctx, statement, started, callErr)
	}}
}

func (db instrumentedDBTX) record(ctx context.Context, statement string, started time.Time, err error) {
	if db.metrics == nil {
		return
	}
	db.metrics.RecordDatabaseCall(ctx, databaseCaller(statement), databaseOperation(statement), db.clock.Now().Sub(started), err)
}

type instrumentedRows struct {
	pgx.Rows
	once   sync.Once
	record func(error)
}

func (rows *instrumentedRows) Next() bool {
	next := rows.Rows.Next()
	if !next {
		rows.finish()
	}
	return next
}

func (rows *instrumentedRows) Close() {
	rows.Rows.Close()
	rows.finish()
}

func (rows *instrumentedRows) finish() {
	rows.once.Do(func() { rows.record(rows.Rows.Err()) })
}

type instrumentedRow struct {
	pgx.Row
	record func(error)
}

func (row instrumentedRow) Scan(dest ...any) error {
	err := row.Row.Scan(dest...)
	row.record(err)
	return err
}

type instrumentedTx struct {
	pgx.Tx
	metrics CallMetrics
	clock   clock
}

func (tx instrumentedTx) Begin(ctx context.Context) (pgx.Tx, error) {
	started := tx.clock.Now()
	child, err := tx.Tx.Begin(ctx)
	tx.record(ctx, "Begin", "begin", started, err)
	if err != nil {
		return nil, err
	}
	return instrumentedTx{Tx: child, metrics: tx.metrics, clock: tx.clock}, nil
}

func (tx instrumentedTx) Commit(ctx context.Context) error {
	started := tx.clock.Now()
	err := tx.Tx.Commit(ctx)
	tx.record(ctx, "Commit", "commit", started, err)
	return err
}

func (tx instrumentedTx) Rollback(ctx context.Context) error {
	started := tx.clock.Now()
	err := tx.Tx.Rollback(ctx)
	// A deferred rollback after a successful commit is the normal pgx pattern
	// and does not execute a database operation.
	if errors.Is(err, pgx.ErrTxClosed) {
		return err
	}
	tx.record(ctx, "Rollback", "rollback", started, err)
	return err
}

func (tx instrumentedTx) Exec(ctx context.Context, statement string, args ...any) (pgconn.CommandTag, error) {
	return tx.dbtx().Exec(ctx, statement, args...)
}

func (tx instrumentedTx) Query(ctx context.Context, statement string, args ...any) (pgx.Rows, error) {
	return tx.dbtx().Query(ctx, statement, args...)
}

func (tx instrumentedTx) QueryRow(ctx context.Context, statement string, args ...any) pgx.Row {
	return tx.dbtx().QueryRow(ctx, statement, args...)
}

func (tx instrumentedTx) dbtx() instrumentedDBTX {
	return instrumentedDBTX{db: tx.Tx, metrics: tx.metrics, clock: tx.clock}
}

func (tx instrumentedTx) record(ctx context.Context, caller, operation string, started time.Time, err error) {
	if tx.metrics != nil {
		tx.metrics.RecordDatabaseCall(ctx, caller, operation, tx.clock.Now().Sub(started), err)
	}
}

func databaseCaller(statement string) string {
	fields := strings.Fields(statement)
	if len(fields) >= 3 && fields[0] == "--" && fields[1] == "name:" {
		return fields[2]
	}
	return "Unmapped"
}
