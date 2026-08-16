package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base32"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/adenosine-dev/adenosine/internal/auth"
	"github.com/adenosine-dev/adenosine/internal/comment"
	"github.com/adenosine-dev/adenosine/internal/database"
	"github.com/adenosine-dev/adenosine/internal/issue"
	"github.com/adenosine-dev/adenosine/internal/moderation"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/google/uuid"
)

const (
	authorDID      = "did:plc:bbbbbbbbbbbbbbbbbbbbbbbb"
	ownerDID       = "did:plc:cccccccccccccccccccccccc"
	repositoryRKey = "0198a8512a897ae2a370dc68883e3af5"
	repositoryCID  = "bafybeigdyrzt5sfp7udm7hu76uh7y26nf3efuylqabf3oclgtqy55fbzdi"
	rootBody       = "Bob's federated root comment."
	replyBody      = "Bob's reply to the exact root observation."
	rootEventID    = 27
	replyEventID   = 28
	deleteEventID  = 29
)

var (
	repositoryURI  = "at://" + ownerDID + "/dev.adenosine.repo/" + repositoryRKey
	rootCreatedAt  = time.Date(2026, 8, 9, 15, 0, 0, 0, time.UTC)
	replyCreatedAt = time.Date(2026, 8, 9, 15, 1, 0, 0, time.UTC)
	httpClient     = &http.Client{Timeout: 10 * time.Second}
)

type sequenceClock struct {
	values []time.Time
	index  int
}

func (clock *sequenceClock) Now() time.Time {
	value := clock.values[clock.index]
	clock.index++
	return value
}

type deterministicPublisher struct {
	issue  issue.StrongRef
	parent *issue.StrongRef
	mode   string
	calls  int
}

func (publisher *deterministicPublisher) CreateIssueComment(_ context.Context, author, rkey string, record issue.CommentRecord) (issue.Comment, error) {
	if publisher.mode != "create" || author != authorDID {
		return issue.Comment{}, fmt.Errorf("unexpected comment publication mode/author %q/%q", publisher.mode, author)
	}
	if err := canonicalRKey(rkey); err != nil {
		return issue.Comment{}, err
	}
	wantBody, wantTime := rootBody, rootCreatedAt
	var wantParent *issue.StrongRef
	if publisher.calls == 1 {
		wantBody, wantTime, wantParent = replyBody, replyCreatedAt, publisher.parent
	}
	if publisher.calls > 1 || record.Subject != publisher.issue || !sameRef(record.Parent, wantParent) || record.Body != wantBody ||
		!record.CreatedAt.Equal(wantTime) || !record.UpdatedAt.Equal(wantTime) || record.CreatedAt.Location() != time.UTC || record.UpdatedAt.Location() != time.UTC {
		return issue.Comment{}, fmt.Errorf("unexpected comment record for rkey %q: %+v", rkey, record)
	}
	recordJSON, err := encodeRecord(record)
	if err != nil {
		return issue.Comment{}, err
	}
	value := issue.Comment{
		URI: "at://" + author + "/" + issue.CommentCollection + "/" + rkey,
		CID: deterministicCID(recordJSON), AuthorDID: author, CommentRecord: record,
	}
	if err := value.Validate(); err != nil {
		return issue.Comment{}, fmt.Errorf("validate deterministic comment envelope: %w", err)
	}
	publisher.calls++
	return value, nil
}

func (publisher *deterministicPublisher) DeleteIssueComment(_ context.Context, author, commentURI string) error {
	if publisher.mode != "delete" || author != authorDID || publisher.parent == nil || commentURI != publisher.parent.URI {
		return fmt.Errorf("unexpected comment deletion mode/author/URI %q/%q/%q", publisher.mode, author, commentURI)
	}
	parsed, err := syntax.ParseATURI(commentURI)
	if err != nil || parsed.String() != commentURI || parsed.Authority().String() != authorDID || parsed.Collection().String() != issue.CommentCollection {
		return fmt.Errorf("delete target is not Bob's canonical comment URI: %q", commentURI)
	}
	return canonicalRKey(parsed.RecordKey().String())
}

