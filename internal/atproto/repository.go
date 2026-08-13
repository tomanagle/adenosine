package atproto

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/adenosine-dev/adenosine/internal/repository"
	"github.com/bluesky-social/indigo/atproto/auth/oauth"
	"github.com/bluesky-social/indigo/atproto/syntax"
)

const repositoryCollection = "dev.adenosine.repo"

// Publish writes a deterministic public repository record using the owner's latest OAuth delegation.
func (client *Client) Publish(ctx context.Context, publication repository.Publication) (repository.ATIdentity, error) {
	did, _, host, err := client.resolveProfileIdentity(ctx, publication.OwnerDID)
	if err != nil {
		return repository.ATIdentity{}, fmt.Errorf("resolve repository owner: %w", err)
	}
	rkey := strings.ReplaceAll(publication.ID.String(), "-", "")
	if _, err := syntax.ParseRecordKey(rkey); err != nil {
		return repository.ATIdentity{}, fmt.Errorf("derive repository record key: %w", err)
	}

	lock := client.operationLock(did.String())
	lock.Lock()
	defer lock.Unlock()
	latest, err := client.sessionStore.GetLatestSession(ctx, did)
	if err != nil {
		return repository.ATIdentity{}, &ProviderError{Operation: "repository credential load", Err: err}
	}
	if latest == nil {
		return repository.ATIdentity{}, &ProviderError{Operation: "repository credential load", Err: ErrSessionInvalid}
	}
	sessionID := latest.SessionID
	clearSession(latest)
	session, err := client.resumeSession(ctx, did, sessionID)
	if err != nil {
		return repository.ATIdentity{}, &ProviderError{Operation: "repository credential resume", Err: err}
	}
	if session == nil || session.Data == nil {
		return repository.ATIdentity{}, &ProviderError{Operation: "repository credential resume", Err: ErrSessionInvalid}
	}
	defer clearSession(session.Data)
	if session.Data.AccountDID != did || !samePDSHost(session.Data.HostURL, host) {
		return repository.ATIdentity{}, &ProviderError{Operation: "repository credential verification", Err: ErrSessionInvalid}
	}
	// Persist DPoP nonce/token rotations ourselves because Indigo's callback cannot report failures.
	session.PersistSessionCallback = func(context.Context, *oauth.ClientSessionData) {}

	createdAt := publication.CreatedAt.UTC().Format(syntax.AtprotoDatetimeLayout)
	updatedAt := publication.UpdatedAt.UTC().Format(syntax.AtprotoDatetimeLayout)
	input := putRecordInput{
		Collection: repositoryCollection,
		Repo:       did.String(),
		RKey:       rkey,
		Record: map[string]any{
			"$type": repositoryCollection, "slug": publication.Slug, "name": publication.Name,
			"defaultBranch": publication.DefaultBranch,
			"git":           map[string]any{"https": publication.GitHTTPS, "ssh": publication.GitSSH},
			"web":           publication.Web, "createdAt": createdAt, "updatedAt": updatedAt,
		},
	}
	if publication.Description != "" {
		input.Record["description"] = publication.Description
	}
	if publication.Organization != nil {
		input.Record["organization"] = map[string]any{"uri": publication.Organization.URI, "cid": publication.Organization.CID}
	}
	if publication.ForkedFrom != nil {
		input.Record["forkedFrom"] = map[string]any{"uri": publication.ForkedFrom.URI, "cid": publication.ForkedFrom.CID}
	}
	var output putRecordOutput
	operationErr := client.apiFactory(host, session).Post(ctx, putRecordNSID, input, &output)
	persistenceErr := client.sessionStore.SaveSession(ctx, *session.Data)
	if operationErr != nil || persistenceErr != nil {
		return repository.ATIdentity{}, errors.Join(
			providerErrorUnlessNil("repository record publication", operationErr),
			providerErrorUnlessNil("repository credential persistence", persistenceErr),
		)
	}

	wantURI := "at://" + did.String() + "/" + repositoryCollection + "/" + rkey
	uri, err := syntax.ParseATURI(output.URI)
	if err != nil || uri.String() != wantURI {
		return repository.ATIdentity{}, &ProviderError{Operation: "repository response validation", Err: errors.New("published repository URI is not canonical")}
	}
	cid, err := syntax.ParseCID(output.CID)
	if err != nil || cid.String() != output.CID {
		return repository.ATIdentity{}, &ProviderError{Operation: "repository response validation", Err: errors.New("published repository CID is not canonical")}
	}
	return repository.ATIdentity{URI: output.URI, CID: output.CID}, nil
}

// Unpublish removes a public repository record using CID compare-and-swap.
func (client *Client) Unpublish(ctx context.Context, publication repository.Publication, identity repository.ATIdentity) error {
	did, _, host, err := client.resolveProfileIdentity(ctx, publication.OwnerDID)
	if err != nil {
		return fmt.Errorf("resolve repository owner: %w", err)
	}
	rkey := strings.ReplaceAll(publication.ID.String(), "-", "")
	uri, err := syntax.ParseATURI(identity.URI)
	if err != nil || uri.String() != identity.URI || uri.Authority().String() != did.String() || uri.Collection().String() != repositoryCollection || uri.RecordKey().String() != rkey {
		return &ProviderError{Operation: "repository deletion validation", Err: errors.New("repository URI is not canonical")}
	}
	cid, err := syntax.ParseCID(identity.CID)
	if err != nil || cid.String() != identity.CID {
		return &ProviderError{Operation: "repository deletion validation", Err: errors.New("repository CID is not canonical")}
	}

	lock := client.operationLock(did.String())
	lock.Lock()
	defer lock.Unlock()
	latest, err := client.sessionStore.GetLatestSession(ctx, did)
	if err != nil {
		return &ProviderError{Operation: "repository credential load", Err: err}
	}
	if latest == nil {
		return &ProviderError{Operation: "repository credential load", Err: ErrSessionInvalid}
	}
	sessionID := latest.SessionID
	clearSession(latest)
	session, err := client.resumeSession(ctx, did, sessionID)
	if err != nil {
		return &ProviderError{Operation: "repository credential resume", Err: err}
	}
	if session == nil || session.Data == nil {
		return &ProviderError{Operation: "repository credential resume", Err: ErrSessionInvalid}
	}
	defer clearSession(session.Data)
	if session.Data.AccountDID != did || !samePDSHost(session.Data.HostURL, host) {
		return &ProviderError{Operation: "repository credential verification", Err: ErrSessionInvalid}
	}
	session.PersistSessionCallback = func(context.Context, *oauth.ClientSessionData) {}
	operationErr := client.apiFactory(host, session).Post(ctx, deleteRecordNSID, starDeleteRecordInput{
		Collection: repositoryCollection, Repo: did.String(), RKey: rkey, SwapRecord: identity.CID,
	}, &struct{}{})
	persistenceErr := client.sessionStore.SaveSession(ctx, *session.Data)
	return errors.Join(
		providerErrorUnlessNil("repository record deletion", operationErr),
		providerErrorUnlessNil("repository credential persistence", persistenceErr),
	)
}

func providerErrorUnlessNil(operation string, err error) error {
	if err == nil {
		return nil
	}
	return &ProviderError{Operation: operation, Err: err}
}
