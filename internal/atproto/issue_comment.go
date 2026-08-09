package atproto

import (
	"context"
	"errors"

	"github.com/adenosine-dev/adenosine/internal/issue"
	"github.com/bluesky-social/indigo/atproto/auth/oauth"
	"github.com/bluesky-social/indigo/atproto/syntax"
)

type issueCommentStrongRef struct {
	URI string `json:"uri"`
	CID string `json:"cid"`
}

type issueCommentRecord struct {
	Type      string                 `json:"$type"`
	Subject   issueCommentStrongRef  `json:"subject"`
	Parent    *issueCommentStrongRef `json:"parent,omitempty"`
	Body      string                 `json:"body"`
	CreatedAt string                 `json:"createdAt"`
	UpdatedAt string                 `json:"updatedAt"`
}

type issueCommentDeleteRecordInput struct {
	Collection string `json:"collection"`
	Repo       string `json:"repo"`
	RKey       string `json:"rkey"`
	SwapRecord string `json:"swapRecord"`
}

// CreateIssueComment creates comment-author-owned content using a caller-retained retry-safe record key.
func (client *Client) CreateIssueComment(ctx context.Context, authorDID, rkey string, record issue.CommentRecord) (issue.Comment, error) {
	if err := issue.ValidateRecordKey(rkey); err != nil {
		return issue.Comment{}, err
	}
	if err := record.Validate(); err != nil {
		return issue.Comment{}, err
	}
	if record.Parent != nil && record.Parent.URI == "at://"+authorDID+"/"+issue.CommentCollection+"/"+rkey {
		return issue.Comment{}, &issue.ValidationError{Field: "parent.uri", Problem: "must differ from the comment URI"}
	}
	did, host, err := client.resolveIssueIdentity(ctx, authorDID)
	if err != nil {
		return issue.Comment{}, err
	}

	lock := client.operationLock(did.String())
	lock.Lock()
	defer lock.Unlock()
	session, err := client.resumeIssueSession(ctx, did, host)
	if err != nil {
		return issue.Comment{}, err
	}
	defer clearSession(session.Data)
	session.PersistSessionCallback = func(context.Context, *oauth.ClientSessionData) {}

	result, operationErr := client.createIssueCommentRecord(ctx, host, session, did.String(), rkey, record)
	persistenceErr := client.persistIssueSession(ctx, session, "comment credential persistence")
	if operationErr != nil || persistenceErr != nil {
		return issue.Comment{}, errors.Join(operationErr, persistenceErr)
	}
	return result, nil
}

func (client *Client) createIssueCommentRecord(ctx context.Context, host string, session *oauth.ClientSession, did, rkey string, record issue.CommentRecord) (issue.Comment, error) {
	createdAt, updatedAt, err := canonicalIssueTimes(record.CreatedAt, record.UpdatedAt)
	if err != nil {
		return issue.Comment{}, err
	}
	input := issuePutRecordInput{
		Collection: issue.CommentCollection, Repo: did, RKey: rkey, SwapRecord: nil,
		Record: map[string]any{
			"$type": issue.CommentCollection, "subject": strongRefMap(record.Subject), "body": record.Body,
			"createdAt": createdAt, "updatedAt": updatedAt,
		},
	}
	if record.Parent != nil {
		input.Record["parent"] = strongRefMap(*record.Parent)
	}
	api := client.apiFactory(host, session)
	var output putRecordOutput
	if err := api.Post(ctx, putRecordNSID, input, &output); err != nil {
		if !isInvalidSwap(err) {
			return issue.Comment{}, &issue.ProviderError{Operation: "create comment", Err: err}
		}
		existing, found, getErr := getIssueRecord(ctx, api, did, issue.CommentCollection, rkey)
		if getErr != nil {
			return issue.Comment{}, getErr
		}
		if !found {
			return issue.Comment{}, &issue.ConflictError{Err: errors.New("create-only comment disappeared")}
		}
		decoded, decodeErr := decodeIssueComment(existing, did, rkey)
		if decodeErr != nil || !sameIssueCommentRecord(decoded.CommentRecord, record) {
			return issue.Comment{}, &issue.ConflictError{Err: decodeErr}
		}
		return decoded, nil
	}

	result := issue.Comment{URI: output.URI, CID: output.CID, AuthorDID: did, CommentRecord: record}
	result.CreatedAt, _ = syntax.ParseDatetimeTime(createdAt)
	result.UpdatedAt, _ = syntax.ParseDatetimeTime(updatedAt)
	if err := validateIssueEnvelope(result.URI, result.CID, did, issue.CommentCollection, rkey); err != nil {
		return issue.Comment{}, &issue.ProviderError{Operation: "create comment response validation", Err: err}
	}
	if err := result.Validate(); err != nil {
		return issue.Comment{}, &issue.ProviderError{Operation: "create comment response validation", Err: err}
	}
	return result, nil
}

