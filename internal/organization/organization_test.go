package organization

import (
	"errors"
	"testing"
	"time"
)

func TestCreateInputValidate(t *testing.T) {
	t.Parallel()
	testCases := []struct {
		name    string
		input   CreateInput
		wantErr error
	}{
		{
			name:  "valid GitHub-shaped organization",
			input: CreateInput{CreatorDID: "did:plc:alice", Slug: "adenosine-labs", Name: "Adenosine Labs", Website: "https://adenosine.dev", BasePermission: BasePermissionRead},
		},
		{
			name:    "uppercase slug",
			input:   CreateInput{CreatorDID: "did:plc:alice", Slug: "Adenosine", Name: "Adenosine", BasePermission: BasePermissionRead},
			wantErr: ErrValidation,
		},
		{
			name:    "reserved application route",
			input:   CreateInput{CreatorDID: "did:plc:alice", Slug: "explore", Name: "Explore", BasePermission: BasePermissionRead},
			wantErr: ErrValidation,
		},
		{
			name:    "unsafe website",
			input:   CreateInput{CreatorDID: "did:plc:alice", Slug: "adenosine", Name: "Adenosine", Website: "javascript:alert(1)", BasePermission: BasePermissionRead},
			wantErr: ErrValidation,
		},
		{
			name:    "unknown base permission",
			input:   CreateInput{CreatorDID: "did:plc:alice", Slug: "adenosine", Name: "Adenosine", BasePermission: "owner"},
			wantErr: ErrValidation,
		},
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

func TestInvitationActive(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 8, 13, 0, 0, 0, 0, time.UTC)
	revokedAt := now.Add(-time.Hour)
	acceptedAt := now.Add(-time.Hour)
	testCases := []struct {
		name       string
		invitation Invitation
		want       bool
	}{
		{name: "pending", invitation: Invitation{ExpiresAt: now.Add(time.Hour)}, want: true},
		{name: "expired", invitation: Invitation{ExpiresAt: now}},
		{name: "revoked", invitation: Invitation{ExpiresAt: now.Add(time.Hour), RevokedAt: &revokedAt}},
		{name: "accepted", invitation: Invitation{ExpiresAt: now.Add(time.Hour), AcceptedAt: &acceptedAt}},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			if got := testCase.invitation.Active(now); got != testCase.want {
				t.Fatalf("Active() = %t, want %t", got, testCase.want)
			}
		})
	}
}

func TestEffectiveRepositoryRole(t *testing.T) {
	t.Parallel()
	testCases := []struct {
		name       string
		memberRole Role
		base       BasePermission
		direct     RepositoryRole
		teams      []RepositoryRole
		want       RepositoryRole
	}{
		{name: "owner is always admin", memberRole: RoleOwner, want: RepositoryRoleAdmin},
		{name: "base permission", memberRole: RoleMember, base: BasePermissionRead, want: RepositoryRoleRead},
		{name: "direct permission wins", memberRole: RoleMember, base: BasePermissionRead, direct: RepositoryRoleMaintain, want: RepositoryRoleMaintain},
		{name: "strongest team wins", memberRole: RoleMember, base: BasePermissionWrite, direct: RepositoryRoleRead, teams: []RepositoryRole{RepositoryRoleTriage, RepositoryRoleAdmin}, want: RepositoryRoleAdmin},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			got := EffectiveRepositoryRole(testCase.memberRole, testCase.base, testCase.direct, testCase.teams)
			if got != testCase.want {
				t.Fatalf("EffectiveRepositoryRole() = %q, want %q", got, testCase.want)
			}
		})
	}
}
