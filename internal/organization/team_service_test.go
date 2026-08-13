package organization

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
)

type memoryTeamStore struct {
	owner             Member
	teamMember        TeamMember
	team              Team
	ownerErr          error
	teamMemberErr     error
	putMemberCalls    int
	removeMemberCalls int
	createCalls       int
	updateCalls       int
	deleteCalls       int
	hasChildren       bool
	isDescendant      bool
	audits            []AuditEvent
}

func (store *memoryTeamStore) GetOwner(context.Context, ID, string) (Member, error) {
	if store.ownerErr != nil {
		return Member{}, store.ownerErr
	}
	return store.owner, nil
}

func (*memoryTeamStore) GetMember(context.Context, ID, string) (Member, error) {
	return Member{AccountDID: "did:plc:viewer", Role: RoleMember}, nil
}

func (store *memoryTeamStore) CreateTeam(_ context.Context, value Team) (Team, error) {
	store.createCalls++
	return value, nil
}
func (store *memoryTeamStore) UpdateTeam(_ context.Context, value Team) (Team, error) {
	store.updateCalls++
	store.team = value
	return value, nil
}
func (store *memoryTeamStore) DeleteTeam(context.Context, ID, uuid.UUID, time.Time) (int64, error) {
	store.deleteCalls++
	return 1, nil
}
func (store *memoryTeamStore) TeamHasChildren(context.Context, ID, uuid.UUID) (bool, error) {
	return store.hasChildren, nil
}
func (store *memoryTeamStore) IsTeamDescendant(context.Context, ID, uuid.UUID, uuid.UUID) (bool, error) {
	return store.isDescendant, nil
}
func (store *memoryTeamStore) ListTeams(context.Context, ID, string) ([]Team, error) {
	return []Team{store.team}, nil
}
func (store *memoryTeamStore) PageTeams(context.Context, ID, string, bool, *uuid.UUID, int32) ([]Team, error) {
	return []Team{store.team}, nil
}
func (store *memoryTeamStore) GetTeam(context.Context, ID, uuid.UUID) (Team, error) {
	return store.team, nil
}
func (store *memoryTeamStore) AddTeamMember(_ context.Context, teamID uuid.UUID, did string, role TeamRole, now time.Time) (TeamMember, error) {
	store.putMemberCalls++
	return TeamMember{TeamID: teamID, AccountDID: did, Role: role, CreatedAt: now, UpdatedAt: now}, nil
}
func (store *memoryTeamStore) GetTeamMember(context.Context, uuid.UUID, string) (TeamMember, error) {
	if store.teamMemberErr != nil {
		return TeamMember{}, store.teamMemberErr
	}
	return store.teamMember, nil
}
func (*memoryTeamStore) ListTeamMembers(context.Context, uuid.UUID) ([]TeamMember, error) {
	return nil, nil
}
func (*memoryTeamStore) PageTeamMembers(context.Context, uuid.UUID, string, int32) ([]TeamMember, error) {
	return nil, nil
}
func (store *memoryTeamStore) RemoveTeamMember(context.Context, uuid.UUID, string) error {
	store.removeMemberCalls++
	return nil
}
func (*memoryTeamStore) PutTeamRepository(context.Context, uuid.UUID, uuid.UUID, RepositoryRole, time.Time) (TeamRepository, error) {
	return TeamRepository{}, nil
}
func (*memoryTeamStore) ListTeamRepositories(context.Context, uuid.UUID) ([]TeamRepository, error) {
	return nil, nil
}
func (*memoryTeamStore) PageTeamRepositories(context.Context, uuid.UUID, *uuid.UUID, int32) ([]TeamRepository, error) {
	return nil, nil
}
func (*memoryTeamStore) RemoveTeamRepository(context.Context, uuid.UUID, uuid.UUID) error { return nil }
func (store *memoryTeamStore) RecordAudit(_ context.Context, event AuditEvent) error {
	store.audits = append(store.audits, event)
	return nil
}

