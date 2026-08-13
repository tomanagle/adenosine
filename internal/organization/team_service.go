package organization

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
)

type teamStore interface {
	GetOwner(context.Context, ID, string) (Member, error)
	GetMember(context.Context, ID, string) (Member, error)
	CreateTeam(context.Context, Team) (Team, error)
	UpdateTeam(context.Context, Team) (Team, error)
	DeleteTeam(context.Context, ID, uuid.UUID, time.Time) (int64, error)
	TeamHasChildren(context.Context, ID, uuid.UUID) (bool, error)
	IsTeamDescendant(context.Context, ID, uuid.UUID, uuid.UUID) (bool, error)
	ListTeams(context.Context, ID, string) ([]Team, error)
	PageTeams(context.Context, ID, string, bool, *uuid.UUID, int32) ([]Team, error)
	GetTeam(context.Context, ID, uuid.UUID) (Team, error)
	AddTeamMember(context.Context, uuid.UUID, string, TeamRole, time.Time) (TeamMember, error)
	GetTeamMember(context.Context, uuid.UUID, string) (TeamMember, error)
	ListTeamMembers(context.Context, uuid.UUID) ([]TeamMember, error)
	PageTeamMembers(context.Context, uuid.UUID, string, int32) ([]TeamMember, error)
	RemoveTeamMember(context.Context, uuid.UUID, string) error
	PutTeamRepository(context.Context, uuid.UUID, uuid.UUID, RepositoryRole, time.Time) (TeamRepository, error)
	ListTeamRepositories(context.Context, uuid.UUID) ([]TeamRepository, error)
	PageTeamRepositories(context.Context, uuid.UUID, *uuid.UUID, int32) ([]TeamRepository, error)
	RemoveTeamRepository(context.Context, uuid.UUID, uuid.UUID) error
	RecordAudit(context.Context, AuditEvent) error
}

func (service *TeamService) Repositories(ctx context.Context, organizationID ID, teamID uuid.UUID, actorDID string) ([]TeamRepository, error) {
	if _, err := service.store.GetMember(ctx, organizationID, actorDID); err != nil {
		return nil, ErrForbidden
	}
	if _, err := service.store.GetTeam(ctx, organizationID, teamID); err != nil {
		return nil, fmt.Errorf("get team: %w", err)
	}
	values, err := service.store.ListTeamRepositories(ctx, teamID)
	if err != nil {
		return nil, fmt.Errorf("list team repositories: %w", err)
	}
	return values, nil
}

func (service *TeamService) PageRepositories(ctx context.Context, organizationID ID, teamID uuid.UUID, actorDID string, after *uuid.UUID, limit int) (Page[TeamRepository], error) {
	if err := validateCollectionLimit(limit); err != nil {
		return Page[TeamRepository]{}, err
	}
	if _, err := service.store.GetMember(ctx, organizationID, actorDID); err != nil {
		return Page[TeamRepository]{}, ErrForbidden
	}
	if _, err := service.store.GetTeam(ctx, organizationID, teamID); err != nil {
		return Page[TeamRepository]{}, fmt.Errorf("get team: %w", err)
	}
	values, err := service.store.PageTeamRepositories(ctx, teamID, after, int32(limit+1))
	if err != nil {
		return Page[TeamRepository]{}, fmt.Errorf("page team repositories: %w", err)
	}
	return keysetPage(values, limit, func(value TeamRepository) string { return value.RepositoryID.String() }), nil
}

func (service *TeamService) PutRepository(ctx context.Context, organizationID ID, teamID uuid.UUID, actorDID string, repositoryID uuid.UUID, role RepositoryRole) (TeamRepository, error) {
	if err := role.Validate(); err != nil {
		return TeamRepository{}, ErrValidation
	}
	if _, err := service.store.GetTeam(ctx, organizationID, teamID); err != nil {
		return TeamRepository{}, fmt.Errorf("get team: %w", err)
	}
	if err := service.authorizeMaintainer(ctx, organizationID, teamID, actorDID); err != nil {
		return TeamRepository{}, err
	}
	value, err := service.store.PutTeamRepository(ctx, teamID, repositoryID, role, service.clock.Now().UTC())
	if err != nil {
		return TeamRepository{}, fmt.Errorf("put team repository: %w", err)
	}
	if err := service.audit(ctx, organizationID, actorDID, "team.repository.put", "repository", repositoryID.String(), map[string]any{"team_id": teamID.String(), "role": role}); err != nil {
		return TeamRepository{}, err
	}
	return value, nil
}

