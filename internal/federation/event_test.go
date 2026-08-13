package federation

import (
	"errors"
	"strings"
	"testing"

	"github.com/adenosine-dev/adenosine/internal/issue"
	"github.com/adenosine-dev/adenosine/internal/pullrequest"
	"github.com/adenosine-dev/adenosine/internal/star"
)

const (
	testDID = "did:plc:ewvi7nxzyoun6zhxrhs64oiz"
	testCID = "bafybeigdyrzt5sfp7udm7hu76uh7y26nf3efuylqabf3oclgtqy55fbzdi"
)

func TestDecodeEventAcceptsOfficialTapEvents(t *testing.T) {
	t.Parallel()
	testCases := []struct {
		name       string
		body       string
		wantType   string
		wantAction string
	}{
		{
			name: "profile create",
			body: recordEnvelope(1, ProfileCollection, "self", "create",
				`{"$type":"dev.adenosine.profile","displayName":"Alice","createdAt":"2026-08-09T12:00:00Z"}`),
			wantType: "record", wantAction: "create",
		},
		{
			name: "repository update",
			body: recordEnvelope(2, RepositoryCollection, "project", "update",
				`{"$type":"dev.adenosine.repo","slug":"project","name":"Project","defaultBranch":"main","git":{"https":"https://code.example/project.git","ssh":"ssh://git@code.example/project.git"},"web":"https://code.example/project","createdAt":"2026-08-09T12:00:00Z","updatedAt":"2026-08-09T13:00:00Z"}`),
			wantType: "record", wantAction: "update",
		},
		{
			name: "development repository on loopback HTTP",
			body: recordEnvelope(4, RepositoryCollection, "project", "create",
				`{"$type":"dev.adenosine.repo","slug":"project","name":"Project","defaultBranch":"main","git":{"https":"http://127.0.0.1:58080/project.git"},"web":"http://127.0.0.1:58080/project","createdAt":"2026-08-09T12:00:00Z","updatedAt":"2026-08-09T12:00:00Z"}`),
			wantType: "record", wantAction: "create",
		},
		{
			name:     "identity",
			body:     `{"id":3,"type":"identity","identity":{"did":"` + testDID + `","handle":"alice.example","is_active":true,"status":"active"}}`,
			wantType: "identity",
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			event, err := DecodeEvent([]byte(testCase.body))
			if err != nil {
				t.Fatal(err)
			}
			if event.Type != testCase.wantType {
				t.Fatalf("type = %q", event.Type)
			}
			if event.Record != nil && event.Record.Action != testCase.wantAction {
				t.Fatalf("action = %q", event.Record.Action)
			}
		})
	}
}

