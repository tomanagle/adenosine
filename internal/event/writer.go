// Package event persists durable work after authoritative state changes.
package event

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	dbgen "github.com/adenosine-dev/adenosine/internal/database/generated"
	"github.com/adenosine-dev/adenosine/internal/repository"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
)

var eventSHAPattern = regexp.MustCompile(`^(?:[0-9a-f]{40}|[0-9a-f]{64})$`)

// GitRefsUpdated describes one pull-request-owned target ref update.
type GitRefsUpdated struct {
	RepositoryID   repository.ID
	Ref            string
	OldSHA         string
	NewSHA         string
	HeadSHA        string
	ActorDID       string
	PullRequestURI string
	PullRequestCID string
	Strategy       string
}

// Writer persists events to the PostgreSQL outbox.
type Writer struct {
	queries *dbgen.Queries
}

// NewWriter constructs an outbox event writer.
func NewWriter(queries *dbgen.Queries) *Writer {
	return &Writer{queries: queries}
}

// GitPushReceived records successful receive-pack completion for asynchronous processing.
func (writer *Writer) GitPushReceived(ctx context.Context, repo repository.Repository) error {
	id, err := uuid.NewV7()
	if err != nil {
		return fmt.Errorf("generate push event ID: %w", err)
	}
	payload, err := json.Marshal(struct {
		RepositoryID string `json:"repository_id"`
	}{RepositoryID: repo.ID.String()})
	if err != nil {
		return fmt.Errorf("encode push event: %w", err)
	}
	now := time.Now().UTC()
	traceparent, tracestate := traceContext(ctx)
	if _, err := writer.queries.CreateOutboxEvent(ctx, dbgen.CreateOutboxEventParams{
		ID:            pgtype.UUID{Bytes: id, Valid: true},
		Type:          "git.push_received",
		AggregateType: "repository",
		AggregateID:   repo.ID.String(),
		Payload:       payload,
		CreatedAt:     pgtype.Timestamptz{Time: now, Valid: true},
		AvailableAt:   pgtype.Timestamptz{Time: now, Valid: true},
		Traceparent:   traceparent,
		Tracestate:    tracestate,
	}); err != nil {
		return fmt.Errorf("create push outbox event: %w", err)
	}
	return nil
}

// GitRefsUpdated records a merge ref update once for an exact PR CID and merge commit.
func (writer *Writer) GitRefsUpdated(ctx context.Context, input GitRefsUpdated) error {
	if err := validateGitRefsUpdated(input); err != nil {
		return err
	}
	id := uuid.NewSHA1(uuid.NameSpaceOID, []byte("git.refs_updated\x00"+input.PullRequestCID+"\x00"+input.NewSHA))
	payload, err := json.Marshal(struct {
		RepositoryID   string `json:"repository_id"`
		Ref            string `json:"ref"`
		OldSHA         string `json:"old_sha"`
		NewSHA         string `json:"new_sha"`
		HeadSHA        string `json:"head_sha"`
		ActorDID       string `json:"actor_did"`
		PullRequestURI string `json:"pull_request_uri"`
		Strategy       string `json:"strategy"`
	}{input.RepositoryID.String(), input.Ref, input.OldSHA, input.NewSHA, input.HeadSHA, input.ActorDID, input.PullRequestURI, input.Strategy})
	if err != nil {
		return fmt.Errorf("encode refs-updated event: %w", err)
	}
	now := time.Now().UTC()
	traceparent, tracestate := traceContext(ctx)
	if err := writer.queries.CreateOutboxEventIfAbsent(ctx, dbgen.CreateOutboxEventIfAbsentParams{
		ID: pgtype.UUID{Bytes: id, Valid: true}, Type: "git.refs_updated", AggregateType: "repository",
		AggregateID: input.RepositoryID.String(), Payload: payload,
		CreatedAt: pgtype.Timestamptz{Time: now, Valid: true}, AvailableAt: pgtype.Timestamptz{Time: now, Valid: true},
		Traceparent: traceparent, Tracestate: tracestate,
	}); err != nil {
		return fmt.Errorf("create refs-updated outbox event: %w", err)
	}
	return nil
}

