package atproto

import (
	"context"
	"crypto/sha256"
	"encoding/base32"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/adenosine-dev/adenosine/internal/organization"
	"github.com/bluesky-social/indigo/atproto/auth/oauth"
	"github.com/bluesky-social/indigo/atproto/syntax"
)

const organizationCollection = "dev.adenosine.organization"

const (
	organizationGrantCollection      = "dev.adenosine.organizationGrant"
	organizationMembershipCollection = "dev.adenosine.organizationMembership"
	organizationRevocationCollection = "dev.adenosine.organizationRevocation"
)

type organizationDeleteRecordInput struct {
	Collection string `json:"collection"`
	Repo       string `json:"repo"`
	RKey       string `json:"rkey"`
	SwapRecord string `json:"swapRecord"`
}

// PublishOrganization writes the stable organization root record to its creator's AT repository.
func (client *Client) PublishOrganization(ctx context.Context, publication organization.Publication) (organization.ATIdentity, error) {
	did, _, host, err := client.resolveProfileIdentity(ctx, publication.CreatorDID)
	if err != nil {
		return organization.ATIdentity{}, fmt.Errorf("resolve organization creator: %w", err)
	}
	rkey := strings.ReplaceAll(publication.ID.String(), "-", "")
	if _, err := syntax.ParseRecordKey(rkey); err != nil {
		return organization.ATIdentity{}, fmt.Errorf("derive organization record key: %w", err)
	}

	lock := client.operationLock(did.String())
	lock.Lock()
	defer lock.Unlock()
	latest, err := client.sessionStore.GetLatestSession(ctx, did)
	if err != nil {
		return organization.ATIdentity{}, &ProviderError{Operation: "organization credential load", Err: err}
	}
	if latest == nil {
		return organization.ATIdentity{}, &ProviderError{Operation: "organization credential load", Err: ErrSessionInvalid}
	}
	sessionID := latest.SessionID
	clearSession(latest)
	session, err := client.resumeSession(ctx, did, sessionID)
	if err != nil {
		return organization.ATIdentity{}, &ProviderError{Operation: "organization credential resume", Err: err}
	}
	if session == nil || session.Data == nil {
		return organization.ATIdentity{}, &ProviderError{Operation: "organization credential resume", Err: ErrSessionInvalid}
	}
	defer clearSession(session.Data)
	if session.Data.AccountDID != did || !samePDSHost(session.Data.HostURL, host) {
		return organization.ATIdentity{}, &ProviderError{Operation: "organization credential verification", Err: ErrSessionInvalid}
	}
	session.PersistSessionCallback = func(context.Context, *oauth.ClientSessionData) {}

	input := putRecordInput{
		Collection: organizationCollection,
		Repo:       did.String(),
		RKey:       rkey,
		Record: map[string]any{
			"$type": organizationCollection, "slug": publication.Slug, "name": publication.Name,
			"createdAt": publication.CreatedAt.UTC().Format(syntax.AtprotoDatetimeLayout),
			"updatedAt": publication.UpdatedAt.UTC().Format(syntax.AtprotoDatetimeLayout),
		},
	}
	if publication.Description != "" {
		input.Record["description"] = publication.Description
	}
	if publication.Website != "" {
		input.Record["website"] = publication.Website
	}
	if publication.Location != "" {
		input.Record["location"] = publication.Location
	}
	var output putRecordOutput
	operationErr := client.apiFactory(host, session).Post(ctx, putRecordNSID, input, &output)
	persistenceErr := client.sessionStore.SaveSession(ctx, *session.Data)
	if operationErr != nil || persistenceErr != nil {
		return organization.ATIdentity{}, errors.Join(
			providerErrorUnlessNil("organization record publication", operationErr),
			providerErrorUnlessNil("organization credential persistence", persistenceErr),
		)
	}
	wantURI := "at://" + did.String() + "/" + organizationCollection + "/" + rkey
	uri, err := syntax.ParseATURI(output.URI)
	if err != nil || uri.String() != wantURI {
		return organization.ATIdentity{}, &ProviderError{Operation: "organization response validation", Err: errors.New("published organization URI is not canonical")}
	}
	cid, err := syntax.ParseCID(output.CID)
	if err != nil || cid.String() != output.CID {
		return organization.ATIdentity{}, &ProviderError{Operation: "organization response validation", Err: errors.New("published organization CID is not canonical")}
	}
	return organization.ATIdentity{URI: output.URI, CID: output.CID}, nil
}

