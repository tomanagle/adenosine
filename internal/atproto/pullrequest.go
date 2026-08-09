package atproto

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"time"

	"github.com/adenosine-dev/adenosine/internal/pullrequest"
	"github.com/bluesky-social/indigo/atproto/auth/oauth"
	indigoidentity "github.com/bluesky-social/indigo/atproto/identity"
	"github.com/bluesky-social/indigo/atproto/syntax"
)

type pullRequestPutRecordInput struct {
	Collection string         `json:"collection"`
	Record     map[string]any `json:"record"`
	Repo       string         `json:"repo"`
	RKey       string         `json:"rkey"`
	SwapRecord *string        `json:"swapRecord"`
}

type pullRequestStrongRefWire struct {
	URI string `json:"uri"`
	CID string `json:"cid"`
}

type pullRequestWireRecord struct {
	Type             string                   `json:"$type"`
	SourceRepository pullRequestStrongRefWire `json:"sourceRepository"`
	TargetRepository pullRequestStrongRefWire `json:"targetRepository"`
	SourceBranch     string                   `json:"sourceBranch"`
	TargetBranch     string                   `json:"targetBranch"`
	HeadSHA          string                   `json:"headSHA"`
	Title            string                   `json:"title"`
	Body             string                   `json:"body"`
	CreatedAt        string                   `json:"createdAt"`
	UpdatedAt        string                   `json:"updatedAt"`
}

type pullRequestReviewWireRecord struct {
	Type      string                   `json:"$type"`
	Subject   pullRequestStrongRefWire `json:"subject"`
	Verdict   pullrequest.Verdict      `json:"verdict"`
	Body      string                   `json:"body"`
	CreatedAt string                   `json:"createdAt"`
	UpdatedAt string                   `json:"updatedAt"`
}

type pullRequestStatusWireRecord struct {
	Type             string                   `json:"$type"`
	Subject          pullRequestStrongRefWire `json:"subject"`
	TargetRepository pullRequestStrongRefWire `json:"targetRepository"`
	State            pullrequest.State        `json:"state"`
	MergeCommitSHA   string                   `json:"mergeCommitSHA,omitempty"`
	CreatedAt        string                   `json:"createdAt"`
	UpdatedAt        string                   `json:"updatedAt"`
}

// CreatePullRequest creates contributor-authored content using a retry-safe caller key.
func (client *Client) CreatePullRequest(ctx context.Context, authorDID, rkey string, record pullrequest.Record) (pullrequest.PullRequest, error) {
	if err := pullrequest.ValidateRecordKey(rkey); err != nil {
		return pullrequest.PullRequest{}, err
	}
	if err := record.Validate(); err != nil {
		return pullrequest.PullRequest{}, err
	}
	var result pullrequest.PullRequest
	err := client.withPullRequestSession(ctx, authorDID, func(host, did string, session *oauth.ClientSession) error {
		createdAt, updatedAt, err := canonicalPullRequestTimes(record.CreatedAt, record.UpdatedAt)
		if err != nil {
			return err
		}
		input := pullRequestPutRecordInput{Collection: pullrequest.Collection, Repo: did, RKey: rkey, Record: map[string]any{
			"$type": pullrequest.Collection, "sourceRepository": pullRequestStrongRef(record.SourceRepository), "targetRepository": pullRequestStrongRef(record.TargetRepository),
			"sourceBranch": record.SourceBranch, "targetBranch": record.TargetBranch, "headSHA": record.HeadSHA, "title": record.Title, "body": record.Body,
			"createdAt": createdAt, "updatedAt": updatedAt,
		}}
		api := client.apiFactory(host, session)
		var output putRecordOutput
		if err := api.Post(ctx, putRecordNSID, input, &output); err != nil {
			if !isInvalidSwap(err) {
				return &pullrequest.ProviderError{Operation: "create", Err: err}
			}
			existing, found, getErr := getPullRequestRecord(ctx, api, did, pullrequest.Collection, rkey)
			if getErr != nil {
				return getErr
			}
			if !found {
				return &pullrequest.ConflictError{Err: errors.New("create-only record disappeared")}
			}
			decoded, decodeErr := decodePullRequest(existing, did, rkey)
			if decodeErr != nil || !samePullRequestRecord(decoded.Record, record) {
				return &pullrequest.ConflictError{Err: decodeErr}
			}
			result = decoded
			return nil
		}
		result = pullrequest.PullRequest{URI: output.URI, CID: output.CID, AuthorDID: did, Record: record}
		result.CreatedAt, _ = syntax.ParseDatetimeTime(createdAt)
		result.UpdatedAt, _ = syntax.ParseDatetimeTime(updatedAt)
		if err := validatePullRequestEnvelope(result.URI, result.CID, did, pullrequest.Collection, rkey); err != nil {
			return &pullrequest.ProviderError{Operation: "create response validation", Err: err}
		}
		return nil
	})
	return result, err
}

