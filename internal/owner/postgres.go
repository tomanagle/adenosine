package owner

import (
	"context"
	"errors"
	"fmt"
	"strings"

	dbgen "github.com/adenosine-dev/adenosine/internal/database/generated"
	"github.com/jackc/pgx/v5"
)

type PostgresResolver struct {
	queries *dbgen.Queries
}

func NewPostgresResolver(queries *dbgen.Queries) *PostgresResolver {
	return &PostgresResolver{queries: queries}
}

func (resolver *PostgresResolver) Resolve(ctx context.Context, name string) (Owner, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return Owner{}, ErrNotFound
	}
	row, err := resolver.queries.ResolveOwner(ctx, name)
	if errors.Is(err, pgx.ErrNoRows) {
		return Owner{}, ErrNotFound
	}
	if err != nil {
		return Owner{}, fmt.Errorf("resolve owner: %w", err)
	}
	return Owner{
		Kind: Kind(row.Kind), CanonicalName: row.CanonicalName,
		AccountDID: row.AccountDid.String, OrganizationSlug: row.OrganizationSlug.String,
	}, nil
}
