package triage

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	dbgen "github.com/adenosine-dev/adenosine/internal/database/generated"
	"github.com/adenosine-dev/adenosine/internal/repository"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

// PostgresStore reads effective triage state from the local network projection.
type PostgresStore struct{ queries *dbgen.Queries }

// NewPostgresStore constructs a PostgreSQL triage projection store.
func NewPostgresStore(queries *dbgen.Queries) *PostgresStore { return &PostgresStore{queries: queries} }

func (store *PostgresStore) ResolveRepository(ctx context.Context, route RepositoryRoute) (RepositoryTarget, error) {
	row, err := store.queries.ResolveTriageRepository(ctx, dbgen.ResolveTriageRepositoryParams{RepositoryOwner: route.Owner, RepositorySlug: route.Slug})
	if errors.Is(err, pgx.ErrNoRows) {
		return RepositoryTarget{}, ErrNotFound
	}
	if err != nil {
		return RepositoryTarget{}, fmt.Errorf("resolve triage repository: %w", err)
	}
	return RepositoryTarget{ID: repository.ID(row.ID.Bytes), Repository: StrongRef{URI: row.Uri, CID: row.Cid.String}, OwnerDID: row.OwnerDid}, nil
}

func (store *PostgresStore) ResolveRepositoryForRead(ctx context.Context, route RepositoryRoute) (RepositoryTarget, error) {
	row, err := store.queries.ResolveReadableTriageRepository(ctx, dbgen.ResolveReadableTriageRepositoryParams{RepositoryOwner: route.Owner, RepositorySlug: route.Slug})
	if errors.Is(err, pgx.ErrNoRows) {
		return RepositoryTarget{}, ErrNotFound
	}
	if err != nil {
		return RepositoryTarget{}, fmt.Errorf("resolve readable triage repository: %w", err)
	}
	return RepositoryTarget{Repository: StrongRef{URI: row.Uri, CID: row.Cid.String}, OwnerDID: row.OwnerDid}, nil
}

func (store *PostgresStore) ListLabels(ctx context.Context, repositoryURI, viewerDID string, limit int, cursorURI string) ([]Label, error) {
	rows, err := store.queries.ListRepositoryLabels(ctx, dbgen.ListRepositoryLabelsParams{RepositoryUri: repositoryURI, ViewerDid: optionalText(viewerDID), CursorUri: optionalText(cursorURI), ResultLimit: int32(limit)})
	if err != nil {
		return nil, fmt.Errorf("query repository labels: %w", err)
	}
	values := make([]Label, len(rows))
	for index, row := range rows {
		values[index] = projectedLabel(row)
	}
	return values, nil
}

func (store *PostgresStore) GetLabel(ctx context.Context, repositoryURI, id, viewerDID string) (Label, error) {
	if err := ValidateRecordKey(id); err != nil {
		return Label{}, err
	}
	row, err := store.queries.GetRepositoryLabel(ctx, dbgen.GetRepositoryLabelParams{RepositoryUri: repositoryURI, LabelID: id, ViewerDid: optionalText(viewerDID)})
	if errors.Is(err, pgx.ErrNoRows) {
		return Label{}, ErrNotFound
	}
	if err != nil {
		return Label{}, fmt.Errorf("query repository label: %w", err)
	}
	return projectedLabel(row), nil
}

func (store *PostgresStore) ListMilestones(ctx context.Context, repositoryURI, viewerDID string, limit int, cursorURI string) ([]Milestone, error) {
	rows, err := store.queries.ListRepositoryMilestones(ctx, dbgen.ListRepositoryMilestonesParams{RepositoryUri: repositoryURI, ViewerDid: optionalText(viewerDID), CursorUri: optionalText(cursorURI), ResultLimit: int32(limit)})
	if err != nil {
		return nil, fmt.Errorf("query repository milestones: %w", err)
	}
	values := make([]Milestone, len(rows))
	for index, row := range rows {
		values[index] = projectedMilestone(row)
	}
	return values, nil
}

func (store *PostgresStore) GetMilestone(ctx context.Context, repositoryURI, id, viewerDID string) (Milestone, error) {
	if err := ValidateRecordKey(id); err != nil {
		return Milestone{}, err
	}
	row, err := store.queries.GetRepositoryMilestone(ctx, dbgen.GetRepositoryMilestoneParams{RepositoryUri: repositoryURI, MilestoneID: id, ViewerDid: optionalText(viewerDID)})
	if errors.Is(err, pgx.ErrNoRows) {
		return Milestone{}, ErrNotFound
	}
	if err != nil {
		return Milestone{}, fmt.Errorf("query repository milestone: %w", err)
	}
	return projectedMilestone(row), nil
}