func TestDecodeEventRejectsUnknownCollectionRecordFields(t *testing.T) {
	t.Parallel()
	repositoryURI := "at://" + testDID + "/" + RepositoryCollection + "/project"
	issueURI := "at://" + testBobDID + "/" + IssueCollection + "/issue1"
	pullRequestURI := "at://" + testBobDID + "/" + PullRequestCollection + "/pr1"
	starRKey, err := star.RecordKey(repositoryURI)
	if err != nil {
		t.Fatal(err)
	}
	issueStatusRKey, err := issue.StatusRecordKey(issueURI)
	if err != nil {
		t.Fatal(err)
	}
	pullRequestStatusRKey, err := pullrequest.StatusRecordKey(pullRequestURI)
	if err != nil {
		t.Fatal(err)
	}
	pullRequestReviewRequestRKey, err := pullrequest.ReviewRequestRecordKey(pullRequestURI, testBobDID)
	if err != nil {
		t.Fatal(err)
	}
	unknownTopLevel := func(record string) string {
		return strings.TrimSuffix(record, "}") + `,"unexpected":true}`
	}
	validProfile := `{"$type":"dev.adenosine.profile","createdAt":"2026-08-09T12:00:00Z"}`
	validRepository := repositoryRecord("Project")
	validIssue := issueRecord(repositoryURI, "Title")
	testCases := []struct {
		name       string
		collection string
		rkey       string
		record     string
	}{
		{name: "profile top level", collection: ProfileCollection, rkey: "self", record: unknownTopLevel(validProfile)},
		{name: "repository top level", collection: RepositoryCollection, rkey: "project", record: unknownTopLevel(validRepository)},
		{name: "star top level", collection: StarCollection, rkey: starRKey, record: unknownTopLevel(starRecord(repositoryURI, testCID, "2026-08-09T12:00:00Z"))},
		{name: "issue top level", collection: IssueCollection, rkey: "issue1", record: unknownTopLevel(validIssue)},
		{name: "issue comment top level", collection: issue.CommentCollection, rkey: "comment1", record: unknownTopLevel(issueCommentRecord(issueURI, "", "Comment"))},
		{name: "issue status top level", collection: IssueStatusCollection, rkey: issueStatusRKey, record: unknownTopLevel(issueStatusRecord(issueURI, repositoryURI, "open"))},
		{name: "pull request top level", collection: PullRequestCollection, rkey: "pr1", record: unknownTopLevel(pullRequestRecord(repositoryURI, repositoryURI, "Title"))},
		{name: "pull request status top level", collection: PullRequestStatusCollection, rkey: pullRequestStatusRKey, record: unknownTopLevel(pullRequestStatusRecord(pullRequestURI, repositoryURI, "open", ""))},
		{name: "pull request review top level", collection: PullRequestReviewCollection, rkey: "review1", record: unknownTopLevel(pullRequestReviewRecord(pullRequestURI, "comment", "Review"))},
		{name: "pull request review request top level", collection: PullRequestReviewRequestCollection, rkey: pullRequestReviewRequestRKey, record: unknownTopLevel(pullRequestReviewRequestRecord(pullRequestURI, repositoryURI, testBobDID, testDID))},
		{name: "repository git object", collection: RepositoryCollection, rkey: "project", record: strings.Replace(validRepository, `"git":{"https":"https://code.example/project.git"}`, `"git":{"https":"https://code.example/project.git","unexpected":true}`, 1)},
		{name: "issue repository strong ref", collection: IssueCollection, rkey: "issue1", record: strings.Replace(validIssue, `"cid":"`+testCID+`"`, `"cid":"`+testCID+`","unexpected":true`, 1)},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			body := recordEnvelopeForDID(27, testBobDID, testCase.collection, testCase.rkey, "create", testCase.record)
			if eventID, ok := EventID([]byte(body)); !ok || eventID != 27 {
				t.Fatalf("EventID() = %d, %t", eventID, ok)
			}
			_, err := DecodeEvent([]byte(body))
			if !errors.Is(err, ErrInvalidEvent) {
				t.Fatalf("DecodeEvent() error = %v", err)
			}
		})
	}
}

func TestDecodeEventRejectsMalformedOrNoncanonicalInput(t *testing.T) {
	t.Parallel()
	validProfile := `{"$type":"dev.adenosine.profile","createdAt":"2026-08-09T12:00:00Z"}`
	validRepository := `{"$type":"dev.adenosine.repo","slug":"project","name":"Project","defaultBranch":"main","git":{"https":"https://code.example/project.git"},"web":"https://code.example/project","createdAt":"2026-08-09T12:00:00Z","updatedAt":"2026-08-09T12:00:00Z"}`
	testCases := []struct {
		name string
		body string
	}{
		{name: "unknown envelope field", body: strings.TrimSuffix(recordEnvelope(1, ProfileCollection, "self", "create", validProfile), "}") + `,"extra":true}`},
		{name: "unknown Tap record field", body: strings.Replace(recordEnvelope(1, ProfileCollection, "self", "create", validProfile), `"live":true`, `"live":true,"extra":true`, 1)},
		{name: "profile is not self", body: recordEnvelope(1, ProfileCollection, "other", "create", validProfile)},
		{name: "noncanonical datetime", body: recordEnvelope(1, ProfileCollection, "self", "create", strings.Replace(validProfile, "Z", "+00:00", 1))},
		{name: "missing repository slug", body: recordEnvelope(1, RepositoryCollection, "project", "create", strings.Replace(validRepository, `"slug":"project",`, "", 1))},
		{name: "unsafe web URL", body: recordEnvelope(1, RepositoryCollection, "project", "create", strings.Replace(validRepository, "https://code.example/project\"", "https://user@code.example/project\"", 1))},
		{name: "wrong record type", body: recordEnvelope(1, ProfileCollection, "self", "create", strings.Replace(validProfile, ProfileCollection, RepositoryCollection, 1))},
		{name: "legacy identity casing", body: `{"id":3,"type":"identity","identity":{"did":"` + testDID + `","handle":"alice.example","isActive":true,"status":"active"}}`},
		{name: "oversized", body: strings.Repeat(" ", maxEventBytes+1)},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			_, err := DecodeEvent([]byte(testCase.body))
			if !errors.Is(err, ErrInvalidEvent) {
				t.Fatalf("error = %v", err)
			}
		})
	}
}

