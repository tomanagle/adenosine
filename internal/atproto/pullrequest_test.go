package atproto

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/adenosine-dev/adenosine/internal/pullrequest"
)

const pullRequestRKey = "0198a8512a897ae2a370dc68883e3af5"

var pullRequestTime = time.Date(2026, time.August, 10, 12, 0, 0, 0, time.UTC)

func TestCreatePullRequestAndReviewUseExactLexiconFields(t *testing.T) {
	t.Parallel()
	testCases := []struct {
		name       string
		collection string
		invoke     func(*Client) error
		wantFields []string
	}{
		{name: "pull request", collection: pullrequest.Collection, invoke: func(client *Client) error {
			_, err := client.CreatePullRequest(context.Background(), canonicalDID, pullRequestRKey, pullRequestTestRecord())
			return err
		}, wantFields: []string{`"sourceRepository"`, `"targetRepository"`, `"sourceBranch"`, `"targetBranch"`, `"headSHA"`}},
		{name: "review", collection: pullrequest.ReviewCollection, invoke: func(client *Client) error {
			_, err := client.CreatePullRequestReview(context.Background(), canonicalDID, pullRequestRKey, pullRequestReviewTestRecord())
			return err
		}, wantFields: []string{`"subject"`, `"verdict":"approve"`, `"body"`}},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			api := &fakeStarAPI{putOutput: &putRecordOutput{URI: pullRequestEnvelopeURI(testCase.collection, pullRequestRKey), CID: profileCID}}
			store := &starSessionStore{}
			client, _ := newStarClient(t, api, store)
			if err := testCase.invoke(client); err != nil {
				t.Fatalf("publication error = %v", err)
			}
			input, ok := api.postCalls[0].input.(pullRequestPutRecordInput)
			encoded, _ := json.Marshal(input.Record)
			if !ok || input.Collection != testCase.collection || input.RKey != pullRequestRKey || input.SwapRecord != nil {
				t.Fatalf("put input = %#v", api.postCalls[0].input)
			}
			for _, field := range testCase.wantFields {
				if !strings.Contains(string(encoded), field) {
					t.Errorf("record %s does not contain %s", encoded, field)
				}
			}
			if store.saveCalls != 1 {
				t.Fatalf("save calls = %d", store.saveCalls)
			}
		})
	}
}

func TestPullRequestCreateOnlyRetriesRequireExactContent(t *testing.T) {
	t.Parallel()
	changedPR := pullRequestTestRecord()
	changedPR.HeadSHA = strings.Repeat("a", 40)
	changedReview := pullRequestReviewTestRecord()
	changedReview.Verdict = pullrequest.VerdictRequestChanges
	testCases := []struct {
		name    string
		output  getRecordOutput
		invoke  func(*Client) error
		wantErr error
	}{
		{name: "exact pull request retry", output: pullRequestGetOutput(pullRequestTestRecord()), invoke: invokePullRequestCreate},
		{name: "changed pull request conflicts", output: pullRequestGetOutput(changedPR), invoke: invokePullRequestCreate, wantErr: pullrequest.ErrConflict},
		{name: "exact review retry", output: pullRequestReviewGetOutput(pullRequestReviewTestRecord()), invoke: invokePullRequestReviewCreate},
		{name: "changed review conflicts", output: pullRequestReviewGetOutput(changedReview), invoke: invokePullRequestReviewCreate, wantErr: pullrequest.ErrConflict},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			api := &fakeStarAPI{postErrors: []error{invalidSwapError()}, getOutputs: []getRecordOutput{testCase.output}}
			client, _ := newStarClient(t, api, &starSessionStore{})
			err := testCase.invoke(client)
			if !errors.Is(err, testCase.wantErr) {
				t.Fatalf("publication error = %v, want %v", err, testCase.wantErr)
			}
		})
	}
}

