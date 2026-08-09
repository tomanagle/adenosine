package atproto

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/adenosine-dev/adenosine/internal/repository"
	"github.com/bluesky-social/indigo/atproto/atclient"
	"github.com/bluesky-social/indigo/atproto/auth/oauth"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/google/uuid"
)

type fakeRepositoryAPI struct {
	output    putRecordOutput
	postErr   error
	postInput any
	post      func()
}

func (*fakeRepositoryAPI) Get(context.Context, syntax.NSID, map[string]any, any) error { return nil }

func (api *fakeRepositoryAPI) Post(_ context.Context, _ syntax.NSID, input, output any) error {
	api.postInput = input
	if api.post != nil {
		api.post()
	}
	if api.postErr != nil {
		return api.postErr
	}
	encoded, _ := json.Marshal(api.output)
	return json.Unmarshal(encoded, output)
}

type repositorySessionStore struct {
	latest  oauth.ClientSessionData
	saved   oauth.ClientSessionData
	saveErr error
	calls   int
}

func (store *repositorySessionStore) GetLatestSession(context.Context, syntax.DID) (*oauth.ClientSessionData, error) {
	value := store.latest
	return &value, nil
}

func (store *repositorySessionStore) SaveSession(_ context.Context, session oauth.ClientSessionData) error {
	store.calls++
	store.saved = session
	return store.saveErr
}

func (*repositorySessionStore) DeleteSession(context.Context, syntax.DID, string) error { return nil }

func TestRepositoryPublishUsesStableUUIDKeyAndPersistsRotatedSession(t *testing.T) {
	t.Parallel()
	id := repository.ID(uuid.MustParse("0198a851-2a89-7ae2-a370-dc68883e3af1"))
	rkey := "0198a8512a897ae2a370dc68883e3af1"
	testCases := []struct {
		name string
	}{
		{name: "deterministic publication"},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			did := parsedDID(t)
			data := oauth.ClientSessionData{AccountDID: did, SessionID: "session", HostURL: "https://pds.example", AccessToken: "token"}
			store := &repositorySessionStore{latest: data}
			session := &oauth.ClientSession{Data: &data}
			api := &fakeRepositoryAPI{output: putRecordOutput{URI: "at://" + canonicalDID + "/" + repositoryCollection + "/" + rkey, CID: profileCID}}
			api.post = func() { session.Data.DPoPHostNonce = "rotated" }
			client := &Client{directory: profileIdentity(t, syntax.Handle("mutable.test")), sessionStore: store,
				apiFactory: func(string, atclient.AuthMethod) profileAPI { return api },
				resume:     func(context.Context, syntax.DID, string) (*oauth.ClientSession, error) { return session, nil }}
			createdAt := time.Date(2026, time.August, 9, 12, 0, 0, 0, time.UTC)
			identity, err := client.Publish(context.Background(), repository.Publication{
				ID: id, OwnerDID: canonicalDID, Slug: "project", Name: "project", DefaultBranch: "main",
				GitHTTPS: "https://code.test/did:plc:owner/project.git", GitSSH: "ssh://git@code.test/did:plc:owner/project.git",
				Web: "https://code.test/did:plc:owner/project", CreatedAt: createdAt, UpdatedAt: createdAt,
			})
			if err != nil {
				t.Fatal(err)
			}
			input, ok := api.postInput.(putRecordInput)
			if !ok || input.Repo != canonicalDID || input.RKey != rkey || input.Collection != repositoryCollection {
				t.Fatalf("put input = %#v", api.postInput)
			}
			wantGit := map[string]any{"https": "https://code.test/did:plc:owner/project.git", "ssh": "ssh://git@code.test/did:plc:owner/project.git"}
			if input.Record["slug"] != "project" || !reflect.DeepEqual(input.Record["git"], wantGit) || store.calls != 1 || store.saved.DPoPHostNonce != "rotated" {
				t.Fatalf("record/session = %#v / %#v", input.Record, store.saved)
			}
			if identity.URI != api.output.URI || identity.CID != profileCID {
				t.Fatalf("identity = %#v", identity)
			}
		})
	}
}

func TestRepositoryPublishRejectsNoncanonicalIdentityAndPersistsAfterFailure(t *testing.T) {
	t.Parallel()
	id := repository.ID(uuid.MustParse("0198a851-2a89-7ae2-a370-dc68883e3af1"))
	rkey := "0198a8512a897ae2a370dc68883e3af1"
	testCases := []struct {
		name    string
		output  putRecordOutput
		postErr error
	}{
		{name: "wrong URI", output: putRecordOutput{URI: "at://" + canonicalDID + "/" + repositoryCollection + "/wrong", CID: profileCID}},
		{name: "invalid CID", output: putRecordOutput{URI: "at://" + canonicalDID + "/" + repositoryCollection + "/" + rkey, CID: "invalid"}},
		{name: "provider failure", postErr: errors.New("provider failed")},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			did := parsedDID(t)
			data := oauth.ClientSessionData{AccountDID: did, SessionID: "session", HostURL: "https://pds.example"}
			store := &repositorySessionStore{latest: data}
			session := &oauth.ClientSession{Data: &data}
			api := &fakeRepositoryAPI{output: testCase.output, postErr: testCase.postErr, post: func() { session.Data.DPoPHostNonce = "rotated" }}
			client := &Client{directory: profileIdentity(t, syntax.Handle("mutable.test")), sessionStore: store,
				apiFactory: func(string, atclient.AuthMethod) profileAPI { return api },
				resume:     func(context.Context, syntax.DID, string) (*oauth.ClientSession, error) { return session, nil }}
			_, err := client.Publish(context.Background(), repository.Publication{ID: id, OwnerDID: canonicalDID, Name: "project", DefaultBranch: "main", CreatedAt: time.Now(), UpdatedAt: time.Now()})
			if err == nil || store.calls != 1 || store.saved.DPoPHostNonce != "rotated" {
				t.Fatalf("error/session = %v / %#v", err, store.saved)
			}
		})
	}
}
