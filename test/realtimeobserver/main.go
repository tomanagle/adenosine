package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strings"
	"time"
)

const (
	wantAuthor = "did:plc:dddddddddddddddddddddddd"
	wantRepo   = "at://did:plc:cccccccccccccccccccccccc/dev.adenosine.repo/0198a8512a897ae2a370dc68883e3af5"
)

var approved = map[string]bool{
	"uri": true, "cid": true, "author_did": true, "repository_uri": true,
	"repository_cid": true, "record_created_at": true, "indexed_at": true,
}

type shapeMessage struct {
	Headers  map[string]any `json:"headers"`
	Value    map[string]any `json:"value"`
	OldValue map[string]any `json:"old_value"`
}

type continuation struct{ handle, offset string }

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	if err := run(ctx); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(ctx context.Context) error {
	initial, messages, err := request(ctx, continuation{offset: "-1"}, "initial", false)
	if err != nil {
		return fmt.Errorf("initial star shape: %w", err)
	}
	if err := assertNoFixture(messages); err != nil {
		return fmt.Errorf("initial star shape: %w", err)
	}
	fmt.Printf("initial shape complete: handle=%s offset=%s messages=%d\n", initial.handle, initial.offset, len(messages))

	afterCreate, messages, err := request(ctx, initial, "create-live", true)
	if err != nil {
		return fmt.Errorf("live create continuation: %w", err)
	}
	if err := assertOperation(messages, "insert"); err != nil {
		return fmt.Errorf("live create continuation: %w", err)
	}
	fmt.Printf("live create observed on existing request: handle=%s offset=%s\n", afterCreate.handle, afterCreate.offset)

	_, messages, err = request(ctx, afterCreate, "delete-live", true)
	if err != nil {
		return fmt.Errorf("resumed live delete continuation: %w", err)
	}
	if err := assertOperation(messages, "delete"); err != nil {
		return fmt.Errorf("resumed live delete continuation: %w", err)
	}
	fmt.Println("live delete observed after reconnect from handle+offset")
	return nil
}

func request(parent context.Context, current continuation, phase string, live bool) (continuation, []shapeMessage, error) {
	ctx, cancel := context.WithTimeout(parent, 50*time.Second)
	defer cancel()
	query := url.Values{"offset": {current.offset}, "cache-buster": {phase}}
	if current.handle != "" {
		query.Set("handle", current.handle)
	}
	if live {
		query.Set("live", "true")
	}
	endpoint := strings.TrimRight(requiredEnv("SYNC_URL"), "/") + "/api/v1/sync/stars?" + query.Encode()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return continuation{}, nil, err
	}
	response, err := (&http.Client{}).Do(request)
	if err != nil {
		return continuation{}, nil, err
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, 2<<20))
	if err != nil {
		return continuation{}, nil, fmt.Errorf("read response: %w", err)
	}
	if response.StatusCode != http.StatusOK {
		return continuation{}, nil, fmt.Errorf("status=%d headers=%v body=%s", response.StatusCode, response.Header, strings.TrimSpace(string(body)))
	}
	next := continuation{handle: response.Header.Get("Electric-Handle"), offset: response.Header.Get("Electric-Offset")}
	if next.handle == "" || next.offset == "" {
		return continuation{}, nil, fmt.Errorf("missing continuation headers: headers=%v body=%s", response.Header, strings.TrimSpace(string(body)))
	}
	var messages []shapeMessage
	if err := json.Unmarshal(body, &messages); err != nil {
		return continuation{}, nil, fmt.Errorf("decode messages: %w: body=%s", err, strings.TrimSpace(string(body)))
	}
	return next, messages, nil
}

func assertNoFixture(messages []shapeMessage) error {
	for _, message := range messages {
		if message.Value["author_did"] == wantAuthor || message.OldValue["author_did"] == wantAuthor {
			return fmt.Errorf("realtime fixture already present: %+v", message)
		}
	}
	return nil
}

func assertOperation(messages []shapeMessage, operation string) error {
	for _, message := range messages {
		for _, value := range []map[string]any{message.Value, message.OldValue} {
			if err := assertApproved(value); err != nil {
				return err
			}
		}
		if message.Headers["operation"] != operation {
			continue
		}
		value := message.Value
		if operation == "delete" && len(value) == 0 {
			value = message.OldValue
		}
		if value["author_did"] == wantAuthor || strings.Contains(fmt.Sprint(value["uri"]), wantAuthor) {
			if operation == "insert" && (len(value) != len(approved) || value["repository_uri"] != wantRepo) {
				return fmt.Errorf("insert is not the complete approved SyncStar: %+v", value)
			}
			return nil
		}
	}
	return fmt.Errorf("fixture %s not found in messages: %+v", operation, messages)
}

func assertApproved(value map[string]any) error {
	var unexpected []string
	for key := range value {
		if !approved[key] {
			unexpected = append(unexpected, key)
		}
	}
	if len(unexpected) > 0 {
		sort.Strings(unexpected)
		return fmt.Errorf("unapproved SyncStar fields %v in %+v", unexpected, value)
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
