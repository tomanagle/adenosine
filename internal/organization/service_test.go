package organization

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/adenosine-dev/adenosine/internal/requestcontext"
	"github.com/google/uuid"
)

type memoryStore struct {
	value                Organization
	members              []Member
	owner                Member
	member               Member
	ownerErr             error
	memberErr            error
	roleCalls            int
	removeCalls          int
	visibilityCalls      int
	visibilityGrant      ATIdentity
	visibilityMembership ATIdentity
	roleGrant            ATIdentity
	roleMembership       ATIdentity
	audits               []AuditEvent
	listAudits           []AuditEvent
	auditAfter           *uuid.UUID
	auditLimit           int32
	createErr            error
	activeErr            error
	failErr              error
	failed               bool
	activated            bool
}

func (store *memoryStore) Create(_ context.Context, value Organization) (Organization, error) {
	store.value = value
	return value, store.createErr
}
func (store *memoryStore) Activate(_ context.Context, _ ID, identity ATIdentity, updatedAt time.Time) (Organization, error) {
	store.activated = true
	store.value.State, store.value.ATURI, store.value.ATCID, store.value.UpdatedAt = StateActive, identity.URI, identity.CID, updatedAt
	return store.value, store.activeErr
}
func (store *memoryStore) Fail(context.Context, ID, time.Time) error {
	store.failed = true
	return store.failErr
}
func (store *memoryStore) GetBySlug(context.Context, string) (Organization, error) {
	return store.value, nil
}
func (store *memoryStore) ListForAccount(context.Context, string) ([]Organization, error) {
	return []Organization{store.value}, nil
}
func (store *memoryStore) PageForAccount(context.Context, string, *uuid.UUID, int32) ([]Organization, error) {
	return []Organization{store.value}, nil
}
func (store *memoryStore) Update(_ context.Context, value Organization) (Organization, error) {
	store.value = value
	return value, nil
}
func (store *memoryStore) ListMembers(context.Context, ID) ([]Member, error) {
	return store.members, nil
}
func (store *memoryStore) PageMembers(context.Context, ID, bool, string, int32) ([]Member, error) {
	return store.members, nil
}
func (store *memoryStore) GetByID(context.Context, ID) (Organization, error) {
	return store.value, nil
}
func (store *memoryStore) GetOwner(context.Context, ID, string) (Member, error) {
	if store.ownerErr != nil {
		return Member{}, store.ownerErr
	}
	if store.owner.AccountDID == "" {
		return Member{}, ErrNotFound
	}
	return store.owner, nil
}
func (store *memoryStore) GetMember(_ context.Context, _ ID, did string) (Member, error) {
	if store.memberErr != nil {
		return Member{}, store.memberErr
	}
	if did == store.owner.AccountDID && store.owner.AccountDID != "" {
		return store.owner, nil
	}
	if store.member.AccountDID == "" {
		return Member{}, ErrNotFound
	}
	return store.member, nil
}
func (store *memoryStore) CreateInvitation(_ context.Context, value Invitation) (Invitation, error) {
	return value, nil
}
func (store *memoryStore) RevokeInvitation(context.Context, uuid.UUID, time.Time) error { return nil }
func (store *memoryStore) GetInvitation(context.Context, uuid.UUID) (Invitation, error) {
	return Invitation{}, ErrNotFound
}
func (store *memoryStore) ListInvitations(context.Context, ID) ([]Invitation, error) {
	return nil, nil
}
func (store *memoryStore) PageInvitations(context.Context, ID, *uuid.UUID, int32) ([]Invitation, error) {
	return nil, nil
}
func (store *memoryStore) ListPendingInvitations(context.Context, string, time.Time) ([]Invitation, error) {
	return nil, nil
}
func (store *memoryStore) PagePendingInvitations(context.Context, string, time.Time, *uuid.UUID, int32) ([]Invitation, error) {
	return nil, nil
}
func (store *memoryStore) AcceptInvitation(context.Context, uuid.UUID, string, ATIdentity, time.Time) (Member, error) {
	return Member{}, nil
}
func (store *memoryStore) UpdateMemberRole(_ context.Context, _ ID, _ string, _ Role, grant, membership ATIdentity, _ time.Time) (Member, error) {
	store.roleCalls++
	store.roleGrant, store.roleMembership = grant, membership
	return store.member, nil
}
func (store *memoryStore) RemoveMember(context.Context, ID, string) (Member, error) {
	store.removeCalls++
	return store.member, nil
}
func (store *memoryStore) UpdateVisibility(_ context.Context, _ ID, _ string, visibility MembershipVisibility, grant, membership ATIdentity, _ time.Time) (Member, error) {
	store.visibilityCalls++
	store.visibilityGrant, store.visibilityMembership = grant, membership
	store.member.Visibility = visibility
	store.member.GrantURI, store.member.GrantCID = grant.URI, grant.CID
	store.member.MembershipURI, store.member.MembershipCID = membership.URI, membership.CID
	return store.member, nil
}
func (store *memoryStore) RecordAudit(_ context.Context, event AuditEvent) error {
	store.audits = append(store.audits, event)
	return nil
}
func (store *memoryStore) ListAuditEvents(_ context.Context, _ ID, after *uuid.UUID, limit int32) ([]AuditEvent, error) {
	store.auditAfter = after
	store.auditLimit = limit
	return store.listAudits, nil
}

