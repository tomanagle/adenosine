package restapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"testing"
	"time"

	generated "github.com/adenosine-dev/adenosine/api/generated/go"
	"github.com/adenosine-dev/adenosine/internal/organization"
	"github.com/adenosine-dev/adenosine/internal/repository"
	"github.com/google/uuid"
)

type fakeOrganizationManager struct {
	value      organization.Organization
	auditPage  organization.AuditPage
	err        error
	auditCalls int
	auditActor string
	auditLimit int
	auditAfter *uuid.UUID
	member     organization.Member
	memberErr  error
}

type fakeCollaboratorManager struct {
	page      organization.CollaboratorPage
	put       organization.RepositoryCollaborator
	err       error
	operation string
	actorDID  string
	after     string
	limit     int
}

func (manager *fakeCollaboratorManager) List(_ context.Context, _ organization.ID, _ uuid.UUID, actorDID, after string, limit int) (organization.CollaboratorPage, error) {
	manager.operation, manager.actorDID, manager.after, manager.limit = "list", actorDID, after, limit
	return manager.page, manager.err
}
func (manager *fakeCollaboratorManager) Put(_ context.Context, _ organization.ID, repositoryID uuid.UUID, actorDID, collaboratorDID string, role organization.RepositoryRole) (organization.RepositoryCollaborator, error) {
	manager.operation, manager.actorDID = "put", actorDID
	value := manager.put
	value.RepositoryID, value.AccountDID, value.Role = repositoryID, collaboratorDID, role
	return value, manager.err
}
func (manager *fakeCollaboratorManager) Remove(_ context.Context, _ organization.ID, _ uuid.UUID, actorDID, _ string) error {
	manager.operation, manager.actorDID = "remove", actorDID
	return manager.err
}

func (*fakeOrganizationManager) Create(context.Context, organization.CreateInput) (organization.Organization, error) {
	return organization.Organization{}, nil
}
func (*fakeOrganizationManager) Update(context.Context, organization.UpdateInput) (organization.Organization, error) {
	return organization.Organization{}, nil
}
func (manager *fakeOrganizationManager) GetBySlug(context.Context, string) (organization.Organization, error) {
	return manager.value, nil
}
func (*fakeOrganizationManager) ListForAccount(context.Context, string) ([]organization.Organization, error) {
	return nil, nil
}
func (*fakeOrganizationManager) ListMembers(context.Context, organization.ID) ([]organization.Member, error) {
	return nil, nil
}
func (manager *fakeOrganizationManager) GetMember(context.Context, organization.ID, string) (organization.Member, error) {
	return manager.member, manager.memberErr
}
func (*fakeOrganizationManager) Invite(context.Context, organization.InviteInput) (organization.Invitation, error) {
	return organization.Invitation{}, nil
}
func (*fakeOrganizationManager) Accept(context.Context, uuid.UUID, string) (organization.Member, error) {
	return organization.Member{}, nil
}
func (*fakeOrganizationManager) ListPendingInvitations(context.Context, string) ([]organization.Invitation, error) {
	return nil, nil
}
func (*fakeOrganizationManager) ListInvitations(context.Context, organization.ID, string) ([]organization.Invitation, error) {
	return nil, nil
}
func (*fakeOrganizationManager) RevokeInvitation(context.Context, organization.ID, uuid.UUID, string) error {
	return nil
}
func (*fakeOrganizationManager) SetVisibility(context.Context, organization.ID, string, organization.MembershipVisibility) (organization.Member, error) {
	return organization.Member{}, nil
}
func (*fakeOrganizationManager) ChangeRole(context.Context, organization.ID, string, string, organization.Role) (organization.Member, error) {
	return organization.Member{}, nil
}
func (*fakeOrganizationManager) Remove(context.Context, organization.ID, string, string) error {
	return nil
}
func (manager *fakeOrganizationManager) AuditEvents(_ context.Context, _ organization.ID, actor string, limit int, after *uuid.UUID) (organization.AuditPage, error) {
	manager.auditCalls++
	manager.auditActor, manager.auditLimit, manager.auditAfter = actor, limit, after
	return manager.auditPage, manager.err
}

