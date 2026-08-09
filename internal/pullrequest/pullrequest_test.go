package pullrequest

import (
	"errors"
	"strings"
	"testing"
	"time"
)

const (
	testSourceRepositoryURI = "at://did:plc:source/dev.adenosine.repo/0198a8512a897ae2a370dc68883e3af1"
	testTargetRepositoryURI = "at://did:plc:target/dev.adenosine.repo/0198a8512a897ae2a370dc68883e3af2"
	testPullRequestURI      = "at://did:plc:contributor/dev.adenosine.pullRequest/0198a8512a897ae2a370dc68883e3af3"
	testReviewURI           = "at://did:plc:reviewer/dev.adenosine.pullRequestReview/0198a8512a897ae2a370dc68883e3af4"
	testCID                 = "bafybeigdyrzt5sfp7udm7hu76uh7y26nf3efuylqabf3oclgtqy55fbzdi"
	testSHA1                = "0123456789abcdef0123456789abcdef01234567"
	testSHA256              = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
)

func TestRecordValidation(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 8, 9, 12, 0, 0, 0, time.UTC)
	valid := Record{SourceRepository: StrongRef{URI: testSourceRepositoryURI, CID: testCID}, TargetRepository: StrongRef{URI: testTargetRepositoryURI, CID: testCID}, SourceBranch: "feature/pr", TargetBranch: "main", HeadSHA: testSHA1, Title: "title", Body: "body", CreatedAt: now, UpdatedAt: now}
	testCases := []struct {
		name   string
		mutate func(*Record)
		want   error
	}{
		{name: "valid SHA-1"},
		{name: "valid SHA-256", mutate: func(value *Record) { value.HeadSHA = testSHA256 }},
		{name: "same repository different branch", mutate: func(value *Record) { value.SourceRepository = value.TargetRepository }},
		{name: "different repository same branch", mutate: func(value *Record) { value.SourceBranch = value.TargetBranch }},
		{name: "same repository and branch", mutate: func(value *Record) {
			value.SourceRepository = value.TargetRepository
			value.SourceBranch = value.TargetBranch
		}, want: ErrValidation},
		{name: "wrong source collection", mutate: func(value *Record) { value.SourceRepository.URI = testPullRequestURI }, want: ErrValidation},
		{name: "missing source rkey", mutate: func(value *Record) { value.SourceRepository.URI = "at://did:plc:source/dev.adenosine.repo" }, want: ErrValidation},
		{name: "handle source authority", mutate: func(value *Record) { value.SourceRepository.URI = "at://source.example/dev.adenosine.repo/key" }, want: ErrValidation},
		{name: "noncanonical source URI", mutate: func(value *Record) { value.SourceRepository.URI = "AT://did:plc:source/dev.adenosine.repo/key" }, want: ErrValidation},
		{name: "noncanonical source CID", mutate: func(value *Record) { value.SourceRepository.CID = strings.ToUpper(testCID) }, want: ErrValidation},
		{name: "wrong target collection", mutate: func(value *Record) { value.TargetRepository.URI = testPullRequestURI }, want: ErrValidation},
		{name: "malformed target CID", mutate: func(value *Record) { value.TargetRepository.CID = "not-a-cid" }, want: ErrValidation},
		{name: "short SHA", mutate: func(value *Record) { value.HeadSHA = strings.Repeat("a", 39) }, want: ErrValidation},
		{name: "intermediate SHA length", mutate: func(value *Record) { value.HeadSHA = strings.Repeat("a", 41) }, want: ErrValidation},
		{name: "long SHA", mutate: func(value *Record) { value.HeadSHA = strings.Repeat("a", 65) }, want: ErrValidation},
		{name: "uppercase SHA", mutate: func(value *Record) { value.HeadSHA = strings.ToUpper(testSHA1) }, want: ErrValidation},
		{name: "nonhex SHA", mutate: func(value *Record) { value.HeadSHA = strings.Repeat("g", 40) }, want: ErrValidation},
		{name: "null SHA-1", mutate: func(value *Record) { value.HeadSHA = strings.Repeat("0", 40) }, want: ErrValidation},
		{name: "null SHA-256", mutate: func(value *Record) { value.HeadSHA = strings.Repeat("0", 64) }, want: ErrValidation},
		{name: "empty title", mutate: func(value *Record) { value.Title = "" }, want: ErrValidation},
		{name: "title at maximum", mutate: func(value *Record) { value.Title = strings.Repeat("x", 255) }},
		{name: "title too long", mutate: func(value *Record) { value.Title = strings.Repeat("x", 256) }, want: ErrValidation},
		{name: "title invalid UTF-8", mutate: func(value *Record) { value.Title = string([]byte{0xff}) }, want: ErrValidation},
		{name: "body invalid UTF-8", mutate: func(value *Record) { value.Body = string([]byte{0xff}) }, want: ErrValidation},
		{name: "body at maximum", mutate: func(value *Record) { value.Body = strings.Repeat("x", 65535) }},
		{name: "body too long", mutate: func(value *Record) { value.Body = strings.Repeat("x", 65536) }, want: ErrValidation},
		{name: "missing created timestamp", mutate: func(value *Record) { value.CreatedAt = time.Time{} }, want: ErrValidation},
		{name: "missing updated timestamp", mutate: func(value *Record) { value.UpdatedAt = time.Time{} }, want: ErrValidation},
		{name: "updated before created", mutate: func(value *Record) { value.UpdatedAt = now.Add(-time.Second) }, want: ErrValidation},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			value := valid
			if testCase.mutate != nil {
				testCase.mutate(&value)
			}
			if err := value.Validate(); !errors.Is(err, testCase.want) {
				t.Fatalf("Validate() error = %v, want %v", err, testCase.want)
			}
		})
	}
}