type unavailableIssuePublisher struct{}

func (unavailableIssuePublisher) CreateIssue(context.Context, string, string, issue.Record) (issue.Issue, error) {
	return issue.Issue{}, fmt.Errorf("issue publication is outside comment acceptance")
}

func (unavailableIssuePublisher) PutIssueStatus(context.Context, string, issue.StatusRecord) (issue.Status, error) {
	return issue.Status{}, fmt.Errorf("issue status publication is outside comment acceptance")
}

type commentPage struct {
	CommentCount int64 `json:"comment_count"`
	Data         []struct {
		URI       string  `json:"uri"`
		CID       string  `json:"cid"`
		ParentURI *string `json:"parent_uri"`
		ParentCID *string `json:"parent_cid"`
		Body      string  `json:"body"`
	} `json:"items"`
}

type rowSnapshot struct {
	total, active int64
	rootCID       string
}

func main() {
	phase := flag.String("phase", "create", "comment acceptance phase: create, moderate, or delete")
	flag.Parse()
	var err error
	switch *phase {
	case "create":
		err = create(context.Background())
	case "moderate":
		err = moderate(context.Background())
	case "delete":
		err = deleteRoot(context.Background())
	default:
		err = fmt.Errorf("unknown phase %q", *phase)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Printf("federation comment phase %s passed\n", *phase)
}

func create(ctx context.Context) error {
	db, err := database.Open(ctx, requiredEnv("DATABASE_URL_B"), nil)
	if err != nil {
		return fmt.Errorf("open B database: %w", err)
	}
	defer db.Close()
	issueTarget, err := projectedIssue(ctx, db)
	if err != nil {
		return err
	}
	publisher := &deterministicPublisher{issue: issueTarget, mode: "create"}
	service := comment.NewService(comment.NewPostgresStore(db.Queries()), publisher, &sequenceClock{values: []time.Time{rootCreatedAt, replyCreatedAt}})
	root, err := service.Create(ctx, authorDID, comment.CreateInput{IssueURI: issueTarget.URI, Body: rootBody})
	if err != nil {
		return fmt.Errorf("create root through B comment service: %w", err)
	}
	publisher.parent = &issue.StrongRef{URI: root.URI, CID: root.CID}
	rootEvent, err := createEvent(rootEventID, root)
	if err != nil {
		return err
	}
	if err := deliverEverywhere(ctx, rootEvent); err != nil {
		return fmt.Errorf("deliver root comment: %w", err)
	}
	reply, err := service.Create(ctx, authorDID, comment.CreateInput{IssueURI: issueTarget.URI, ParentURI: root.URI, Body: replyBody})
	if err != nil {
		return fmt.Errorf("create reply through B comment service: %w", err)
	}
	if reply.Parent == nil || *reply.Parent != (issue.StrongRef{URI: root.URI, CID: root.CID}) {
		return fmt.Errorf("reply did not retain exact root strongRef: %+v", reply.Parent)
	}
	replyEvent, err := createEvent(replyEventID, reply)
	if err != nil {
		return err
	}
	if err := deliverEverywhere(ctx, replyEvent); err != nil {
		return fmt.Errorf("deliver reply comment: %w", err)
	}
	return nil
}

func moderate(ctx context.Context) error {
	db, err := database.Open(ctx, requiredEnv("DATABASE_URL_A"), nil)
	if err != nil {
		return fmt.Errorf("open A database: %w", err)
	}
	defer db.Close()
	issueTarget, err := projectedIssue(ctx, db)
	if err != nil {
		return err
	}
	beforePage, err := getComments(ctx, requiredEnv("ADENOSINE_A_URL"), issueTarget.URI, "")
	if err != nil {
		return err
	}
	root, reply, err := exactThread(beforePage)
	if err != nil {
		return fmt.Errorf("A anonymous thread before moderation: %w", err)
	}
	beforeRows, err := snapshotRows(ctx, db, issueTarget.URI, root.URI)
	if err != nil {
		return err
	}

	authStore := auth.NewPostgresStore(db.Queries())
	if _, err := authStore.UpsertLogin(ctx, ownerDID, "hosted.example", time.Now()); err != nil {
		return fmt.Errorf("ensure hosted owner account: %w", err)
	}
	sessions := auth.NewSessionService(authStore, auth.SystemClock{}, auth.UUIDv7Generator{}, auth.RandomSessionSecretGenerator{}, time.Hour)
	_, plaintext, err := sessions.CreateSession(ctx, ownerDID)
	if err != nil {
		return fmt.Errorf("create hosted owner session: %w", err)
	}
	moderator := moderation.NewService(moderation.NewPostgresStore(db.Queries()), auth.SystemClock{})
	if err := moderator.Hide(ctx, ownerDID, root.URI); err != nil {
		return fmt.Errorf("hide Bob root through moderation service: %w", err)
	}
	afterRows, err := snapshotRows(ctx, db, issueTarget.URI, root.URI)
	if err != nil {
		return err
	}
	if afterRows != beforeRows || afterRows.total != 2 || afterRows.active != 2 || afterRows.rootCID != root.CID {
		return fmt.Errorf("moderation changed network rows: before=%+v after=%+v", beforeRows, afterRows)
	}

	personalized, err := getComments(ctx, requiredEnv("ADENOSINE_A_URL"), issueTarget.URI, plaintext)
	if err != nil {
		return err
	}
	if personalized.CommentCount != 1 || len(personalized.Data) != 1 || personalized.Data[0].URI != reply.URI ||
		personalized.Data[0].ParentURI == nil || *personalized.Data[0].ParentURI != root.URI || personalized.Data[0].ParentCID == nil || *personalized.Data[0].ParentCID != root.CID {
		return fmt.Errorf("personalized A projection did not hide only root: %+v", personalized)
	}
	for _, testCase := range []struct{ name, baseURL string }{
		{name: "A anonymous", baseURL: requiredEnv("ADENOSINE_A_URL")},
		{name: "B anonymous", baseURL: requiredEnv("ADENOSINE_B_URL")},
	} {
		page, requestErr := getComments(ctx, testCase.baseURL, issueTarget.URI, "")
		if requestErr != nil {
			return fmt.Errorf("%s after local moderation: %w", testCase.name, requestErr)
		}
		if _, _, requestErr = exactThread(page); requestErr != nil {
			return fmt.Errorf("%s changed by A moderation: %w", testCase.name, requestErr)
		}
	}
	return nil
}

func deleteRoot(ctx context.Context) error {
	db, err := database.Open(ctx, requiredEnv("DATABASE_URL_B"), nil)
	if err != nil {
		return fmt.Errorf("open B database: %w", err)
	}
	defer db.Close()
	issueTarget, err := projectedIssue(ctx, db)
	if err != nil {
		return err
	}
	projection, err := comment.NewService(comment.NewPostgresStore(db.Queries()), &deterministicPublisher{}, auth.SystemClock{}).Get(ctx, issueTarget.URI, "")
	if err != nil {
		return fmt.Errorf("read B comments before delete: %w", err)
	}
	var root issue.Comment
	for _, value := range projection.Comments {
		if value.Body == rootBody {
			root = value.Comment
		}
	}
	if root.URI == "" {
		return fmt.Errorf("Bob root comment not found in B projection")
	}
	publisher := &deterministicPublisher{mode: "delete", parent: &issue.StrongRef{URI: root.URI, CID: root.CID}}
	service := comment.NewService(comment.NewPostgresStore(db.Queries()), publisher, auth.SystemClock{})
	if err := service.Delete(ctx, authorDID, root.URI); err != nil {
		return fmt.Errorf("delete root through B comment service: %w", err)
	}
	parsed, _ := syntax.ParseATURI(root.URI)
	event, err := json.Marshal(map[string]any{
		"id": deleteEventID, "type": "record",
		"record": map[string]any{"live": false, "rev": "3kzfcijpj2z2a", "did": authorDID, "collection": issue.CommentCollection, "rkey": parsed.RecordKey().String(), "action": "delete"},
	})
	if err != nil {
		return fmt.Errorf("encode delete tombstone: %w", err)
	}
	return deliverEverywhere(ctx, event)
}

func projectedIssue(ctx context.Context, db *database.DB) (issue.StrongRef, error) {
	service := issue.NewService(issue.NewPostgresStore(db.Queries()), unavailableIssuePublisher{}, auth.SystemClock{}, nil)
	projection, err := service.Get(ctx, repositoryURI)
	if err != nil {
		return issue.StrongRef{}, fmt.Errorf("read B PostgreSQL issue projection: %w", err)
	}
	if projection.IssueCount != 1 || len(projection.Issues) != 1 {
		return issue.StrongRef{}, fmt.Errorf("issue projection has count/data %d/%d, want 1/1", projection.IssueCount, len(projection.Issues))
	}
	value := projection.Issues[0]
	if value.AuthorDID != authorDID || value.Repository != (issue.StrongRef{URI: repositoryURI, CID: repositoryCID}) {
		return issue.StrongRef{}, fmt.Errorf("unexpected projected issue: %+v", value)
	}
	return issue.StrongRef{URI: value.URI, CID: value.CID}, nil
}

func createEvent(id int, value issue.Comment) ([]byte, error) {
	record, err := encodeRecord(value.CommentRecord)
	if err != nil {
		return nil, err
	}
	parsed, err := syntax.ParseATURI(value.URI)
	if err != nil {
		return nil, fmt.Errorf("parse comment event URI: %w", err)
	}
	event, err := json.Marshal(map[string]any{
		"id": id, "type": "record",
		"record": map[string]any{
			"live": true, "rev": "3kzfcijpj2z2a", "did": value.AuthorDID, "collection": issue.CommentCollection,
			"rkey": parsed.RecordKey().String(), "action": "create", "cid": value.CID, "record": json.RawMessage(record),
		},
	})
	if err != nil {
		return nil, fmt.Errorf("encode comment event: %w", err)
	}
	return event, nil
}

func encodeRecord(record issue.CommentRecord) ([]byte, error) {
	type strongRef struct {
		URI string `json:"uri"`
		CID string `json:"cid"`
	}
	wire := struct {
		Type      string     `json:"$type"`
		Subject   strongRef  `json:"subject"`
		Parent    *strongRef `json:"parent,omitempty"`
		Body      string     `json:"body"`
		CreatedAt string     `json:"createdAt"`
		UpdatedAt string     `json:"updatedAt"`
	}{
		Type: issue.CommentCollection, Subject: strongRef{URI: record.Subject.URI, CID: record.Subject.CID}, Body: record.Body,
		CreatedAt: record.CreatedAt.UTC().Format(syntax.AtprotoDatetimeLayout), UpdatedAt: record.UpdatedAt.UTC().Format(syntax.AtprotoDatetimeLayout),
	}
	if record.Parent != nil {
		wire.Parent = &strongRef{URI: record.Parent.URI, CID: record.Parent.CID}
	}
	value, err := json.Marshal(wire)
	if err != nil {
		return nil, fmt.Errorf("encode deterministic comment record: %w", err)
	}
	return value, nil
}

func deterministicCID(record []byte) string {
	digest := sha256.Sum256(record)
	raw := append([]byte{0x01, 0x55, 0x12, 0x20}, digest[:]...)
	return "b" + strings.ToLower(base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(raw))
}

func canonicalRKey(value string) error {
	if err := issue.ValidateRecordKey(value); err != nil {
		return fmt.Errorf("validate service-generated comment rkey %q: %w", value, err)
	}
	id, err := uuid.Parse(value)
	if err != nil || id.Version() != 7 || strings.ReplaceAll(id.String(), "-", "") != value {
		return fmt.Errorf("service-generated comment rkey %q is not canonical compact UUIDv7", value)
	}
	return nil
}

func sameRef(left, right *issue.StrongRef) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return *left == *right
}

