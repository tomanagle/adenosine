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
	"github.com/bluesky-social/indigo/atproto/auth/oauth"
	"github.com/bluesky-social/indigo/atproto/syntax"
)

type issueCommentAPI struct {
	*fakeStarAPI
	getParams []map[string]any
}

func (api *issueCommentAPI) Get(ctx context.Context, nsid syntax.NSID, params map[string]any, output any) error {
	api.getParams = append(api.getParams, params)
	return api.fakeStarAPI.Get(ctx, nsid, params, output)
}

type issueCommentSessionStore struct {
	*starSessionStore
	saveContextErr error
}

func (store *issueCommentSessionStore) SaveSession(ctx context.Context, value oauth.ClientSessionData) error {
	store.saveContextErr = ctx.Err()
	return store.starSessionStore.SaveSession(ctx, value)
}

func newIssueCommentClient(t *testing.T, api *fakeStarAPI, store *issueCommentSessionStore) (*Client, *oauth.ClientSession, *issueCommentAPI) {
	t.Helper()
	client, session := newStarClient(t, api, store.starSessionStore)
	commentAPI := &issueCommentAPI{fakeStarAPI: api}
	client.apiFactory = func(string, atclient.AuthMethod) profileAPI { return commentAPI }
	client.sessionStore = store
	return client, session, commentAPI
}

func TestCreateIssueCommentUsesCallerKeyCreateOnlyAndPersistsRotation(t *testing.T) {
	t.Parallel()
	withoutParent := issueTestComment()
	withoutParent.Parent = nil
	testCases := []struct {
		name    string
		rkey    string
		record  issue.CommentRecord
		wantErr error
	}{
		{name: "create-only publication", rkey: issueRKey, record: issueTestComment()},
		{name: "parent is optional", rkey: issueRKey, record: withoutParent},
		{name: "invalid caller key", rkey: "invalid key", record: issueTestComment(), wantErr: issue.ErrValidation},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			api := &fakeStarAPI{putOutput: &putRecordOutput{URI: issueCommentURI(), CID: profileCID}}
			store := &starSessionStore{}
			client, session := newStarClient(t, api, store)
			api.post = func(int) { session.Data.DPoPHostNonce = "rotated" }
			result, err := client.CreateIssueComment(context.Background(), canonicalDID, testCase.rkey, testCase.record)
			if !errors.Is(err, testCase.wantErr) {
				t.Fatalf("CreateIssueComment() error = %v, want %v", err, testCase.wantErr)
			}
			if testCase.wantErr != nil {
				if len(api.postCalls) != 0 || store.saveCalls != 0 {
					t.Fatalf("provider/save calls = %d/%d", len(api.postCalls), store.saveCalls)
				}
				return
			}
			if result.URI != issueCommentURI() || result.CID != profileCID || result.AuthorDID != canonicalDID || !sameIssueCommentRecord(result.CommentRecord, testCase.record) {
				t.Fatalf("comment = %#v", result)
			}
			input, ok := api.postCalls[0].input.(issuePutRecordInput)
			encoded, _ := json.Marshal(input)
			if !ok || input.Repo != canonicalDID || input.Collection != issue.CommentCollection || input.RKey != issueRKey || input.SwapRecord != nil || !strings.Contains(string(encoded), `"swapRecord":null`) {
				t.Fatalf("put input = %#v JSON=%s", api.postCalls[0].input, encoded)
			}
			if testCase.record.Parent != nil && !strings.Contains(string(encoded), `"parent":{"cid":"`+testCase.record.Parent.CID+`","uri":"`+testCase.record.Parent.URI+`"}`) {
				t.Fatalf("put JSON does not preserve parent = %s", encoded)
			}
			if testCase.record.Parent == nil && strings.Contains(string(encoded), `"parent"`) {
				t.Fatalf("put JSON contains absent parent = %s", encoded)
			}
			if store.saveCalls != 1 || store.saved.DPoPHostNonce != "rotated" {
				t.Fatalf("saved session/calls = %#v/%d", store.saved, store.saveCalls)
			}
		})
	}
}

