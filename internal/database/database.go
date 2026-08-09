// Package database owns PostgreSQL connection lifecycle.
package database

import (
	"context"
	"fmt"

	dbgen "github.com/adenosine-dev/adenosine/internal/database/generated"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// DB is the process-wide PostgreSQL pool.
type DB struct {
	pool *pgxpool.Pool
}

// Open constructs and verifies a PostgreSQL pool.
func Open(ctx context.Context, databaseURL string) (*DB, error) {
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		return nil, fmt.Errorf("create postgres pool: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping postgres: %w", err)
	}
	return &DB{pool: pool}, nil
}

// Ping verifies that PostgreSQL can accept a query.
func (db *DB) Ping(ctx context.Context) error {
	if err := db.pool.Ping(ctx); err != nil {
		return fmt.Errorf("ping postgres: %w", err)
	}
	return nil
}

// Queries returns the generated typed query set bound to this pool.
func (db *DB) Queries() *dbgen.Queries {
	return dbgen.New(db.pool)
}

// Begin starts a transaction for components that require atomic generated queries.
func (db *DB) Begin(ctx context.Context) (pgx.Tx, error) {
	return db.pool.Begin(ctx)
}

// Close releases the PostgreSQL pool.
func (db *DB) Close() {
	db.pool.Close()
}
