package restapi

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	generated "github.com/adenosine-dev/adenosine/api/generated/go"
	"github.com/adenosine-dev/adenosine/internal/triage"
)

type fakeTriageManager struct {
	labels        []triage.Label
	milestones    []triage.Milestone
	metadata      triage.Metadata
	listLimit     int
	listCursor    string
	viewerDID     string
	actorDID      string
	subjectURI    string
	subjectKind   triage.SubjectKind
	metadataInput triage.MetadataInput
}

func (manager *fakeTriageManager) ListLabels(_ context.Context, _ triage.RepositoryRoute, viewerDID string, limit int, cursor string) ([]triage.Label, error) {
	manager.viewerDID, manager.listLimit, manager.listCursor = viewerDID, limit, cursor
	return manager.labels, nil
}

func (manager *fakeTriageManager) GetLabel(context.Context, triage.RepositoryRoute, string, string) (triage.Label, error) {
	return manager.labels[0], nil
}

func (manager *fakeTriageManager) CreateLabel(_ context.Context, actorDID string, _ triage.RepositoryRoute, input triage.LabelInput) (triage.Label, error) {
	manager.actorDID = actorDID
	value := manager.labels[0]
	value.Name, value.Color, value.Description = input.Name, input.Color, input.Description
	return value, nil
}

func (manager *fakeTriageManager) UpdateLabel(_ context.Context, actorDID string, _ triage.RepositoryRoute, _ string, input triage.LabelInput) (triage.Label, error) {
	return manager.CreateLabel(context.Background(), actorDID, triage.RepositoryRoute{}, input)
}

func (manager *fakeTriageManager) DeleteLabel(_ context.Context, actorDID string, _ triage.RepositoryRoute, _ string) error {
	manager.actorDID = actorDID
	return nil
}

func (manager *fakeTriageManager) ListMilestones(_ context.Context, _ triage.RepositoryRoute, viewerDID string, limit int, cursor string) ([]triage.Milestone, error) {
	manager.viewerDID, manager.listLimit, manager.listCursor = viewerDID, limit, cursor
	return manager.milestones, nil
}

func (manager *fakeTriageManager) GetMilestone(context.Context, triage.RepositoryRoute, string, string) (triage.Milestone, error) {
	return manager.milestones[0], nil
}

func (manager *fakeTriageManager) CreateMilestone(_ context.Context, actorDID string, _ triage.RepositoryRoute, input triage.MilestoneInput) (triage.Milestone, error) {
	manager.actorDID = actorDID
	value := manager.milestones[0]
	value.Title, value.Description, value.State, value.DueAt = input.Title, input.Description, input.State, input.DueAt
	return value, nil
}

func (manager *fakeTriageManager) UpdateMilestone(_ context.Context, actorDID string, _ triage.RepositoryRoute, _ string, input triage.MilestoneInput) (triage.Milestone, error) {
	return manager.CreateMilestone(context.Background(), actorDID, triage.RepositoryRoute{}, input)
}

func (manager *fakeTriageManager) DeleteMilestone(_ context.Context, actorDID string, _ triage.RepositoryRoute, _ string) error {
	manager.actorDID = actorDID
	return nil
}

func (manager *fakeTriageManager) GetMetadata(_ context.Context, _ triage.RepositoryRoute, kind triage.SubjectKind, subjectURI, viewerDID string) (triage.Metadata, error) {
	manager.subjectKind, manager.subjectURI, manager.viewerDID = kind, subjectURI, viewerDID
	return manager.metadata, nil
}

func (manager *fakeTriageManager) PutMetadata(_ context.Context, actorDID string, _ triage.RepositoryRoute, kind triage.SubjectKind, subjectURI string, input triage.MetadataInput) (triage.Metadata, error) {
	manager.actorDID, manager.subjectKind, manager.subjectURI, manager.metadataInput = actorDID, kind, subjectURI, input
	return manager.metadata, nil
}