// RepositoryActivity records a portable collaboration action for local webhook delivery.
func (writer *Writer) RepositoryActivity(ctx context.Context, eventType, subjectURI string, value any) error {
	id, err := uuid.NewV7()
	if err != nil {
		return fmt.Errorf("generate repository activity event ID: %w", err)
	}
	payload, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("encode repository activity event: %w", err)
	}
	now := time.Now().UTC()
	traceparent, tracestate := traceContext(ctx)
	if err := writer.queries.CreateRepositoryActivityEvent(ctx, dbgen.CreateRepositoryActivityEventParams{SubjectUri: subjectURI, ID: pgtype.UUID{Bytes: id, Valid: true}, Type: eventType, Payload: payload, CreatedAt: pgtype.Timestamptz{Time: now, Valid: true}, Traceparent: traceparent, Tracestate: tracestate}); err != nil {
		return fmt.Errorf("create repository activity event: %w", err)
	}
	return nil
}

// ContextFromOutbox attaches a valid persisted remote parent to the worker
// context. Missing or malformed fields deliberately leave ctx unchanged.
func ContextFromOutbox(ctx context.Context, outboxEvent dbgen.OpsOutboxEvent) context.Context {
	carrier := propagation.MapCarrier{}
	if outboxEvent.Traceparent.Valid {
		carrier.Set("traceparent", outboxEvent.Traceparent.String)
	}
	if outboxEvent.Tracestate.Valid {
		carrier.Set("tracestate", outboxEvent.Tracestate.String)
	}
	extracted := otel.GetTextMapPropagator().Extract(context.Background(), carrier)
	spanContext := trace.SpanContextFromContext(extracted)
	if !spanContext.IsValid() {
		return ctx
	}
	return trace.ContextWithRemoteSpanContext(ctx, spanContext)
}

func traceContext(ctx context.Context) (pgtype.Text, pgtype.Text) {
	carrier := propagation.MapCarrier{}
	otel.GetTextMapPropagator().Inject(ctx, carrier)
	return nullableText(carrier.Get("traceparent")), nullableText(carrier.Get("tracestate"))
}

func nullableText(value string) pgtype.Text {
	return pgtype.Text{String: value, Valid: value != ""}
}

func validateGitRefsUpdated(input GitRefsUpdated) error {
	if input.RepositoryID == (repository.ID{}) {
		return errors.New("create refs-updated event: repository ID is required")
	}
	if !strings.HasPrefix(input.Ref, "refs/heads/") || len(input.Ref) == len("refs/heads/") || strings.ContainsAny(input.Ref, " \t\r\n") {
		return errors.New("create refs-updated event: target ref must be a branch ref")
	}
	for name, value := range map[string]string{"old SHA": input.OldSHA, "new SHA": input.NewSHA, "head SHA": input.HeadSHA} {
		if !eventSHAPattern.MatchString(value) || strings.Trim(value, "0") == "" {
			return fmt.Errorf("create refs-updated event: %s is invalid", name)
		}
	}
	did, err := syntax.ParseDID(input.ActorDID)
	if err != nil || did.String() != input.ActorDID {
		return errors.New("create refs-updated event: actor DID is invalid")
	}
	uri, err := syntax.ParseATURI(input.PullRequestURI)
	if err != nil || uri.String() != input.PullRequestURI || uri.Collection().String() != "dev.adenosine.pullRequest" || uri.RecordKey().String() == "" {
		return errors.New("create refs-updated event: pull request URI is invalid")
	}
	uriDID, err := uri.Authority().AsDID()
	if err != nil || uriDID.String() != uri.Authority().String() {
		return errors.New("create refs-updated event: pull request URI authority is invalid")
	}
	cid, err := syntax.ParseCID(input.PullRequestCID)
	if err != nil || cid.String() != input.PullRequestCID {
		return errors.New("create refs-updated event: pull request CID is invalid")
	}
	if input.Strategy != "merge-commit" && input.Strategy != "squash" {
		return errors.New("create refs-updated event: strategy is invalid")
	}
	return nil
}
