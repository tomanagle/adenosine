package atproto

import (
	"context"
	"testing"
	"time"

	"github.com/adenosine-dev/adenosine/internal/repository"
	"github.com/adenosine-dev/adenosine/internal/transfer"
	"github.com/bluesky-social/indigo/atproto/atclient"
	"github.com/bluesky-social/indigo/atproto/auth/oauth"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/google/uuid"
)

func TestTransferPublicationUsesDeterministicRecords(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 8, 14, 1, 2, 3, 0, time.UTC)
	id := uuid.MustParse("0198a851-2a89-7ae2-a370-dc68883e3af4")
	proposalRKey := transfer.ProposalRecordKey(id)
	proposalURI := "at://" + canonicalDID + "/" + transfer.ProposalCollection + "/" + proposalRKey
	repositoryIdentity := transfer.Identity{URI: "at://" + canonicalDID + "/" + repositoryCollection + "/repo", CID: profileCID}
	testCases := []struct {
		name       string
		outputURI  string
		operation  func(context.Context, *Client) (transfer.Identity, error)
		collection string
		rkey       string
		assert     func(*testing.T, putRecordInput)
	}{
		{
			name: "proposal", outputURI: proposalURI, collection: transfer.ProposalCollection, rkey: proposalRKey,
			operation: func(ctx context.Context, client *Client) (transfer.Identity, error) {
				return client.PublishProposal(ctx, transfer.ProposalPublication{ID: id, ActorDID: canonicalDID, Repository: repositoryIdentity, DestinationDID: canonicalDID, DestinationOwnerAlias: "acme", DestinationOrganization: &repository.ATIdentity{URI: "at://" + canonicalDID + "/dev.adenosine.organization/acme", CID: profileCID}, CreatedAt: now, ExpiresAt: now.Add(time.Hour)})
			},
			assert: func(t *testing.T, input putRecordInput) {
				if input.Record["destinationDID"] != canonicalDID || input.Record["destinationOwner"] != "acme" || input.Record["destinationOrganization"] == nil {
					t.Fatalf("proposal record = %#v", input.Record)
				}
			},
		},
		{
			name: "acceptance", outputURI: "at://" + canonicalDID + "/" + transfer.AcceptanceCollection + "/" + transfer.AcceptanceRecordKey(proposalURI), collection: transfer.AcceptanceCollection, rkey: transfer.AcceptanceRecordKey(proposalURI),
			operation: func(ctx context.Context, client *Client) (transfer.Identity, error) {
				return client.PublishAcceptance(ctx, transfer.AcceptancePublication{ActorDID: canonicalDID, Proposal: transfer.Identity{URI: proposalURI, CID: profileCID}, Repository: repositoryIdentity, CreatedAt: now})
			},
			assert: func(t *testing.T, input putRecordInput) {
				if input.Record["proposal"] == nil || input.Record["repository"] == nil {
					t.Fatalf("acceptance record = %#v", input.Record)
				}
			},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			did := parsedDID(t)
			data := oauth.ClientSessionData{AccountDID: did, SessionID: "session", HostURL: "https://pds.example"}
			store := &repositorySessionStore{latest: data}
			session := &oauth.ClientSession{Data: &data}
			api := &fakeRepositoryAPI{output: putRecordOutput{URI: testCase.outputURI, CID: profileCID}}
			client := &Client{directory: profileIdentity(t, syntax.Handle("alice.test")), sessionStore: store,
				apiFactory: func(string, atclient.AuthMethod) profileAPI { return api },
				resume:     func(context.Context, syntax.DID, string) (*oauth.ClientSession, error) { return session, nil }}
			identity, err := testCase.operation(context.Background(), client)
			if err != nil || identity.URI != testCase.outputURI || store.calls != 1 {
				t.Fatalf("publication = %#v, error=%v, saves=%d", identity, err, store.calls)
			}
			input, ok := api.postInput.(putRecordInput)
			if !ok || input.Collection != testCase.collection || input.RKey != testCase.rkey || input.Repo != canonicalDID {
				t.Fatalf("put input = %#v", api.postInput)
			}
			testCase.assert(t, input)
		})
	}
}

func TestDeleteProposalIsAuthorityBoundAndIdempotent(t *testing.T) {
	t.Parallel()
	id := uuid.MustParse("0198a851-2a89-7ae2-a370-dc68883e3af4")
	rkey := transfer.ProposalRecordKey(id)
	publication := transfer.ProposalPublication{ID: id, ActorDID: canonicalDID}
	testCases := []struct {
		name     string
		identity transfer.Identity
		postErr  error
		wantErr  bool
	}{
		{name: "deletes exact record", identity: transfer.Identity{URI: "at://" + canonicalDID + "/" + transfer.ProposalCollection + "/" + rkey, CID: profileCID}},
		{name: "missing record is success", identity: transfer.Identity{URI: "at://" + canonicalDID + "/" + transfer.ProposalCollection + "/" + rkey, CID: profileCID}, postErr: recordNotFoundError()},
		{name: "rejects another authority", identity: transfer.Identity{URI: "at://did:plc:other/" + transfer.ProposalCollection + "/" + rkey, CID: profileCID}, wantErr: true},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			did := parsedDID(t)
			data := oauth.ClientSessionData{AccountDID: did, SessionID: "session", HostURL: "https://pds.example"}
			store := &repositorySessionStore{latest: data}
			api := &fakeRepositoryAPI{postErr: testCase.postErr}
			client := &Client{directory: profileIdentity(t, syntax.Handle("alice.test")), sessionStore: store,
				apiFactory: func(string, atclient.AuthMethod) profileAPI { return api },
				resume: func(context.Context, syntax.DID, string) (*oauth.ClientSession, error) {
					return &oauth.ClientSession{Data: &data}, nil
				}}
			err := client.DeleteProposal(context.Background(), publication, testCase.identity)
			if (err != nil) != testCase.wantErr {
				t.Fatalf("DeleteProposal() error = %v, want error %v", err, testCase.wantErr)
			}
		})
	}
}
