package atproto

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/adenosine-dev/adenosine/internal/issue"
	"github.com/bluesky-social/indigo/atproto/atclient"
)

const (
	issueRKey          = "0198a8512a897ae2a370dc68883e3af1"
	issueRepositoryURI = "at://" + canonicalDID + "/dev.adenosine.repo/0198a8512a897ae2a370dc68883e3af1"
)

var issueTime = time.Date(2026, time.August, 9, 12, 0, 0, 0, time.UTC)

func TestCreateIssueUsesCallerKeyCreateOnlyAndPersistsRotation(t *testing.T) {
	t.Parallel()
	testCases := []struct {
		name    string
		record  issue.Record
		wantErr error
	}{
		{name: "create-only publication", record: issueTestRecord()},
		{name: "invalid caller key", record: issueTestRecord(), wantErr: issue.ErrValidation},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			api := &fakeStarAPI{putOutput: &putRecordOutput{URI: issueURI(), CID: profileCID}}
			store := &starSessionStore{}
			client, session := newStarClient(t, api, store)
			api.post = func(int) { session.Data.DPoPHostNonce = "rotated" }
			rkey := issueRKey
			if testCase.wantErr != nil {
				rkey = "invalid key"
			}
			result, err := client.CreateIssue(context.Background(), canonicalDID, rkey, testCase.record)
			if !errors.Is(err, testCase.wantErr) {
				t.Fatalf("CreateIssue() error = %v, want %v", err, testCase.wantErr)
			}
			if testCase.wantErr != nil {
				if len(api.postCalls) != 0 || store.saveCalls != 0 {
					t.Fatalf("provider/save calls = %d/%d", len(api.postCalls), store.saveCalls)
				}
				return
			}
			if result.URI != issueURI() || result.CID != profileCID || result.AuthorDID != canonicalDID || !sameIssueRecord(result.Record, testCase.record) {
				t.Fatalf("issue = %#v", result)
			}
			input, ok := api.postCalls[0].input.(issuePutRecordInput)
			encoded, _ := json.Marshal(input)
			if !ok || input.Collection != issue.Collection || input.RKey != issueRKey || input.SwapRecord != nil || !strings.Contains(string(encoded), `"swapRecord":null`) {
				t.Fatalf("put input = %#v JSON=%s", api.postCalls[0].input, encoded)
			}
			if store.saveCalls != 1 || store.saved.DPoPHostNonce != "rotated" {
				t.Fatalf("saved session/calls = %#v/%d", store.saved, store.saveCalls)
			}
		})
	}
}

func TestCreateIssueRetryRequiresExactContentAndValidEnvelope(t *testing.T) {
	t.Parallel()
	changed := issueTestRecord()
	changed.Title = "occupied"
	oldCID := issueTestRecord()
	oldCID.Repository.CID = "bafybeibwzif5g2dkjaxtv3p67te2g7a2n5xwbn2khoxalctlp7ch5vmo3a"
	testCases := []struct {
		name    string
		output  getRecordOutput
		wantErr error
	}{
		{name: "exact retry with extension", output: issueGetOutput(issueTestRecord(), map[string]any{"future": true})},
		{name: "changed content conflicts", output: issueGetOutput(changed, nil), wantErr: issue.ErrConflict},
		{name: "different observed repository CID conflicts", output: issueGetOutput(oldCID, nil), wantErr: issue.ErrConflict},
		{name: "malformed occupant conflicts", output: issueRawOutput(`{"$type":"dev.adenosine.issue"}`), wantErr: issue.ErrConflict},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			api := &fakeStarAPI{postErrors: []error{invalidSwapError()}, getOutputs: []getRecordOutput{testCase.output}}
			store := &starSessionStore{}
			client, _ := newStarClient(t, api, store)
			result, err := client.CreateIssue(context.Background(), canonicalDID, issueRKey, issueTestRecord())
			if !errors.Is(err, testCase.wantErr) || (err != nil && strings.Contains(err.Error(), "provider-secret")) {
				t.Fatalf("CreateIssue() error = %v, want %v", err, testCase.wantErr)
			}
			if testCase.wantErr == nil && !sameIssueRecord(result.Record, issueTestRecord()) {
				t.Fatalf("issue = %#v", result)
			}
			if len(api.postCalls) != 1 || api.getCalls != 1 || store.saveCalls != 1 {
				t.Fatalf("post/get/save calls = %d/%d/%d", len(api.postCalls), api.getCalls, store.saveCalls)
			}
		})
	}
}

