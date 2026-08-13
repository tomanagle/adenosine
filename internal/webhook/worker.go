package webhook

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	dbgen "github.com/adenosine-dev/adenosine/internal/database/generated"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
)

const (
	workerBatchSize = 20
	maxAttempts     = 8
	maxResponseBody = 64 * 1024
)

type Worker struct {
	queries *dbgen.Queries
	secrets *Service
	client  *http.Client
	id      string
	now     func() time.Time
}

func NewWorker(queries *dbgen.Queries, secrets *Service) *Worker {
	return newWorker(queries, secrets, net.DefaultResolver)
}

func newWorker(queries *dbgen.Queries, secrets *Service, resolver *net.Resolver) *Worker {
	dialer := &net.Dialer{Timeout: 5 * time.Second, KeepAlive: 30 * time.Second}
	transport := &http.Transport{
		DialContext: secureDialContext(dialer, resolver), ForceAttemptHTTP2: true,
		TLSHandshakeTimeout: 5 * time.Second, ResponseHeaderTimeout: 5 * time.Second,
		IdleConnTimeout: 30 * time.Second, TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12},
	}
	return &Worker{queries: queries, secrets: secrets, client: &http.Client{Transport: transport, Timeout: 10 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return errors.New("webhook redirects are disabled") }}, id: uuid.NewString(), now: time.Now}
}

func (worker *Worker) Run(ctx context.Context) error {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		if err := worker.process(ctx); err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return err
		}
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		}
	}
}

func (worker *Worker) process(ctx context.Context) error {
	if err := worker.dispatchOutbox(ctx); err != nil {
		return err
	}
	return worker.deliver(ctx)
}

func (worker *Worker) dispatchOutbox(ctx context.Context) error {
	now := worker.now().UTC()
	events, err := worker.queries.ClaimOutboxEvents(ctx, dbgen.ClaimOutboxEventsParams{ClaimTime: pgTime(now), ClaimedBy: pgtype.Text{String: worker.id, Valid: true}, StaleBefore: pgTime(now.Add(-5 * time.Minute)), BatchSize: workerBatchSize})
	if err != nil {
		return fmt.Errorf("claim webhook outbox events: %w", err)
	}
	for _, event := range events {
		eventType := webhookEventType(event.Type)
		if eventType != "" && event.AggregateType == "repository" {
			repositoryID, parseErr := uuid.Parse(event.AggregateID)
			if parseErr != nil {
				return worker.retryOutbox(ctx, event.ID, "invalid_repository_id", now)
			}
			body, marshalErr := json.Marshal(struct {
				ID         uuid.UUID       `json:"id"`
				Event      string          `json:"event"`
				OccurredAt time.Time       `json:"occurred_at"`
				Payload    json.RawMessage `json:"payload"`
			}{uuid.UUID(event.ID.Bytes), eventType, event.CreatedAt.Time, event.Payload})
			if marshalErr != nil {
				return worker.retryOutbox(ctx, event.ID, "encode_payload", now)
			}
			if err := worker.queries.CreateWebhookDeliveriesForEvent(ctx, dbgen.CreateWebhookDeliveriesForEventParams{EventID: event.ID, EventType: eventType, RequestBody: body, CreatedAt: event.CreatedAt, RepositoryID: pgUUID(repositoryID)}); err != nil {
				return worker.retryOutbox(ctx, event.ID, "enqueue_delivery", now)
			}
		}
		if err := worker.queries.CompleteOutboxEvent(ctx, dbgen.CompleteOutboxEventParams{ID: event.ID, CompletedAt: pgTime(now)}); err != nil {
			return fmt.Errorf("complete webhook outbox event: %w", err)
		}
	}
	return nil
}

func (worker *Worker) retryOutbox(ctx context.Context, id pgtype.UUID, code string, now time.Time) error {
	if err := worker.queries.RetryOutboxEvent(ctx, dbgen.RetryOutboxEventParams{AvailableAt: pgTime(now.Add(time.Minute)), LastErrorCode: pgtype.Text{String: code, Valid: true}, ID: id}); err != nil {
		return fmt.Errorf("retry webhook outbox event: %w", err)
	}
	return nil
}

