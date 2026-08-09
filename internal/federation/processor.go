package federation

import (
	"context"
	"fmt"
	"time"

	"github.com/adenosine-dev/adenosine/internal/database"
)

const defaultConsumer = "adenosine"

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
	event, validationErr := DecodeEvent(body)
	if validationErr != nil {
		id, ok := EventID(body)
		if !ok {
			return Result{}, validationErr
		}
		event = Event{ID: id}
	}
	rejection := ""
	if validationErr != nil {
		rejection = validationErr.Error()
	}
	duplicate, err := processor.store.Store(ctx, event, rejection)
	if err != nil {
		return Result{}, fmt.Errorf("store federation event %d: %w", event.ID, err)
	}
	result := Result{EventID: event.ID, Outcome: "applied", Duplicate: duplicate, Rejection: rejection}
	if validationErr != nil {
		result.Outcome = "rejected"
	}
	return result, nil
}
