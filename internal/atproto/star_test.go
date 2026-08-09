package atproto

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/adenosine-dev/adenosine/internal/star"
	"github.com/bluesky-social/indigo/atproto/atclient"
	"github.com/bluesky-social/indigo/atproto/auth/oauth"
	"github.com/bluesky-social/indigo/atproto/syntax"
)

const (
	starTargetURI = "at://did:plc:ewvi7nxzyoun6zhxrhs64oiz/dev.adenosine.repo/0198a8512a897ae2a370dc68883e3af1"
	starRKey      = "xntfrphq7h5fpvjl3kghtvuib7gchz2lpcj4rd6cmbtbvvetjida"
)

var starCreatedAt = time.Date(2026, time.August, 9, 12, 0, 0, 0, time.UTC)

type starAPICall struct {
	nsid  syntax.NSID
	input any
}

type fakeStarAPI struct {
	getOutputs []getRecordOutput
	getErrors  []error
	getCalls   int
	putOutput  *putRecordOutput
	postErrors []error
	postCalls  []starAPICall
	post       func(int)
}

func (api *fakeStarAPI) Get(_ context.Context, _ syntax.NSID, _ map[string]any, output any) error {
	index := api.getCalls
	api.getCalls++
	if index < len(api.getErrors) && api.getErrors[index] != nil {
		return api.getErrors[index]
	}
	if index >= len(api.getOutputs) {
		return errors.New("unexpected get")
	}
	encoded, _ := json.Marshal(api.getOutputs[index])
	return json.Unmarshal(encoded, output)
}

func (api *fakeStarAPI) Post(_ context.Context, nsid syntax.NSID, input, output any) error {
	index := len(api.postCalls)
	api.postCalls = append(api.postCalls, starAPICall{nsid: nsid, input: input})
	if api.post != nil {
		api.post(index)
	}
	if index < len(api.postErrors) && api.postErrors[index] != nil {
		return api.postErrors[index]
	}
	if nsid == putRecordNSID {
		result := putRecordOutput{URI: starURI(), CID: profileCID}
		if api.putOutput != nil {
			result = *api.putOutput
		}
		encoded, _ := json.Marshal(result)
		return json.Unmarshal(encoded, output)
	}
	return nil
}

type starSessionStore struct {
	latest    *oauth.ClientSessionData
	latestErr error
	saved     oauth.ClientSessionData
	saveErr   error
	saveCalls int
}

func (store *starSessionStore) GetLatestSession(context.Context, syntax.DID) (*oauth.ClientSessionData, error) {
	if store.latestErr != nil {
		return nil, store.latestErr
	}
	if store.latest == nil {
		return nil, nil
	}
	value := *store.latest
	return &value, nil
}

func (store *starSessionStore) SaveSession(_ context.Context, value oauth.ClientSessionData) error {
	store.saveCalls++
	store.saved = value
	return store.saveErr
}

func (*starSessionStore) DeleteSession(context.Context, syntax.DID, string) error { return nil }

func newStarClient(t *testing.T, api *fakeStarAPI, store *starSessionStore) (*Client, *oauth.ClientSession) {
	t.Helper()
	did := parsedDID(t)
	data := oauth.ClientSessionData{AccountDID: did, SessionID: "session-secret", HostURL: "https://pds.example", AccessToken: "token-secret"}
	if store.latest == nil && store.latestErr == nil {
		latest := data
		store.latest = &latest
	}
	session := &oauth.ClientSession{Data: &data}
	client := &Client{
		directory:    profileIdentity(t, syntax.Handle("alice.test")),
		sessionStore: store,
		apiFactory:   func(string, atclient.AuthMethod) profileAPI { return api },
		resume:       func(context.Context, syntax.DID, string) (*oauth.ClientSession, error) { return session, nil },
	}
	return client, session
}