// CreatePullRequestReview creates reviewer-owned feedback using a retry-safe caller key.
func (client *Client) CreatePullRequestReview(ctx context.Context, authorDID, rkey string, record pullrequest.ReviewRecord) (pullrequest.Review, error) {
	if err := pullrequest.ValidateRecordKey(rkey); err != nil {
		return pullrequest.Review{}, err
	}
	if err := record.Validate(); err != nil {
		return pullrequest.Review{}, err
	}
	var result pullrequest.Review
	err := client.withPullRequestSession(ctx, authorDID, func(host, did string, session *oauth.ClientSession) error {
		createdAt, updatedAt, err := canonicalPullRequestTimes(record.CreatedAt, record.UpdatedAt)
		if err != nil {
			return err
		}
		input := pullRequestPutRecordInput{Collection: pullrequest.ReviewCollection, Repo: did, RKey: rkey, Record: map[string]any{
			"$type": pullrequest.ReviewCollection, "subject": pullRequestStrongRef(record.Subject), "verdict": record.Verdict,
			"body": record.Body, "createdAt": createdAt, "updatedAt": updatedAt,
		}}
		api := client.apiFactory(host, session)
		var output putRecordOutput
		if err := api.Post(ctx, putRecordNSID, input, &output); err != nil {
			if !isInvalidSwap(err) {
				return &pullrequest.ProviderError{Operation: "create review", Err: err}
			}
			existing, found, getErr := getPullRequestRecord(ctx, api, did, pullrequest.ReviewCollection, rkey)
			if getErr != nil {
				return getErr
			}
			if !found {
				return &pullrequest.ConflictError{Err: errors.New("create-only review disappeared")}
			}
			decoded, decodeErr := decodePullRequestReview(existing, did, rkey)
			if decodeErr != nil || !samePullRequestReviewRecord(decoded.ReviewRecord, record) {
				return &pullrequest.ConflictError{Err: decodeErr}
			}
			result = decoded
			return nil
		}
		result = pullrequest.Review{URI: output.URI, CID: output.CID, AuthorDID: did, ReviewRecord: record}
		result.CreatedAt, _ = syntax.ParseDatetimeTime(createdAt)
		result.UpdatedAt, _ = syntax.ParseDatetimeTime(updatedAt)
		if err := validatePullRequestEnvelope(result.URI, result.CID, did, pullrequest.ReviewCollection, rkey); err != nil {
			return &pullrequest.ProviderError{Operation: "review response validation", Err: err}
		}
		return nil
	})
	return result, err
}

