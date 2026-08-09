package atproto

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"time"

	"github.com/adenosine-dev/adenosine/internal/issue"
	"github.com/bluesky-social/indigo/atproto/auth/oauth"
	indigoidentity "github.com/bluesky-social/indigo/atproto/identity"
	"github.com/bluesky-social/indigo/atproto/syntax"
)

type issuePutRecordInput struct {
	Collection string         `json:"collection"`
	Record     map[string]any `json:"record"`
	Repo       string         `json:"repo"`
	RKey       string         `json:"rkey"`
	SwapRecord *string        `json:"swapRecord"`
}

type issueRecord struct {
	Type       string `json:"$type"`
	Repository struct {
		URI string `json:"uri"`
		CID string `json:"cid"`
	} `json:"repository"`
	Title     string `json:"title"`
	Body      string `json:"body"`
	CreatedAt string `json:"createdAt"`
	UpdatedAt string `json:"updatedAt"`
}

type issueStatusRecord struct {
	Type    string `json:"$type"`
	Subject struct {
		URI string `json:"uri"`
		CID string `json:"cid"`
	} `json:"subject"`
	Repository struct {
		URI string `json:"uri"`
		CID string `json:"cid"`
	} `json:"repository"`
	State     issue.State `json:"state"`
	CreatedAt string      `json:"createdAt"`
	UpdatedAt string      `json:"updatedAt"`
}

// CreateIssue creates a reporter-authored issue using a caller-retained retry-safe record key.
func (client *Client) CreateIssue(ctx context.Context, authorDID, rkey string, record issue.Record) (issue.Issue, error) {
	if err := issue.ValidateRecordKey(rkey); err != nil {
		return issue.Issue{}, err
	}
	if err := record.Validate(); err != nil {
		return issue.Issue{}, err
	}
	did, host, err := client.resolveIssueIdentity(ctx, authorDID)
	if err != nil {
		return issue.Issue{}, err
	}

	lock := client.operationLock(did.String())
	lock.Lock()
	defer lock.Unlock()
	session, err := client.resumeIssueSession(ctx, did, host)
	if err != nil {
		return issue.Issue{}, err
	}
	defer clearSession(session.Data)
	session.PersistSessionCallback = func(context.Context, *oauth.ClientSessionData) {}

	result, operationErr := client.createIssueRecord(ctx, host, session, did.String(), rkey, record)
	persistenceErr := client.persistIssueSession(ctx, session, "credential persistence")
	if operationErr != nil || persistenceErr != nil {
		return issue.Issue{}, errors.Join(operationErr, persistenceErr)
	}
	return result, nil
}

func (client *Client) createIssueRecord(ctx context.Context, host string, session *oauth.ClientSession, did, rkey string, record issue.Record) (issue.Issue, error) {
	createdAt, updatedAt, err := canonicalIssueTimes(record.CreatedAt, record.UpdatedAt)
	if err != nil {
		return issue.Issue{}, err
	}
	input := issuePutRecordInput{
		Collection: issue.Collection, Repo: did, RKey: rkey, SwapRecord: nil,
		Record: map[string]any{
			"$type": issue.Collection, "repository": strongRefMap(record.Repository),
			"title": record.Title, "body": record.Body, "createdAt": createdAt, "updatedAt": updatedAt,
		},
	}
	api := client.apiFactory(host, session)
	var output putRecordOutput
	if err := api.Post(ctx, putRecordNSID, input, &output); err != nil {
		if !isInvalidSwap(err) {
			return issue.Issue{}, &issue.ProviderError{Operation: "create", Err: err}
		}
		existing, found, getErr := getIssueRecord(ctx, api, did, issue.Collection, rkey)
		if getErr != nil {
			return issue.Issue{}, getErr
		}
		if !found {
			return issue.Issue{}, &issue.ConflictError{Err: errors.New("create-only record disappeared")}
		}
		decoded, decodeErr := decodeIssue(existing, did, rkey)
		if decodeErr != nil || !sameIssueRecord(decoded.Record, record) {
			return issue.Issue{}, &issue.ConflictError{Err: decodeErr}
		}
		return decoded, nil
	}

	result := issue.Issue{URI: output.URI, CID: output.CID, AuthorDID: did, Record: record}
	result.CreatedAt, _ = syntax.ParseDatetimeTime(createdAt)
	result.UpdatedAt, _ = syntax.ParseDatetimeTime(updatedAt)
	if err := validateIssueEnvelope(result.URI, result.CID, did, issue.Collection, rkey); err != nil {
		return issue.Issue{}, &issue.ProviderError{Operation: "create response validation", Err: err}
	}
	return result, nil
}

