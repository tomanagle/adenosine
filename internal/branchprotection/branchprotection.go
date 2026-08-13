// Package branchprotection manages native receive-pack safety policy.
package branchprotection

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
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
	ID                   uuid.UUID
	RepositoryID         repository.ID
	Pattern              string
	DenyForcePush        bool
	DenyDeletion         bool
	RequiredApprovals    int
	DismissStaleReviews  bool
	RequiredStatusChecks []string
	RequireSignedCommits bool
	CreatedAt            time.Time
	UpdatedAt            time.Time
}

type Input struct {
	Pattern              string
	DenyForcePush        bool
	DenyDeletion         bool
	RequiredApprovals    int
	DismissStaleReviews  bool
	RequiredStatusChecks []string
	RequireSignedCommits bool
}
type Page struct {
	Items      []Protection
	NextCursor *uuid.UUID
}

type gitConfigurer interface {
	SetReceiveProtection(context.Context, repository.ID, bool, bool) error
	InstallPushAuthorization(context.Context, repository.ID) error
}

type Service struct {
	queries   *dbgen.Queries
	git       gitConfigurer
	evaluator *Evaluator
}

func NewService(queries *dbgen.Queries, git gitConfigurer) *Service {
	inspector, _ := git.(repositoryInspector)
	return &Service{queries: queries, git: git, evaluator: newEvaluator(postgresEvaluationStore{queries: queries}, inspector)}
}

// Authorize evaluates an atomic set of proposed ref updates before mutation.
func (service *Service) Authorize(ctx context.Context, repositoryID repository.ID, updates []RefUpdate) error {
	return service.evaluator.Authorize(ctx, repositoryID, updates)
}

// Reconcile installs managed authorization hooks for policies restored from storage.
func (service *Service) Reconcile(ctx context.Context) error {
	repositoryIDs, err := service.queries.ListProtectedRepositoryIDs(ctx)
	if err != nil {
		return fmt.Errorf("list protected repositories: %w", err)
	}
	for _, repositoryID := range repositoryIDs {
		if err := service.syncGit(ctx, repository.ID(repositoryID.Bytes)); err != nil {
			return err
		}
	}
	return nil
}

func (service *Service) Create(ctx context.Context, repositoryID repository.ID, input Input, now time.Time) (Protection, error) {
	input, err := input.normalized()
	if err != nil {
		return Protection{}, err
	}
	id, err := uuid.NewV7()
	if err != nil {
		return Protection{}, fmt.Errorf("generate branch protection ID: %w", err)
	}
	row, err := service.queries.CreateBranchProtection(ctx, dbgen.CreateBranchProtectionParams{
		ID: pgUUID(id), RepositoryID: pgUUID(uuid.UUID(repositoryID)), Pattern: input.Pattern,
		DenyForcePush: input.DenyForcePush, DenyDeletion: input.DenyDeletion,
		RequiredApprovals: int16(input.RequiredApprovals), DismissStaleReviews: input.DismissStaleReviews,
		RequiredStatusChecks: input.RequiredStatusChecks, RequireSignedCommits: input.RequireSignedCommits,
		CreatedAt: pgTime(now),
	})
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
	input, err := input.normalized()
	if err != nil {
		return Protection{}, err
	}
	row, err := service.queries.UpdateBranchProtection(ctx, dbgen.UpdateBranchProtectionParams{
		Pattern: input.Pattern, DenyForcePush: input.DenyForcePush, DenyDeletion: input.DenyDeletion,
		RequiredApprovals: int16(input.RequiredApprovals), DismissStaleReviews: input.DismissStaleReviews,
		RequiredStatusChecks: input.RequiredStatusChecks, RequireSignedCommits: input.RequireSignedCommits,
		UpdatedAt: pgTime(now), ID: pgUUID(id), RepositoryID: pgUUID(uuid.UUID(repositoryID)),
	})
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
	_, err := input.normalized()
	return err
}