func (store *PostgresStore) ResolveSubject(ctx context.Context, route RepositoryRoute, kind SubjectKind, subjectURI, viewerDID string) (subjectTarget, error) {
	return store.resolveSubject(ctx, route, kind, subjectURI, viewerDID, false)
}

func (store *PostgresStore) ResolveSubjectForRead(ctx context.Context, route RepositoryRoute, kind SubjectKind, subjectURI, viewerDID string) (subjectTarget, error) {
	return store.resolveSubject(ctx, route, kind, subjectURI, viewerDID, true)
}

func (store *PostgresStore) resolveSubject(ctx context.Context, route RepositoryRoute, kind SubjectKind, subjectURI, viewerDID string, readOnly bool) (subjectTarget, error) {
	var repositoryTarget RepositoryTarget
	var err error
	if readOnly {
		repositoryTarget, err = store.ResolveRepositoryForRead(ctx, route)
	} else {
		repositoryTarget, err = store.ResolveRepository(ctx, route)
	}
	if err != nil {
		return subjectTarget{}, err
	}
	var subject StrongRef
	switch kind {
	case SubjectIssue:
		row, queryErr := store.queries.ResolveIssueTriageSubject(ctx, dbgen.ResolveIssueTriageSubjectParams{RepositoryUri: repositoryTarget.Repository.URI, SubjectUri: subjectURI})
		if errors.Is(queryErr, pgx.ErrNoRows) {
			return subjectTarget{}, ErrNotFound
		}
		if queryErr != nil {
			return subjectTarget{}, fmt.Errorf("resolve issue triage subject: %w", queryErr)
		}
		subject = StrongRef{URI: row.Uri, CID: row.Cid.String}
	case SubjectPullRequest:
		row, queryErr := store.queries.ResolvePullRequestTriageSubject(ctx, dbgen.ResolvePullRequestTriageSubjectParams{RepositoryUri: repositoryTarget.Repository.URI, SubjectUri: subjectURI})
		if errors.Is(queryErr, pgx.ErrNoRows) {
			return subjectTarget{}, ErrNotFound
		}
		if queryErr != nil {
			return subjectTarget{}, fmt.Errorf("resolve pull request triage subject: %w", queryErr)
		}
		subject = StrongRef{URI: row.Uri, CID: row.Cid.String}
	default:
		return subjectTarget{}, &ValidationError{Field: "kind", Problem: "must be issue or pull_request"}
	}
	metadata, err := store.getMetadata(ctx, repositoryTarget.Repository.URI, kind, subject, viewerDID)
	if err != nil {
		return subjectTarget{}, err
	}
	return subjectTarget{RepositoryTarget: repositoryTarget, Subject: subject, Metadata: metadata}, nil
}

func (store *PostgresStore) ResolveLabelURIs(ctx context.Context, repositoryURI string, ids []string) ([]string, error) {
	if len(ids) == 0 {
		return []string{}, nil
	}
	for _, id := range ids {
		if err := ValidateRecordKey(id); err != nil {
			return nil, err
		}
	}
	rows, err := store.queries.ResolveRepositoryLabelURIs(ctx, dbgen.ResolveRepositoryLabelURIsParams{RepositoryUri: repositoryURI, LabelIds: ids})
	if err != nil {
		return nil, fmt.Errorf("resolve triage labels: %w", err)
	}
	resolved := make(map[string]string, len(rows))
	for _, row := range rows {
		if _, exists := resolved[row.Rkey]; !exists {
			resolved[row.Rkey] = row.Uri
		}
	}
	if len(resolved) != len(ids) {
		return nil, ErrNotFound
	}
	values := make([]string, 0, len(ids))
	for _, id := range ids {
		values = append(values, resolved[id])
	}
	slices.Sort(values)
	return values, nil
}

func (store *PostgresStore) ResolveMilestoneURI(ctx context.Context, repositoryURI, id string) (string, error) {
	if err := ValidateRecordKey(id); err != nil {
		return "", err
	}
	uri, err := store.queries.ResolveRepositoryMilestoneURI(ctx, dbgen.ResolveRepositoryMilestoneURIParams{RepositoryUri: repositoryURI, MilestoneID: id})
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrNotFound
	}
	if err != nil {
		return "", fmt.Errorf("resolve triage milestone: %w", err)
	}
	return uri, nil
}

func (store *PostgresStore) ValidateAssignees(ctx context.Context, dids []string) error {
	if len(dids) == 0 {
		return nil
	}
	count, err := store.queries.CountVisibleTriageAssignees(ctx, dids)
	if err != nil {
		return fmt.Errorf("validate triage assignees: %w", err)
	}
	if count != int64(len(dids)) {
		return ErrNotFound
	}
	return nil
}

