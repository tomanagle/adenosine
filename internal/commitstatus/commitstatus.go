// Package commitstatus owns provider-neutral commit statuses and check runs.
package commitstatus

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"time"

	dbgen "github.com/adenosine-dev/adenosine/internal/database/generated"
	"github.com/adenosine-dev/adenosine/internal/repository"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

const Retention = 90 * 24 * time.Hour

var (
	ErrNotFound   = errors.New("commit status resource not found")
	ErrConflict   = errors.New("commit status resource conflict")
	ErrValidation = errors.New("commit status validation failed")
)

var shaPattern = regexp.MustCompile(`^(?:[0-9a-f]{40}|[0-9a-f]{64})$`)

type State string

const (
	StatePending State = "pending"
	StateSuccess State = "success"
	StateFailure State = "failure"
	StateError   State = "error"
)

type CommitStatus struct {
	ID           uuid.UUID
	RepositoryID repository.ID
	CommitSHA    string
	Context      string
	State        State
	Description  string
	TargetURL    *string
	CreatorDID   string
	ExternalID   string
	CreatedAt    time.Time
}

type StatusInput struct {
	Context     string
	State       State
	Description string
	TargetURL   *string
	ExternalID  string
}

type CheckStatus string

const (
	CheckQueued     CheckStatus = "queued"
	CheckInProgress CheckStatus = "in_progress"
	CheckCompleted  CheckStatus = "completed"
)

type Conclusion string

const (
	ConclusionSuccess        Conclusion = "success"
	ConclusionFailure        Conclusion = "failure"
	ConclusionNeutral        Conclusion = "neutral"
	ConclusionCancelled      Conclusion = "cancelled"
	ConclusionSkipped        Conclusion = "skipped"
	ConclusionTimedOut       Conclusion = "timed_out"
	ConclusionActionRequired Conclusion = "action_required"
)