func TestOrganizationAuditEndpoint(t *testing.T) {
	t.Parallel()
	organizationID := organization.ID(uuid.MustParse("0198a851-2a89-7ae2-a370-dc68883e3af1"))
	eventID := uuid.MustParse("0198a851-2a89-7ae2-a370-dc68883e3af2")
	nextID := uuid.MustParse("0198a851-2a89-7ae2-a370-dc68883e3af3")
	now := time.Date(2026, 8, 13, 1, 2, 3, 0, time.UTC)
	testCases := []struct {
		name       string
		path       string
		session    bool
		manager    *fakeOrganizationManager
		wantStatus int
		wantCalls  int
		wantCode   string
	}{
		{name: "owner receives items envelope and cursor", path: "/api/v1/organizations/adenosine/audit-log?limit=2", session: true, manager: &fakeOrganizationManager{value: organization.Organization{ID: organizationID, Slug: "adenosine"}, auditPage: organization.AuditPage{Items: []organization.AuditEvent{{ID: eventID, ActorDID: "did:plc:alice", Action: "member.role", TargetType: "member", TargetID: "did:plc:bob", RequestID: "request-1", Metadata: json.RawMessage(`{"to":"owner"}`), CreatedAt: now}}, NextCursor: &nextID}}, wantStatus: http.StatusOK, wantCalls: 1},
		{name: "session is required", path: "/api/v1/organizations/adenosine/audit-log", manager: &fakeOrganizationManager{value: organization.Organization{ID: organizationID, Slug: "adenosine"}}, wantStatus: http.StatusUnauthorized, wantCode: "authentication_required"},
		{name: "invalid cursor is rejected before service", path: "/api/v1/organizations/adenosine/audit-log?cursor=invalid", session: true, manager: &fakeOrganizationManager{value: organization.Organization{ID: organizationID, Slug: "adenosine"}}, wantStatus: http.StatusBadRequest},
		{name: "nonowner is forbidden", path: "/api/v1/organizations/adenosine/audit-log", session: true, manager: &fakeOrganizationManager{value: organization.Organization{ID: organizationID, Slug: "adenosine"}, err: organization.ErrForbidden}, wantStatus: http.StatusForbidden, wantCalls: 1, wantCode: "permission_denied"},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			server := testAPIServer(t, Dependencies{Sessions: fakeSessions{}, Organizations: testCase.manager})
			response := performAPIRequest(server, http.MethodGet, testCase.path, "", testCase.session, false, "")
			if response.Code != testCase.wantStatus || testCase.manager.auditCalls != testCase.wantCalls {
				t.Fatalf("status/calls = %d/%d, want %d/%d: %s", response.Code, testCase.manager.auditCalls, testCase.wantStatus, testCase.wantCalls, response.Body.String())
			}
			if testCase.wantCode != "" {
				var body generated.ErrorResponse
				if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil || body.Error.Code != testCase.wantCode {
					t.Fatalf("error response = %#v, %v", body, err)
				}
			}
			if testCase.wantStatus == http.StatusOK {
				var body generated.OrganizationAuditEventList
				if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
					t.Fatalf("decode response: %v", err)
				}
				if len(body.Items) != 1 || body.Items[0].Id != eventID || body.Items[0].Metadata["to"] != "owner" || body.Page.NextCursor == nil {
					t.Fatalf("audit response = %#v", body)
				}
				if testCase.manager.auditActor != "did:plc:alice" || testCase.manager.auditLimit != 2 || testCase.manager.auditAfter != nil {
					t.Fatalf("audit inputs = %q/%d/%v", testCase.manager.auditActor, testCase.manager.auditLimit, testCase.manager.auditAfter)
				}
			}
		})
	}
}

func TestOrganizationCreatorErrorMapping(t *testing.T) {
	t.Parallel()
	testCases := []struct {
		name     string
		err      error
		wantCode string
	}{
		{name: "creator remains owner", err: organization.ErrCreatorOwner, wantCode: "organization_creator_owner"},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			response := &responseCapture{}
			handler := &apiHandler{}
			request, _ := http.NewRequestWithContext(context.Background(), http.MethodPatch, "/", nil)
			handler.writeError(response, request, testCase.err)
			if response.status != http.StatusConflict || !errors.Is(testCase.err, organization.ErrCreatorOwner) {
				t.Fatalf("status = %d, want %d", response.status, http.StatusConflict)
			}
			var body generated.ErrorResponse
			if err := json.Unmarshal(response.body, &body); err != nil || body.Error.Code != testCase.wantCode {
				t.Fatalf("error response = %#v, %v", body, err)
			}
		})
	}
}

