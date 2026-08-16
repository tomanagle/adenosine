package federation

import (
	"errors"
	"strings"
	"testing"

	"github.com/adenosine-dev/adenosine/internal/triage"
)

func TestDecodeRepositoryTriageAuthority(t *testing.T) {
	repositoryURI := "at://" + testDID + "/" + RepositoryCollection + "/project"
	forgedRepositoryURI := "at://" + testBobDID + "/" + RepositoryCollection + "/project"
	testCases := []struct {
		name       string
		repository string
		wantErr    error
	}{
		{name: "accepts repository owner label", repository: repositoryURI},
		{name: "rejects forged repository authority", repository: forgedRepositoryURI, wantErr: ErrInvalidEvent},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			record := `{"$type":"` + RepositoryLabelCollection + `","repository":{"uri":"` + testCase.repository + `","cid":"` + testCID + `"},"name":"bug","color":"a0b1c2","description":"Broken behavior","createdAt":"2026-08-16T01:02:03Z","updatedAt":"2026-08-16T01:02:03Z"}`
			event, err := DecodeEvent([]byte(recordEnvelope(150, RepositoryLabelCollection, "label", "create", record)))
			if !errors.Is(err, testCase.wantErr) {
				t.Fatalf("error = %v, want %v", err, testCase.wantErr)
			}
			if testCase.wantErr == nil && (event.Record == nil || event.Record.RepositoryLabel == nil || event.Record.RepositoryLabel.Name != "bug") {
				t.Fatalf("record = %#v", event.Record)
			}
		})
	}
}

func TestDecodeSubjectTriageIdentity(t *testing.T) {
	repositoryURI := "at://" + testDID + "/" + RepositoryCollection + "/project"
	issueURI := "at://" + testBobDID + "/" + IssueCollection + "/issue"
	rkey, err := triage.MetadataRecordKey(issueURI)
	if err != nil {
		t.Fatal(err)
	}
	testCases := []struct {
		name    string
		rkey    string
		wantErr error
	}{
		{name: "accepts deterministic metadata slot", rkey: rkey},
		{name: "rejects a different metadata slot", rkey: strings.Repeat("a", 52), wantErr: ErrInvalidEvent},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			record := `{"$type":"` + SubjectTriageCollection + `","subject":{"uri":"` + issueURI + `","cid":"` + testCID + `"},"kind":"issue","repository":{"uri":"` + repositoryURI + `","cid":"` + testCID + `"},"labels":[],"assignees":[],"createdAt":"2026-08-16T01:02:03Z","updatedAt":"2026-08-16T01:02:03Z"}`
			event, decodeErr := DecodeEvent([]byte(recordEnvelope(151, SubjectTriageCollection, testCase.rkey, "create", record)))
			if !errors.Is(decodeErr, testCase.wantErr) {
				t.Fatalf("error = %v, want %v", decodeErr, testCase.wantErr)
			}
			if testCase.wantErr == nil && (event.Record == nil || event.Record.SubjectTriage == nil || event.Record.SubjectTriage.Subject.URI != issueURI) {
				t.Fatalf("record = %#v", event.Record)
			}
		})
	}
}