func TestCreateIssueCommentRejectsEventualSelfParent(t *testing.T) {
	t.Parallel()
	testCases := []struct {
		name string
	}{
		{name: "eventual envelope cannot be its own parent"},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			record := issueTestComment()
			record.Parent = &issue.StrongRef{URI: issueCommentURI(), CID: profileCID}
			api := &fakeStarAPI{}
			store := &starSessionStore{}
			client, _ := newStarClient(t, api, store)
			_, err := client.CreateIssueComment(context.Background(), canonicalDID, issueRKey, record)
			if !errors.Is(err, issue.ErrValidation) || len(api.postCalls) != 0 || store.saveCalls != 0 {
				t.Fatalf("error/post/save calls = %v/%d/%d", err, len(api.postCalls), store.saveCalls)
			}
		})
	}
}

func TestCreateIssueCommentRetryRequiresExactContentAndValidEnvelope(t *testing.T) {
	t.Parallel()
	changedBody := issueTestComment()
	changedBody.Body = "occupied"
	changedSubject := issueTestComment()
	changedSubject.Subject.CID = "bafybeibwzif5g2dkjaxtv3p67te2g7a2n5xwbn2khoxalctlp7ch5vmo3a"
	changedParent := issueTestComment()
	changedParent.Parent = &issue.StrongRef{URI: "at://did:plc:bob/" + issue.CommentCollection + "/other", CID: profileCID}
	testCases := []struct {
		name      string
		output    getRecordOutput
		getError  error
		wantErr   error
		wantFound bool
	}{
		{name: "exact retry with extension", output: issueCommentGetOutput(issueTestComment(), map[string]any{"future": true}), wantFound: true},
		{name: "changed body conflicts", output: issueCommentGetOutput(changedBody, nil), wantErr: issue.ErrConflict, wantFound: true},
		{name: "different observed issue CID conflicts", output: issueCommentGetOutput(changedSubject, nil), wantErr: issue.ErrConflict, wantFound: true},
		{name: "different parent conflicts", output: issueCommentGetOutput(changedParent, nil), wantErr: issue.ErrConflict, wantFound: true},
		{name: "malformed occupant conflicts", output: issueCommentRawOutput(`{"$type":"dev.adenosine.issueComment"}`), wantErr: issue.ErrConflict, wantFound: true},
		{name: "disappeared occupant conflicts", getError: recordNotFoundError(), wantErr: issue.ErrConflict},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			api := &fakeStarAPI{postErrors: []error{invalidSwapError()}, getErrors: []error{testCase.getError}}
			if testCase.wantFound {
				api.getOutputs = []getRecordOutput{testCase.output}
			}
			store := &starSessionStore{}
			client, _ := newStarClient(t, api, store)
			result, err := client.CreateIssueComment(context.Background(), canonicalDID, issueRKey, issueTestComment())
			if !errors.Is(err, testCase.wantErr) || (err != nil && strings.Contains(err.Error(), "provider-secret")) {
				t.Fatalf("CreateIssueComment() error = %v, want %v", err, testCase.wantErr)
			}
			if testCase.wantErr == nil && !sameIssueCommentRecord(result.CommentRecord, issueTestComment()) {
				t.Fatalf("comment = %#v", result)
			}
			if len(api.postCalls) != 1 || api.getCalls != 1 || store.saveCalls != 1 {
				t.Fatalf("post/get/save calls = %d/%d/%d", len(api.postCalls), api.getCalls, store.saveCalls)
			}
		})
	}
}

