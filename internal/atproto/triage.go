package atproto

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/adenosine-dev/adenosine/internal/triage"
	"github.com/bluesky-social/indigo/atproto/auth/oauth"
	indigoidentity "github.com/bluesky-social/indigo/atproto/identity"
	"github.com/bluesky-social/indigo/atproto/syntax"
)

type triagePutRecordInput struct {
	Collection string         `json:"collection"`
	Record     map[string]any `json:"record"`
	Repo       string         `json:"repo"`
	RKey       string         `json:"rkey"`
	SwapRecord *string        `json:"swapRecord"`
}

type triageDeleteRecordInput struct {
	Collection string `json:"collection"`
	Repo       string `json:"repo"`
	RKey       string `json:"rkey"`
	SwapRecord string `json:"swapRecord"`
}

type triageStrongRefWire struct {
	URI string `json:"uri"`
	CID string `json:"cid"`
}

type labelWireRecord struct {
	Type        string              `json:"$type"`
	Repository  triageStrongRefWire `json:"repository"`
	Name        string              `json:"name"`
	Color       string              `json:"color"`
	Description string              `json:"description"`
	CreatedAt   string              `json:"createdAt"`
	UpdatedAt   string              `json:"updatedAt"`
}

type milestoneWireRecord struct {
	Type        string                `json:"$type"`
	Repository  triageStrongRefWire   `json:"repository"`
	Title       string                `json:"title"`
	Description string                `json:"description"`
	State       triage.MilestoneState `json:"state"`
	DueAt       *string               `json:"dueAt,omitempty"`
	ClosedAt    *string               `json:"closedAt,omitempty"`
	CreatedAt   string                `json:"createdAt"`
	UpdatedAt   string                `json:"updatedAt"`
}

type metadataWireRecord struct {
	Type       string              `json:"$type"`
	Subject    triageStrongRefWire `json:"subject"`
	Kind       triage.SubjectKind  `json:"kind"`
	Repository triageStrongRefWire `json:"repository"`
	Labels     []string            `json:"labels"`
	Assignees  []string            `json:"assignees"`
	Milestone  string              `json:"milestone,omitempty"`
	CreatedAt  string              `json:"createdAt"`
	UpdatedAt  string              `json:"updatedAt"`
}

// CreateLabel creates a repository-authoritative label with a caller-generated key.
func (client *Client) CreateLabel(ctx context.Context, authorDID, rkey string, record triage.LabelRecord) (triage.Label, error) {
	return client.publishLabel(ctx, authorDID, rkey, "", record)
}

// PutLabel compare-and-swaps a repository-authoritative label.
func (client *Client) PutLabel(ctx context.Context, authorDID, rkey, swapCID string, record triage.LabelRecord) (triage.Label, error) {
	if swapCID == "" {
		return triage.Label{}, &triage.ValidationError{Field: "swapCID", Problem: "must not be empty"}
	}
	return client.publishLabel(ctx, authorDID, rkey, swapCID, record)
}

