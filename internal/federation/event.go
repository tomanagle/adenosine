// Package federation validates and projects trusted Tap synchronization output.
package federation

import (
	"bytes"
	"crypto/sha256"
	"encoding/base32"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/adenosine-dev/adenosine/internal/issue"
	"github.com/adenosine-dev/adenosine/internal/profile"
	"github.com/adenosine-dev/adenosine/internal/pullrequest"
	"github.com/adenosine-dev/adenosine/internal/star"
	"github.com/bluesky-social/indigo/atproto/syntax"
)

const (
	ProfileCollection                = "dev.adenosine.profile"
	RepositoryCollection             = "dev.adenosine.repo"
	StarCollection                   = star.Collection
	IssueCollection                  = issue.Collection
	IssueStatusCollection            = issue.StatusCollection
	PullRequestCollection            = pullrequest.Collection
	PullRequestStatusCollection      = pullrequest.StatusCollection
	PullRequestReviewCollection      = pullrequest.ReviewCollection
	OrganizationCollection           = "dev.adenosine.organization"
	OrganizationGrantCollection      = "dev.adenosine.organizationGrant"
	OrganizationMembershipCollection = "dev.adenosine.organizationMembership"
	OrganizationRevocationCollection = "dev.adenosine.organizationRevocation"
	maxEventBytes                    = 1 << 20
)

var (
	slugPattern             = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]*$`)
	organizationSlugPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`)
	starRKeyPattern         = regexp.MustCompile(`^[a-z2-7]{52}$`)
)

// ErrInvalidEvent identifies untrusted input which should be acknowledged and discarded.
var ErrInvalidEvent = errors.New("invalid federation event")

// Event is a validated Tap event.
type Event struct {
	ID       int64
	Type     string
	Record   *RecordEvent
	Identity *IdentityEvent
}

// RecordEvent is a validated Tap record mutation.
type RecordEvent struct {
	DID                    string
	Collection             string
	RKey                   string
	URI                    string
	Action                 string
	CID                    string
	Raw                    []byte
	Profile                *ProfileRecord
	Repository             *RepositoryRecord
	Star                   *StarRecord
	Issue                  *issue.Record
	IssueComment           *issue.CommentRecord
	IssueStatus            *issue.StatusRecord
	PullRequest            *pullrequest.Record
	PullRequestStatus      *pullrequest.StatusRecord
	PullRequestReview      *pullrequest.ReviewRecord
	Organization           *OrganizationRecord
	OrganizationGrant      *OrganizationGrantRecord
	OrganizationMembership *OrganizationMembershipRecord
	OrganizationRevocation *OrganizationRevocationRecord
}

// IdentityEvent is a validated Tap identity projection.
type IdentityEvent struct {
	DID      string
	Handle   string
	Status   string
	IsActive bool
}

// ProfileRecord is the portable profile projection.
type ProfileRecord struct {
	DisplayName string
	Bio         string
	Website     string
	Location    string
	CreatedAt   time.Time
}

// RepositoryRecord is the portable repository projection.
type RepositoryRecord struct {
	Slug          string
	Name          string
	Description   string
	DefaultBranch string
	GitHTTPS      string
	GitSSH        string
	Web           string
	CreatedAt     time.Time
	UpdatedAt     time.Time
	Organization  *StrongRef
}

// StarRecord is the decoded subset needed by the federation projection.
type StarRecord struct {
	RepositoryURI string
	RepositoryCID string
	CreatedAt     time.Time
}

type StrongRef struct {
	URI string
	CID string
}
type OrganizationRecord struct {
	Slug, Name, Description, Website, Location string
	CreatedAt, UpdatedAt                       time.Time
}
type OrganizationGrantRecord struct {
	Organization  StrongRef
	Subject, Role string
	Authority     StrongRef
	CreatedAt     time.Time
	ExpiresAt     *time.Time
}
type OrganizationMembershipRecord struct {
	Organization, Grant  StrongRef
	Visibility           string
	CreatedAt, UpdatedAt time.Time
}
type OrganizationRevocationRecord struct {
	Organization, Grant StrongRef
	Subject             string
	Authority           StrongRef
	CreatedAt           time.Time
}

type envelope struct {
	ID       int64           `json:"id"`
	Type     string          `json:"type"`
	Record   json.RawMessage `json:"record,omitempty"`
	Identity json.RawMessage `json:"identity,omitempty"`
}

type tapRecord struct {
	Live       *bool           `json:"live"`
	Rev        string          `json:"rev"`
	DID        string          `json:"did"`
	Collection string          `json:"collection"`
	RKey       string          `json:"rkey"`
	Action     string          `json:"action"`
	CID        string          `json:"cid,omitempty"`
	Record     json.RawMessage `json:"record,omitempty"`
}

type tapIdentity struct {
	DID      string `json:"did"`
	Handle   string `json:"handle,omitempty"`
	Status   string `json:"status"`
	IsActive *bool  `json:"is_active"`
}

// EventID extracts a usable receipt key even when the remaining envelope is invalid.
func EventID(body []byte) (int64, bool) {
	if len(body) == 0 || len(body) > maxEventBytes {
		return 0, false
	}
	var value struct {
		ID int64 `json:"id"`
	}
	if err := json.Unmarshal(body, &value); err != nil || value.ID <= 0 {
		return 0, false
	}
	return value.ID, true
}