func TestCreateIssueCommentRejectsInvalidResponsesAndRedactsFailures(t *testing.T) {
	t.Parallel()
	testCases := []struct {
		name      string
		output    *putRecordOutput
		postError error
		saveError error
		wantErr   error
	}{
		{name: "provider failure", postError: &atclient.APIError{StatusCode: http.StatusServiceUnavailable, Message: "provider-secret"}, wantErr: issue.ErrProvider},
		{name: "persistence failure", output: &putRecordOutput{URI: issueCommentURI(), CID: profileCID}, saveError: errors.New("database-secret"), wantErr: issue.ErrProvider},
		{name: "wrong response URI", output: &putRecordOutput{URI: "at://" + canonicalDID + "/" + issue.CommentCollection + "/wrong", CID: profileCID}, wantErr: issue.ErrProvider},
		{name: "invalid response CID", output: &putRecordOutput{URI: issueCommentURI(), CID: "not-a-cid"}, wantErr: issue.ErrProvider},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			api := &fakeStarAPI{putOutput: testCase.output, postErrors: []error{testCase.postError}}
			store := &starSessionStore{saveErr: testCase.saveError}
			client, _ := newStarClient(t, api, store)
			_, err := client.CreateIssueComment(context.Background(), canonicalDID, issueRKey, issueTestComment())
			if !errors.Is(err, testCase.wantErr) || strings.Contains(err.Error(), "secret") || store.saveCalls != 1 {
				t.Fatalf("error/save calls = %v/%d", err, store.saveCalls)
			}
		})
	}
}

func TestCreateIssueCommentUsesLatestAuthorizedSession(t *testing.T) {
	t.Parallel()
	testCases := []struct {
		name      string
		authorDID string
		configure func(*starSessionStore, *oauth.ClientSession)
		wantErr   error
	}{
		{name: "invalid author", authorDID: "alice.test", wantErr: issue.ErrValidation},
		{name: "missing latest session", authorDID: canonicalDID, configure: func(store *starSessionStore, _ *oauth.ClientSession) {
			store.latest = nil
			store.latestErr = ErrSessionNotFound
		}, wantErr: issue.ErrAuthorization},
		{name: "latest session PDS host mismatch", authorDID: canonicalDID, configure: func(_ *starSessionStore, session *oauth.ClientSession) {
			session.Data.HostURL = "https://attacker.example"
		}, wantErr: issue.ErrProvider},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			api := &fakeStarAPI{putOutput: &putRecordOutput{URI: issueCommentURI(), CID: profileCID}}
			store := &starSessionStore{}
			client, session := newStarClient(t, api, store)
			if testCase.configure != nil {
				testCase.configure(store, session)
			}
			_, err := client.CreateIssueComment(context.Background(), testCase.authorDID, issueRKey, issueTestComment())
			if !errors.Is(err, testCase.wantErr) || len(api.postCalls) != 0 {
				t.Fatalf("error/post calls = %v/%d", err, len(api.postCalls))
			}
		})
	}
}