func TestTeamServiceCreate(t *testing.T) {
	t.Parallel()
	organizationID := ID(uuid.MustParse("0198a851-2a89-7ae2-a370-dc68883e3af1"))
	parentID := uuid.MustParse("0198a851-2a89-7ae2-a370-dc68883e3af2")
	testCases := []struct {
		name       string
		parent     *uuid.UUID
		parentTeam Team
		visibility TeamVisibility
		wantErr    error
		wantCalls  int
	}{
		{name: "creates independent secret team", visibility: TeamVisibilitySecret, wantCalls: 1},
		{name: "creates nested visible team", parent: &parentID, parentTeam: Team{ID: parentID, OrganizationID: organizationID, Visibility: TeamVisibilityVisible}, visibility: TeamVisibilityVisible, wantCalls: 1},
		{name: "rejects secret child", parent: &parentID, parentTeam: Team{ID: parentID, OrganizationID: organizationID, Visibility: TeamVisibilityVisible}, visibility: TeamVisibilitySecret, wantErr: ErrValidation},
		{name: "rejects secret parent", parent: &parentID, parentTeam: Team{ID: parentID, OrganizationID: organizationID, Visibility: TeamVisibilitySecret}, visibility: TeamVisibilityVisible, wantErr: ErrValidation},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			store := &memoryTeamStore{owner: Member{AccountDID: "did:plc:owner", Role: RoleOwner}, team: testCase.parentTeam}
			service := NewTeamService(store, fixedClock{time.Date(2026, 8, 13, 1, 2, 3, 0, time.UTC)}, fixedIDs{organizationID})
			_, err := service.Create(context.Background(), CreateTeamInput{OrganizationID: organizationID, ActorDID: "did:plc:owner", ParentTeamID: testCase.parent, Slug: "platform", Name: "Platform", Visibility: testCase.visibility})
			if !errors.Is(err, testCase.wantErr) {
				t.Fatalf("Create() error = %v, want %v", err, testCase.wantErr)
			}
			if store.createCalls != testCase.wantCalls {
				t.Fatalf("create calls = %d, want %d", store.createCalls, testCase.wantCalls)
			}
		})
	}
}

func TestTeamServiceUpdate(t *testing.T) {
	t.Parallel()
	organizationID := ID(uuid.MustParse("0198a851-2a89-7ae2-a370-dc68883e3af1"))
	teamID := uuid.MustParse("0198a851-2a89-7ae2-a370-dc68883e3af2")
	parentID := uuid.MustParse("0198a851-2a89-7ae2-a370-dc68883e3af3")
	testCases := []struct {
		name           string
		parent         *uuid.UUID
		visibility     TeamVisibility
		ownerErr       error
		teamMember     TeamMember
		hasChildren    bool
		isDescendant   bool
		wantErr        error
		wantUpdates    int
		wantAuditCount int
	}{
		{name: "owner updates team settings", visibility: TeamVisibilityVisible, wantUpdates: 1, wantAuditCount: 1},
		{name: "maintainer updates team settings", visibility: TeamVisibilityVisible, ownerErr: ErrNotFound, teamMember: TeamMember{Role: TeamRoleMaintainer}, wantUpdates: 1, wantAuditCount: 1},
		{name: "secret team cannot have children", visibility: TeamVisibilitySecret, hasChildren: true, wantErr: ErrValidation},
		{name: "secret team cannot have parent", parent: &parentID, visibility: TeamVisibilitySecret, wantErr: ErrValidation},
		{name: "team cannot move beneath descendant", parent: &parentID, visibility: TeamVisibilityVisible, isDescendant: true, wantErr: ErrValidation},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			store := &memoryTeamStore{
				owner: Member{AccountDID: "did:plc:owner", Role: RoleOwner}, team: Team{ID: teamID, OrganizationID: organizationID, Visibility: TeamVisibilityVisible},
				ownerErr: testCase.ownerErr, teamMember: testCase.teamMember, hasChildren: testCase.hasChildren, isDescendant: testCase.isDescendant,
			}
			service := NewTeamService(store, fixedClock{time.Date(2026, 8, 13, 1, 2, 3, 0, time.UTC)}, fixedIDs{organizationID})
			_, err := service.Update(context.Background(), UpdateTeamInput{OrganizationID: organizationID, TeamID: teamID, ActorDID: "did:plc:actor", ParentTeamID: testCase.parent, Name: "Platform", Visibility: testCase.visibility})
			if !errors.Is(err, testCase.wantErr) {
				t.Fatalf("Update() error = %v, want %v", err, testCase.wantErr)
			}
			if store.updateCalls != testCase.wantUpdates || len(store.audits) != testCase.wantAuditCount {
				t.Fatalf("updates/audits = %d/%d, want %d/%d", store.updateCalls, len(store.audits), testCase.wantUpdates, testCase.wantAuditCount)
			}
		})
	}
}

func TestTeamServiceDelete(t *testing.T) {
	t.Parallel()
	organizationID := ID(uuid.MustParse("0198a851-2a89-7ae2-a370-dc68883e3af1"))
	teamID := uuid.MustParse("0198a851-2a89-7ae2-a370-dc68883e3af2")
	testCases := []struct {
		name        string
		ownerErr    error
		teamMember  TeamMember
		wantErr     error
		wantDeletes int
	}{
		{name: "owner deletes team hierarchy", wantDeletes: 1},
		{name: "maintainer deletes team hierarchy", ownerErr: ErrNotFound, teamMember: TeamMember{Role: TeamRoleMaintainer}, wantDeletes: 1},
		{name: "ordinary member cannot delete team", ownerErr: ErrNotFound, teamMember: TeamMember{Role: TeamRoleMember}, wantErr: ErrForbidden},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			store := &memoryTeamStore{owner: Member{Role: RoleOwner}, team: Team{ID: teamID, OrganizationID: organizationID}, ownerErr: testCase.ownerErr, teamMember: testCase.teamMember}
			service := NewTeamService(store, fixedClock{time.Date(2026, 8, 13, 1, 2, 3, 0, time.UTC)}, fixedIDs{organizationID})
			err := service.Delete(context.Background(), organizationID, teamID, "did:plc:actor")
			if !errors.Is(err, testCase.wantErr) {
				t.Fatalf("Delete() error = %v, want %v", err, testCase.wantErr)
			}
			if store.deleteCalls != testCase.wantDeletes {
				t.Fatalf("delete calls = %d, want %d", store.deleteCalls, testCase.wantDeletes)
			}
		})
	}
}

