package branchprotection

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"

	dbgen "github.com/adenosine-dev/adenosine/internal/database/generated"
	"github.com/adenosine-dev/adenosine/internal/repository"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

var objectIDPattern = regexp.MustCompile(`^(?:[0-9a-f]{40}|[0-9a-f]{64})$`)

// RefUpdate is the immutable command tuple supplied by Git pre-receive and REST ref mutations.
type RefUpdate struct {
	OldSHA      string
	NewSHA      string
	Ref         string
	EvidenceSHA string
}

type ReviewSummary struct {
	Found            bool
	ApprovalCount    int
	ChangesRequested bool
}

type AllowedSigner struct {
	Principal string
	PublicKey string
}

type evaluationStore interface {
	Policies(context.Context, repository.ID) ([]Protection, error)
	StatusStates(context.Context, repository.ID, string, []string) (map[string]string, error)
	Reviews(context.Context, repository.ID, string, string, bool) (ReviewSummary, error)
	Signers(context.Context) ([]AllowedSigner, error)
}

type repositoryInspector interface {
	IsAncestor(context.Context, repository.ID, string, string) (bool, error)
	NewCommits(context.Context, repository.ID, string, string) ([]string, error)
	VerifySSHSignatures(context.Context, repository.ID, []string, []AllowedSigner) error
}

type Evaluator struct {
	store     evaluationStore
	inspector repositoryInspector
}

func newEvaluator(store evaluationStore, inspector repositoryInspector) *Evaluator {
	return &Evaluator{store: store, inspector: inspector}
}

// Rejection is safe to return to a Git client and contains no database or Git stderr detail.
type Rejection struct{ Reasons []string }

func (rejection *Rejection) Error() string {
	return "branch protection rejected the update: " + strings.Join(rejection.Reasons, "; ")
}

// Authorize evaluates all ref commands as one atomic receive operation.
func (evaluator *Evaluator) Authorize(ctx context.Context, repositoryID repository.ID, updates []RefUpdate) error {
	if evaluator == nil || evaluator.inspector == nil {
		return errors.New("branch protection evaluator is unavailable")
	}
	policies, err := evaluator.store.Policies(ctx, repositoryID)
	if err != nil {
		return fmt.Errorf("load branch protection policies: %w", err)
	}
	reasons := []string{}
	for _, update := range updates {
		branch, applies, validationErr := validateRefUpdate(update)
		if validationErr != nil {
			return validationErr
		}
		if !applies {
			continue
		}
		policy := Select(policies, branch)
		if policy == nil {
			continue
		}
		violations, evaluateErr := evaluator.evaluate(ctx, repositoryID, branch, update, *policy)
		if evaluateErr != nil {
			return evaluateErr
		}
		for _, violation := range violations {
			reasons = append(reasons, update.Ref+": "+violation)
		}
	}
	if len(reasons) > 0 {
		return &Rejection{Reasons: reasons}
	}
	return nil
}

func (evaluator *Evaluator) evaluate(ctx context.Context, repositoryID repository.ID, branch string, update RefUpdate, policy Protection) ([]string, error) {
	if isZeroObjectID(update.NewSHA) {
		if policy.DenyDeletion {
			return []string{"branch deletion is not allowed by " + policy.Pattern}, nil
		}
		return nil, nil
	}
	violations := []string{}
	evidenceSHA := update.EvidenceSHA
	if evidenceSHA == "" {
		evidenceSHA = update.NewSHA
	}
	if policy.DenyForcePush && !isZeroObjectID(update.OldSHA) {
		ancestor, err := evaluator.inspector.IsAncestor(ctx, repositoryID, update.OldSHA, update.NewSHA)
		if err != nil {
			return nil, fmt.Errorf("evaluate fast-forward for %s: %w", update.Ref, err)
		}
		if !ancestor {
			violations = append(violations, "non-fast-forward update is not allowed by "+policy.Pattern)
		}
	}
	if len(policy.RequiredStatusChecks) > 0 {
		states, err := evaluator.store.StatusStates(ctx, repositoryID, evidenceSHA, policy.RequiredStatusChecks)
		if err != nil {
			return nil, fmt.Errorf("evaluate required statuses for %s: %w", update.Ref, err)
		}
		for _, context := range policy.RequiredStatusChecks {
			if states[context] != "success" {
				violations = append(violations, fmt.Sprintf("required status %q is not successful", context))
			}
		}
	}
	if policy.RequiredApprovals > 0 {
		reviews, err := evaluator.store.Reviews(ctx, repositoryID, branch, evidenceSHA, policy.DismissStaleReviews)
		if err != nil {
			return nil, fmt.Errorf("evaluate required reviews for %s: %w", update.Ref, err)
		}
		switch {
		case !reviews.Found:
			violations = append(violations, "a matching open pull request is required")
		case reviews.ChangesRequested:
			violations = append(violations, "the latest review state requests changes")
		case reviews.ApprovalCount < policy.RequiredApprovals:
			violations = append(violations, fmt.Sprintf("requires %d approval(s), found %d", policy.RequiredApprovals, reviews.ApprovalCount))
		}
	}
	if policy.RequireSignedCommits {
		commits, err := evaluator.inspector.NewCommits(ctx, repositoryID, update.OldSHA, update.NewSHA)
		if err != nil {
			return nil, fmt.Errorf("enumerate commits for %s: %w", update.Ref, err)
		}
		signers, err := evaluator.store.Signers(ctx)
		if err != nil {
			return nil, fmt.Errorf("load trusted commit signers: %w", err)
		}
		if err := evaluator.inspector.VerifySSHSignatures(ctx, repositoryID, commits, signers); err != nil {
			violations = append(violations, "every newly reachable commit must have a trusted SSH signature")
		}
	}
	return violations, nil
}

