package transfer

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/adenosine-dev/adenosine/internal/repository"
	"github.com/google/uuid"
)

var (
	testRepositoryID = repository.ID(uuid.MustParse("0198a851-2a89-7ae2-a370-dc68883e3af3"))
	testTransferID   = uuid.MustParse("0198a851-2a89-7ae2-a370-dc68883e3af4")
	testNow          = time.Date(2026, 8, 14, 1, 2, 3, 0, time.UTC)
)

type stubStore struct {
	owner       Owner
	pending     Transfer
	transfer    Transfer
	repository  repository.Repository
	page        []Transfer
	resolveErr  error
	pendingErr  error
	getErr      error
	canAccept   bool
	canInitiate bool
	canComplete bool
	created     Transfer
	cancelled   bool
	pageLimit   int32
	pageAfter   *uuid.UUID
	completed   bool
	private     bool
	acceptedBy  string
	acceptActor string
	sourceAlias string
}

func (store *stubStore) ResolveOwner(context.Context, string) (Owner, error) {
	return store.owner, store.resolveErr
}
func (store *stubStore) ResolveOrganizationIdentity(context.Context, uuid.UUID) (repository.ATIdentity, error) {
	return repository.ATIdentity{}, nil
}
func (store *stubStore) CanInitiate(context.Context, repository.ID, string) (bool, error) {
	return store.canInitiate, nil
}
func (store *stubStore) CanAccept(_ context.Context, owner Owner, actorDID string) (bool, error) {
	if store.acceptActor != "" {
		return actorDID == store.acceptActor && owner.AccountDID == actorDID, nil
	}
	return store.canAccept, nil
}
func (store *stubStore) CanComplete(context.Context, uuid.UUID) (bool, error) {
	return store.canComplete, nil
}
func (store *stubStore) ResolveSourceAlias(context.Context, repository.ID) (string, error) {
	if store.sourceAlias == "" {
		return "alice.test", nil
	}
	return store.sourceAlias, nil
}
func (store *stubStore) GetRepository(context.Context, repository.ID) (repository.Repository, error) {
	return store.repository, store.getErr
}
func (store *stubStore) GetPending(context.Context, repository.ID) (Transfer, error) {
	return store.pending, store.pendingErr
}
func (store *stubStore) Create(_ context.Context, value Transfer) (Transfer, error) {
	store.created = value
	return value, nil
}
func (store *stubStore) Get(context.Context, uuid.UUID) (Transfer, error) {
	return store.transfer, store.getErr
}
func (store *stubStore) Page(_ context.Context, _ repository.ID, after *uuid.UUID, limit int32) ([]Transfer, error) {
	store.pageAfter, store.pageLimit = after, limit
	return store.page, nil
}
func (store *stubStore) SetProposal(_ context.Context, _ uuid.UUID, identity Identity) (Transfer, error) {
	value := store.created
	if value.ID == uuid.Nil {
		value = store.pending
	}
	value.Proposal = &identity
	return value, nil
}
func (store *stubStore) SetSuccessor(_ context.Context, _ uuid.UUID, identity Identity) (Transfer, error) {
	store.transfer.Successor = &identity
	return store.transfer, nil
}
func (store *stubStore) StartAcceptance(_ context.Context, _ uuid.UUID, at time.Time) (Transfer, error) {
	if store.transfer.AcceptanceStartedAt == nil {
		store.transfer.AcceptanceStartedAt = &at
	}
	return store.transfer, nil
}
func (store *stubStore) SetAcceptance(_ context.Context, _ uuid.UUID, identity Identity) (Transfer, error) {
	store.transfer.Acceptance = &identity
	return store.transfer, nil
}
func (store *stubStore) SetSourceRedirect(_ context.Context, _ uuid.UUID, cid string) (Transfer, error) {
	store.transfer.SourceRedirectCID = cid
	return store.transfer, nil
}
func (store *stubStore) Cancel(_ context.Context, _ uuid.UUID, at time.Time) (Transfer, error) {
	store.cancelled = true
	value := store.transfer
	value.Status, value.CancelledAt = StatusCancelled, &at
	return value, nil
}
func (store *stubStore) Complete(_ context.Context, _ uuid.UUID, _ uuid.UUID, _ uuid.UUID, actorDID string, at time.Time) (Transfer, error) {
	store.completed, store.acceptedBy = true, actorDID
	store.transfer.Status, store.transfer.AcceptedByDID, store.transfer.AcceptedAt = StatusCompleted, actorDID, &at
	return store.transfer, nil
}
func (store *stubStore) CompletePrivate(ctx context.Context, id, aliasID, sourceDIDAliasID uuid.UUID, actorDID string, at time.Time) (Transfer, error) {
	store.private = true
	return store.Complete(ctx, id, aliasID, sourceDIDAliasID, actorDID, at)
}

