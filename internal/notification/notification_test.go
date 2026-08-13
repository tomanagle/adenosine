package notification

import (
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestCursorRoundTrip(t *testing.T) {
	t.Parallel()
	testCases := []struct {
		name    string
		value   string
		wantErr error
	}{
		{name: "round trip"},
		{name: "malformed", value: "not-a-cursor", wantErr: ErrValidation},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			timestamp := time.Date(2026, 8, 14, 1, 2, 3, 456, time.UTC)
			id := uuid.MustParse("0198a5be-9c00-7000-8000-000000000001")
			value := testCase.value
			if value == "" {
				value = encodeCursor(timestamp, id)
			}
			gotTime, gotID, err := decodeCursor(value)
			if !errors.Is(err, testCase.wantErr) {
				t.Fatalf("decodeCursor() error = %v, want %v", err, testCase.wantErr)
			}
			if testCase.wantErr == nil && (!gotTime.Equal(timestamp) || gotID != id) {
				t.Fatalf("decoded = %s %s", gotTime, gotID)
			}
		})
	}
}