func (manager *fakeTriageManager) DeleteMetadata(_ context.Context, actorDID string, _ triage.RepositoryRoute, kind triage.SubjectKind, subjectURI string) error {
	manager.actorDID, manager.subjectKind, manager.subjectURI = actorDID, kind, subjectURI
	return nil
}

func TestRepositoryTriageEndpoints(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 8, 16, 1, 2, 3, 0, time.UTC)
	repositoryRef := triage.StrongRef{URI: "at://did:plc:alice/dev.adenosine.repo/project", CID: "bafyreirepository"}
	labels := []triage.Label{
		{URI: "at://did:plc:alice/dev.adenosine.repositoryLabel/one", CID: "bafyrei1", AuthorDID: "did:plc:alice", RKey: "one", LabelRecord: triage.LabelRecord{Repository: repositoryRef, Name: "bug", Color: "d73a4a", CreatedAt: now, UpdatedAt: now}, IndexedAt: now},
		{URI: "at://did:plc:alice/dev.adenosine.repositoryLabel/two", CID: "bafyrei2", AuthorDID: "did:plc:alice", RKey: "two", LabelRecord: triage.LabelRecord{Repository: repositoryRef, Name: "feature", Color: "0e8a16", CreatedAt: now, UpdatedAt: now}, IndexedAt: now},
	}
	milestones := []triage.Milestone{{URI: "at://did:plc:alice/dev.adenosine.repositoryMilestone/v1", CID: "bafyreiv1", AuthorDID: "did:plc:alice", RKey: "v1", MilestoneRecord: triage.MilestoneRecord{Repository: repositoryRef, Title: "v1", State: triage.MilestoneOpen, CreatedAt: now, UpdatedAt: now}, IndexedAt: now}}
	subjectURI := "at://did:plc:bob/dev.adenosine.issue/one"
	metadata := triage.Metadata{URI: "at://did:plc:alice/dev.adenosine.subjectTriage/key", CID: "bafyreimetadata", AuthorDID: "did:plc:alice", RKey: "key", MetadataRecord: triage.MetadataRecord{Subject: triage.StrongRef{URI: subjectURI, CID: "bafyreissue"}, Kind: triage.SubjectIssue, Repository: repositoryRef, LabelURIs: []string{labels[0].URI}, AssigneeDIDs: []string{"did:plc:bob"}, CreatedAt: now, UpdatedAt: now}, Labels: labels[:1], Assignees: []triage.Assignee{{DID: "did:plc:bob", Handle: "bob.test"}}, IndexedAt: now}
	encodedSubject := base64.RawURLEncoding.EncodeToString([]byte(subjectURI))

	testCases := []struct {
		name       string
		method     string
		path       string
		body       string
		session    bool
		origin     bool
		wantStatus int
		assert     func(*testing.T, *fakeTriageManager, []byte, http.Header)
	}{
		{name: "lists labels in an object envelope with opaque pagination", method: http.MethodGet, path: "/api/v1/repositories/alice/project/labels?limit=1", session: true, wantStatus: http.StatusOK, assert: func(t *testing.T, manager *fakeTriageManager, body []byte, header http.Header) {
			var response generated.RepositoryLabelList
			err := json.Unmarshal(body, &response)
			if err != nil || len(response.Items) != 1 || response.Page.NextCursor == nil || strings.Contains(*response.Page.NextCursor, labels[0].URI) || manager.listLimit != 2 || manager.viewerDID != "did:plc:alice" || header.Get("Vary") != "Cookie" {
				t.Fatalf("response=%+v err=%v limit=%d viewer=%q vary=%q", response, err, manager.listLimit, manager.viewerDID, header.Get("Vary"))
			}
		}},
		{name: "rejects a label cursor from another scope", method: http.MethodGet, path: "/api/v1/repositories/alice/project/labels?cursor=invalid", wantStatus: http.StatusBadRequest, assert: func(*testing.T, *fakeTriageManager, []byte, http.Header) {}},
		{name: "creates a pending label projection", method: http.MethodPost, path: "/api/v1/repositories/alice/project/labels", body: `{"name":"security","color":"b60205","description":"Security work"}`, session: true, origin: true, wantStatus: http.StatusAccepted, assert: func(t *testing.T, manager *fakeTriageManager, body []byte, header http.Header) {
			var response generated.RepositoryLabelMutation
			err := json.Unmarshal(body, &response)
			if err != nil || bool(response.Projected) || response.Label.Name != "security" || manager.actorDID != "did:plc:alice" || header.Get("Location") == "" {
				t.Fatalf("response=%+v err=%v actor=%q location=%q", response, err, manager.actorDID, header.Get("Location"))
			}
		}},
		{name: "requires exact origin for label mutation", method: http.MethodPost, path: "/api/v1/repositories/alice/project/labels", body: `{"name":"security","color":"b60205","description":""}`, session: true, wantStatus: http.StatusForbidden, assert: func(*testing.T, *fakeTriageManager, []byte, http.Header) {}},
		{name: "lists milestones in an object envelope", method: http.MethodGet, path: "/api/v1/repositories/alice/project/milestones", wantStatus: http.StatusOK, assert: func(t *testing.T, _ *fakeTriageManager, body []byte, _ http.Header) {
			var response generated.RepositoryMilestoneList
			if err := json.Unmarshal(body, &response); err != nil || len(response.Items) != 1 || response.Items[0].Title != "v1" {
				t.Fatalf("response=%+v err=%v", response, err)
			}
		}},
		{name: "reads an issue triage snapshot", method: http.MethodGet, path: "/api/v1/repositories/alice/project/issues/" + encodedSubject + "/triage", session: true, wantStatus: http.StatusOK, assert: func(t *testing.T, manager *fakeTriageManager, body []byte, _ http.Header) {
			var response generated.SubjectTriage
			err := json.Unmarshal(body, &response)
			if err != nil || manager.subjectURI != subjectURI || manager.subjectKind != triage.SubjectIssue || manager.viewerDID != "did:plc:alice" || len(response.LabelIds) != 1 || response.LabelIds[0] != "one" {
				t.Fatalf("response=%+v err=%v subject=%q kind=%q viewer=%q", response, err, manager.subjectURI, manager.subjectKind, manager.viewerDID)
			}
		}},
		{name: "atomically replaces pull request triage", method: http.MethodPut, path: "/api/v1/repositories/alice/project/pulls/" + encodedSubject + "/triage", body: `{"label_ids":["one"],"assignee_dids":["did:plc:bob"],"milestone_id":"v1"}`, session: true, origin: true, wantStatus: http.StatusAccepted, assert: func(t *testing.T, manager *fakeTriageManager, body []byte, _ http.Header) {
			var response generated.SubjectTriageMutation
			err := json.Unmarshal(body, &response)
			if err != nil || bool(response.Projected) || manager.subjectKind != triage.SubjectPullRequest || manager.metadataInput.MilestoneID != "v1" || len(manager.metadataInput.LabelIDs) != 1 {
				t.Fatalf("response=%+v err=%v kind=%q input=%+v", response, err, manager.subjectKind, manager.metadataInput)
			}
		}},
		{name: "rejects malformed subject identifiers", method: http.MethodGet, path: "/api/v1/repositories/alice/project/issues/not_base64!/triage", wantStatus: http.StatusBadRequest, assert: func(*testing.T, *fakeTriageManager, []byte, http.Header) {}},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			manager := &fakeTriageManager{labels: labels, milestones: milestones, metadata: metadata}
			server := testAPIServer(t, Dependencies{Triage: manager, Sessions: fakeSessions{}})
			response := performAPIRequest(server, testCase.method, testCase.path, testCase.body, testCase.session, testCase.origin, "")
			if response.Code != testCase.wantStatus {
				t.Fatalf("status=%d, want %d: %s", response.Code, testCase.wantStatus, response.Body.String())
			}
			testCase.assert(t, manager, response.Body.Bytes(), response.Header())
		})
	}
}
