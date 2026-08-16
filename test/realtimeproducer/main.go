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
	"os"
	"strings"
	"time"

	"github.com/adenosine-dev/adenosine/internal/database"
	"github.com/adenosine-dev/adenosine/internal/star"
	"github.com/bluesky-social/indigo/atproto/syntax"
)

const (
	authorDID            = "did:plc:dddddddddddddddddddddddd"
	sourceRepositoryDID  = "did:plc:cccccccccccccccccccccccc"
	currentRepositoryDID = "did:plc:bbbbbbbbbbbbbbbbbbbbbbbb"
	repositoryRKey       = "0198a8512a897ae2a370dc68883e3af5"
	repositoryCID        = "bafybeigdyrzt5sfp7udm7hu76uh7y26nf3efuylqabf3oclgtqy55fbzdi"
)

var (
	repositoryURI        = "at://" + sourceRepositoryDID + "/dev.adenosine.repo/" + repositoryRKey
	currentRepositoryURI = "at://" + currentRepositoryDID + "/dev.adenosine.repo/" + repositoryRKey
	createdAt            = time.Date(2026, 8, 10, 9, 0, 0, 0, time.UTC)
)

type fixedClock struct{}

func (fixedClock) Now() time.Time { return createdAt }

type deterministicPublisher struct{ created star.Star }

func (publisher *deterministicPublisher) CreateStar(_ context.Context, author string, target star.Target, at time.Time) (star.Star, error) {
	if author != authorDID || target != (star.Target{URI: currentRepositoryURI, CID: repositoryCID}) || !at.Equal(createdAt) {
		return star.Star{}, fmt.Errorf("unexpected publication input: author=%s target=%+v at=%s", author, target, at)
	}
	record, err := recordJSON(target, at)
	if err != nil {
		return star.Star{}, err
	}
	rkey, err := star.RecordKey(target.URI)
	if err != nil {
		return star.Star{}, err
	}
	publisher.created = star.Star{URI: "at://" + author + "/" + star.Collection + "/" + rkey, CID: deterministicCID(record), AuthorDID: author, Target: target, CreatedAt: at}
	return publisher.created, nil
}

func (publisher *deterministicPublisher) DeleteStar(_ context.Context, author string, target star.Target) error {
	if author != authorDID || target != (star.Target{URI: currentRepositoryURI, CID: repositoryCID}) {
		return fmt.Errorf("unexpected deletion input: author=%s target=%+v", author, target)
	}
	return nil
}

func main() {
	phase := flag.String("phase", "create", "create or delete")
	flag.Parse()
	if err := run(context.Background(), *phase); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(ctx context.Context, phase string) error {
	db, err := database.Open(ctx, requiredEnv("DATABASE_URL"), nil)
	if err != nil {
		return fmt.Errorf("open A database: %w", err)
	}
	defer db.Close()
	publisher := &deterministicPublisher{}
	service := star.NewService(star.NewPostgresStore(db.Queries()), publisher, fixedClock{})
	rkey, err := star.RecordKey(currentRepositoryURI)
	if err != nil {
		return err
	}

	var events [][]byte
	switch phase {
	case "create":
		created, err := service.Create(ctx, authorDID, repositoryURI)
		if err != nil {
			return fmt.Errorf("publish through A star service: %w", err)
		}
		record, err := recordJSON(created.Target, created.CreatedAt)
		if err != nil {
			return err
		}
		create, err := event(900, "create", rkey, created.CID, record)
		if err != nil {
			return err
		}
		stale, err := event(899, "delete", rkey, "", nil)
		if err != nil {
			return err
		}
		// Replay and a lower-ID delete exercise receipt idempotency and stale projection guards.
		events = [][]byte{create, create, stale}
	case "delete":
		if err := service.Delete(ctx, authorDID, repositoryURI); err != nil {
			return fmt.Errorf("delete through A star service: %w", err)
		}
		deletion, err := event(901, "delete", rkey, "", nil)
		if err != nil {
			return err
		}
		events = [][]byte{deletion, deletion}
	default:
		return fmt.Errorf("unknown phase %q", phase)
	}
	for index, body := range events {
		if err := publishBoundary(ctx, body); err != nil {
			return fmt.Errorf("cross deterministic Tap-output boundary event %d: %w", index+1, err)
		}
	}
	fmt.Printf("published %s star lifecycle phase across deterministic Tap-output boundary\n", phase)
	return nil
}

func event(id int64, action, rkey, cid string, record []byte) ([]byte, error) {
	payload := map[string]any{"live": true, "rev": "3kzfcijpj2z2a", "did": authorDID, "collection": star.Collection, "rkey": rkey, "action": action}
	if action != "delete" {
		payload["cid"], payload["record"] = cid, json.RawMessage(record)
	}
	body, err := json.Marshal(map[string]any{"id": id, "type": "record", "record": payload})
	if err != nil {
		return nil, fmt.Errorf("encode Tap event: %w", err)
	}
	return body, nil
}

func recordJSON(target star.Target, at time.Time) ([]byte, error) {
	return json.Marshal(map[string]any{"$type": star.Collection, "subject": map[string]string{"uri": target.URI, "cid": target.CID}, "createdAt": at.Format(syntax.AtprotoDatetimeLayout)})
}

func deterministicCID(record []byte) string {
	digest := sha256.Sum256(record)
	raw := append([]byte{0x01, 0x55, 0x12, 0x20}, digest[:]...)
	return "b" + strings.ToLower(base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(raw))
}

func publishBoundary(ctx context.Context, body []byte) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(requiredEnv("PUBLICATION_BOUNDARY_URL"), "/")+"/tap-output", bytes.NewReader(body))
	if err != nil {
		return err
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := (&http.Client{Timeout: 15 * time.Second}).Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusNoContent {
		responseBody, _ := io.ReadAll(io.LimitReader(response.Body, 4096))
		return fmt.Errorf("status %d: %s", response.StatusCode, strings.TrimSpace(string(responseBody)))
	}
	return nil
}

func requiredEnv(name string) string {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		fmt.Fprintln(os.Stderr, name+" is required")
		os.Exit(2)
	}
	return value
}