func (client *Client) publishLabel(ctx context.Context, authorDID, rkey, swapCID string, record triage.LabelRecord) (triage.Label, error) {
	if err := triage.ValidateRecordKey(rkey); err != nil {
		return triage.Label{}, err
	}
	if err := record.Validate(); err != nil {
		return triage.Label{}, err
	}
	if owner, err := triage.RepositoryOwnerDID(record.Repository.URI); err != nil || owner != authorDID {
		return triage.Label{}, &triage.AuthorizationError{Err: err}
	}
	var result triage.Label
	err := client.withTriageSession(ctx, authorDID, func(host, did string, session *oauth.ClientSession) error {
		createdAt, updatedAt, err := canonicalTriageTimes(record.CreatedAt, record.UpdatedAt)
		if err != nil {
			return err
		}
		wire := map[string]any{"$type": triage.LabelCollection, "repository": triageStrongRef(record.Repository), "name": record.Name, "color": record.Color, "description": record.Description, "createdAt": createdAt, "updatedAt": updatedAt}
		output, latest, err := client.putTriageRecord(ctx, host, session, did, triage.LabelCollection, rkey, swapPointer(swapCID), wire)
		if err != nil {
			return err
		}
		if latest != nil {
			current, decodeErr := decodeLabel(*latest, did, rkey)
			if decodeErr != nil || !sameLabelRecord(current.LabelRecord, record) {
				return &triage.ConflictError{Err: decodeErr}
			}
			result = current
			return nil
		}
		result = triage.Label{URI: output.URI, CID: output.CID, AuthorDID: did, RKey: rkey, LabelRecord: record}
		result.CreatedAt, _ = syntax.ParseDatetimeTime(createdAt)
		result.UpdatedAt, _ = syntax.ParseDatetimeTime(updatedAt)
		if err := result.Validate(); err != nil {
			return &triage.ProviderError{Operation: "label response validation", Err: err}
		}
		return nil
	})
	return result, err
}

// CreateMilestone creates a repository-authoritative milestone with a caller-generated key.
func (client *Client) CreateMilestone(ctx context.Context, authorDID, rkey string, record triage.MilestoneRecord) (triage.Milestone, error) {
	return client.publishMilestone(ctx, authorDID, rkey, "", record)
}

// PutMilestone compare-and-swaps a repository-authoritative milestone.
func (client *Client) PutMilestone(ctx context.Context, authorDID, rkey, swapCID string, record triage.MilestoneRecord) (triage.Milestone, error) {
	if swapCID == "" {
		return triage.Milestone{}, &triage.ValidationError{Field: "swapCID", Problem: "must not be empty"}
	}
	return client.publishMilestone(ctx, authorDID, rkey, swapCID, record)
}

func (client *Client) publishMilestone(ctx context.Context, authorDID, rkey, swapCID string, record triage.MilestoneRecord) (triage.Milestone, error) {
	if err := triage.ValidateRecordKey(rkey); err != nil {
		return triage.Milestone{}, err
	}
	if err := record.Validate(); err != nil {
		return triage.Milestone{}, err
	}
	if owner, err := triage.RepositoryOwnerDID(record.Repository.URI); err != nil || owner != authorDID {
		return triage.Milestone{}, &triage.AuthorizationError{Err: err}
	}
	var result triage.Milestone
	err := client.withTriageSession(ctx, authorDID, func(host, did string, session *oauth.ClientSession) error {
		createdAt, updatedAt, err := canonicalTriageTimes(record.CreatedAt, record.UpdatedAt)
		if err != nil {
			return err
		}
		wire := map[string]any{"$type": triage.MilestoneCollection, "repository": triageStrongRef(record.Repository), "title": record.Title, "description": record.Description, "state": record.State, "createdAt": createdAt, "updatedAt": updatedAt}
		if record.DueAt != nil {
			wire["dueAt"] = formatTriageTime(*record.DueAt)
		}
		if record.ClosedAt != nil {
			wire["closedAt"] = formatTriageTime(*record.ClosedAt)
		}
		output, latest, err := client.putTriageRecord(ctx, host, session, did, triage.MilestoneCollection, rkey, swapPointer(swapCID), wire)
		if err != nil {
			return err
		}
		if latest != nil {
			current, decodeErr := decodeMilestone(*latest, did, rkey)
			if decodeErr != nil || !sameMilestoneRecord(current.MilestoneRecord, record) {
				return &triage.ConflictError{Err: decodeErr}
			}
			result = current
			return nil
		}
		result = triage.Milestone{URI: output.URI, CID: output.CID, AuthorDID: did, RKey: rkey, MilestoneRecord: record}
		result.CreatedAt, _ = syntax.ParseDatetimeTime(createdAt)
		result.UpdatedAt, _ = syntax.ParseDatetimeTime(updatedAt)
		if err := result.Validate(); err != nil {
			return &triage.ProviderError{Operation: "milestone response validation", Err: err}
		}
		return nil
	})
	return result, err
}

