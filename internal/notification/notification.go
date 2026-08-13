// Package notification derives private inbox items from indexed forge activity.
package notification

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"time"

	dbgen "github.com/adenosine-dev/adenosine/internal/database/generated"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
)

var ErrValidation = errors.New("notification validation failed")

type Notification struct {
	ID             uuid.UUID
	Kind           string
	ActorDID       string
	RepositoryURI  string
	Owner          string
	RepositorySlug string
	SubjectURI     string
	SubjectKind    string
	Title          string
	OccurredAt     time.Time
	Read           bool
}

type Page struct {
	Items      []Notification
	NextCursor string
}

type Store struct{ queries *dbgen.Queries }

func NewStore(queries *dbgen.Queries) *Store { return &Store{queries: queries} }

func (store *Store) Page(ctx context.Context, accountDID string, unreadOnly bool, cursor string, limit int) (Page, error) {
	if limit < 1 || limit > 100 {
		return Page{}, fmt.Errorf("%w: limit must be between 1 and 100", ErrValidation)
	}
	var afterTime pgtype.Timestamptz
	var afterID pgtype.UUID
	if cursor != "" {
		decodedTime, decodedID, err := decodeCursor(cursor)
		if err != nil {
			return Page{}, err
		}
		afterTime, afterID = pgtype.Timestamptz{Time: decodedTime, Valid: true}, pgtype.UUID{Bytes: decodedID, Valid: true}
	}
	rows, err := store.queries.PageNotifications(ctx, dbgen.PageNotificationsParams{
		AccountDid: accountDID, UnreadOnly: unreadOnly, AfterTime: afterTime, AfterID: afterID, PageLimit: int32(limit + 1),
	})
	if err != nil {
		return Page{}, fmt.Errorf("page notifications: %w", err)
	}
	values := make([]Notification, min(len(rows), limit))
	for index := range values {
		row := rows[index]
		values[index] = Notification{ID: uuid.UUID(row.ID.Bytes), Kind: row.Kind, ActorDID: row.ActorDid, RepositoryURI: row.RepositoryUri, Owner: row.Owner, RepositorySlug: row.RepositorySlug, SubjectURI: row.SubjectUri, SubjectKind: row.SubjectKind, Title: row.Title, OccurredAt: row.OccurredAt.Time, Read: row.Read}
	}
	page := Page{Items: values}
	if len(rows) > limit && len(values) > 0 {
		page.NextCursor = encodeCursor(values[len(values)-1].OccurredAt, values[len(values)-1].ID)
	}
	return page, nil
}

func (store *Store) SetRead(ctx context.Context, accountDID string, id uuid.UUID, read bool, now time.Time) error {
	var readAt pgtype.Timestamptz
	if read {
		readAt = pgtype.Timestamptz{Time: now.UTC(), Valid: true}
	}
	if err := store.queries.PutNotificationReadState(ctx, dbgen.PutNotificationReadStateParams{AccountDid: accountDID, NotificationKey: pgtype.UUID{Bytes: id, Valid: true}, ReadAt: readAt, UpdatedAt: pgtype.Timestamptz{Time: now.UTC(), Valid: true}}); err != nil {
		return fmt.Errorf("set notification read state: %w", err)
	}
	return nil
}

func (store *Store) Dismiss(ctx context.Context, accountDID string, id uuid.UUID, now time.Time) error {
	if err := store.queries.DismissNotification(ctx, dbgen.DismissNotificationParams{AccountDid: accountDID, NotificationKey: pgtype.UUID{Bytes: id, Valid: true}, DismissedAt: pgtype.Timestamptz{Time: now.UTC(), Valid: true}}); err != nil {
		return fmt.Errorf("dismiss notification: %w", err)
	}
	return nil
}

func encodeCursor(occurredAt time.Time, id uuid.UUID) string {
	return base64.RawURLEncoding.EncodeToString([]byte(occurredAt.UTC().Format(time.RFC3339Nano) + "\n" + id.String()))
}

func decodeCursor(value string) (time.Time, uuid.UUID, error) {
	if len(value) > 256 {
		return time.Time{}, uuid.Nil, ErrValidation
	}
	decoded, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil {
		return time.Time{}, uuid.Nil, fmt.Errorf("%w: cursor is invalid", ErrValidation)
	}
	parts := strings.Split(string(decoded), "\n")
	if len(parts) != 2 {
		return time.Time{}, uuid.Nil, fmt.Errorf("%w: cursor is invalid", ErrValidation)
	}
	occurredAt, timeErr := time.Parse(time.RFC3339Nano, parts[0])
	id, idErr := uuid.Parse(parts[1])
	if timeErr != nil || idErr != nil {
		return time.Time{}, uuid.Nil, fmt.Errorf("%w: cursor is invalid", ErrValidation)
	}
	return occurredAt, id, nil
}