func (worker *Worker) deliver(ctx context.Context) error {
	now := worker.now().UTC()
	deliveries, err := worker.queries.ClaimWebhookDeliveries(ctx, dbgen.ClaimWebhookDeliveriesParams{ClaimTime: pgTime(now), ClaimedBy: pgtype.Text{String: worker.id, Valid: true}, StaleBefore: pgTime(now.Add(-5 * time.Minute)), BatchSize: workerBatchSize})
	if err != nil {
		return fmt.Errorf("claim webhook deliveries: %w", err)
	}
	for _, delivery := range deliveries {
		status, body, code := worker.send(ctx, delivery)
		statusValue := pgtype.Int4{}
		if status != 0 {
			statusValue = pgtype.Int4{Int32: int32(status), Valid: true}
		}
		bodyValue := pgtype.Text{String: body, Valid: body != ""}
		if code == "" {
			err = worker.queries.CompleteWebhookDelivery(ctx, dbgen.CompleteWebhookDeliveryParams{ResponseStatus: statusValue, ResponseBody: bodyValue, DeliveredAt: pgTime(worker.now()), ID: delivery.ID})
		} else if delivery.Attempts >= maxAttempts {
			err = worker.queries.FailWebhookDelivery(ctx, dbgen.FailWebhookDeliveryParams{ResponseStatus: statusValue, ResponseBody: bodyValue, FailedAt: pgTime(worker.now()), LastErrorCode: pgtype.Text{String: code, Valid: true}, ID: delivery.ID})
		} else {
			err = worker.queries.RetryWebhookDelivery(ctx, dbgen.RetryWebhookDeliveryParams{ResponseStatus: statusValue, ResponseBody: bodyValue, AvailableAt: pgTime(worker.now().Add(retryDelay(delivery.Attempts))), LastErrorCode: pgtype.Text{String: code, Valid: true}, ID: delivery.ID})
		}
		if err != nil {
			return fmt.Errorf("record webhook delivery result: %w", err)
		}
	}
	return nil
}

func (worker *Worker) send(ctx context.Context, delivery dbgen.ClaimWebhookDeliveriesRow) (int, string, string) {
	secret, err := worker.secrets.decrypt(delivery.SecretCiphertext)
	if err != nil {
		return 0, "", "secret_decryption"
	}
	mac := hmac.New(sha256.New, secret)
	_, _ = mac.Write(delivery.RequestBody)
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, delivery.Url, bytes.NewReader(delivery.RequestBody))
	if err != nil {
		return 0, "", "invalid_request"
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("User-Agent", "Adenosine-Webhooks/1.0")
	request.Header.Set("X-Adenosine-Delivery", uuid.UUID(delivery.ID.Bytes).String())
	request.Header.Set("X-Adenosine-Event", delivery.EventType)
	request.Header.Set("X-Adenosine-Signature-256", "sha256="+hex.EncodeToString(mac.Sum(nil)))
	response, err := worker.client.Do(request)
	if err != nil {
		return 0, "", "network_error"
	}
	defer response.Body.Close()
	contents, readErr := io.ReadAll(io.LimitReader(response.Body, maxResponseBody+1))
	if readErr != nil {
		return response.StatusCode, "", "response_read"
	}
	if len(contents) > maxResponseBody {
		contents = contents[:maxResponseBody]
	}
	body := string(contents)
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return response.StatusCode, body, "http_" + strconv.Itoa(response.StatusCode)
	}
	return response.StatusCode, body, ""
}

func secureDialContext(dialer *net.Dialer, resolver *net.Resolver) func(context.Context, string, string) (net.Conn, error) {
	return func(ctx context.Context, network, address string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(address)
		if err != nil {
			return nil, fmt.Errorf("validate webhook address: %w", err)
		}
		addresses, err := resolver.LookupIP(ctx, "ip", host)
		if err != nil || len(addresses) == 0 {
			return nil, errors.New("webhook host has no public address")
		}
		for _, candidate := range addresses {
			if !publicIP(candidate) {
				return nil, errors.New("webhook host has a non-public address")
			}
		}
		var joined error
		for _, candidate := range addresses {
			connection, dialErr := dialer.DialContext(ctx, network, net.JoinHostPort(candidate.String(), port))
			if dialErr == nil {
				return connection, nil
			}
			joined = errors.Join(joined, dialErr)
		}
		return nil, fmt.Errorf("connect to webhook host: %w", joined)
	}
}

func webhookEventType(value string) string {
	switch {
	case value == "git.push_received":
		return "push"
	case value == "git.refs_updated" || strings.HasPrefix(value, "pull_request."):
		return "pull_request"
	case strings.HasPrefix(value, "issue."):
		return "issue"
	case strings.HasPrefix(value, "review."):
		return "review"
	case strings.HasPrefix(value, "status."):
		return "status"
	case strings.HasPrefix(value, "check_run."):
		return "check_run"
	default:
		return ""
	}
}

func retryDelay(attempt int32) time.Duration {
	delay := time.Minute << max(attempt-1, 0)
	if delay > time.Hour {
		return time.Hour
	}
	return delay
}
