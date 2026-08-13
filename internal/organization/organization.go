// Package organization contains organization membership and access-control rules.
package organization

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/google/uuid"
)

const (
	// InvitationLifetime follows GitHub's default organization invitation expiry.
	InvitationLifetime  = 7 * 24 * time.Hour
	maxNameBytes        = 255
	maxDescriptionBytes = 2000
)

var (
	ErrNotFound      = errors.New("organization not found")
	ErrAlreadyExists = errors.New("organization already exists")
	ErrForbidden     = errors.New("organization action forbidden")
	ErrLastOwner     = errors.New("organization must retain at least one owner")
	ErrCreatorOwner  = errors.New("organization creator must remain an owner")
	ErrInvitation    = errors.New("organization invitation is not active")
	ErrValidation    = errors.New("organization validation failed")

	slugPattern        = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`)
	reservedOwnerSlugs = map[string]struct{}{
		"api": {}, "docs": {}, "explore": {}, "health": {}, "login": {}, "oauth": {},
		"organizations": {}, "profiles": {}, "settings": {},
	}
)

// ID is an immutable local organization identifier.
type ID uuid.UUID

func (id ID) String() string { return uuid.UUID(id).String() }

// Role is the GitHub-compatible organization-level role.
type Role string

const (
	RoleOwner  Role = "owner"
	RoleMember Role = "member"
)

func (role Role) Validate() error {
	if role != RoleOwner && role != RoleMember {
		return fmt.Errorf("role must be owner or member")
	}
	return nil
}

// MembershipVisibility controls whether membership is shown publicly.
type MembershipVisibility string

const (
	VisibilityPrivate MembershipVisibility = "private"
	VisibilityPublic  MembershipVisibility = "public"
)

func (visibility MembershipVisibility) Validate() error {
	if visibility != VisibilityPrivate && visibility != VisibilityPublic {
		return fmt.Errorf("membership visibility must be private or public")
	}
	return nil
}

// BasePermission is inherited by every organization member for organization repositories.
type BasePermission string

const (
	BasePermissionNone  BasePermission = "none"
	BasePermissionRead  BasePermission = "read"
	BasePermissionWrite BasePermission = "write"
)

func (permission BasePermission) Validate() error {
	if permission != BasePermissionNone && permission != BasePermissionRead && permission != BasePermissionWrite {
		return fmt.Errorf("base permission must be none, read, or write")
	}
	return nil
}

// RepositoryRole follows GitHub's built-in repository role ordering.
type RepositoryRole string

const (
	RepositoryRoleNone     RepositoryRole = "none"
	RepositoryRoleRead     RepositoryRole = "read"
	RepositoryRoleTriage   RepositoryRole = "triage"
	RepositoryRoleWrite    RepositoryRole = "write"
	RepositoryRoleMaintain RepositoryRole = "maintain"
	RepositoryRoleAdmin    RepositoryRole = "admin"
)

var repositoryRoleRank = map[RepositoryRole]int{
	RepositoryRoleNone: 0, RepositoryRoleRead: 1, RepositoryRoleTriage: 2,
	RepositoryRoleWrite: 3, RepositoryRoleMaintain: 4, RepositoryRoleAdmin: 5,
}

func (role RepositoryRole) Validate() error {
	if _, ok := repositoryRoleRank[role]; !ok || role == RepositoryRoleNone {
		return fmt.Errorf("repository role must be read, triage, write, maintain, or admin")
	}
	return nil
}

// TeamRole controls whether a team member can administer the team.
type TeamRole string

const (
	TeamRoleMember     TeamRole = "member"
	TeamRoleMaintainer TeamRole = "maintainer"
)

// TeamVisibility follows GitHub's visible and secret team model.
type TeamVisibility string

const (
	TeamVisibilityVisible TeamVisibility = "visible"
	TeamVisibilitySecret  TeamVisibility = "secret"
)

// Organization is the authoritative local organization profile and policy.
type Organization struct {
	ID                   ID
	Slug                 string
	Name                 string
	Description          string
	Website              string
	Location             string
	CreatorDID           string
	BasePermission       BasePermission
	MembersCanCreateRepo bool
	State                State
	ATURI                string
	ATCID                string
	CreatedAt            time.Time
	UpdatedAt            time.Time
}

// State records creation and deletion progress across PostgreSQL and the owner's AT repository.
type State string

const (
	StateCreating State = "creating"
	StateActive   State = "active"
	StateFailed   State = "failed"
	StateDeleting State = "deleting"
	StateDeleted  State = "deleted"
)

// ATIdentity is the canonical identity returned by an organization publication.
type ATIdentity struct {
	URI string
	CID string
}

// Page is a bounded keyset page. NextCursor is the stable key of the final item
// and is wrapped as an opaque, collection-scoped cursor by the HTTP boundary.
type Page[T any] struct {
	Items      []T
	NextCursor string
}

// Publication is the portable organization root record written by its creator.
type Publication struct {
	ID          ID
	CreatorDID  string
	Slug        string
	Name        string
	Description string
	Website     string
	Location    string
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// GrantPublication is an owner-authored invitation or role grant.
type GrantPublication struct {
	ID           uuid.UUID
	ActorDID     string
	Organization ATIdentity
	SubjectDID   string
	Role         Role
	Authority    ATIdentity
	CreatedAt    time.Time
	ExpiresAt    time.Time
}

// MembershipPublication is the invitee-authored acceptance and visibility choice.
type MembershipPublication struct {
	ActorDID     string
	Organization ATIdentity
	Grant        ATIdentity
	Visibility   MembershipVisibility
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

// RevocationPublication is an owner-authored removal of a specific grant.
type RevocationPublication struct {
	ID           uuid.UUID
	ActorDID     string
	Organization ATIdentity
	Grant        ATIdentity
	SubjectDID   string
	Authority    ATIdentity
	CreatedAt    time.Time
}

// InviteInput requests a pending organization membership.
type InviteInput struct {
	OrganizationID ID
	ActorDID       string
	InviteeDID     string
	Role           Role
}

func (input InviteInput) Validate() error {
	if input.OrganizationID == (ID{}) || strings.TrimSpace(input.ActorDID) == "" || strings.TrimSpace(input.InviteeDID) == "" {
		return fmt.Errorf("%w: organization, actor, and invitee are required", ErrValidation)
	}
	if input.ActorDID == input.InviteeDID {
		return fmt.Errorf("%w: an owner cannot invite themselves", ErrValidation)
	}
	if err := validateDID(input.ActorDID); err != nil {
		return fmt.Errorf("%w: actor DID: %v", ErrValidation, err)
	}
	if err := validateDID(input.InviteeDID); err != nil {
		return fmt.Errorf("%w: invitee DID: %v", ErrValidation, err)
	}
	if err := input.Role.Validate(); err != nil {
		return fmt.Errorf("%w: %v", ErrValidation, err)
	}
	return nil
}

func validateDID(value string) error {
	did, err := syntax.ParseDID(value)
	if err != nil || did.String() != value {
		return fmt.Errorf("must be a canonical DID")
	}
	return nil
}

// Member joins a user identity to one organization. Membership is private by default.
type Member struct {
	OrganizationID ID
	AccountDID     string
	Handle         string
	Role           Role
	Visibility     MembershipVisibility
	InvitedByDID   string
	GrantURI       string
	GrantCID       string
	MembershipURI  string
	MembershipCID  string
	JoinedAt       time.Time
	UpdatedAt      time.Time
}

// Invitation is a pending, owner-issued membership grant.
type Invitation struct {
	ID               uuid.UUID
	OrganizationID   ID
	OrganizationSlug string
	OrganizationName string
	InviteeDID       string
	Role             Role
	InvitedByDID     string
	GrantURI         string
	GrantCID         string
	CreatedAt        time.Time
	ExpiresAt        time.Time
	RevokedAt        *time.Time
	AcceptedAt       *time.Time
}

func (invitation Invitation) Active(now time.Time) bool {
	return invitation.RevokedAt == nil && invitation.AcceptedAt == nil && now.Before(invitation.ExpiresAt)
}

// Team groups organization members for shared repository access.
type Team struct {
	ID             uuid.UUID
	OrganizationID ID
	ParentTeamID   *uuid.UUID
	Slug           string
	Name           string
	Description    string
	Visibility     TeamVisibility
	ViewerIsMember bool
	ViewerRole     TeamRole
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

type TeamMember struct {
	TeamID     uuid.UUID
	AccountDID string
	Handle     string
	Role       TeamRole
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

type TeamRepository struct {
	TeamID         uuid.UUID
	RepositoryID   uuid.UUID
	RepositorySlug string
	Role           RepositoryRole
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

// RepositoryCollaborator is a direct repository assignment. A collaborator need not be an
// organization member, matching GitHub's outside-collaborator model.
type RepositoryCollaborator struct {
	RepositoryID   uuid.UUID
	RepositorySlug string
	AccountDID     string
	Handle         string
	Role           RepositoryRole
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

type CollaboratorPage struct {
	Items      []RepositoryCollaborator
	NextCursor *string
}

// AuditEvent is an immutable local security record for an organization mutation.
type AuditEvent struct {
	ID             uuid.UUID
	OrganizationID ID
	ActorDID       string
	Action         string
	TargetType     string
	TargetID       string
	RequestID      string
	Metadata       json.RawMessage
	CreatedAt      time.Time
}

// AuditPage is a stable, newest-first keyset page of organization security events.
type AuditPage struct {
	Items      []AuditEvent
	NextCursor *uuid.UUID
}

type CreateTeamInput struct {
	OrganizationID ID
	ActorDID       string
	ParentTeamID   *uuid.UUID
	Slug           string
	Name           string
	Description    string
	Visibility     TeamVisibility
}

// UpdateTeamInput replaces the mutable team settings. A nil ParentTeamID makes
// the team top-level; nested secret teams are rejected.
type UpdateTeamInput struct {
	OrganizationID ID
	TeamID         uuid.UUID
	ActorDID       string
	ParentTeamID   *uuid.UUID
	Name           string
	Description    string
	Visibility     TeamVisibility
}

func (input UpdateTeamInput) Validate() error {
	if input.OrganizationID == (ID{}) || input.TeamID == uuid.Nil || input.ActorDID == "" {
		return fmt.Errorf("%w: organization, team, and actor are required", ErrValidation)
	}
	if strings.TrimSpace(input.Name) == "" || len(input.Name) > maxNameBytes || len(input.Description) > maxDescriptionBytes {
		return fmt.Errorf("%w: team name or description is invalid", ErrValidation)
	}
	if input.Visibility != TeamVisibilityVisible && input.Visibility != TeamVisibilitySecret {
		return fmt.Errorf("%w: team visibility must be visible or secret", ErrValidation)
	}
	if input.ParentTeamID != nil && *input.ParentTeamID == input.TeamID {
		return fmt.Errorf("%w: a team cannot be its own parent", ErrValidation)
	}
	return nil
}

func (input CreateTeamInput) Validate() error {
	if input.OrganizationID == (ID{}) || input.ActorDID == "" {
		return fmt.Errorf("%w: organization and actor are required", ErrValidation)
	}
	if len(input.Slug) > 100 || !slugPattern.MatchString(input.Slug) {
		return fmt.Errorf("%w: team slug is invalid", ErrValidation)
	}
	if strings.TrimSpace(input.Name) == "" || len(input.Name) > maxNameBytes || len(input.Description) > maxDescriptionBytes {
		return fmt.Errorf("%w: team name or description is invalid", ErrValidation)
	}
	if input.Visibility != TeamVisibilityVisible && input.Visibility != TeamVisibilitySecret {
		return fmt.Errorf("%w: team visibility must be visible or secret", ErrValidation)
	}
	return nil
}

// CreateInput is validated organization creation data.
type CreateInput struct {
	CreatorDID           string
	Slug                 string
	Name                 string
	Description          string
	Website              string
	Location             string
	BasePermission       BasePermission
	MembersCanCreateRepo bool
}

// UpdateInput replaces mutable organization profile and repository policy fields.
type UpdateInput struct {
	OrganizationID       ID
	ActorDID             string
	Name                 string
	Description          string
	Website              string
	Location             string
	BasePermission       BasePermission
	MembersCanCreateRepo bool
}

func (input UpdateInput) Validate() error {
	if input.OrganizationID == (ID{}) || strings.TrimSpace(input.ActorDID) == "" {
		return fmt.Errorf("%w: organization and actor are required", ErrValidation)
	}
	if strings.TrimSpace(input.Name) == "" || len(input.Name) > maxNameBytes || len(input.Description) > maxDescriptionBytes {
		return fmt.Errorf("%w: organization name or description is invalid", ErrValidation)
	}
	if err := validateOptionalWebsite(input.Website); err != nil {
		return fmt.Errorf("%w: %v", ErrValidation, err)
	}
	if err := input.BasePermission.Validate(); err != nil {
		return fmt.Errorf("%w: %v", ErrValidation, err)
	}
	return nil
}

func (input CreateInput) Validate() error {
	if strings.TrimSpace(input.CreatorDID) == "" {
		return fmt.Errorf("%w: creator DID must not be empty", ErrValidation)
	}
	if len(input.Slug) > 100 || !slugPattern.MatchString(input.Slug) {
		return fmt.Errorf("%w: slug must match %s and contain at most 100 characters", ErrValidation, slugPattern)
	}
	if _, reserved := reservedOwnerSlugs[input.Slug]; reserved {
		return fmt.Errorf("%w: slug is reserved by an application route", ErrValidation)
	}
	if strings.TrimSpace(input.Name) == "" || len(input.Name) > maxNameBytes {
		return fmt.Errorf("%w: name must contain between 1 and %d bytes", ErrValidation, maxNameBytes)
	}
	if len(input.Description) > maxDescriptionBytes {
		return fmt.Errorf("%w: description must contain at most %d bytes", ErrValidation, maxDescriptionBytes)
	}
	if len(input.Location) > maxNameBytes {
		return fmt.Errorf("%w: location must contain at most %d bytes", ErrValidation, maxNameBytes)
	}
	if err := validateOptionalWebsite(input.Website); err != nil {
		return fmt.Errorf("%w: %v", ErrValidation, err)
	}
	if err := input.BasePermission.Validate(); err != nil {
		return fmt.Errorf("%w: %v", ErrValidation, err)
	}
	return nil
}

func validateOptionalWebsite(value string) error {
	if value == "" {
		return nil
	}
	website, err := url.ParseRequestURI(value)
	if err != nil || (website.Scheme != "http" && website.Scheme != "https") || website.Host == "" {
		return fmt.Errorf("website must be an absolute HTTP or HTTPS URL")
	}
	return nil
}

// EffectiveRepositoryRole returns the strongest access inherited from organization policy,
// direct assignment, or team assignment. Organization owners always receive admin.
func EffectiveRepositoryRole(memberRole Role, base BasePermission, direct RepositoryRole, teams []RepositoryRole) RepositoryRole {
	if memberRole == RoleOwner {
		return RepositoryRoleAdmin
	}
	result := RepositoryRoleNone
	switch base {
	case BasePermissionRead:
		result = RepositoryRoleRead
	case BasePermissionWrite:
		result = RepositoryRoleWrite
	}
	if repositoryRoleRank[direct] > repositoryRoleRank[result] {
		result = direct
	}
	for _, role := range teams {
		if repositoryRoleRank[role] > repositoryRoleRank[result] {
			result = role
		}
	}
	return result
}