func TestPutIssueStatusRejectsNonOwnerBeforeProvider(t *testing.T) {
	t.Parallel()
	testCases := []struct {
		name      string
		authorDID string
		wantErr   error
	}{
		{name: "non-owner cannot close", authorDID: "did:plc:mallory", wantErr: issue.ErrAuthorization},
		{name: "non-owner cannot reopen", authorDID: "did:plc:mallory", wantErr: issue.ErrAuthorization},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			record := issueTestStatus()
			if strings.Contains(testCase.name, "reopen") {
				record.State = issue.StateOpen
			}
			client := &Client{}
			_, err := client.PutIssueStatus(context.Background(), testCase.authorDID, record)
			if !errors.Is(err, testCase.wantErr) || strings.Contains(err.Error(), "mallory") {
				t.Fatalf("PutIssueStatus() error = %v", err)
			}
		})
	}
}

func TestPutIssueStatusUsesGetThenCreateOrUpdateCAS(t *testing.T) {
	t.Parallel()
	oldReferences := issueTestStatus()
	oldReferences.Subject.CID = "bafybeibwzif5g2dkjaxtv3p67te2g7a2n5xwbn2khoxalctlp7ch5vmo3a"
	oldReferences.Repository.CID = oldReferences.Subject.CID
	testCases := []struct {
		name       string
		getErrors  []error
		getOutputs []getRecordOutput
		record     issue.StatusRecord
		wantSwap   *string
		wantPosts  int
		wantErr    error
	}{
		{name: "create uses explicit absent CAS", getErrors: []error{recordNotFoundError()}, record: issueTestStatus(), wantPosts: 1},
		{name: "exact retry returns current without write", getOutputs: []getRecordOutput{statusGetOutput(issueTestStatus(), nil)}, record: issueTestStatus()},
		{name: "update uses current CID", getOutputs: []getRecordOutput{statusGetOutput(issueTestStatus(), nil)}, record: statusAt(issue.StateOpen, issueTime.Add(time.Second)), wantSwap: stringPointer(profileCID), wantPosts: 1},
		{name: "old observed CIDs retain URI identity", getOutputs: []getRecordOutput{statusGetOutput(oldReferences, map[string]any{"extension": "accepted"})}, record: statusAt(issue.StateOpen, issueTime.Add(time.Second)), wantSwap: stringPointer(profileCID), wantPosts: 1},
		{name: "stale update cannot overwrite newer status", getOutputs: []getRecordOutput{statusGetOutput(statusAt(issue.StateClosed, issueTime.Add(2*time.Second)), nil)}, record: statusAt(issue.StateOpen, issueTime.Add(time.Second)), wantErr: issue.ErrConflict},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			rkey, _ := issue.StatusRecordKey(issueSubjectURI())
			api := &fakeStarAPI{
				getErrors: testCase.getErrors, getOutputs: testCase.getOutputs,
				putOutput: &putRecordOutput{URI: statusURI(rkey), CID: profileCID},
			}
			store := &starSessionStore{}
			client, _ := newStarClient(t, api, store)
			result, err := client.PutIssueStatus(context.Background(), canonicalDID, testCase.record)
			if !errors.Is(err, testCase.wantErr) {
				t.Fatalf("PutIssueStatus() error = %v, want %v", err, testCase.wantErr)
			}
			if testCase.wantErr != nil {
				if len(api.postCalls) != 0 {
					t.Fatalf("stale status made %d provider writes", len(api.postCalls))
				}
				return
			}
			if len(api.postCalls) != testCase.wantPosts {
				t.Fatalf("post calls = %d, want %d", len(api.postCalls), testCase.wantPosts)
			}
			if testCase.wantPosts == 1 {
				input, ok := api.postCalls[0].input.(issuePutRecordInput)
				if !ok || input.Collection != issue.StatusCollection || input.RKey != rkey || !equalStringPointers(input.SwapRecord, testCase.wantSwap) {
					t.Fatalf("put input = %#v", api.postCalls[0].input)
				}
			}
			if result.State != testCase.record.State || api.getCalls != 1 || store.saveCalls != 1 {
				t.Fatalf("result/get/post/save = %#v/%d/%d/%d", result, api.getCalls, len(api.postCalls), store.saveCalls)
			}
		})
	}
}