func TestBranchValidation(t *testing.T) {
	t.Parallel()
	testCases := []struct {
		name  string
		value string
		want  error
	}{
		{name: "simple", value: "main"},
		{name: "nested", value: "feature/pull-request"},
		{name: "punctuation", value: "release-1.2_3"},
		{name: "Unicode", value: "feature/caf\u00e9"},
		{name: "maximum bytes", value: strings.Repeat("a", 255)},
		{name: "empty", value: "", want: ErrValidation},
		{name: "too long", value: strings.Repeat("a", 256), want: ErrValidation},
		{name: "invalid UTF-8", value: string([]byte{0xff}), want: ErrValidation},
		{name: "at sign only", value: "@", want: ErrValidation},
		{name: "leading dash", value: "-unsafe", want: ErrValidation},
		{name: "leading slash", value: "/main", want: ErrValidation},
		{name: "trailing slash", value: "main/", want: ErrValidation},
		{name: "double slash", value: "feature//work", want: ErrValidation},
		{name: "leading dot component", value: "feature/.hidden", want: ErrValidation},
		{name: "lock suffix", value: "feature/main.lock", want: ErrValidation},
		{name: "double dot", value: "release..next", want: ErrValidation},
		{name: "reflog syntax", value: "main@{1}", want: ErrValidation},
		{name: "trailing dot", value: "main.", want: ErrValidation},
		{name: "ASCII space", value: "feature work", want: ErrValidation},
		{name: "Unicode whitespace", value: "feature\u00a0work", want: ErrValidation},
		{name: "tilde", value: "main~1", want: ErrValidation},
		{name: "caret", value: "main^", want: ErrValidation},
		{name: "colon", value: "feature:work", want: ErrValidation},
		{name: "question mark", value: "feature?", want: ErrValidation},
		{name: "asterisk", value: "feature*", want: ErrValidation},
		{name: "open bracket", value: "feature[1", want: ErrValidation},
		{name: "backslash", value: `feature\work`, want: ErrValidation},
		{name: "control", value: "feature\nwork", want: ErrValidation},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			if err := validateBranch(testCase.value, "branch"); !errors.Is(err, testCase.want) {
				t.Fatalf("validateBranch(%q) error = %v, want %v", testCase.value, err, testCase.want)
			}
		})
	}
}

func TestStatusRecordValidation(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 8, 9, 12, 0, 0, 0, time.UTC)
	valid := StatusRecord{Subject: StrongRef{URI: testPullRequestURI, CID: testCID}, TargetRepository: StrongRef{URI: testTargetRepositoryURI, CID: testCID}, State: StateOpen, CreatedAt: now, UpdatedAt: now}
	testCases := []struct {
		name   string
		mutate func(*StatusRecord)
		want   error
	}{
		{name: "open"},
		{name: "closed", mutate: func(value *StatusRecord) { value.State = StateClosed }},
		{name: "merged SHA-1", mutate: func(value *StatusRecord) { value.State = StateMerged; value.MergeCommitSHA = testSHA1 }},
		{name: "merged SHA-256", mutate: func(value *StatusRecord) { value.State = StateMerged; value.MergeCommitSHA = testSHA256 }},
		{name: "unknown state", mutate: func(value *StatusRecord) { value.State = "draft" }, want: ErrValidation},
		{name: "merged without SHA", mutate: func(value *StatusRecord) { value.State = StateMerged }, want: ErrValidation},
		{name: "merged malformed SHA", mutate: func(value *StatusRecord) { value.State = StateMerged; value.MergeCommitSHA = strings.Repeat("A", 40) }, want: ErrValidation},
		{name: "merged null SHA", mutate: func(value *StatusRecord) { value.State = StateMerged; value.MergeCommitSHA = strings.Repeat("0", 40) }, want: ErrValidation},
		{name: "open with SHA", mutate: func(value *StatusRecord) { value.MergeCommitSHA = testSHA1 }, want: ErrValidation},
		{name: "closed with SHA", mutate: func(value *StatusRecord) { value.State = StateClosed; value.MergeCommitSHA = testSHA1 }, want: ErrValidation},
		{name: "wrong subject collection", mutate: func(value *StatusRecord) { value.Subject.URI = testTargetRepositoryURI }, want: ErrValidation},
		{name: "malformed subject CID", mutate: func(value *StatusRecord) { value.Subject.CID = "bad" }, want: ErrValidation},
		{name: "wrong target collection", mutate: func(value *StatusRecord) { value.TargetRepository.URI = testPullRequestURI }, want: ErrValidation},
		{name: "missing created timestamp", mutate: func(value *StatusRecord) { value.CreatedAt = time.Time{} }, want: ErrValidation},
		{name: "missing updated timestamp", mutate: func(value *StatusRecord) { value.UpdatedAt = time.Time{} }, want: ErrValidation},
		{name: "updated before created", mutate: func(value *StatusRecord) { value.UpdatedAt = now.Add(-time.Second) }, want: ErrValidation},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			value := valid
			if testCase.mutate != nil {
				testCase.mutate(&value)
			}
			if err := value.Validate(); !errors.Is(err, testCase.want) {
				t.Fatalf("Validate() error = %v, want %v", err, testCase.want)
			}
		})
	}
}

