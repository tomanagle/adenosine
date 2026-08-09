package profile

import (
	"context"
	"errors"
	"fmt"
	"time"

	dbgen "github.com/adenosine-dev/adenosine/internal/database/generated"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

// PostgresStore persists rebuildable network profile projections.
type PostgresStore struct{ queries *dbgen.Queries }

// NewPostgresStore constructs a profile projection store.
func NewPostgresStore(queries *dbgen.Queries) *PostgresStore { return &PostgresStore{queries: queries} }

// Get loads a projected profile by canonical DID.
func (store *PostgresStore) Get(ctx context.Context, did string) (Profile, error) {
	row, err := store.queries.GetProfile(ctx, did)
	if errors.Is(err, pgx.ErrNoRows) {
		return Profile{}, &NotFoundError{DID: did}
	}
	if err != nil {
		return Profile{}, fmt.Errorf("get profile query: %w", err)
	}
	return profileFromRow(row), nil
}

func profileFromRow(row dbgen.NetworkProfile) Profile {
	return Profile{
		DID: row.Did, URI: textValue(row.ProfileUri), CID: textValue(row.ProfileCid), Handle: textValue(row.Handle),
		DisplayName: textValue(row.DisplayName), Bio: textValue(row.Bio), AvatarRef: textValue(row.AvatarRef),
		Website: textValue(row.Website), Location: textValue(row.Location), RepositoryCount: row.RepositoryCount,
		ContributionCount: row.ContributionCount, RecordCreatedAt: timeValue(row.RecordCreatedAt), IndexedAt: timeValue(row.IndexedAt),
	}
}

func textValue(value pgtype.Text) string {
	if !value.Valid {
		return ""
	}
	return value.String
}

func timeValue(value pgtype.Timestamptz) time.Time {
	if !value.Valid {
		return time.Time{}
	}
	return value.Time
}

var _ projectionStore = (*PostgresStore)(nil)
