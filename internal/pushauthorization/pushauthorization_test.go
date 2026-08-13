package pushauthorization

import (
	"strings"
	"testing"
)

func TestParseUpdates(t *testing.T) {
	t.Parallel()
	oldSHA, newSHA := strings.Repeat("a", 40), strings.Repeat("b", 40)
	testCases := []struct {
		name      string
		input     string
		wantCount int
		wantErr   bool
	}{
		{name: "one branch", input: oldSHA + " " + newSHA + " refs/heads/main\n", wantCount: 1},
		{name: "atomic branches", input: oldSHA + " " + newSHA + " refs/heads/main\n" + oldSHA + " " + newSHA + " refs/heads/release\n", wantCount: 2},
		{name: "missing tuple", input: oldSHA + " " + newSHA + "\n", wantErr: true},
		{name: "empty", wantErr: true},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			updates, err := parseUpdates(strings.NewReader(testCase.input))
			if (err != nil) != testCase.wantErr || len(updates) != testCase.wantCount {
				t.Fatalf("parseUpdates() = %d updates, error %v", len(updates), err)
			}
		})
	}
}
