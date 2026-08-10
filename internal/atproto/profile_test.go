package atproto

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/adenosine-dev/adenosine/internal/profile"
	"github.com/bluesky-social/indigo/atproto/atclient"
	"github.com/bluesky-social/indigo/atproto/auth/oauth"
	indigoidentity "github.com/bluesky-social/indigo/atproto/identity"
	"github.com/bluesky-social/indigo/atproto/syntax"
)

const profileCID = "bafybeigdyrzt5sfp7udm7hu76uh7y26nf3efuylqabf3oclgtqy55fbzdi"

type fakeProfileAPI struct {
	getOutput any
	getErr    error
	postErr   error
	getParams map[string]any
	postInput any
	post      func()
}

func (api *fakeProfileAPI) Get(_ context.Context, _ syntax.NSID, params map[string]any, out any) error {
	api.getParams = params
	if api.getErr != nil {
		return api.getErr
	}
	value, _ := json.Marshal(api.getOutput)
	return json.Unmarshal(value, out)
}

func (api *fakeProfileAPI) Post(_ context.Context, _ syntax.NSID, input, out any) error {
	api.postInput = input
	if api.post != nil {
		api.post()
	}
	if api.postErr != nil {
		return api.postErr
	}
	value, _ := json.Marshal(putRecordOutput{URI: "at://" + canonicalDID + "/dev.adenosine.profile/self", CID: profileCID})
	return json.Unmarshal(value, out)
}

type profileSessionStore struct {
	latest   oauth.ClientSessionData
	saved    oauth.ClientSessionData
	saveErr  error
	saveCall int
}

func (store *profileSessionStore) GetLatestSession(context.Context, syntax.DID) (*oauth.ClientSessionData, error) {
	value := store.latest
	return &value, nil
}
func (store *profileSessionStore) SaveSession(_ context.Context, value oauth.ClientSessionData) error {
	store.saveCall++
	store.saved = value
	return store.saveErr
}
func (*profileSessionStore) DeleteSession(context.Context, syntax.DID, string) error { return nil }

func profileIdentity(t *testing.T, handle syntax.Handle) fakeDirectory {
	t.Helper()
	return fakeDirectory{identity: &indigoidentity.Identity{
		DID: parsedDID(t), Handle: handle,
		Services: map[string]indigoidentity.ServiceEndpoint{
			"atproto_pds": {Type: "AtprotoPersonalDataServer", URL: "https://pds.example"},
		},
	}}
}

func profileClient(t *testing.T, api *fakeProfileAPI, store *profileSessionStore) (*Client, *oauth.ClientSession) {
	t.Helper()
	did := parsedDID(t)
	data := oauth.ClientSessionData{AccountDID: did, SessionID: "session-secret", HostURL: "https://pds.example", AccessToken: "token-secret"}
	if store != nil {
		store.latest = data
	}
	session := &oauth.ClientSession{Data: &data}
	client := &Client{
		directory: profileIdentity(t, syntax.Handle("Alice.Test")), sessionStore: store,
		apiFactory: func(string, atclient.AuthMethod) profileAPI { return api },
		resume:     func(context.Context, syntax.DID, string) (*oauth.ClientSession, error) { return session, nil },
	}
	return client, session
}

func TestProfileGetReturnsStrictCanonicalRecordAndVerifiedHandle(t *testing.T) {
	t.Parallel()
	testCases := []struct{ name string }{{name: "canonical record"}}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			value := json.RawMessage(`{"$type":"dev.adenosine.profile","displayName":"Alice","bio":"Builds things","website":"https://alice.test","location":"Earth","createdAt":"2026-08-09T12:00:00Z"}`)
			api := &fakeProfileAPI{getOutput: getRecordOutput{
				CID: stringPointer(profileCID), URI: "at://" + canonicalDID + "/dev.adenosine.profile/self", Value: &value,
			}}
			client, _ := profileClient(t, api, &profileSessionStore{})
			result, err := client.Get(context.Background(), canonicalDID)
			if err != nil {
				t.Fatal(err)
			}
			if result.DID != canonicalDID || result.Handle != "alice.test" || result.DisplayName != "Alice" || result.CID != profileCID {
				t.Fatalf("profile = %#v", result)
			}
			wantParams := map[string]any{"collection": profileCollection, "repo": canonicalDID, "rkey": profileRKey}
			if !reflect.DeepEqual(api.getParams, wantParams) {
				t.Fatalf("get params = %#v", api.getParams)
			}
		})
	}
}