func TestDeleteIssueCommentUsesURIAddressAndFetchedCID(t *testing.T) {
	t.Parallel()
	testCases := []struct {
		name       string
		getError   error
		output     getRecordOutput
		postError  error
		wantPosts  int
		cancelPost bool
		wantErr    error
	}{
		{name: "absent is success", getError: recordNotFoundError()},
		{name: "compare and swap delete tolerates extensions", output: issueCommentGetOutput(issueTestComment(), map[string]any{"future": true}), wantPosts: 1},
		{name: "provider reports already absent", output: issueCommentGetOutput(issueTestComment(), nil), postError: recordNotFoundError(), wantPosts: 1},
		{name: "canceled request still persists rotated DPoP", output: issueCommentGetOutput(issueTestComment(), nil), wantPosts: 1, cancelPost: true},
		{name: "invalid comment record conflicts", output: issueCommentRawOutput(`{"$type":"dev.adenosine.issueComment"}`), wantErr: issue.ErrConflict},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			api := &fakeStarAPI{getErrors: []error{testCase.getError}, postErrors: []error{testCase.postError}}
			if testCase.getError == nil {
				api.getOutputs = []getRecordOutput{testCase.output}
			}
			store := &issueCommentSessionStore{starSessionStore: &starSessionStore{}}
			client, session, commentAPI := newIssueCommentClient(t, api, store)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if testCase.cancelPost {
				api.post = func(int) {
					session.Data.DPoPHostNonce = "rotated"
					cancel()
				}
			}
			err := client.DeleteIssueComment(ctx, canonicalDID, issueCommentURI())
			if !errors.Is(err, testCase.wantErr) {
				t.Fatalf("DeleteIssueComment() error = %v, want %v", err, testCase.wantErr)
			}
			if api.getCalls != 1 || len(api.postCalls) != testCase.wantPosts || store.saveCalls != 1 || store.saveContextErr != nil {
				t.Fatalf("get/post/save/context = %d/%d/%d/%v", api.getCalls, len(api.postCalls), store.saveCalls, store.saveContextErr)
			}
			params := commentAPI.getParams[0]
			if params["repo"] != canonicalDID || params["collection"] != issue.CommentCollection || params["rkey"] != issueRKey {
				t.Fatalf("get params = %#v", params)
			}
			if testCase.wantPosts == 1 {
				input, ok := api.postCalls[0].input.(issueCommentDeleteRecordInput)
				if !ok || api.postCalls[0].nsid != deleteRecordNSID || input.Repo != canonicalDID || input.Collection != issue.CommentCollection || input.RKey != issueRKey || input.SwapRecord != profileCID {
					t.Fatalf("delete input = %#v", api.postCalls[0])
				}
			}
			if testCase.cancelPost && store.saved.DPoPHostNonce != "rotated" {
				t.Fatalf("saved session = %#v", store.saved)
			}
		})
	}
}

func TestDeleteIssueCommentBoundsInvalidSwapAndPreservesConcurrentRecord(t *testing.T) {
	t.Parallel()
	updated := issueTestComment()
	updated.Body = "concurrently updated"
	selfParent := issueTestComment()
	selfParent.Parent = &issue.StrongRef{URI: issueCommentURI(), CID: profileCID}
	testCases := []struct {
		name      string
		second    getRecordOutput
		secondErr error
		wantErr   error
	}{
		{name: "concurrent update survives", second: issueCommentGetOutputWithCID(updated, nil, "bafybeibwzif5g2dkjaxtv3p67te2g7a2n5xwbn2khoxalctlp7ch5vmo3a")},
		{name: "concurrent deletion is success", secondErr: recordNotFoundError()},
		{name: "invalid recreated occupant conflicts", second: issueCommentGetOutputWithCID(selfParent, nil, "bafybeibwzif5g2dkjaxtv3p67te2g7a2n5xwbn2khoxalctlp7ch5vmo3a"), wantErr: issue.ErrConflict},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			api := &fakeStarAPI{
				getOutputs: []getRecordOutput{issueCommentGetOutput(issueTestComment(), nil), testCase.second},
				getErrors:  []error{nil, testCase.secondErr},
				postErrors: []error{invalidSwapError()},
			}
			store := &starSessionStore{}
			client, _ := newStarClient(t, api, store)
			err := client.DeleteIssueComment(context.Background(), canonicalDID, issueCommentURI())
			if !errors.Is(err, testCase.wantErr) || api.getCalls != 2 || len(api.postCalls) != 1 || store.saveCalls != 1 {
				t.Fatalf("error/get/post/save = %v/%d/%d/%d", err, api.getCalls, len(api.postCalls), store.saveCalls)
			}
		})
	}
}