type fixedClock struct{ now time.Time }

func (clock fixedClock) Now() time.Time { return clock.now }

type fixedIDs struct{ id ID }

func (ids fixedIDs) New() (ID, error) { return ids.id, nil }

type sequenceIDs struct {
	ids  []ID
	next int
}

func (ids *sequenceIDs) New() (ID, error) {
	if ids.next >= len(ids.ids) {
		return ID{}, errors.New("no test ID available")
	}
	value := ids.ids[ids.next]
	ids.next++
	return value, nil
}

type fakePublisher struct {
	identity           ATIdentity
	membershipIdentity ATIdentity
	err                error
	got                Publication
	grants             []GrantPublication
	memberships        []MembershipPublication
	revocations        []RevocationPublication
	grantErr           error
	revocationErr      error
	deleteCalls        int
}

func (publisher *fakePublisher) PublishOrganization(_ context.Context, publication Publication) (ATIdentity, error) {
	publisher.got = publication
	return publisher.identity, publisher.err
}
func (publisher *fakePublisher) PublishOrganizationGrant(_ context.Context, value GrantPublication) (ATIdentity, error) {
	publisher.grants = append(publisher.grants, value)
	return publisher.identity, publisher.grantErr
}
func (publisher *fakePublisher) PublishOrganizationMembership(_ context.Context, value MembershipPublication) (ATIdentity, error) {
	publisher.memberships = append(publisher.memberships, value)
	if publisher.membershipIdentity != (ATIdentity{}) {
		return publisher.membershipIdentity, publisher.err
	}
	return publisher.identity, publisher.err
}
func (publisher *fakePublisher) PublishOrganizationRevocation(_ context.Context, value RevocationPublication) (ATIdentity, error) {
	publisher.revocations = append(publisher.revocations, value)
	return publisher.identity, publisher.revocationErr
}
func (publisher *fakePublisher) DeleteOrganizationMembership(context.Context, string, ATIdentity, ATIdentity) error {
	publisher.deleteCalls++
	return nil
}

