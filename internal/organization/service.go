package organization

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/adenosine-dev/adenosine/internal/requestcontext"
	"github.com/google/uuid"
)

type store interface {
	Create(context.Context, Organization) (Organization, error)
	Activate(context.Context, ID, ATIdentity, time.Time) (Organization, error)
	Fail(context.Context, ID, time.Time) error
	GetBySlug(context.Context, string) (Organization, error)
	Update(context.Context, Organization) (Organization, error)
	ListForAccount(context.Context, string) ([]Organization, error)
	PageForAccount(context.Context, string, *uuid.UUID, int32) ([]Organization, error)
	ListMembers(context.Context, ID) ([]Member, error)
	PageMembers(context.Context, ID, bool, string, int32) ([]Member, error)
	GetByID(context.Context, ID) (Organization, error)
	GetOwner(context.Context, ID, string) (Member, error)
	GetMember(context.Context, ID, string) (Member, error)
	CreateInvitation(context.Context, Invitation) (Invitation, error)
	RevokeInvitation(context.Context, uuid.UUID, time.Time) error
	GetInvitation(context.Context, uuid.UUID) (Invitation, error)
	ListInvitations(context.Context, ID) ([]Invitation, error)
	PageInvitations(context.Context, ID, *uuid.UUID, int32) ([]Invitation, error)
	ListPendingInvitations(context.Context, string, time.Time) ([]Invitation, error)
	PagePendingInvitations(context.Context, string, time.Time, *uuid.UUID, int32) ([]Invitation, error)
	AcceptInvitation(context.Context, uuid.UUID, string, ATIdentity, time.Time) (Member, error)
	UpdateMemberRole(context.Context, ID, string, Role, ATIdentity, ATIdentity, time.Time) (Member, error)
	RemoveMember(context.Context, ID, string) (Member, error)
	UpdateVisibility(context.Context, ID, string, MembershipVisibility, ATIdentity, ATIdentity, time.Time) (Member, error)
	RecordAudit(context.Context, AuditEvent) error
	ListAuditEvents(context.Context, ID, *uuid.UUID, int32) ([]AuditEvent, error)
}

type clock interface{ Now() time.Time }
type idGenerator interface{ New() (ID, error) }
type publisher interface {
	PublishOrganization(context.Context, Publication) (ATIdentity, error)
	PublishOrganizationGrant(context.Context, GrantPublication) (ATIdentity, error)
	PublishOrganizationMembership(context.Context, MembershipPublication) (ATIdentity, error)
	PublishOrganizationRevocation(context.Context, RevocationPublication) (ATIdentity, error)
	DeleteOrganizationMembership(context.Context, string, ATIdentity, ATIdentity) error
}

// Service coordinates authoritative organization state with public AT records.
type Service struct {
	store     store
	clock     clock
	ids       idGenerator
	publisher publisher
}

func NewService(store store, clock clock, ids idGenerator, publisher publisher) *Service {
	return &Service{store: store, clock: clock, ids: ids, publisher: publisher}
}

// Create atomically creates the organization and private creator membership, publishes the
// portable root record, then activates the organization.
func (service *Service) Create(ctx context.Context, input CreateInput) (Organization, error) {
	if err := input.Validate(); err != nil {
		return Organization{}, err
	}
	id, err := service.ids.New()
	if err != nil {
		return Organization{}, fmt.Errorf("generate organization ID: %w", err)
	}
	now := service.clock.Now().UTC()
	organization, err := service.store.Create(ctx, Organization{
		ID: id, Slug: input.Slug, Name: input.Name, Description: input.Description,
		Website: input.Website, Location: input.Location, CreatorDID: input.CreatorDID,
		BasePermission: input.BasePermission, MembersCanCreateRepo: input.MembersCanCreateRepo,
		State: StateCreating, CreatedAt: now, UpdatedAt: now,
	})
	if err != nil {
		return Organization{}, fmt.Errorf("create organization: %w", err)
	}
	identity, err := service.publisher.PublishOrganization(ctx, Publication{
		ID: organization.ID, CreatorDID: organization.CreatorDID, Slug: organization.Slug,
		Name: organization.Name, Description: organization.Description, Website: organization.Website,
		Location: organization.Location, CreatedAt: organization.CreatedAt, UpdatedAt: organization.UpdatedAt,
	})
	if err != nil {
		return Organization{}, service.fail(ctx, organization.ID, fmt.Errorf("publish organization: %w", err))
	}
	organization, err = service.store.Activate(ctx, organization.ID, identity, service.clock.Now().UTC())
	if err != nil {
		return Organization{}, service.fail(ctx, organization.ID, fmt.Errorf("activate organization: %w", err))
	}
	if err := service.audit(ctx, organization.ID, input.CreatorDID, "organization.create", "organization", organization.ID.String(), map[string]any{"slug": organization.Slug}); err != nil {
		return Organization{}, err
	}
	return organization, nil
}

