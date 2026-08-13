package restapi

import (
	"errors"
	"testing"
)

func TestPaginate(t *testing.T) {
	testCases := []struct {
		name       string
		values     []string
		limit      int
		cursor     func(t *testing.T) *string
		scope      string
		wantItems  []string
		wantCursor bool
		wantErr    error
	}{
		{name: "first page", values: []string{"a", "b", "c"}, limit: 2, scope: "letters", wantItems: []string{"a", "b"}, wantCursor: true},
		{name: "final page", values: []string{"a", "b", "c"}, limit: 2, scope: "letters", cursor: func(t *testing.T) *string {
			t.Helper()
			value, err := encodePageCursor(pageCursor{Version: cursorVersion, Scope: "letters", Key: "b"})
			if err != nil {
				t.Fatal(err)
			}
			return &value
		}, wantItems: []string{"c"}},
		{name: "cursor is bound to scope", values: []string{"a"}, limit: 1, scope: "letters", cursor: func(t *testing.T) *string {
			t.Helper()
			value, err := encodePageCursor(pageCursor{Version: cursorVersion, Scope: "numbers", Key: "a"})
			if err != nil {
				t.Fatal(err)
			}
			return &value
		}, wantErr: errInvalidCursor},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			var cursor *string
			if testCase.cursor != nil {
				cursor = testCase.cursor(t)
			}
			items, next, err := paginate(testCase.values, &testCase.limit, cursor, testCase.scope, func(value string) string { return value })
			if !errors.Is(err, testCase.wantErr) {
				t.Fatalf("paginate error = %v, want %v", err, testCase.wantErr)
			}
			if len(items) != len(testCase.wantItems) {
				t.Fatalf("items = %v, want %v", items, testCase.wantItems)
			}
			for index := range items {
				if items[index] != testCase.wantItems[index] {
					t.Fatalf("items = %v, want %v", items, testCase.wantItems)
				}
			}
			if (next != nil) != testCase.wantCursor {
				t.Fatalf("next cursor present = %t, want %t", next != nil, testCase.wantCursor)
			}
		})
	}
}