// DecodeEvent decodes the bounded, strict Tap envelope and validates its payload.
func DecodeEvent(body []byte) (Event, error) {
	if len(body) == 0 || len(body) > maxEventBytes {
		return Event{}, invalid("event envelope exceeds the permitted size")
	}
	var wire envelope
	if err := decodeStrict(body, &wire); err != nil {
		return Event{}, invalid("decode envelope: %v", err)
	}
	if wire.ID <= 0 {
		return Event{}, invalid("event id must be positive")
	}
	event := Event{ID: wire.ID, Type: wire.Type}
	switch wire.Type {
	case "record":
		if len(wire.Record) == 0 || len(wire.Identity) != 0 {
			return Event{}, invalid("record event must contain only a record payload")
		}
		record, err := decodeRecordEvent(wire.Record)
		if err != nil {
			return Event{}, err
		}
		event.Record = &record
	case "identity":
		if len(wire.Identity) == 0 || len(wire.Record) != 0 {
			return Event{}, invalid("identity event must contain only an identity payload")
		}
		identity, err := decodeIdentityEvent(wire.Identity)
		if err != nil {
			return Event{}, err
		}
		event.Identity = &identity
	default:
		return Event{}, invalid("unsupported event type %q", wire.Type)
	}
	return event, nil
}