func (service *Service) fail(ctx context.Context, id ID, cause error) error {
	err := service.store.Fail(context.WithoutCancel(ctx), id, service.clock.Now().UTC())
	if err != nil {
		err = fmt.Errorf("mark organization failed: %w", err)
	}
	return errors.Join(cause, err)
}

func (service *Service) GetBySlug(ctx context.Context, slug string) (Organization, error) {
	organization, err := service.store.GetBySlug(ctx, slug)
	if err != nil {
		return Organization{}, fmt.Errorf("get organization: %w", err)
	}
	return organization, nil
}

// Update replaces mutable profile and repository-policy fields and republishes the stable AT root.
func (service *Service) Update(ctx context.Context, input UpdateInput) (Organization, error) {
	if err := input.Validate(); err != nil {
		return Organization{}, err
	}
	value, _, err := service.authorizeOwner(ctx, input.OrganizationID, input.ActorDID)
	if err != nil {
		return Organization{}, err
	}
	value.Name = input.Name
	value.Description = input.Description
	value.Website = input.Website
	value.Location = input.Location
	value.BasePermission = input.BasePermission
	value.MembersCanCreateRepo = input.MembersCanCreateRepo
	value.UpdatedAt = service.clock.Now().UTC()
	identity, err := service.publisher.PublishOrganization(ctx, Publication{
		ID: value.ID, CreatorDID: value.CreatorDID, Slug: value.Slug, Name: value.Name,
		Description: value.Description, Website: value.Website, Location: value.Location,
		CreatedAt: value.CreatedAt, UpdatedAt: value.UpdatedAt,
	})
	if err != nil {
		return Organization{}, fmt.Errorf("publish organization update: %w", err)
	}
	value.ATCID = identity.CID
	updated, err := service.store.Update(ctx, value)
	if err != nil {
		return Organization{}, fmt.Errorf("store organization update: %w", err)
	}
	if err := service.audit(ctx, value.ID, input.ActorDID, "organization.update", "organization", value.ID.String(), map[string]any{"base_permission": value.BasePermission, "members_can_create_repositories": value.MembersCanCreateRepo}); err != nil {
		return Organization{}, err
	}
	return updated, nil
}

func (service *Service) ListForAccount(ctx context.Context, did string) ([]Organization, error) {
	organizations, err := service.store.ListForAccount(ctx, did)
	if err != nil {
		return nil, fmt.Errorf("list organizations: %w", err)
	}
	return organizations, nil
}

func (service *Service) PageForAccount(ctx context.Context, did string, after *uuid.UUID, limit int) (Page[Organization], error) {
	if err := validateCollectionLimit(limit); err != nil {
		return Page[Organization]{}, err
	}
	values, err := service.store.PageForAccount(ctx, did, after, int32(limit+1))
	if err != nil {
		return Page[Organization]{}, fmt.Errorf("page organizations: %w", err)
	}
	return keysetPage(values, limit, func(value Organization) string { return value.ID.String() }), nil
}

func (service *Service) ListMembers(ctx context.Context, id ID) ([]Member, error) {
	members, err := service.store.ListMembers(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("list organization members: %w", err)
	}
	return members, nil
}

func (service *Service) PageMembers(ctx context.Context, id ID, includePrivate bool, after string, limit int) (Page[Member], error) {
	if err := validateCollectionLimit(limit); err != nil {
		return Page[Member]{}, err
	}
	values, err := service.store.PageMembers(ctx, id, includePrivate, after, int32(limit+1))
	if err != nil {
		return Page[Member]{}, fmt.Errorf("page organization members: %w", err)
	}
	return keysetPage(values, limit, func(value Member) string { return value.AccountDID }), nil
}

