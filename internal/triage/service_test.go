package triage

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/adenosine-dev/adenosine/internal/repository"
	"github.com/google/uuid"
)

var (
	testNow        = time.Date(2026, 8, 16, 1, 2, 3, 0, time.UTC)
	testRepository = RepositoryTarget{
		ID:         repository.ID(uuid.MustParse("0198af4d-5780-7e61-b188-2854fa145f3b")),
		OwnerDID:   "did:plc:owner",
		Repository: StrongRef{URI: "at://did:plc:owner/dev.adenosine.repo/example", CID: "bafybeigdyrzt5sfp7udm7hu76uh7y26nf3efuylqabf3oclgtqy55fbzdi"},
	}
	testIssue = StrongRef{URI: "at://did:plc:reporter/dev.adenosine.issue/issue1", CID: "bafybeibwzif5g2dkjaxtv3p67te2g7a2n5xwbn2khoxalctlp7ch5vmo3a"}
)

func TestServiceCreateLabel(t *testing.T) {
	testCases := []struct {
		name       string
		allowed    bool
		input      LabelInput
		wantErr    error
		wantRecord LabelRecord
	}{
		{name: "normalizes and publishes authorized label", allowed: true, input: LabelInput{Name: "  bug ", Color: "#A0B1C2", Description: "  broken behavior "}, wantRecord: LabelRecord{Repository: testRepository.Repository, Name: "bug", Color: "a0b1c2", Description: "broken behavior", CreatedAt: testNow, UpdatedAt: testNow}},
		{name: "rejects unauthorized maintainer", input: LabelInput{Name: "bug", Color: "a0b1c2"}, wantErr: ErrAuthorization},
		{name: "rejects invalid color", allowed: true, input: LabelInput{Name: "bug", Color: "purple"}, wantErr: ErrValidation},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			store := &serviceStore{repository: testRepository}
			publisher := &servicePublisher{}
			service := NewService(store, publisher, serviceAuthorizer{allowed: testCase.allowed}, fixedClock{now: testNow})
			_, err := service.CreateLabel(context.Background(), "did:plc:maintainer", RepositoryRoute{Owner: "owner.test", Slug: "example"}, testCase.input)
			if !errors.Is(err, testCase.wantErr) {
				t.Fatalf("error = %v, want %v", err, testCase.wantErr)
			}
			if testCase.wantErr == nil && publisher.labelRecord != testCase.wantRecord {
				t.Fatalf("record = %#v, want %#v", publisher.labelRecord, testCase.wantRecord)
			}
			if testCase.wantErr == nil && (publisher.author != testRepository.OwnerDID || publisher.rkey == "") {
				t.Fatalf("publication = author %q key %q", publisher.author, publisher.rkey)
			}
		})
	}
}

func TestServicePutMetadata(t *testing.T) {
	labelOne := "at://did:plc:owner/dev.adenosine.repositoryLabel/one"
	labelTwo := "at://did:plc:owner/dev.adenosine.repositoryLabel/two"
	testCases := []struct {
		name          string
		input         MetadataInput
		visible       []string
		wantErr       error
		wantLabels    []string
		wantAssignees []string
	}{
		{name: "replaces a complete sorted snapshot", input: MetadataInput{LabelIDs: []string{"two", "one", "one"}, AssigneeDIDs: []string{"did:plc:z", "did:plc:a", "did:plc:a"}, MilestoneID: "release"}, visible: []string{"did:plc:a", "did:plc:z"}, wantLabels: []string{labelOne, labelTwo}, wantAssignees: []string{"did:plc:a", "did:plc:z"}},
		{name: "rejects unknown assignee", input: MetadataInput{AssigneeDIDs: []string{"did:plc:missing"}}, wantErr: ErrNotFound},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			store := &serviceStore{repository: testRepository, subject: testIssue, labelURIs: map[string]string{"one": labelOne, "two": labelTwo}, milestoneURI: "at://did:plc:owner/dev.adenosine.repositoryMilestone/release", visibleAssignees: testCase.visible}
			publisher := &servicePublisher{}
			service := NewService(store, publisher, serviceAuthorizer{allowed: true}, fixedClock{now: testNow})
			_, err := service.PutMetadata(context.Background(), "did:plc:maintainer", RepositoryRoute{Owner: "owner.test", Slug: "example"}, SubjectIssue, testIssue.URI, testCase.input)
			if !errors.Is(err, testCase.wantErr) {
				t.Fatalf("error = %v, want %v", err, testCase.wantErr)
			}
			if testCase.wantErr == nil {
				if !slices.Equal(publisher.metadataRecord.LabelURIs, testCase.wantLabels) || !slices.Equal(publisher.metadataRecord.AssigneeDIDs, testCase.wantAssignees) {
					t.Fatalf("metadata = %#v", publisher.metadataRecord)
				}
				if publisher.metadataRecord.MilestoneURI != store.milestoneURI || publisher.metadataRecord.Subject != testIssue {
					t.Fatalf("metadata references = %#v", publisher.metadataRecord)
				}
			}
		})
	}
}

func TestServiceGetMetadata(t *testing.T) {
	testCases := []struct {
		name string
		want Metadata
	}{
		{name: "returns an empty object instead of a missing association", want: Metadata{MetadataRecord: MetadataRecord{Subject: testIssue, Kind: SubjectIssue, Repository: testRepository.Repository, LabelURIs: []string{}, AssigneeDIDs: []string{}}, Labels: []Label{}, Assignees: []Assignee{}}},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			store := &serviceStore{repository: testRepository, subject: testIssue}
			service := NewService(store, &servicePublisher{}, serviceAuthorizer{}, fixedClock{})
			value, err := service.GetMetadata(context.Background(), RepositoryRoute{Owner: "owner.test", Slug: "example"}, SubjectIssue, testIssue.URI, "did:plc:viewer")
			if err != nil || value.Subject != testCase.want.Subject || value.Repository != testCase.want.Repository || value.Labels == nil || value.Assignees == nil {
				t.Fatalf("value = %#v, error = %v", value, err)
			}
		})
	}
}