func TestServiceChangeRole(t *testing.T) {
	t.Parallel()
	organizationID := ID(uuid.MustParse("0198a851-2a89-7ae2-a370-dc68883e3af1"))
	grantID := ID(uuid.MustParse("0198a851-2a89-7ae2-a370-dc68883e3af2"))
	revocationID := ID(uuid.MustParse("0198a851-2a89-7ae2-a370-dc68883e3af3"))
	value := Organization{ID: organizationID, CreatorDID: "did:plc:alice", ATURI: "at://did:plc:alice/dev.adenosine.organization/root", ATCID: "bafy-root"}
	owner := Member{AccountDID: "did:plc:alice", Role: RoleOwner}
	testCases := []struct {
		name            string
		target          Member
		role            Role
		wantErr         error
		wantRoleCalls   int
		wantGrants      int
		wantMemberships int
		wantRevocations int
	}{
		{name: "changes public role and reattests before revoking prior grant", target: Member{AccountDID: "did:plc:bob", Role: RoleMember, Visibility: VisibilityPublic, GrantURI: "at://did:plc:alice/dev.adenosine.organizationGrant/old", GrantCID: "bafy-old", MembershipURI: "at://did:plc:bob/dev.adenosine.organizationMembership/current", MembershipCID: "bafy-membership", JoinedAt: time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC)}, role: RoleOwner, wantRoleCalls: 1, wantGrants: 1, wantMemberships: 1, wantRevocations: 1},
		{name: "changes private role without public records", target: Member{AccountDID: "did:plc:bob", Role: RoleMember, Visibility: VisibilityPrivate}, role: RoleOwner, wantRoleCalls: 1},
		{name: "same role is idempotent", target: Member{AccountDID: "did:plc:bob", Role: RoleMember}, role: RoleMember},
		{name: "creator cannot be demoted", target: Member{AccountDID: "did:plc:alice", Role: RoleOwner}, role: RoleMember, wantErr: ErrCreatorOwner},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			store := &memoryStore{value: value, owner: owner, member: testCase.target}
			publisher := &fakePublisher{
				identity:           ATIdentity{URI: "at://did:plc:alice/dev.adenosine.organizationGrant/new", CID: "bafy-new"},
				membershipIdentity: ATIdentity{URI: "at://did:plc:bob/dev.adenosine.organizationMembership/current", CID: "bafy-membership-new"},
			}
			ids := &sequenceIDs{ids: []ID{grantID, revocationID}}
			service := NewService(store, fixedClock{time.Date(2026, 8, 13, 1, 2, 3, 0, time.UTC)}, ids, publisher)
			_, err := service.ChangeRole(context.Background(), organizationID, owner.AccountDID, testCase.target.AccountDID, testCase.role)
			if !errors.Is(err, testCase.wantErr) {
				t.Fatalf("ChangeRole() error = %v, want %v", err, testCase.wantErr)
			}
			if store.roleCalls != testCase.wantRoleCalls || len(publisher.grants) != testCase.wantGrants || len(publisher.memberships) != testCase.wantMemberships || len(publisher.revocations) != testCase.wantRevocations {
				t.Fatalf("role calls/grants/memberships/revocations = %d/%d/%d/%d, want %d/%d/%d/%d", store.roleCalls, len(publisher.grants), len(publisher.memberships), len(publisher.revocations), testCase.wantRoleCalls, testCase.wantGrants, testCase.wantMemberships, testCase.wantRevocations)
			}
			if testCase.wantMemberships == 1 && (publisher.memberships[0].ActorDID != testCase.target.AccountDID || publisher.memberships[0].Grant != publisher.identity || store.roleGrant != publisher.identity || store.roleMembership != publisher.membershipIdentity) {
				t.Fatalf("membership re-attestation = %#v, stored = %#v", publisher.memberships[0], store.roleMembership)
			}
			if len(publisher.revocations) == 1 && publisher.revocations[0].Grant.URI != testCase.target.GrantURI {
				t.Fatalf("revoked grant = %q, want %q", publisher.revocations[0].Grant.URI, testCase.target.GrantURI)
			}
		})
	}
}

func TestServiceRemove(t *testing.T) {
	t.Parallel()
	organizationID := ID(uuid.MustParse("0198a851-2a89-7ae2-a370-dc68883e3af1"))
	value := Organization{ID: organizationID, CreatorDID: "did:plc:alice", ATURI: "at://did:plc:alice/dev.adenosine.organization/root", ATCID: "bafy-root"}
	testCases := []struct {
		name            string
		actor           Member
		target          Member
		wantErr         error
		wantRemoveCalls int
		wantRevocations int
		wantDeletes     int
	}{
		{name: "owner removes public member", actor: Member{AccountDID: "did:plc:alice", Role: RoleOwner}, target: Member{AccountDID: "did:plc:bob", Role: RoleMember, GrantURI: "at://did:plc:alice/dev.adenosine.organizationGrant/bob", GrantCID: "bafy-bob", MembershipURI: "at://did:plc:bob/dev.adenosine.organizationMembership/current", MembershipCID: "bafy-membership"}, wantRemoveCalls: 1, wantRevocations: 1, wantDeletes: 1},
		{name: "public member leaves through controller authority", actor: Member{AccountDID: "did:plc:bob", Role: RoleMember}, target: Member{AccountDID: "did:plc:bob", Role: RoleMember, GrantURI: "at://did:plc:alice/dev.adenosine.organizationGrant/bob", GrantCID: "bafy-bob", MembershipURI: "at://did:plc:bob/dev.adenosine.organizationMembership/current", MembershipCID: "bafy-membership"}, wantRemoveCalls: 1, wantRevocations: 1, wantDeletes: 1},
		{name: "creator cannot be removed", actor: Member{AccountDID: "did:plc:alice", Role: RoleOwner}, target: Member{AccountDID: "did:plc:alice", Role: RoleOwner}, wantErr: ErrCreatorOwner},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			owner := Member{AccountDID: "did:plc:alice", Role: RoleOwner}
			store := &memoryStore{value: value, owner: owner, member: testCase.target}
			publisher := &fakePublisher{}
			service := NewService(store, fixedClock{time.Date(2026, 8, 13, 1, 2, 3, 0, time.UTC)}, fixedIDs{ID(uuid.MustParse("0198a851-2a89-7ae2-a370-dc68883e3af4"))}, publisher)
			err := service.Remove(context.Background(), organizationID, testCase.actor.AccountDID, testCase.target.AccountDID)
			if !errors.Is(err, testCase.wantErr) {
				t.Fatalf("Remove() error = %v, want %v", err, testCase.wantErr)
			}
			if store.removeCalls != testCase.wantRemoveCalls || len(publisher.revocations) != testCase.wantRevocations || publisher.deleteCalls != testCase.wantDeletes {
				t.Fatalf("remove calls/revocations/deletes = %d/%d/%d, want %d/%d/%d", store.removeCalls, len(publisher.revocations), publisher.deleteCalls, testCase.wantRemoveCalls, testCase.wantRevocations, testCase.wantDeletes)
			}
			if testCase.name == "public member leaves through controller authority" && (publisher.revocations[0].ActorDID != value.CreatorDID || publisher.revocations[0].Authority.URI != value.ATURI) {
				t.Fatalf("self-removal revocation = %#v", publisher.revocations[0])
			}
		})
	}
}