func (service *Service) GetMember(ctx context.Context, id ID, did string) (Member, error) {
	member, err := service.store.GetMember(ctx, id, did)
	if err != nil {
		return Member{}, fmt.Errorf("get organization member: %w", err)
	}
	return member, nil
}

// Invite creates a seven-day private invitation. Public AT records are deliberately deferred
// until the invitee explicitly makes their accepted membership public.
func (service *Service) Invite(ctx context.Context, input InviteInput) (Invitation, error) {
	if err := input.Validate(); err != nil {
		return Invitation{}, err
	}
	organization, err := service.store.GetByID(ctx, input.OrganizationID)
	if err != nil {
		return Invitation{}, fmt.Errorf("load organization for invitation: %w", err)
	}
	_, err = service.store.GetOwner(ctx, input.OrganizationID, input.ActorDID)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return Invitation{}, ErrForbidden
		}
		return Invitation{}, fmt.Errorf("authorize organization invitation: %w", err)
	}
	id, err := service.ids.New()
	if err != nil {
		return Invitation{}, fmt.Errorf("generate invitation ID: %w", err)
	}
	now := service.clock.Now().UTC()
	invitation, err := service.store.CreateInvitation(ctx, Invitation{
		ID: uuid.UUID(id), OrganizationID: organization.ID, InviteeDID: input.InviteeDID,
		Role: input.Role, InvitedByDID: input.ActorDID, CreatedAt: now, ExpiresAt: now.Add(InvitationLifetime),
	})
	if err != nil {
		return Invitation{}, fmt.Errorf("create organization invitation: %w", err)
	}
	if err := service.audit(ctx, organization.ID, input.ActorDID, "member.invite", "invitation", invitation.ID.String(), map[string]any{"subject_did": input.InviteeDID, "role": input.Role}); err != nil {
		return Invitation{}, err
	}
	return invitation, nil
}

// Accept activates a private membership without placing the relationship in a public AT repository.
func (service *Service) Accept(ctx context.Context, invitationID uuid.UUID, actorDID string) (Member, error) {
	invitation, err := service.store.GetInvitation(ctx, invitationID)
	if err != nil {
		return Member{}, fmt.Errorf("get organization invitation: %w", err)
	}
	if invitation.InviteeDID != actorDID || !invitation.Active(service.clock.Now().UTC()) {
		return Member{}, ErrInvitation
	}
	now := service.clock.Now().UTC()
	member, err := service.store.AcceptInvitation(ctx, invitationID, actorDID, ATIdentity{}, now)
	if err != nil {
		return Member{}, fmt.Errorf("accept organization invitation: %w", err)
	}
	if err := service.audit(ctx, invitation.OrganizationID, actorDID, "member.accept", "member", actorDID, map[string]any{"invitation_id": invitationID.String(), "role": invitation.Role}); err != nil {
		return Member{}, err
	}
	return member, nil
}

func (service *Service) ListInvitations(ctx context.Context, organizationID ID, actorDID string) ([]Invitation, error) {
	if _, err := service.store.GetOwner(ctx, organizationID, actorDID); err != nil {
		if errors.Is(err, ErrNotFound) {
			return nil, ErrForbidden
		}
		return nil, fmt.Errorf("authorize invitation list: %w", err)
	}
	return service.store.ListInvitations(ctx, organizationID)
}

func (service *Service) PageInvitations(ctx context.Context, organizationID ID, actorDID string, after *uuid.UUID, limit int) (Page[Invitation], error) {
	if err := validateCollectionLimit(limit); err != nil {
		return Page[Invitation]{}, err
	}
	if _, err := service.store.GetOwner(ctx, organizationID, actorDID); err != nil {
		if errors.Is(err, ErrNotFound) {
			return Page[Invitation]{}, ErrForbidden
		}
		return Page[Invitation]{}, fmt.Errorf("authorize invitation page: %w", err)
	}
	values, err := service.store.PageInvitations(ctx, organizationID, after, int32(limit+1))
	if err != nil {
		return Page[Invitation]{}, fmt.Errorf("page organization invitations: %w", err)
	}
	return keysetPage(values, limit, func(value Invitation) string { return value.ID.String() }), nil
}