func TestCreateStarUsesCreateOnlyDeterministicRecordAndPersistsRotation(t *testing.T) {
	t.Parallel()
	testCases := []struct {
		name      string
		createdAt time.Time
		wantTime  time.Time
	}{
		{name: "created", createdAt: starCreatedAt, wantTime: starCreatedAt},
		{
			name:      "response uses authoritative millisecond precision",
			createdAt: time.Date(2026, time.August, 9, 12, 0, 0, 123456789, time.UTC),
			wantTime:  time.Date(2026, time.August, 9, 12, 0, 0, 123000000, time.UTC),
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			api := &fakeStarAPI{}
			store := &starSessionStore{}
			client, session := newStarClient(t, api, store)
			api.post = func(int) { session.Data.DPoPHostNonce = "rotated" }
			result, err := client.CreateStar(context.Background(), canonicalDID, starTarget(), testCase.createdAt)
			if err != nil {
				t.Fatal(err)
			}
			if result.URI != starURI() || result.CID != profileCID || result.AuthorDID != canonicalDID || result.Target != starTarget() || !result.CreatedAt.Equal(testCase.wantTime) {
				t.Fatalf("star = %#v", result)
			}
			if len(api.postCalls) != 1 || api.postCalls[0].nsid != putRecordNSID {
				t.Fatalf("post calls = %#v", api.postCalls)
			}
			input, ok := api.postCalls[0].input.(starPutRecordInput)
			if !ok || input.Repo != canonicalDID || input.Collection != star.Collection || input.RKey != starRKey || input.SwapRecord != nil {
				t.Fatalf("put input = %#v", api.postCalls[0].input)
			}
			encoded, _ := json.Marshal(input)
			if !strings.Contains(string(encoded), `"swapRecord":null`) {
				t.Fatalf("put JSON = %s", encoded)
			}
			if store.saveCalls != 1 || store.saved.DPoPHostNonce != "rotated" {
				t.Fatalf("saved session/calls = %#v / %d", store.saved, store.saveCalls)
			}
		})
	}
}

func TestCreateStarRecoversOnlyValidSameTargetInvalidSwap(t *testing.T) {
	t.Parallel()
	otherTarget := star.Target{URI: "at://did:plc:ewvi7nxzyoun6zhxrhs64oiz/dev.adenosine.repo/other", CID: profileCID}
	updatedTarget := star.Target{URI: starTargetURI, CID: "bafybeibwzif5g2dkjaxtv3p67te2g7a2n5xwbn2khoxalctlp7ch5vmo3a"}
	testCases := []struct {
		name       string
		get        getRecordOutput
		wantTarget star.Target
		wantErr    error
	}{
		{name: "same target preserves original creation time", get: starGetOutput(starTarget(), "2026-08-08T10:00:00Z"), wantTarget: starTarget()},
		{name: "same repository with observed older CID preserves star", get: starGetOutput(updatedTarget, "2026-08-08T10:00:00Z"), wantTarget: updatedTarget},
		{name: "different target conflicts", get: starGetOutput(otherTarget, "2026-08-08T10:00:00Z"), wantErr: star.ErrConflict},
		{name: "malformed occupant conflicts", get: starRawOutput(`{"$type":"dev.adenosine.star","subject":{"uri":"bad","cid":"bad"},"createdAt":"2026-08-08T10:00:00Z"}`), wantErr: star.ErrConflict},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			api := &fakeStarAPI{postErrors: []error{invalidSwapError()}, getOutputs: []getRecordOutput{testCase.get}}
			store := &starSessionStore{}
			client, _ := newStarClient(t, api, store)
			result, err := client.CreateStar(context.Background(), canonicalDID, starTarget(), starCreatedAt)
			if testCase.wantErr != nil {
				if !errors.Is(err, testCase.wantErr) || strings.Contains(err.Error(), "provider-secret") {
					t.Fatalf("error = %v", err)
				}
				return
			}
			if err != nil || result.Target != testCase.wantTarget || result.CreatedAt.Format(time.RFC3339) != "2026-08-08T10:00:00Z" {
				t.Fatalf("star/error = %#v / %v", result, err)
			}
		})
	}
}