type stubPublisher struct{}

func (stubPublisher) PublishProposal(_ context.Context, value ProposalPublication) (Identity, error) {
	return Identity{URI: "at://" + value.ActorDID + "/" + ProposalCollection + "/" + ProposalRecordKey(value.ID), CID: "bafyproposal"}, nil
}
func (stubPublisher) DeleteProposal(context.Context, ProposalPublication, Identity) error { return nil }
func (stubPublisher) PublishAcceptance(context.Context, AcceptancePublication) (Identity, error) {
	return Identity{}, nil
}

type recordingPublisher struct {
	acceptance AcceptancePublication
	proposals  int
	accepts    int
	err        error
}

func (publisher *recordingPublisher) PublishProposal(_ context.Context, value ProposalPublication) (Identity, error) {
	publisher.proposals++
	return Identity{URI: "at://" + value.ActorDID + "/" + ProposalCollection + "/" + ProposalRecordKey(value.ID), CID: "bafyproposal"}, publisher.err
}
func (*recordingPublisher) DeleteProposal(context.Context, ProposalPublication, Identity) error {
	return nil
}
func (publisher *recordingPublisher) PublishAcceptance(_ context.Context, value AcceptancePublication) (Identity, error) {
	publisher.accepts++
	publisher.acceptance = value
	return Identity{URI: "at://" + value.ActorDID + "/" + AcceptanceCollection + "/acceptance", CID: "bafyacceptance"}, publisher.err
}

type recordingRepositoryPublisher struct{ publications []repository.Publication }

func (publisher *recordingRepositoryPublisher) Publish(_ context.Context, value repository.Publication) (repository.ATIdentity, error) {
	publisher.publications = append(publisher.publications, value)
	cid := "bafysuccessor"
	if value.TransferredTo != nil {
		cid = "bafyredirect"
	}
	return repository.ATIdentity{URI: "at://" + value.OwnerDID + "/dev.adenosine.repo/record", CID: cid}, nil
}

type fixedEndpoints struct{}

func (fixedEndpoints) For(value repository.Repository) (string, string, string) {
	owner := value.OwnerDID
	if value.OrganizationSlug != "" {
		owner = value.OrganizationSlug
	}
	return "https://code.test/" + owner + "/" + value.Slug, "https://code.test/" + owner + "/" + value.Slug + ".git", "ssh://git@code.test/" + owner + "/" + value.Slug + ".git"
}

type fixedIDs struct{ id uuid.UUID }

func (value fixedIDs) New() (uuid.UUID, error) { return value.id, nil }

type fixedClock struct{ now time.Time }

func (value fixedClock) Now() time.Time { return value.now }