func (service *Service) ListPendingInvitations(ctx context.Context, actorDID string) ([]Invitation, error) {
	return service.store.ListPendingInvitations(ctx, actorDID, service.clock.Now().UTC())
}

func (service *Service) PagePendingInvitations(ctx context.Context, actorDID string, after *uuid.UUID, limit int) (Page[Invitation], error) {
	if err := validateCollectionLimit(limit); err != nil {
		return Page[Invitation]{}, err
	}
	values, err := service.store.PagePendingInvitations(ctx, actorDID, service.clock.Now().UTC(), after, int32(limit+1))
	if err != nil {
		return Page[Invitation]{}, fmt.Errorf("page pending organization invitations: %w", err)
	}
	return keysetPage(values, limit, func(value Invitation) string { return value.ID.String() }), nil
}

func validateCollectionLimit(limit int) error {
	if limit < 1 || limit > 100 {
		return fmt.Errorf("%w: limit must be between 1 and 100", ErrValidation)
	}
	return nil
}

func keysetPage[T any](values []T, limit int, key func(T) string) Page[T] {
	page := Page[T]{Items: values}
	if len(values) > limit {
		page.Items = values[:limit]
		page.NextCursor = key(page.Items[len(page.Items)-1])
	}
	if page.Items == nil {
		page.Items = []T{}
	}
	return page
}

// RevokeInvitation invalidates a pending invitation and revokes a legacy public grant when present.
func (service *Service) RevokeInvitation(ctx context.Context, organizationID ID, invitationID uuid.UUID, actorDID string) error {
	value, owner, err := service.authorizeOwner(ctx, organizationID, actorDID)
	if err != nil {
		return err
	}
	invitation, err := service.store.GetInvitation(ctx, invitationID)
	if err != nil || invitation.OrganizationID != organizationID || !invitation.Active(service.clock.Now().UTC()) {
		return ErrInvitation
	}
	now := service.clock.Now().UTC()
	if invitation.GrantURI != "" {
		id, err := service.ids.New()
		if err != nil {
			return fmt.Errorf("generate invitation revocation ID: %w", err)
		}
		issuer, authority := publicationAuthority(value, owner)
		if _, err := service.publisher.PublishOrganizationRevocation(ctx, RevocationPublication{
			ID: uuid.UUID(id), ActorDID: issuer,
			Organization: ATIdentity{URI: value.ATURI, CID: value.ATCID},
			Grant:        ATIdentity{URI: invitation.GrantURI, CID: invitation.GrantCID}, SubjectDID: invitation.InviteeDID,
			Authority: authority, CreatedAt: now,
		}); err != nil {
			return fmt.Errorf("publish invitation revocation: %w", err)
		}
	}
	if err := service.store.RevokeInvitation(ctx, invitationID, now); err != nil {
		return fmt.Errorf("revoke organization invitation: %w", err)
	}
	if err := service.audit(ctx, organizationID, actorDID, "invitation.revoke", "invitation", invitationID.String(), map[string]any{"subject_did": invitation.InviteeDID}); err != nil {
		return err
	}
	return nil
}