func TestProfileGetRejectsInvalidProviderEnvelopeAndUnknownRecordFields(t *testing.T) {
	t.Parallel()
	testCases := []struct {
		name  string
		uri   string
		cid   string
		value string
	}{
		{name: "URI", uri: "at://" + canonicalDID + "/dev.adenosine.profile/other", cid: profileCID, value: `{"$type":"dev.adenosine.profile","createdAt":"2026-08-09T12:00:00Z"}`},
		{name: "CID", uri: "at://" + canonicalDID + "/dev.adenosine.profile/self", cid: "not-a-cid", value: `{"$type":"dev.adenosine.profile","createdAt":"2026-08-09T12:00:00Z"}`},
		{name: "createdAt", uri: "at://" + canonicalDID + "/dev.adenosine.profile/self", cid: profileCID, value: `{"$type":"dev.adenosine.profile","createdAt":"2026-08-09T12:00:00+00:00"}`},
		{name: "unknown", uri: "at://" + canonicalDID + "/dev.adenosine.profile/self", cid: profileCID, value: `{"$type":"dev.adenosine.profile","createdAt":"2026-08-09T12:00:00Z","token":"secret"}`},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			value := json.RawMessage(testCase.value)
			api := &fakeProfileAPI{getOutput: getRecordOutput{CID: &testCase.cid, URI: testCase.uri, Value: &value}}
			client, _ := profileClient(t, api, &profileSessionStore{})
			_, err := client.Get(context.Background(), canonicalDID)
			if !errors.Is(err, profile.ErrProvider) || strings.Contains(err.Error(), "secret") {
				t.Fatalf("error = %v", err)
			}
		})
	}
}

func TestProfileGetMapsNotFoundWithoutProviderDetails(t *testing.T) {
	t.Parallel()
	testCases := []struct{ name string }{{name: "record not found"}}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			api := &fakeProfileAPI{getErr: &atclient.APIError{StatusCode: http.StatusBadRequest, Name: "RecordNotFound", Message: "token-secret"}}
			client, _ := profileClient(t, api, &profileSessionStore{})
			_, err := client.Get(context.Background(), canonicalDID)
			if !errors.Is(err, profile.ErrNotFound) || strings.Contains(err.Error(), "token-secret") {
				t.Fatalf("error = %v", err)
			}
		})
	}
}

func TestProfilePutUsesSelfRecordAndExplicitlyPersistsRotations(t *testing.T) {
	t.Parallel()
	testCases := []struct{ name string }{{name: "successful put"}}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			store := &profileSessionStore{}
			api := &fakeProfileAPI{}
			client, session := profileClient(t, api, store)
			api.post = func() { session.Data.DPoPHostNonce = "rotated-nonce" }
			createdAt := time.Date(2026, time.August, 9, 12, 0, 0, 0, time.UTC)
			result, err := client.Put(context.Background(), canonicalDID, profile.Record{DisplayName: "Alice", CreatedAt: createdAt})
			if err != nil {
				t.Fatal(err)
			}
			input, ok := api.postInput.(putRecordInput)
			if !ok || input.Repo != canonicalDID || input.Collection != profileCollection || input.RKey != profileRKey || input.Record["$type"] != profileCollection {
				t.Fatalf("put input = %#v", api.postInput)
			}
			if store.saveCall != 1 || store.saved.DPoPHostNonce != "rotated-nonce" {
				t.Fatalf("saved session = %#v, calls = %d", store.saved, store.saveCall)
			}
			if result.URI != "at://"+canonicalDID+"/dev.adenosine.profile/self" || result.Handle != "alice.test" {
				t.Fatalf("profile = %#v", result)
			}
		})
	}
}

