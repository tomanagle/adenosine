package branchprotection

import (
	"errors"
	"testing"
)

func TestInputValidate(t *testing.T) {
	t.Parallel()
	testCases := []struct {
		name    string
		input   Input
		wantErr error
	}{
		{name: "force push and deletion", input: Input{Pattern: "*", DenyForcePush: true, DenyDeletion: true}},
		{name: "deletion only", input: Input{Pattern: "*", DenyDeletion: true}},
		{name: "branch pattern unsupported", input: Input{Pattern: "main", DenyForcePush: true}, wantErr: ErrValidation},
		{name: "empty policy", input: Input{Pattern: "*"}, wantErr: ErrValidation},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			err := testCase.input.Validate()
			if !errors.Is(err, testCase.wantErr) {
				t.Fatalf("Validate() error = %v, want %v", err, testCase.wantErr)
			}
		})
	}
}
