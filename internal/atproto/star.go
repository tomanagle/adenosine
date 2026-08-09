package atproto

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"time"

	"github.com/adenosine-dev/adenosine/internal/star"
	"github.com/bluesky-social/indigo/atproto/atclient"
	"github.com/bluesky-social/indigo/atproto/auth/oauth"
	indigoidentity "github.com/bluesky-social/indigo/atproto/identity"
	"github.com/bluesky-social/indigo/atproto/syntax"
)

var deleteRecordNSID = syntax.NSID("com.atproto.repo.deleteRecord")

type starPutRecordInput struct {
	Collection string         `json:"collection"`
	Record     map[string]any `json:"record"`
	Repo       string         `json:"repo"`
	RKey       string         `json:"rkey"`
	SwapRecord *string        `json:"swapRecord"`
}

type starDeleteRecordInput struct {
	Collection string `json:"collection"`
	Repo       string `json:"repo"`
	RKey       string `json:"rkey"`
	SwapRecord string `json:"swapRecord"`
}

type starRecord struct {
	Type    string `json:"$type"`
	Subject struct {
		URI string `json:"uri"`
		CID string `json:"cid"`
	} `json:"subject"`
	CreatedAt string `json:"createdAt"`
}

// CreateStar creates the authenticated DID's deterministic star for the current projected target.
func (client *Client) CreateStar(ctx context.Context, authorDID string, target star.Target, createdAt time.Time) (star.Star, error) {
	if err := target.Validate(); err != nil {
		return star.Star{}, err
	}
	if createdAt.IsZero() {
		return star.Star{}, &star.ValidationError{Field: "createdAt", Problem: "must not be empty"}
	}
	rkey, err := star.RecordKey(target.URI)
	if err != nil {
		return star.Star{}, err
	}
	did, _, host, err := client.resolveStarIdentity(ctx, authorDID)
	if err != nil {
		return star.Star{}, err
	}

	lock := client.operationLock(did.String())
	lock.Lock()
	defer lock.Unlock()
	session, err := client.resumeStarSession(ctx, did, host)
	if err != nil {
		return star.Star{}, err
	}
	defer clearSession(session.Data)
	session.PersistSessionCallback = func(context.Context, *oauth.ClientSessionData) {}

	result, operationErr := client.createStarRecord(ctx, host, session, did.String(), rkey, target, createdAt)
	persistCtx, cancelPersist := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	persistenceErr := client.sessionStore.SaveSession(persistCtx, *session.Data)
	cancelPersist()
	if persistenceErr != nil {
		persistenceErr = &star.ProviderError{Operation: "credential persistence", Err: persistenceErr}
	}
	if operationErr != nil || persistenceErr != nil {
		return star.Star{}, errors.Join(operationErr, persistenceErr)
	}
	return result, nil
}

func (client *Client) createStarRecord(ctx context.Context, host string, session *oauth.ClientSession, did, rkey string, target star.Target, createdAt time.Time) (star.Star, error) {
	createdAtValue := createdAt.UTC().Format(syntax.AtprotoDatetimeLayout)
	authoritativeCreatedAt, err := syntax.ParseDatetimeTime(createdAtValue)
	if err != nil {
		return star.Star{}, &star.ValidationError{Field: "createdAt", Problem: "must be an AT Protocol datetime"}
	}
	input := starPutRecordInput{
		Collection: star.Collection,
		Repo:       did,
		RKey:       rkey,
		SwapRecord: nil,
		Record: map[string]any{
			"$type":     star.Collection,
			"subject":   map[string]any{"uri": target.URI, "cid": target.CID},
			"createdAt": createdAtValue,
		},
	}
	api := client.apiFactory(host, session)
	var output putRecordOutput
	if err := api.Post(ctx, putRecordNSID, input, &output); err != nil {
		if !isInvalidSwap(err) {
			return star.Star{}, &star.ProviderError{Operation: "create", Err: err}
		}
		existing, found, getErr := getStarRecord(ctx, api, did, rkey)
		if getErr != nil {
			return star.Star{}, getErr
		}
		if !found {
			return star.Star{}, &star.ConflictError{Err: errors.New("deterministic record disappeared")}
		}
		decoded, decodeErr := decodeStar(existing, did, rkey)
		if decodeErr != nil {
			return star.Star{}, &star.ConflictError{Err: decodeErr}
		}
		if decoded.Target.URI != target.URI {
			return star.Star{}, &star.ConflictError{Err: errors.New("deterministic record has another target")}
		}
		return decoded, nil
	}

	result := star.Star{URI: output.URI, CID: output.CID, AuthorDID: did, Target: target, CreatedAt: authoritativeCreatedAt}
	if err := validateStarEnvelope(result, did, rkey); err != nil {
		return star.Star{}, &star.ProviderError{Operation: "create response validation", Err: err}
	}
	return result, nil
}