func TestProfilePutSurfacesPersistenceFailureAndRejectsSessionHostMismatch(t *testing.T) {
	t.Parallel()
	testCases := []struct {
		name               string
		saveErr            error
		hostURL            string
		persistenceFailure bool
	}{
		{name: "persistence failure", saveErr: errors.New("database token-secret"), persistenceFailure: true},
		{name: "session host mismatch", hostURL: "https://attacker.example"},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			store := &profileSessionStore{saveErr: testCase.saveErr}
			api := &fakeProfileAPI{}
			client, session := profileClient(t, api, store)
			if testCase.hostURL != "" {
				session.Data.HostURL = testCase.hostURL
			}
			_, err := client.Put(context.Background(), canonicalDID, profile.Record{CreatedAt: time.Now()})
			if testCase.persistenceFailure {
				if !errors.Is(err, profile.ErrProvider) || strings.Contains(err.Error(), "token-secret") || store.saveCall != 1 {
					t.Fatalf("persistence error = %v, calls = %d", err, store.saveCall)
				}
				return
			}
			if !errors.Is(err, profile.ErrProvider) || api.postInput != nil {
				t.Fatalf("host mismatch error/input = %v, %#v", err, api.postInput)
			}
		})
	}
}

func TestProfilePutPersistsRotationsAfterProviderFailure(t *testing.T) {
	t.Parallel()
	testCases := []struct{ name string }{{name: "provider failure"}}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			store := &profileSessionStore{}
			api := &fakeProfileAPI{postErr: &atclient.APIError{StatusCode: http.StatusServiceUnavailable, Message: "token-secret"}}
			client, session := profileClient(t, api, store)
			api.post = func() { session.Data.DPoPHostNonce = "failure-rotation" }
			_, err := client.Put(context.Background(), canonicalDID, profile.Record{CreatedAt: time.Now()})
			if !errors.Is(err, profile.ErrProvider) || strings.Contains(err.Error(), "token-secret") {
				t.Fatalf("provider error = %v", err)
			}
			if store.saveCall != 1 || store.saved.DPoPHostNonce != "failure-rotation" {
				t.Fatalf("saved session = %#v, calls = %d", store.saved, store.saveCall)
			}
		})
	}
}

func TestProfilePutSerializesSameDIDOperations(t *testing.T) {
	testCases := []struct{ name string }{{name: "same DID"}}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			store := &profileSessionStore{}
			api := &fakeProfileAPI{}
			client, _ := profileClient(t, api, store)
			client.resume = func(context.Context, syntax.DID, string) (*oauth.ClientSession, error) {
				data := store.latest
				return &oauth.ClientSession{Data: &data}, nil
			}
			var active atomic.Int32
			var maximum atomic.Int32
			api.post = func() {
				current := active.Add(1)
				if current > maximum.Load() {
					maximum.Store(current)
				}
				time.Sleep(20 * time.Millisecond)
				active.Add(-1)
			}
			var wait sync.WaitGroup
			for range 4 {
				wait.Add(1)
				go func() {
					defer wait.Done()
					if _, err := client.Put(context.Background(), canonicalDID, profile.Record{CreatedAt: time.Now()}); err != nil {
						t.Errorf("put: %v", err)
					}
				}()
			}
			wait.Wait()
			if maximum.Load() != 1 {
				t.Fatalf("maximum concurrent same-DID operations = %d", maximum.Load())
			}
		})
	}
}

func stringPointer(value string) *string { return &value }
