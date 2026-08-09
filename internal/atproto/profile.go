package atproto

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"

	"github.com/adenosine-dev/adenosine/internal/profile"
	"github.com/bluesky-social/indigo/atproto/atclient"
	"github.com/bluesky-social/indigo/atproto/auth/oauth"
	indigoidentity "github.com/bluesky-social/indigo/atproto/identity"
	"github.com/bluesky-social/indigo/atproto/syntax"
)

const (
	profileCollection = "dev.adenosine.profile"
	profileRKey       = "self"
)

var (
	getRecordNSID = syntax.NSID("com.atproto.repo.getRecord")
	putRecordNSID = syntax.NSID("com.atproto.repo.putRecord")
)

type profileAPI interface {
	Get(context.Context, syntax.NSID, map[string]any, any) error
	Post(context.Context, syntax.NSID, any, any) error
}

type getRecordOutput struct {
	CID   *string          `json:"cid"`
	URI   string           `json:"uri"`
	Value *json.RawMessage `json:"value"`
}

type putRecordInput struct {
	Collection string         `json:"collection"`
	Record     map[string]any `json:"record"`
	Repo       string         `json:"repo"`
	RKey       string         `json:"rkey"`
}

type putRecordOutput struct {
	CID string `json:"cid"`
	URI string `json:"uri"`
}

type profileRecord struct {
	Type        string `json:"$type"`
	DisplayName string `json:"displayName,omitempty"`
	Bio         string `json:"bio,omitempty"`
	Website     string `json:"website,omitempty"`
	Location    string `json:"location,omitempty"`
	CreatedAt   string `json:"createdAt"`
}

// Get reads the canonical public self profile from the DID's current PDS.
func (client *Client) Get(ctx context.Context, rawDID string) (profile.Profile, error) {
	did, identity, host, err := client.resolveProfileIdentity(ctx, rawDID)
	if err != nil {
		return profile.Profile{}, err
	}
	var output getRecordOutput
	err = client.apiFactory(host, nil).Get(ctx, getRecordNSID, map[string]any{
		"collection": profileCollection,
		"repo":       did.String(),
		"rkey":       profileRKey,
	}, &output)
	if err != nil {
		return profile.Profile{}, mapProfileAPIError("get", did.String(), err)
	}
	result, err := decodeProfile(output, did.String())
	if err != nil {
		return profile.Profile{}, &profile.ProviderError{Operation: "get", Err: err}
	}
	if !identity.Handle.IsInvalidHandle() {
		result.Handle = identity.Handle.Normalize().String()
	}
	return result, nil
}

// Put writes the deterministic self profile using the latest OAuth credential.
func (client *Client) Put(ctx context.Context, rawDID string, record profile.Record) (profile.Profile, error) {
	if record.CreatedAt.IsZero() {
		return profile.Profile{}, &profile.ValidationError{Field: "createdAt", Problem: "must not be empty"}
	}
	if err := (profile.UpdateInput{
		DisplayName: record.DisplayName, Bio: record.Bio, Website: record.Website, Location: record.Location,
	}).Validate(); err != nil {
		return profile.Profile{}, err
	}
	did, identity, host, err := client.resolveProfileIdentity(ctx, rawDID)
	if err != nil {
		return profile.Profile{}, err
	}
	lock := client.operationLock(did.String())
	lock.Lock()
	defer lock.Unlock()

	latest, err := client.sessionStore.GetLatestSession(ctx, did)
	if err != nil {
		if errors.Is(err, ErrSessionNotFound) {
			return profile.Profile{}, &profile.AuthorizationError{Err: err}
		}
		return profile.Profile{}, &profile.ProviderError{Operation: "credential load", Err: err}
	}
	sessionID := latest.SessionID
	clearSession(latest)
	session, err := client.resumeSession(ctx, did, sessionID)
	sessionID = ""
	if err != nil {
		if errors.Is(err, ErrSessionNotFound) {
			return profile.Profile{}, &profile.AuthorizationError{Err: err}
		}
		return profile.Profile{}, &profile.ProviderError{Operation: "credential resume", Err: err}
	}
	if session == nil || session.Data == nil {
		return profile.Profile{}, &profile.ProviderError{Operation: "credential resume", Err: ErrSessionInvalid}
	}
	defer clearSession(session.Data)
	if session.Data.AccountDID != did || !samePDSHost(session.Data.HostURL, host) {
		return profile.Profile{}, &profile.ProviderError{Operation: "credential verification", Err: ErrSessionInvalid}
	}
	// Indigo's callback logs session IDs and cannot return persistence failures.
	// Keep rotations in memory and persist them explicitly below.
	session.PersistSessionCallback = func(context.Context, *oauth.ClientSessionData) {}

	createdAt := record.CreatedAt.UTC().Format(syntax.AtprotoDatetimeLayout)
	input := putRecordInput{
		Collection: profileCollection,
		Repo:       did.String(),
		RKey:       profileRKey,
		Record: map[string]any{
			"$type":       profileCollection,
			"displayName": record.DisplayName,
			"bio":         record.Bio,
			"website":     record.Website,
			"location":    record.Location,
			"createdAt":   createdAt,
		},
	}
	var output putRecordOutput
	operationErr := client.apiFactory(host, session).Post(ctx, putRecordNSID, input, &output)
	persistenceErr := client.sessionStore.SaveSession(ctx, *session.Data)
	if operationErr != nil || persistenceErr != nil {
		var mappedOperation, mappedPersistence error
		if operationErr != nil {
			mappedOperation = mapProfileAPIError("put", did.String(), operationErr)
		}
		if persistenceErr != nil {
			mappedPersistence = &profile.ProviderError{Operation: "credential persistence", Err: persistenceErr}
		}
		return profile.Profile{}, errors.Join(mappedOperation, mappedPersistence)
	}
	value, _ := json.Marshal(profileRecord{
		Type: profileCollection, DisplayName: record.DisplayName, Bio: record.Bio,
		Website: record.Website, Location: record.Location, CreatedAt: createdAt,
	})
	result, err := decodeProfile(getRecordOutput{CID: &output.CID, URI: output.URI, Value: rawMessage(value)}, did.String())
	if err != nil {
		return profile.Profile{}, &profile.ProviderError{Operation: "put", Err: err}
	}
	if !identity.Handle.IsInvalidHandle() {
		result.Handle = identity.Handle.Normalize().String()
	}
	return result, nil
}

