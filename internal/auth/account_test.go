package auth

import (
	"testing"

	dbgen "github.com/adenosine-dev/adenosine/internal/database/generated"
	"github.com/jackc/pgx/v5/pgtype"
)

func TestAccountFromRowPreservesOptionalHandle(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name string
	}{
		{name: "optional handle"},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			withHandle := accountFromRow(dbgen.CoreAccount{
				Did:         "did:plc:alice",
				HandleCache: pgtype.Text{String: "alice.example", Valid: true},
			})
			if withHandle.DID != "did:plc:alice" || withHandle.Handle == nil || *withHandle.Handle != "alice.example" {
				t.Fatalf("account = %#v", withHandle)
			}

			withoutHandle := accountFromRow(dbgen.CoreAccount{Did: "did:plc:bob"})
			if withoutHandle.Handle != nil {
				t.Fatalf("optional handle = %q, want nil", *withoutHandle.Handle)
			}
		})
	}
}