func decodeRecordEvent(raw []byte) (RecordEvent, error) {
	var wire tapRecord
	if err := decodeStrict(raw, &wire); err != nil {
		return RecordEvent{}, invalid("decode record event: %v", err)
	}
	if wire.Live == nil {
		return RecordEvent{}, invalid("record live flag is required")
	}
	rev, err := syntax.ParseRecordKey(wire.Rev)
	if err != nil || rev.String() != wire.Rev {
		return RecordEvent{}, invalid("record rev is not canonical")
	}
	if err := canonicalDID(wire.DID); err != nil {
		return RecordEvent{}, err
	}
	collection, err := syntax.ParseNSID(wire.Collection)
	if err != nil || collection.String() != wire.Collection {
		return RecordEvent{}, invalid("collection is not a canonical NSID")
	}
	if wire.Collection != ProfileCollection && wire.Collection != RepositoryCollection && wire.Collection != StarCollection && wire.Collection != IssueCollection && wire.Collection != issue.CommentCollection && wire.Collection != IssueStatusCollection && wire.Collection != PullRequestCollection && wire.Collection != PullRequestStatusCollection && wire.Collection != PullRequestReviewCollection && wire.Collection != OrganizationCollection && wire.Collection != OrganizationGrantCollection && wire.Collection != OrganizationMembershipCollection && wire.Collection != OrganizationRevocationCollection {
		return RecordEvent{}, invalid("unsupported collection %q", wire.Collection)
	}
	rkey, err := syntax.ParseRecordKey(wire.RKey)
	if err != nil || rkey.String() != wire.RKey {
		return RecordEvent{}, invalid("rkey is not canonical")
	}
	result := RecordEvent{
		DID: wire.DID, Collection: wire.Collection, RKey: wire.RKey,
		URI: "at://" + wire.DID + "/" + wire.Collection + "/" + wire.RKey, Action: wire.Action,
	}
	uri, err := syntax.ParseATURI(result.URI)
	if err != nil || uri.String() != result.URI {
		return RecordEvent{}, invalid("record URI is not canonical")
	}
	if wire.Collection == ProfileCollection && wire.RKey != "self" {
		return RecordEvent{}, invalid("profile rkey must be self")
	}
	if wire.Collection == StarCollection && !starRKeyPattern.MatchString(wire.RKey) {
		return RecordEvent{}, invalid("star rkey is not deterministic key shaped")
	}
	if wire.Collection == IssueCollection {
		if err := issue.ValidateRecordKey(wire.RKey); err != nil {
			return RecordEvent{}, invalid("issue rkey: %v", err)
		}
	}
	if wire.Collection == issue.CommentCollection {
		if err := issue.ValidateRecordKey(wire.RKey); err != nil {
			return RecordEvent{}, invalid("issue comment rkey: %v", err)
		}
	}
	if wire.Collection == IssueStatusCollection && !starRKeyPattern.MatchString(wire.RKey) {
		return RecordEvent{}, invalid("issue status rkey is not deterministic key shaped")
	}
	if wire.Collection == PullRequestStatusCollection && !starRKeyPattern.MatchString(wire.RKey) {
		return RecordEvent{}, invalid("pull request status rkey is not deterministic key shaped")
	}
	if wire.Collection == PullRequestCollection || wire.Collection == PullRequestReviewCollection {
		if err := pullrequest.ValidateRecordKey(wire.RKey); err != nil {
			return RecordEvent{}, invalid("pull request record rkey: %v", err)
		}
	}
	if wire.Collection == OrganizationMembershipCollection && !starRKeyPattern.MatchString(wire.RKey) {
		return RecordEvent{}, invalid("organization membership rkey is not deterministic key shaped")
	}
	switch wire.Action {
	case "delete":
		if wire.CID != "" || len(wire.Record) != 0 {
			return RecordEvent{}, invalid("delete event must not contain cid or record")
		}
		return result, nil
	case "create", "update":
	default:
		return RecordEvent{}, invalid("unsupported record action %q", wire.Action)
	}
	cid, err := syntax.ParseCID(wire.CID)
	if err != nil || cid.String() != wire.CID {
		return RecordEvent{}, invalid("cid is not canonical")
	}
	if len(wire.Record) == 0 || bytes.Equal(wire.Record, []byte("null")) {
		return RecordEvent{}, invalid("record value is required")
	}
	result.CID = wire.CID
	result.Raw = append([]byte(nil), wire.Record...)
	if wire.Collection == ProfileCollection {
		value, err := decodeProfileRecord(wire.Record)
		if err != nil {
			return RecordEvent{}, err
		}
		result.Profile = &value
	} else if wire.Collection == RepositoryCollection {
		value, err := decodeRepositoryRecord(wire.Record)
		if err != nil {
			return RecordEvent{}, err
		}
		result.Repository = &value
	} else if wire.Collection == StarCollection {
		value, err := decodeStarRecord(wire.Record)
		if err != nil {
			return RecordEvent{}, err
		}
		expectedRKey, err := star.RecordKey(value.RepositoryURI)
		if err != nil || wire.RKey != expectedRKey {
			return RecordEvent{}, invalid("star rkey does not match repository URI")
		}
		result.Star = &value
	} else if wire.Collection == IssueCollection {
		value, err := decodeIssueRecord(wire.Record)
		if err != nil {
			return RecordEvent{}, err
		}
		result.Issue = &value
	} else if wire.Collection == issue.CommentCollection {
		value, err := decodeIssueCommentRecord(wire.Record)
		if err != nil {
			return RecordEvent{}, err
		}
		if err := (issue.Comment{URI: result.URI, CID: result.CID, AuthorDID: result.DID, CommentRecord: value}).Validate(); err != nil {
			return RecordEvent{}, invalid("issue comment: %v", err)
		}
		result.IssueComment = &value
	} else if wire.Collection == IssueStatusCollection {
		value, err := decodeIssueStatusRecord(wire.Record)
		if err != nil {
			return RecordEvent{}, err
		}
		expectedRKey, err := issue.StatusRecordKey(value.Subject.URI)
		if err != nil || wire.RKey != expectedRKey {
			return RecordEvent{}, invalid("issue status rkey does not match issue URI")
		}
		result.IssueStatus = &value
	} else if wire.Collection == PullRequestCollection {
		value, err := decodePullRequestRecord(wire.Record)
		if err != nil {
			return RecordEvent{}, err
		}
		if err := (pullrequest.PullRequest{URI: result.URI, CID: result.CID, AuthorDID: result.DID, Record: value}).Validate(); err != nil {
			return RecordEvent{}, invalid("pull request: %v", err)
		}
		result.PullRequest = &value
	} else if wire.Collection == PullRequestStatusCollection {
		value, err := decodePullRequestStatusRecord(wire.Record)
		if err != nil {
			return RecordEvent{}, err
		}
		expectedRKey, err := pullrequest.StatusRecordKey(value.Subject.URI)
		if err != nil || wire.RKey != expectedRKey {
			return RecordEvent{}, invalid("pull request status rkey does not match pull request URI")
		}
		result.PullRequestStatus = &value
	} else if wire.Collection == PullRequestReviewCollection {
		value, err := decodePullRequestReviewRecord(wire.Record)
		if err != nil {
			return RecordEvent{}, err
		}
		if err := (pullrequest.Review{URI: result.URI, CID: result.CID, AuthorDID: result.DID, ReviewRecord: value}).Validate(); err != nil {
			return RecordEvent{}, invalid("pull request review: %v", err)
		}
		result.PullRequestReview = &value
	} else if wire.Collection == OrganizationCollection {
		value, err := decodeOrganizationRecord(wire.Record)
		if err != nil {
			return RecordEvent{}, err
		}
		result.Organization = &value
	} else if wire.Collection == OrganizationGrantCollection {
		value, err := decodeOrganizationGrantRecord(wire.Record)
		if err != nil {
			return RecordEvent{}, err
		}
		result.OrganizationGrant = &value
	} else if wire.Collection == OrganizationMembershipCollection {
		value, err := decodeOrganizationMembershipRecord(wire.Record)
		if err != nil {
			return RecordEvent{}, err
		}
		digest := sha256.Sum256([]byte(value.Organization.URI))
		expected := strings.ToLower(base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(digest[:]))
		if wire.RKey != expected {
			return RecordEvent{}, invalid("organization membership rkey does not match organization URI")
		}
		result.OrganizationMembership = &value
	} else {
		value, err := decodeOrganizationRevocationRecord(wire.Record)
		if err != nil {
			return RecordEvent{}, err
		}
		result.OrganizationRevocation = &value
	}
	return result, nil
}

func decodePullRequestRecord(raw []byte) (pullrequest.Record, error) {
	var wire struct {
		Type             string                `json:"$type"`
		SourceRepository pullrequest.StrongRef `json:"sourceRepository"`
		TargetRepository pullrequest.StrongRef `json:"targetRepository"`
		SourceBranch     string                `json:"sourceBranch"`
		TargetBranch     string                `json:"targetBranch"`
		HeadSHA          string                `json:"headSHA"`
		Title            string                `json:"title"`
		Body             string                `json:"body"`
		CreatedAt        string                `json:"createdAt"`
		UpdatedAt        string                `json:"updatedAt"`
	}
	if err := decodeStrict(raw, &wire); err != nil {
		return pullrequest.Record{}, invalid("decode pull request record: %v", err)
	}
	if wire.Type != PullRequestCollection {
		return pullrequest.Record{}, invalid("pull request record type is invalid")
	}
	createdAt, err := canonicalDatetime(wire.CreatedAt)
	if err != nil {
		return pullrequest.Record{}, err
	}
	updatedAt, err := canonicalDatetime(wire.UpdatedAt)
	if err != nil {
		return pullrequest.Record{}, err
	}
	value := pullrequest.Record{
		SourceRepository: wire.SourceRepository, TargetRepository: wire.TargetRepository,
		SourceBranch: wire.SourceBranch, TargetBranch: wire.TargetBranch, HeadSHA: wire.HeadSHA,
		Title: wire.Title, Body: wire.Body, CreatedAt: createdAt, UpdatedAt: updatedAt,
	}
	if err := value.Validate(); err != nil {
		return pullrequest.Record{}, invalid("pull request record: %v", err)
	}
	return value, nil
}