func TestReviewRecordValidation(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 8, 9, 12, 0, 0, 0, time.UTC)
	valid := ReviewRecord{Subject: StrongRef{URI: testPullRequestURI, CID: testCID}, Verdict: VerdictComment, Body: "review", CreatedAt: now, UpdatedAt: now}
	testCases := []struct {
		name   string
		mutate func(*ReviewRecord)
		want   error
	}{
		{name: "comment"},
		{name: "approve", mutate: func(value *ReviewRecord) { value.Verdict = VerdictApprove }},
		{name: "request changes", mutate: func(value *ReviewRecord) { value.Verdict = VerdictRequestChanges }},
		{name: "unknown verdict", mutate: func(value *ReviewRecord) { value.Verdict = "reject" }, want: ErrValidation},
		{name: "wrong subject collection", mutate: func(value *ReviewRecord) { value.Subject.URI = testTargetRepositoryURI }, want: ErrValidation},
		{name: "noncanonical subject CID", mutate: func(value *ReviewRecord) { value.Subject.CID = strings.ToUpper(testCID) }, want: ErrValidation},
		{name: "body at maximum", mutate: func(value *ReviewRecord) { value.Body = strings.Repeat("x", 65535) }},
		{name: "body too long", mutate: func(value *ReviewRecord) { value.Body = strings.Repeat("x", 65536) }, want: ErrValidation},
		{name: "body invalid UTF-8", mutate: func(value *ReviewRecord) { value.Body = string([]byte{0xff}) }, want: ErrValidation},
		{name: "missing created timestamp", mutate: func(value *ReviewRecord) { value.CreatedAt = time.Time{} }, want: ErrValidation},
		{name: "missing updated timestamp", mutate: func(value *ReviewRecord) { value.UpdatedAt = time.Time{} }, want: ErrValidation},
		{name: "updated before created", mutate: func(value *ReviewRecord) { value.UpdatedAt = now.Add(-time.Second) }, want: ErrValidation},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			value := valid
			if testCase.mutate != nil {
				testCase.mutate(&value)
			}
			if err := value.Validate(); !errors.Is(err, testCase.want) {
				t.Fatalf("Validate() error = %v, want %v", err, testCase.want)
			}
		})
	}
}