func TestOrganizationRepositoryCollaboratorEndpoints(t *testing.T) {
	t.Parallel()
	organizationID := organization.ID(uuid.MustParse("0198a851-2a89-7ae2-a370-dc68883e3af1"))
	repositoryID := uuid.MustParse("0198a851-2a89-7ae2-a370-dc68883e3af2")
	now := time.Date(2026, 8, 13, 1, 2, 3, 0, time.UTC)
	basePath := "/api/v1/organizations/adenosine/repositories/" + repositoryID.String() + "/collaborators"
	testCases := []struct {
		name          string
		method        string
		path          string
		body          string
		session       bool
		origin        bool
		manager       *fakeCollaboratorManager
		wantStatus    int
		wantOperation string
		wantItems     int
	}{
		{name: "lists collaborators in items envelope", method: http.MethodGet, path: basePath + "?limit=2", session: true, manager: &fakeCollaboratorManager{page: organization.CollaboratorPage{Items: []organization.RepositoryCollaborator{{RepositoryID: repositoryID, AccountDID: "did:plc:bob", Role: organization.RepositoryRoleRead, CreatedAt: now, UpdatedAt: now}}}}, wantStatus: http.StatusOK, wantOperation: "list", wantItems: 1},
		{name: "assigns collaborator", method: http.MethodPut, path: basePath + "/did:plc:bob", body: `{"role":"maintain"}`, session: true, origin: true, manager: &fakeCollaboratorManager{put: organization.RepositoryCollaborator{CreatedAt: now, UpdatedAt: now}}, wantStatus: http.StatusOK, wantOperation: "put"},
		{name: "removes collaborator", method: http.MethodDelete, path: basePath + "/did:plc:bob", session: true, origin: true, manager: &fakeCollaboratorManager{}, wantStatus: http.StatusNoContent, wantOperation: "remove"},
		{name: "mutation requires origin", method: http.MethodPut, path: basePath + "/did:plc:bob", body: `{"role":"read"}`, session: true, manager: &fakeCollaboratorManager{}, wantStatus: http.StatusForbidden},
		{name: "list requires session", method: http.MethodGet, path: basePath, manager: &fakeCollaboratorManager{}, wantStatus: http.StatusUnauthorized},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			organizations := &fakeOrganizationManager{value: organization.Organization{ID: organizationID, Slug: "adenosine"}}
			server := testAPIServer(t, Dependencies{Sessions: fakeSessions{}, Organizations: organizations, Collaborators: testCase.manager})
			response := performAPIRequest(server, testCase.method, testCase.path, testCase.body, testCase.session, testCase.origin, "")
			if response.Code != testCase.wantStatus || testCase.manager.operation != testCase.wantOperation {
				t.Fatalf("status/operation = %d/%q, want %d/%q: %s", response.Code, testCase.manager.operation, testCase.wantStatus, testCase.wantOperation, response.Body.String())
			}
			if testCase.wantOperation != "" && testCase.manager.actorDID != "did:plc:alice" {
				t.Fatalf("actor DID = %q", testCase.manager.actorDID)
			}
			if testCase.wantItems > 0 {
				var body generated.OrganizationRepositoryCollaboratorList
				if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil || len(body.Items) != testCase.wantItems {
					t.Fatalf("collaborator response = %#v, %v", body, err)
				}
			}
		})
	}
}

type fakeOrganizationTeamManager struct {
	operation   string
	updateInput organization.UpdateTeamInput
	deleteTeam  uuid.UUID
	actorDID    string
	team        organization.Team
	err         error
}