func decodePullRequestStatusRecord(raw []byte) (pullrequest.StatusRecord, error) {
	var wire struct {
		Type             string                `json:"$type"`
		Subject          pullrequest.StrongRef `json:"subject"`
		TargetRepository pullrequest.StrongRef `json:"targetRepository"`
		State            pullrequest.State     `json:"state"`
		MergeCommitSHA   string                `json:"mergeCommitSHA,omitempty"`
		CreatedAt        string                `json:"createdAt"`
		UpdatedAt        string                `json:"updatedAt"`
	}
	if err := decodeStrict(raw, &wire); err != nil {
		return pullrequest.StatusRecord{}, invalid("decode pull request status record: %v", err)
	}
	if wire.Type != PullRequestStatusCollection {
		return pullrequest.StatusRecord{}, invalid("pull request status record type is invalid")
	}
	createdAt, err := canonicalDatetime(wire.CreatedAt)
	if err != nil {
		return pullrequest.StatusRecord{}, err
	}
	updatedAt, err := canonicalDatetime(wire.UpdatedAt)
	if err != nil {
		return pullrequest.StatusRecord{}, err
	}
	value := pullrequest.StatusRecord{
		Subject: wire.Subject, TargetRepository: wire.TargetRepository, State: wire.State,
		MergeCommitSHA: wire.MergeCommitSHA, CreatedAt: createdAt, UpdatedAt: updatedAt,
	}
	if err := value.Validate(); err != nil {
		return pullrequest.StatusRecord{}, invalid("pull request status record: %v", err)
	}
	return value, nil
}

func decodePullRequestReviewRecord(raw []byte) (pullrequest.ReviewRecord, error) {
	var wire struct {
		Type      string                `json:"$type"`
		Subject   pullrequest.StrongRef `json:"subject"`
		Verdict   pullrequest.Verdict   `json:"verdict"`
		Body      string                `json:"body"`
		CreatedAt string                `json:"createdAt"`
		UpdatedAt string                `json:"updatedAt"`
	}
	if err := decodeStrict(raw, &wire); err != nil {
		return pullrequest.ReviewRecord{}, invalid("decode pull request review record: %v", err)
	}
	if wire.Type != PullRequestReviewCollection {
		return pullrequest.ReviewRecord{}, invalid("pull request review record type is invalid")
	}
	createdAt, err := canonicalDatetime(wire.CreatedAt)
	if err != nil {
		return pullrequest.ReviewRecord{}, err
	}
	updatedAt, err := canonicalDatetime(wire.UpdatedAt)
	if err != nil {
		return pullrequest.ReviewRecord{}, err
	}
	value := pullrequest.ReviewRecord{Subject: wire.Subject, Verdict: wire.Verdict, Body: wire.Body, CreatedAt: createdAt, UpdatedAt: updatedAt}
	if err := value.Validate(); err != nil {
		return pullrequest.ReviewRecord{}, invalid("pull request review record: %v", err)
	}
	return value, nil
}

func decodeIssueCommentRecord(raw []byte) (issue.CommentRecord, error) {
	var wire struct {
		Type      string           `json:"$type"`
		Subject   issue.StrongRef  `json:"subject"`
		Parent    *issue.StrongRef `json:"parent,omitempty"`
		Body      string           `json:"body"`
		CreatedAt string           `json:"createdAt"`
		UpdatedAt string           `json:"updatedAt"`
	}
	if err := decodeStrict(raw, &wire); err != nil {
		return issue.CommentRecord{}, invalid("decode issue comment record: %v", err)
	}
	if wire.Type != issue.CommentCollection {
		return issue.CommentRecord{}, invalid("issue comment record type is invalid")
	}
	createdAt, err := canonicalDatetime(wire.CreatedAt)
	if err != nil {
		return issue.CommentRecord{}, err
	}
	updatedAt, err := canonicalDatetime(wire.UpdatedAt)
	if err != nil {
		return issue.CommentRecord{}, err
	}
	value := issue.CommentRecord{Subject: wire.Subject, Parent: wire.Parent, Body: wire.Body, CreatedAt: createdAt, UpdatedAt: updatedAt}
	if err := value.Validate(); err != nil {
		return issue.CommentRecord{}, invalid("issue comment record: %v", err)
	}
	return value, nil
}