func TestEnvelopeValidationAndAuthority(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 8, 9, 12, 0, 0, 0, time.UTC)
	statusKey, _ := StatusRecordKey(testPullRequestURI)
	validRecord := Record{SourceRepository: StrongRef{URI: testSourceRepositoryURI, CID: testCID}, TargetRepository: StrongRef{URI: testTargetRepositoryURI, CID: testCID}, SourceBranch: "feature", TargetBranch: "main", HeadSHA: testSHA1, Title: "title", Body: "body", CreatedAt: now, UpdatedAt: now}
	validStatus := StatusRecord{Subject: StrongRef{URI: testPullRequestURI, CID: testCID}, TargetRepository: StrongRef{URI: testTargetRepositoryURI, CID: testCID}, State: StateOpen, CreatedAt: now, UpdatedAt: now}
	validReview := ReviewRecord{Subject: StrongRef{URI: testPullRequestURI, CID: testCID}, Verdict: VerdictApprove, Body: "review", CreatedAt: now, UpdatedAt: now}
	testCases := []struct {
		name  string
		value interface{ Validate() error }
		want  error
	}{
		{name: "contributor owns pull request despite different source owner", value: PullRequest{URI: testPullRequestURI, CID: testCID, AuthorDID: "did:plc:contributor", Record: validRecord}},
		{name: "source owner may author pull request", value: PullRequest{URI: "at://did:plc:source/" + Collection + "/key", CID: testCID, AuthorDID: "did:plc:source", Record: validRecord}},
		{name: "pull request envelope mismatch", value: PullRequest{URI: testPullRequestURI, CID: testCID, AuthorDID: "did:plc:mallory", Record: validRecord}, want: ErrAuthorization},
		{name: "pull request malformed author DID", value: PullRequest{URI: testPullRequestURI, CID: testCID, AuthorDID: "contributor.example", Record: validRecord}, want: ErrValidation},
		{name: "pull request malformed CID", value: PullRequest{URI: testPullRequestURI, CID: "bad", AuthorDID: "did:plc:contributor", Record: validRecord}, want: ErrValidation},
		{name: "target owner owns status", value: Status{URI: "at://did:plc:target/" + StatusCollection + "/" + statusKey, CID: testCID, AuthorDID: "did:plc:target", StatusRecord: validStatus}},
		{name: "status envelope mismatch", value: Status{URI: "at://did:plc:target/" + StatusCollection + "/" + statusKey, CID: testCID, AuthorDID: "did:plc:mallory", StatusRecord: validStatus}, want: ErrAuthorization},
		{name: "non-target owner status", value: Status{URI: "at://did:plc:source/" + StatusCollection + "/" + statusKey, CID: testCID, AuthorDID: "did:plc:source", StatusRecord: validStatus}, want: ErrAuthorization},
		{name: "wrong status key", value: Status{URI: "at://did:plc:target/" + StatusCollection + "/wrong", CID: testCID, AuthorDID: "did:plc:target", StatusRecord: validStatus}, want: ErrValidation},
		{name: "reviewer owns review without target authority", value: Review{URI: testReviewURI, CID: testCID, AuthorDID: "did:plc:reviewer", ReviewRecord: validReview}},
		{name: "contributor may review", value: Review{URI: "at://did:plc:contributor/" + ReviewCollection + "/key", CID: testCID, AuthorDID: "did:plc:contributor", ReviewRecord: validReview}},
		{name: "review envelope mismatch", value: Review{URI: testReviewURI, CID: testCID, AuthorDID: "did:plc:mallory", ReviewRecord: validReview}, want: ErrAuthorization},
		{name: "review wrong collection", value: Review{URI: testPullRequestURI, CID: testCID, AuthorDID: "did:plc:contributor", ReviewRecord: validReview}, want: ErrValidation},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			if err := testCase.value.Validate(); !errors.Is(err, testCase.want) {
				t.Fatalf("Validate() error = %v, want %v", err, testCase.want)
			}
		})
	}
}

func TestRecordKeys(t *testing.T) {
	t.Parallel()
	testCases := []struct {
		name    string
		uri     string
		want    string
		wantErr bool
	}{
		{name: "stable lowercase unpadded base32", uri: testPullRequestURI, want: "o7ubbajqktfoors6nqm6vnpvtznhqismdm3dvbttxfec7hmmglda"},
		{name: "wrong collection", uri: testTargetRepositoryURI, wantErr: true},
		{name: "missing rkey", uri: "at://did:plc:contributor/dev.adenosine.pullRequest", wantErr: true},
		{name: "handle authority", uri: "at://contributor.example/dev.adenosine.pullRequest/key", wantErr: true},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			got, err := StatusRecordKey(testCase.uri)
			if (err != nil) != testCase.wantErr || got != testCase.want {
				t.Fatalf("StatusRecordKey() = %q, %v, want %q", got, err, testCase.want)
			}
			if err == nil && (got != strings.ToLower(got) || strings.Contains(got, "=")) {
				t.Fatalf("status key is not lowercase unpadded base32: %q", got)
			}
		})
	}
}

func TestRandomAndCallerRecordKeys(t *testing.T) {
	t.Parallel()
	testCases := []struct {
		name      string
		value     string
		generated bool
		want      error
	}{
		{name: "generated compact UUIDv7", generated: true},
		{name: "valid caller key", value: "review-1"},
		{name: "empty", value: "", want: ErrValidation},
		{name: "slash", value: "bad/key", want: ErrValidation},
		{name: "space", value: "bad key", want: ErrValidation},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			value := testCase.value
			if testCase.generated {
				var err error
				value, err = RandomRecordKey()
				if err != nil || len(value) != 32 || value != strings.ToLower(value) || value[12] != '7' {
					t.Fatalf("RandomRecordKey() = %q, %v", value, err)
				}
			}
			if err := ValidateRecordKey(value); !errors.Is(err, testCase.want) {
				t.Fatalf("ValidateRecordKey(%q) error = %v, want %v", value, err, testCase.want)
			}
		})
	}
}
