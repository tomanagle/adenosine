// Package branchprotection manages native receive-pack safety policy.
package branchprotection

import (
	"context"
	"errors"
	"fmt"
	"time"

	dbgen "github.com/adenosine-dev/adenosine/internal/database/generated"
	"github.com/adenosine-dev/adenosine/internal/repository"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
)

var (
	ErrNotFound   = errors.New("branch protection not found")
	ErrConflict   = errors.New("branch protection already exists")
	ErrValidation = errors.New("branch protection validation failed")
)

type Protection struct {
	ID            uuid.UUID
	RepositoryID  repository.ID
	Pattern       string
	DenyForcePush bool
	DenyDeletion  bool
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

type Input struct {
	Pattern                     string
	DenyForcePush, DenyDeletion bool
}
type Page struct {
	Items      []Protection
	NextCursor *uuid.UUID
}

type gitConfigurer interface {
	SetReceiveProtection(context.Context, repository.ID, bool, bool) error
}

type Service struct {
	queries *dbgen.Queries
	git     gitConfigurer
}

func NewService(queries *dbgen.Queries, git gitConfigurer) *Service {
	return &Service{queries: queries, git: git}
}

func (service *Service) Create(ctx context.Context, repositoryID repository.ID, input Input, now time.Time) (Protection, error) {
	if err := input.Validate(); err != nil {
		return Protection{}, err
	}
	id, err := uuid.NewV7()
	if err != nil {
		return Protection{}, fmt.Errorf("generate branch protection ID: %w", err)
	}
	row, err := service.queries.CreateBranchProtection(ctx, dbgen.CreateBranchProtectionParams{ID: pgUUID(id), RepositoryID: pgUUID(uuid.UUID(repositoryID)), Pattern: input.Pattern, DenyForcePush: input.DenyForcePush, DenyDeletion: input.DenyDeletion, CreatedAt: pgTime(now)})
	if err != nil {
		return Protection{}, mapError(err)
	}
	if err := service.syncGit(ctx, repositoryID); err != nil {
		return Protection{}, err
	}
	return fromRow(row), nil
}

func (service *Service) Get(ctx context.Context, repositoryID repository.ID, id uuid.UUID) (Protection, error) {
	row, err := service.queries.GetBranchProtection(ctx, dbgen.GetBranchProtectionParams{ID: pgUUID(id), RepositoryID: pgUUID(uuid.UUID(repositoryID))})
	if err != nil {
		return Protection{}, mapError(err)
	}
	return fromRow(row), nil
}

func (service *Service) Page(ctx context.Context, repositoryID repository.ID, after *uuid.UUID, limit int) (Page, error) {
	if limit < 1 || limit > 100 {
		return Page{}, fmt.Errorf("%w: limit must be between 1 and 100", ErrValidation)
	}
	rows, err := service.queries.PageBranchProtections(ctx, dbgen.PageBranchProtectionsParams{RepositoryID: pgUUID(uuid.UUID(repositoryID)), AfterID: optionalUUID(after), PageLimit: int32(limit + 1)})
	if err != nil {
		return Page{}, fmt.Errorf("page branch protections: %w", err)
	}
	page := Page{Items: make([]Protection, min(len(rows), limit))}
	for index := range page.Items {
		page.Items[index] = fromRow(rows[index])
	}
	if len(rows) > limit {
		next := page.Items[len(page.Items)-1].ID
		page.NextCursor = &next
	}
	return page, nil
}

func (service *Service) Update(ctx context.Context, repositoryID repository.ID, id uuid.UUID, input Input, now time.Time) (Protection, error) {
	if err := input.Validate(); err != nil {
		return Protection{}, err
	}
	row, err := service.queries.UpdateBranchProtection(ctx, dbgen.UpdateBranchProtectionParams{Pattern: input.Pattern, DenyForcePush: input.DenyForcePush, DenyDeletion: input.DenyDeletion, UpdatedAt: pgTime(now), ID: pgUUID(id), RepositoryID: pgUUID(uuid.UUID(repositoryID))})
	if err != nil {
		return Protection{}, mapError(err)
	}
	if err := service.syncGit(ctx, repositoryID); err != nil {
		return Protection{}, err
	}
	return fromRow(row), nil
}

func (service *Service) Delete(ctx context.Context, repositoryID repository.ID, id uuid.UUID) error {
	rows, err := service.queries.DeleteBranchProtection(ctx, dbgen.DeleteBranchProtectionParams{ID: pgUUID(id), RepositoryID: pgUUID(uuid.UUID(repositoryID))})
	if err != nil {
		return fmt.Errorf("delete branch protection: %w", err)
	}
	if rows == 0 {
		return ErrNotFound
	}
	return service.syncGit(ctx, repositoryID)
}

func (input Input) Validate() error {
	if input.Pattern != "*" {
		return fmt.Errorf("%w: basic protection supports only the repository-wide * pattern", ErrValidation)
	}
	if !input.DenyForcePush && !input.DenyDeletion {
		return fmt.Errorf("%w: at least one protection must be enabled", ErrValidation)
	}
	return nil
}

func (service *Service) syncGit(ctx context.Context, repositoryID repository.ID) error {
	effective, err := service.queries.GetEffectiveReceiveProtection(ctx, pgUUID(uuid.UUID(repositoryID)))
	if err != nil {
		return fmt.Errorf("resolve effective branch protection: %w", err)
	}
	if err := service.git.SetReceiveProtection(ctx, repositoryID, effective.DenyForcePush, effective.DenyDeletion); err != nil {
		return fmt.Errorf("apply Git branch protection: %w", err)
	}
	return nil
}

func fromRow(row dbgen.CoreBranchProtection) Protection {
	return Protection{ID: uuid.UUID(row.ID.Bytes), RepositoryID: repository.ID(row.RepositoryID.Bytes), Pattern: row.Pattern, DenyForcePush: row.DenyForcePush, DenyDeletion: row.DenyDeletion, CreatedAt: row.CreatedAt.Time, UpdatedAt: row.UpdatedAt.Time}
}
func mapError(err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		return ErrConflict
	}
	return fmt.Errorf("branch protection query: %w", err)
}
func pgUUID(id uuid.UUID) pgtype.UUID { return pgtype.UUID{Bytes: id, Valid: true} }
func optionalUUID(id *uuid.UUID) pgtype.UUID {
	if id == nil {
		return pgtype.UUID{}
	}
	return pgUUID(*id)
}
func pgTime(value time.Time) pgtype.Timestamptz {
	return pgtype.Timestamptz{Time: value.UTC(), Valid: true}
}
