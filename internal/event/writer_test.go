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

func validRefsUpdated() GitRefsUpdated {
	return GitRefsUpdated{
		RepositoryID: repository.ID(uuid.MustParse("11111111-2222-3333-4444-555555555555")),
		Ref:          "refs/heads/main", OldSHA: strings.Repeat("a", 40), NewSHA: strings.Repeat("b", 40), HeadSHA: strings.Repeat("c", 40),
		ActorDID: "did:plc:alice", PullRequestURI: "at://did:plc:bob/dev.adenosine.pullRequest/pr", PullRequestCID: "bafybeigdyrzt5sfp7udm7hu76uh7y26nf3efuylqabf3oclgtqy55fbzdi", Strategy: "merge-commit",
	}
}