func TestPortableAuthorityValidation(t *testing.T) {
	testCases := []struct {
		name    string
		value   Label
		wantErr error
	}{
		{name: "accepts repository owner authority", value: Label{URI: "at://did:plc:owner/dev.adenosine.repositoryLabel/label", CID: "bafybeihdwdcefgh4dqkjv67uzcmw7ojee6xedzdetojuzjevtenxquvyku", AuthorDID: "did:plc:owner", RKey: "label", LabelRecord: LabelRecord{Repository: testRepository.Repository, Name: "bug", Color: "a0b1c2", CreatedAt: testNow, UpdatedAt: testNow}}},
		{name: "rejects forged authority", value: Label{URI: "at://did:plc:attacker/dev.adenosine.repositoryLabel/label", CID: "bafybeihdwdcefgh4dqkjv67uzcmw7ojee6xedzdetojuzjevtenxquvyku", AuthorDID: "did:plc:attacker", RKey: "label", LabelRecord: LabelRecord{Repository: testRepository.Repository, Name: "bug", Color: "a0b1c2", CreatedAt: testNow, UpdatedAt: testNow}}, wantErr: ErrAuthorization},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			if err := testCase.value.Validate(); !errors.Is(err, testCase.wantErr) {
				t.Fatalf("error = %v, want %v", err, testCase.wantErr)
			}
		})
	}
}

type serviceStore struct {
	repository       RepositoryTarget
	subject          StrongRef
	metadata         *Metadata
	labelURIs        map[string]string
	milestoneURI     string
	visibleAssignees []string
}

func (store *serviceStore) ResolveRepository(context.Context, RepositoryRoute) (RepositoryTarget, error) {
	return store.repository, nil
}
func (store *serviceStore) ResolveRepositoryForRead(context.Context, RepositoryRoute) (RepositoryTarget, error) {
	return store.repository, nil
}
func (*serviceStore) ListLabels(context.Context, string, string, int, string) ([]Label, error) {
	return []Label{}, nil
}
func (*serviceStore) GetLabel(context.Context, string, string, string) (Label, error) {
	return Label{}, ErrNotFound
}
func (*serviceStore) ListMilestones(context.Context, string, string, int, string) ([]Milestone, error) {
	return []Milestone{}, nil
}
func (*serviceStore) GetMilestone(context.Context, string, string, string) (Milestone, error) {
	return Milestone{}, ErrNotFound
}
func (store *serviceStore) ResolveSubject(context.Context, RepositoryRoute, SubjectKind, string, string) (subjectTarget, error) {
	return subjectTarget{RepositoryTarget: store.repository, Subject: store.subject, Metadata: store.metadata}, nil
}
func (store *serviceStore) ResolveSubjectForRead(ctx context.Context, route RepositoryRoute, kind SubjectKind, subject, viewer string) (subjectTarget, error) {
	return store.ResolveSubject(ctx, route, kind, subject, viewer)
}
func (store *serviceStore) ResolveLabelURIs(_ context.Context, _ string, ids []string) ([]string, error) {
	values := make([]string, 0, len(ids))
	for _, id := range ids {
		uri, ok := store.labelURIs[id]
		if !ok {
			return nil, ErrNotFound
		}
		values = append(values, uri)
	}
	slices.Sort(values)
	return values, nil
}
func (store *serviceStore) ResolveMilestoneURI(context.Context, string, string) (string, error) {
	if store.milestoneURI == "" {
		return "", ErrNotFound
	}
	return store.milestoneURI, nil
}
func (store *serviceStore) ValidateAssignees(_ context.Context, dids []string) error {
	if !slices.Equal(dids, store.visibleAssignees) {
		return ErrNotFound
	}
	return nil
}

type servicePublisher struct {
	author         string
	rkey           string
	labelRecord    LabelRecord
	metadataRecord MetadataRecord
}

func (publisher *servicePublisher) CreateLabel(_ context.Context, author, rkey string, record LabelRecord) (Label, error) {
	publisher.author, publisher.rkey, publisher.labelRecord = author, rkey, record
	return Label{URI: "at://" + author + "/" + LabelCollection + "/" + rkey, CID: "bafybeihdwdcefgh4dqkjv67uzcmw7ojee6xedzdetojuzjevtenxquvyku", AuthorDID: author, RKey: rkey, LabelRecord: record}, nil
}
func (*servicePublisher) PutLabel(context.Context, string, string, string, LabelRecord) (Label, error) {
	return Label{}, nil
}
func (*servicePublisher) CreateMilestone(context.Context, string, string, MilestoneRecord) (Milestone, error) {
	return Milestone{}, nil
}
func (*servicePublisher) PutMilestone(context.Context, string, string, string, MilestoneRecord) (Milestone, error) {
	return Milestone{}, nil
}
func (publisher *servicePublisher) PutSubjectTriage(_ context.Context, author, _ string, record MetadataRecord) (Metadata, error) {
	publisher.author, publisher.metadataRecord = author, record
	return Metadata{MetadataRecord: record}, nil
}
func (*servicePublisher) DeleteTriageRecord(context.Context, string, string, string, string) error {
	return nil
}

type serviceAuthorizer struct{ allowed bool }

func (authorizer serviceAuthorizer) CanTriageRepository(context.Context, string, repository.ID) (bool, error) {
	return authorizer.allowed, nil
}

type fixedClock struct{ now time.Time }

func (clock fixedClock) Now() time.Time { return clock.now }