func TestTeamServicePutMember(t *testing.T) {
	t.Parallel()
	organizationID := ID(uuid.MustParse("0198a851-2a89-7ae2-a370-dc68883e3af1"))
	teamID := uuid.MustParse("0198a851-2a89-7ae2-a370-dc68883e3af2")
	testCases := []struct {
		name          string
		ownerErr      error
		teamMember    TeamMember
		teamMemberErr error
		role          TeamRole
		wantErr       error
		wantCalls     int
	}{
		{name: "organization owner manages membership", role: TeamRoleMember, wantCalls: 1},
		{name: "team maintainer manages membership", ownerErr: ErrNotFound, teamMember: TeamMember{Role: TeamRoleMaintainer}, role: TeamRoleMaintainer, wantCalls: 1},
		{name: "ordinary team member cannot manage membership", ownerErr: ErrNotFound, teamMember: TeamMember{Role: TeamRoleMember}, role: TeamRoleMember, wantErr: ErrForbidden},
		{name: "nonmember cannot manage membership", ownerErr: ErrNotFound, teamMemberErr: ErrNotFound, role: TeamRoleMember, wantErr: ErrForbidden},
		{name: "invalid role is rejected", role: TeamRole("admin"), wantErr: ErrValidation},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			store := &memoryTeamStore{
				owner:    Member{AccountDID: "did:plc:owner", Role: RoleOwner},
				team:     Team{ID: teamID, OrganizationID: organizationID},
				ownerErr: testCase.ownerErr, teamMember: testCase.teamMember, teamMemberErr: testCase.teamMemberErr,
			}
			service := NewTeamService(store, fixedClock{time.Date(2026, 8, 13, 1, 2, 3, 0, time.UTC)}, fixedIDs{organizationID})
			_, err := service.PutMember(context.Background(), organizationID, teamID, "did:plc:actor", "did:plc:member", testCase.role)
			if !errors.Is(err, testCase.wantErr) {
				t.Fatalf("PutMember() error = %v, want %v", err, testCase.wantErr)
			}
			if store.putMemberCalls != testCase.wantCalls {
				t.Fatalf("AddTeamMember() calls = %d, want %d", store.putMemberCalls, testCase.wantCalls)
			}
			if len(store.audits) != testCase.wantCalls {
				t.Fatalf("audit events = %d, want %d", len(store.audits), testCase.wantCalls)
			}
		})
	}
}

func TestTeamServiceRemoveMember(t *testing.T) {
	t.Parallel()
	organizationID := ID(uuid.MustParse("0198a851-2a89-7ae2-a370-dc68883e3af1"))
	teamID := uuid.MustParse("0198a851-2a89-7ae2-a370-dc68883e3af2")
	testCases := []struct {
		name       string
		ownerErr   error
		teamMember TeamMember
		wantErr    error
		wantCalls  int
	}{
		{name: "organization owner removes membership", wantCalls: 1},
		{name: "team maintainer removes membership", ownerErr: ErrNotFound, teamMember: TeamMember{Role: TeamRoleMaintainer}, wantCalls: 1},
		{name: "ordinary member cannot remove membership", ownerErr: ErrNotFound, teamMember: TeamMember{Role: TeamRoleMember}, wantErr: ErrForbidden},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			store := &memoryTeamStore{
				owner:    Member{AccountDID: "did:plc:owner", Role: RoleOwner},
				team:     Team{ID: teamID, OrganizationID: organizationID},
				ownerErr: testCase.ownerErr, teamMember: testCase.teamMember,
			}
			service := NewTeamService(store, fixedClock{}, fixedIDs{organizationID})
			err := service.RemoveMember(context.Background(), organizationID, teamID, "did:plc:actor", "did:plc:member")
			if !errors.Is(err, testCase.wantErr) {
				t.Fatalf("RemoveMember() error = %v, want %v", err, testCase.wantErr)
			}
			if store.removeMemberCalls != testCase.wantCalls {
				t.Fatalf("RemoveTeamMember() calls = %d, want %d", store.removeMemberCalls, testCase.wantCalls)
			}
			if len(store.audits) != testCase.wantCalls {
				t.Fatalf("audit events = %d, want %d", len(store.audits), testCase.wantCalls)
			}
		})
	}
}