func (store *PostgresStore) getMetadata(ctx context.Context, repositoryURI string, kind SubjectKind, subject StrongRef, viewerDID string) (*Metadata, error) {
	row, err := store.queries.GetSubjectTriage(ctx, dbgen.GetSubjectTriageParams{RepositoryUri: repositoryURI, SubjectUri: subject.URI, SubjectKind: string(kind), ViewerDid: optionalText(viewerDID)})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("query subject triage: %w", err)
	}
	metadata := projectedMetadata(row)
	labels, err := store.queries.ListSubjectTriageLabels(ctx, dbgen.ListSubjectTriageLabelsParams{MetadataUri: row.Uri, ViewerDid: optionalText(viewerDID)})
	if err != nil {
		return nil, fmt.Errorf("query subject triage labels: %w", err)
	}
	metadata.Labels = make([]Label, len(labels))
	for index, label := range labels {
		metadata.Labels[index] = projectedLabel(label)
	}
	assignees, err := store.queries.ListSubjectTriageAssignees(ctx, dbgen.ListSubjectTriageAssigneesParams{MetadataUri: row.Uri, ViewerDid: optionalText(viewerDID)})
	if err != nil {
		return nil, fmt.Errorf("query subject triage assignees: %w", err)
	}
	metadata.Assignees = make([]Assignee, len(assignees))
	for index, assignee := range assignees {
		metadata.Assignees[index] = Assignee{DID: assignee.Did, Handle: assignee.Handle.String, DisplayName: assignee.DisplayName.String}
	}
	if row.MilestoneUri.Valid {
		milestone, milestoneErr := store.queries.GetSubjectTriageMilestone(ctx, dbgen.GetSubjectTriageMilestoneParams{MetadataUri: row.Uri, ViewerDid: optionalText(viewerDID)})
		if milestoneErr != nil && !errors.Is(milestoneErr, pgx.ErrNoRows) {
			return nil, fmt.Errorf("query subject triage milestone: %w", milestoneErr)
		}
		if milestoneErr == nil {
			value := projectedMilestone(milestone)
			metadata.Milestone = &value
		}
	}
	return &metadata, nil
}

func projectedLabel(row dbgen.NetworkRepositoryLabel) Label {
	return Label{URI: row.Uri, CID: row.Cid.String, AuthorDID: row.AuthorDid, RKey: row.Rkey, LabelRecord: LabelRecord{Repository: StrongRef{URI: row.RepositoryUri, CID: row.RepositoryCid}, Name: row.Name, Color: row.Color, Description: row.Description, CreatedAt: row.RecordCreatedAt.Time, UpdatedAt: row.RecordUpdatedAt.Time}, IndexedAt: row.IndexedAt.Time}
}

func projectedMilestone(row dbgen.NetworkRepositoryMilestone) Milestone {
	return Milestone{URI: row.Uri, CID: row.Cid.String, AuthorDID: row.AuthorDid, RKey: row.Rkey, MilestoneRecord: MilestoneRecord{Repository: StrongRef{URI: row.RepositoryUri, CID: row.RepositoryCid}, Title: row.Title, Description: row.Description, State: MilestoneState(row.State), DueAt: optionalTime(row.DueAt), ClosedAt: optionalTime(row.ClosedAt), CreatedAt: row.RecordCreatedAt.Time, UpdatedAt: row.RecordUpdatedAt.Time}, IndexedAt: row.IndexedAt.Time}
}

func projectedMetadata(row dbgen.NetworkSubjectTriage) Metadata {
	return Metadata{URI: row.Uri, CID: row.Cid.String, AuthorDID: row.AuthorDid, RKey: row.Rkey, MetadataRecord: MetadataRecord{Subject: StrongRef{URI: row.SubjectUri, CID: row.SubjectCid}, Kind: SubjectKind(row.SubjectKind), Repository: StrongRef{URI: row.RepositoryUri, CID: row.RepositoryCid}, LabelURIs: append([]string(nil), row.LabelUris...), AssigneeDIDs: append([]string(nil), row.AssigneeDids...), MilestoneURI: row.MilestoneUri.String, CreatedAt: row.RecordCreatedAt.Time, UpdatedAt: row.RecordUpdatedAt.Time}, Labels: []Label{}, Assignees: []Assignee{}, IndexedAt: row.IndexedAt.Time}
}

func optionalText(value string) pgtype.Text { return pgtype.Text{String: value, Valid: value != ""} }

func optionalTime(value pgtype.Timestamptz) *time.Time {
	if !value.Valid {
		return nil
	}
	result := value.Time
	return &result
}