func decodeIssueRecord(raw []byte) (issue.Record, error) {
	var wire struct {
		Type       string          `json:"$type"`
		Repository issue.StrongRef `json:"repository"`
		Title      string          `json:"title"`
		Body       string          `json:"body"`
		CreatedAt  string          `json:"createdAt"`
		UpdatedAt  string          `json:"updatedAt"`
	}
	if err := decodeStrict(raw, &wire); err != nil {
		return issue.Record{}, invalid("decode issue record: %v", err)
	}
	if wire.Type != IssueCollection {
		return issue.Record{}, invalid("issue record type is invalid")
	}
	createdAt, err := canonicalDatetime(wire.CreatedAt)
	if err != nil {
		return issue.Record{}, err
	}
	updatedAt, err := canonicalDatetime(wire.UpdatedAt)
	if err != nil {
		return issue.Record{}, err
	}
	value := issue.Record{Repository: wire.Repository, Title: wire.Title, Body: wire.Body, CreatedAt: createdAt, UpdatedAt: updatedAt}
	if err := value.Validate(); err != nil {
		return issue.Record{}, invalid("issue record: %v", err)
	}
	return value, nil
}

func decodeIssueStatusRecord(raw []byte) (issue.StatusRecord, error) {
	var wire struct {
		Type       string          `json:"$type"`
		Subject    issue.StrongRef `json:"subject"`
		Repository issue.StrongRef `json:"repository"`
		State      issue.State     `json:"state"`
		CreatedAt  string          `json:"createdAt"`
		UpdatedAt  string          `json:"updatedAt"`
	}
	if err := decodeStrict(raw, &wire); err != nil {
		return issue.StatusRecord{}, invalid("decode issue status record: %v", err)
	}
	if wire.Type != IssueStatusCollection {
		return issue.StatusRecord{}, invalid("issue status record type is invalid")
	}
	createdAt, err := canonicalDatetime(wire.CreatedAt)
	if err != nil {
		return issue.StatusRecord{}, err
	}
	updatedAt, err := canonicalDatetime(wire.UpdatedAt)
	if err != nil {
		return issue.StatusRecord{}, err
	}
	value := issue.StatusRecord{Subject: wire.Subject, Repository: wire.Repository, State: wire.State, CreatedAt: createdAt, UpdatedAt: updatedAt}
	if err := value.Validate(); err != nil {
		return issue.StatusRecord{}, invalid("issue status record: %v", err)
	}
	return value, nil
}

func decodeStarRecord(raw []byte) (StarRecord, error) {
	var wire struct {
		Type    string `json:"$type"`
		Subject struct {
			URI string `json:"uri"`
			CID string `json:"cid"`
		} `json:"subject"`
		CreatedAt string `json:"createdAt"`
	}
	if err := decodeStrict(raw, &wire); err != nil {
		return StarRecord{}, invalid("decode star record: %v", err)
	}
	if wire.Type != StarCollection {
		return StarRecord{}, invalid("star record type is invalid")
	}
	target := star.Target{URI: wire.Subject.URI, CID: wire.Subject.CID}
	if err := target.Validate(); err != nil {
		return StarRecord{}, invalid("star subject: %v", err)
	}
	createdAt, err := canonicalDatetime(wire.CreatedAt)
	if err != nil {
		return StarRecord{}, err
	}
	return StarRecord{RepositoryURI: wire.Subject.URI, RepositoryCID: wire.Subject.CID, CreatedAt: createdAt}, nil
}

func decodeIdentityEvent(raw []byte) (IdentityEvent, error) {
	var wire tapIdentity
	if err := decodeStrict(raw, &wire); err != nil {
		return IdentityEvent{}, invalid("decode identity event: %v", err)
	}
	if err := canonicalDID(wire.DID); err != nil {
		return IdentityEvent{}, err
	}
	if wire.Status == "" || wire.IsActive == nil {
		return IdentityEvent{}, invalid("identity status and isActive are required")
	}
	if wire.Handle != "" {
		handle, err := syntax.ParseHandle(wire.Handle)
		if err != nil || handle.Normalize().String() != wire.Handle {
			return IdentityEvent{}, invalid("handle is not canonical")
		}
	}
	return IdentityEvent{DID: wire.DID, Handle: wire.Handle, Status: wire.Status, IsActive: *wire.IsActive}, nil
}

func decodeProfileRecord(raw []byte) (ProfileRecord, error) {
	var wire struct {
		Type        string `json:"$type"`
		DisplayName string `json:"displayName,omitempty"`
		Bio         string `json:"bio,omitempty"`
		Website     string `json:"website,omitempty"`
		Location    string `json:"location,omitempty"`
		CreatedAt   string `json:"createdAt"`
	}
	if err := decodeStrict(raw, &wire); err != nil {
		return ProfileRecord{}, invalid("decode profile record: %v", err)
	}
	if wire.Type != ProfileCollection {
		return ProfileRecord{}, invalid("profile record type is invalid")
	}
	createdAt, err := canonicalDatetime(wire.CreatedAt)
	if err != nil {
		return ProfileRecord{}, err
	}
	if err := (profile.UpdateInput{DisplayName: wire.DisplayName, Bio: wire.Bio, Website: wire.Website, Location: wire.Location}).Validate(); err != nil {
		return ProfileRecord{}, invalid("profile record: %v", err)
	}
	return ProfileRecord{DisplayName: wire.DisplayName, Bio: wire.Bio, Website: wire.Website, Location: wire.Location, CreatedAt: createdAt}, nil
}

