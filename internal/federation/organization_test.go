package federation

import (
	"crypto/sha256"
	"encoding/base32"
	"strings"
	"testing"
)

func TestDecodeOrganizationRecords(t *testing.T) {
	organizationURI := "at://" + testDID + "/" + OrganizationCollection + "/0123456789abcdef0123456789abcdef"
	grantURI := "at://" + testDID + "/" + OrganizationGrantCollection + "/1123456789abcdef0123456789abcdef"
	digest := sha256.Sum256([]byte(organizationURI))
	membershipKey := strings.ToLower(base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(digest[:]))
	organizationRef := `{"uri":"` + organizationURI + `","cid":"` + testCID + `"}`
	grantRef := `{"uri":"` + grantURI + `","cid":"` + testCID + `"}`
	testCases := []struct {
		name, did, collection, rkey, record string
		wantKind                            string
		wantError                           bool
	}{
		{name: "organization root", did: testDID, collection: OrganizationCollection, rkey: "0123456789abcdef0123456789abcdef", record: `{"$type":"` + OrganizationCollection + `","slug":"adenosine","name":"Adenosine","createdAt":"2026-08-09T12:00:00Z","updatedAt":"2026-08-09T13:00:00Z"}`, wantKind: "organization"},
		{name: "owner grant", did: testDID, collection: OrganizationGrantCollection, rkey: "1123456789abcdef0123456789abcdef", record: `{"$type":"` + OrganizationGrantCollection + `","organization":` + organizationRef + `,"subject":"` + testBobDID + `","role":"member","authority":` + organizationRef + `,"createdAt":"2026-08-09T12:00:00Z","expiresAt":"2026-08-16T12:00:00Z"}`, wantKind: "grant"},
		{name: "public member consent", did: testBobDID, collection: OrganizationMembershipCollection, rkey: membershipKey, record: `{"$type":"` + OrganizationMembershipCollection + `","organization":` + organizationRef + `,"grant":` + grantRef + `,"visibility":"public","createdAt":"2026-08-09T12:00:00Z","updatedAt":"2026-08-09T12:00:00Z"}`, wantKind: "membership"},
		{name: "revocation", did: testDID, collection: OrganizationRevocationCollection, rkey: "2123456789abcdef0123456789abcdef", record: `{"$type":"` + OrganizationRevocationCollection + `","organization":` + organizationRef + `,"grant":` + grantRef + `,"subject":"` + testBobDID + `","authority":` + organizationRef + `,"createdAt":"2026-08-09T14:00:00Z"}`, wantKind: "revocation"},
		{name: "membership key binds organization", did: testBobDID, collection: OrganizationMembershipCollection, rkey: strings.Repeat("a", 52), record: `{"$type":"` + OrganizationMembershipCollection + `","organization":` + organizationRef + `,"grant":` + grantRef + `,"visibility":"public","createdAt":"2026-08-09T12:00:00Z","updatedAt":"2026-08-09T12:00:00Z"}`, wantError: true},
		{name: "private membership is never a public record", did: testBobDID, collection: OrganizationMembershipCollection, rkey: membershipKey, record: `{"$type":"` + OrganizationMembershipCollection + `","organization":` + organizationRef + `,"grant":` + grantRef + `,"visibility":"private","createdAt":"2026-08-09T12:00:00Z","updatedAt":"2026-08-09T12:00:00Z"}`, wantError: true},
		{name: "grant subject must be a DID", did: testDID, collection: OrganizationGrantCollection, rkey: "3123456789abcdef0123456789abcdef", record: `{"$type":"` + OrganizationGrantCollection + `","organization":` + organizationRef + `,"subject":"alice.example","role":"member","authority":` + organizationRef + `,"createdAt":"2026-08-09T12:00:00Z"}`, wantError: true},
	}

	for index, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			event, err := DecodeEvent([]byte(recordEnvelopeForDID(int64(index+1), testCase.did, testCase.collection, testCase.rkey, "create", testCase.record)))
			if testCase.wantError {
				if err == nil {
					t.Fatal("DecodeEvent() error = nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("DecodeEvent() error = %v", err)
			}
			kind := ""
			switch {
			case event.Record.Organization != nil:
				kind = "organization"
			case event.Record.OrganizationGrant != nil:
				kind = "grant"
			case event.Record.OrganizationMembership != nil:
				kind = "membership"
			case event.Record.OrganizationRevocation != nil:
				kind = "revocation"
			}
			if kind != testCase.wantKind {
				t.Fatalf("decoded kind = %q, want %q", kind, testCase.wantKind)
			}
		})
	}
}