func (service *TeamService) RemoveRepository(ctx context.Context, organizationID ID, teamID uuid.UUID, actorDID string, repositoryID uuid.UUID) error {
	if _, err := service.store.GetTeam(ctx, organizationID, teamID); err != nil {
		return fmt.Errorf("get team: %w", err)
	}
	if err := service.authorizeMaintainer(ctx, organizationID, teamID, actorDID); err != nil {
		return err
	}
	if err := service.store.RemoveTeamRepository(ctx, teamID, repositoryID); err != nil {
		return fmt.Errorf("remove team repository: %w", err)
	}
	if err := service.audit(ctx, organizationID, actorDID, "team.repository.remove", "repository", repositoryID.String(), map[string]any{"team_id": teamID.String()}); err != nil {
		return err
	}
	return nil
}

type TeamService struct {
	store teamStore
	clock clock
	ids   idGenerator
}

func NewTeamService(store teamStore, clock clock, ids idGenerator) *TeamService {
	return &TeamService{store: store, clock: clock, ids: ids}
}

func (service *TeamService) Create(ctx context.Context, input CreateTeamInput) (Team, error) {
	if err := input.Validate(); err != nil {
		return Team{}, err
	}
	if _, err := service.store.GetOwner(ctx, input.OrganizationID, input.ActorDID); err != nil {
		if errors.Is(err, ErrNotFound) {
			return Team{}, ErrForbidden
		}
		return Team{}, fmt.Errorf("authorize team creation: %w", err)
	}
	if input.ParentTeamID != nil {
		parent, err := service.store.GetTeam(ctx, input.OrganizationID, *input.ParentTeamID)
		if err != nil {
			return Team{}, fmt.Errorf("get parent team: %w", err)
		}
		if input.Visibility == TeamVisibilitySecret || parent.Visibility == TeamVisibilitySecret {
			return Team{}, fmt.Errorf("%w: secret teams cannot be nested", ErrValidation)
		}
	}
	id, err := service.ids.New()
	if err != nil {
		return Team{}, fmt.Errorf("generate team ID: %w", err)
	}
	now := service.clock.Now().UTC()
	team, err := service.store.CreateTeam(ctx, Team{ID: uuid.UUID(id), OrganizationID: input.OrganizationID, ParentTeamID: input.ParentTeamID, Slug: input.Slug, Name: input.Name, Description: input.Description, Visibility: input.Visibility, CreatedAt: now, UpdatedAt: now})
	if err != nil {
		return Team{}, fmt.Errorf("create organization team: %w", err)
	}
	if err := service.audit(ctx, input.OrganizationID, input.ActorDID, "team.create", "team", team.ID.String(), map[string]any{"slug": team.Slug, "visibility": team.Visibility, "parent_team_id": team.ParentTeamID}); err != nil {
		return Team{}, err
	}
	return team, nil
}

