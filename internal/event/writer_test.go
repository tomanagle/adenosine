package event

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	dbgen "github.com/adenosine-dev/adenosine/internal/database/generated"
	"github.com/adenosine-dev/adenosine/internal/repository"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
)

type eventDB struct {
	queries []string
	args    [][]any
	err     error
}

func (db *eventDB) Exec(_ context.Context, query string, arguments ...any) (pgconn.CommandTag, error) {
	db.queries = append(db.queries, query)
	db.args = append(db.args, arguments)
	return pgconn.NewCommandTag("INSERT 0 1"), db.err
}
func (*eventDB) Query(context.Context, string, ...any) (pgx.Rows, error) {
	return nil, errors.New("unexpected Query")
}
func (*eventDB) QueryRow(context.Context, string, ...any) pgx.Row { return eventRow{} }

type eventRow struct{}

func (eventRow) Scan(...any) error { return errors.New("unexpected QueryRow") }

func TestGitRefsUpdatedPayloadAndIdempotentIdentity(t *testing.T) {
	t.Parallel()
	testCases := []struct{ name string }{{name: "payload and idempotent identity"}}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			db := &eventDB{}
			writer := NewWriter(dbgen.New(db))
			input := validRefsUpdated()
			if err := writer.GitRefsUpdated(context.Background(), input); err != nil {
				t.Fatalf("GitRefsUpdated() error = %v", err)
			}
			if err := writer.GitRefsUpdated(context.Background(), input); err != nil {
				t.Fatalf("GitRefsUpdated() retry error = %v", err)
			}
			if len(db.args) != 2 || db.args[0][0] != db.args[1][0] || !strings.Contains(db.queries[0], "ON CONFLICT (id) DO NOTHING") {
				t.Fatalf("outbox retries are not idempotent: queries=%d IDs=%v/%v", len(db.args), db.args[0][0], db.args[1][0])
			}
			if db.args[0][1] != "git.refs_updated" || db.args[0][2] != "repository" || db.args[0][3] != input.RepositoryID.String() {
				t.Fatalf("event metadata = %#v", db.args[0][1:4])
			}
			var payload map[string]string
			if err := json.Unmarshal(db.args[0][4].([]byte), &payload); err != nil {
				t.Fatalf("decode payload: %v", err)
			}
			want := map[string]string{"repository_id": input.RepositoryID.String(), "ref": input.Ref, "old_sha": input.OldSHA, "new_sha": input.NewSHA, "head_sha": input.HeadSHA, "actor_did": input.ActorDID, "pull_request_uri": input.PullRequestURI, "strategy": input.Strategy}
			for key, value := range want {
				if payload[key] != value {
					t.Errorf("payload[%q] = %q, want %q", key, payload[key], value)
				}
			}
			firstID := db.args[0][0].(pgtype.UUID)
			changed := input
			changed.NewSHA = strings.Repeat("d", 40)
			if err := writer.GitRefsUpdated(context.Background(), changed); err != nil {
				t.Fatalf("changed GitRefsUpdated() error = %v", err)
			}
			if firstID == db.args[2][0].(pgtype.UUID) {
				t.Fatal("different merge commit reused event ID")
			}
		})
	}
}

func TestRepositoryEventUsesRepositoryAggregate(t *testing.T) {
	t.Parallel()
	testCases := []struct{ name string }{{name: "status event"}}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			db := &eventDB{}
			repositoryID := repository.ID(uuid.MustParse("0198a851-2a89-7ae2-a370-dc68883e3af1"))
			if err := NewWriter(dbgen.New(db)).RepositoryEvent(context.Background(), repositoryID, "status.created", map[string]string{"context": "ci/test"}); err != nil {
				t.Fatalf("RepositoryEvent() error = %v", err)
			}
			if len(db.args) != 1 || db.args[0][1] != "status.created" || db.args[0][2] != "repository" || db.args[0][3] != repositoryID.String() {
				t.Fatalf("event metadata = %#v", db.args)
			}
		})
	}
}

