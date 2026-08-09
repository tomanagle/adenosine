package star

import (
	"errors"
	"strings"
	"testing"
)

const (
	testRepositoryURI = "at://did:plc:ewvi7nxzyoun6zhxrhs64oiz/dev.adenosine.repo/0198a8512a897ae2a370dc68883e3af1"
	testCID           = "bafybeigdyrzt5sfp7udm7hu76uh7y26nf3efuylqabf3oclgtqy55fbzdi"
)

func TestTargetValidate(t *testing.T) {
	t.Parallel()
	testCases := []struct {
		name    string
		target  Target
		wantErr bool
	}{
		{name: "canonical strong reference", target: Target{URI: testRepositoryURI, CID: testCID}},
		{name: "handle authority", target: Target{URI: "at://alice.test/dev.adenosine.repo/key", CID: testCID}, wantErr: true},
		{name: "wrong collection", target: Target{URI: "at://did:plc:ewvi7nxzyoun6zhxrhs64oiz/app.bsky.feed.post/key", CID: testCID}, wantErr: true},
		{name: "missing record key", target: Target{URI: "at://did:plc:ewvi7nxzyoun6zhxrhs64oiz/dev.adenosine.repo", CID: testCID}, wantErr: true},
		{name: "noncanonical CID", target: Target{URI: testRepositoryURI, CID: strings.ToUpper(testCID)}, wantErr: true},
		{name: "invalid CID", target: Target{URI: testRepositoryURI, CID: "not-a-cid"}, wantErr: true},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			err := testCase.target.Validate()
			if (err != nil) != testCase.wantErr || (err != nil && !errors.Is(err, ErrValidation)) {
				t.Fatalf("Validate() error = %v", err)
			}
		})
	}
}

func TestRecordKey(t *testing.T) {
	t.Parallel()
	testCases := []struct {
		name    string
		uri     string
		want    string
		wantErr bool
	}{
		{name: "stable lowercase unpadded base32", uri: testRepositoryURI, want: "xntfrphq7h5fpvjl3kghtvuib7gchz2lpcj4rd6cmbtbvvetjida"},
		{name: "invalid target", uri: "https://example.test/repository", wantErr: true},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			got, err := RecordKey(testCase.uri)
			if (err != nil) != testCase.wantErr || got != testCase.want {
				t.Fatalf("RecordKey() = %q, %v", got, err)
			}
			if strings.Contains(got, "=") || got != strings.ToLower(got) {
				t.Fatalf("record key is not lowercase unpadded base32: %q", got)
			}
		})
	}
}