// PutPullRequestStatus creates or compare-and-swaps target-authoritative state.
func (client *Client) PutPullRequestStatus(ctx context.Context, authorDID string, record pullrequest.StatusRecord) (pullrequest.Status, error) {
	if err := record.Validate(); err != nil {
		return pullrequest.Status{}, err
	}
	owner, err := pullrequest.RepositoryOwnerDID(record.TargetRepository.URI)
	if err != nil {
		return pullrequest.Status{}, err
	}
	if authorDID != owner {
		return pullrequest.Status{}, &pullrequest.AuthorizationError{Err: errors.New("status author is not target repository owner")}
	}
	rkey, err := pullrequest.StatusRecordKey(record.Subject.URI)
	if err != nil {
		return pullrequest.Status{}, err
	}
	var result pullrequest.Status
	err = client.withPullRequestSession(ctx, authorDID, func(host, did string, session *oauth.ClientSession) error {
		api := client.apiFactory(host, session)
		existing, found, err := getPullRequestRecord(ctx, api, did, pullrequest.StatusCollection, rkey)
		if err != nil {
			return err
		}
		var swap *string
		if found {
			current, decodeErr := decodePullRequestStatus(existing, did, rkey)
			if decodeErr != nil {
				return &pullrequest.ConflictError{Err: decodeErr}
			}
			if current.Subject.URI != record.Subject.URI || current.TargetRepository.URI != record.TargetRepository.URI {
				return &pullrequest.ConflictError{Err: errors.New("status slot has incompatible identity")}
			}
			record.CreatedAt = current.CreatedAt
			if samePullRequestStatusRecord(current.StatusRecord, record) {
				result = current
				return nil
			}
			if !record.UpdatedAt.After(current.UpdatedAt) {
				return &pullrequest.ConflictError{Err: errors.New("status update is stale")}
			}
			swap = &current.CID
		}
		createdAt, updatedAt, err := canonicalPullRequestTimes(record.CreatedAt, record.UpdatedAt)
		if err != nil {
			return err
		}
		wire := map[string]any{"$type": pullrequest.StatusCollection, "subject": pullRequestStrongRef(record.Subject),
			"targetRepository": pullRequestStrongRef(record.TargetRepository), "state": record.State, "createdAt": createdAt, "updatedAt": updatedAt}
		if record.MergeCommitSHA != "" {
			wire["mergeCommitSHA"] = record.MergeCommitSHA
		}
		input := pullRequestPutRecordInput{Collection: pullrequest.StatusCollection, Repo: did, RKey: rkey, SwapRecord: swap, Record: wire}
		var output putRecordOutput
		if err := api.Post(ctx, putRecordNSID, input, &output); err != nil {
			if !isInvalidSwap(err) {
				return &pullrequest.ProviderError{Operation: "put status", Err: err}
			}
			latest, latestFound, getErr := getPullRequestRecord(ctx, api, did, pullrequest.StatusCollection, rkey)
			if getErr != nil {
				return getErr
			}
			if latestFound {
				current, decodeErr := decodePullRequestStatus(latest, did, rkey)
				if decodeErr == nil && samePullRequestStatusRecord(current.StatusRecord, record) {
					result = current
					return nil
				}
			}
			return &pullrequest.ConflictError{Err: errors.New("status changed concurrently")}
		}
		result = pullrequest.Status{URI: output.URI, CID: output.CID, AuthorDID: did, StatusRecord: record}
		result.CreatedAt, _ = syntax.ParseDatetimeTime(createdAt)
		result.UpdatedAt, _ = syntax.ParseDatetimeTime(updatedAt)
		if err := validatePullRequestEnvelope(result.URI, result.CID, did, pullrequest.StatusCollection, rkey); err != nil {
			return &pullrequest.ProviderError{Operation: "status response validation", Err: err}
		}
		return nil
	})
	return result, err
}

func (client *Client) withPullRequestSession(ctx context.Context, rawDID string, operation func(string, string, *oauth.ClientSession) error) error {
	did, host, err := client.resolvePullRequestIdentity(ctx, rawDID)
	if err != nil {
		return err
	}
	lock := client.operationLock(did.String())
	lock.Lock()
	defer lock.Unlock()
	session, err := client.resumePullRequestSession(ctx, did, host)
	if err != nil {
		return err
	}
	defer clearSession(session.Data)
	session.PersistSessionCallback = func(context.Context, *oauth.ClientSessionData) {}
	operationErr := operation(host, did.String(), session)
	persistErr := client.persistPullRequestSession(ctx, session)
	return errors.Join(operationErr, persistErr)
}