func (input Input) normalized() (Input, error) {
	input.Pattern = strings.TrimSpace(input.Pattern)
	if !validPattern(input.Pattern) {
		return Input{}, fmt.Errorf("%w: pattern must be *, an exact branch, or a namespace ending in /*", ErrValidation)
	}
	if input.RequiredApprovals < 0 || input.RequiredApprovals > 100 {
		return Input{}, fmt.Errorf("%w: required approvals must be between 0 and 100", ErrValidation)
	}
	if len(input.RequiredStatusChecks) > 50 {
		return Input{}, fmt.Errorf("%w: no more than 50 status contexts may be required", ErrValidation)
	}
	checks := make([]string, 0, len(input.RequiredStatusChecks))
	seen := make(map[string]struct{}, len(input.RequiredStatusChecks))
	for _, context := range input.RequiredStatusChecks {
		context = strings.TrimSpace(context)
		if context == "" || len(context) > 100 || strings.ContainsAny(context, "\r\n\x00") {
			return Input{}, fmt.Errorf("%w: status contexts must contain between 1 and 100 safe characters", ErrValidation)
		}
		if _, duplicate := seen[context]; duplicate {
			return Input{}, fmt.Errorf("%w: status contexts must be unique", ErrValidation)
		}
		seen[context] = struct{}{}
		checks = append(checks, context)
	}
	sort.Strings(checks)
	input.RequiredStatusChecks = checks
	if !input.DenyForcePush && !input.DenyDeletion && input.RequiredApprovals == 0 && len(checks) == 0 && !input.RequireSignedCommits {
		return Input{}, fmt.Errorf("%w: at least one protection must be enabled", ErrValidation)
	}
	return input, nil
}

// Select returns the single deterministic policy for a branch. Exact patterns win,
// followed by the longest namespace prefix and finally the repository-wide fallback.
func Select(protections []Protection, branch string) *Protection {
	var selected *Protection
	selectedRank, selectedLength := 0, 0
	for index := range protections {
		rank, length, matches := patternRank(protections[index].Pattern, branch)
		if !matches || rank < selectedRank || (rank == selectedRank && length <= selectedLength) {
			continue
		}
		selected, selectedRank, selectedLength = &protections[index], rank, length
	}
	return selected
}

func validPattern(pattern string) bool {
	if pattern == "*" {
		return true
	}
	branch := strings.TrimSuffix(pattern, "/*")
	if branch == "" || (strings.Contains(pattern, "*") && branch == pattern) || len(branch) > 255 {
		return false
	}
	if strings.HasPrefix(branch, "/") || strings.HasSuffix(branch, "/") || strings.HasSuffix(branch, ".") {
		return false
	}
	if branch == "HEAD" || branch == "@" || strings.Contains(branch, "..") || strings.Contains(branch, "//") || strings.Contains(branch, "@{") || strings.ContainsAny(branch, " ~^:?\\[") {
		return false
	}
	for _, character := range branch {
		if character < 0x20 || character == 0x7f {
			return false
		}
	}
	for _, component := range strings.Split(branch, "/") {
		if component == "" || strings.HasPrefix(component, ".") || strings.HasSuffix(component, ".lock") {
			return false
		}
	}
	return true
}

func patternRank(pattern, branch string) (int, int, bool) {
	switch {
	case pattern == branch:
		return 3, len(pattern), true
	case pattern == "*":
		return 1, 0, true
	case strings.HasSuffix(pattern, "/*"):
		prefix := strings.TrimSuffix(pattern, "*")
		return 2, len(prefix), strings.HasPrefix(branch, prefix) && len(branch) > len(prefix)
	default:
		return 0, 0, false
	}
}

func (service *Service) syncGit(ctx context.Context, repositoryID repository.ID) error {
	policies, err := service.queries.ListBranchProtectionsForEvaluation(ctx, pgUUID(uuid.UUID(repositoryID)))
	if err != nil {
		return fmt.Errorf("resolve branch protection configuration: %w", err)
	}
	if len(policies) > 0 {
		if err := service.git.InstallPushAuthorization(ctx, repositoryID); err != nil {
			return fmt.Errorf("install Git push authorization: %w", err)
		}
	}
	// Pattern-aware policy is enforced by pre-receive. Native repository-wide
	// flags must stay off or a namespace policy would unintentionally affect all refs.
	if err := service.git.SetReceiveProtection(ctx, repositoryID, false, false); err != nil {
		return fmt.Errorf("apply Git branch protection: %w", err)
	}
	return nil
}

func fromRow(row dbgen.CoreBranchProtection) Protection {
	return Protection{
		ID: uuid.UUID(row.ID.Bytes), RepositoryID: repository.ID(row.RepositoryID.Bytes), Pattern: row.Pattern,
		DenyForcePush: row.DenyForcePush, DenyDeletion: row.DenyDeletion,
		RequiredApprovals: int(row.RequiredApprovals), DismissStaleReviews: row.DismissStaleReviews,
		RequiredStatusChecks: append([]string(nil), row.RequiredStatusChecks...), RequireSignedCommits: row.RequireSignedCommits,
		CreatedAt: row.CreatedAt.Time, UpdatedAt: row.UpdatedAt.Time,
	}
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