func TestCreateStarRejectsInvalidResponsesAndRedactsFailures(t *testing.T) {
	t.Parallel()
	testCases := []struct {
		name      string
		output    *putRecordOutput
		postError error
		saveError error
		want      error
	}{
		{name: "provider failure", postError: &atclient.APIError{StatusCode: http.StatusServiceUnavailable, Message: "provider-secret"}, want: star.ErrProvider},
		{name: "persistence failure", saveError: errors.New("database-secret"), want: star.ErrProvider},
		{name: "wrong response URI", output: &putRecordOutput{URI: "at://" + canonicalDID + "/" + star.Collection + "/wrong", CID: profileCID}, want: star.ErrProvider},
		{name: "invalid response CID", output: &putRecordOutput{URI: starURI(), CID: "not-a-cid"}, want: star.ErrProvider},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			api := &fakeStarAPI{putOutput: testCase.output, postErrors: []error{testCase.postError}}
			store := &starSessionStore{saveErr: testCase.saveError}
			client, _ := newStarClient(t, api, store)
			_, err := client.CreateStar(context.Background(), canonicalDID, starTarget(), starCreatedAt)
			if !errors.Is(err, testCase.want) || strings.Contains(err.Error(), "secret") || store.saveCalls != 1 {
				t.Fatalf("error/save calls = %v / %d", err, store.saveCalls)
			}
		})
	}
}

func TestDeleteStarUsesFetchedCIDAndHandlesAbsence(t *testing.T) {
	t.Parallel()
	testCases := []struct {
		name      string
		getErrors []error
		outputs   []getRecordOutput
		postError error
		wantPosts int
	}{
		{name: "absent is success", getErrors: []error{recordNotFoundError()}, wantPosts: 0},
		{name: "compare and swap delete", outputs: []getRecordOutput{starGetOutput(starTarget(), "2026-08-08T10:00:00Z")}, wantPosts: 1},
		{name: "repository CID update does not strand star", outputs: []getRecordOutput{starGetOutput(star.Target{URI: starTargetURI, CID: "bafybeibwzif5g2dkjaxtv3p67te2g7a2n5xwbn2khoxalctlp7ch5vmo3a"}, "2026-08-08T10:00:00Z")}, wantPosts: 1},
		{name: "provider reports already absent", outputs: []getRecordOutput{starGetOutput(starTarget(), "2026-08-08T10:00:00Z")}, postError: recordNotFoundError(), wantPosts: 1},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			api := &fakeStarAPI{getErrors: testCase.getErrors, getOutputs: testCase.outputs, postErrors: []error{testCase.postError}}
			store := &starSessionStore{}
			client, _ := newStarClient(t, api, store)
			if err := client.DeleteStar(context.Background(), canonicalDID, starTarget()); err != nil {
				t.Fatal(err)
			}
			if len(api.postCalls) != testCase.wantPosts || store.saveCalls != 1 {
				t.Fatalf("post/save calls = %d / %d", len(api.postCalls), store.saveCalls)
			}
			if testCase.wantPosts == 1 {
				input, ok := api.postCalls[0].input.(starDeleteRecordInput)
				if !ok || api.postCalls[0].nsid != deleteRecordNSID || input.SwapRecord != profileCID || input.RKey != starRKey {
					t.Fatalf("delete input = %#v", api.postCalls[0])
				}
			}
		})
	}
}