func TestPutPullRequestStatusEnforcesOwnerAndCAS(t *testing.T) {
	t.Parallel()
	current := pullRequestStatusTestRecord()
	desired := current
	desired.State = pullrequest.StateOpen
	desired.UpdatedAt = pullRequestTime.Add(time.Minute)
	rkey, _ := pullrequest.StatusRecordKey(current.Subject.URI)
	testCases := []struct {
		name      string
		author    string
		outputs   []getRecordOutput
		record    pullrequest.StatusRecord
		wantPosts int
		wantSwap  *string
		wantErr   error
	}{
		{name: "non-owner rejected before provider", author: "did:plc:mallory", record: current, wantErr: pullrequest.ErrAuthorization},
		{name: "create absent slot", author: canonicalDID, record: current, wantPosts: 1},
		{name: "exact retry avoids write", author: canonicalDID, outputs: []getRecordOutput{pullRequestStatusGetOutput(current)}, record: current},
		{name: "update swaps current CID and preserves creation", author: canonicalDID, outputs: []getRecordOutput{pullRequestStatusGetOutput(current)}, record: desired, wantPosts: 1, wantSwap: stringPointer(profileCID)},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			if testCase.wantErr != nil {
				client := &Client{}
				_, err := client.PutPullRequestStatus(context.Background(), testCase.author, testCase.record)
				if !errors.Is(err, testCase.wantErr) || strings.Contains(err.Error(), "mallory") {
					t.Fatalf("PutPullRequestStatus() error = %v", err)
				}
				return
			}
			api := &fakeStarAPI{getOutputs: testCase.outputs, putOutput: &putRecordOutput{URI: pullRequestEnvelopeURI(pullrequest.StatusCollection, rkey), CID: profileCID}}
			if len(testCase.outputs) == 0 {
				api.getErrors = []error{recordNotFoundError()}
			}
			client, _ := newStarClient(t, api, &starSessionStore{})
			result, err := client.PutPullRequestStatus(context.Background(), testCase.author, testCase.record)
			if err != nil || len(api.postCalls) != testCase.wantPosts {
				t.Fatalf("result/error/posts = %#v/%v/%d", result, err, len(api.postCalls))
			}
			if testCase.wantPosts == 1 {
				input := api.postCalls[0].input.(pullRequestPutRecordInput)
				if !equalStringPointers(input.SwapRecord, testCase.wantSwap) || input.Collection != pullrequest.StatusCollection {
					t.Fatalf("status input = %#v", input)
				}
				if input.Record["createdAt"] != pullRequestTime.Format(time.RFC3339) {
					t.Fatalf("createdAt = %v", input.Record["createdAt"])
				}
			}
		})
	}
}

func TestPullRequestProviderFailuresAndEnvelopesAreRedacted(t *testing.T) {
	t.Parallel()
	testCases := []struct {
		name   string
		output putRecordOutput
	}{
		{name: "wrong URI", output: putRecordOutput{URI: "at://did:plc:other/" + pullrequest.Collection + "/" + pullRequestRKey, CID: profileCID}},
		{name: "wrong CID", output: putRecordOutput{URI: pullRequestEnvelopeURI(pullrequest.Collection, pullRequestRKey), CID: "provider-secret"}},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			api := &fakeStarAPI{putOutput: &testCase.output}
			client, _ := newStarClient(t, api, &starSessionStore{})
			_, err := client.CreatePullRequest(context.Background(), canonicalDID, pullRequestRKey, pullRequestTestRecord())
			if !errors.Is(err, pullrequest.ErrProvider) || strings.Contains(err.Error(), "other") || strings.Contains(err.Error(), "provider-secret") {
				t.Fatalf("CreatePullRequest() error = %v", err)
			}
		})
	}
}