func TestDeleteIssueCommentRejectsInvalidOrForeignURIWithoutProviderCalls(t *testing.T) {
	t.Parallel()
	testCases := []struct {
		name       string
		authorDID  string
		commentURI string
		wantErr    error
	}{
		{name: "invalid author DID", authorDID: "alice.test", commentURI: issueCommentURI(), wantErr: issue.ErrValidation},
		{name: "wrong collection", authorDID: canonicalDID, commentURI: issueSubjectURI(), wantErr: issue.ErrValidation},
		{name: "foreign author", authorDID: canonicalDID, commentURI: "at://did:plc:zyxwvutsrqponmlkjihgfedc/" + issue.CommentCollection + "/" + issueRKey, wantErr: issue.ErrAuthorization},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			api := &fakeStarAPI{}
			store := &starSessionStore{}
			client, _ := newStarClient(t, api, store)
			err := client.DeleteIssueComment(context.Background(), testCase.authorDID, testCase.commentURI)
			if !errors.Is(err, testCase.wantErr) || api.getCalls != 0 || len(api.postCalls) != 0 || store.saveCalls != 0 {
				t.Fatalf("error/get/post/save = %v/%d/%d/%d", err, api.getCalls, len(api.postCalls), store.saveCalls)
			}
		})
	}
}

func TestDeleteIssueCommentRedactsProviderAndPersistenceFailures(t *testing.T) {
	t.Parallel()
	testCases := []struct {
		name      string
		getError  error
		postError error
		saveError error
	}{
		{name: "get failure", getError: &atclient.APIError{StatusCode: http.StatusServiceUnavailable, Message: "provider-secret"}},
		{name: "delete failure", postError: &atclient.APIError{StatusCode: http.StatusServiceUnavailable, Message: "provider-secret"}},
		{name: "persistence failure", saveError: errors.New("database-secret")},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			api := &fakeStarAPI{getErrors: []error{testCase.getError}, postErrors: []error{testCase.postError}}
			if testCase.getError == nil {
				api.getOutputs = []getRecordOutput{issueCommentGetOutput(issueTestComment(), nil)}
			}
			store := &starSessionStore{saveErr: testCase.saveError}
			client, _ := newStarClient(t, api, store)
			err := client.DeleteIssueComment(context.Background(), canonicalDID, issueCommentURI())
			if !errors.Is(err, issue.ErrProvider) || strings.Contains(err.Error(), "secret") || store.saveCalls != 1 {
				t.Fatalf("error/save = %v/%d", err, store.saveCalls)
			}
		})
	}
}

func issueTestComment() issue.CommentRecord {
	return issue.CommentRecord{
		Subject: issue.StrongRef{URI: issueSubjectURI(), CID: profileCID},
		Parent:  &issue.StrongRef{URI: "at://did:plc:bob/" + issue.CommentCollection + "/parent", CID: profileCID},
		Body:    "Issue comment", CreatedAt: issueTime, UpdatedAt: issueTime,
	}
}

func issueCommentURI() string {
	return "at://" + canonicalDID + "/" + issue.CommentCollection + "/" + issueRKey
}

func issueCommentGetOutput(record issue.CommentRecord, extension map[string]any) getRecordOutput {
	return issueCommentGetOutputWithCID(record, extension, profileCID)
}

func issueCommentGetOutputWithCID(record issue.CommentRecord, extension map[string]any, cid string) getRecordOutput {
	value := map[string]any{
		"$type": issue.CommentCollection, "subject": strongRefMap(record.Subject), "body": record.Body,
		"createdAt": record.CreatedAt.Format(time.RFC3339), "updatedAt": record.UpdatedAt.Format(time.RFC3339),
	}
	if record.Parent != nil {
		value["parent"] = strongRefMap(*record.Parent)
	}
	for key, item := range extension {
		value[key] = item
	}
	encoded, _ := json.Marshal(value)
	raw := json.RawMessage(encoded)
	return getRecordOutput{URI: issueCommentURI(), CID: &cid, Value: &raw}
}

func issueCommentRawOutput(value string) getRecordOutput {
	raw := json.RawMessage(value)
	cid := profileCID
	return getRecordOutput{URI: issueCommentURI(), CID: &cid, Value: &raw}
}
