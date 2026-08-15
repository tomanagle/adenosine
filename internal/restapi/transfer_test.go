package restapi

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	generated "github.com/adenosine-dev/adenosine/api/generated/go"
	"github.com/adenosine-dev/adenosine/internal/repository"
	"github.com/adenosine-dev/adenosine/internal/transfer"
	"github.com/google/uuid"
)

type transferManager struct {
	value            transfer.Transfer
	destinationAlias string
	pageAfter        *uuid.UUID
	pageLimit        int
	accepted         bool
	cancelled        bool
}

func (manager *transferManager) Initiate(_ context.Context, _ repository.Repository, _ string, destinationAlias string) (transfer.Transfer, error) {
	manager.destinationAlias = destinationAlias
	return manager.value, nil
}
func (manager *transferManager) Get(context.Context, uuid.UUID, string) (transfer.Transfer, error) {
	return manager.value, nil
}
func (manager *transferManager) Page(_ context.Context, _ repository.ID, _ string, after *uuid.UUID, limit int) (transfer.Page, error) {
	manager.pageAfter, manager.pageLimit = after, limit
	next := uuid.MustParse("0198a851-2a89-7ae2-a370-dc68883e3af8")
	return transfer.Page{Items: []transfer.Transfer{manager.value}, NextCursor: &next}, nil
}
func (manager *transferManager) Accept(context.Context, uuid.UUID, string) (transfer.Transfer, error) {
	manager.accepted = true
	manager.value.Status = transfer.StatusCompleted
	return manager.value, nil
}
func (manager *transferManager) Cancel(context.Context, uuid.UUID, string) (transfer.Transfer, error) {
	manager.cancelled = true
	manager.value.Status = transfer.StatusCancelled
	return manager.value, nil
}

func TestRepositoryTransferEndpoints(t *testing.T) {
	t.Parallel()
	repositoryID := repository.ID(uuid.MustParse("0198a851-2a89-7ae2-a370-dc68883e3af3"))
	transferID := uuid.MustParse("0198a851-2a89-7ae2-a370-dc68883e3af4")
	now := time.Date(2026, 8, 14, 1, 2, 3, 0, time.UTC)
	repo := repository.Repository{ID: repositoryID, OwnerDID: "did:plc:alice", Slug: "project", State: repository.StateActive}
	value := transfer.Transfer{ID: transferID, RepositoryID: repositoryID, SourceOwnerAlias: "alice", Destination: transfer.Owner{Alias: "bob"}, Status: transfer.StatusPending, CreatedAt: now, ExpiresAt: now.Add(time.Hour)}
	testCases := []struct {
		name, method, path, body string
		origin                   bool
		wantStatus               int
		assert                   func(*testing.T, *transferManager, []byte, http.Header)
	}{
		{name: "creates proposal", method: http.MethodPost, path: "/api/v1/repositories/alice/project/transfers", body: `{"destination_owner":"bob"}`, origin: true, wantStatus: http.StatusAccepted, assert: func(t *testing.T, manager *transferManager, _ []byte, header http.Header) {
			if manager.destinationAlias != "bob" || header.Get("Location") != "/api/v1/repository-transfers/"+transferID.String() {
				t.Fatalf("proposal destination/header = %q/%q", manager.destinationAlias, header.Get("Location"))
			}
		}},
		{name: "lists opaque cursor page", method: http.MethodGet, path: "/api/v1/repositories/alice/project/transfers?limit=10", wantStatus: http.StatusOK, assert: func(t *testing.T, manager *transferManager, body []byte, _ http.Header) {
			var response generated.RepositoryTransferList
			if err := json.Unmarshal(body, &response); err != nil || len(response.Items) != 1 || response.Page.NextCursor == nil || strings.Contains(*response.Page.NextCursor, "0198a851") || manager.pageLimit != 10 {
				t.Fatalf("page = %+v, error=%v, limit=%d", response, err, manager.pageLimit)
			}
		}},
		{name: "rejects cursor from another scope", method: http.MethodGet, path: "/api/v1/repositories/alice/project/transfers?cursor=invalid", wantStatus: http.StatusBadRequest, assert: func(*testing.T, *transferManager, []byte, http.Header) {}},
		{name: "inspects transfer", method: http.MethodGet, path: "/api/v1/repository-transfers/" + transferID.String(), wantStatus: http.StatusOK, assert: func(t *testing.T, _ *transferManager, body []byte, _ http.Header) {
			var response generated.RepositoryTransfer
			if err := json.Unmarshal(body, &response); err != nil || uuid.UUID(response.Id) != transferID {
				t.Fatalf("transfer = %+v, error=%v", response, err)
			}
		}},
		{name: "accepts transfer", method: http.MethodPost, path: "/api/v1/repository-transfers/" + transferID.String() + "/acceptance", origin: true, wantStatus: http.StatusOK, assert: func(t *testing.T, manager *transferManager, _ []byte, _ http.Header) {
			if !manager.accepted {
				t.Fatal("acceptance was not called")
			}
		}},
		{name: "cancels transfer", method: http.MethodDelete, path: "/api/v1/repository-transfers/" + transferID.String(), origin: true, wantStatus: http.StatusNoContent, assert: func(t *testing.T, manager *transferManager, _ []byte, _ http.Header) {
			if !manager.cancelled {
				t.Fatal("cancellation was not called")
			}
		}},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			manager := &transferManager{value: value}
			server := testAPIServer(t, Dependencies{Repositories: fixedRepositoryManager{repository: repo}, Transfers: manager, Sessions: fakeSessions{}})
			response := performAPIRequest(server, testCase.method, testCase.path, testCase.body, true, testCase.origin, "")
			if response.Code != testCase.wantStatus {
				t.Fatalf("status = %d, want %d: %s", response.Code, testCase.wantStatus, response.Body.String())
			}
			testCase.assert(t, manager, response.Body.Bytes(), response.Header())
		})
	}
}