// PutIssueStatus creates or compare-and-swaps repository-authoritative issue state.
func (client *Client) PutIssueStatus(ctx context.Context, authorDID string, record issue.StatusRecord) (issue.Status, error) {
	if err := record.Validate(); err != nil {
		return issue.Status{}, err
	}
	ownerDID, err := issue.RepositoryOwnerDID(record.Repository.URI)
	if err != nil {
		return issue.Status{}, err
	}
	// Authority is checked before identity resolution, credential loading, or any PDS request.
	if authorDID != ownerDID {
		return issue.Status{}, &issue.AuthorizationError{Err: errors.New("status author is not repository owner")}
	}
	rkey, err := issue.StatusRecordKey(record.Subject.URI)
	if err != nil {
		return issue.Status{}, err
	}
	did, host, err := client.resolveIssueIdentity(ctx, authorDID)
	if err != nil {
		return issue.Status{}, err
	}

	lock := client.operationLock(did.String())
	lock.Lock()
	defer lock.Unlock()
	session, err := client.resumeIssueSession(ctx, did, host)
	if err != nil {
		return issue.Status{}, err
	}
	defer clearSession(session.Data)
	session.PersistSessionCallback = func(context.Context, *oauth.ClientSessionData) {}

	result, operationErr := client.putIssueStatusRecord(ctx, host, session, did.String(), rkey, record)
	persistenceErr := client.persistIssueSession(ctx, session, "status credential persistence")
	if operationErr != nil || persistenceErr != nil {
		return issue.Status{}, errors.Join(operationErr, persistenceErr)
	}
	return result, nil
}

func (client *Client) putIssueStatusRecord(ctx context.Context, host string, session *oauth.ClientSession, did, rkey string, record issue.StatusRecord) (issue.Status, error) {
	api := client.apiFactory(host, session)
	existing, found, err := getIssueRecord(ctx, api, did, issue.StatusCollection, rkey)
	if err != nil {
		return issue.Status{}, err
	}
	var swapRecord *string
	if found {
		current, decodeErr := decodeIssueStatus(existing, did, rkey)
		if decodeErr != nil {
			return issue.Status{}, &issue.ConflictError{Err: decodeErr}
		}
		if current.Subject.URI != record.Subject.URI || current.Repository.URI != record.Repository.URI {
			return issue.Status{}, &issue.ConflictError{Err: errors.New("status slot has incompatible identity")}
		}
		// The PDS record is authoritative for immutable creation time; the local
		// projection may lag a just-created status during an immediate update.
		record.CreatedAt = current.CreatedAt
		if sameStatusRecord(current.StatusRecord, record) {
			return current, nil
		}
		if !record.UpdatedAt.After(current.UpdatedAt) {
			return issue.Status{}, &issue.ConflictError{Err: errors.New("status update is stale")}
		}
		swapRecord = &current.CID
	}
	createdAt, updatedAt, validationErr := canonicalIssueTimes(record.CreatedAt, record.UpdatedAt)
	if validationErr != nil {
		return issue.Status{}, validationErr
	}
	input := issuePutRecordInput{
		Collection: issue.StatusCollection, Repo: did, RKey: rkey, SwapRecord: swapRecord,
		Record: map[string]any{
			"$type": issue.StatusCollection, "subject": strongRefMap(record.Subject),
			"repository": strongRefMap(record.Repository), "state": record.State,
			"createdAt": createdAt, "updatedAt": updatedAt,
		},
	}
	var output putRecordOutput
	if err := api.Post(ctx, putRecordNSID, input, &output); err != nil {
		if !isInvalidSwap(err) {
			return issue.Status{}, &issue.ProviderError{Operation: "put status", Err: err}
		}
		currentOutput, currentFound, getErr := getIssueRecord(ctx, api, did, issue.StatusCollection, rkey)
		if getErr != nil {
			return issue.Status{}, getErr
		}
		if currentFound {
			current, decodeErr := decodeIssueStatus(currentOutput, did, rkey)
			if decodeErr == nil && sameStatusRecord(current.StatusRecord, record) {
				return current, nil
			}
		}
		return issue.Status{}, &issue.ConflictError{Err: errors.New("status changed concurrently")}
	}
	result := issue.Status{URI: output.URI, CID: output.CID, AuthorDID: did, StatusRecord: record}
	result.CreatedAt, _ = syntax.ParseDatetimeTime(createdAt)
	result.UpdatedAt, _ = syntax.ParseDatetimeTime(updatedAt)
	if err := validateIssueEnvelope(result.URI, result.CID, did, issue.StatusCollection, rkey); err != nil {
		return issue.Status{}, &issue.ProviderError{Operation: "status response validation", Err: err}
	}
	return result, nil
}

