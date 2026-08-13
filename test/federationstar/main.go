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
	"github.com/adenosine-dev/adenosine/internal/star"
	"github.com/bluesky-social/indigo/atproto/syntax"
)

const (
	authorDID      = "did:plc:bbbbbbbbbbbbbbbbbbbbbbbb"
	repositoryDID  = "did:plc:cccccccccccccccccccccccc"
	repositoryRKey = "0198a8512a897ae2a370dc68883e3af5"
	repositoryCID  = "bafybeigdyrzt5sfp7udm7hu76uh7y26nf3efuylqabf3oclgtqy55fbzdi"
	eventID        = 25
)

var (
	repositoryURI = "at://" + repositoryDID + "/dev.adenosine.repo/" + repositoryRKey
	createdAt     = time.Date(2026, 8, 9, 13, 0, 0, 0, time.UTC)
	httpClient    = &http.Client{Timeout: 10 * time.Second}
)

type fixedClock struct{}

func (fixedClock) Now() time.Time { return createdAt }

type deterministicPublisher struct{}

func (deterministicPublisher) CreateStar(_ context.Context, author string, target star.Target, at time.Time) (star.Star, error) {
	did, err := syntax.ParseDID(author)
	if err != nil || did.String() != author || author != authorDID {
		return star.Star{}, fmt.Errorf("unexpected star author %q", author)
	}
	wantTarget := star.Target{URI: repositoryURI, CID: repositoryCID}
	if err := target.Validate(); err != nil {
		return star.Star{}, fmt.Errorf("validate current star target: %w", err)
	}
	if target != wantTarget {
		return star.Star{}, fmt.Errorf("unexpected current star target %+v", target)
	}
	if !at.Equal(createdAt) || at.Location() != time.UTC {
		return star.Star{}, fmt.Errorf("unexpected star createdAt %s", at)
	}
	rkey, err := star.RecordKey(target.URI)
	if err != nil {
		return star.Star{}, fmt.Errorf("derive star rkey: %w", err)
	}
	record, err := recordJSON(target, at)
	if err != nil {
		return star.Star{}, err
	}
	cid := deterministicCID(record)
	if err := star.ValidateCID(cid); err != nil {
		return star.Star{}, fmt.Errorf("validate deterministic star CID: %w", err)
	}
	return star.Star{
		URI: "at://" + author + "/" + star.Collection + "/" + rkey, CID: cid,
		AuthorDID: author, Target: target, CreatedAt: at,
	}, nil
}

func (deterministicPublisher) DeleteStar(context.Context, string, star.Target) error {
	return fmt.Errorf("star deletion is outside federation acceptance")
}

func main() {
	if err := run(context.Background()); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(ctx context.Context) error {
	db, err := database.Open(ctx, requiredEnv("DATABASE_URL"), nil)
	if err != nil {
		return fmt.Errorf("open B database: %w", err)
	}
	defer db.Close()

	service := star.NewService(star.NewPostgresStore(db.Queries()), deterministicPublisher{}, fixedClock{})
	created, err := service.Create(ctx, authorDID, repositoryURI)
	if err != nil {
		return fmt.Errorf("create star through B service: %w", err)
	}
	record, err := recordJSON(created.Target, created.CreatedAt)
	if err != nil {
		return err
	}
	rkey, err := star.RecordKey(created.Target.URI)
	if err != nil {
		return fmt.Errorf("derive delivered star rkey: %w", err)
	}
	wantURI := "at://" + authorDID + "/" + star.Collection + "/" + rkey
	wantCID := deterministicCID(record)
	if created.URI != wantURI || created.CID != wantCID || created.AuthorDID != authorDID ||
		created.Target != (star.Target{URI: repositoryURI, CID: repositoryCID}) || !created.CreatedAt.Equal(createdAt) {
		return fmt.Errorf("publisher returned unexpected authoritative envelope: %+v", created)
	}

	event, err := json.Marshal(map[string]any{
		"id": eventID, "type": "record",
		"record": map[string]any{
			"live": true, "rev": "3kzfcijpj2z2a", "did": authorDID, "collection": star.Collection,
			"rkey": rkey, "action": "create", "cid": created.CID, "record": json.RawMessage(record),
		},
	})
	if err != nil {
		return fmt.Errorf("encode Tap star event: %w", err)
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
	for _, delivery := range deliveries {
		if err := deliver(ctx, delivery.url, event); err != nil {
			return fmt.Errorf("%s: %w", delivery.name, err)
		}
	}
	fmt.Printf("created and delivered %s (%s) through B star service\n", created.URI, created.CID)
	return nil
}

func recordJSON(target star.Target, at time.Time) ([]byte, error) {
	record := struct {
		Type    string      `json:"$type"`
		Subject starSubject `json:"subject"`
		Created string      `json:"createdAt"`
	}{
		Type: star.Collection, Subject: starSubject{URI: target.URI, CID: target.CID},
		Created: at.UTC().Format(syntax.AtprotoDatetimeLayout),
	}
	value, err := json.Marshal(record)
	if err != nil {
		return nil, fmt.Errorf("encode deterministic star record: %w", err)
	}
	return value, nil
}

type starSubject struct {
	URI string `json:"uri"`
	CID string `json:"cid"`
}

func deterministicCID(record []byte) string {
	digest := sha256.Sum256(record)
	// CIDv1, raw, sha2-256, 32-byte digest gives the synthetic publication stable content identity.
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