func getPullRequestRecord(ctx context.Context, api profileAPI, did, collection, rkey string) (getRecordOutput, bool, error) {
	var output getRecordOutput
	err := api.Get(ctx, getRecordNSID, map[string]any{"collection": collection, "repo": did, "rkey": rkey}, &output)
	if isRecordNotFound(err) {
		return getRecordOutput{}, false, nil
	}
	if err != nil {
		return getRecordOutput{}, false, &pullrequest.ProviderError{Operation: "get", Err: err}
	}
	return output, true, nil
}

func decodePullRequest(output getRecordOutput, did, rkey string) (pullrequest.PullRequest, error) {
	if output.CID == nil || output.Value == nil {
		return pullrequest.PullRequest{}, errors.New("pull request envelope is incomplete")
	}
	if err := validatePullRequestEnvelope(output.URI, *output.CID, did, pullrequest.Collection, rkey); err != nil {
		return pullrequest.PullRequest{}, err
	}
	var wire pullRequestWireRecord
	if err := decodePullRequestJSON(*output.Value, &wire); err != nil || wire.Type != pullrequest.Collection {
		return pullrequest.PullRequest{}, errors.New("pull request record is invalid")
	}
	record := pullrequest.Record{SourceRepository: pullrequest.StrongRef{URI: wire.SourceRepository.URI, CID: wire.SourceRepository.CID},
		TargetRepository: pullrequest.StrongRef{URI: wire.TargetRepository.URI, CID: wire.TargetRepository.CID}, SourceBranch: wire.SourceBranch,
		TargetBranch: wire.TargetBranch, HeadSHA: wire.HeadSHA, Title: wire.Title, Body: wire.Body}
	var err error
	record.CreatedAt, err = parseCanonicalPullRequestTime(wire.CreatedAt)
	if err == nil {
		record.UpdatedAt, err = parseCanonicalPullRequestTime(wire.UpdatedAt)
	}
	if err != nil {
		return pullrequest.PullRequest{}, err
	}
	if err := record.Validate(); err != nil {
		return pullrequest.PullRequest{}, err
	}
	return pullrequest.PullRequest{URI: output.URI, CID: *output.CID, AuthorDID: did, Record: record}, nil
}

func decodePullRequestReview(output getRecordOutput, did, rkey string) (pullrequest.Review, error) {
	if output.CID == nil || output.Value == nil {
		return pullrequest.Review{}, errors.New("pull request review envelope is incomplete")
	}
	if err := validatePullRequestEnvelope(output.URI, *output.CID, did, pullrequest.ReviewCollection, rkey); err != nil {
		return pullrequest.Review{}, err
	}
	var wire pullRequestReviewWireRecord
	if err := decodePullRequestJSON(*output.Value, &wire); err != nil || wire.Type != pullrequest.ReviewCollection {
		return pullrequest.Review{}, errors.New("pull request review record is invalid")
	}
	record := pullrequest.ReviewRecord{Subject: pullrequest.StrongRef{URI: wire.Subject.URI, CID: wire.Subject.CID}, Verdict: wire.Verdict, Body: wire.Body}
	var err error
	record.CreatedAt, err = parseCanonicalPullRequestTime(wire.CreatedAt)
	if err == nil {
		record.UpdatedAt, err = parseCanonicalPullRequestTime(wire.UpdatedAt)
	}
	if err != nil {
		return pullrequest.Review{}, err
	}
	if err := record.Validate(); err != nil {
		return pullrequest.Review{}, err
	}
	return pullrequest.Review{URI: output.URI, CID: *output.CID, AuthorDID: did, ReviewRecord: record}, nil
}