// SetVisibility lets only the member change whether their membership is public.
func (service *Service) SetVisibility(ctx context.Context, organizationID ID, actorDID string, visibility MembershipVisibility) (Member, error) {
	if err := visibility.Validate(); err != nil {
		return Member{}, fmt.Errorf("%w: %v", ErrValidation, err)
	}
	organization, err := service.store.GetByID(ctx, organizationID)
	if err != nil {
		return Member{}, fmt.Errorf("get organization for visibility: %w", err)
	}
	member, err := service.store.GetMember(ctx, organizationID, actorDID)
	if err != nil || member.AccountDID == "" {
		return Member{}, ErrForbidden
	}
	now := service.clock.Now().UTC()
	if member.Visibility == visibility {
		return member, nil
	}
	organizationIdentity := ATIdentity{URI: organization.ATURI, CID: organization.ATCID}
	grant := ATIdentity{URI: member.GrantURI, CID: member.GrantCID}
	membership := ATIdentity{URI: member.MembershipURI, CID: member.MembershipCID}
	if visibility == VisibilityPublic {
		owner, err := service.store.GetOwner(ctx, organizationID, organization.CreatorDID)
		if err != nil {
			return Member{}, fmt.Errorf("load organization controller: %w", err)
		}
		grantID, err := service.ids.New()
		if err != nil {
			return Member{}, fmt.Errorf("generate public membership grant ID: %w", err)
		}
		issuer, authority := publicationAuthority(organization, owner)
		grant, err = service.publisher.PublishOrganizationGrant(ctx, GrantPublication{ID: uuid.UUID(grantID), ActorDID: issuer, Organization: organizationIdentity, SubjectDID: actorDID, Role: member.Role, Authority: authority, CreatedAt: now})
		if err != nil {
			return Member{}, fmt.Errorf("publish public membership grant: %w", err)
		}
		membership, err = service.publisher.PublishOrganizationMembership(ctx, MembershipPublication{ActorDID: actorDID, Organization: organizationIdentity, Grant: grant, Visibility: VisibilityPublic, CreatedAt: member.JoinedAt, UpdatedAt: now})
		if err != nil {
			return Member{}, fmt.Errorf("publish organization membership visibility: %w", err)
		}
	} else {
		if grant.URI != "" {
			owner, err := service.store.GetOwner(ctx, organizationID, organization.CreatorDID)
			if err != nil {
				return Member{}, fmt.Errorf("load organization controller: %w", err)
			}
			revocationID, err := service.ids.New()
			if err != nil {
				return Member{}, fmt.Errorf("generate public membership revocation ID: %w", err)
			}
			issuer, authority := publicationAuthority(organization, owner)
			if _, err := service.publisher.PublishOrganizationRevocation(ctx, RevocationPublication{ID: uuid.UUID(revocationID), ActorDID: issuer, Organization: organizationIdentity, Grant: grant, SubjectDID: actorDID, Authority: authority, CreatedAt: now}); err != nil {
				return Member{}, fmt.Errorf("revoke public membership grant: %w", err)
			}
		}
		if membership.URI != "" {
			if err := service.publisher.DeleteOrganizationMembership(ctx, actorDID, organizationIdentity, membership); err != nil {
				return Member{}, fmt.Errorf("delete public organization membership: %w", err)
			}
		}
		grant, membership = ATIdentity{}, ATIdentity{}
	}
	updated, err := service.store.UpdateVisibility(ctx, organizationID, actorDID, visibility, grant, membership, now)
	if err != nil {
		return Member{}, err
	}
	if err := service.audit(ctx, organizationID, actorDID, "member.visibility", "member", actorDID, map[string]any{"visibility": visibility}); err != nil {
		return Member{}, err
	}
	return updated, nil
}