func TestInitiate(t *testing.T) {
	repo := repository.Repository{ID: testRepositoryID, OwnerDID: "did:plc:alice", Slug: "project", State: repository.StateActive, ATURI: "at://did:plc:alice/dev.adenosine.repo/project", ATCID: "bafyrepo"}
	destination := Owner{Kind: OwnerAccount, Alias: "bob.test", AccountDID: "did:plc:bob", RecordAuthorDID: "did:plc:bob"}
	testCases := []struct {
		name        string
		store       *stubStore
		canInitiate bool
		dest        string
		wantErr     error
		wantID      uuid.UUID
		wantCreate  bool
	}{
		{name: "creates bilateral proposal state", store: &stubStore{owner: destination, pendingErr: ErrNotFound}, canInitiate: true, dest: "bob.test", wantID: testTransferID, wantCreate: true},
		{name: "resumes matching pending transfer", store: &stubStore{owner: destination, pending: Transfer{ID: testTransferID, Destination: destination, ExpiresAt: testNow.Add(time.Hour)}}, canInitiate: true, dest: "bob.test", wantID: testTransferID},
		{name: "requires expired pending transfer cancellation", store: &stubStore{owner: destination, pending: Transfer{ID: testTransferID, Destination: destination, ExpiresAt: testNow}}, canInitiate: true, dest: "bob.test", wantErr: ErrConflict},
		{name: "rejects competing pending destination", store: &stubStore{owner: destination, pending: Transfer{Destination: Owner{Kind: OwnerAccount, AccountDID: "did:plc:carol"}, ExpiresAt: testNow.Add(time.Hour)}}, canInitiate: true, dest: "bob.test", wantErr: ErrConflict},
		{name: "requires current owner", store: &stubStore{owner: destination, pendingErr: ErrNotFound}, dest: "bob.test", wantErr: ErrForbidden},
		{name: "rejects same owner", store: &stubStore{owner: Owner{Kind: OwnerAccount, AccountDID: repo.OwnerDID}, pendingErr: ErrNotFound}, canInitiate: true, dest: "alice.test", wantErr: ErrValidation},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			testCase.store.canInitiate = testCase.canInitiate
			service := NewService(testCase.store, stubPublisher{}, nil, nil, fixedIDs{id: testTransferID}, fixedClock{now: testNow})
			got, err := service.Initiate(context.Background(), repo, repo.OwnerDID, testCase.dest)
			if !errors.Is(err, testCase.wantErr) || got.ID != testCase.wantID {
				t.Fatalf("Initiate() = %#v, %v, want ID %s, error %v", got, err, testCase.wantID, testCase.wantErr)
			}
			if (testCase.store.created.ID != uuid.Nil) != testCase.wantCreate {
				t.Errorf("created = %#v, want create %v", testCase.store.created, testCase.wantCreate)
			}
			if testCase.wantCreate && (testCase.store.created.SourceRepository == nil || testCase.store.created.ExpiresAt != testNow.Add(Lifetime)) {
				t.Errorf("created transfer = %#v", testCase.store.created)
			}
		})
	}
}

func TestAuthorizeAcceptance(t *testing.T) {
	pending := Transfer{ID: testTransferID, RepositoryID: testRepositoryID, Destination: Owner{Kind: OwnerAccount, AccountDID: "did:plc:bob"}, Status: StatusPending, ExpiresAt: testNow.Add(time.Hour)}
	startedAt := testNow.Add(30 * time.Minute)
	repo := repository.Repository{ID: testRepositoryID, State: repository.StateActive}
	testCases := []struct {
		name     string
		store    *stubStore
		now      time.Time
		wantErr  error
		wantRepo repository.ID
	}{
		{name: "destination owner accepts", store: &stubStore{transfer: pending, repository: repo, canAccept: true}, now: testNow, wantRepo: testRepositoryID},
		{name: "started workflow resumes after expiry", store: &stubStore{transfer: func() Transfer { value := pending; value.AcceptanceStartedAt = &startedAt; return value }(), repository: repo, canAccept: true}, now: pending.ExpiresAt.Add(time.Hour), wantRepo: testRepositoryID},
		{name: "other account forbidden", store: &stubStore{transfer: pending}, now: testNow, wantErr: ErrForbidden},
		{name: "expired proposal conflicts", store: &stubStore{transfer: pending, canAccept: true}, now: pending.ExpiresAt, wantErr: ErrConflict},
		{name: "completed transfer conflicts", store: &stubStore{transfer: func() Transfer { value := pending; value.Status = StatusCompleted; return value }(), canAccept: true}, now: testNow, wantErr: ErrConflict},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			service := NewService(testCase.store, nil, nil, nil, fixedIDs{}, fixedClock{now: testCase.now})
			_, gotRepo, err := service.AuthorizeAcceptance(context.Background(), testTransferID, "did:plc:bob")
			if !errors.Is(err, testCase.wantErr) || gotRepo.ID != testCase.wantRepo {
				t.Fatalf("AuthorizeAcceptance() repository/error = %#v/%v, want %s/%v", gotRepo, err, testCase.wantRepo, testCase.wantErr)
			}
		})
	}
}