// PutSubjectTriage creates or compare-and-swaps the deterministic metadata slot for a subject.
func (client *Client) PutSubjectTriage(ctx context.Context, authorDID, swapCID string, record triage.MetadataRecord) (triage.Metadata, error) {
	if err := record.Validate(); err != nil {
		return triage.Metadata{}, err
	}
	if owner, err := triage.RepositoryOwnerDID(record.Repository.URI); err != nil || owner != authorDID {
		return triage.Metadata{}, &triage.AuthorizationError{Err: err}
	}
	rkey, err := triage.MetadataRecordKey(record.Subject.URI)
	if err != nil {
		return triage.Metadata{}, err
	}
	var result triage.Metadata
	err = client.withTriageSession(ctx, authorDID, func(host, did string, session *oauth.ClientSession) error {
		createdAt, updatedAt, err := canonicalTriageTimes(record.CreatedAt, record.UpdatedAt)
		if err != nil {
			return err
		}
		wire := map[string]any{"$type": triage.MetadataCollection, "subject": triageStrongRef(record.Subject), "kind": record.Kind, "repository": triageStrongRef(record.Repository), "labels": record.LabelURIs, "assignees": record.AssigneeDIDs, "createdAt": createdAt, "updatedAt": updatedAt}
		if record.MilestoneURI != "" {
			wire["milestone"] = record.MilestoneURI
		}
		output, latest, err := client.putTriageRecord(ctx, host, session, did, triage.MetadataCollection, rkey, swapPointer(swapCID), wire)
		if err != nil {
			return err
		}
		if latest != nil {
			current, decodeErr := decodeMetadata(*latest, did, rkey)
			if decodeErr != nil || !sameMetadataRecord(current.MetadataRecord, record) {
				return &triage.ConflictError{Err: decodeErr}
			}
			result = current
			return nil
		}
		result = triage.Metadata{URI: output.URI, CID: output.CID, AuthorDID: did, RKey: rkey, MetadataRecord: record, Labels: []triage.Label{}, Assignees: []triage.Assignee{}}
		result.CreatedAt, _ = syntax.ParseDatetimeTime(createdAt)
		result.UpdatedAt, _ = syntax.ParseDatetimeTime(updatedAt)
		if err := result.Validate(); err != nil {
			return &triage.ProviderError{Operation: "subject triage response validation", Err: err}
		}
		return nil
	})
	return result, err
}

// DeleteTriageRecord compare-and-swaps one current triage record to a tombstone.
func (client *Client) DeleteTriageRecord(ctx context.Context, authorDID, collection, rkey, swapCID string) error {
	if collection != triage.LabelCollection && collection != triage.MilestoneCollection && collection != triage.MetadataCollection {
		return &triage.ValidationError{Field: "collection", Problem: "must be a triage collection"}
	}
	if err := triage.ValidateRecordKey(rkey); err != nil {
		return err
	}
	if swapCID == "" {
		return &triage.ValidationError{Field: "swapCID", Problem: "must not be empty"}
	}
	return client.withTriageSession(ctx, authorDID, func(host, did string, session *oauth.ClientSession) error {
		api := client.apiFactory(host, session)
		input := triageDeleteRecordInput{Collection: collection, Repo: did, RKey: rkey, SwapRecord: swapCID}
		if err := api.Post(ctx, deleteRecordNSID, input, &struct{}{}); err != nil {
			if isRecordNotFound(err) {
				return nil
			}
			if !isInvalidSwap(err) {
				return &triage.ProviderError{Operation: "delete", Err: err}
			}
			latest, found, getErr := getTriageRecord(ctx, api, did, collection, rkey)
			if getErr != nil || !found {
				return getErr
			}
			if latest.CID != nil && *latest.CID == swapCID {
				return &triage.ProviderError{Operation: "delete retry", Err: err}
			}
			return &triage.ConflictError{Err: errors.New("record changed concurrently")}
		}
		return nil
	})
}