type CheckRun struct {
	ID            uuid.UUID
	RepositoryID  repository.ID
	CommitSHA     string
	Name          string
	ExternalID    string
	CreatorDID    string
	Status        CheckStatus
	Conclusion    *Conclusion
	DetailsURL    *string
	OutputTitle   string
	OutputSummary string
	Version       int64
	StartedAt     *time.Time
	CompletedAt   *time.Time
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

type CheckRunInput struct {
	CommitSHA     string
	Name          string
	ExternalID    string
	Status        CheckStatus
	Conclusion    *Conclusion
	DetailsURL    *string
	OutputTitle   string
	OutputSummary string
	StartedAt     *time.Time
	CompletedAt   *time.Time
}

type CheckRunUpdate struct {
	ExpectedVersion int64
	Status          CheckStatus
	Conclusion      *Conclusion
	DetailsURL      *string
	OutputTitle     string
	OutputSummary   string
	StartedAt       *time.Time
	CompletedAt     *time.Time
}

type Page[T any] struct {
	Items      []T
	NextCursor *uuid.UUID
}

type Combined struct {
	SHA   string
	State State
	Items []CommitStatus
}

type Service struct {
	queries *dbgen.Queries
}

func NewService(queries *dbgen.Queries) *Service { return &Service{queries: queries} }

func (service *Service) CreateStatus(ctx context.Context, repositoryID repository.ID, creatorDID, commitSHA string, input StatusInput, now time.Time) (CommitStatus, bool, error) {
	normalized, err := normalizeStatusInput(commitSHA, creatorDID, input)
	if err != nil {
		return CommitStatus{}, false, err
	}
	fingerprint, err := requestHash(struct {
		CommitSHA string
		Input     StatusInput
	}{commitSHA, normalized})
	if err != nil {
		return CommitStatus{}, false, err
	}
	id, err := uuid.NewV7()
	if err != nil {
		return CommitStatus{}, false, fmt.Errorf("generate commit status ID: %w", err)
	}
	now = now.UTC()
	row, err := service.queries.CreateCommitStatus(ctx, dbgen.CreateCommitStatusParams{
		ID: pgUUID(id), RepositoryID: pgUUID(uuid.UUID(repositoryID)), CommitSha: commitSHA,
		Context: normalized.Context, State: string(normalized.State), Description: normalized.Description,
		TargetUrl: optionalText(normalized.TargetURL), CreatorDid: creatorDID, ExternalID: normalized.ExternalID,
		RequestHash: fingerprint, CreatedAt: pgTime(now), ExpiresAt: pgTime(now.Add(Retention)),
	})
	if err == nil {
		return statusFromRow(row), true, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return CommitStatus{}, false, fmt.Errorf("create commit status: %w", err)
	}
	existing, err := service.queries.GetCommitStatusByExternalID(ctx, dbgen.GetCommitStatusByExternalIDParams{RepositoryID: pgUUID(uuid.UUID(repositoryID)), CreatorDid: creatorDID, ExternalID: normalized.ExternalID})
	if err != nil {
		return CommitStatus{}, false, fmt.Errorf("resolve commit status replay: %w", err)
	}
	if !bytes.Equal(existing.RequestHash, fingerprint) {
		return CommitStatus{}, false, fmt.Errorf("%w: external_id was already used with a different status", ErrConflict)
	}
	return statusFromRow(existing), false, nil
}

func (service *Service) PageStatuses(ctx context.Context, repositoryID repository.ID, commitSHA string, after *uuid.UUID, limit int) (Page[CommitStatus], error) {
	if err := validatePage(commitSHA, limit); err != nil {
		return Page[CommitStatus]{}, err
	}
	rows, err := service.queries.PageCommitStatuses(ctx, dbgen.PageCommitStatusesParams{RepositoryID: pgUUID(uuid.UUID(repositoryID)), CommitSha: commitSHA, AfterID: optionalUUID(after), PageLimit: int32(limit + 1)})
	if err != nil {
		return Page[CommitStatus]{}, fmt.Errorf("page commit statuses: %w", err)
	}
	page := Page[CommitStatus]{Items: make([]CommitStatus, min(len(rows), limit))}
	for index := range page.Items {
		page.Items[index] = statusFromRow(rows[index])
	}
	if len(rows) > limit {
		next := page.Items[len(page.Items)-1].ID
		page.NextCursor = &next
	}
	return page, nil
}

func (service *Service) Combined(ctx context.Context, repositoryID repository.ID, commitSHA string) (Combined, error) {
	if !shaPattern.MatchString(commitSHA) {
		return Combined{}, fmt.Errorf("%w: commit SHA must be a full lowercase object ID", ErrValidation)
	}
	rows, err := service.queries.LatestCommitStatuses(ctx, dbgen.LatestCommitStatusesParams{RepositoryID: pgUUID(uuid.UUID(repositoryID)), CommitSha: commitSHA})
	if err != nil {
		return Combined{}, fmt.Errorf("read combined commit status: %w", err)
	}
	result := Combined{SHA: commitSHA, State: StatePending, Items: make([]CommitStatus, len(rows))}
	if len(rows) > 0 {
		result.State = StateSuccess
	}
	for index, row := range rows {
		result.Items[index] = statusFromRow(row)
		result.State = combineState(result.State, result.Items[index].State)
	}
	return result, nil
}

func (service *Service) CreateCheckRun(ctx context.Context, repositoryID repository.ID, creatorDID string, input CheckRunInput, now time.Time) (CheckRun, bool, error) {
	normalized, err := normalizeCheckRunInput(input, creatorDID, now.UTC())
	if err != nil {
		return CheckRun{}, false, err
	}
	fingerprint, err := requestHash(normalized)
	if err != nil {
		return CheckRun{}, false, err
	}
	id, err := uuid.NewV7()
	if err != nil {
		return CheckRun{}, false, fmt.Errorf("generate check run ID: %w", err)
	}
	now = now.UTC()
	row, err := service.queries.CreateCheckRun(ctx, dbgen.CreateCheckRunParams{
		ID: pgUUID(id), RepositoryID: pgUUID(uuid.UUID(repositoryID)), CommitSha: normalized.CommitSHA,
		Name: normalized.Name, ExternalID: normalized.ExternalID, CreatorDid: creatorDID,
		Status: string(normalized.Status), Conclusion: optionalConclusion(normalized.Conclusion), DetailsUrl: optionalText(normalized.DetailsURL),
		OutputTitle: normalized.OutputTitle, OutputSummary: normalized.OutputSummary, CreateRequestHash: fingerprint,
		StartedAt: optionalTime(normalized.StartedAt), CompletedAt: optionalTime(normalized.CompletedAt),
		CreatedAt: pgTime(now), ExpiresAt: pgTime(now.Add(Retention)),
	})
	if err == nil {
		return checkRunFromRow(row), true, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return CheckRun{}, false, fmt.Errorf("create check run: %w", err)
	}
	existing, err := service.queries.GetCheckRunByExternalID(ctx, dbgen.GetCheckRunByExternalIDParams{RepositoryID: pgUUID(uuid.UUID(repositoryID)), CreatorDid: creatorDID, ExternalID: normalized.ExternalID})
	if err != nil {
		return CheckRun{}, false, fmt.Errorf("resolve check run replay: %w", err)
	}
	if !bytes.Equal(existing.CreateRequestHash, fingerprint) {
		return CheckRun{}, false, fmt.Errorf("%w: external_id was already used with a different check run", ErrConflict)
	}
	return checkRunFromRow(existing), false, nil
}

func (service *Service) GetCheckRun(ctx context.Context, repositoryID repository.ID, id uuid.UUID) (CheckRun, error) {
	row, err := service.queries.GetCheckRun(ctx, dbgen.GetCheckRunParams{ID: pgUUID(id), RepositoryID: pgUUID(uuid.UUID(repositoryID))})
	if errors.Is(err, pgx.ErrNoRows) {
		return CheckRun{}, ErrNotFound
	}
	if err != nil {
		return CheckRun{}, fmt.Errorf("get check run: %w", err)
	}
	return checkRunFromRow(row), nil
}

func (service *Service) PageCheckRuns(ctx context.Context, repositoryID repository.ID, commitSHA string, after *uuid.UUID, limit int) (Page[CheckRun], error) {
	if err := validatePage(commitSHA, limit); err != nil {
		return Page[CheckRun]{}, err
	}
	rows, err := service.queries.PageCheckRuns(ctx, dbgen.PageCheckRunsParams{RepositoryID: pgUUID(uuid.UUID(repositoryID)), CommitSha: commitSHA, AfterID: optionalUUID(after), PageLimit: int32(limit + 1)})
	if err != nil {
		return Page[CheckRun]{}, fmt.Errorf("page check runs: %w", err)
	}
	page := Page[CheckRun]{Items: make([]CheckRun, min(len(rows), limit))}
	for index := range page.Items {
		page.Items[index] = checkRunFromRow(rows[index])
	}
	if len(rows) > limit {
		next := page.Items[len(page.Items)-1].ID
		page.NextCursor = &next
	}
	return page, nil
}

func (service *Service) UpdateCheckRun(ctx context.Context, repositoryID repository.ID, creatorDID string, id uuid.UUID, input CheckRunUpdate, now time.Time) (CheckRun, bool, error) {
	current, err := service.GetCheckRun(ctx, repositoryID, id)
	if err != nil {
		return CheckRun{}, false, err
	}
	if current.CreatorDID != creatorDID {
		return CheckRun{}, false, ErrNotFound
	}
	normalized, err := normalizeCheckRunUpdate(current, input, now.UTC())
	if err != nil {
		return CheckRun{}, false, err
	}
	if checkRunMatches(current, normalized) {
		return current, false, nil
	}
	if current.Version != normalized.ExpectedVersion {
		return CheckRun{}, false, fmt.Errorf("%w: expected check run version %d, current version is %d", ErrConflict, normalized.ExpectedVersion, current.Version)
	}
	row, err := service.queries.UpdateCheckRun(ctx, dbgen.UpdateCheckRunParams{
		Status: string(normalized.Status), Conclusion: optionalConclusion(normalized.Conclusion), DetailsUrl: optionalText(normalized.DetailsURL),
		OutputTitle: normalized.OutputTitle, OutputSummary: normalized.OutputSummary,
		StartedAt: optionalTime(normalized.StartedAt), CompletedAt: optionalTime(normalized.CompletedAt),
		UpdatedAt: pgTime(now.UTC()), ExpiresAt: pgTime(now.UTC().Add(Retention)), ID: pgUUID(id),
		RepositoryID: pgUUID(uuid.UUID(repositoryID)), CreatorDid: creatorDID, ExpectedVersion: normalized.ExpectedVersion,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return CheckRun{}, false, ErrConflict
	}
	if err != nil {
		return CheckRun{}, false, fmt.Errorf("update check run: %w", err)
	}
	return checkRunFromRow(row), true, nil
}

func normalizeStatusInput(commitSHA, creatorDID string, input StatusInput) (StatusInput, error) {
	input.Context = strings.TrimSpace(input.Context)
	input.Description = strings.TrimSpace(input.Description)
	input.ExternalID = strings.TrimSpace(input.ExternalID)
	if !shaPattern.MatchString(commitSHA) {
		return StatusInput{}, fmt.Errorf("%w: commit SHA must be a full lowercase object ID", ErrValidation)
	}
	if strings.TrimSpace(creatorDID) == "" {
		return StatusInput{}, fmt.Errorf("%w: creator DID is required", ErrValidation)
	}
	if err := validateName("context", input.Context, 100); err != nil {
		return StatusInput{}, err
	}
	if input.State != StatePending && input.State != StateSuccess && input.State != StateFailure && input.State != StateError {
		return StatusInput{}, fmt.Errorf("%w: unsupported status state", ErrValidation)
	}
	if len(input.Description) > 140 {
		return StatusInput{}, fmt.Errorf("%w: description must not exceed 140 characters", ErrValidation)
	}
	if err := validateName("external_id", input.ExternalID, 255); err != nil {
		return StatusInput{}, err
	}
	if err := validateURL(input.TargetURL); err != nil {
		return StatusInput{}, err
	}
	return input, nil
}

func normalizeCheckRunInput(input CheckRunInput, creatorDID string, now time.Time) (CheckRunInput, error) {
	input.Name = strings.TrimSpace(input.Name)
	input.ExternalID = strings.TrimSpace(input.ExternalID)
	input.OutputTitle = strings.TrimSpace(input.OutputTitle)
	if !shaPattern.MatchString(input.CommitSHA) {
		return CheckRunInput{}, fmt.Errorf("%w: commit SHA must be a full lowercase object ID", ErrValidation)
	}
	if strings.TrimSpace(creatorDID) == "" {
		return CheckRunInput{}, fmt.Errorf("%w: creator DID is required", ErrValidation)
	}
	if err := validateName("name", input.Name, 100); err != nil {
		return CheckRunInput{}, err
	}
	if err := validateName("external_id", input.ExternalID, 255); err != nil {
		return CheckRunInput{}, err
	}
	if err := validateCheckOutput(input.OutputTitle, input.OutputSummary); err != nil {
		return CheckRunInput{}, err
	}
	if err := validateURL(input.DetailsURL); err != nil {
		return CheckRunInput{}, err
	}
	started, completed, err := normalizeLifecycle(input.Status, input.Conclusion, input.StartedAt, input.CompletedAt, nil, now)
	if err != nil {
		return CheckRunInput{}, err
	}
	input.StartedAt, input.CompletedAt = started, completed
	return input, nil
}

func normalizeCheckRunUpdate(current CheckRun, input CheckRunUpdate, now time.Time) (CheckRunUpdate, error) {
	input.OutputTitle = strings.TrimSpace(input.OutputTitle)
	if input.ExpectedVersion < 1 {
		return CheckRunUpdate{}, fmt.Errorf("%w: expected_version must be positive", ErrValidation)
	}
	if err := validateCheckOutput(input.OutputTitle, input.OutputSummary); err != nil {
		return CheckRunUpdate{}, err
	}
	if err := validateURL(input.DetailsURL); err != nil {
		return CheckRunUpdate{}, err
	}
	if !validTransition(current.Status, input.Status) {
		return CheckRunUpdate{}, fmt.Errorf("%w: check run cannot transition from %s to %s", ErrConflict, current.Status, input.Status)
	}
	started, completed, err := normalizeLifecycle(input.Status, input.Conclusion, input.StartedAt, input.CompletedAt, current.StartedAt, now)
	if err != nil {
		return CheckRunUpdate{}, err
	}
	input.StartedAt, input.CompletedAt = started, completed
	return input, nil
}

func normalizeLifecycle(status CheckStatus, conclusion *Conclusion, startedAt, completedAt, priorStartedAt *time.Time, now time.Time) (*time.Time, *time.Time, error) {
	switch status {
	case CheckQueued:
		if conclusion != nil || startedAt != nil || completedAt != nil {
			return nil, nil, fmt.Errorf("%w: queued checks cannot have conclusion or execution timestamps", ErrValidation)
		}
	case CheckInProgress:
		if conclusion != nil || completedAt != nil {
			return nil, nil, fmt.Errorf("%w: in-progress checks cannot have a conclusion or completion time", ErrValidation)
		}
		if startedAt == nil {
			startedAt = priorStartedAt
		}
		if startedAt == nil {
			value := now.UTC()
			startedAt = &value
		}
	case CheckCompleted:
		if conclusion == nil || !validConclusion(*conclusion) {
			return nil, nil, fmt.Errorf("%w: completed checks require a supported conclusion", ErrValidation)
		}
		if startedAt == nil {
			startedAt = priorStartedAt
		}
		if startedAt == nil {
			value := now.UTC()
			startedAt = &value
		}
		if completedAt == nil {
			value := now.UTC()
			completedAt = &value
		}
		if completedAt.Before(*startedAt) {
			return nil, nil, fmt.Errorf("%w: completion time must not precede start time", ErrValidation)
		}
	default:
		return nil, nil, fmt.Errorf("%w: unsupported check status", ErrValidation)
	}
	return utcTime(startedAt), utcTime(completedAt), nil
}

func validTransition(from, to CheckStatus) bool {
	if from == to {
		return true
	}
	return (from == CheckQueued && (to == CheckInProgress || to == CheckCompleted)) || (from == CheckInProgress && to == CheckCompleted)
}

func validConclusion(value Conclusion) bool {
	switch value {
	case ConclusionSuccess, ConclusionFailure, ConclusionNeutral, ConclusionCancelled, ConclusionSkipped, ConclusionTimedOut, ConclusionActionRequired:
		return true
	default:
		return false
	}
}

func validatePage(commitSHA string, limit int) error {
	if !shaPattern.MatchString(commitSHA) {
		return fmt.Errorf("%w: commit SHA must be a full lowercase object ID", ErrValidation)
	}
	if limit < 1 || limit > 100 {
		return fmt.Errorf("%w: limit must be between 1 and 100", ErrValidation)
	}
	return nil
}

func validateName(field, value string, maximum int) error {
	if value == "" || len(value) > maximum || strings.ContainsAny(value, "\r\n\x00") {
		return fmt.Errorf("%w: %s must contain between 1 and %d safe characters", ErrValidation, field, maximum)
	}
	return nil
}

func validateCheckOutput(title, summary string) error {
	if len(title) > 255 || len(summary) > 65535 {
		return fmt.Errorf("%w: check output exceeds its size limit", ErrValidation)
	}
	return nil
}

func validateURL(value *string) error {
	if value == nil {
		return nil
	}
	trimmed := strings.TrimSpace(*value)
	parsed, err := url.ParseRequestURI(trimmed)
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil {
		return fmt.Errorf("%w: URLs must be absolute HTTPS URLs without user information", ErrValidation)
	}
	*value = trimmed
	return nil
}

func requestHash(value any) ([]byte, error) {
	encoded, err := json.Marshal(value)
	if err != nil {
		return nil, fmt.Errorf("encode replay fingerprint: %w", err)
	}
	hash := sha256.Sum256(encoded)
	return hash[:], nil
}

func combineState(current, candidate State) State {
	priority := map[State]int{StateSuccess: 0, StatePending: 1, StateFailure: 2, StateError: 3}
	if priority[candidate] > priority[current] {
		return candidate
	}
	return current
}

func checkRunMatches(current CheckRun, input CheckRunUpdate) bool {
	return current.Status == input.Status && equalConclusion(current.Conclusion, input.Conclusion) && equalString(current.DetailsURL, input.DetailsURL) &&
		current.OutputTitle == input.OutputTitle && current.OutputSummary == input.OutputSummary && equalTime(current.StartedAt, input.StartedAt) && equalTime(current.CompletedAt, input.CompletedAt)
}

func equalConclusion(left, right *Conclusion) bool {
	return (left == nil && right == nil) || (left != nil && right != nil && *left == *right)
}
func equalString(left, right *string) bool {
	return (left == nil && right == nil) || (left != nil && right != nil && *left == *right)
}
func equalTime(left, right *time.Time) bool {
	return (left == nil && right == nil) || (left != nil && right != nil && left.Equal(*right))
}

func statusFromRow(row dbgen.CoreCommitStatus) CommitStatus {
	return CommitStatus{ID: uuid.UUID(row.ID.Bytes), RepositoryID: repository.ID(row.RepositoryID.Bytes), CommitSHA: row.CommitSha,
		Context: row.Context, State: State(row.State), Description: row.Description, TargetURL: textPointer(row.TargetUrl),
		CreatorDID: row.CreatorDid, ExternalID: row.ExternalID, CreatedAt: row.CreatedAt.Time}
}

func checkRunFromRow(row dbgen.CoreCheckRun) CheckRun {
	value := CheckRun{ID: uuid.UUID(row.ID.Bytes), RepositoryID: repository.ID(row.RepositoryID.Bytes), CommitSHA: row.CommitSha,
		Name: row.Name, ExternalID: row.ExternalID, CreatorDID: row.CreatorDid, Status: CheckStatus(row.Status),
		DetailsURL: textPointer(row.DetailsUrl), OutputTitle: row.OutputTitle, OutputSummary: row.OutputSummary,
		Version: row.Version, StartedAt: timePointer(row.StartedAt), CompletedAt: timePointer(row.CompletedAt),
		CreatedAt: row.CreatedAt.Time, UpdatedAt: row.UpdatedAt.Time}
	if row.Conclusion.Valid {
		conclusion := Conclusion(row.Conclusion.String)
		value.Conclusion = &conclusion
	}
	return value
}

func pgUUID(value uuid.UUID) pgtype.UUID { return pgtype.UUID{Bytes: value, Valid: true} }
func optionalUUID(value *uuid.UUID) pgtype.UUID {
	if value == nil {
		return pgtype.UUID{}
	}
	return pgUUID(*value)
}
func pgTime(value time.Time) pgtype.Timestamptz {
	return pgtype.Timestamptz{Time: value.UTC(), Valid: true}
}
func optionalTime(value *time.Time) pgtype.Timestamptz {
	if value == nil {
		return pgtype.Timestamptz{}
	}
	return pgTime(*value)
}
func optionalText(value *string) pgtype.Text {
	if value == nil {
		return pgtype.Text{}
	}
	return pgtype.Text{String: *value, Valid: true}
}
func optionalConclusion(value *Conclusion) pgtype.Text {
	if value == nil {
		return pgtype.Text{}
	}
	return pgtype.Text{String: string(*value), Valid: true}
}
func textPointer(value pgtype.Text) *string {
	if !value.Valid {
		return nil
	}
	result := value.String
	return &result
}
func timePointer(value pgtype.Timestamptz) *time.Time {
	if !value.Valid {
		return nil
	}
	result := value.Time
	return &result
}
func utcTime(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	result := value.UTC()
	return &result
}
