package atproto

import (
	"context"
	"errors"
	"fmt"

	"github.com/adenosine-dev/adenosine/internal/transfer"
	"github.com/bluesky-social/indigo/atproto/auth/oauth"
	"github.com/bluesky-social/indigo/atproto/syntax"
)

// PublishProposal writes a deterministic current-owner transfer proposal.
func (client *Client) PublishProposal(ctx context.Context, publication transfer.ProposalPublication) (transfer.Identity, error) {
	if err := validateTransferIdentity(publication.Repository, repositoryCollection); err != nil {
		return transfer.Identity{}, err
	}
	if publication.DestinationOwnerAlias == "" || !publication.ExpiresAt.After(publication.CreatedAt) {
		return transfer.Identity{}, errors.New("transfer destination and future expiry are required")
	}
	if did, err := syntax.ParseDID(publication.DestinationDID); err != nil || did.String() != publication.DestinationDID {
		return transfer.Identity{}, errors.New("transfer destination DID is not canonical")
	}
	rkey := transfer.ProposalRecordKey(publication.ID)
	if _, err := syntax.ParseRecordKey(rkey); err != nil {
		return transfer.Identity{}, fmt.Errorf("derive transfer proposal key: %w", err)
	}
	record := map[string]any{
		"$type":            transfer.ProposalCollection,
		"repository":       map[string]any{"uri": publication.Repository.URI, "cid": publication.Repository.CID},
		"destinationDID":   publication.DestinationDID,
		"destinationOwner": publication.DestinationOwnerAlias,
		"createdAt":        publication.CreatedAt.UTC().Format(syntax.AtprotoDatetimeLayout),
		"expiresAt":        publication.ExpiresAt.UTC().Format(syntax.AtprotoDatetimeLayout),
	}
	if publication.DestinationOrganization != nil {
		organization := transfer.Identity{URI: publication.DestinationOrganization.URI, CID: publication.DestinationOrganization.CID}
		if err := validateTransferIdentity(organization, organizationCollection); err != nil {
			return transfer.Identity{}, err
		}
		record["destinationOrganization"] = map[string]any{"uri": organization.URI, "cid": organization.CID}
	}
	return client.putTransferRecord(ctx, publication.ActorDID, transfer.ProposalCollection, rkey, record)
}

// PublishAcceptance writes a deterministic destination-owner acceptance.
func (client *Client) PublishAcceptance(ctx context.Context, publication transfer.AcceptancePublication) (transfer.Identity, error) {
	if err := validateTransferIdentity(publication.Proposal, transfer.ProposalCollection); err != nil {
		return transfer.Identity{}, err
	}
	if err := validateTransferIdentity(publication.Repository, repositoryCollection); err != nil {
		return transfer.Identity{}, err
	}
	rkey := transfer.AcceptanceRecordKey(publication.Proposal.URI)
	record := map[string]any{
		"$type":      transfer.AcceptanceCollection,
		"proposal":   map[string]any{"uri": publication.Proposal.URI, "cid": publication.Proposal.CID},
		"repository": map[string]any{"uri": publication.Repository.URI, "cid": publication.Repository.CID},
		"createdAt":  publication.CreatedAt.UTC().Format(syntax.AtprotoDatetimeLayout),
	}
	return client.putTransferRecord(ctx, publication.ActorDID, transfer.AcceptanceCollection, rkey, record)
}

// DeleteProposal removes a proposal that has not been accepted.
func (client *Client) DeleteProposal(ctx context.Context, publication transfer.ProposalPublication, identity transfer.Identity) error {
	rkey := transfer.ProposalRecordKey(publication.ID)
	uri, err := syntax.ParseATURI(identity.URI)
	if err != nil || uri.String() != identity.URI || uri.Authority().String() != publication.ActorDID || uri.Collection().String() != transfer.ProposalCollection || uri.RecordKey().String() != rkey {
		return errors.New("transfer proposal URI is not canonical")
	}
	if err := validateCID(identity.CID); err != nil {
		return err
	}
	return client.deleteTransferRecord(ctx, publication.ActorDID, transfer.ProposalCollection, rkey, identity.CID)
}

