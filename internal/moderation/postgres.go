package moderation

import (
	"context"
	"fmt"
	"time"

	dbgen "github.com/adenosine-dev/adenosine/internal/database/generated"
	"github.com/jackc/pgx/v5/pgtype"
)

// PostgresStore persists account-local moderation preferences.
type PostgresStore struct{ queries *dbgen.Queries }

// NewPostgresStore constructs a moderation store from generated queries.
func NewPostgresStore(queries *dbgen.Queries) *PostgresStore { return &PostgresStore{queries: queries} }

func (store *PostgresStore) PutBlock(ctx context.Context, accountDID, blockedDID string, createdAt time.Time) error {
	err := store.queries.BlockDID(ctx, dbgen.BlockDIDParams{AccountDid: accountDID, BlockedDid: blockedDID, CreatedAt: pgTime(createdAt)})
	if err != nil {
		return fmt.Errorf("put blocked DID: %w", err)
	}
	return nil
}

func (store *PostgresStore) DeleteBlock(ctx context.Context, accountDID, blockedDID string) error {
	err := store.queries.UnblockDID(ctx, dbgen.UnblockDIDParams{AccountDid: accountDID, BlockedDid: blockedDID})
	if err != nil {
		return fmt.Errorf("delete blocked DID: %w", err)
	}
	return nil
}

func (store *PostgresStore) ListBlocks(ctx context.Context, accountDID string) ([]BlockedDID, error) {
	rows, err := store.queries.ListBlockedDIDs(ctx, accountDID)
	if err != nil {
		return nil, fmt.Errorf("query blocked DIDs: %w", err)
	}
	values := make([]BlockedDID, len(rows))
	for index, row := range rows {
		values[index] = BlockedDID{DID: row.BlockedDid, CreatedAt: row.CreatedAt.Time}
	}
	return values, nil
}

func (store *PostgresStore) PutHidden(ctx context.Context, accountDID, recordURI string, createdAt time.Time) error {
	err := store.queries.HideRecord(ctx, dbgen.HideRecordParams{AccountDid: accountDID, RecordUri: recordURI, CreatedAt: pgTime(createdAt)})
	if err != nil {
		return fmt.Errorf("put hidden record: %w", err)
	}
	return nil
}

func (store *PostgresStore) DeleteHidden(ctx context.Context, accountDID, recordURI string) error {
	err := store.queries.UnhideRecord(ctx, dbgen.UnhideRecordParams{AccountDid: accountDID, RecordUri: recordURI})
	if err != nil {
		return fmt.Errorf("delete hidden record: %w", err)
	}
	return nil
}

func (store *PostgresStore) ListHidden(ctx context.Context, accountDID string) ([]HiddenRecord, error) {
	rows, err := store.queries.ListHiddenRecords(ctx, accountDID)
	if err != nil {
		return nil, fmt.Errorf("query hidden records: %w", err)
	}
	values := make([]HiddenRecord, len(rows))
	for index, row := range rows {
		values[index] = HiddenRecord{URI: row.RecordUri, CreatedAt: row.CreatedAt.Time}
	}
	return values, nil
}

func pgTime(value time.Time) pgtype.Timestamptz {
	return pgtype.Timestamptz{Time: value.UTC(), Valid: true}
}