// ChangeRole lets an owner promote or demote a member while preserving the last-owner invariant.
func (service *Service) ChangeRole(ctx context.Context, organizationID ID, actorDID, memberDID string, role Role) (Member, error) {
	if err := role.Validate(); err != nil {
		return Member{}, fmt.Errorf("%w: %v", ErrValidation, err)
	}
	organization, owner, err := service.authorizeOwner(ctx, organizationID, actorDID)
	if err != nil {
		return Member{}, err
	}
	target, err := service.store.GetMember(ctx, organizationID, memberDID)
	if err != nil {
		return Member{}, fmt.Errorf("get organization member: %w", err)
	}
	if target.Role == role {
		return target, nil
	}
	// The portable organization root remains controlled by its AT repository author.
	// Until organizations have their own rotatable DID, pretending that author can be
	// demoted would leave the public authority graph saying something different from
	// the local database.
	if memberDID == organization.CreatorDID {
		return Member{}, ErrCreatorOwner
	}
	if target.Visibility != VisibilityPublic {
		member, err := service.store.UpdateMemberRole(ctx, organizationID, memberDID, role, ATIdentity{}, ATIdentity{}, service.clock.Now().UTC())
		if err != nil {
			return Member{}, fmt.Errorf("update private organization role: %w", err)
		}
		if err := service.audit(ctx, organizationID, actorDID, "member.role", "member", memberDID, map[string]any{"from": target.Role, "to": role}); err != nil {
			return Member{}, err
		}
		return member, nil
	}
	id, err := service.ids.New()
	if err != nil {
		return Member{}, fmt.Errorf("generate role grant ID: %w", err)
	}
	var revocationID ID
	if target.GrantURI != "" {
		revocationID, err = service.ids.New()
		if err != nil {
			return Member{}, fmt.Errorf("generate prior role revocation ID: %w", err)
		}
	}
	now := service.clock.Now().UTC()
	issuer, authority := publicationAuthority(organization, owner)
	grant, err := service.publisher.PublishOrganizationGrant(ctx, GrantPublication{
		ID: uuid.UUID(id), ActorDID: issuer,
		Organization: ATIdentity{URI: organization.ATURI, CID: organization.ATCID},
		SubjectDID:   memberDID, Role: role, Authority: authority, CreatedAt: now,
	})
	if err != nil {
		return Member{}, fmt.Errorf("publish organization role: %w", err)
	}
	membership, err := service.publisher.PublishOrganizationMembership(ctx, MembershipPublication{
		ActorDID: memberDID, Organization: ATIdentity{URI: organization.ATURI, CID: organization.ATCID},
		Grant: grant, Visibility: VisibilityPublic, CreatedAt: target.JoinedAt, UpdatedAt: now,
	})
	if err != nil {
		return Member{}, fmt.Errorf("publish organization role membership: %w", err)
	}
	if target.GrantURI != "" {
		if _, err := service.publisher.PublishOrganizationRevocation(ctx, RevocationPublication{
			ID: uuid.UUID(revocationID), ActorDID: issuer,
			Organization: ATIdentity{URI: organization.ATURI, CID: organization.ATCID},
			Grant:        ATIdentity{URI: target.GrantURI, CID: target.GrantCID}, SubjectDID: memberDID,
			Authority: authority, CreatedAt: now,
		}); err != nil {
			return Member{}, fmt.Errorf("revoke prior organization role: %w", err)
		}
	}
	member, err := service.store.UpdateMemberRole(ctx, organizationID, memberDID, role, grant, membership, now)
	if err != nil {
		return Member{}, fmt.Errorf("update organization role: %w", err)
	}
	if err := service.audit(ctx, organizationID, actorDID, "member.role", "member", memberDID, map[string]any{"from": target.Role, "to": role}); err != nil {
		return Member{}, err
	}
	return member, nil
}

// Remove removes a member. Owners may remove anyone; a member may remove themselves.
func (service *Service) Remove(ctx context.Context, organizationID ID, actorDID, memberDID string) error {
	organization, err := service.store.GetByID(ctx, organizationID)
	if err != nil {
		return fmt.Errorf("get organization for member removal: %w", err)
	}
	actor, actorErr := service.store.GetMember(ctx, organizationID, actorDID)
	if actorErr != nil || (actor.Role != RoleOwner && actorDID != memberDID) {
		return ErrForbidden
	}
	target, err := service.store.GetMember(ctx, organizationID, memberDID)
	if err != nil {
		return fmt.Errorf("get member for removal: %w", err)
	}
	if memberDID == organization.CreatorDID {
		return ErrCreatorOwner
	}
	now := service.clock.Now().UTC()
	if target.GrantURI != "" {
		revocationAuthority := actor
		if actor.Role != RoleOwner {
			revocationAuthority, err = service.store.GetOwner(ctx, organizationID, organization.CreatorDID)
			if err != nil {
				return fmt.Errorf("load organization controller for member removal: %w", err)
			}
		}
		id, err := service.ids.New()
		if err != nil {
			return fmt.Errorf("generate membership revocation ID: %w", err)
		}
		issuer, authority := publicationAuthority(organization, revocationAuthority)
		if _, err := service.publisher.PublishOrganizationRevocation(ctx, RevocationPublication{
			ID: uuid.UUID(id), ActorDID: issuer,
			Organization: ATIdentity{URI: organization.ATURI, CID: organization.ATCID},
			Grant:        ATIdentity{URI: target.GrantURI, CID: target.GrantCID}, SubjectDID: memberDID,
			Authority: authority, CreatedAt: now,
		}); err != nil {
			return fmt.Errorf("publish organization member removal: %w", err)
		}
	}
	if target.MembershipURI != "" {
		if err := service.publisher.DeleteOrganizationMembership(
			ctx,
			memberDID,
			ATIdentity{URI: organization.ATURI, CID: organization.ATCID},
			ATIdentity{URI: target.MembershipURI, CID: target.MembershipCID},
		); err != nil {
			return fmt.Errorf("delete public organization membership: %w", err)
		}
	}
	if _, err := service.store.RemoveMember(ctx, organizationID, memberDID); err != nil {
		return fmt.Errorf("remove organization member: %w", err)
	}
	if err := service.audit(ctx, organizationID, actorDID, "member.remove", "member", memberDID, map[string]any{"role": target.Role}); err != nil {
		return err
	}
	return nil
}