func decodeOrganizationRecord(raw []byte) (OrganizationRecord, error) {
	var wire struct {
		Type        string `json:"$type"`
		Slug        string `json:"slug"`
		Name        string `json:"name"`
		Description string `json:"description,omitempty"`
		Website     string `json:"website,omitempty"`
		Location    string `json:"location,omitempty"`
		CreatedAt   string `json:"createdAt"`
		UpdatedAt   string `json:"updatedAt"`
	}
	if err := decodeStrict(raw, &wire); err != nil {
		return OrganizationRecord{}, invalid("decode organization record: %v", err)
	}
	if wire.Type != OrganizationCollection || len(wire.Slug) > 100 || !organizationSlugPattern.MatchString(wire.Slug) || !validText(wire.Name, 255, 100, true) || !validText(wire.Description, 2000, 2000, false) || !validText(wire.Location, 255, 100, false) {
		return OrganizationRecord{}, invalid("organization record fields are invalid")
	}
	if wire.Website != "" {
		if len(wire.Website) > 2048 || validateWebEndpoint(wire.Website) != nil {
			return OrganizationRecord{}, invalid("organization website is invalid")
		}
	}
	createdAt, err := canonicalDatetime(wire.CreatedAt)
	if err != nil {
		return OrganizationRecord{}, err
	}
	updatedAt, err := canonicalDatetime(wire.UpdatedAt)
	if err != nil {
		return OrganizationRecord{}, err
	}
	if updatedAt.Before(createdAt) {
		return OrganizationRecord{}, invalid("organization updatedAt must not precede createdAt")
	}
	return OrganizationRecord{Slug: wire.Slug, Name: wire.Name, Description: wire.Description, Website: wire.Website, Location: wire.Location, CreatedAt: createdAt, UpdatedAt: updatedAt}, nil
}

func decodeOrganizationGrantRecord(raw []byte) (OrganizationGrantRecord, error) {
	var wire struct {
		Type         string    `json:"$type"`
		Organization StrongRef `json:"organization"`
		Subject      string    `json:"subject"`
		Role         string    `json:"role"`
		Authority    StrongRef `json:"authority"`
		CreatedAt    string    `json:"createdAt"`
		ExpiresAt    string    `json:"expiresAt,omitempty"`
	}
	if err := decodeStrict(raw, &wire); err != nil {
		return OrganizationGrantRecord{}, invalid("decode organization grant: %v", err)
	}
	if wire.Type != OrganizationGrantCollection || (wire.Role != "owner" && wire.Role != "member") || canonicalDID(wire.Subject) != nil {
		return OrganizationGrantRecord{}, invalid("organization grant fields are invalid")
	}
	if err := validateStrongRef(wire.Organization, OrganizationCollection); err != nil {
		return OrganizationGrantRecord{}, err
	}
	if err := validateStrongRef(wire.Authority, ""); err != nil {
		return OrganizationGrantRecord{}, err
	}
	createdAt, err := canonicalDatetime(wire.CreatedAt)
	if err != nil {
		return OrganizationGrantRecord{}, err
	}
	var expiresAt *time.Time
	if wire.ExpiresAt != "" {
		parsed, parseErr := canonicalDatetime(wire.ExpiresAt)
		if parseErr != nil || !parsed.After(createdAt) {
			return OrganizationGrantRecord{}, invalid("organization grant expiry is invalid")
		}
		expiresAt = &parsed
	}
	return OrganizationGrantRecord{Organization: wire.Organization, Subject: wire.Subject, Role: wire.Role, Authority: wire.Authority, CreatedAt: createdAt, ExpiresAt: expiresAt}, nil
}

func decodeOrganizationMembershipRecord(raw []byte) (OrganizationMembershipRecord, error) {
	var wire struct {
		Type         string    `json:"$type"`
		Organization StrongRef `json:"organization"`
		Grant        StrongRef `json:"grant"`
		Visibility   string    `json:"visibility"`
		CreatedAt    string    `json:"createdAt"`
		UpdatedAt    string    `json:"updatedAt"`
	}
	if err := decodeStrict(raw, &wire); err != nil {
		return OrganizationMembershipRecord{}, invalid("decode organization membership: %v", err)
	}
	if wire.Type != OrganizationMembershipCollection || wire.Visibility != "public" {
		return OrganizationMembershipRecord{}, invalid("organization membership fields are invalid")
	}
	if err := validateStrongRef(wire.Organization, OrganizationCollection); err != nil {
		return OrganizationMembershipRecord{}, err
	}
	if err := validateStrongRef(wire.Grant, OrganizationGrantCollection); err != nil {
		return OrganizationMembershipRecord{}, err
	}
	createdAt, err := canonicalDatetime(wire.CreatedAt)
	if err != nil {
		return OrganizationMembershipRecord{}, err
	}
	updatedAt, err := canonicalDatetime(wire.UpdatedAt)
	if err != nil {
		return OrganizationMembershipRecord{}, err
	}
	if updatedAt.Before(createdAt) {
		return OrganizationMembershipRecord{}, invalid("organization membership updatedAt must not precede createdAt")
	}
	return OrganizationMembershipRecord{Organization: wire.Organization, Grant: wire.Grant, Visibility: wire.Visibility, CreatedAt: createdAt, UpdatedAt: updatedAt}, nil
}

