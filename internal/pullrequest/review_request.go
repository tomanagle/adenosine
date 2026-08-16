package pullrequest

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/adenosine-dev/adenosine/internal/repository"
)

// ProjectedReviewRequest is one live target-authoritative request attached to
// the exact current pull request CID.
type ProjectedReviewRequest struct {
	ReviewRequest
	IndexedAt time.Time
}

type ReviewRequestPage struct {
	Items      []ProjectedReviewRequest
	NextCursor string
}

type ReviewRequestInput struct {
	PullRequestURI string
	ReviewerDID    string
}

type reviewRequestProjectionStore interface {
	PageReviewRequests(context.Context, string, string, time.Time, string, int) ([]ProjectedReviewRequest, error)
	ReviewRequestModerationAllowed(context.Context, string, string, string, string) (bool, error)
}

type reviewRequestPublisher interface {
	PutPullRequestReviewRequest(context.Context, string, ReviewRequestRecord) (ReviewRequest, error)
	DeletePullRequestReviewRequest(context.Context, string, string, string) error
}

type repositoryReader interface {
	CanReadRepository(context.Context, string, repository.ID) (bool, error)
}

// ReviewRequests returns an efficient keyset page for the exact current PR observation.
func (service *Service) ReviewRequests(ctx context.Context, pullRequestURI, viewerDID, cursor string, limit int) (ReviewRequestPage, error) {
	if _, err := validateATURI(pullRequestURI, Collection, "pull_request_uri"); err != nil {
		return ReviewRequestPage{}, err
	}
	if viewerDID != "" {
		if err := validateDID(viewerDID, "viewer"); err != nil {
			return ReviewRequestPage{}, err
		}
	}
	if limit < 1 || limit > 100 {
		return ReviewRequestPage{}, &ValidationError{Field: "limit", Problem: "must be between 1 and 100"}
	}
	afterTime, afterURI, err := decodeReviewRequestCursor(cursor)
	if err != nil {
		return ReviewRequestPage{}, err
	}
	store, ok := service.store.(reviewRequestProjectionStore)
	if !ok {
		return ReviewRequestPage{}, errors.New("review request projection is unavailable")
	}
	values, err := store.PageReviewRequests(ctx, pullRequestURI, viewerDID, afterTime, afterURI, limit+1)
	if err != nil {
		return ReviewRequestPage{}, projectionError("page projected review requests", err)
	}
	page := ReviewRequestPage{Items: values}
	if len(page.Items) > limit {
		page.Items = page.Items[:limit]
		last := page.Items[len(page.Items)-1]
		page.NextCursor = encodeReviewRequestCursor(last.UpdatedAt, last.URI)
	}
	if page.Items == nil {
		page.Items = []ProjectedReviewRequest{}
	}
	return page, nil
}

// PutReviewRequest publishes one idempotent target-owner record after local
// triage, visibility, block, and hide policy checks.
func (service *Service) PutReviewRequest(ctx context.Context, actorDID string, input ReviewRequestInput) (ReviewRequest, error) {
	if err := validateDID(actorDID, "actor"); err != nil {
		return ReviewRequest{}, err
	}
	if err := validateDID(input.ReviewerDID, "reviewer"); err != nil {
		return ReviewRequest{}, err
	}
	target, err := service.reviewRequestTarget(ctx, actorDID, input)
	if err != nil {
		return ReviewRequest{}, err
	}
	reader, ok := service.authorizer.(repositoryReader)
	if !ok {
		return ReviewRequest{}, ErrPermissionDenied
	}
	visible, err := reader.CanReadRepository(ctx, input.ReviewerDID, *target.RepositoryID)
	if err != nil {
		return ReviewRequest{}, fmt.Errorf("authorize requested reviewer visibility: %w", err)
	}
	store, ok := service.store.(reviewRequestProjectionStore)
	if !ok {
		return ReviewRequest{}, errors.New("review request projection is unavailable")
	}
	moderationAllowed, err := store.ReviewRequestModerationAllowed(ctx, actorDID, input.ReviewerDID, input.PullRequestURI, target.TargetRepository.URI)
	if err != nil {
		return ReviewRequest{}, fmt.Errorf("apply review request moderation: %w", err)
	}
	if !visible || !moderationAllowed {
		return ReviewRequest{}, ErrPermissionDenied
	}
	now := service.clock.Now().UTC()
	record := ReviewRequestRecord{
		Subject: target.Subject, TargetRepository: target.TargetRepository, ReviewerDID: input.ReviewerDID,
		RequestedByDID: actorDID, CreatedAt: now, UpdatedAt: now,
	}
	if err := record.Validate(); err != nil {
		return ReviewRequest{}, err
	}
	publisher, ok := service.publisher.(reviewRequestPublisher)
	if !ok {
		return ReviewRequest{}, errors.New("review request publisher is unavailable")
	}
	ownerDID, err := RepositoryOwnerDID(target.TargetRepository.URI)
	if err != nil {
		return ReviewRequest{}, err
	}
	return publisher.PutPullRequestReviewRequest(ctx, ownerDID, record)
}

