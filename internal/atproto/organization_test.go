package atproto

import (
	"context"
	"crypto/sha256"
	"encoding/base32"
	"errors"
	"strings"
	"testing"

	"github.com/adenosine-dev/adenosine/internal/organization"
)

func TestDeleteOrganizationMembership(t *testing.T) {
	t.Parallel()
	organizationIdentity := organization.ATIdentity{URI: "at://" + canonicalDID + "/dev.adenosine.organization/root", CID: profileCID}
	digest := sha256.Sum256([]byte(organizationIdentity.URI))
	rkey := strings.ToLower(base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(digest[:]))
	wantURI := "at://" + canonicalDID + "/" + organizationMembershipCollection + "/" + rkey
	testCases := []struct {
		name       string
		membership organization.ATIdentity
		postErr    error
		wantErr    bool
		wantPosts  int
	}{
		{name: "deletes deterministic public record", membership: organization.ATIdentity{URI: wantURI, CID: profileCID}, wantPosts: 1},
		{name: "missing record is idempotent", membership: organization.ATIdentity{URI: wantURI, CID: profileCID}, postErr: recordNotFoundError(), wantPosts: 1},
		{name: "membership must belong to actor and organization", membership: organization.ATIdentity{URI: "at://" + canonicalDID + "/" + organizationMembershipCollection + "/other", CID: profileCID}, wantErr: true},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			api := &fakeStarAPI{}
			if testCase.postErr != nil {
				api.postErrors = []error{testCase.postErr}
			}
			store := &starSessionStore{}
			client, _ := newStarClient(t, api, store)
			err := client.DeleteOrganizationMembership(context.Background(), canonicalDID, organizationIdentity, testCase.membership)
			if (err != nil) != testCase.wantErr {
				t.Fatalf("DeleteOrganizationMembership() error = %v, want error %t", err, testCase.wantErr)
			}
			if len(api.postCalls) != testCase.wantPosts {
				t.Fatalf("post calls = %d, want %d", len(api.postCalls), testCase.wantPosts)
			}
			if testCase.wantPosts == 1 {
				input, ok := api.postCalls[0].input.(organizationDeleteRecordInput)
				if !ok || api.postCalls[0].nsid != deleteRecordNSID || input.Collection != organizationMembershipCollection || input.Repo != canonicalDID || input.RKey != rkey || input.SwapRecord != testCase.membership.CID {
					t.Fatalf("delete input = %#v", api.postCalls[0])
				}
				if store.saveCalls != 1 {
					t.Fatalf("session saves = %d, want 1", store.saveCalls)
				}
			}
			if testCase.wantErr && !errors.Is(err, ErrProviderFailure) {
				t.Fatalf("error = %v, want provider error", err)
			}
		})
	}
}