func (client *Client) resumeSession(ctx context.Context, did syntax.DID, sessionID string) (*oauth.ClientSession, error) {
	if client.resume != nil {
		return client.resume(ctx, did, sessionID)
	}
	if client.app == nil {
		return nil, ErrSessionNotFound
	}
	return client.app.ResumeSession(ctx, did, sessionID)
}

func (client *Client) resolveProfileIdentity(ctx context.Context, rawDID string) (syntax.DID, *indigoidentity.Identity, string, error) {
	did, err := syntax.ParseDID(rawDID)
	if err != nil || did.String() != rawDID {
		return "", nil, "", &profile.ValidationError{Field: "DID", Problem: "must be a valid canonical AT Protocol DID"}
	}
	identity, err := client.directory.LookupDID(ctx, did)
	if err != nil {
		return "", nil, "", &profile.ProviderError{Operation: "identity resolution", Err: err}
	}
	if identity == nil || identity.DID != did {
		return "", nil, "", &profile.ProviderError{Operation: "identity verification", Err: errors.New("resolved DID mismatch")}
	}
	host, err := canonicalPDSHost(identity.PDSEndpoint())
	if err != nil {
		return "", nil, "", &profile.ProviderError{Operation: "PDS verification", Err: err}
	}
	return did, identity, host, nil
}

func canonicalPDSHost(raw string) (string, error) {
	parsed, err := url.Parse(raw)
	if err != nil || !safePublicURL(parsed) || parsed.RawQuery != "" || parsed.Fragment != "" {
		return "", errors.New("identity has no permitted PDS endpoint")
	}
	return strings.TrimSuffix(parsed.String(), "/"), nil
}

func samePDSHost(left, right string) bool {
	canonical, err := canonicalPDSHost(left)
	return err == nil && canonical == right
}

func decodeProfile(output getRecordOutput, did string) (profile.Profile, error) {
	wantURI := "at://" + did + "/" + profileCollection + "/" + profileRKey
	uri, err := syntax.ParseATURI(output.URI)
	if err != nil || uri.String() != wantURI {
		return profile.Profile{}, errors.New("profile URI is not canonical")
	}
	if output.CID == nil {
		return profile.Profile{}, errors.New("profile CID is missing")
	}
	cid, err := syntax.ParseCID(*output.CID)
	if err != nil || cid.String() != *output.CID {
		return profile.Profile{}, errors.New("profile CID is not canonical")
	}
	if output.Value == nil {
		return profile.Profile{}, errors.New("profile record is missing")
	}
	var record profileRecord
	decoder := json.NewDecoder(bytes.NewReader(*output.Value))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&record); err != nil {
		return profile.Profile{}, fmt.Errorf("decode profile record: %w", err)
	}
	if err := ensureJSONEOF(decoder); err != nil {
		return profile.Profile{}, err
	}
	if record.Type != profileCollection {
		return profile.Profile{}, errors.New("profile record type is invalid")
	}
	createdAt, err := syntax.ParseDatetimeTime(record.CreatedAt)
	if err != nil || createdAt.UTC().Format(syntax.AtprotoDatetimeLayout) != record.CreatedAt {
		return profile.Profile{}, errors.New("profile createdAt is not canonical")
	}
	result := profile.Profile{
		DID: did, URI: output.URI, CID: *output.CID, DisplayName: record.DisplayName,
		Bio: record.Bio, Website: record.Website, Location: record.Location, RecordCreatedAt: createdAt,
	}
	if err := (profile.UpdateInput{DisplayName: result.DisplayName, Bio: result.Bio, Website: result.Website, Location: result.Location}).Validate(); err != nil {
		return profile.Profile{}, err
	}
	return result, nil
}

func ensureJSONEOF(decoder *json.Decoder) error {
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return errors.New("profile record contains trailing JSON")
	}
	return nil
}

func mapProfileAPIError(operation, did string, err error) error {
	var apiErr *atclient.APIError
	if errors.As(err, &apiErr) {
		if apiErr.StatusCode == http.StatusNotFound || apiErr.Name == "RecordNotFound" || apiErr.Name == "RepoNotFound" {
			return &profile.NotFoundError{DID: did}
		}
	}
	return &profile.ProviderError{Operation: operation, Err: err}
}

func rawMessage(value []byte) *json.RawMessage {
	raw := json.RawMessage(value)
	return &raw
}

func (client *Client) operationLock(did string) *sync.Mutex {
	lock, _ := client.operations.LoadOrStore(did, &sync.Mutex{})
	return lock.(*sync.Mutex)
}
