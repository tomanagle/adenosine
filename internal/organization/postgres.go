package organization

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	dbgen "github.com/adenosine-dev/adenosine/internal/database/generated"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
)

type transactionBeginner interface {
	Begin(context.Context) (pgx.Tx, error)
}

// PostgresStore persists authoritative organization and membership state.
type PostgresStore struct {
	db      transactionBeginner
	queries *dbgen.Queries
}

func NewPostgresStore(db transactionBeginner, queries *dbgen.Queries) *PostgresStore {
	return &PostgresStore{db: db, queries: queries}
}

func (store *PostgresStore) Create(ctx context.Context, value Organization) (Organization, error) {
	tx, err := store.db.Begin(ctx)
	if err != nil {
		return Organization{}, fmt.Errorf("begin organization creation: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	queries := dbgen.New(tx)
	row, err := queries.CreateOrganization(ctx, dbgen.CreateOrganizationParams{
		ID: pgUUID(value.ID), Slug: value.Slug, Name: value.Name, Description: pgText(value.Description),
		Website: pgText(value.Website), Location: pgText(value.Location), CreatorDid: value.CreatorDID,
		BasePermission: string(value.BasePermission), MembersCanCreateRepositories: value.MembersCanCreateRepo,
		CreatedAt: pgTime(value.CreatedAt),
	})
	if err != nil {
		return Organization{}, mapStoreError(err)
	}
	if err := queries.CreateOrganizationOwnerRoute(ctx, dbgen.CreateOrganizationOwnerRouteParams{
		Alias: value.Slug, OrganizationID: pgUUID(value.ID), CreatedAt: pgTime(value.CreatedAt),
	}); err != nil {
		return Organization{}, mapStoreError(err)
	}
	if _, err := queries.CreateOrganizationOwner(ctx, dbgen.CreateOrganizationOwnerParams{
		OrganizationID: pgUUID(value.ID), AccountDid: value.CreatorDID, JoinedAt: pgTime(value.CreatedAt),
	}); err != nil {
		return Organization{}, mapStoreError(err)
	}
	if err := tx.Commit(ctx); err != nil {
		return Organization{}, fmt.Errorf("commit organization creation: %w", err)
	}
	return organizationFromRow(row), nil
}

func (store *PostgresStore) Activate(ctx context.Context, id ID, identity ATIdentity, updatedAt time.Time) (Organization, error) {
	row, err := store.queries.ActivateOrganization(ctx, dbgen.ActivateOrganizationParams{
		ID: pgUUID(id), AtUri: pgText(identity.URI), AtCid: pgText(identity.CID), UpdatedAt: pgTime(updatedAt),
	})
	if err != nil {
		return Organization{}, mapStoreError(err)
	}
	return organizationFromRow(row), nil
}

func (store *PostgresStore) Fail(ctx context.Context, id ID, updatedAt time.Time) error {
	_, err := store.queries.FailOrganization(ctx, dbgen.FailOrganizationParams{ID: pgUUID(id), UpdatedAt: pgTime(updatedAt)})
	return mapStoreError(err)
}

func (store *PostgresStore) GetBySlug(ctx context.Context, slug string) (Organization, error) {
	row, err := store.queries.GetOrganizationBySlug(ctx, slug)
	if err != nil {
		return Organization{}, mapStoreError(err)
	}
	return organizationFromRow(row), nil
}

func (store *PostgresStore) Update(ctx context.Context, value Organization) (Organization, error) {
	row, err := store.queries.UpdateOrganization(ctx, dbgen.UpdateOrganizationParams{
		ID: pgUUID(value.ID), Name: value.Name, Description: pgText(value.Description),
		Website: pgText(value.Website), Location: pgText(value.Location), BasePermission: string(value.BasePermission),
		MembersCanCreateRepositories: value.MembersCanCreateRepo, AtCid: pgText(value.ATCID), UpdatedAt: pgTime(value.UpdatedAt),
	})
	if err != nil {
		return Organization{}, mapStoreError(err)
	}
	return organizationFromRow(row), nil
}

func (store *PostgresStore) GetByID(ctx context.Context, id ID) (Organization, error) {
	row, err := store.queries.GetOrganizationByID(ctx, pgUUID(id))
	if err != nil {
		return Organization{}, mapStoreError(err)
	}
	return organizationFromRow(row), nil
}

func (store *PostgresStore) GetOwner(ctx context.Context, id ID, did string) (Member, error) {
	row, err := store.queries.GetOrganizationOwner(ctx, dbgen.GetOrganizationOwnerParams{OrganizationID: pgUUID(id), AccountDid: did})
	if err != nil {
		return Member{}, mapStoreError(err)
	}
	return memberFromRow(row), nil
}

func (store *PostgresStore) GetMember(ctx context.Context, id ID, did string) (Member, error) {
	row, err := store.queries.GetOrganizationMember(ctx, dbgen.GetOrganizationMemberParams{OrganizationID: pgUUID(id), AccountDid: did})
	if err != nil {
		return Member{}, mapStoreError(err)
	}
	return memberFromRow(row), nil
}

func (store *PostgresStore) ListForAccount(ctx context.Context, did string) ([]Organization, error) {
	rows, err := store.queries.ListOrganizationsForAccount(ctx, did)
	if err != nil {
		return nil, mapStoreError(err)
	}
	result := make([]Organization, len(rows))
	for index, row := range rows {
		result[index] = organizationFromRow(row)
	}
	return result, nil
}

func (store *PostgresStore) PageForAccount(ctx context.Context, did string, after *uuid.UUID, limit int32) ([]Organization, error) {
	var afterID pgtype.UUID
	if after != nil {
		afterID = pgRawUUID(*after)
	}
	rows, err := store.queries.PageOrganizationsForAccount(ctx, dbgen.PageOrganizationsForAccountParams{AccountDid: did, AfterID: afterID, PageLimit: limit})
	if err != nil {
		return nil, mapStoreError(err)
	}
	result := make([]Organization, len(rows))
	for index, row := range rows {
		result[index] = organizationFromRow(row)
	}
	return result, nil
}

func (store *PostgresStore) ListMembers(ctx context.Context, id ID) ([]Member, error) {
	rows, err := store.queries.ListOrganizationMembers(ctx, pgUUID(id))
	if err != nil {
		return nil, mapStoreError(err)
	}
	result := make([]Member, len(rows))
	for index, row := range rows {
		result[index] = Member{
			OrganizationID: ID(row.OrganizationID.Bytes), AccountDID: row.AccountDid, Handle: row.HandleCache.String,
			Role: Role(row.Role), Visibility: MembershipVisibility(row.Visibility), InvitedByDID: row.InvitedByDid,
			GrantURI: row.GrantUri.String, GrantCID: row.GrantCid.String,
			MembershipURI: row.MembershipUri.String, MembershipCID: row.MembershipCid.String,
			JoinedAt: row.JoinedAt.Time, UpdatedAt: row.UpdatedAt.Time,
		}
	}
	return result, nil
}

func (store *PostgresStore) PageMembers(ctx context.Context, id ID, includePrivate bool, after string, limit int32) ([]Member, error) {
	rows, err := store.queries.PageOrganizationMembers(ctx, dbgen.PageOrganizationMembersParams{OrganizationID: pgUUID(id), IncludePrivate: includePrivate, AfterDid: after, PageLimit: limit})
	if err != nil {
		return nil, mapStoreError(err)
	}
	result := make([]Member, len(rows))
	for index, row := range rows {
		result[index] = Member{
			OrganizationID: ID(row.OrganizationID.Bytes), AccountDID: row.AccountDid, Handle: row.HandleCache.String,
			Role: Role(row.Role), Visibility: MembershipVisibility(row.Visibility), InvitedByDID: row.InvitedByDid,
			GrantURI: row.GrantUri.String, GrantCID: row.GrantCid.String,
			MembershipURI: row.MembershipUri.String, MembershipCID: row.MembershipCid.String,
			JoinedAt: row.JoinedAt.Time, UpdatedAt: row.UpdatedAt.Time,
		}
	}
	return result, nil
}

func (store *PostgresStore) RecordAudit(ctx context.Context, event AuditEvent) error {
	metadata := event.Metadata
	if len(metadata) == 0 {
		metadata = json.RawMessage(`{}`)
	}
	return mapStoreError(store.queries.InsertOrganizationAuditEvent(ctx, dbgen.InsertOrganizationAuditEventParams{
		ID: pgRawUUID(event.ID), OrganizationID: pgUUID(event.OrganizationID), ActorDid: event.ActorDID,
		Action: event.Action, TargetType: event.TargetType, TargetID: event.TargetID,
		RequestID: pgText(event.RequestID), Metadata: metadata, CreatedAt: pgTime(event.CreatedAt),
	}))
}

func (store *PostgresStore) ListAuditEvents(ctx context.Context, organizationID ID, after *uuid.UUID, limit int32) ([]AuditEvent, error) {
	var afterID pgtype.UUID
	if after != nil {
		afterID = pgRawUUID(*after)
	}
	rows, err := store.queries.ListOrganizationAuditEvents(ctx, dbgen.ListOrganizationAuditEventsParams{OrganizationID: pgUUID(organizationID), AfterID: afterID, PageLimit: limit})
	if err != nil {
		return nil, mapStoreError(err)
	}
	result := make([]AuditEvent, len(rows))
	for index, row := range rows {
		result[index] = AuditEvent{ID: uuid.UUID(row.ID.Bytes), OrganizationID: ID(row.OrganizationID.Bytes), ActorDID: row.ActorDid, Action: row.Action, TargetType: row.TargetType, TargetID: row.TargetID, RequestID: row.RequestID.String, Metadata: append(json.RawMessage(nil), row.Metadata...), CreatedAt: row.CreatedAt.Time}
	}
	return result, nil
}

func (store *PostgresStore) CreateInvitation(ctx context.Context, invitation Invitation) (Invitation, error) {
	row, err := store.queries.CreateOrganizationInvitation(ctx, dbgen.CreateOrganizationInvitationParams{
		ID: pgRawUUID(invitation.ID), OrganizationID: pgUUID(invitation.OrganizationID),
		InviteeDid: invitation.InviteeDID, Role: string(invitation.Role), InvitedByDid: invitation.InvitedByDID,
		CreatedAt: pgTime(invitation.CreatedAt), ExpiresAt: pgTime(invitation.ExpiresAt),
	})
	if err != nil {
		return Invitation{}, mapStoreError(err)
	}
	return invitationFromRow(row), nil
}

func (store *PostgresStore) SetInvitationGrant(ctx context.Context, id uuid.UUID, identity ATIdentity) (Invitation, error) {
	row, err := store.queries.SetOrganizationInvitationGrant(ctx, dbgen.SetOrganizationInvitationGrantParams{
		ID: pgRawUUID(id), GrantUri: pgText(identity.URI), GrantCid: pgText(identity.CID),
	})
	if err != nil {
		return Invitation{}, mapStoreError(err)
	}
	return invitationFromRow(row), nil
}

func (store *PostgresStore) RevokeInvitation(ctx context.Context, id uuid.UUID, revokedAt time.Time) error {
	_, err := store.queries.RevokeOrganizationInvitation(ctx, dbgen.RevokeOrganizationInvitationParams{ID: pgRawUUID(id), RevokedAt: pgTime(revokedAt)})
	return mapStoreError(err)
}

func (store *PostgresStore) GetInvitation(ctx context.Context, id uuid.UUID) (Invitation, error) {
	row, err := store.queries.GetOrganizationInvitation(ctx, pgRawUUID(id))
	if err != nil {
		return Invitation{}, mapStoreError(err)
	}
	return invitationFromRow(row), nil
}

func (store *PostgresStore) ListInvitations(ctx context.Context, organizationID ID) ([]Invitation, error) {
	rows, err := store.queries.ListOrganizationInvitations(ctx, pgUUID(organizationID))
	if err != nil {
		return nil, mapStoreError(err)
	}
	result := make([]Invitation, len(rows))
	for index, row := range rows {
		result[index] = invitationFromRow(row)
	}
	return result, nil
}

func (store *PostgresStore) PageInvitations(ctx context.Context, organizationID ID, after *uuid.UUID, limit int32) ([]Invitation, error) {
	var afterID pgtype.UUID
	if after != nil {
		afterID = pgRawUUID(*after)
	}
	rows, err := store.queries.PageOrganizationInvitations(ctx, dbgen.PageOrganizationInvitationsParams{OrganizationID: pgUUID(organizationID), AfterID: afterID, PageLimit: limit})
	if err != nil {
		return nil, mapStoreError(err)
	}
	result := make([]Invitation, len(rows))
	for index, row := range rows {
		result[index] = invitationFromRow(row)
	}
	return result, nil
}

func (store *PostgresStore) ListPendingInvitations(ctx context.Context, did string, now time.Time) ([]Invitation, error) {
	rows, err := store.queries.ListPendingOrganizationInvitationsForAccount(ctx, dbgen.ListPendingOrganizationInvitationsForAccountParams{InviteeDid: did, ExpiresAt: pgTime(now)})
	if err != nil {
		return nil, mapStoreError(err)
	}
	result := make([]Invitation, len(rows))
	for index, row := range rows {
		result[index] = Invitation{
			ID: uuid.UUID(row.ID.Bytes), OrganizationID: ID(row.OrganizationID.Bytes),
			OrganizationSlug: row.OrganizationSlug, OrganizationName: row.OrganizationName,
			InviteeDID: row.InviteeDid, Role: Role(row.Role), InvitedByDID: row.InvitedByDid,
			GrantURI: row.GrantUri.String, GrantCID: row.GrantCid.String,
			CreatedAt: row.CreatedAt.Time, ExpiresAt: row.ExpiresAt.Time,
		}
	}
	return result, nil
}

func (store *PostgresStore) PagePendingInvitations(ctx context.Context, did string, now time.Time, after *uuid.UUID, limit int32) ([]Invitation, error) {
	var afterID pgtype.UUID
	if after != nil {
		afterID = pgRawUUID(*after)
	}
	rows, err := store.queries.PagePendingOrganizationInvitationsForAccount(ctx, dbgen.PagePendingOrganizationInvitationsForAccountParams{InviteeDid: did, ExpiresAt: pgTime(now), AfterID: afterID, PageLimit: limit})
	if err != nil {
		return nil, mapStoreError(err)
	}
	result := make([]Invitation, len(rows))
	for index, row := range rows {
		result[index] = Invitation{
			ID: uuid.UUID(row.ID.Bytes), OrganizationID: ID(row.OrganizationID.Bytes),
			OrganizationSlug: row.OrganizationSlug, OrganizationName: row.OrganizationName,
			InviteeDID: row.InviteeDid, Role: Role(row.Role), InvitedByDID: row.InvitedByDid,
			GrantURI: row.GrantUri.String, GrantCID: row.GrantCid.String,
			CreatedAt: row.CreatedAt.Time, ExpiresAt: row.ExpiresAt.Time,
		}
	}
	return result, nil
}

func (store *PostgresStore) AcceptInvitation(ctx context.Context, id uuid.UUID, actorDID string, membership ATIdentity, now time.Time) (Member, error) {
	tx, err := store.db.Begin(ctx)
	if err != nil {
		return Member{}, fmt.Errorf("begin invitation acceptance: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	queries := dbgen.New(tx)
	invitation, err := queries.LockOrganizationInvitation(ctx, pgRawUUID(id))
	if err != nil {
		return Member{}, mapStoreError(err)
	}
	value := invitationFromRow(invitation)
	if value.InviteeDID != actorDID || !value.Active(now) {
		return Member{}, ErrInvitation
	}
	if _, err := queries.AcceptOrganizationInvitation(ctx, dbgen.AcceptOrganizationInvitationParams{ID: pgRawUUID(id), AcceptedAt: pgTime(now)}); err != nil {
		return Member{}, mapStoreError(err)
	}
	member, err := queries.CreateOrganizationMemberFromInvitation(ctx, dbgen.CreateOrganizationMemberFromInvitationParams{
		OrganizationID: invitation.OrganizationID, AccountDid: actorDID, Role: invitation.Role,
		InvitedByDid: invitation.InvitedByDid, GrantUri: invitation.GrantUri, GrantCid: invitation.GrantCid,
		MembershipUri: pgText(membership.URI), MembershipCid: pgText(membership.CID), JoinedAt: pgTime(now),
	})
	if err != nil {
		return Member{}, mapStoreError(err)
	}
	if err := tx.Commit(ctx); err != nil {
		return Member{}, fmt.Errorf("commit invitation acceptance: %w", err)
	}
	return memberFromRow(member), nil
}

func (store *PostgresStore) UpdateMemberRole(ctx context.Context, organizationID ID, did string, role Role, grant, membership ATIdentity, now time.Time) (Member, error) {
	tx, err := store.db.Begin(ctx)
	if err != nil {
		return Member{}, fmt.Errorf("begin member role update: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	queries := dbgen.New(tx)
	owners, err := queries.LockOrganizationOwners(ctx, pgUUID(organizationID))
	if err != nil {
		return Member{}, mapStoreError(err)
	}
	if role != RoleOwner && len(owners) == 1 && owners[0] == did {
		return Member{}, ErrLastOwner
	}
	member, err := queries.UpdateOrganizationMemberRole(ctx, dbgen.UpdateOrganizationMemberRoleParams{
		OrganizationID: pgUUID(organizationID), AccountDid: did, Role: string(role),
		GrantUri: pgText(grant.URI), GrantCid: pgText(grant.CID),
		MembershipUri: pgText(membership.URI), MembershipCid: pgText(membership.CID), UpdatedAt: pgTime(now),
	})
	if err != nil {
		return Member{}, mapStoreError(err)
	}
	if err := tx.Commit(ctx); err != nil {
		return Member{}, fmt.Errorf("commit member role update: %w", err)
	}
	return memberFromRow(member), nil
}

func (store *PostgresStore) RemoveMember(ctx context.Context, organizationID ID, did string) (Member, error) {
	tx, err := store.db.Begin(ctx)
	if err != nil {
		return Member{}, fmt.Errorf("begin member removal: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	queries := dbgen.New(tx)
	owners, err := queries.LockOrganizationOwners(ctx, pgUUID(organizationID))
	if err != nil {
		return Member{}, mapStoreError(err)
	}
	if len(owners) == 1 && owners[0] == did {
		return Member{}, ErrLastOwner
	}
	member, err := queries.DeleteOrganizationMember(ctx, dbgen.DeleteOrganizationMemberParams{OrganizationID: pgUUID(organizationID), AccountDid: did})
	if err != nil {
		return Member{}, mapStoreError(err)
	}
	if err := tx.Commit(ctx); err != nil {
		return Member{}, fmt.Errorf("commit member removal: %w", err)
	}
	return memberFromRow(member), nil
}

func (store *PostgresStore) UpdateVisibility(ctx context.Context, organizationID ID, did string, visibility MembershipVisibility, grant, membership ATIdentity, now time.Time) (Member, error) {
	row, err := store.queries.UpdateOrganizationMembershipVisibility(ctx, dbgen.UpdateOrganizationMembershipVisibilityParams{
		OrganizationID: pgUUID(organizationID), AccountDid: did, Visibility: string(visibility),
		GrantUri: pgText(grant.URI), GrantCid: pgText(grant.CID),
		MembershipUri: pgText(membership.URI), MembershipCid: pgText(membership.CID), UpdatedAt: pgTime(now),
	})
	if err != nil {
		return Member{}, mapStoreError(err)
	}
	return memberFromRow(row), nil
}

func organizationFromRow(row dbgen.CoreOrganization) Organization {
	return Organization{
		ID: ID(row.ID.Bytes), Slug: row.Slug, Name: row.Name, Description: row.Description.String,
		Website: row.Website.String, Location: row.Location.String, CreatorDID: row.CreatorDid,
		BasePermission: BasePermission(row.BasePermission), MembersCanCreateRepo: row.MembersCanCreateRepositories,
		State: State(row.State), ATURI: row.AtUri.String, ATCID: row.AtCid.String,
		CreatedAt: row.CreatedAt.Time, UpdatedAt: row.UpdatedAt.Time,
	}
}

func memberFromRow(row dbgen.CoreOrganizationMember) Member {
	return Member{
		OrganizationID: ID(row.OrganizationID.Bytes), AccountDID: row.AccountDid, Role: Role(row.Role),
		Visibility: MembershipVisibility(row.Visibility), InvitedByDID: row.InvitedByDid,
		GrantURI: row.GrantUri.String, GrantCID: row.GrantCid.String,
		MembershipURI: row.MembershipUri.String, MembershipCID: row.MembershipCid.String,
		JoinedAt: row.JoinedAt.Time, UpdatedAt: row.UpdatedAt.Time,
	}
}

func invitationFromRow(row dbgen.CoreOrganizationInvitation) Invitation {
	result := Invitation{
		ID: uuid.UUID(row.ID.Bytes), OrganizationID: ID(row.OrganizationID.Bytes), InviteeDID: row.InviteeDid,
		Role: Role(row.Role), InvitedByDID: row.InvitedByDid, GrantURI: row.GrantUri.String,
		GrantCID: row.GrantCid.String, CreatedAt: row.CreatedAt.Time, ExpiresAt: row.ExpiresAt.Time,
	}
	if row.AcceptedAt.Valid {
		value := row.AcceptedAt.Time
		result.AcceptedAt = &value
	}
	if row.RevokedAt.Valid {
		value := row.RevokedAt.Time
		result.RevokedAt = &value
	}
	return result
}

func mapStoreError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	var postgresError *pgconn.PgError
	if errors.As(err, &postgresError) && postgresError.Code == "23505" {
		return ErrAlreadyExists
	}
	return fmt.Errorf("organization query: %w", err)
}

func pgUUID(id ID) pgtype.UUID           { return pgtype.UUID{Bytes: [16]byte(id), Valid: true} }
func pgRawUUID(id uuid.UUID) pgtype.UUID { return pgtype.UUID{Bytes: id, Valid: true} }
func pgText(value string) pgtype.Text    { return pgtype.Text{String: value, Valid: value != ""} }
func pgTime(value time.Time) pgtype.Timestamptz {
	return pgtype.Timestamptz{Time: value, Valid: true}
}