func decodePullRequestStatus(output getRecordOutput, did, rkey string) (pullrequest.Status, error) {
	if output.CID == nil || output.Value == nil {
		return pullrequest.Status{}, errors.New("pull request status envelope is incomplete")
	}
	if err := validatePullRequestEnvelope(output.URI, *output.CID, did, pullrequest.StatusCollection, rkey); err != nil {
		return pullrequest.Status{}, err
	}
	var wire pullRequestStatusWireRecord
	if err := decodePullRequestJSON(*output.Value, &wire); err != nil || wire.Type != pullrequest.StatusCollection {
		return pullrequest.Status{}, errors.New("pull request status record is invalid")
	}
	record := pullrequest.StatusRecord{Subject: pullrequest.StrongRef{URI: wire.Subject.URI, CID: wire.Subject.CID}, TargetRepository: pullrequest.StrongRef{URI: wire.TargetRepository.URI, CID: wire.TargetRepository.CID}, State: wire.State, MergeCommitSHA: wire.MergeCommitSHA}
	var err error
	record.CreatedAt, err = parseCanonicalPullRequestTime(wire.CreatedAt)
	if err == nil {
		record.UpdatedAt, err = parseCanonicalPullRequestTime(wire.UpdatedAt)
	}
	if err != nil {
		return pullrequest.Status{}, err
	}
	if err := record.Validate(); err != nil {
		return pullrequest.Status{}, err
	}
	owner, err := pullrequest.RepositoryOwnerDID(record.TargetRepository.URI)
	if err != nil || owner != did {
		return pullrequest.Status{}, errors.New("pull request status authority is invalid")
	}
	return pullrequest.Status{URI: output.URI, CID: *output.CID, AuthorDID: did, StatusRecord: record}, nil
}

func decodePullRequestJSON(value json.RawMessage, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(value))
	if err := decoder.Decode(target); err != nil {
		return err
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return errors.New("record contains trailing JSON")
	}
	return nil
}

func validatePullRequestEnvelope(uriValue, cidValue, did, collection, rkey string) error {
	want := "at://" + did + "/" + collection + "/" + rkey
	uri, err := syntax.ParseATURI(uriValue)
	if err != nil || uri.String() != uriValue || uriValue != want {
		return errors.New("pull request URI is not canonical")
	}
	if err := pullrequest.ValidateCID(cidValue); err != nil {
		return errors.New("pull request CID is not canonical")
	}
	return nil
}

func canonicalPullRequestTimes(createdAt, updatedAt time.Time) (string, string, error) {
	created, updated := createdAt.UTC().Format(syntax.AtprotoDatetimeLayout), updatedAt.UTC().Format(syntax.AtprotoDatetimeLayout)
	if _, err := parseCanonicalPullRequestTime(created); err != nil {
		return "", "", &pullrequest.ValidationError{Field: "createdAt", Problem: "must be an AT Protocol datetime"}
	}
	if _, err := parseCanonicalPullRequestTime(updated); err != nil {
		return "", "", &pullrequest.ValidationError{Field: "updatedAt", Problem: "must be an AT Protocol datetime"}
	}
	return created, updated, nil
}

func parseCanonicalPullRequestTime(value string) (time.Time, error) {
	parsed, err := syntax.ParseDatetimeTime(value)
	if err != nil || parsed.UTC().Format(syntax.AtprotoDatetimeLayout) != value {
		return time.Time{}, errors.New("pull request datetime is not canonical")
	}
	return parsed, nil
}