func pullRequestTestRecord() pullrequest.Record {
	return pullrequest.Record{SourceRepository: pullrequest.StrongRef{URI: "at://did:plc:source/dev.adenosine.repo/source", CID: profileCID},
		TargetRepository: pullrequest.StrongRef{URI: "at://" + canonicalDID + "/dev.adenosine.repo/target", CID: profileCID},
		SourceBranch:     "feature/review", TargetBranch: "main", HeadSHA: "0123456789abcdef0123456789abcdef01234567", Title: "Title", Body: "Body", CreatedAt: pullRequestTime, UpdatedAt: pullRequestTime}
}

func pullRequestReviewTestRecord() pullrequest.ReviewRecord {
	return pullrequest.ReviewRecord{Subject: pullrequest.StrongRef{URI: pullRequestSubjectURI(), CID: profileCID}, Verdict: pullrequest.VerdictApprove, Body: "Looks good", CreatedAt: pullRequestTime, UpdatedAt: pullRequestTime}
}

func pullRequestStatusTestRecord() pullrequest.StatusRecord {
	return pullrequest.StatusRecord{Subject: pullrequest.StrongRef{URI: pullRequestSubjectURI(), CID: profileCID}, TargetRepository: pullRequestTestRecord().TargetRepository, State: pullrequest.StateClosed, CreatedAt: pullRequestTime, UpdatedAt: pullRequestTime}
}

func pullRequestSubjectURI() string {
	return "at://did:plc:contributor/" + pullrequest.Collection + "/" + pullRequestRKey
}
func pullRequestEnvelopeURI(collection, rkey string) string {
	return "at://" + canonicalDID + "/" + collection + "/" + rkey
}
func invokePullRequestCreate(client *Client) error {
	_, err := client.CreatePullRequest(context.Background(), canonicalDID, pullRequestRKey, pullRequestTestRecord())
	return err
}
func invokePullRequestReviewCreate(client *Client) error {
	_, err := client.CreatePullRequestReview(context.Background(), canonicalDID, pullRequestRKey, pullRequestReviewTestRecord())
	return err
}

func pullRequestGetOutput(record pullrequest.Record) getRecordOutput {
	value := map[string]any{"$type": pullrequest.Collection, "sourceRepository": pullRequestStrongRef(record.SourceRepository), "targetRepository": pullRequestStrongRef(record.TargetRepository),
		"sourceBranch": record.SourceBranch, "targetBranch": record.TargetBranch, "headSHA": record.HeadSHA, "title": record.Title, "body": record.Body,
		"createdAt": record.CreatedAt.Format(time.RFC3339), "updatedAt": record.UpdatedAt.Format(time.RFC3339)}
	return pullRequestRawOutput(pullrequest.Collection, pullRequestRKey, value)
}

func pullRequestReviewGetOutput(record pullrequest.ReviewRecord) getRecordOutput {
	value := map[string]any{"$type": pullrequest.ReviewCollection, "subject": pullRequestStrongRef(record.Subject), "verdict": record.Verdict, "body": record.Body,
		"createdAt": record.CreatedAt.Format(time.RFC3339), "updatedAt": record.UpdatedAt.Format(time.RFC3339)}
	return pullRequestRawOutput(pullrequest.ReviewCollection, pullRequestRKey, value)
}

func pullRequestStatusGetOutput(record pullrequest.StatusRecord) getRecordOutput {
	rkey, _ := pullrequest.StatusRecordKey(record.Subject.URI)
	value := map[string]any{"$type": pullrequest.StatusCollection, "subject": pullRequestStrongRef(record.Subject), "targetRepository": pullRequestStrongRef(record.TargetRepository),
		"state": record.State, "createdAt": record.CreatedAt.Format(time.RFC3339), "updatedAt": record.UpdatedAt.Format(time.RFC3339)}
	return pullRequestRawOutput(pullrequest.StatusCollection, rkey, value)
}

func pullRequestRawOutput(collection, rkey string, value map[string]any) getRecordOutput {
	encoded, _ := json.Marshal(value)
	raw, cid := json.RawMessage(encoded), profileCID
	return getRecordOutput{URI: pullRequestEnvelopeURI(collection, rkey), CID: &cid, Value: &raw}
}