func decodeIssueComment(output getRecordOutput, did, rkey string) (issue.Comment, error) {
	if output.CID == nil || output.Value == nil {
		return issue.Comment{}, errors.New("issue comment envelope is incomplete")
	}
	if err := validateIssueEnvelope(output.URI, *output.CID, did, issue.CommentCollection, rkey); err != nil {
		return issue.Comment{}, err
	}
	var wire issueCommentRecord
	if err := decodeIssueJSON(*output.Value, &wire); err != nil {
		return issue.Comment{}, err
	}
	if wire.Type != issue.CommentCollection {
		return issue.Comment{}, errors.New("issue comment record type is invalid")
	}
	record := issue.CommentRecord{
		Subject: issue.StrongRef{URI: wire.Subject.URI, CID: wire.Subject.CID},
		Body:    wire.Body,
	}
	if wire.Parent != nil {
		record.Parent = &issue.StrongRef{URI: wire.Parent.URI, CID: wire.Parent.CID}
	}
	var err error
	record.CreatedAt, err = parseCanonicalIssueTime(wire.CreatedAt)
	if err == nil {
		record.UpdatedAt, err = parseCanonicalIssueTime(wire.UpdatedAt)
	}
	if err != nil {
		return issue.Comment{}, err
	}
	if err := record.Validate(); err != nil {
		return issue.Comment{}, err
	}
	result := issue.Comment{URI: output.URI, CID: *output.CID, AuthorDID: did, CommentRecord: record}
	if err := result.Validate(); err != nil {
		return issue.Comment{}, err
	}
	return result, nil
}

func sameIssueCommentRecord(left, right issue.CommentRecord) bool {
	return left.Subject == right.Subject && sameOptionalStrongRef(left.Parent, right.Parent) && left.Body == right.Body &&
		sameIssueTime(left.CreatedAt, right.CreatedAt) && sameIssueTime(left.UpdatedAt, right.UpdatedAt)
}

func sameOptionalStrongRef(left, right *issue.StrongRef) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return *left == *right
}

// DeleteIssueComment compare-and-swaps an authenticated author's comment identified by its AT URI.
func (client *Client) DeleteIssueComment(ctx context.Context, authorDID, commentURI string) error {
	did, err := syntax.ParseDID(authorDID)
	if err != nil || did.String() != authorDID {
		return &issue.ValidationError{Field: "authorDID", Problem: "must be a canonical AT Protocol DID"}
	}
	uri, err := syntax.ParseATURI(commentURI)
	if err != nil || uri.String() != commentURI || uri.Collection().String() != issue.CommentCollection || uri.RecordKey().String() == "" {
		return &issue.ValidationError{Field: "commentURI", Problem: "must be a canonical " + issue.CommentCollection + " AT URI"}
	}
	uriDID, err := uri.Authority().AsDID()
	if err != nil || uriDID.String() != uri.Authority().String() {
		return &issue.ValidationError{Field: "commentURI", Problem: "must use a canonical DID authority"}
	}
	if uriDID != did {
		return &issue.AuthorizationError{Err: errors.New("comment is not owned by authenticated author")}
	}
	if err := issue.ValidateRecordKey(uri.RecordKey().String()); err != nil {
		return err
	}
	_, host, err := client.resolveIssueIdentity(ctx, uriDID.String())
	if err != nil {
		return err
	}

	lock := client.operationLock(uriDID.String())
	lock.Lock()
	defer lock.Unlock()
	session, err := client.resumeIssueSession(ctx, uriDID, host)
	if err != nil {
		return err
	}
	defer clearSession(session.Data)
	session.PersistSessionCallback = func(context.Context, *oauth.ClientSessionData) {}

	operationErr := client.deleteIssueCommentRecord(ctx, host, session, uriDID.String(), uri.Collection().String(), uri.RecordKey().String())
	persistenceErr := client.persistIssueSession(ctx, session, "comment credential persistence")
	return errors.Join(operationErr, persistenceErr)
}

func (client *Client) deleteIssueCommentRecord(ctx context.Context, host string, session *oauth.ClientSession, did, collection, rkey string) error {
	api := client.apiFactory(host, session)
	existing, found, err := getIssueRecord(ctx, api, did, collection, rkey)
	if err != nil || !found {
		return err
	}
	decoded, err := decodeIssueComment(existing, did, rkey)
	if err != nil {
		return &issue.ConflictError{Err: err}
	}
	input := issueCommentDeleteRecordInput{Collection: collection, Repo: did, RKey: rkey, SwapRecord: decoded.CID}
	if err := api.Post(ctx, deleteRecordNSID, input, &struct{}{}); err != nil {
		if isRecordNotFound(err) {
			return nil
		}
		if !isInvalidSwap(err) {
			return &issue.ProviderError{Operation: "delete comment", Err: err}
		}
		current, currentFound, getErr := getIssueRecord(ctx, api, did, collection, rkey)
		if getErr != nil || !currentFound {
			return getErr
		}
		if _, decodeErr := decodeIssueComment(current, did, rkey); decodeErr != nil {
			return &issue.ConflictError{Err: decodeErr}
		}
		// The slot changed after the fetched CID; the concurrent version must survive.
		return nil
	}
	return nil
}