// PublishOrganizationGrant writes an owner-authored invitation grant.
func (client *Client) PublishOrganizationGrant(ctx context.Context, publication organization.GrantPublication) (organization.ATIdentity, error) {
	rkey := strings.ReplaceAll(publication.ID.String(), "-", "")
	record := map[string]any{
		"$type":        organizationGrantCollection,
		"organization": strongRef(publication.Organization),
		"subject":      publication.SubjectDID,
		"role":         string(publication.Role),
		"authority":    strongRef(publication.Authority),
		"createdAt":    publication.CreatedAt.UTC().Format(syntax.AtprotoDatetimeLayout),
	}
	if !publication.ExpiresAt.IsZero() {
		record["expiresAt"] = publication.ExpiresAt.UTC().Format(syntax.AtprotoDatetimeLayout)
	}
	return client.publishOrganizationRecord(ctx, publication.ActorDID, organizationGrantCollection, rkey, record, "organization grant")
}

// PublishOrganizationMembership writes the member-authored acceptance and visibility record.
func (client *Client) PublishOrganizationMembership(ctx context.Context, publication organization.MembershipPublication) (organization.ATIdentity, error) {
	digest := sha256.Sum256([]byte(publication.Organization.URI))
	rkey := strings.ToLower(base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(digest[:]))
	record := map[string]any{
		"$type":        organizationMembershipCollection,
		"organization": strongRef(publication.Organization),
		"grant":        strongRef(publication.Grant),
		"visibility":   string(publication.Visibility),
		"createdAt":    publication.CreatedAt.UTC().Format(syntax.AtprotoDatetimeLayout),
		"updatedAt":    publication.UpdatedAt.UTC().Format(syntax.AtprotoDatetimeLayout),
	}
	return client.publishOrganizationRecord(ctx, publication.ActorDID, organizationMembershipCollection, rkey, record, "organization membership")
}

// PublishOrganizationRevocation writes an owner-authored removal record.
func (client *Client) PublishOrganizationRevocation(ctx context.Context, publication organization.RevocationPublication) (organization.ATIdentity, error) {
	rkey := strings.ReplaceAll(publication.ID.String(), "-", "")
	record := map[string]any{
		"$type":        organizationRevocationCollection,
		"organization": strongRef(publication.Organization),
		"grant":        strongRef(publication.Grant),
		"subject":      publication.SubjectDID,
		"authority":    strongRef(publication.Authority),
		"createdAt":    publication.CreatedAt.UTC().Format(syntax.AtprotoDatetimeLayout),
	}
	return client.publishOrganizationRecord(ctx, publication.ActorDID, organizationRevocationCollection, rkey, record, "organization revocation")
}

// DeleteOrganizationMembership removes the current public membership record. Historical AT
// repository commits may remain observable, so callers must not describe this as erasing history.
func (client *Client) DeleteOrganizationMembership(ctx context.Context, actorDID string, organizationIdentity, membership organization.ATIdentity) error {
	digest := sha256.Sum256([]byte(organizationIdentity.URI))
	rkey := strings.ToLower(base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(digest[:]))
	did, _, host, err := client.resolveProfileIdentity(ctx, actorDID)
	if err != nil {
		return fmt.Errorf("resolve organization membership actor: %w", err)
	}
	wantURI := "at://" + did.String() + "/" + organizationMembershipCollection + "/" + rkey
	if membership.URI != wantURI {
		return &ProviderError{Operation: "organization membership deletion validation", Err: errors.New("membership URI does not match actor and organization")}
	}
	lock := client.operationLock(did.String())
	lock.Lock()
	defer lock.Unlock()
	latest, err := client.sessionStore.GetLatestSession(ctx, did)
	if err != nil || latest == nil {
		return &ProviderError{Operation: "organization membership credential load", Err: errors.Join(err, ErrSessionInvalid)}
	}
	sessionID := latest.SessionID
	clearSession(latest)
	session, err := client.resumeSession(ctx, did, sessionID)
	if err != nil || session == nil || session.Data == nil {
		return &ProviderError{Operation: "organization membership credential resume", Err: errors.Join(err, ErrSessionInvalid)}
	}
	defer clearSession(session.Data)
	if session.Data.AccountDID != did || !samePDSHost(session.Data.HostURL, host) {
		return &ProviderError{Operation: "organization membership credential verification", Err: ErrSessionInvalid}
	}
	session.PersistSessionCallback = func(context.Context, *oauth.ClientSessionData) {}
	operationErr := client.apiFactory(host, session).Post(ctx, deleteRecordNSID, organizationDeleteRecordInput{Collection: organizationMembershipCollection, Repo: did.String(), RKey: rkey, SwapRecord: membership.CID}, &struct{}{})
	if isRecordNotFound(operationErr) {
		operationErr = nil
	}
	persistCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	persistenceErr := client.sessionStore.SaveSession(persistCtx, *session.Data)
	cancel()
	return errors.Join(
		providerErrorUnlessNil("organization membership deletion", operationErr),
		providerErrorUnlessNil("organization credential persistence", persistenceErr),
	)
}