func (client *Client) putTransferRecord(ctx context.Context, actorDID, collection, rkey string, record map[string]any) (transfer.Identity, error) {
	did, _, host, err := client.resolveProfileIdentity(ctx, actorDID)
	if err != nil {
		return transfer.Identity{}, fmt.Errorf("resolve transfer author: %w", err)
	}
	lock := client.operationLock(did.String())
	lock.Lock()
	defer lock.Unlock()
	session, err := client.transferSession(ctx, did, host)
	if err != nil {
		return transfer.Identity{}, err
	}
	defer clearSession(session.Data)
	var output putRecordOutput
	operationErr := client.apiFactory(host, session).Post(ctx, putRecordNSID, putRecordInput{
		Collection: collection, Repo: did.String(), RKey: rkey, Record: record,
	}, &output)
	persistenceErr := client.sessionStore.SaveSession(ctx, *session.Data)
	if operationErr != nil || persistenceErr != nil {
		return transfer.Identity{}, errors.Join(providerErrorUnlessNil("transfer record publication", operationErr), providerErrorUnlessNil("transfer credential persistence", persistenceErr))
	}
	wantURI := "at://" + did.String() + "/" + collection + "/" + rkey
	uri, err := syntax.ParseATURI(output.URI)
	if err != nil || uri.String() != wantURI {
		return transfer.Identity{}, &ProviderError{Operation: "transfer response validation", Err: errors.New("published transfer URI is not canonical")}
	}
	if err := validateCID(output.CID); err != nil {
		return transfer.Identity{}, &ProviderError{Operation: "transfer response validation", Err: err}
	}
	return transfer.Identity{URI: output.URI, CID: output.CID}, nil
}

func (client *Client) deleteTransferRecord(ctx context.Context, actorDID, collection, rkey, cid string) error {
	did, _, host, err := client.resolveProfileIdentity(ctx, actorDID)
	if err != nil {
		return fmt.Errorf("resolve transfer author: %w", err)
	}
	lock := client.operationLock(did.String())
	lock.Lock()
	defer lock.Unlock()
	session, err := client.transferSession(ctx, did, host)
	if err != nil {
		return err
	}
	defer clearSession(session.Data)
	operationErr := client.apiFactory(host, session).Post(ctx, deleteRecordNSID, starDeleteRecordInput{Collection: collection, Repo: did.String(), RKey: rkey, SwapRecord: cid}, &struct{}{})
	if isRecordNotFound(operationErr) {
		operationErr = nil
	}
	persistenceErr := client.sessionStore.SaveSession(ctx, *session.Data)
	return errors.Join(providerErrorUnlessNil("transfer record deletion", operationErr), providerErrorUnlessNil("transfer credential persistence", persistenceErr))
}

func (client *Client) transferSession(ctx context.Context, did syntax.DID, host string) (*oauth.ClientSession, error) {
	latest, err := client.sessionStore.GetLatestSession(ctx, did)
	if err != nil {
		return nil, &ProviderError{Operation: "transfer credential load", Err: err}
	}
	if latest == nil {
		return nil, &ProviderError{Operation: "transfer credential load", Err: ErrSessionInvalid}
	}
	sessionID := latest.SessionID
	clearSession(latest)
	session, err := client.resumeSession(ctx, did, sessionID)
	if err != nil {
		return nil, &ProviderError{Operation: "transfer credential resume", Err: err}
	}
	if session == nil || session.Data == nil || session.Data.AccountDID != did || !samePDSHost(session.Data.HostURL, host) {
		return nil, &ProviderError{Operation: "transfer credential verification", Err: ErrSessionInvalid}
	}
	session.PersistSessionCallback = func(context.Context, *oauth.ClientSessionData) {}
	return session, nil
}

func validateTransferIdentity(value transfer.Identity, collection string) error {
	uri, err := syntax.ParseATURI(value.URI)
	if err != nil || uri.String() != value.URI || uri.Collection().String() != collection {
		return fmt.Errorf("transfer strong reference must use %s", collection)
	}
	return validateCID(value.CID)
}

func validateCID(value string) error {
	cid, err := syntax.ParseCID(value)
	if err != nil || cid.String() != value {
		return errors.New("transfer CID is not canonical")
	}
	return nil
}