func validateRefUpdate(update RefUpdate) (string, bool, error) {
	if !objectIDPattern.MatchString(update.OldSHA) || !objectIDPattern.MatchString(update.NewSHA) || len(update.OldSHA) != len(update.NewSHA) {
		return "", false, fmt.Errorf("%w: ref update contains an invalid object ID", ErrValidation)
	}
	if update.EvidenceSHA != "" && (!objectIDPattern.MatchString(update.EvidenceSHA) || len(update.EvidenceSHA) != len(update.NewSHA)) {
		return "", false, fmt.Errorf("%w: ref update contains an invalid evidence object ID", ErrValidation)
	}
	if !strings.HasPrefix(update.Ref, "refs/heads/") {
		return "", false, nil
	}
	branch := strings.TrimPrefix(update.Ref, "refs/heads/")
	if !validPattern(branch) || branch == "*" || strings.HasSuffix(branch, "/*") {
		return "", false, fmt.Errorf("%w: ref update contains an invalid branch", ErrValidation)
	}
	return branch, true, nil
}

func isZeroObjectID(value string) bool { return strings.Trim(value, "0") == "" }

type postgresEvaluationStore struct{ queries *dbgen.Queries }

func (store postgresEvaluationStore) Policies(ctx context.Context, repositoryID repository.ID) ([]Protection, error) {
	rows, err := store.queries.ListBranchProtectionsForEvaluation(ctx, pgUUID(uuid.UUID(repositoryID)))
	if err != nil {
		return nil, err
	}
	values := make([]Protection, len(rows))
	for index, row := range rows {
		values[index] = fromRow(row)
	}
	return values, nil
}

func (store postgresEvaluationStore) StatusStates(ctx context.Context, repositoryID repository.ID, sha string, contexts []string) (map[string]string, error) {
	rows, err := store.queries.LatestRequiredCommitStatuses(ctx, dbgen.LatestRequiredCommitStatusesParams{
		RepositoryID: pgUUID(uuid.UUID(repositoryID)), CommitSha: sha, RequiredContexts: contexts,
	})
	if err != nil {
		return nil, err
	}
	states := make(map[string]string, len(rows))
	for _, row := range rows {
		states[row.Context] = row.State
	}
	return states, nil
}

func (store postgresEvaluationStore) Reviews(ctx context.Context, repositoryID repository.ID, branch, sha string, dismissStale bool) (ReviewSummary, error) {
	row, err := store.queries.GetBranchProtectionReviewSummary(ctx, dbgen.GetBranchProtectionReviewSummaryParams{
		RepositoryID: pgUUID(uuid.UUID(repositoryID)), TargetBranch: branch, HeadSha: sha, DismissStaleReviews: dismissStale,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return ReviewSummary{}, nil
	}
	if err != nil {
		return ReviewSummary{}, err
	}
	return ReviewSummary{Found: true, ApprovalCount: int(row.ApprovalCount), ChangesRequested: row.ChangesRequested}, nil
}

func (store postgresEvaluationStore) Signers(ctx context.Context) ([]AllowedSigner, error) {
	rows, err := store.queries.ListActiveSSHKeysForCommitVerification(ctx)
	if err != nil {
		return nil, err
	}
	values := make([]AllowedSigner, len(rows))
	for index, row := range rows {
		values[index] = AllowedSigner{Principal: row.AccountDid, PublicKey: row.PublicKey}
	}
	sort.Slice(values, func(left, right int) bool {
		if values[left].Principal == values[right].Principal {
			return values[left].PublicKey < values[right].PublicKey
		}
		return values[left].Principal < values[right].Principal
	})
	return values, nil
}