func TestDecodeEventValidatesStarStrongReferencesAndDeterministicKeys(t *testing.T) {
	t.Parallel()
	repositoryURI := "at://" + testDID + "/" + RepositoryCollection + "/project"
	rkey, err := star.RecordKey(repositoryURI)
	if err != nil {
		t.Fatal(err)
	}
	validRecord := starRecord(repositoryURI, testCID, "2026-08-09T12:00:00Z")
	testCases := []struct {
		name      string
		action    string
		rkey      string
		record    string
		wantError bool
	}{
		{name: "create", action: "create", rkey: rkey, record: validRecord},
		{name: "update observed CID", action: "update", rkey: rkey, record: validRecord},
		{name: "delete", action: "delete", rkey: rkey},
		{name: "delete with non-deterministic key", action: "delete", rkey: "wrong", wantError: true},
		{name: "wrong deterministic key", action: "create", rkey: "wrong", record: validRecord, wantError: true},
		{name: "handle authority", action: "create", rkey: rkey, record: starRecord("at://alice.example/"+RepositoryCollection+"/project", testCID, "2026-08-09T12:00:00Z"), wantError: true},
		{name: "wrong collection", action: "create", rkey: rkey, record: starRecord("at://"+testDID+"/app.bsky.feed.post/project", testCID, "2026-08-09T12:00:00Z"), wantError: true},
		{name: "noncanonical CID", action: "create", rkey: rkey, record: starRecord(repositoryURI, strings.ToUpper(testCID), "2026-08-09T12:00:00Z"), wantError: true},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			_, err := DecodeEvent([]byte(recordEnvelope(40, StarCollection, testCase.rkey, testCase.action, testCase.record)))
			if (err != nil) != testCase.wantError {
				t.Fatalf("DecodeEvent() error = %v, wantError = %t", err, testCase.wantError)
			}
		})
	}
}