func deliverEverywhere(ctx context.Context, event []byte) error {
	testCases := []struct{ name, baseURL string }{
		{name: "A initial", baseURL: requiredEnv("ADENOSINE_A_URL")},
		{name: "B initial", baseURL: requiredEnv("ADENOSINE_B_URL")},
		{name: "A replay", baseURL: requiredEnv("ADENOSINE_A_URL")},
		{name: "B replay", baseURL: requiredEnv("ADENOSINE_B_URL")},
	}
	for _, testCase := range testCases {
		request, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(testCase.baseURL, "/")+"/internal/federation/tap", bytes.NewReader(event))
		if err != nil {
			return err
		}
		request.Header.Set("Content-Type", "application/json")
		request.SetBasicAuth("admin", requiredEnv("ADENOSINE_TAP_ADMIN_PASSWORD"))
		response, err := httpClient.Do(request)
		if err != nil {
			return fmt.Errorf("%s: %w", testCase.name, err)
		}
		body, readErr := io.ReadAll(io.LimitReader(response.Body, 4096))
		response.Body.Close()
		if readErr != nil {
			return fmt.Errorf("%s response: %w", testCase.name, readErr)
		}
		if response.StatusCode != http.StatusNoContent {
			return fmt.Errorf("%s status = %d, want 204: %s", testCase.name, response.StatusCode, strings.TrimSpace(string(body)))
		}
	}
	return nil
}