// DeleteStar compare-and-swaps the authenticated DID's star for the current projected target.
func (client *Client) DeleteStar(ctx context.Context, authorDID string, target star.Target) error {
	if err := target.Validate(); err != nil {
		return err
	}
	rkey, err := star.RecordKey(target.URI)
	if err != nil {
		return err
	}
	did, _, host, err := client.resolveStarIdentity(ctx, authorDID)
	if err != nil {
		return err
	}

	lock := client.operationLock(did.String())
	lock.Lock()
	defer lock.Unlock()
	session, err := client.resumeStarSession(ctx, did, host)
	if err != nil {
		return err
	}
	defer clearSession(session.Data)
	session.PersistSessionCallback = func(context.Context, *oauth.ClientSessionData) {}

	operationErr := client.deleteStarRecord(ctx, host, session, did.String(), rkey, target)
	persistCtx, cancelPersist := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	persistenceErr := client.sessionStore.SaveSession(persistCtx, *session.Data)
	cancelPersist()
	if persistenceErr != nil {
		persistenceErr = &star.ProviderError{Operation: "credential persistence", Err: persistenceErr}
	}
	return errors.Join(operationErr, persistenceErr)
}

func (client *Client) deleteStarRecord(ctx context.Context, host string, session *oauth.ClientSession, did, rkey string, target star.Target) error {
	api := client.apiFactory(host, session)
	existing, found, err := getStarRecord(ctx, api, did, rkey)
	if err != nil || !found {
		return err
	}
	decoded, err := decodeStar(existing, did, rkey)
	if err != nil {
		return &star.ConflictError{Err: err}
	}
	if decoded.Target.URI != target.URI {
		return &star.ConflictError{Err: errors.New("deterministic record has another target")}
	}

	input := starDeleteRecordInput{Collection: star.Collection, Repo: did, RKey: rkey, SwapRecord: decoded.CID}
	if err := api.Post(ctx, deleteRecordNSID, input, &struct{}{}); err != nil {
		if isRecordNotFound(err) {
			return nil
		}
		if !isInvalidSwap(err) {
			return &star.ProviderError{Operation: "delete", Err: err}
		}
		current, currentFound, getErr := getStarRecord(ctx, api, did, rkey)
		if getErr != nil || !currentFound {
			return getErr
		}
		currentStar, decodeErr := decodeStar(current, did, rkey)
		if decodeErr != nil {
			return &star.ConflictError{Err: decodeErr}
		}
		if currentStar.Target.URI != target.URI {
			return &star.ConflictError{Err: errors.New("deterministic record has another target")}
		}
		// A changed same-target record is a concurrent re-star and must survive this delete.
		return nil
	}
	return nil
}

func getStarRecord(ctx context.Context, api profileAPI, did, rkey string) (getRecordOutput, bool, error) {
	var output getRecordOutput
	err := api.Get(ctx, getRecordNSID, map[string]any{"collection": star.Collection, "repo": did, "rkey": rkey}, &output)
	if isRecordNotFound(err) {
		return getRecordOutput{}, false, nil
	}
	if err != nil {
		return getRecordOutput{}, false, &star.ProviderError{Operation: "get", Err: err}
	}
	return output, true, nil
}