func TestDeleteStarBoundsInvalidSwapRefetchWithoutDeletingRestar(t *testing.T) {
	t.Parallel()
	otherTarget := star.Target{URI: "at://did:plc:ewvi7nxzyoun6zhxrhs64oiz/dev.adenosine.repo/other", CID: profileCID}
	testCases := []struct {
		name      string
		second    getRecordOutput
		secondErr error
		wantErr   error
		wantGets  int
	}{
		{name: "concurrent same-target re-star survives", second: starGetOutputWithCID(starTarget(), "2026-08-09T13:00:00Z", "bafybeibwzif5g2dkjaxtv3p67te2g7a2n5xwbn2khoxalctlp7ch5vmo3a"), wantGets: 2},
		{name: "concurrent deletion is success", secondErr: recordNotFoundError(), wantGets: 2},
		{name: "different occupant conflicts", second: starGetOutput(otherTarget, "2026-08-09T13:00:00Z"), wantErr: star.ErrConflict, wantGets: 2},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			api := &fakeStarAPI{
				getOutputs: []getRecordOutput{starGetOutput(starTarget(), "2026-08-08T10:00:00Z"), testCase.second},
				getErrors:  []error{nil, testCase.secondErr},
				postErrors: []error{invalidSwapError()},
			}
			store := &starSessionStore{}
			client, _ := newStarClient(t, api, store)
			err := client.DeleteStar(context.Background(), canonicalDID, starTarget())
			if !errors.Is(err, testCase.wantErr) || api.getCalls != testCase.wantGets || len(api.postCalls) != 1 {
				t.Fatalf("error/get/post calls = %v / %d / %d", err, api.getCalls, len(api.postCalls))
			}
		})
	}
}

func TestStarOperationsValidateAuthorizationAndSessionHost(t *testing.T) {
	t.Parallel()
	testCases := []struct {
		name      string
		authorDID string
		configure func(*starSessionStore, *oauth.ClientSession)
		want      error
	}{
		{name: "invalid author", authorDID: "alice.test", want: star.ErrValidation},
		{name: "missing latest session", authorDID: canonicalDID, configure: func(store *starSessionStore, _ *oauth.ClientSession) {
			store.latest = nil
			store.latestErr = ErrSessionNotFound
		}, want: star.ErrAuthorization},
		{name: "PDS host mismatch", authorDID: canonicalDID, configure: func(_ *starSessionStore, session *oauth.ClientSession) {
			session.Data.HostURL = "https://attacker.example"
		}, want: star.ErrProvider},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			api := &fakeStarAPI{}
			store := &starSessionStore{}
			client, session := newStarClient(t, api, store)
			if testCase.configure != nil {
				testCase.configure(store, session)
			}
			_, err := client.CreateStar(context.Background(), testCase.authorDID, starTarget(), starCreatedAt)
			if !errors.Is(err, testCase.want) || len(api.postCalls) != 0 {
				t.Fatalf("error/post calls = %v / %d", err, len(api.postCalls))
			}
		})
	}
}

func starTarget() star.Target { return star.Target{URI: starTargetURI, CID: profileCID} }
func starURI() string         { return "at://" + canonicalDID + "/" + star.Collection + "/" + starRKey }

func starGetOutput(target star.Target, createdAt string) getRecordOutput {
	return starGetOutputWithCID(target, createdAt, profileCID)
}

func starGetOutputWithCID(target star.Target, createdAt, cid string) getRecordOutput {
	record, _ := json.Marshal(map[string]any{
		"$type": star.Collection, "subject": map[string]any{"uri": target.URI, "cid": target.CID}, "createdAt": createdAt,
	})
	value := json.RawMessage(record)
	return getRecordOutput{CID: &cid, URI: starURI(), Value: &value}
}

func starRawOutput(value string) getRecordOutput {
	record := json.RawMessage(value)
	cid := profileCID
	return getRecordOutput{CID: &cid, URI: starURI(), Value: &record}
}

func invalidSwapError() error {
	return &atclient.APIError{StatusCode: http.StatusBadRequest, Name: "InvalidSwap", Message: "provider-secret"}
}

func recordNotFoundError() error {
	return &atclient.APIError{StatusCode: http.StatusBadRequest, Name: "RecordNotFound", Message: "provider-secret"}
}