// DeleteReviewRequest cancels the deterministic request slot. Missing records
// are treated as already cancelled by the publication boundary.
func (service *Service) DeleteReviewRequest(ctx context.Context, actorDID string, input ReviewRequestInput) error {
	if err := validateDID(actorDID, "actor"); err != nil {
		return err
	}
	if err := validateDID(input.ReviewerDID, "reviewer"); err != nil {
		return err
	}
	target, err := service.reviewRequestTarget(ctx, actorDID, input)
	if err != nil {
		return err
	}
	publisher, ok := service.publisher.(reviewRequestPublisher)
	if !ok {
		return errors.New("review request publisher is unavailable")
	}
	ownerDID, err := RepositoryOwnerDID(target.TargetRepository.URI)
	if err != nil {
		return err
	}
	return publisher.DeletePullRequestReviewRequest(ctx, ownerDID, input.PullRequestURI, input.ReviewerDID)
}

func (service *Service) reviewRequestTarget(ctx context.Context, actorDID string, input ReviewRequestInput) (statusTarget, error) {
	if _, err := validateATURI(input.PullRequestURI, Collection, "pull_request_uri"); err != nil {
		return statusTarget{}, err
	}
	pull, err := service.store.Get(ctx, input.PullRequestURI)
	if err != nil {
		return statusTarget{}, projectionError("get review request pull request", err)
	}
	if pull.State != StateOpen {
		return statusTarget{}, &ConflictError{Err: errors.New("pull request is not open")}
	}
	target, err := service.store.GetStatusTarget(ctx, input.PullRequestURI)
	if err != nil {
		return statusTarget{}, projectionError("get review request target", err)
	}
	if target.RepositoryID == nil {
		return statusTarget{}, ErrNotFound
	}
	triager, ok := service.authorizer.(repositoryTriager)
	if !ok {
		return statusTarget{}, ErrPermissionDenied
	}
	allowed, err := triager.CanTriageRepository(ctx, actorDID, *target.RepositoryID)
	if err != nil {
		return statusTarget{}, fmt.Errorf("authorize review request: %w", err)
	}
	if !allowed {
		return statusTarget{}, ErrPermissionDenied
	}
	return target, nil
}

func encodeReviewRequestCursor(updatedAt time.Time, uri string) string {
	return base64.RawURLEncoding.EncodeToString([]byte(updatedAt.UTC().Format(time.RFC3339Nano) + "\n" + uri))
}

func decodeReviewRequestCursor(cursor string) (time.Time, string, error) {
	if cursor == "" {
		return time.Time{}, "", nil
	}
	if len(cursor) > 2048 {
		return time.Time{}, "", &ValidationError{Field: "cursor", Problem: "is invalid"}
	}
	decoded, err := base64.RawURLEncoding.DecodeString(cursor)
	if err != nil {
		return time.Time{}, "", &ValidationError{Field: "cursor", Problem: "is invalid"}
	}
	parts := strings.Split(string(decoded), "\n")
	if len(parts) != 2 {
		return time.Time{}, "", &ValidationError{Field: "cursor", Problem: "is invalid"}
	}
	updatedAt, err := time.Parse(time.RFC3339Nano, parts[0])
	if err != nil {
		return time.Time{}, "", &ValidationError{Field: "cursor", Problem: "is invalid"}
	}
	if _, err := validateATURI(parts[1], ReviewRequestCollection, "cursor"); err != nil {
		return time.Time{}, "", &ValidationError{Field: "cursor", Problem: "is invalid"}
	}
	return updatedAt, parts[1], nil
}
