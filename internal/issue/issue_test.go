package issue

import (
	"errors"
	"strings"
	"testing"
	"time"
)

const (
	testRepositoryURI = "at://did:plc:ewvi7nxzyoun6zhxrhs64oiz/dev.adenosine.repo/0198a8512a897ae2a370dc68883e3af1"
	testIssueURI      = "at://did:plc:alice/dev.adenosine.issue/0198a8512a897ae2a370dc68883e3af1"
	testCommentURI    = "at://did:plc:bob/dev.adenosine.issueComment/0198a8512a897ae2a370dc68883e3af1"
	testCID           = "bafybeigdyrzt5sfp7udm7hu76uh7y26nf3efuylqabf3oclgtqy55fbzdi"
)

func TestRecordValidation(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 8, 9, 12, 0, 0, 0, time.UTC)
	valid := Record{Repository: StrongRef{URI: testRepositoryURI, CID: testCID}, Title: "title", Body: "body", CreatedAt: now, UpdatedAt: now}
	testCases := []struct {
		name   string
		mutate func(*Record)
		want   error
	}{
		{name: "valid"},
		{name: "wrong repository collection", mutate: func(value *Record) { value.Repository.URI = testIssueURI }, want: ErrValidation},
		{name: "noncanonical repository CID", mutate: func(value *Record) { value.Repository.CID = strings.ToUpper(testCID) }, want: ErrValidation},
		{name: "title too long", mutate: func(value *Record) { value.Title = strings.Repeat("x", 256) }, want: ErrValidation},
		{name: "body too long", mutate: func(value *Record) { value.Body = strings.Repeat("x", 65536) }, want: ErrValidation},
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

func TestCommentRecordValidation(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 8, 9, 12, 0, 0, 0, time.UTC)
	valid := CommentRecord{Subject: StrongRef{URI: testIssueURI, CID: testCID}, Body: "comment", CreatedAt: now, UpdatedAt: now}
	testCases := []struct {
		name   string
		mutate func(*CommentRecord)
		want   error
	}{
		{name: "valid"},
		{name: "wrong subject collection", mutate: func(value *CommentRecord) { value.Subject.URI = testRepositoryURI }, want: ErrValidation},
		{name: "noncanonical subject CID", mutate: func(value *CommentRecord) { value.Subject.CID = strings.ToUpper(testCID) }, want: ErrValidation},
		{name: "canonical parent", mutate: func(value *CommentRecord) { value.Parent = &StrongRef{URI: testCommentURI, CID: testCID} }},
		{name: "wrong parent collection", mutate: func(value *CommentRecord) { value.Parent = &StrongRef{URI: testIssueURI, CID: testCID} }, want: ErrValidation},
		{name: "noncanonical parent CID", mutate: func(value *CommentRecord) {
			value.Parent = &StrongRef{URI: testCommentURI, CID: strings.ToUpper(testCID)}
		}, want: ErrValidation},
		{name: "body at maximum length", mutate: func(value *CommentRecord) { value.Body = strings.Repeat("x", 65535) }},
		{name: "body too long", mutate: func(value *CommentRecord) { value.Body = strings.Repeat("x", 65536) }, want: ErrValidation},
		{name: "missing created timestamp", mutate: func(value *CommentRecord) { value.CreatedAt = time.Time{} }, want: ErrValidation},
		{name: "missing updated timestamp", mutate: func(value *CommentRecord) { value.UpdatedAt = time.Time{} }, want: ErrValidation},
		{name: "updated before created", mutate: func(value *CommentRecord) { value.UpdatedAt = now.Add(-time.Second) }, want: ErrValidation},
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

func TestStatusValidationAndAuthority(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 8, 9, 12, 0, 0, 0, time.UTC)
	valid := StatusRecord{Subject: StrongRef{URI: testIssueURI, CID: testCID}, Repository: StrongRef{URI: testRepositoryURI, CID: testCID}, State: StateOpen, CreatedAt: now, UpdatedAt: now}
	testCases := []struct {
		name   string
		mutate func(*StatusRecord)
		want   error
	}{
		{name: "open"},
		{name: "closed", mutate: func(value *StatusRecord) { value.State = StateClosed }},
		{name: "wrong subject collection", mutate: func(value *StatusRecord) { value.Subject.URI = testRepositoryURI }, want: ErrValidation},
		{name: "unknown state", mutate: func(value *StatusRecord) { value.State = "triaged" }, want: ErrValidation},
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

func TestEnvelopeValidation(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 8, 9, 12, 0, 0, 0, time.UTC)
	statusKey, _ := StatusRecordKey(testIssueURI)
	validRecord := Record{Repository: StrongRef{URI: testRepositoryURI, CID: testCID}, Title: "title", Body: "body", CreatedAt: now, UpdatedAt: now}
	validComment := CommentRecord{Subject: StrongRef{URI: testIssueURI, CID: testCID}, Body: "comment", CreatedAt: now, UpdatedAt: now}
	validStatus := StatusRecord{Subject: StrongRef{URI: testIssueURI, CID: testCID}, Repository: StrongRef{URI: testRepositoryURI, CID: testCID}, State: StateOpen, CreatedAt: now, UpdatedAt: now}
	testCases := []struct {
		name  string
		value interface{ Validate() error }
		want  error
	}{
		{name: "reporter owns issue content", value: Issue{URI: testIssueURI, CID: testCID, AuthorDID: "did:plc:alice", Record: validRecord}},
		{name: "issue envelope authority mismatch", value: Issue{URI: testIssueURI, CID: testCID, AuthorDID: "did:plc:mallory", Record: validRecord}, want: ErrAuthorization},
		{name: "comment author owns comment content", value: Comment{URI: testCommentURI, CID: testCID, AuthorDID: "did:plc:bob", CommentRecord: validComment}},
		{name: "comment envelope authority mismatch", value: Comment{URI: testCommentURI, CID: testCID, AuthorDID: "did:plc:mallory", CommentRecord: validComment}, want: ErrAuthorization},
		{name: "comment cannot reply to itself", value: Comment{URI: testCommentURI, CID: testCID, AuthorDID: "did:plc:bob", CommentRecord: CommentRecord{Subject: validComment.Subject, Parent: &StrongRef{URI: testCommentURI, CID: testCID}, Body: "comment", CreatedAt: now, UpdatedAt: now}}, want: ErrValidation},
		{name: "repository owner owns status", value: Status{URI: "at://did:plc:ewvi7nxzyoun6zhxrhs64oiz/" + StatusCollection + "/" + statusKey, CID: testCID, AuthorDID: "did:plc:ewvi7nxzyoun6zhxrhs64oiz", StatusRecord: validStatus}},
		{name: "wrong status key", value: Status{URI: "at://did:plc:ewvi7nxzyoun6zhxrhs64oiz/" + StatusCollection + "/wrong", CID: testCID, AuthorDID: "did:plc:ewvi7nxzyoun6zhxrhs64oiz", StatusRecord: validStatus}, want: ErrValidation},
		{name: "non-owner status author", value: Status{URI: "at://did:plc:mallory/" + StatusCollection + "/" + statusKey, CID: testCID, AuthorDID: "did:plc:mallory", StatusRecord: validStatus}, want: ErrAuthorization},
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
		{name: "deterministic status key", uri: testIssueURI, want: "qpcodcvuzp75x2neyrygzcosbain3anmlhjjvmcpokfoiiudlnmq"},
		{name: "wrong collection", uri: testRepositoryURI, wantErr: true},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			got, err := StatusRecordKey(testCase.uri)
			if (err != nil) != testCase.wantErr || got != testCase.want {
				t.Fatalf("StatusRecordKey() = %q, %v", got, err)
			}
		})
	}
}

func TestRandomRecordKey(t *testing.T) {
	t.Parallel()
	testCases := []struct{ name string }{{name: "UUIDv7-derived lowercase hex"}}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			key, err := RandomRecordKey()
			if err != nil || len(key) != 32 || key != strings.ToLower(key) || ValidateRecordKey(key) != nil {
				t.Fatalf("RandomRecordKey() = %q, %v", key, err)
			}
		})
	}
}
