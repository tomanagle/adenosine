package federation

import (
	"context"
	"fmt"
	"time"

	"github.com/adenosine-dev/adenosine/internal/database"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
)

const defaultConsumer = "adenosine"

var (
	federationMeter                 = otel.Meter("github.com/adenosine-dev/adenosine/internal/federation")
	federationEvents, _             = federationMeter.Int64Counter("adenosine.federation.events")
	federationErrors, _             = federationMeter.Int64Counter("adenosine.federation.errors")
	federationProcessingDuration, _ = federationMeter.Float64Histogram("adenosine.federation.processing.duration", metric.WithUnit("s"))
)

// Result describes a durable processing decision suitable for a webhook response.
type Result struct {
	EventID   int64
	Outcome   string
	Duplicate bool
	Rejection string
}

type eventStore interface {
	Store(context.Context, Event, string) (bool, error)
}

// Processor validates untrusted Tap events and atomically persists their outcome.
type Processor struct {
	store eventStore
}

// NewProcessor constructs a PostgreSQL-backed federation processor.
func NewProcessor(db *database.DB, consumer string) *Processor {
	if consumer == "" {
		consumer = defaultConsumer
	}
	return &Processor{store: &postgresStore{db: db, consumer: consumer, now: time.Now}}
}

// Process validates and durably applies or rejects one Tap event.
func (processor *Processor) Process(ctx context.Context, body []byte) (Result, error) {
	ctx, span := otel.Tracer("github.com/adenosine-dev/adenosine/internal/federation").Start(ctx, "federation.process")
	defer span.End()
	started := time.Now()
	resultName := "applied"
	collection := "none"
	duplicateName := "false"
	defer func() {
		attrs := federationAttributes(resultName, collection, duplicateName)
		federationEvents.Add(ctx, 1, metric.WithAttributes(attrs...))
		federationProcessingDuration.Record(ctx, time.Since(started).Seconds(), metric.WithAttributes(attrs...))
	}()

	_, decodeSpan := otel.Tracer("github.com/adenosine-dev/adenosine/internal/federation").Start(ctx, "federation.validate")
	event, validationErr := DecodeEvent(body)
	if event.Record != nil {
		collection = boundedCollection(event.Record.Collection)
	} else if event.Identity != nil {
		collection = "identity"
	}
	if validationErr != nil {
		decodeSpan.SetStatus(codes.Error, "federation validation failed")
	}
	decodeSpan.End()
	if validationErr != nil {
		resultName = "rejected"
		id, ok := EventID(body)
		if !ok {
			resultName = "invalid"
			federationErrors.Add(ctx, 1, metric.WithAttributes(attribute.String("federation.stage", "validate")))
			span.SetStatus(codes.Error, "federation envelope invalid")
			return Result{}, validationErr
		}
		event = Event{ID: id}
	}
	rejection := ""
	if validationErr != nil {
		rejection = validationErr.Error()
	}
	storeCtx, storeSpan := otel.Tracer("github.com/adenosine-dev/adenosine/internal/federation").Start(ctx, "federation.project", trace.WithAttributes(attribute.String("federation.collection", collection)))
	duplicate, err := processor.store.Store(storeCtx, event, rejection)
	if err != nil {
		resultName = "failure"
		storeSpan.SetStatus(codes.Error, "federation projection failed")
		storeSpan.End()
		federationErrors.Add(ctx, 1, metric.WithAttributes(attribute.String("federation.stage", "project")))
		span.SetStatus(codes.Error, "federation processing failed")
		return Result{}, fmt.Errorf("store federation event %d: %w", event.ID, err)
	}
	storeSpan.End()
	if duplicate {
		duplicateName = "true"
	}
	result := Result{EventID: event.ID, Outcome: "applied", Duplicate: duplicate, Rejection: rejection}
	if validationErr != nil {
		result.Outcome = "rejected"
	}
	return result, nil
}

func federationAttributes(result, collection, duplicate string) []attribute.KeyValue {
	return []attribute.KeyValue{
		attribute.String("federation.result", result),
		attribute.String("federation.collection", collection),
		attribute.String("federation.duplicate", duplicate),
	}
}

func boundedCollection(collection string) string {
	switch collection {
	case ProfileCollection, RepositoryCollection, StarCollection, IssueCollection, IssueStatusCollection,
		PullRequestCollection, PullRequestStatusCollection, PullRequestReviewCollection,
		RepositoryLabelCollection, RepositoryMilestoneCollection, SubjectTriageCollection,
		OrganizationCollection, OrganizationGrantCollection, OrganizationMembershipCollection, OrganizationRevocationCollection,
		RepositoryTransferCollection, RepositoryTransferAcceptanceCollection:
		return collection
	default:
		return "other"
	}
}