func getIssueRecord(ctx context.Context, api profileAPI, did, collection, rkey string) (getRecordOutput, bool, error) {
	var output getRecordOutput
	err := api.Get(ctx, getRecordNSID, map[string]any{"collection": collection, "repo": did, "rkey": rkey}, &output)
	if isRecordNotFound(err) {
		return getRecordOutput{}, false, nil
	}
	if err != nil {
		return getRecordOutput{}, false, &issue.ProviderError{Operation: "get", Err: err}
	}
	return output, true, nil
}

func decodeIssue(output getRecordOutput, did, rkey string) (issue.Issue, error) {
	if output.CID == nil || output.Value == nil {
		return issue.Issue{}, errors.New("issue envelope is incomplete")
	}
	if err := validateIssueEnvelope(output.URI, *output.CID, did, issue.Collection, rkey); err != nil {
		return issue.Issue{}, err
	}
	var wire issueRecord
	if err := decodeIssueJSON(*output.Value, &wire); err != nil {
		return issue.Issue{}, err
	}
	if wire.Type != issue.Collection {
		return issue.Issue{}, errors.New("issue record type is invalid")
	}
	record := issue.Record{
		Repository: issue.StrongRef{URI: wire.Repository.URI, CID: wire.Repository.CID},
		Title:      wire.Title, Body: wire.Body,
	}
	var err error
	record.CreatedAt, err = parseCanonicalIssueTime(wire.CreatedAt)
	if err == nil {
		record.UpdatedAt, err = parseCanonicalIssueTime(wire.UpdatedAt)
	}
	if err != nil {
		return issue.Issue{}, err
	}
	if err := record.Validate(); err != nil {
		return issue.Issue{}, err
	}
	return issue.Issue{URI: output.URI, CID: *output.CID, AuthorDID: did, Record: record}, nil
}

func decodeIssueStatus(output getRecordOutput, did, rkey string) (issue.Status, error) {
	if output.CID == nil || output.Value == nil {
		return issue.Status{}, errors.New("issue status envelope is incomplete")
	}
	if err := validateIssueEnvelope(output.URI, *output.CID, did, issue.StatusCollection, rkey); err != nil {
		return issue.Status{}, err
	}
	var wire issueStatusRecord
	if err := decodeIssueJSON(*output.Value, &wire); err != nil {
		return issue.Status{}, err
	}
	if wire.Type != issue.StatusCollection {
		return issue.Status{}, errors.New("issue status record type is invalid")
	}
	record := issue.StatusRecord{
		Subject:    issue.StrongRef{URI: wire.Subject.URI, CID: wire.Subject.CID},
		Repository: issue.StrongRef{URI: wire.Repository.URI, CID: wire.Repository.CID}, State: wire.State,
	}
	var err error
	record.CreatedAt, err = parseCanonicalIssueTime(wire.CreatedAt)
	if err == nil {
		record.UpdatedAt, err = parseCanonicalIssueTime(wire.UpdatedAt)
	}
	if err != nil {
		return issue.Status{}, err
	}
	if err := record.Validate(); err != nil {
		return issue.Status{}, err
	}
	owner, err := issue.RepositoryOwnerDID(record.Repository.URI)
	if err != nil || owner != did {
		return issue.Status{}, errors.New("issue status authority is invalid")
	}
	return issue.Status{URI: output.URI, CID: *output.CID, AuthorDID: did, StatusRecord: record}, nil
}

func decodeIssueJSON(value json.RawMessage, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(value))
	if err := decoder.Decode(target); err != nil {
		return errors.New("issue record is invalid")
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return errors.New("issue record contains trailing JSON")
	}
	return nil
}

func validateIssueEnvelope(uriValue, cidValue, did, collection, rkey string) error {
	wantURI := "at://" + did + "/" + collection + "/" + rkey
	uri, err := syntax.ParseATURI(uriValue)
	if err != nil || uri.String() != uriValue || uriValue != wantURI {
		return errors.New("issue URI is not canonical")
	}
	if err := issue.ValidateCID(cidValue); err != nil {
		return errors.New("issue CID is not canonical")
	}
	return nil
}