func pullRequestStrongRef(ref pullrequest.StrongRef) map[string]any {
	return map[string]any{"uri": ref.URI, "cid": ref.CID}
}
func samePullRequestTime(a, b time.Time) bool {
	return a.UTC().Format(syntax.AtprotoDatetimeLayout) == b.UTC().Format(syntax.AtprotoDatetimeLayout)
}
func samePullRequestRecord(a, b pullrequest.Record) bool {
	return a.SourceRepository == b.SourceRepository && a.TargetRepository == b.TargetRepository && a.SourceBranch == b.SourceBranch && a.TargetBranch == b.TargetBranch && a.HeadSHA == b.HeadSHA && a.Title == b.Title && a.Body == b.Body && samePullRequestTime(a.CreatedAt, b.CreatedAt) && samePullRequestTime(a.UpdatedAt, b.UpdatedAt)
}
func samePullRequestReviewRecord(a, b pullrequest.ReviewRecord) bool {
	return a.Subject == b.Subject && a.Verdict == b.Verdict && a.Body == b.Body && samePullRequestTime(a.CreatedAt, b.CreatedAt) && samePullRequestTime(a.UpdatedAt, b.UpdatedAt)
}
func samePullRequestStatusRecord(a, b pullrequest.StatusRecord) bool {
	return a.Subject == b.Subject && a.TargetRepository == b.TargetRepository && a.State == b.State && a.MergeCommitSHA == b.MergeCommitSHA && samePullRequestTime(a.CreatedAt, b.CreatedAt) && samePullRequestTime(a.UpdatedAt, b.UpdatedAt)
}

func (client *Client) resolvePullRequestIdentity(ctx context.Context, rawDID string) (syntax.DID, string, error) {
	did, err := syntax.ParseDID(rawDID)
	if err != nil || did.String() != rawDID {
		return "", "", &pullrequest.ValidationError{Field: "authorDID", Problem: "must be a valid canonical AT Protocol DID"}
	}
	identity, err := client.directory.LookupDID(ctx, did)
	if err != nil {
		return "", "", &pullrequest.ProviderError{Operation: "identity resolution", Err: err}
	}
	if identity == nil || identity.DID != did {
		return "", "", &pullrequest.ProviderError{Operation: "identity verification", Err: errors.New("resolved DID mismatch")}
	}
	host, err := pullRequestPDSHost(identity)
	if err != nil {
		return "", "", &pullrequest.ProviderError{Operation: "PDS verification", Err: err}
	}
	return did, host, nil
}
func pullRequestPDSHost(identity *indigoidentity.Identity) (string, error) {
	return canonicalPDSHost(identity.PDSEndpoint())
}
func (client *Client) resumePullRequestSession(ctx context.Context, did syntax.DID, host string) (*oauth.ClientSession, error) {
	latest, err := client.sessionStore.GetLatestSession(ctx, did)
	if err != nil {
		if errors.Is(err, ErrSessionNotFound) {
			return nil, &pullrequest.AuthorizationError{Err: err}
		}
		return nil, &pullrequest.ProviderError{Operation: "credential load", Err: err}
	}
	if latest == nil {
		return nil, &pullrequest.AuthorizationError{Err: ErrSessionNotFound}
	}
	sessionID := latest.SessionID
	clearSession(latest)
	session, err := client.resumeSession(ctx, did, sessionID)
	sessionID = ""
	if err != nil {
		if errors.Is(err, ErrSessionNotFound) {
			return nil, &pullrequest.AuthorizationError{Err: err}
		}
		return nil, &pullrequest.ProviderError{Operation: "credential resume", Err: err}
	}
	if session == nil || session.Data == nil {
		return nil, &pullrequest.ProviderError{Operation: "credential resume", Err: ErrSessionInvalid}
	}
	if session.Data.AccountDID != did || !samePDSHost(session.Data.HostURL, host) {
		clearSession(session.Data)
		return nil, &pullrequest.ProviderError{Operation: "credential verification", Err: ErrSessionInvalid}
	}
	return session, nil
}
func (client *Client) persistPullRequestSession(ctx context.Context, session *oauth.ClientSession) error {
	persistCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	if err := client.sessionStore.SaveSession(persistCtx, *session.Data); err != nil {
		return &pullrequest.ProviderError{Operation: "credential persistence", Err: err}
	}
	return nil
}