func TestServiceSetVisibility(t *testing.T) {
	t.Parallel()
	organizationID := ID(uuid.MustParse("0198a851-2a89-7ae2-a370-dc68883e3af1"))
	value := Organization{ID: organizationID, CreatorDID: "did:plc:alice", ATURI: "at://did:plc:alice/dev.adenosine.organization/root", ATCID: "bafy-root"}
	publicGrant := "at://did:plc:alice/dev.adenosine.organizationGrant/public"
	publicMembership := "at://did:plc:bob/dev.adenosine.organizationMembership/public"
	testCases := []struct {
		name            string
		member          Member
		visibility      MembershipVisibility
		wantCalls       int
		wantGrants      int
		wantMemberships int
		wantRevocations int
		wantDeletes     int
	}{
		{name: "private membership becomes public with consent records", member: Member{AccountDID: "did:plc:bob", Role: RoleMember, Visibility: VisibilityPrivate}, visibility: VisibilityPublic, wantCalls: 1, wantGrants: 1, wantMemberships: 1},
		{name: "public membership becomes private and removes current evidence", member: Member{AccountDID: "did:plc:bob", Role: RoleMember, Visibility: VisibilityPublic, GrantURI: publicGrant, GrantCID: "bafy-grant", MembershipURI: publicMembership, MembershipCID: "bafy-membership"}, visibility: VisibilityPrivate, wantCalls: 1, wantRevocations: 1, wantDeletes: 1},
		{name: "same visibility is idempotent", member: Member{AccountDID: "did:plc:bob", Role: RoleMember, Visibility: VisibilityPrivate}, visibility: VisibilityPrivate},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			store := &memoryStore{value: value, owner: Member{AccountDID: "did:plc:alice", Role: RoleOwner}, member: testCase.member}
			publisher := &fakePublisher{identity: ATIdentity{URI: "at://did:plc:alice/dev.adenosine.organizationGrant/new", CID: "bafy-new"}}
			service := NewService(store, fixedClock{time.Date(2026, 8, 13, 1, 2, 3, 0, time.UTC)}, fixedIDs{ID(uuid.MustParse("0198a851-2a89-7ae2-a370-dc68883e3af4"))}, publisher)
			_, err := service.SetVisibility(context.Background(), organizationID, testCase.member.AccountDID, testCase.visibility)
			if err != nil {
				t.Fatalf("SetVisibility() error = %v", err)
			}
			if store.visibilityCalls != testCase.wantCalls || len(publisher.grants) != testCase.wantGrants || len(publisher.memberships) != testCase.wantMemberships || len(publisher.revocations) != testCase.wantRevocations || publisher.deleteCalls != testCase.wantDeletes {
				t.Fatalf("updates/grants/memberships/revocations/deletes = %d/%d/%d/%d/%d, want %d/%d/%d/%d/%d", store.visibilityCalls, len(publisher.grants), len(publisher.memberships), len(publisher.revocations), publisher.deleteCalls, testCase.wantCalls, testCase.wantGrants, testCase.wantMemberships, testCase.wantRevocations, testCase.wantDeletes)
			}
			if testCase.visibility == VisibilityPrivate && testCase.wantCalls == 1 && (store.visibilityGrant.URI != "" || store.visibilityMembership.URI != "") {
				t.Fatalf("private membership retained public identities: %#v %#v", store.visibilityGrant, store.visibilityMembership)
			}
		})
	}
}