func (service *TeamService) Update(ctx context.Context, input UpdateTeamInput) (Team, error) {
	if err := input.Validate(); err != nil {
		return Team{}, err
	}
	current, err := service.store.GetTeam(ctx, input.OrganizationID, input.TeamID)
	if err != nil {
		return Team{}, fmt.Errorf("get team: %w", err)
	}
	if err := service.authorizeMaintainer(ctx, input.OrganizationID, input.TeamID, input.ActorDID); err != nil {
		return Team{}, err
	}
	if input.ParentTeamID != nil {
		parent, err := service.store.GetTeam(ctx, input.OrganizationID, *input.ParentTeamID)
		if err != nil {
			return Team{}, fmt.Errorf("get parent team: %w", err)
		}
		if input.Visibility == TeamVisibilitySecret || parent.Visibility == TeamVisibilitySecret {
			return Team{}, fmt.Errorf("%w: secret teams cannot be nested", ErrValidation)
		}
		isDescendant, err := service.store.IsTeamDescendant(ctx, input.OrganizationID, input.TeamID, *input.ParentTeamID)
		if err != nil {
			return Team{}, fmt.Errorf("check team hierarchy: %w", err)
		}
		if isDescendant {
			return Team{}, fmt.Errorf("%w: a team cannot be moved beneath its child", ErrValidation)
		}
		if current.ParentTeamID == nil || *current.ParentTeamID != *input.ParentTeamID {
			if err := service.authorizeMaintainer(ctx, input.OrganizationID, *input.ParentTeamID, input.ActorDID); err != nil {
				return Team{}, err
			}
		}
	}
	if input.Visibility == TeamVisibilitySecret {
		hasChildren, err := service.store.TeamHasChildren(ctx, input.OrganizationID, input.TeamID)
		if err != nil {
			return Team{}, fmt.Errorf("check child teams: %w", err)
		}
		if input.ParentTeamID != nil || hasChildren {
			return Team{}, fmt.Errorf("%w: secret teams cannot be nested", ErrValidation)
		}
	}
	now := service.clock.Now().UTC()
	current.ParentTeamID = input.ParentTeamID
	current.Name = input.Name
	current.Description = input.Description
	current.Visibility = input.Visibility
	current.UpdatedAt = now
	updated, err := service.store.UpdateTeam(ctx, current)
	if err != nil {
		return Team{}, fmt.Errorf("update team: %w", err)
	}
	if err := service.audit(ctx, input.OrganizationID, input.ActorDID, "team.update", "team", input.TeamID.String(), map[string]any{"visibility": input.Visibility, "parent_team_id": input.ParentTeamID}); err != nil {
		return Team{}, err
	}
	return updated, nil
}

func (service *TeamService) Delete(ctx context.Context, organizationID ID, teamID uuid.UUID, actorDID string) error {
	if _, err := service.store.GetTeam(ctx, organizationID, teamID); err != nil {
		return fmt.Errorf("get team: %w", err)
	}
	if err := service.authorizeMaintainer(ctx, organizationID, teamID, actorDID); err != nil {
		return err
	}
	count, err := service.store.DeleteTeam(ctx, organizationID, teamID, service.clock.Now().UTC())
	if err != nil {
		return fmt.Errorf("delete team hierarchy: %w", err)
	}
	if count == 0 {
		return ErrNotFound
	}
	return service.audit(ctx, organizationID, actorDID, "team.delete", "team", teamID.String(), map[string]any{"deleted_team_count": count})
}

func (service *TeamService) List(ctx context.Context, organizationID ID, actorDID string) ([]Team, error) {
	member, err := service.store.GetMember(ctx, organizationID, actorDID)
	if err != nil {
		return nil, ErrForbidden
	}
	teams, err := service.store.ListTeams(ctx, organizationID, actorDID)
	if err != nil {
		return nil, fmt.Errorf("list organization teams: %w", err)
	}
	if member.Role == RoleOwner {
		return teams, nil
	}
	visible := make([]Team, 0, len(teams))
	for _, team := range teams {
		if team.Visibility == TeamVisibilityVisible || team.ViewerIsMember {
			visible = append(visible, team)
		}
	}
	return visible, nil
}

func (service *TeamService) PageList(ctx context.Context, organizationID ID, actorDID string, after *uuid.UUID, limit int) (Page[Team], error) {
	if err := validateCollectionLimit(limit); err != nil {
		return Page[Team]{}, err
	}
	member, err := service.store.GetMember(ctx, organizationID, actorDID)
	if err != nil {
		return Page[Team]{}, ErrForbidden
	}
	values, err := service.store.PageTeams(ctx, organizationID, actorDID, member.Role == RoleOwner, after, int32(limit+1))
	if err != nil {
		return Page[Team]{}, fmt.Errorf("page organization teams: %w", err)
	}
	return keysetPage(values, limit, func(value Team) string { return value.ID.String() }), nil
}

