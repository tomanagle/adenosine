// Package webhook owns repository webhook configuration and delivery.
package webhook

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"fmt"
	"net"
	"net/url"
	"slices"
	"strings"
	"time"

	dbgen "github.com/adenosine-dev/adenosine/internal/database/generated"
	"github.com/adenosine-dev/adenosine/internal/repository"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

var (
	ErrNotFound   = errors.New("webhook not found")
	ErrValidation = errors.New("webhook validation failed")
)

var allowedEvents = []string{"issue", "pull_request", "push", "review"}

type Webhook struct {
	ID           uuid.UUID
	RepositoryID repository.ID
	URL          string
	Events       []string
	Enabled      bool
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

type Delivery struct {
	ID             uuid.UUID
	WebhookID      uuid.UUID
	EventType      string
	Attempts       int
	ResponseStatus *int
	ResponseBody   string
	DeliveredAt    *time.Time
	FailedAt       *time.Time
	LastErrorCode  string
	CreatedAt      time.Time
}

type Page[T any] struct {
	Items      []T
	NextCursor *uuid.UUID
}

type CreateInput struct {
	URL, Secret string
	Events      []string
	Enabled     bool
}
type UpdateInput struct {
	URL     string
	Secret  *string
	Events  []string
	Enabled bool
}

type Service struct {
	queries *dbgen.Queries
	cipher  cipher.AEAD
}

func NewService(queries *dbgen.Queries, rootKey []byte) (*Service, error) {
	if len(rootKey) != 32 {
		return nil, errors.New("webhook root key must contain 32 bytes")
	}
	derived := hmac.New(sha256.New, rootKey)
	_, _ = derived.Write([]byte("adenosine/webhook/secrets/v1"))
	block, err := aes.NewCipher(derived.Sum(nil))
	if err != nil {
		return nil, fmt.Errorf("create webhook cipher: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("create webhook AEAD: %w", err)
	}
	return &Service{queries: queries, cipher: aead}, nil
}

func (service *Service) Create(ctx context.Context, repositoryID repository.ID, input CreateInput, now time.Time) (Webhook, error) {
	if err := validateInput(input.URL, input.Secret, input.Events); err != nil {
		return Webhook{}, err
	}
	id, err := uuid.NewV7()
	if err != nil {
		return Webhook{}, fmt.Errorf("generate webhook ID: %w", err)
	}
	secret, err := service.encrypt([]byte(input.Secret))
	if err != nil {
		return Webhook{}, err
	}
	row, err := service.queries.CreateRepositoryWebhook(ctx, dbgen.CreateRepositoryWebhookParams{ID: pgUUID(id), RepositoryID: pgUUID(uuid.UUID(repositoryID)), Url: input.URL, SecretCiphertext: secret, Events: normalizedEvents(input.Events), Enabled: input.Enabled, CreatedAt: pgTime(now)})
	if err != nil {
		return Webhook{}, fmt.Errorf("create repository webhook: %w", err)
	}
	return webhookFromRow(row), nil
}

func (service *Service) Get(ctx context.Context, repositoryID repository.ID, id uuid.UUID) (Webhook, error) {
	row, err := service.queries.GetRepositoryWebhook(ctx, dbgen.GetRepositoryWebhookParams{ID: pgUUID(id), RepositoryID: pgUUID(uuid.UUID(repositoryID))})
	if errors.Is(err, pgx.ErrNoRows) {
		return Webhook{}, ErrNotFound
	}
	if err != nil {
		return Webhook{}, fmt.Errorf("get repository webhook: %w", err)
	}
	return webhookFromRow(row), nil
}

func (service *Service) Page(ctx context.Context, repositoryID repository.ID, after *uuid.UUID, limit int) (Page[Webhook], error) {
	if limit < 1 || limit > 100 {
		return Page[Webhook]{}, fmt.Errorf("%w: limit must be between 1 and 100", ErrValidation)
	}
	rows, err := service.queries.PageRepositoryWebhooks(ctx, dbgen.PageRepositoryWebhooksParams{RepositoryID: pgUUID(uuid.UUID(repositoryID)), AfterID: optionalUUID(after), PageLimit: int32(limit + 1)})
	if err != nil {
		return Page[Webhook]{}, fmt.Errorf("page repository webhooks: %w", err)
	}
	page := Page[Webhook]{Items: make([]Webhook, min(len(rows), limit))}
	for index := range page.Items {
		page.Items[index] = webhookFromRow(rows[index])
	}
	if len(rows) > limit {
		next := page.Items[len(page.Items)-1].ID
		page.NextCursor = &next
	}
	return page, nil
}

func (service *Service) Update(ctx context.Context, repositoryID repository.ID, id uuid.UUID, input UpdateInput, now time.Time) (Webhook, error) {
	secretValue := "unchanged-secret-value"
	if input.Secret != nil {
		secretValue = *input.Secret
	}
	if err := validateInput(input.URL, secretValue, input.Events); err != nil {
		return Webhook{}, err
	}
	var ciphertext []byte
	var err error
	if input.Secret != nil {
		ciphertext, err = service.encrypt([]byte(*input.Secret))
		if err != nil {
			return Webhook{}, err
		}
	}
	row, err := service.queries.UpdateRepositoryWebhook(ctx, dbgen.UpdateRepositoryWebhookParams{Url: input.URL, ReplaceSecret: input.Secret != nil, SecretCiphertext: ciphertext, Events: normalizedEvents(input.Events), Enabled: input.Enabled, UpdatedAt: pgTime(now), ID: pgUUID(id), RepositoryID: pgUUID(uuid.UUID(repositoryID))})
	if errors.Is(err, pgx.ErrNoRows) {
		return Webhook{}, ErrNotFound
	}
	if err != nil {
		return Webhook{}, fmt.Errorf("update repository webhook: %w", err)
	}
	return webhookFromRow(row), nil
}

func (service *Service) Delete(ctx context.Context, repositoryID repository.ID, id uuid.UUID, now time.Time) error {
	rows, err := service.queries.DeleteRepositoryWebhook(ctx, dbgen.DeleteRepositoryWebhookParams{DeletedAt: pgTime(now), ID: pgUUID(id), RepositoryID: pgUUID(uuid.UUID(repositoryID))})
	if err != nil {
		return fmt.Errorf("delete repository webhook: %w", err)
	}
	if rows == 0 {
		return ErrNotFound
	}
	return nil
}

func (service *Service) Deliveries(ctx context.Context, repositoryID repository.ID, webhookID uuid.UUID, after *uuid.UUID, limit int) (Page[Delivery], error) {
	if limit < 1 || limit > 100 {
		return Page[Delivery]{}, fmt.Errorf("%w: limit must be between 1 and 100", ErrValidation)
	}
	rows, err := service.queries.PageWebhookDeliveries(ctx, dbgen.PageWebhookDeliveriesParams{WebhookID: pgUUID(webhookID), RepositoryID: pgUUID(uuid.UUID(repositoryID)), AfterID: optionalUUID(after), PageLimit: int32(limit + 1)})
	if err != nil {
		return Page[Delivery]{}, fmt.Errorf("page webhook deliveries: %w", err)
	}
	page := Page[Delivery]{Items: make([]Delivery, min(len(rows), limit))}
	for index := range page.Items {
		page.Items[index] = deliveryFromRow(rows[index])
	}
	if len(rows) > limit {
		next := page.Items[len(page.Items)-1].ID
		page.NextCursor = &next
	}
	return page, nil
}

func (service *Service) Redeliver(ctx context.Context, repositoryID repository.ID, webhookID, originalID uuid.UUID, now time.Time) (Delivery, error) {
	id, err := uuid.NewV7()
	if err != nil {
		return Delivery{}, fmt.Errorf("generate delivery ID: %w", err)
	}
	row, err := service.queries.RedeliverWebhookDelivery(ctx, dbgen.RedeliverWebhookDeliveryParams{ID: pgUUID(id), CreatedAt: pgTime(now), OriginalID: pgUUID(originalID), WebhookID: pgUUID(webhookID), RepositoryID: pgUUID(uuid.UUID(repositoryID))})
	if errors.Is(err, pgx.ErrNoRows) {
		return Delivery{}, ErrNotFound
	}
	if err != nil {
		return Delivery{}, fmt.Errorf("redeliver webhook delivery: %w", err)
	}
	return deliveryFromRow(row), nil
}

func (service *Service) encrypt(value []byte) ([]byte, error) {
	nonce := make([]byte, service.cipher.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, fmt.Errorf("generate webhook secret nonce: %w", err)
	}
	return service.cipher.Seal(nonce, nonce, value, []byte("adenosine:webhook:v1")), nil
}

func (service *Service) decrypt(value []byte) ([]byte, error) {
	if len(value) < service.cipher.NonceSize() {
		return nil, errors.New("webhook secret ciphertext is invalid")
	}
	plaintext, err := service.cipher.Open(nil, value[:service.cipher.NonceSize()], value[service.cipher.NonceSize():], []byte("adenosine:webhook:v1"))
	if err != nil {
		return nil, fmt.Errorf("decrypt webhook secret: %w", err)
	}
	return plaintext, nil
}

func validateInput(rawURL, secret string, events []string) error {
	parsed, err := url.ParseRequestURI(rawURL)
	if err != nil || parsed.Scheme != "https" || parsed.Hostname() == "" || parsed.User != nil || parsed.Fragment != "" {
		return fmt.Errorf("%w: URL must be an absolute HTTPS URL without userinfo or fragment", ErrValidation)
	}
	host := strings.ToLower(parsed.Hostname())
	if host == "localhost" || strings.HasSuffix(host, ".localhost") {
		return fmt.Errorf("%w: URL host is not public", ErrValidation)
	}
	if address := net.ParseIP(host); address != nil && !publicIP(address) {
		return fmt.Errorf("%w: URL host is not public", ErrValidation)
	}
	if len(secret) < 16 || len(secret) > 256 {
		return fmt.Errorf("%w: secret must contain between 16 and 256 characters", ErrValidation)
	}
	if len(events) == 0 {
		return fmt.Errorf("%w: at least one event is required", ErrValidation)
	}
	for _, event := range events {
		if !slices.Contains(allowedEvents, event) {
			return fmt.Errorf("%w: unsupported event %q", ErrValidation, event)
		}
	}
	return nil
}

func normalizedEvents(events []string) []string {
	result := slices.Clone(events)
	slices.Sort(result)
	return slices.Compact(result)
}
func publicIP(ip net.IP) bool {
	return ip.IsGlobalUnicast() && !ip.IsLoopback() && !ip.IsPrivate() && !ip.IsLinkLocalUnicast() && !ip.IsLinkLocalMulticast() && !ip.IsMulticast() && !ip.IsUnspecified()
}
func webhookFromRow(row dbgen.CoreRepositoryWebhook) Webhook {
	return Webhook{ID: uuid.UUID(row.ID.Bytes), RepositoryID: repository.ID(row.RepositoryID.Bytes), URL: row.Url, Events: slices.Clone(row.Events), Enabled: row.Enabled, CreatedAt: row.CreatedAt.Time, UpdatedAt: row.UpdatedAt.Time}
}
func deliveryFromRow(row dbgen.OpsWebhookDelivery) Delivery {
	value := Delivery{ID: uuid.UUID(row.ID.Bytes), WebhookID: uuid.UUID(row.WebhookID.Bytes), EventType: row.EventType, Attempts: int(row.Attempts), ResponseBody: row.ResponseBody.String, LastErrorCode: row.LastErrorCode.String, CreatedAt: row.CreatedAt.Time}
	if row.ResponseStatus.Valid {
		status := int(row.ResponseStatus.Int32)
		value.ResponseStatus = &status
	}
	if row.DeliveredAt.Valid {
		at := row.DeliveredAt.Time
		value.DeliveredAt = &at
	}
	if row.FailedAt.Valid {
		at := row.FailedAt.Time
		value.FailedAt = &at
	}
	return value
}
func pgUUID(id uuid.UUID) pgtype.UUID { return pgtype.UUID{Bytes: id, Valid: true} }
func optionalUUID(id *uuid.UUID) pgtype.UUID {
	if id == nil {
		return pgtype.UUID{}
	}
	return pgUUID(*id)
}
func pgTime(value time.Time) pgtype.Timestamptz {
	return pgtype.Timestamptz{Time: value.UTC(), Valid: true}
}