func TestServiceCreate(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 8, 13, 1, 2, 3, 0, time.UTC)
	id := ID(uuid.MustParse("0198a851-2a89-7ae2-a370-dc68883e3af1"))
	testCases := []struct {
		name       string
		publisher  *fakePublisher
		wantErr    bool
		wantFailed bool
		wantAudits int
	}{
		{name: "publishes and activates", publisher: &fakePublisher{identity: ATIdentity{URI: "at://did:plc:alice/dev.adenosine.organization/0198a8512a897ae2a370dc68883e3af1", CID: "bafy-test"}}, wantAudits: 1},
		{name: "publication failure marks failed", publisher: &fakePublisher{err: errors.New("PDS unavailable")}, wantErr: true, wantFailed: true},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			store := &memoryStore{}
			service := NewService(store, fixedClock{now}, fixedIDs{id}, testCase.publisher)
			ctx := requestcontext.WithRequestID(context.Background(), "request-1")
			got, err := service.Create(ctx, CreateInput{
				CreatorDID: "did:plc:alice", Slug: "adenosine", Name: "Adenosine",
				BasePermission: BasePermissionRead, MembersCanCreateRepo: true,
			})
			if (err != nil) != testCase.wantErr {
				t.Fatalf("Create() error = %v, want error %t", err, testCase.wantErr)
			}
			if store.failed != testCase.wantFailed {
				t.Fatalf("failed state = %t, want %t", store.failed, testCase.wantFailed)
			}
			if len(store.audits) != testCase.wantAudits {
				t.Fatalf("audit events = %d, want %d", len(store.audits), testCase.wantAudits)
			}
			if len(store.audits) == 1 && store.audits[0].RequestID != "request-1" {
				t.Fatalf("audit request ID = %q", store.audits[0].RequestID)
			}
			if !testCase.wantErr && (got.State != StateActive || !store.activated || testCase.publisher.got.CreatorDID != "did:plc:alice") {
				t.Fatalf("created organization = %#v, publication = %#v", got, testCase.publisher.got)
			}
		})
	}
}

func TestServiceAuditEvents(t *testing.T) {
	t.Parallel()
	organizationID := ID(uuid.MustParse("0198a851-2a89-7ae2-a370-dc68883e3af1"))
	firstID := uuid.MustParse("0198a851-2a89-7ae2-a370-dc68883e3af2")
	secondID := uuid.MustParse("0198a851-2a89-7ae2-a370-dc68883e3af3")
	testCases := []struct {
		name        string
		limit       int
		ownerErr    error
		wantErr     error
		wantItems   int
		wantNext    *uuid.UUID
		wantDBLimit int32
	}{
		{name: "returns keyset page", limit: 1, wantItems: 1, wantNext: &firstID, wantDBLimit: 2},
		{name: "owner required", limit: 30, ownerErr: ErrNotFound, wantErr: ErrForbidden},
		{name: "limit validated", limit: 101, wantErr: ErrValidation},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			store := &memoryStore{
				owner: Member{AccountDID: "did:plc:alice", Role: RoleOwner}, ownerErr: testCase.ownerErr,
				listAudits: []AuditEvent{{ID: firstID}, {ID: secondID}},
			}
			service := NewService(store, fixedClock{}, fixedIDs{organizationID}, &fakePublisher{})
			page, err := service.AuditEvents(context.Background(), organizationID, "did:plc:alice", testCase.limit, nil)
			if !errors.Is(err, testCase.wantErr) {
				t.Fatalf("AuditEvents() error = %v, want %v", err, testCase.wantErr)
			}
			if len(page.Items) != testCase.wantItems {
				t.Fatalf("items = %d, want %d", len(page.Items), testCase.wantItems)
			}
			if (page.NextCursor == nil) != (testCase.wantNext == nil) || page.NextCursor != nil && *page.NextCursor != *testCase.wantNext {
				t.Fatalf("next cursor = %v, want %v", page.NextCursor, testCase.wantNext)
			}
			if store.auditLimit != testCase.wantDBLimit {
				t.Fatalf("database limit = %d, want %d", store.auditLimit, testCase.wantDBLimit)
			}
		})
	}
}