func TestInspectTransfer(t *testing.T) {
	completed := Transfer{ID: testTransferID, RepositoryID: testRepositoryID, SourceOwnerDID: "did:plc:alice", Destination: Owner{Kind: OwnerAccount, AccountDID: "did:plc:bob"}, Status: StatusCompleted}
	testCases := []struct {
		name     string
		actorDID string
		wantErr  error
	}{
		{name: "former source owner can inspect completed history", actorDID: "did:plc:alice"},
		{name: "destination owner can inspect completed history", actorDID: "did:plc:bob"},
		{name: "unrelated account cannot inspect", actorDID: "did:plc:mallory", wantErr: ErrForbidden},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			store := &stubStore{transfer: completed, acceptActor: testCase.actorDID}
			service := NewService(store, nil, nil, nil, fixedIDs{}, fixedClock{now: testNow})
			_, err := service.Get(context.Background(), testTransferID, testCase.actorDID)
			if !errors.Is(err, testCase.wantErr) {
				t.Fatalf("Get() error = %v, want %v", err, testCase.wantErr)
			}
		})
	}
}

func TestAccept(t *testing.T) {
	organizationID := uuid.MustParse("0198a851-2a89-7ae2-a370-dc68883e3af6")
	source := &Identity{URI: "at://did:plc:alice/dev.adenosine.repo/source", CID: "bafysource"}
	proposal := &Identity{URI: "at://did:plc:alice/" + ProposalCollection + "/proposal", CID: "bafyproposal"}
	successor := &Identity{URI: "at://did:plc:root/dev.adenosine.repo/successor", CID: "bafysuccessor"}
	acceptance := &Identity{URI: "at://did:plc:root/" + AcceptanceCollection + "/acceptance", CID: "bafyacceptance"}
	startedAt := testNow.Add(30 * time.Minute)
	base := Transfer{
		ID: testTransferID, RepositoryID: testRepositoryID, SourceOwnerDID: "did:plc:alice",
		SourceOwnerAlias: "alice.test", SourceRepository: source,
		Destination: Owner{Kind: OwnerOrganization, Alias: "acme", AccountDID: "did:plc:root", RecordAuthorDID: "did:plc:root", OrganizationID: &organizationID, Organization: &repository.ATIdentity{URI: "at://did:plc:root/dev.adenosine.organization/acme", CID: "bafyorg"}},
		Proposal:    proposal, Status: StatusPending, CreatedAt: testNow, ExpiresAt: testNow.Add(time.Hour),
	}
	testCases := []struct {
		name              string
		value             Transfer
		private           bool
		publisherErr      error
		canComplete       bool
		now               time.Time
		wantErr           error
		wantRepoPublishes int
		wantAccepts       int
		wantWorkflowAt    time.Time
	}{
		{name: "publishes and completes bilateral chain", value: base, canComplete: true, wantRepoPublishes: 2, wantAccepts: 1},
		{name: "resumes persisted publication steps", value: func() Transfer {
			value := base
			value.Successor, value.Acceptance, value.SourceRedirectCID = successor, acceptance, "bafyredirect"
			return value
		}(), canComplete: true, wantRepoPublishes: 0, wantAccepts: 0},
		{name: "resumes started workflow after expiry", value: func() Transfer {
			value := base
			value.AcceptanceStartedAt = &startedAt
			return value
		}(), canComplete: true, now: base.ExpiresAt.Add(time.Hour), wantRepoPublishes: 2, wantAccepts: 1, wantWorkflowAt: startedAt},
		{name: "completes private transfer atomically", value: func() Transfer { value := base; value.SourceRepository, value.Proposal = nil, nil; return value }(), canComplete: true, private: true},
		{name: "rejects destination route conflict before publication", value: base, wantErr: ErrConflict},
		{name: "redacts provider failure", value: base, canComplete: true, publisherErr: errors.New("provider-secret"), wantErr: ErrProvider, wantRepoPublishes: 1, wantAccepts: 1},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			store := &stubStore{transfer: testCase.value, repository: repository.Repository{ID: testRepositoryID, OwnerDID: "did:plc:alice", Slug: "project", DisplayName: "Project", State: repository.StateActive, CreatedAt: testNow}, canAccept: true, canComplete: testCase.canComplete}
			publisher := &recordingPublisher{err: testCase.publisherErr}
			repositoryPublisher := &recordingRepositoryPublisher{}
			now := testCase.now
			if now.IsZero() {
				now = testNow
			}
			service := NewService(store, publisher, repositoryPublisher, fixedEndpoints{}, fixedIDs{id: uuid.MustParse("0198a851-2a89-7ae2-a370-dc68883e3af7")}, fixedClock{now: now})
			got, err := service.Accept(context.Background(), testTransferID, "did:plc:accepting-owner")
			if !errors.Is(err, testCase.wantErr) {
				t.Fatalf("Accept() error = %v, want %v", err, testCase.wantErr)
			}
			if testCase.publisherErr != nil && strings.Contains(err.Error(), "provider-secret") {
				t.Fatalf("Accept() leaked provider error: %v", err)
			}
			if len(repositoryPublisher.publications) != testCase.wantRepoPublishes || publisher.accepts != testCase.wantAccepts {
				t.Fatalf("publication calls = %d/%d, want %d/%d", len(repositoryPublisher.publications), publisher.accepts, testCase.wantRepoPublishes, testCase.wantAccepts)
			}
			if testCase.wantErr == nil && (!store.completed || store.private != testCase.private || got.Status != StatusCompleted || store.acceptedBy != "did:plc:accepting-owner") {
				t.Fatalf("completion = %#v, private=%v, acceptedBy=%q", got, store.private, store.acceptedBy)
			}
			if testCase.wantAccepts == 1 && testCase.publisherErr == nil && publisher.acceptance.ActorDID != "did:plc:root" {
				t.Fatalf("acceptance actor = %q", publisher.acceptance.ActorDID)
			}
			if !testCase.wantWorkflowAt.IsZero() && (!publisher.acceptance.CreatedAt.Equal(testCase.wantWorkflowAt) || !repositoryPublisher.publications[0].UpdatedAt.Equal(testCase.wantWorkflowAt)) {
				t.Fatalf("portable workflow timestamps = %s/%s, want %s", publisher.acceptance.CreatedAt, repositoryPublisher.publications[0].UpdatedAt, testCase.wantWorkflowAt)
			}
			if testCase.wantRepoPublishes == 2 && (repositoryPublisher.publications[0].TransferredFrom == nil || repositoryPublisher.publications[1].TransferredTo == nil) {
				t.Fatalf("repository publications = %#v", repositoryPublisher.publications)
			}
		})
	}
}