func decodeOrganizationRevocationRecord(raw []byte) (OrganizationRevocationRecord, error) {
	var wire struct {
		Type         string    `json:"$type"`
		Organization StrongRef `json:"organization"`
		Grant        StrongRef `json:"grant"`
		Subject      string    `json:"subject"`
		Authority    StrongRef `json:"authority"`
		CreatedAt    string    `json:"createdAt"`
	}
	if err := decodeStrict(raw, &wire); err != nil {
		return OrganizationRevocationRecord{}, invalid("decode organization revocation: %v", err)
	}
	if wire.Type != OrganizationRevocationCollection || canonicalDID(wire.Subject) != nil {
		return OrganizationRevocationRecord{}, invalid("organization revocation fields are invalid")
	}
	if err := validateStrongRef(wire.Organization, OrganizationCollection); err != nil {
		return OrganizationRevocationRecord{}, err
	}
	if err := validateStrongRef(wire.Grant, OrganizationGrantCollection); err != nil {
		return OrganizationRevocationRecord{}, err
	}
	if err := validateStrongRef(wire.Authority, ""); err != nil {
		return OrganizationRevocationRecord{}, err
	}
	createdAt, err := canonicalDatetime(wire.CreatedAt)
	if err != nil {
		return OrganizationRevocationRecord{}, err
	}
	return OrganizationRevocationRecord{Organization: wire.Organization, Grant: wire.Grant, Subject: wire.Subject, Authority: wire.Authority, CreatedAt: createdAt}, nil
}

func validateStrongRef(value StrongRef, collection string) error {
	uri, err := syntax.ParseATURI(value.URI)
	if err != nil || uri.String() != value.URI {
		return invalid("strong reference URI is invalid")
	}
	cid, err := syntax.ParseCID(value.CID)
	if err != nil || cid.String() != value.CID {
		return invalid("strong reference CID is invalid")
	}
	if collection != "" && !strings.Contains(value.URI, "/"+collection+"/") {
		return invalid("strong reference collection is invalid")
	}
	return nil
}

func decodeRepositoryRecord(raw []byte) (RepositoryRecord, error) {
	var wire struct {
		Type          string     `json:"$type"`
		Slug          string     `json:"slug"`
		Name          string     `json:"name"`
		Description   string     `json:"description,omitempty"`
		Organization  *StrongRef `json:"organization,omitempty"`
		DefaultBranch string     `json:"defaultBranch"`
		Git           struct {
			HTTPS string `json:"https"`
			SSH   string `json:"ssh,omitempty"`
		} `json:"git"`
		Web       string `json:"web"`
		CreatedAt string `json:"createdAt"`
		UpdatedAt string `json:"updatedAt"`
	}
	if err := decodeStrict(raw, &wire); err != nil {
		return RepositoryRecord{}, invalid("decode repository record: %v", err)
	}
	if wire.Type != RepositoryCollection {
		return RepositoryRecord{}, invalid("repository record type is invalid")
	}
	if len(wire.Slug) > 100 || !slugPattern.MatchString(wire.Slug) {
		return RepositoryRecord{}, invalid("repository slug is invalid")
	}
	if !validText(wire.Name, 100, 100, true) || !validText(wire.Description, 2000, 2000, false) || !validDefaultBranch(wire.DefaultBranch) {
		return RepositoryRecord{}, invalid("repository text fields are invalid")
	}
	if len(wire.Git.HTTPS) > 2048 || len(wire.Git.SSH) > 2048 || len(wire.Web) > 2048 {
		return RepositoryRecord{}, invalid("repository URL is too long")
	}
	if err := validateWebEndpoint(wire.Git.HTTPS); err != nil {
		return RepositoryRecord{}, invalid("git https URL: %v", err)
	}
	if wire.Git.SSH != "" {
		if err := validateGitSSHEndpoint(wire.Git.SSH); err != nil {
			return RepositoryRecord{}, invalid("git ssh URL: %v", err)
		}
	}
	if err := validateWebEndpoint(wire.Web); err != nil {
		return RepositoryRecord{}, invalid("web URL: %v", err)
	}
	if wire.Organization != nil {
		if err := validateStrongRef(*wire.Organization, OrganizationCollection); err != nil {
			return RepositoryRecord{}, err
		}
	}
	createdAt, err := canonicalDatetime(wire.CreatedAt)
	if err != nil {
		return RepositoryRecord{}, err
	}
	updatedAt, err := canonicalDatetime(wire.UpdatedAt)
	if err != nil {
		return RepositoryRecord{}, err
	}
	if updatedAt.Before(createdAt) {
		return RepositoryRecord{}, invalid("repository updatedAt must not precede createdAt")
	}
	return RepositoryRecord{
		Slug: wire.Slug, Name: wire.Name, Description: wire.Description, DefaultBranch: wire.DefaultBranch,
		GitHTTPS: wire.Git.HTTPS, GitSSH: wire.Git.SSH, Web: wire.Web, CreatedAt: createdAt, UpdatedAt: updatedAt, Organization: wire.Organization,
	}, nil
}

func validText(value string, maximumBytes, maximumRunes int, required bool) bool {
	return (!required || value != "") && utf8.ValidString(value) && len(value) <= maximumBytes && utf8.RuneCountInString(value) <= maximumRunes
}

func canonicalDID(value string) error {
	did, err := syntax.ParseDID(value)
	if err != nil || did.String() != value {
		return invalid("did is not canonical")
	}
	return nil
}

func canonicalDatetime(value string) (time.Time, error) {
	parsed, err := syntax.ParseDatetimeTime(value)
	if err != nil || parsed.UTC().Format(syntax.AtprotoDatetimeLayout) != value {
		return time.Time{}, invalid("datetime is not canonical")
	}
	return parsed, nil
}