// AuditEvents returns an owner-only, newest-first keyset page.
func (service *Service) AuditEvents(ctx context.Context, organizationID ID, actorDID string, limit int, after *uuid.UUID) (AuditPage, error) {
	if limit < 1 || limit > 100 {
		return AuditPage{}, fmt.Errorf("%w: audit limit must be between 1 and 100", ErrValidation)
	}
	if _, err := service.store.GetOwner(ctx, organizationID, actorDID); err != nil {
		if errors.Is(err, ErrNotFound) {
			return AuditPage{}, ErrForbidden
		}
		return AuditPage{}, fmt.Errorf("authorize audit log: %w", err)
	}
	values, err := service.store.ListAuditEvents(ctx, organizationID, after, int32(limit+1))
	if err != nil {
		return AuditPage{}, fmt.Errorf("list organization audit events: %w", err)
	}
	page := AuditPage{Items: values}
	if len(values) > limit {
		page.Items = values[:limit]
		next := page.Items[len(page.Items)-1].ID
		page.NextCursor = &next
	}
	if page.Items == nil {
		page.Items = []AuditEvent{}
	}
	return page, nil
}

func (service *Service) audit(ctx context.Context, organizationID ID, actorDID, action, targetType, targetID string, metadata any) error {
	return recordOrganizationAudit(ctx, service.store, service.clock, organizationID, actorDID, action, targetType, targetID, metadata)
}

type auditStore interface {
	RecordAudit(context.Context, AuditEvent) error
}

func recordOrganizationAudit(ctx context.Context, store auditStore, clock clock, organizationID ID, actorDID, action, targetType, targetID string, metadata any) error {
	encoded, err := json.Marshal(metadata)
	if err != nil {
		return fmt.Errorf("encode organization audit metadata: %w", err)
	}
	event := AuditEvent{ID: uuid.New(), OrganizationID: organizationID, ActorDID: actorDID, Action: action, TargetType: targetType, TargetID: targetID, RequestID: requestcontext.RequestID(ctx), Metadata: encoded, CreatedAt: clock.Now().UTC()}
	if err := store.RecordAudit(ctx, event); err != nil {
		return fmt.Errorf("record organization audit event: %w", err)
	}
	return nil
}

func (service *Service) authorizeOwner(ctx context.Context, organizationID ID, actorDID string) (Organization, Member, error) {
	organization, err := service.store.GetByID(ctx, organizationID)
	if err != nil {
		return Organization{}, Member{}, fmt.Errorf("get organization: %w", err)
	}
	owner, err := service.store.GetOwner(ctx, organizationID, actorDID)
	if errors.Is(err, ErrNotFound) {
		return Organization{}, Member{}, ErrForbidden
	}
	if err != nil {
		return Organization{}, Member{}, fmt.Errorf("authorize organization owner: %w", err)
	}
	return organization, owner, nil
}

func publicationAuthority(organization Organization, owner Member) (string, ATIdentity) {
	if owner.GrantURI != "" {
		return owner.AccountDID, ATIdentity{URI: owner.GrantURI, CID: owner.GrantCID}
	}
	return organization.CreatorDID, ATIdentity{URI: organization.ATURI, CID: organization.ATCID}
}