func canonicalIssueTimes(createdAt, updatedAt time.Time) (string, string, error) {
	created := createdAt.UTC().Format(syntax.AtprotoDatetimeLayout)
	updated := updatedAt.UTC().Format(syntax.AtprotoDatetimeLayout)
	if _, err := parseCanonicalIssueTime(created); err != nil {
		return "", "", &issue.ValidationError{Field: "createdAt", Problem: "must be an AT Protocol datetime"}
	}
	if _, err := parseCanonicalIssueTime(updated); err != nil {
		return "", "", &issue.ValidationError{Field: "updatedAt", Problem: "must be an AT Protocol datetime"}
	}
	return created, updated, nil
}

func parseCanonicalIssueTime(value string) (time.Time, error) {
	parsed, err := syntax.ParseDatetimeTime(value)
	if err != nil || parsed.UTC().Format(syntax.AtprotoDatetimeLayout) != value {
		return time.Time{}, errors.New("issue datetime is not canonical")
	}
	return parsed, nil
}

func strongRefMap(ref issue.StrongRef) map[string]any {
	return map[string]any{"uri": ref.URI, "cid": ref.CID}
}

func sameIssueRecord(left, right issue.Record) bool {
	return left.Repository == right.Repository && left.Title == right.Title && left.Body == right.Body &&
		sameIssueTime(left.CreatedAt, right.CreatedAt) && sameIssueTime(left.UpdatedAt, right.UpdatedAt)
}

func sameStatusRecord(left, right issue.StatusRecord) bool {
	return left.Subject == right.Subject && left.Repository == right.Repository && left.State == right.State &&
		sameIssueTime(left.CreatedAt, right.CreatedAt) && sameIssueTime(left.UpdatedAt, right.UpdatedAt)
}

func sameIssueTime(left, right time.Time) bool {
	return left.UTC().Format(syntax.AtprotoDatetimeLayout) == right.UTC().Format(syntax.AtprotoDatetimeLayout)
}

func (client *Client) resolveIssueIdentity(ctx context.Context, rawDID string) (syntax.DID, string, error) {
	did, err := syntax.ParseDID(rawDID)
	if err != nil || did.String() != rawDID {
		return "", "", &issue.ValidationError{Field: "authorDID", Problem: "must be a valid canonical AT Protocol DID"}
	}
	identity, err := client.directory.LookupDID(ctx, did)
	if err != nil {
		return "", "", &issue.ProviderError{Operation: "identity resolution", Err: err}
	}
	if identity == nil || identity.DID != did {
		return "", "", &issue.ProviderError{Operation: "identity verification", Err: errors.New("resolved DID mismatch")}
	}
	host, err := issuePDSHost(identity)
	if err != nil {
		return "", "", &issue.ProviderError{Operation: "PDS verification", Err: err}
	}
	return did, host, nil
}

func issuePDSHost(identity *indigoidentity.Identity) (string, error) {
	return canonicalPDSHost(identity.PDSEndpoint())
}

func (client *Client) resumeIssueSession(ctx context.Context, did syntax.DID, host string) (*oauth.ClientSession, error) {
	latest, err := client.sessionStore.GetLatestSession(ctx, did)
	if err != nil {
		if errors.Is(err, ErrSessionNotFound) {
			return nil, &issue.AuthorizationError{Err: err}
		}
		return nil, &issue.ProviderError{Operation: "credential load", Err: err}
	}
	if latest == nil {
		return nil, &issue.AuthorizationError{Err: ErrSessionNotFound}
	}
	sessionID := latest.SessionID
	clearSession(latest)
	session, err := client.resumeSession(ctx, did, sessionID)
	sessionID = ""
	if err != nil {
		if errors.Is(err, ErrSessionNotFound) {
			return nil, &issue.AuthorizationError{Err: err}
		}
		return nil, &issue.ProviderError{Operation: "credential resume", Err: err}
	}
	if session == nil || session.Data == nil {
		return nil, &issue.ProviderError{Operation: "credential resume", Err: ErrSessionInvalid}
	}
	if session.Data.AccountDID != did || !samePDSHost(session.Data.HostURL, host) {
		clearSession(session.Data)
		return nil, &issue.ProviderError{Operation: "credential verification", Err: ErrSessionInvalid}
	}
	return session, nil
}

func (client *Client) persistIssueSession(ctx context.Context, session *oauth.ClientSession, operation string) error {
	persistCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	if err := client.sessionStore.SaveSession(persistCtx, *session.Data); err != nil {
		return &issue.ProviderError{Operation: operation, Err: err}
	}
	return nil
}
