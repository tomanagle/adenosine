package repository

import (
	"fmt"
	"time"

	"github.com/google/uuid"
)

// SystemClock provides UTC wall-clock time.
type SystemClock struct{}

// Now returns the current time.
func (SystemClock) Now() time.Time { return time.Now() }

// UUIDv7Generator creates time-ordered repository IDs.
type UUIDv7Generator struct{}

// New returns a UUIDv7 repository ID.
func (UUIDv7Generator) New() (ID, error) {
	id, err := uuid.NewV7()
	if err != nil {
		return ID{}, fmt.Errorf("generate UUIDv7: %w", err)
	}
	return ID(id), nil
}
