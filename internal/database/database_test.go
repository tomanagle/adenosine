package database

import "testing"

func TestDatabaseOperationRedactsStatement(t *testing.T) {
	testCases := []struct {
		name      string
		statement string
		want      string
	}{
		{name: "generated query comment", statement: "-- name: Account :one\nSELECT secret FROM auth.accounts", want: "select"},
		{name: "mutation", statement: "INSERT INTO ops.outbox_events VALUES ($1)", want: "insert"},
		{name: "unknown", statement: "VACUUM", want: "other"},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			if got := databaseOperation(testCase.statement); got != testCase.want {
				t.Fatalf("databaseOperation() = %q, want %q", got, testCase.want)
			}
		})
	}
}