func validDefaultBranch(value string) bool {
	if !validText(value, 255, 255, true) || value == "@" || value[0] == '-' || value[0] == '/' || value[len(value)-1] == '/' || value[len(value)-1] == '.' {
		return false
	}
	if strings.Contains(value, "..") || strings.Contains(value, "//") || strings.Contains(value, "@{") {
		return false
	}
	for _, character := range value {
		if unicode.IsControl(character) || strings.ContainsRune(" ~^:?*[\\", character) {
			return false
		}
	}
	for _, component := range strings.Split(value, "/") {
		if component[0] == '.' || strings.HasSuffix(component, ".lock") {
			return false
		}
	}
	return true
}

func validateWebEndpoint(value string) error {
	parsed, err := parseEndpoint(value)
	if err != nil {
		return err
	}
	if parsed.User != nil {
		return errors.New("must not contain user information")
	}
	if parsed.Scheme != "https" && parsed.Scheme != "http" {
		return errors.New("must use HTTPS")
	}
	defaultPort := "443"
	if parsed.Scheme == "http" {
		defaultPort = "80"
		ip := net.ParseIP(parsed.Hostname())
		if ip == nil || !ip.IsLoopback() {
			return errors.New("must use HTTPS except on a literal loopback IP")
		}
	}
	if err := validateEndpointAuthority(parsed, defaultPort); err != nil {
		return err
	}
	return validateEndpointPath(parsed)
}

func validateGitSSHEndpoint(value string) error {
	parsed, err := parseEndpoint(value)
	if err != nil {
		return err
	}
	if parsed.Scheme != "ssh" {
		return errors.New("must be an absolute SSH URI")
	}
	if parsed.User != nil {
		if _, hasPassword := parsed.User.Password(); hasPassword || parsed.User.Username() != "git" || parsed.User.String() != "git" {
			return errors.New("username must be git and password must be absent")
		}
	}
	if err := validateEndpointAuthority(parsed, "22"); err != nil {
		return err
	}
	return validateEndpointPath(parsed)
}

func parseEndpoint(value string) (*url.URL, error) {
	if value == "" || hasControl(value) || strings.Contains(value, "\\") {
		return nil, errors.New("must not contain control characters or backslashes")
	}
	parsed, err := url.Parse(value)
	if err != nil || !parsed.IsAbs() || parsed.Opaque != "" || parsed.Host == "" || parsed.String() != value {
		return nil, errors.New("must be a canonical absolute URI")
	}
	if parsed.RawQuery != "" || parsed.ForceQuery || parsed.Fragment != "" || strings.ContainsAny(value, "?#") {
		return nil, errors.New("must not contain a query or fragment")
	}
	return parsed, nil
}

func validateEndpointAuthority(parsed *url.URL, defaultPort string) error {
	hostname := parsed.Hostname()
	if !validEndpointHost(hostname) {
		return errors.New("host is invalid")
	}
	port := parsed.Port()
	hasPort := false
	if strings.HasPrefix(parsed.Host, "[") {
		closing := strings.LastIndexByte(parsed.Host, ']')
		hasPort = closing >= 0 && closing+1 < len(parsed.Host)
		if hasPort && (port == "" || parsed.Host[closing+1] != ':') {
			return errors.New("port is malformed")
		}
	} else if strings.Contains(parsed.Host, ":") {
		hasPort = true
		if port == "" {
			return errors.New("port is malformed")
		}
	}
	if !hasPort {
		return nil
	}
	number, err := strconv.Atoi(port)
	if err != nil || number < 1 || number > 65535 || strconv.Itoa(number) != port {
		return errors.New("port is malformed")
	}
	if port == defaultPort {
		return errors.New("default port must be omitted")
	}
	return nil
}

func validEndpointHost(hostname string) bool {
	if hostname == "" || strings.Contains(hostname, "%") {
		return false
	}
	if net.ParseIP(hostname) != nil {
		return true
	}
	if len(hostname) > 253 || strings.HasSuffix(hostname, ".") {
		return false
	}
	for _, label := range strings.Split(hostname, ".") {
		if label == "" || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for _, character := range label {
			if !((character >= 'a' && character <= 'z') || (character >= 'A' && character <= 'Z') || (character >= '0' && character <= '9') || character == '-') {
				return false
			}
		}
	}
	return true
}

func validateEndpointPath(parsed *url.URL) error {
	if parsed.Path == "" || parsed.Path == "/" || parsed.Path[0] != '/' || hasControl(parsed.Path) || strings.Contains(parsed.Path, "\\") {
		return errors.New("path must be non-root and unambiguous")
	}
	escaped := strings.ToLower(parsed.EscapedPath())
	for _, encoding := range []string{"%2e", "%2f", "%5c"} {
		if strings.Contains(escaped, encoding) {
			return errors.New("path must not contain encoded dot, slash, or backslash characters")
		}
	}
	for _, component := range strings.Split(strings.TrimPrefix(parsed.Path, "/"), "/") {
		if component == "" || component == "." || component == ".." {
			return errors.New("path must not contain empty or dot segments")
		}
	}
	return nil
}

func hasControl(value string) bool {
	for _, character := range value {
		if unicode.IsControl(character) {
			return true
		}
	}
	return false
}

func decodeStrict(raw []byte, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return errors.New("trailing JSON")
	}
	return nil
}

func invalid(format string, arguments ...any) error {
	return fmt.Errorf("%w: %s", ErrInvalidEvent, fmt.Sprintf(format, arguments...))
}
