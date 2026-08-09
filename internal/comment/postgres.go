package comment

import (
	"context"
	"errors"
	"fmt"

	dbgen "github.com/adenosine-dev/adenosine/internal/database/generated"
	"github.com/adenosine-dev/adenosine/internal/issue"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

// PostgresStore reads active issue and comment projections.
type PostgresStore struct{ queries *dbgen.Queries }

// NewPostgresStore constructs a projected comment store.
func NewPostgresStore(queries *dbgen.Queries) *PostgresStore { return &PostgresStore{queries: queries} }

func (store *PostgresStore) GetProjection(ctx context.Context, issueURI, viewerDID string, limit int) (Projection, error) {
	rows, err := store.queries.ListNetworkIssueComments(ctx, dbgen.ListNetworkIssueCommentsParams{
		IssueUri: issueURI, AccountDid: pgtype.Text{String: viewerDID, Valid: viewerDID != ""}, PageSize: int32(limit),
	})
	if err != nil {
		return Projection{}, fmt.Errorf("query network issue comment projection: %w", err)
	}
	if len(rows) == 0 {
		return Projection{}, issue.ErrNotFound
	}
	projection := Projection{CommentCount: rows[0].CommentCount, Comments: []ProjectedComment{}}
	for _, row := range rows {
		if row.CommentUri == "" {
			continue
		}
		var parent *issue.StrongRef
		if row.ParentUri != "" {
			parent = &issue.StrongRef{URI: row.ParentUri, CID: row.ParentCid}
		}
		projection.Comments = append(projection.Comments, ProjectedComment{
			Comment: issue.Comment{URI: row.CommentUri, CID: row.CommentCid, AuthorDID: row.AuthorDid, CommentRecord: issue.CommentRecord{
				Subject: issue.StrongRef{URI: row.IssueUri, CID: row.IssueCid}, Parent: parent, Body: row.Body,
				CreatedAt: row.RecordCreatedAt.Time, UpdatedAt: row.RecordUpdatedAt.Time,
			}},
			IndexedAt: row.IndexedAt.Time,
		})
	}
	return projection, nil
}

func (store *PostgresStore) GetIssueTarget(ctx context.Context, issueURI string) (issue.StrongRef, error) {
	row, err := store.queries.GetNetworkIssueCommentTarget(ctx, issueURI)
	if errors.Is(err, pgx.ErrNoRows) {
		return issue.StrongRef{}, issue.ErrNotFound
	}
	if err != nil {
		return issue.StrongRef{}, fmt.Errorf("query issue comment target: %w", err)
	}
	return issue.StrongRef{URI: row.Uri, CID: row.Cid.String}, nil
}

func (store *PostgresStore) GetParentTarget(ctx context.Context, parentURI string) (parentTarget, error) {
	row, err := store.queries.GetNetworkIssueCommentParentTarget(ctx, parentURI)
	if errors.Is(err, pgx.ErrNoRows) {
		return parentTarget{}, issue.ErrNotFound
	}
	if err != nil {
		return parentTarget{}, fmt.Errorf("query issue comment parent target: %w", err)
	}
	return parentTarget{Ref: issue.StrongRef{URI: row.Uri, CID: row.Cid.String}, IssueURI: row.IssueUri}, nil
}