func getComments(ctx context.Context, baseURL, issueURI, session string) (commentPage, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(baseURL, "/")+"/api/v1/issues/comments?issue_uri="+url.QueryEscape(issueURI), nil)
	if err != nil {
		return commentPage{}, err
	}
	if session != "" {
		request.AddCookie(&http.Cookie{Name: "adenosine_session", Value: session})
	}
	response, err := httpClient.Do(request)
	if err != nil {
		return commentPage{}, err
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if err != nil {
		return commentPage{}, err
	}
	if response.StatusCode != http.StatusOK {
		return commentPage{}, fmt.Errorf("GET comments status = %d: %s", response.StatusCode, strings.TrimSpace(string(body)))
	}
	var page commentPage
	if err := json.Unmarshal(body, &page); err != nil {
		return commentPage{}, fmt.Errorf("decode comments: %w", err)
	}
	return page, nil
}

func exactThread(page commentPage) (root, reply struct {
	URI, CID string
}, err error) {
	if page.CommentCount != 2 || len(page.Data) != 2 {
		return root, reply, fmt.Errorf("comment count/data = %d/%d, want 2/2", page.CommentCount, len(page.Data))
	}
	for _, value := range page.Data {
		switch value.Body {
		case rootBody:
			if value.ParentURI != nil || value.ParentCID != nil {
				return root, reply, fmt.Errorf("root has parent %v/%v", value.ParentURI, value.ParentCID)
			}
			root.URI, root.CID = value.URI, value.CID
		case replyBody:
			reply.URI, reply.CID = value.URI, value.CID
		default:
			return root, reply, fmt.Errorf("unexpected comment body %q", value.Body)
		}
	}
	if root.URI == "" || reply.URI == "" || page.Data[1].ParentURI == nil || *page.Data[1].ParentURI != root.URI || page.Data[1].ParentCID == nil || *page.Data[1].ParentCID != root.CID {
		return root, reply, fmt.Errorf("thread references are not exact: %+v", page.Data)
	}
	return root, reply, nil
}

func snapshotRows(ctx context.Context, db *database.DB, issueURI, rootURI string) (rowSnapshot, error) {
	tx, err := db.Begin(ctx)
	if err != nil {
		return rowSnapshot{}, fmt.Errorf("begin network row snapshot: %w", err)
	}
	defer tx.Rollback(ctx)
	var value rowSnapshot
	err = tx.QueryRow(ctx, `SELECT count(*), count(*) FILTER (WHERE deleted_at IS NULL), COALESCE(max(cid) FILTER (WHERE uri = $2), '') FROM network.issue_comments WHERE issue_uri = $1`, issueURI, rootURI).
		Scan(&value.total, &value.active, &value.rootCID)
	if err != nil {
		return rowSnapshot{}, fmt.Errorf("snapshot network comment rows: %w", err)
	}
	return value, nil
}

func requiredEnv(name string) string {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		fmt.Fprintf(os.Stderr, "%s is required\n", name)
		os.Exit(2)
	}
	return value
}
