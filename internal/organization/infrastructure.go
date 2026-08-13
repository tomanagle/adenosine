package organization

import (
	"fmt"
	"time"

	"github.com/google/uuid"
)

// SystemClock supplies UTC wall-clock time.
type SystemClock struct{}

func (SystemClock) Now() time.Time { return time.Now() }

// UUIDv7Generator creates time-ordered organization identifiers.
type UUIDv7Generator struct{}

func (UUIDv7Generator) New() (ID, error) {
	id, err := uuid.NewV7()
	if err != nil {
		return ID{}, fmt.Errorf("generate organization UUIDv7: %w", err)
	}
	return ID(id), nil
}
