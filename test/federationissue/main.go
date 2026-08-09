package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base32"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/adenosine-dev/adenosine/internal/database"
	"github.com/adenosine-dev/adenosine/internal/issue"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/google/uuid"
)

const (
	reporterDID    = "did:plc:bbbbbbbbbbbbbbbbbbbbbbbb"
	repositoryDID  = "did:plc:cccccccccccccccccccccccc"
	repositoryRKey = "0198a8512a897ae2a370dc68883e3af5"
	repositoryCID  = "bafybeigdyrzt5sfp7udm7hu76uh7y26nf3efuylqabf3oclgtqy55fbzdi"
	issueTitle     = "Federated issue from Bob"
	issueBody      = "Created through B against A's projected repository."
	eventID        = 26
)

var (
	repositoryURI = "at://" + repositoryDID + "/dev.adenosine.repo/" + repositoryRKey
	createdAt     = time.Date(2026, 8, 9, 14, 0, 0, 0, time.UTC)
	httpClient    = &http.Client{Timeout: 10 * time.Second}
)

type fixedClock struct{}

func (fixedClock) Now() time.Time { return createdAt }

type deterministicPublisher struct{}

func (deterministicPublisher) CreateIssue(_ context.Context, author, rkey string, record issue.Record) (issue.Issue, error) {
	if author != reporterDID {
		return issue.Issue{}, fmt.Errorf("unexpected issue reporter %q", author)
	}
	if err := issue.ValidateRecordKey(rkey); err != nil {
		return issue.Issue{}, fmt.Errorf("validate service-generated issue rkey %q: %w", rkey, err)
	}
	id, err := uuid.Parse(rkey)
	if err != nil || id.Version() != 7 || strings.ReplaceAll(id.String(), "-", "") != rkey {
		return issue.Issue{}, fmt.Errorf("service-generated issue rkey %q is not canonical compact UUIDv7", rkey)
	}
	wantRecord := issue.Record{
		Repository: issue.StrongRef{URI: repositoryURI, CID: repositoryCID},
		Title:      issueTitle, Body: issueBody, CreatedAt: createdAt, UpdatedAt: createdAt,
	}
	if record.Repository != wantRecord.Repository || record.Title != wantRecord.Title || record.Body != wantRecord.Body ||
		!record.CreatedAt.Equal(wantRecord.CreatedAt) || !record.UpdatedAt.Equal(wantRecord.UpdatedAt) ||
		record.CreatedAt.Location() != time.UTC || record.UpdatedAt.Location() != time.UTC {
		return issue.Issue{}, fmt.Errorf("unexpected issue record for rkey %q: %+v", rkey, record)
	}
	recordJSON, err := encodeRecord(record)
	if err != nil {
		return issue.Issue{}, err
	}
	value := issue.Issue{
		URI: "at://" + author + "/" + issue.Collection + "/" + rkey,
		CID: deterministicCID(recordJSON), AuthorDID: author, Record: record,
	}
	if err := value.Validate(); err != nil {
		return issue.Issue{}, fmt.Errorf("validate deterministic issue envelope: %w", err)
	}
	return value, nil
}

func (deterministicPublisher) PutIssueStatus(context.Context, string, issue.StatusRecord) (issue.Status, error) {
	return issue.Status{}, fmt.Errorf("issue status publication is outside federation acceptance")
}