func decodeStar(output getRecordOutput, did, rkey string) (star.Star, error) {
	if output.Value == nil {
		return star.Star{}, errors.New("star record is missing")
	}
	result := star.Star{URI: output.URI, AuthorDID: did}
	if output.CID != nil {
		result.CID = *output.CID
	}
	if err := validateStarEnvelope(result, did, rkey); err != nil {
		return star.Star{}, err
	}
	var record starRecord
	decoder := json.NewDecoder(bytes.NewReader(*output.Value))
	if err := decoder.Decode(&record); err != nil {
		return star.Star{}, errors.New("star record is invalid")
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return star.Star{}, errors.New("star record contains trailing JSON")
	}
	if record.Type != star.Collection {
		return star.Star{}, errors.New("star record type is invalid")
	}
	result.Target = star.Target{URI: record.Subject.URI, CID: record.Subject.CID}
	if err := result.Target.Validate(); err != nil {
		return star.Star{}, err
	}
	createdAt, err := syntax.ParseDatetimeTime(record.CreatedAt)
	if err != nil || createdAt.UTC().Format(syntax.AtprotoDatetimeLayout) != record.CreatedAt {
		return star.Star{}, errors.New("star createdAt is not canonical")
	}
	result.CreatedAt = createdAt
	return result, nil
}

func validateStarEnvelope(value star.Star, did, rkey string) error {
	wantURI := "at://" + did + "/" + star.Collection + "/" + rkey
	uri, err := syntax.ParseATURI(value.URI)
	if err != nil || uri.String() != value.URI || value.URI != wantURI {
		return errors.New("star URI is not canonical")
	}
	if err := star.ValidateCID(value.CID); err != nil {
		return errors.New("star CID is not canonical")
	}
	return nil
}

func (client *Client) resolveStarIdentity(ctx context.Context, rawDID string) (syntax.DID, *indigoidentity.Identity, string, error) {
	did, err := syntax.ParseDID(rawDID)
	if err != nil || did.String() != rawDID {
		return "", nil, "", &star.ValidationError{Field: "authorDID", Problem: "must be a valid canonical AT Protocol DID"}
	}
	identity, err := client.directory.LookupDID(ctx, did)
	if err != nil {
		return "", nil, "", &star.ProviderError{Operation: "identity resolution", Err: err}
	}
	if identity == nil || identity.DID != did {
		return "", nil, "", &star.ProviderError{Operation: "identity verification", Err: errors.New("resolved DID mismatch")}
	}
	host, err := canonicalPDSHost(identity.PDSEndpoint())
	if err != nil {
		return "", nil, "", &star.ProviderError{Operation: "PDS verification", Err: err}
	}
	return did, identity, host, nil
}

func (client *Client) resumeStarSession(ctx context.Context, did syntax.DID, host string) (*oauth.ClientSession, error) {
	latest, err := client.sessionStore.GetLatestSession(ctx, did)
	if err != nil {
		if errors.Is(err, ErrSessionNotFound) {
			return nil, &star.AuthorizationError{Err: err}
		}
		return nil, &star.ProviderError{Operation: "credential load", Err: err}
	}
	if latest == nil {
		return nil, &star.AuthorizationError{Err: ErrSessionNotFound}
	}
	sessionID := latest.SessionID
	clearSession(latest)
	session, err := client.resumeSession(ctx, did, sessionID)
	sessionID = ""
	if err != nil {
		if errors.Is(err, ErrSessionNotFound) {
			return nil, &star.AuthorizationError{Err: err}
		}
		return nil, &star.ProviderError{Operation: "credential resume", Err: err}
	}
	if session == nil || session.Data == nil {
		return nil, &star.ProviderError{Operation: "credential resume", Err: ErrSessionInvalid}
	}
	if session.Data.AccountDID != did || !samePDSHost(session.Data.HostURL, host) {
		clearSession(session.Data)
		return nil, &star.ProviderError{Operation: "credential verification", Err: ErrSessionInvalid}
	}
	return session, nil
}

func isRecordNotFound(err error) bool {
	if err == nil {
		return false
	}
	var apiErr *atclient.APIError
	return errors.As(err, &apiErr) && (apiErr.StatusCode == http.StatusNotFound || apiErr.Name == "RecordNotFound" || apiErr.Name == "RepoNotFound")
}

func isInvalidSwap(err error) bool {
	var apiErr *atclient.APIError
	return errors.As(err, &apiErr) && apiErr.Name == "InvalidSwap"
}