func TestPageAndCancel(t *testing.T) {
	started := testNow
	testCases := []struct {
		name          string
		operation     string
		store         *stubStore
		canInitiate   bool
		limit         int
		wantItems     int
		wantNext      bool
		wantCancelled bool
		wantErr       error
	}{
		{name: "page returns bounded cursor", operation: "page", store: &stubStore{page: []Transfer{{ID: testTransferID}, {ID: uuid.MustParse("0198a851-2a89-7ae2-a370-dc68883e3af5")}}}, canInitiate: true, limit: 1, wantItems: 1, wantNext: true},
		{name: "page validates limit", operation: "page", store: &stubStore{}, limit: 0, wantErr: ErrValidation},
		{name: "source owner cancels pending", operation: "cancel", store: &stubStore{transfer: Transfer{ID: testTransferID, RepositoryID: testRepositoryID, Status: StatusPending}}, canInitiate: true, wantCancelled: true},
		{name: "started acceptance cannot be cancelled", operation: "cancel", store: &stubStore{transfer: Transfer{ID: testTransferID, RepositoryID: testRepositoryID, Status: StatusPending, AcceptanceStartedAt: &started}}, canInitiate: true, wantErr: ErrConflict},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			testCase.store.canInitiate = testCase.canInitiate
			service := NewService(testCase.store, stubPublisher{}, nil, nil, fixedIDs{}, fixedClock{now: testNow})
			var err error
			if testCase.operation == "page" {
				page, pageErr := service.Page(context.Background(), testRepositoryID, "did:plc:alice", nil, testCase.limit)
				err = pageErr
				if len(page.Items) != testCase.wantItems || (page.NextCursor != nil) != testCase.wantNext {
					t.Errorf("Page() = %#v, want %d items, next %v", page, testCase.wantItems, testCase.wantNext)
				}
			} else {
				_, err = service.Cancel(context.Background(), testTransferID, "did:plc:alice")
			}
			if !errors.Is(err, testCase.wantErr) || testCase.store.cancelled != testCase.wantCancelled {
				t.Fatalf("operation error/cancelled = %v/%v, want %v/%v", err, testCase.store.cancelled, testCase.wantErr, testCase.wantCancelled)
			}
		})
	}
}