func TestGitRefsUpdatedValidationAndStoreErrors(t *testing.T) {
	t.Parallel()
	testCases := []struct {
		name     string
		mutate   func(*GitRefsUpdated)
		storeErr error
	}{
		{name: "repository", mutate: func(input *GitRefsUpdated) { input.RepositoryID = repository.ID{} }},
		{name: "ref", mutate: func(input *GitRefsUpdated) { input.Ref = "refs/tags/main" }},
		{name: "sha", mutate: func(input *GitRefsUpdated) { input.NewSHA = "secret" }},
		{name: "actor", mutate: func(input *GitRefsUpdated) { input.ActorDID = "alice" }},
		{name: "URI", mutate: func(input *GitRefsUpdated) { input.PullRequestURI = "at://did:plc:x/dev.adenosine.issue/x" }},
		{name: "CID", mutate: func(input *GitRefsUpdated) { input.PullRequestCID = "bad" }},
		{name: "strategy", mutate: func(input *GitRefsUpdated) { input.Strategy = "rebase" }},
		{name: "store error", storeErr: errors.New("database down")},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			input := validRefsUpdated()
			if testCase.mutate != nil {
				testCase.mutate(&input)
			}
			db := &eventDB{err: testCase.storeErr}
			err := NewWriter(dbgen.New(db)).GitRefsUpdated(context.Background(), input)
			if testCase.storeErr != nil {
				if !errors.Is(err, testCase.storeErr) || !strings.Contains(err.Error(), "create refs-updated outbox event") {
					t.Fatalf("store error = %v", err)
				}
				return
			}
			if err == nil || len(db.args) != 0 {
				t.Fatalf("GitRefsUpdated() error/calls = %v/%d", err, len(db.args))
			}
		})
	}
}

func TestOutboxTraceContext(t *testing.T) {
	previousPropagator := otel.GetTextMapPropagator()
	otel.SetTextMapPropagator(propagation.TraceContext{})
	defer otel.SetTextMapPropagator(previousPropagator)
	provider := sdktrace.NewTracerProvider(sdktrace.WithSampler(sdktrace.AlwaysSample()))
	defer func() { _ = provider.Shutdown(context.Background()) }()
	state, err := trace.ParseTraceState("vendor=value")
	if err != nil {
		t.Fatal(err)
	}
	parent := trace.NewSpanContext(trace.SpanContextConfig{
		TraceID: trace.TraceID{1}, SpanID: trace.SpanID{2}, TraceFlags: trace.FlagsSampled, TraceState: state, Remote: true,
	})
	ctx, span := provider.Tracer("test").Start(trace.ContextWithRemoteSpanContext(context.Background(), parent), "request")
	defer span.End()

	testCases := []struct {
		name      string
		outbox    dbgen.OpsOutboxEvent
		wantValid bool
		wantState string
	}{
		{name: "valid remote context", outbox: dbgen.OpsOutboxEvent{
			Traceparent: pgtype.Text{String: "00-00000000000000000000000000000001-0000000000000002-01", Valid: true},
			Tracestate:  pgtype.Text{String: "vendor=value", Valid: true},
		}, wantValid: true, wantState: "vendor=value"},
		{name: "malformed context falls back", outbox: dbgen.OpsOutboxEvent{Traceparent: pgtype.Text{String: "secret", Valid: true}}},
		{name: "missing context falls back"},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			base := context.Background()
			extracted := ContextFromOutbox(base, testCase.outbox)
			got := trace.SpanContextFromContext(extracted)
			if got.IsValid() != testCase.wantValid || got.TraceState().String() != testCase.wantState {
				t.Fatalf("span context valid/state = %t/%q", got.IsValid(), got.TraceState().String())
			}
		})
	}

	db := &eventDB{}
	if err := NewWriter(dbgen.New(db)).GitRefsUpdated(ctx, validRefsUpdated()); err != nil {
		t.Fatalf("GitRefsUpdated() error = %v", err)
	}
	traceparent := db.args[0][7].(pgtype.Text)
	tracestate := db.args[0][8].(pgtype.Text)
	if !traceparent.Valid || !strings.HasPrefix(traceparent.String, "00-"+trace.SpanContextFromContext(ctx).TraceID().String()+"-") || tracestate.String != "vendor=value" {
		t.Fatalf("persisted context = %q / %q", traceparent.String, tracestate.String)
	}
}

func validRefsUpdated() GitRefsUpdated {
	return GitRefsUpdated{
		RepositoryID: repository.ID(uuid.MustParse("11111111-2222-3333-4444-555555555555")),
		Ref:          "refs/heads/main", OldSHA: strings.Repeat("a", 40), NewSHA: strings.Repeat("b", 40), HeadSHA: strings.Repeat("c", 40),
		ActorDID: "did:plc:alice", PullRequestURI: "at://did:plc:bob/dev.adenosine.pullRequest/pr", PullRequestCID: "bafybeigdyrzt5sfp7udm7hu76uh7y26nf3efuylqabf3oclgtqy55fbzdi", Strategy: "merge-commit",
	}
}