func TestValidateWebEndpointAcceptsProductionAndLoopbackURLs(t *testing.T) {
	t.Parallel()
	testCases := []struct {
		name  string
		value string
	}{
		{name: "HTTPS DNS host", value: "https://code.example/project.git"},
		{name: "HTTPS custom port", value: "https://code.example:8443/group/project"},
		{name: "HTTPS private IPv4", value: "https://10.0.0.8/project"},
		{name: "HTTPS IPv6", value: "https://[2001:db8::1]/project"},
		{name: "HTTP IPv4 loopback", value: "http://127.0.0.1:58080/project.git"},
		{name: "HTTP IPv6 loopback", value: "http://[::1]:58080/project.git"},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			if err := validateWebEndpoint(testCase.value); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestValidateWebEndpointRejectsUnsafeURLs(t *testing.T) {
	t.Parallel()
	testCases := []struct {
		name  string
		value string
	}{
		{name: "relative", value: "/project"},
		{name: "HTTP DNS host", value: "http://code.example/project"},
		{name: "HTTP localhost name", value: "http://localhost/project"},
		{name: "HTTP private IP", value: "http://10.0.0.8/project"},
		{name: "userinfo", value: "https://user@code.example/project"},
		{name: "query", value: "https://code.example/project?ref=main"},
		{name: "empty query", value: "https://code.example/project?"},
		{name: "fragment", value: "https://code.example/project#readme"},
		{name: "control character", value: "https://code.example/pro\nject"},
		{name: "encoded control character", value: "https://code.example/pro%0Aject"},
		{name: "backslash", value: `https://code.example/group\project`},
		{name: "empty path", value: "https://code.example"},
		{name: "root path", value: "https://code.example/"},
		{name: "dot segment", value: "https://code.example/group/../project"},
		{name: "encoded dot", value: "https://code.example/group/%2e%2e/project"},
		{name: "encoded slash", value: "https://code.example/group%2Fproject"},
		{name: "encoded backslash", value: "https://code.example/group%5Cproject"},
		{name: "empty segment", value: "https://code.example/group//project"},
		{name: "trailing slash", value: "https://code.example/project/"},
		{name: "malformed escape", value: "https://code.example/project%"},
		{name: "invalid host label", value: "https://code_.example/project"},
		{name: "empty port", value: "https://code.example:/project"},
		{name: "default HTTPS port", value: "https://code.example:443/project"},
		{name: "default HTTP port", value: "http://127.0.0.1:80/project"},
		{name: "leading zero port", value: "https://code.example:08443/project"},
		{name: "out of range port", value: "https://code.example:65536/project"},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			if err := validateWebEndpoint(testCase.value); err == nil {
				t.Fatalf("validateWebEndpoint(%q) succeeded", testCase.value)
			}
		})
	}
}

func TestValidateGitSSHEndpointAcceptsSafeURIs(t *testing.T) {
	t.Parallel()
	testCases := []struct {
		name  string
		value string
	}{
		{name: "git username", value: "ssh://git@code.example/project.git"},
		{name: "no username", value: "ssh://code.example/project.git"},
		{name: "custom port", value: "ssh://git@code.example:2222/group/project.git"},
		{name: "private host", value: "ssh://git@10.0.0.8/project.git"},
		{name: "IPv6 host", value: "ssh://git@[2001:db8::1]/project.git"},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			if err := validateGitSSHEndpoint(testCase.value); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestValidateGitSSHEndpointRejectsUnsafeURIs(t *testing.T) {
	t.Parallel()
	testCases := []struct {
		name  string
		value string
	}{
		{name: "scp syntax", value: "git@code.example:project.git"},
		{name: "wrong scheme", value: "https://code.example/project.git"},
		{name: "wrong username", value: "ssh://admin@code.example/project.git"},
		{name: "encoded username", value: "ssh://g%69t@code.example/project.git"},
		{name: "password", value: "ssh://git:secret@code.example/project.git"},
		{name: "query", value: "ssh://git@code.example/project.git?ref=main"},
		{name: "fragment", value: "ssh://git@code.example/project.git#main"},
		{name: "missing host", value: "ssh:///project.git"},
		{name: "invalid host", value: "ssh://git@-code.example/project.git"},
		{name: "root path", value: "ssh://git@code.example/"},
		{name: "dot segment", value: "ssh://git@code.example/group/../project.git"},
		{name: "encoded slash", value: "ssh://git@code.example/group%2Fproject.git"},
		{name: "backslash", value: `ssh://git@code.example/group\project.git`},
		{name: "default port", value: "ssh://git@code.example:22/project.git"},
		{name: "malformed port", value: "ssh://git@code.example:port/project.git"},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			if err := validateGitSSHEndpoint(testCase.value); err == nil {
				t.Fatalf("validateGitSSHEndpoint(%q) succeeded", testCase.value)
			}
		})
	}
}