func (client *Client) publishOrganizationRecord(ctx context.Context, actorDID, collection, rkey string, record map[string]any, operation string) (organization.ATIdentity, error) {
	if _, err := syntax.ParseRecordKey(rkey); err != nil {
		return organization.ATIdentity{}, fmt.Errorf("derive %s record key: %w", operation, err)
	}
	did, _, host, err := client.resolveProfileIdentity(ctx, actorDID)
	if err != nil {
		return organization.ATIdentity{}, fmt.Errorf("resolve %s actor: %w", operation, err)
	}
	lock := client.operationLock(did.String())
	lock.Lock()
	defer lock.Unlock()
	latest, err := client.sessionStore.GetLatestSession(ctx, did)
	if err != nil {
		return organization.ATIdentity{}, &ProviderError{Operation: operation + " credential load", Err: err}
	}
	if latest == nil {
		return organization.ATIdentity{}, &ProviderError{Operation: operation + " credential load", Err: ErrSessionInvalid}
	}
	sessionID := latest.SessionID
	clearSession(latest)
	session, err := client.resumeSession(ctx, did, sessionID)
	if err != nil {
		return organization.ATIdentity{}, &ProviderError{Operation: operation + " credential resume", Err: err}
	}
	if session == nil || session.Data == nil {
		return organization.ATIdentity{}, &ProviderError{Operation: operation + " credential resume", Err: ErrSessionInvalid}
	}
	defer clearSession(session.Data)
	if session.Data.AccountDID != did || !samePDSHost(session.Data.HostURL, host) {
		return organization.ATIdentity{}, &ProviderError{Operation: operation + " credential verification", Err: ErrSessionInvalid}
	}
	session.PersistSessionCallback = func(context.Context, *oauth.ClientSessionData) {}
	var output putRecordOutput
	operationErr := client.apiFactory(host, session).Post(ctx, putRecordNSID, putRecordInput{
		Collection: collection, Repo: did.String(), RKey: rkey, Record: record,
	}, &output)
	persistenceErr := client.sessionStore.SaveSession(ctx, *session.Data)
	if operationErr != nil || persistenceErr != nil {
		return organization.ATIdentity{}, errors.Join(
			providerErrorUnlessNil(operation+" publication", operationErr),
			providerErrorUnlessNil(operation+" credential persistence", persistenceErr),
		)
	}
	wantURI := "at://" + did.String() + "/" + collection + "/" + rkey
	uri, err := syntax.ParseATURI(output.URI)
	if err != nil || uri.String() != wantURI {
		return organization.ATIdentity{}, &ProviderError{Operation: operation + " response validation", Err: errors.New("published record URI is not canonical")}
	}
	cid, err := syntax.ParseCID(output.CID)
	if err != nil || cid.String() != output.CID {
		return organization.ATIdentity{}, &ProviderError{Operation: operation + " response validation", Err: errors.New("published record CID is not canonical")}
	}
	return organization.ATIdentity{URI: output.URI, CID: output.CID}, nil
}

func strongRef(identity organization.ATIdentity) map[string]any {
	return map[string]any{"uri": identity.URI, "cid": identity.CID}
}