func (*fakeOrganizationTeamManager) Create(context.Context, organization.CreateTeamInput) (organization.Team, error) {
	return organization.Team{}, nil
}
func (manager *fakeOrganizationTeamManager) Update(_ context.Context, input organization.UpdateTeamInput) (organization.Team, error) {
	manager.operation, manager.updateInput, manager.actorDID = "update", input, input.ActorDID
	return manager.team, manager.err
}
func (manager *fakeOrganizationTeamManager) Delete(_ context.Context, _ organization.ID, teamID uuid.UUID, actorDID string) error {
	manager.operation, manager.deleteTeam, manager.actorDID = "delete", teamID, actorDID
	return manager.err
}
func (*fakeOrganizationTeamManager) List(context.Context, organization.ID, string) ([]organization.Team, error) {
	return nil, nil
}
func (*fakeOrganizationTeamManager) Members(context.Context, organization.ID, uuid.UUID, string) ([]organization.TeamMember, error) {
	return nil, nil
}
func (*fakeOrganizationTeamManager) PutMember(context.Context, organization.ID, uuid.UUID, string, string, organization.TeamRole) (organization.TeamMember, error) {
	return organization.TeamMember{}, nil
}
func (*fakeOrganizationTeamManager) RemoveMember(context.Context, organization.ID, uuid.UUID, string, string) error {
	return nil
}
func (*fakeOrganizationTeamManager) Repositories(context.Context, organization.ID, uuid.UUID, string) ([]organization.TeamRepository, error) {
	return nil, nil
}
func (*fakeOrganizationTeamManager) PutRepository(context.Context, organization.ID, uuid.UUID, string, uuid.UUID, organization.RepositoryRole) (organization.TeamRepository, error) {
	return organization.TeamRepository{}, nil
}
func (*fakeOrganizationTeamManager) RemoveRepository(context.Context, organization.ID, uuid.UUID, string, uuid.UUID) error {
	return nil
}

func TestOrganizationTeamSettingsEndpoints(t *testing.T) {
	t.Parallel()
	organizationID := organization.ID(uuid.MustParse("0198a851-2a89-7ae2-a370-dc68883e3af1"))
	teamID := uuid.MustParse("0198a851-2a89-7ae2-a370-dc68883e3af2")
	now := time.Date(2026, 8, 13, 1, 2, 3, 0, time.UTC)
	basePath := "/api/v1/organizations/adenosine/teams/" + teamID.String()
	testCases := []struct {
		name          string
		method        string
		body          string
		manager       *fakeOrganizationTeamManager
		wantStatus    int
		wantOperation string
	}{
		{name: "updates team settings", method: http.MethodPatch, body: `{"name":"Core","description":"Platform","visibility":"visible","parent_team_id":null}`, manager: &fakeOrganizationTeamManager{team: organization.Team{ID: teamID, OrganizationID: organizationID, Slug: "core", Name: "Core", Description: "Platform", Visibility: organization.TeamVisibilityVisible, CreatedAt: now, UpdatedAt: now}}, wantStatus: http.StatusOK, wantOperation: "update"},
		{name: "deletes team hierarchy", method: http.MethodDelete, manager: &fakeOrganizationTeamManager{}, wantStatus: http.StatusNoContent, wantOperation: "delete"},
		{name: "mutation requires same origin", method: http.MethodPatch, body: `{"name":"Core","visibility":"visible","parent_team_id":null}`, manager: &fakeOrganizationTeamManager{}, wantStatus: http.StatusForbidden},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			organizations := &fakeOrganizationManager{value: organization.Organization{ID: organizationID, Slug: "adenosine"}}
			server := testAPIServer(t, Dependencies{Sessions: fakeSessions{}, Organizations: organizations, Teams: testCase.manager})
			response := performAPIRequest(server, testCase.method, basePath, testCase.body, true, testCase.wantOperation != "", "")
			if response.Code != testCase.wantStatus || testCase.manager.operation != testCase.wantOperation {
				t.Fatalf("status/operation = %d/%q, want %d/%q: %s", response.Code, testCase.manager.operation, testCase.wantStatus, testCase.wantOperation, response.Body.String())
			}
			if testCase.wantOperation != "" && testCase.manager.actorDID != "did:plc:alice" {
				t.Fatalf("actor DID = %q", testCase.manager.actorDID)
			}
			if testCase.wantOperation == "update" && (testCase.manager.updateInput.TeamID != teamID || testCase.manager.updateInput.Name != "Core" || testCase.manager.updateInput.Visibility != organization.TeamVisibilityVisible || testCase.manager.updateInput.ParentTeamID != nil) {
				t.Fatalf("update input = %#v", testCase.manager.updateInput)
			}
			if testCase.wantOperation == "delete" && testCase.manager.deleteTeam != teamID {
				t.Fatalf("deleted team = %s", testCase.manager.deleteTeam)
			}
		})
	}
}