func (client *Client) putTriageRecord(ctx context.Context, host string, session *oauth.ClientSession, did, collection, rkey string, swap *string, record map[string]any) (putRecordOutput, *getRecordOutput, error) {
	api := client.apiFactory(host, session)
	var output putRecordOutput
	if err := api.Post(ctx, putRecordNSID, triagePutRecordInput{Collection: collection, Repo: did, RKey: rkey, SwapRecord: swap, Record: record}, &output); err != nil {
		if !isInvalidSwap(err) {
			return putRecordOutput{}, nil, &triage.ProviderError{Operation: "put", Err: err}
		}
		latest, found, getErr := getTriageRecord(ctx, api, did, collection, rkey)
		if getErr != nil {
			return putRecordOutput{}, nil, getErr
		}
		if !found {
			return putRecordOutput{}, nil, &triage.ConflictError{Err: errors.New("record slot disappeared")}
		}
		return putRecordOutput{}, &latest, nil
	}
	return output, nil, nil
}

func getTriageRecord(ctx context.Context, api profileAPI, did, collection, rkey string) (getRecordOutput, bool, error) {
	var output getRecordOutput
	err := api.Get(ctx, getRecordNSID, map[string]any{"collection": collection, "repo": did, "rkey": rkey}, &output)
	if isRecordNotFound(err) {
		return getRecordOutput{}, false, nil
	}
	if err != nil {
		return getRecordOutput{}, false, &triage.ProviderError{Operation: "get", Err: err}
	}
	return output, true, nil
}

func decodeLabel(output getRecordOutput, did, rkey string) (triage.Label, error) {
	if output.CID == nil || output.Value == nil {
		return triage.Label{}, errors.New("label envelope is incomplete")
	}
	var wire labelWireRecord
	if err := decodeTriageJSON(*output.Value, &wire); err != nil || wire.Type != triage.LabelCollection {
		return triage.Label{}, errors.New("label record is invalid")
	}
	record := triage.LabelRecord{Repository: triage.StrongRef{URI: wire.Repository.URI, CID: wire.Repository.CID}, Name: wire.Name, Color: wire.Color, Description: wire.Description}
	var err error
	record.CreatedAt, err = parseCanonicalTriageTime(wire.CreatedAt)
	if err == nil {
		record.UpdatedAt, err = parseCanonicalTriageTime(wire.UpdatedAt)
	}
	if err != nil {
		return triage.Label{}, err
	}
	result := triage.Label{URI: output.URI, CID: *output.CID, AuthorDID: did, RKey: rkey, LabelRecord: record}
	return result, result.Validate()
}

func decodeMilestone(output getRecordOutput, did, rkey string) (triage.Milestone, error) {
	if output.CID == nil || output.Value == nil {
		return triage.Milestone{}, errors.New("milestone envelope is incomplete")
	}
	var wire milestoneWireRecord
	if err := decodeTriageJSON(*output.Value, &wire); err != nil || wire.Type != triage.MilestoneCollection {
		return triage.Milestone{}, errors.New("milestone record is invalid")
	}
	record := triage.MilestoneRecord{Repository: triage.StrongRef{URI: wire.Repository.URI, CID: wire.Repository.CID}, Title: wire.Title, Description: wire.Description, State: wire.State}
	var err error
	record.CreatedAt, err = parseCanonicalTriageTime(wire.CreatedAt)
	if err == nil {
		record.UpdatedAt, err = parseCanonicalTriageTime(wire.UpdatedAt)
	}
	if err == nil {
		record.DueAt, err = parseOptionalTriageTime(wire.DueAt)
	}
	if err == nil {
		record.ClosedAt, err = parseOptionalTriageTime(wire.ClosedAt)
	}
	if err != nil {
		return triage.Milestone{}, err
	}
	result := triage.Milestone{URI: output.URI, CID: *output.CID, AuthorDID: did, RKey: rkey, MilestoneRecord: record}
	return result, result.Validate()
}

