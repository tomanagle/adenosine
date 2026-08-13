package branchprotection

import (
	"errors"
	"reflect"
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
		{name: "exact branch", input: Input{Pattern: "main", RequiredApprovals: 2}},
		{name: "hyphenated branch", input: Input{Pattern: "feature/account-settings", DenyDeletion: true}},
		{name: "namespace", input: Input{Pattern: "release/*", RequiredStatusChecks: []string{"ci/test"}}},
		{name: "signed commits", input: Input{Pattern: "main", RequireSignedCommits: true}},
		{name: "invalid wildcard", input: Input{Pattern: "release*", DenyDeletion: true}, wantErr: ErrValidation},
		{name: "invalid Git branch", input: Input{Pattern: "feature..name", DenyDeletion: true}, wantErr: ErrValidation},
		{name: "reserved HEAD", input: Input{Pattern: "HEAD", DenyDeletion: true}, wantErr: ErrValidation},
		{name: "approval range", input: Input{Pattern: "main", RequiredApprovals: 101}, wantErr: ErrValidation},
		{name: "duplicate exact context", input: Input{Pattern: "main", RequiredStatusChecks: []string{"ci/test", "ci/test"}}, wantErr: ErrValidation},
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

func TestInputNormalization(t *testing.T) {
	t.Parallel()
	testCases := []struct {
		name       string
		input      Input
		wantChecks []string
	}{
		{name: "contexts are canonical", input: Input{Pattern: " main ", RequiredStatusChecks: []string{" lint ", "build"}}, wantChecks: []string{"build", "lint"}},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			value, err := testCase.input.normalized()
			if err != nil {
				t.Fatalf("normalized() error = %v", err)
			}
			if value.Pattern != "main" || !reflect.DeepEqual(value.RequiredStatusChecks, testCase.wantChecks) {
				t.Fatalf("normalized() = %+v, want checks %v", value, testCase.wantChecks)
			}
		})
	}
}

func TestSelectPrecedence(t *testing.T) {
	t.Parallel()
	protections := []Protection{
		{Pattern: "*"},
		{Pattern: "release/*"},
		{Pattern: "release/hotfix/*"},
		{Pattern: "release/hotfix/urgent"},
	}
	testCases := []struct {
		name        string
		branch      string
		wantPattern string
	}{
		{name: "fallback", branch: "feature/login", wantPattern: "*"},
		{name: "namespace", branch: "release/v1", wantPattern: "release/*"},
		{name: "longest namespace", branch: "release/hotfix/next", wantPattern: "release/hotfix/*"},
		{name: "exact", branch: "release/hotfix/urgent", wantPattern: "release/hotfix/urgent"},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			selected := Select(protections, testCase.branch)
			if selected == nil || selected.Pattern != testCase.wantPattern {
				t.Fatalf("Select() = %+v, want pattern %q", selected, testCase.wantPattern)
			}
		})
	}
}