func main() {
	if err := run(context.Background()); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(ctx context.Context) error {
	db, err := database.Open(ctx, requiredEnv("DATABASE_URL"))
	if err != nil {
		return fmt.Errorf("open B database: %w", err)
	}
	defer db.Close()

	service := issue.NewService(issue.NewPostgresStore(db.Queries()), deterministicPublisher{}, fixedClock{})
	created, err := service.Create(ctx, reporterDID, issue.CreateInput{RepositoryURI: repositoryURI, Title: issueTitle, Body: issueBody})
	if err != nil {
		return fmt.Errorf("create issue through B service: %w", err)
	}
	uri, err := syntax.ParseATURI(created.URI)
	if err != nil {
		return fmt.Errorf("parse created issue URI: %w", err)
	}
	rkey := uri.RecordKey().String()
	record, err := encodeRecord(created.Record)
	if err != nil {
		return err
	}
	wantURI := "at://" + reporterDID + "/" + issue.Collection + "/" + rkey
	wantCID := deterministicCID(record)
	if created.URI != wantURI || created.CID != wantCID || created.AuthorDID != reporterDID ||
		created.Repository != (issue.StrongRef{URI: repositoryURI, CID: repositoryCID}) ||
		created.Title != issueTitle || created.Body != issueBody || !created.CreatedAt.Equal(createdAt) || !created.UpdatedAt.Equal(createdAt) {
		return fmt.Errorf("publisher returned unexpected authoritative envelope: %+v", created)
	}

	event, err := json.Marshal(map[string]any{
		"id": eventID, "type": "record",
		"record": map[string]any{
			"live": true, "rev": "3kzfcijpj2z2a", "did": reporterDID, "collection": issue.Collection,
			"rkey": rkey, "action": "create", "cid": created.CID, "record": json.RawMessage(record),
		},
	})
	if err != nil {
		return fmt.Errorf("encode Tap issue event: %w", err)
	}
	deliveries := []struct {
		name string
		url  string
	}{
		{name: "A initial delivery", url: requiredEnv("ADENOSINE_A_URL")},
		{name: "B initial delivery", url: requiredEnv("ADENOSINE_B_URL")},
		{name: "A replay", url: requiredEnv("ADENOSINE_A_URL")},
		{name: "B replay", url: requiredEnv("ADENOSINE_B_URL")},
	}
	for _, testCase := range deliveries {
		if err := deliver(ctx, testCase.url, event); err != nil {
			return fmt.Errorf("%s: %w", testCase.name, err)
		}
	}
	fmt.Printf("created and delivered %s (%s) through B issue service\n", created.URI, created.CID)
	return nil
}

func encodeRecord(record issue.Record) ([]byte, error) {
	type strongRef struct {
		URI string `json:"uri"`
		CID string `json:"cid"`
	}
	wire := struct {
		Type       string    `json:"$type"`
		Repository strongRef `json:"repository"`
		Title      string    `json:"title"`
		Body       string    `json:"body"`
		CreatedAt  string    `json:"createdAt"`
		UpdatedAt  string    `json:"updatedAt"`
	}{
		Type: issue.Collection, Repository: strongRef{URI: record.Repository.URI, CID: record.Repository.CID}, Title: record.Title, Body: record.Body,
		CreatedAt: record.CreatedAt.UTC().Format(syntax.AtprotoDatetimeLayout),
		UpdatedAt: record.UpdatedAt.UTC().Format(syntax.AtprotoDatetimeLayout),
	}
	value, err := json.Marshal(wire)
	if err != nil {
		return nil, fmt.Errorf("encode deterministic issue record: %w", err)
	}
	return value, nil
}

func deterministicCID(record []byte) string {
	digest := sha256.Sum256(record)
	raw := append([]byte{0x01, 0x55, 0x12, 0x20}, digest[:]...)
	return "b" + strings.ToLower(base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(raw))
}

func deliver(ctx context.Context, baseURL string, body []byte) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(baseURL, "/")+"/internal/federation/tap", bytes.NewReader(body))
	if err != nil {
		return err
	}
	request.Header.Set("Content-Type", "application/json")
	request.SetBasicAuth("admin", requiredEnv("ADENOSINE_TAP_ADMIN_PASSWORD"))
	response, err := httpClient.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusNoContent {
		responseBody, _ := io.ReadAll(io.LimitReader(response.Body, 4096))
		return fmt.Errorf("status = %d, want 204: %s", response.StatusCode, strings.TrimSpace(string(responseBody)))
	}
	return nil
}

func requiredEnv(name string) string {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		fmt.Fprintf(os.Stderr, "%s is required\n", name)
		os.Exit(2)
	}
	return value
}