func decodeMetadata(output getRecordOutput, did, rkey string) (triage.Metadata, error) {
	if output.CID == nil || output.Value == nil {
		return triage.Metadata{}, errors.New("subject triage envelope is incomplete")
	}
	var wire metadataWireRecord
	if err := decodeTriageJSON(*output.Value, &wire); err != nil || wire.Type != triage.MetadataCollection {
		return triage.Metadata{}, errors.New("subject triage record is invalid")
	}
	record := triage.MetadataRecord{Subject: triage.StrongRef{URI: wire.Subject.URI, CID: wire.Subject.CID}, Kind: wire.Kind, Repository: triage.StrongRef{URI: wire.Repository.URI, CID: wire.Repository.CID}, LabelURIs: wire.Labels, AssigneeDIDs: wire.Assignees, MilestoneURI: wire.Milestone}
	var err error
	record.CreatedAt, err = parseCanonicalTriageTime(wire.CreatedAt)
	if err == nil {
		record.UpdatedAt, err = parseCanonicalTriageTime(wire.UpdatedAt)
	}
	if err != nil {
		return triage.Metadata{}, err
	}
	result := triage.Metadata{URI: output.URI, CID: *output.CID, AuthorDID: did, RKey: rkey, MetadataRecord: record, Labels: []triage.Label{}, Assignees: []triage.Assignee{}}
	return result, result.Validate()
}

func (client *Client) withTriageSession(ctx context.Context, rawDID string, operation func(string, string, *oauth.ClientSession) error) error {
	did, host, err := client.resolveTriageIdentity(ctx, rawDID)
	if err != nil {
		return err
	}
	lock := client.operationLock(did.String())
	lock.Lock()
	defer lock.Unlock()
	session, err := client.resumeTriageSession(ctx, did, host)
	if err != nil {
		return err
	}
	defer clearSession(session.Data)
	session.PersistSessionCallback = func(context.Context, *oauth.ClientSessionData) {}
	operationErr := operation(host, did.String(), session)
	persistErr := client.persistTriageSession(ctx, session)
	return errors.Join(operationErr, persistErr)
}

func (client *Client) resolveTriageIdentity(ctx context.Context, rawDID string) (syntax.DID, string, error) {
	did, err := syntax.ParseDID(rawDID)
	if err != nil || did.String() != rawDID {
		return "", "", &triage.ValidationError{Field: "authorDID", Problem: "must be a canonical DID"}
	}
	identity, err := client.directory.LookupDID(ctx, did)
	if err != nil {
		return "", "", &triage.ProviderError{Operation: "identity resolution", Err: err}
	}
	if identity == nil || identity.DID != did {
		return "", "", &triage.ProviderError{Operation: "identity verification", Err: errors.New("resolved DID mismatch")}
	}
	host, err := triagePDSHost(identity)
	if err != nil {
		return "", "", &triage.ProviderError{Operation: "PDS verification", Err: err}
	}
	return did, host, nil
}

func triagePDSHost(identity *indigoidentity.Identity) (string, error) {
	return canonicalPDSHost(identity.PDSEndpoint())
}