func TestValidDefaultBranchMatchesGitRefRules(t *testing.T) {
	t.Parallel()
	testCases := []struct {
		name  string
		value string
		want  bool
	}{
		{name: "main", value: "main", want: true},
		{name: "nested", value: "feature/endpoint-hardening", want: true},
		{name: "punctuation", value: "release-1.2_3", want: true},
		{name: "Unicode", value: "feature/caf\u00e9", want: true},
		{name: "empty", value: "", want: false},
		{name: "at sign only", value: "@", want: false},
		{name: "leading dash", value: "-unsafe", want: false},
		{name: "leading slash", value: "/main", want: false},
		{name: "trailing slash", value: "main/", want: false},
		{name: "double slash", value: "feature//work", want: false},
		{name: "dot component", value: "feature/.hidden", want: false},
		{name: "dot lock suffix", value: "feature/main.lock", want: false},
		{name: "double dot", value: "release..next", want: false},
		{name: "reflog syntax", value: "main@{1}", want: false},
		{name: "trailing dot", value: "main.", want: false},
		{name: "space", value: "feature work", want: false},
		{name: "tilde", value: "main~1", want: false},
		{name: "caret", value: "main^", want: false},
		{name: "colon", value: "feature:work", want: false},
		{name: "question mark", value: "feature?", want: false},
		{name: "asterisk", value: "feature*", want: false},
		{name: "open bracket", value: "feature[1", want: false},
		{name: "backslash", value: `feature\work`, want: false},
		{name: "control", value: "feature\nwork", want: false},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			if got := validDefaultBranch(testCase.value); got != testCase.want {
				t.Fatalf("validDefaultBranch(%q) = %t, want %t", testCase.value, got, testCase.want)
			}
		})
	}
}

func TestDecodeRepositoryRecordRejectsReversedTimestamps(t *testing.T) {
	t.Parallel()
	testCases := []struct {
		name      string
		createdAt string
		updatedAt string
		wantError bool
	}{
		{name: "equal", createdAt: "2026-08-09T12:00:00Z", updatedAt: "2026-08-09T12:00:00Z"},
		{name: "updated later", createdAt: "2026-08-09T12:00:00Z", updatedAt: "2026-08-09T13:00:00Z"},
		{name: "updated earlier", createdAt: "2026-08-09T12:00:00Z", updatedAt: "2026-08-09T11:59:59Z", wantError: true},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			record := `{"$type":"dev.adenosine.repo","slug":"project","name":"Project","defaultBranch":"main","git":{"https":"https://code.example/project.git"},"web":"https://code.example/project","createdAt":"` + testCase.createdAt + `","updatedAt":"` + testCase.updatedAt + `"}`
			_, err := decodeRepositoryRecord([]byte(record))
			if (err != nil) != testCase.wantError {
				t.Fatalf("error = %v, wantError = %t", err, testCase.wantError)
			}
		})
	}
}

func recordEnvelope(id int64, collection, rkey, action, record string) string {
	cidAndRecord := `,"cid":"` + testCID + `","record":` + record
	if action == "delete" {
		cidAndRecord = ""
	}
	return `{"id":` + integerString(id) + `,"type":"record","record":{"live":true,"rev":"3kzfcijpj2z2a","did":"` + testDID + `","collection":"` + collection + `","rkey":"` + rkey + `","action":"` + action + `"` + cidAndRecord + `}}`
}

func integerString(value int64) string {
	if value == 0 {
		return "0"
	}
	digits := make([]byte, 0, 20)
	for value > 0 {
		digits = append(digits, byte('0'+value%10))
		value /= 10
	}
	for left, right := 0, len(digits)-1; left < right; left, right = left+1, right-1 {
		digits[left], digits[right] = digits[right], digits[left]
	}
	return string(digits)
}

func starRecord(repositoryURI, repositoryCID, createdAt string) string {
	return `{"$type":"dev.adenosine.star","subject":{"uri":"` + repositoryURI + `","cid":"` + repositoryCID + `"},"createdAt":"` + createdAt + `"}`
}