func (service *TeamService) Members(ctx context.Context, organizationID ID, teamID uuid.UUID, actorDID string) ([]TeamMember, error) {
	if _, err := service.store.GetMember(ctx, organizationID, actorDID); err != nil {
		return nil, ErrForbidden
	}
	if _, err := service.store.GetTeam(ctx, organizationID, teamID); err != nil {
		return nil, fmt.Errorf("get team: %w", err)
	}
	members, err := service.store.ListTeamMembers(ctx, teamID)
	if err != nil {
		return nil, fmt.Errorf("list team members: %w", err)
	}
	return members, nil
}

func (service *TeamService) PageMembers(ctx context.Context, organizationID ID, teamID uuid.UUID, actorDID, after string, limit int) (Page[TeamMember], error) {
	if err := validateCollectionLimit(limit); err != nil {
		return Page[TeamMember]{}, err
	}
	if _, err := service.store.GetMember(ctx, organizationID, actorDID); err != nil {
		return Page[TeamMember]{}, ErrForbidden
	}
	if _, err := service.store.GetTeam(ctx, organizationID, teamID); err != nil {
		return Page[TeamMember]{}, fmt.Errorf("get team: %w", err)
	}
	values, err := service.store.PageTeamMembers(ctx, teamID, after, int32(limit+1))
	if err != nil {
		return Page[TeamMember]{}, fmt.Errorf("page team members: %w", err)
	}
	return keysetPage(values, limit, func(value TeamMember) string { return value.AccountDID }), nil
}

func (service *TeamService) PutMember(ctx context.Context, organizationID ID, teamID uuid.UUID, actorDID, memberDID string, role TeamRole) (TeamMember, error) {
	if role != TeamRoleMember && role != TeamRoleMaintainer {
		return TeamMember{}, ErrValidation
	}
	if _, err := service.store.GetTeam(ctx, organizationID, teamID); err != nil {
		return TeamMember{}, fmt.Errorf("get team: %w", err)
	}
	if err := service.authorizeMaintainer(ctx, organizationID, teamID, actorDID); err != nil {
		return TeamMember{}, err
	}
	member, err := service.store.AddTeamMember(ctx, teamID, memberDID, role, service.clock.Now().UTC())
	if err != nil {
		return TeamMember{}, fmt.Errorf("add team member: %w", err)
	}
	if err := service.audit(ctx, organizationID, actorDID, "team.member.put", "member", memberDID, map[string]any{"team_id": teamID.String(), "role": role}); err != nil {
		return TeamMember{}, err
	}
	return member, nil
}

func (service *TeamService) RemoveMember(ctx context.Context, organizationID ID, teamID uuid.UUID, actorDID, memberDID string) error {
	if _, err := service.store.GetTeam(ctx, organizationID, teamID); err != nil {
		return fmt.Errorf("get team: %w", err)
	}
	if err := service.authorizeMaintainer(ctx, organizationID, teamID, actorDID); err != nil {
		return err
	}
	if err := service.store.RemoveTeamMember(ctx, teamID, memberDID); err != nil {
		return fmt.Errorf("remove team member: %w", err)
	}
	if err := service.audit(ctx, organizationID, actorDID, "team.member.remove", "member", memberDID, map[string]any{"team_id": teamID.String()}); err != nil {
		return err
	}
	return nil
}

func (service *TeamService) audit(ctx context.Context, organizationID ID, actorDID, action, targetType, targetID string, metadata any) error {
	return recordOrganizationAudit(ctx, service.store, service.clock, organizationID, actorDID, action, targetType, targetID, metadata)
}

func (service *TeamService) authorizeMaintainer(ctx context.Context, organizationID ID, teamID uuid.UUID, actorDID string) error {
	if _, err := service.store.GetOwner(ctx, organizationID, actorDID); err == nil {
		return nil
	} else if !errors.Is(err, ErrNotFound) {
		return fmt.Errorf("authorize organization owner: %w", err)
	}
	member, err := service.store.GetTeamMember(ctx, teamID, actorDID)
	if err == nil && member.Role == TeamRoleMaintainer {
		return nil
	}
	if err != nil && !errors.Is(err, ErrNotFound) {
		return fmt.Errorf("authorize team maintainer: %w", err)
	}
	return ErrForbidden
}