func (client *Client) resumeTriageSession(ctx context.Context, did syntax.DID, host string) (*oauth.ClientSession, error) {
	latest, err := client.sessionStore.GetLatestSession(ctx, did)
	if err != nil {
		if errors.Is(err, ErrSessionNotFound) {
			return nil, &triage.AuthorizationError{Err: err}
		}
		return nil, &triage.ProviderError{Operation: "credential load", Err: err}
	}
	if latest == nil {
		return nil, &triage.AuthorizationError{Err: ErrSessionNotFound}
	}
	sessionID := latest.SessionID
	clearSession(latest)
	session, err := client.resumeSession(ctx, did, sessionID)
	sessionID = ""
	if err != nil {
		if errors.Is(err, ErrSessionNotFound) {
			return nil, &triage.AuthorizationError{Err: err}
		}
		return nil, &triage.ProviderError{Operation: "credential resume", Err: err}
	}
	if session == nil || session.Data == nil {
		return nil, &triage.ProviderError{Operation: "credential resume", Err: ErrSessionInvalid}
	}
	if session.Data.AccountDID != did || !samePDSHost(session.Data.HostURL, host) {
		clearSession(session.Data)
		return nil, &triage.ProviderError{Operation: "credential verification", Err: ErrSessionInvalid}
	}
	return session, nil
}

func (client *Client) persistTriageSession(ctx context.Context, session *oauth.ClientSession) error {
	persistCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	if err := client.sessionStore.SaveSession(persistCtx, *session.Data); err != nil {
		return &triage.ProviderError{Operation: "credential persistence", Err: err}
	}
	return nil
}

func decodeTriageJSON(value json.RawMessage, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(value))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return errors.New("record contains trailing JSON")
	}
	return nil
}

func triageStrongRef(ref triage.StrongRef) map[string]any {
	return map[string]any{"uri": ref.URI, "cid": ref.CID}
}
func swapPointer(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}
func formatTriageTime(value time.Time) string {
	return value.UTC().Format(syntax.AtprotoDatetimeLayout)
}
func canonicalTriageTimes(createdAt, updatedAt time.Time) (string, string, error) {
	created, updated := formatTriageTime(createdAt), formatTriageTime(updatedAt)
	if _, err := parseCanonicalTriageTime(created); err != nil {
		return "", "", err
	}
	if _, err := parseCanonicalTriageTime(updated); err != nil {
		return "", "", err
	}
	return created, updated, nil
}
func parseCanonicalTriageTime(value string) (time.Time, error) {
	parsed, err := syntax.ParseDatetimeTime(value)
	if err != nil || formatTriageTime(parsed) != value {
		return time.Time{}, fmt.Errorf("triage datetime is not canonical")
	}
	return parsed, nil
}
func parseOptionalTriageTime(value *string) (*time.Time, error) {
	if value == nil {
		return nil, nil
	}
	parsed, err := parseCanonicalTriageTime(*value)
	if err != nil {
		return nil, err
	}
	return &parsed, nil
}
func sameTriageTime(left, right time.Time) bool {
	return formatTriageTime(left) == formatTriageTime(right)
}
func sameOptionalTriageTime(left, right *time.Time) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return sameTriageTime(*left, *right)
}
func sameLabelRecord(left, right triage.LabelRecord) bool {
	return left.Repository == right.Repository && left.Name == right.Name && left.Color == right.Color && left.Description == right.Description && sameTriageTime(left.CreatedAt, right.CreatedAt) && sameTriageTime(left.UpdatedAt, right.UpdatedAt)
}
func sameMilestoneRecord(left, right triage.MilestoneRecord) bool {
	return left.Repository == right.Repository && left.Title == right.Title && left.Description == right.Description && left.State == right.State && sameOptionalTriageTime(left.DueAt, right.DueAt) && sameOptionalTriageTime(left.ClosedAt, right.ClosedAt) && sameTriageTime(left.CreatedAt, right.CreatedAt) && sameTriageTime(left.UpdatedAt, right.UpdatedAt)
}
func sameMetadataRecord(left, right triage.MetadataRecord) bool {
	return left.Subject == right.Subject && left.Kind == right.Kind && left.Repository == right.Repository && slicesEqual(left.LabelURIs, right.LabelURIs) && slicesEqual(left.AssigneeDIDs, right.AssigneeDIDs) && left.MilestoneURI == right.MilestoneURI && sameTriageTime(left.CreatedAt, right.CreatedAt) && sameTriageTime(left.UpdatedAt, right.UpdatedAt)
}
func slicesEqual(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}