type fakeOrganizationRepositoryPager struct {
	page      repository.Page
	pageErr   error
	pageCalls int
	actorDID  string
	after     *uuid.UUID
	limit     int
}

func (*fakeOrganizationRepositoryPager) Create(context.Context, repository.CreateInput) (repository.Repository, error) {
	return repository.Repository{}, nil
}
func (*fakeOrganizationRepositoryPager) GetByOwnerSlug(context.Context, string, string) (repository.Repository, error) {
	return repository.Repository{}, repository.ErrNotFound
}
func (*fakeOrganizationRepositoryPager) ListByOrganization(context.Context, uuid.UUID) ([]repository.Repository, error) {
	return nil, nil
}
func (manager *fakeOrganizationRepositoryPager) PageByOrganization(_ context.Context, _ uuid.UUID, actorDID string, after *uuid.UUID, limit int) (repository.Page, error) {
	manager.pageCalls++
	manager.actorDID, manager.after, manager.limit = actorDID, after, limit
	return manager.page, manager.pageErr
}

func TestOrganizationRepositoryPagingEndpoint(t *testing.T) {
	t.Parallel()
	organizationID := organization.ID(uuid.MustParse("0198a851-2a89-7ae2-a370-dc68883e3af1"))
	repositoryID := repository.ID(uuid.MustParse("0198a851-2a89-7ae2-a370-dc68883e3af2"))
	nextID := uuid.MustParse("0198a851-2a89-7ae2-a370-dc68883e3af3")
	now := time.Date(2026, 8, 13, 1, 2, 3, 0, time.UTC)
	testCases := []struct {
		name       string
		path       string
		manager    *fakeOrganizationRepositoryPager
		wantStatus int
		wantCalls  int
	}{
		{name: "uses viewer-aware keyset page", path: "/api/v1/organizations/adenosine/repositories?limit=2", manager: &fakeOrganizationRepositoryPager{page: repository.Page{Items: []repository.Repository{{ID: repositoryID, OwnerDID: "did:plc:alice", Slug: "core", Visibility: repository.VisibilityPrivate, State: repository.StateActive, DefaultBranch: "main", ViewerCanAdmin: true, CreatedAt: now, UpdatedAt: now}}, NextCursor: &nextID}}, wantStatus: http.StatusOK, wantCalls: 1},
		{name: "rejects malformed cursor before paging", path: "/api/v1/organizations/adenosine/repositories?cursor=invalid", manager: &fakeOrganizationRepositoryPager{}, wantStatus: http.StatusBadRequest},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			organizations := &fakeOrganizationManager{value: organization.Organization{ID: organizationID, Slug: "adenosine"}, member: organization.Member{OrganizationID: organizationID, AccountDID: "did:plc:alice", Role: organization.RoleOwner}}
			server := testAPIServer(t, Dependencies{Sessions: fakeSessions{}, Organizations: organizations, Repositories: testCase.manager})
			response := performAPIRequest(server, http.MethodGet, testCase.path, "", true, false, "")
			if response.Code != testCase.wantStatus || testCase.manager.pageCalls != testCase.wantCalls {
				t.Fatalf("status/calls = %d/%d, want %d/%d: %s", response.Code, testCase.manager.pageCalls, testCase.wantStatus, testCase.wantCalls, response.Body.String())
			}
			if testCase.wantStatus == http.StatusOK {
				var body generated.RepositoryList
				if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil || len(body.Items) != 1 || body.Items[0].ViewerCanAdmin == nil || !*body.Items[0].ViewerCanAdmin || body.Page.NextCursor == nil {
					t.Fatalf("repository response = %#v, %v", body, err)
				}
				if testCase.manager.actorDID != "did:plc:alice" || testCase.manager.after != nil || testCase.manager.limit != 2 {
					t.Fatalf("page inputs = %q/%v/%d", testCase.manager.actorDID, testCase.manager.after, testCase.manager.limit)
				}
			}
		})
	}
}

type responseCapture struct {
	header http.Header
	status int
	body   []byte
}

func (response *responseCapture) Header() http.Header {
	if response.header == nil {
		response.header = http.Header{}
	}
	return response.header
}
func (response *responseCapture) WriteHeader(status int) { response.status = status }
func (response *responseCapture) Write(value []byte) (int, error) {
	response.body = append(response.body, value...)
	return len(value), nil
}
