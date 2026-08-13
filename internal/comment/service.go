// Package comment coordinates projected issue comment reads and authoritative writes.
package comment

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/adenosine-dev/adenosine/internal/issue"
	"github.com/bluesky-social/indigo/atproto/syntax"
)

// CreateInput contains comment-author-owned content.
type CreateInput struct {
	IssueURI  string
	ParentURI string
	Body      string
}

// ProjectedComment is a current comment and its local indexing time.
type ProjectedComment struct {
	issue.Comment
	IndexedAt time.Time
}

// Projection is the bounded visible comment projection for one issue.
type Projection struct {
	CommentCount int64
	Comments     []ProjectedComment
}

type parentTarget struct {
	Ref      issue.StrongRef
	IssueURI string
}

type projectionStore interface {
	GetProjection(context.Context, string, string, int) (Projection, error)
	GetIssueTarget(context.Context, string) (issue.StrongRef, error)
	GetParentTarget(context.Context, string) (parentTarget, error)
}

type paginatedProjectionStore interface {
	GetProjectionAfter(context.Context, string, string, int, string) (Projection, error)
}

// Publisher writes authoritative comment records to the authenticated PDS.
type Publisher interface {
	CreateIssueComment(context.Context, string, string, issue.CommentRecord) (issue.Comment, error)
	DeleteIssueComment(context.Context, string, string) error
}

type clock interface{ Now() time.Time }

// Service coordinates projected reads and asynchronous authoritative writes.
type Service struct {
	store     projectionStore
	publisher Publisher
	clock     clock
}

// NewService constructs the comment application service.
func NewService(store projectionStore, publisher Publisher, clock clock) *Service {
	return &Service{store: store, publisher: publisher, clock: clock}
}

// Get returns at most 100 chronological comments visible to the optional viewer.
func (service *Service) Get(ctx context.Context, issueURI, viewerDID string) (Projection, error) {
	if err := validateATURI(issueURI, issue.Collection, "issueURI"); err != nil {
		return Projection{}, err
	}
	if viewerDID != "" {
		if err := validateDID(viewerDID, "viewerDID"); err != nil {
			return Projection{}, err
		}
	}
	projection, err := service.store.GetProjection(ctx, issueURI, viewerDID, 100)
	if err != nil {
		return Projection{}, projectionError("get comment projection", err)
	}
	if projection.Comments == nil {
		projection.Comments = []ProjectedComment{}
	}
	return projection, nil
}

// GetPage returns a keyset page after the exact comment URI supplied by a validated API cursor.
func (service *Service) GetPage(ctx context.Context, issueURI, viewerDID string, limit int, afterURI string) (Projection, error) {
	if err := validateATURI(issueURI, issue.Collection, "issueURI"); err != nil {
		return Projection{}, err
	}
	if viewerDID != "" {
		if err := validateDID(viewerDID, "viewerDID"); err != nil {
			return Projection{}, err
		}
	}
	if limit < 1 || limit > 101 {
		return Projection{}, &issue.ValidationError{Field: "limit", Problem: "must be between 1 and 101"}
	}
	store, ok := service.store.(paginatedProjectionStore)
	if !ok {
		return service.Get(ctx, issueURI, viewerDID)
	}
	projection, err := store.GetProjectionAfter(ctx, issueURI, viewerDID, limit, afterURI)
	if err != nil {
		return Projection{}, projectionError("get comment projection page", err)
	}
	if projection.Comments == nil {
		projection.Comments = []ProjectedComment{}
	}
	return projection, nil
}

// Create publishes a comment without synchronously changing the local projection.
func (service *Service) Create(ctx context.Context, authorDID string, input CreateInput) (issue.Comment, error) {
	if err := validateDID(authorDID, "authorDID"); err != nil {
		return issue.Comment{}, err
	}
	if err := validateATURI(input.IssueURI, issue.Collection, "issueURI"); err != nil {
		return issue.Comment{}, err
	}
	if input.ParentURI != "" {
		if err := validateATURI(input.ParentURI, issue.CommentCollection, "parentURI"); err != nil {
			return issue.Comment{}, err
		}
	}
	subject, err := service.store.GetIssueTarget(ctx, input.IssueURI)
	if err != nil {
		return issue.Comment{}, projectionError("get comment issue target", err)
	}

	var parent *issue.StrongRef
	if input.ParentURI != "" {
		target, targetErr := service.store.GetParentTarget(ctx, input.ParentURI)
		if targetErr != nil {
			return issue.Comment{}, projectionError("get comment parent target", targetErr)
		}
		if target.IssueURI != input.IssueURI {
			return issue.Comment{}, &issue.ValidationError{Field: "parentURI", Problem: "must belong to the same issue"}
		}
		parent = &target.Ref
	}

	now := service.clock.Now().UTC()
	record := issue.CommentRecord{Subject: subject, Parent: parent, Body: input.Body, CreatedAt: now, UpdatedAt: now}
	if err := record.Validate(); err != nil {
		return issue.Comment{}, err
	}
	rkey, err := issue.RandomRecordKey()
	if err != nil {
		return issue.Comment{}, fmt.Errorf("create comment record key: %w", err)
	}
	return service.publisher.CreateIssueComment(ctx, authorDID, rkey, record)
}

// Delete delegates authority checks and compare-and-swap deletion to the publisher.
func (service *Service) Delete(ctx context.Context, authorDID, commentURI string) error {
	if err := validateDID(authorDID, "authorDID"); err != nil {
		return err
	}
	if err := validateATURI(commentURI, issue.CommentCollection, "commentURI"); err != nil {
		return err
	}
	return service.publisher.DeleteIssueComment(ctx, authorDID, commentURI)
}

func validateDID(value, field string) error {
	did, err := syntax.ParseDID(value)
	if err != nil || did.String() != value {
		return &issue.ValidationError{Field: field, Problem: "must be a canonical AT Protocol DID"}
	}
	return nil
}

func validateATURI(value, collection, field string) error {
	uri, err := syntax.ParseATURI(value)
	if err != nil || uri.String() != value || uri.Collection().String() != collection || uri.RecordKey().String() == "" {
		return &issue.ValidationError{Field: field, Problem: "must be a canonical " + collection + " AT URI"}
	}
	did, err := uri.Authority().AsDID()
	if err != nil || did.String() != uri.Authority().String() {
		return &issue.ValidationError{Field: field, Problem: "must use a canonical DID authority"}
	}
	return nil
}

func projectionError(operation string, err error) error {
	if errors.Is(err, issue.ErrNotFound) {
		return issue.ErrNotFound
	}
	return fmt.Errorf("%s: %w", operation, err)
}