func TestPutIssueStatusBoundsInvalidSwapAndRedactsFailures(t *testing.T) {
	t.Parallel()
	current := statusAt(issue.StateOpen, issueTime)
	desired := statusAt(issue.StateClosed, issueTime.Add(time.Second))
	concurrent := statusAt(issue.StateOpen, issueTime.Add(2*time.Second))
	testCases := []struct {
		name       string
		postError  error
		getOutputs []getRecordOutput
		saveError  error
		wantErr    error
		wantGets   int
	}{
		{name: "lost response returns exact retry", postError: invalidSwapError(), getOutputs: []getRecordOutput{statusGetOutput(current, nil), statusGetOutput(desired, nil)}, wantGets: 2},
		{name: "concurrent state change conflicts", postError: invalidSwapError(), getOutputs: []getRecordOutput{statusGetOutput(current, nil), statusGetOutput(concurrent, nil)}, wantErr: issue.ErrConflict, wantGets: 2},
		{name: "provider failure is redacted", postError: &atclient.APIError{StatusCode: http.StatusServiceUnavailable, Message: "provider-secret"}, getOutputs: []getRecordOutput{statusGetOutput(current, nil)}, wantErr: issue.ErrProvider, wantGets: 1},
		{name: "persistence failure is redacted", getOutputs: []getRecordOutput{statusGetOutput(current, nil)}, saveError: errors.New("database-secret"), wantErr: issue.ErrProvider, wantGets: 1},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			rkey, _ := issue.StatusRecordKey(issueSubjectURI())
			api := &fakeStarAPI{getOutputs: testCase.getOutputs, postErrors: []error{testCase.postError}, putOutput: &putRecordOutput{URI: statusURI(rkey), CID: profileCID}}
			store := &starSessionStore{saveErr: testCase.saveError}
			client, _ := newStarClient(t, api, store)
			_, err := client.PutIssueStatus(context.Background(), canonicalDID, desired)
			if !errors.Is(err, testCase.wantErr) || (err != nil && strings.Contains(err.Error(), "secret")) {
				t.Fatalf("PutIssueStatus() error = %v, want %v", err, testCase.wantErr)
			}
			if api.getCalls != testCase.wantGets || len(api.postCalls) != 1 || store.saveCalls != 1 {
				t.Fatalf("get/post/save calls = %d/%d/%d", api.getCalls, len(api.postCalls), store.saveCalls)
			}
		})
	}
}

func issueTestRecord() issue.Record {
	return issue.Record{Repository: issue.StrongRef{URI: issueRepositoryURI, CID: profileCID}, Title: "Issue title", Body: "Issue body", CreatedAt: issueTime, UpdatedAt: issueTime}
}

func issueTestStatus() issue.StatusRecord {
	return issue.StatusRecord{
		Subject: issue.StrongRef{URI: issueSubjectURI(), CID: profileCID}, Repository: issue.StrongRef{URI: issueRepositoryURI, CID: profileCID},
		State: issue.StateClosed, CreatedAt: issueTime, UpdatedAt: issueTime,
	}
}

func statusAt(state issue.State, updatedAt time.Time) issue.StatusRecord {
	record := issueTestStatus()
	record.State = state
	record.UpdatedAt = updatedAt
	return record
}

func issueSubjectURI() string { return "at://did:plc:reporter/" + issue.Collection + "/" + issueRKey }
func issueURI() string        { return "at://" + canonicalDID + "/" + issue.Collection + "/" + issueRKey }
func statusURI(rkey string) string {
	return "at://" + canonicalDID + "/" + issue.StatusCollection + "/" + rkey
}

func issueGetOutput(record issue.Record, extension map[string]any) getRecordOutput {
	value := map[string]any{
		"$type": issue.Collection, "repository": strongRefMap(record.Repository), "title": record.Title, "body": record.Body,
		"createdAt": record.CreatedAt.Format(time.RFC3339), "updatedAt": record.UpdatedAt.Format(time.RFC3339),
	}
	for key, item := range extension {
		value[key] = item
	}
	encoded, _ := json.Marshal(value)
	raw := json.RawMessage(encoded)
	cid := profileCID
	return getRecordOutput{URI: issueURI(), CID: &cid, Value: &raw}
}

func statusGetOutput(record issue.StatusRecord, extension map[string]any) getRecordOutput {
	rkey, _ := issue.StatusRecordKey(record.Subject.URI)
	value := map[string]any{
		"$type": issue.StatusCollection, "subject": strongRefMap(record.Subject), "repository": strongRefMap(record.Repository), "state": record.State,
		"createdAt": record.CreatedAt.Format(time.RFC3339), "updatedAt": record.UpdatedAt.Format(time.RFC3339),
	}
	for key, item := range extension {
		value[key] = item
	}
	encoded, _ := json.Marshal(value)
	raw := json.RawMessage(encoded)
	cid := profileCID
	return getRecordOutput{URI: statusURI(rkey), CID: &cid, Value: &raw}
}

func issueRawOutput(value string) getRecordOutput {
	raw := json.RawMessage(value)
	cid := profileCID
	return getRecordOutput{URI: issueURI(), CID: &cid, Value: &raw}
}

func equalStringPointers(left, right *string) bool {
	return left == nil && right == nil || left != nil && right != nil && *left == *right
}
