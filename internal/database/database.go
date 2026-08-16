// Package database owns PostgreSQL connection lifecycle.
package database

import (
	"context"
	"fmt"
	"strings"
	"time"

	dbgen "github.com/adenosine-dev/adenosine/internal/database/generated"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
)

// DB is the process-wide PostgreSQL pool.
type DB struct {
	pool         *pgxpool.Pool
	metrics      CallMetrics
	clock        clock
	registration metric.Registration
}

// Open constructs and verifies a PostgreSQL pool.
func Open(ctx context.Context, databaseURL string, metrics CallMetrics) (*DB, error) {
	config, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		return nil, fmt.Errorf("parse postgres pool configuration: %w", err)
	}
	config.ConnConfig.Tracer = databaseTracer{}
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		return nil, fmt.Errorf("create postgres pool: %w", err)
	}
	db := &DB{pool: pool, metrics: metrics, clock: systemClock{}}
	if err := db.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping postgres: %w", err)
	}
	if err := db.registerMetrics(); err != nil {
		pool.Close()
		return nil, fmt.Errorf("register postgres pool metrics: %w", err)
	}
	return db, nil
}

// Ping verifies that PostgreSQL can accept a query.
func (db *DB) Ping(ctx context.Context) error {
	started := db.clock.Now()
	err := db.pool.Ping(ctx)
	db.recordCall(ctx, "Ping", "select", started, err)
	if err != nil {
		return fmt.Errorf("ping postgres: %w", err)
	}
	return nil
}

// Queries returns the generated typed query set bound to this pool.
func (db *DB) Queries() *dbgen.Queries {
	return dbgen.New(instrumentedDBTX{db: db.pool, metrics: db.metrics, clock: db.clock})
}

// Begin starts a transaction for components that require atomic generated queries.
func (db *DB) Begin(ctx context.Context) (pgx.Tx, error) {
	started := db.clock.Now()
	tx, err := db.pool.Begin(ctx)
	db.recordCall(ctx, "Begin", "begin", started, err)
	if err != nil {
		return nil, err
	}
	return instrumentedTx{Tx: tx, metrics: db.metrics, clock: db.clock}, nil
}

// Close releases the PostgreSQL pool.
func (db *DB) Close() {
	if db.registration != nil {
		_ = db.registration.Unregister()
	}
	db.pool.Close()
}

func (db *DB) recordCall(ctx context.Context, caller, operation string, started time.Time, err error) {
	if db.metrics != nil {
		db.metrics.RecordDatabaseCall(ctx, caller, operation, db.clock.Now().Sub(started), err)
	}
}

func (db *DB) registerMetrics() error {
	meter := otel.Meter("github.com/adenosine-dev/adenosine/internal/database")
	connections, err := meter.Int64ObservableGauge("adenosine.db.client.connections")
	if err != nil {
		return err
	}
	waits, err := meter.Int64ObservableCounter("adenosine.db.client.connection.waits")
	if err != nil {
		return err
	}
	duration, err := meter.Float64ObservableCounter("adenosine.db.client.connection.wait.duration", metric.WithUnit("s"))
	if err != nil {
		return err
	}
	registration, err := meter.RegisterCallback(func(_ context.Context, observer metric.Observer) error {
		stats := db.pool.Stat()
		observer.ObserveInt64(connections, int64(stats.AcquiredConns()), metric.WithAttributes(attribute.String("state", "used")))
		observer.ObserveInt64(connections, int64(stats.IdleConns()), metric.WithAttributes(attribute.String("state", "idle")))
		observer.ObserveInt64(connections, int64(stats.MaxConns()), metric.WithAttributes(attribute.String("state", "max")))
		observer.ObserveInt64(waits, stats.EmptyAcquireCount())
		observer.ObserveFloat64(duration, stats.EmptyAcquireWaitTime().Seconds())
		return nil
	}, connections, waits, duration)
	if err != nil {
		return err
	}
	db.registration = registration
	return nil
}

type databaseTracer struct{}

func (databaseTracer) TraceQueryStart(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	operation := databaseOperation(data.SQL)
	ctx, _ = otel.Tracer("github.com/adenosine-dev/adenosine/internal/database").Start(ctx, "postgresql."+operation,
		trace.WithAttributes(attribute.String("db.system.name", "postgresql"), attribute.String("db.operation.name", operation)))
	return ctx
}

func (databaseTracer) TraceQueryEnd(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryEndData) {
	span := trace.SpanFromContext(ctx)
	if data.Err != nil {
		span.SetStatus(codes.Error, "PostgreSQL operation failed")
	}
	span.End()
}

func databaseOperation(statement string) string {
	for _, field := range strings.Fields(statement) {
		operation := strings.ToUpper(field)
		switch operation {
		case "SELECT", "INSERT", "UPDATE", "DELETE", "BEGIN", "COMMIT", "ROLLBACK", "CREATE", "ALTER", "DROP":
			return strings.ToLower(operation)
		}
	}
	return "other"
}
