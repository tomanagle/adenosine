package organization

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
)

type memoryCollaboratorStore struct {
	canAdmin     bool
	authorizeErr error
	items        []RepositoryCollaborator
	putCalls     int
	removeCalls  int
	listAfter    string
	listLimit    int32
	audits       []AuditEvent
}

func (store *memoryCollaboratorStore) CanAdminRepository(context.Context, ID, uuid.UUID, string) (bool, error) {
	return store.canAdmin, store.authorizeErr
}
func (store *memoryCollaboratorStore) PutRepositoryCollaborator(_ context.Context, _ ID, repositoryID uuid.UUID, did string, role RepositoryRole, now time.Time) (RepositoryCollaborator, error) {
	store.putCalls++
	return RepositoryCollaborator{RepositoryID: repositoryID, AccountDID: did, Role: role, CreatedAt: now, UpdatedAt: now}, nil
}
func (store *memoryCollaboratorStore) ListRepositoryCollaborators(_ context.Context, _ ID, _ uuid.UUID, after string, limit int32) ([]RepositoryCollaborator, error) {
	store.listAfter, store.listLimit = after, limit
	return store.items, nil
}
func (store *memoryCollaboratorStore) RemoveRepositoryCollaborator(context.Context, ID, uuid.UUID, string) error {
	store.removeCalls++
	return nil
}
func (store *memoryCollaboratorStore) RecordAudit(_ context.Context, event AuditEvent) error {
	store.audits = append(store.audits, event)
	return nil
}

func TestCollaboratorServiceList(t *testing.T) {
	t.Parallel()
	organizationID := ID(uuid.MustParse("0198a851-2a89-7ae2-a370-dc68883e3af1"))
	repositoryID := uuid.MustParse("0198a851-2a89-7ae2-a370-dc68883e3af2")
	authorizeErr := errors.New("database failed")
	testCases := []struct {
		name         string
		canAdmin     bool
		authorizeErr error
		limit        int
		wantErr      error
		wantItems    int
		wantNext     string
		wantDBLimit  int32
	}{
		{name: "returns database keyset page", canAdmin: true, limit: 1, wantItems: 1, wantNext: "did:plc:first", wantDBLimit: 2},
		{name: "repository admin required", limit: 30, wantErr: ErrForbidden},
		{name: "authorization failure is returned", authorizeErr: authorizeErr, limit: 30, wantErr: authorizeErr},
		{name: "limit validated", limit: 101, wantErr: ErrValidation},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			store := &memoryCollaboratorStore{canAdmin: testCase.canAdmin, authorizeErr: testCase.authorizeErr, items: []RepositoryCollaborator{{AccountDID: "did:plc:first"}, {AccountDID: "did:plc:second"}}}
			service := NewCollaboratorService(store, fixedClock{})
			page, err := service.List(context.Background(), organizationID, repositoryID, "did:plc:owner", "", testCase.limit)
			if !errors.Is(err, testCase.wantErr) {
				t.Fatalf("List() error = %v, want %v", err, testCase.wantErr)
			}
			if len(page.Items) != testCase.wantItems || page.NextCursor != nil && *page.NextCursor != testCase.wantNext || page.NextCursor == nil && testCase.wantNext != "" {
				t.Fatalf("page = %#v, want items %d and next %q", page, testCase.wantItems, testCase.wantNext)
			}
			if store.listLimit != testCase.wantDBLimit {
				t.Fatalf("database limit = %d, want %d", store.listLimit, testCase.wantDBLimit)
			}
		})
	}
}

func TestCollaboratorServiceMutations(t *testing.T) {
	t.Parallel()
	organizationID := ID(uuid.MustParse("0198a851-2a89-7ae2-a370-dc68883e3af1"))
	repositoryID := uuid.MustParse("0198a851-2a89-7ae2-a370-dc68883e3af2")
	testCases := []struct {
		name       string
		operation  string
		role       RepositoryRole
		canAdmin   bool
		wantErr    error
		wantPut    int
		wantRemove int
		wantAudits int
	}{
		{name: "repository admin assigns outside collaborator", operation: "put", role: RepositoryRoleMaintain, canAdmin: true, wantPut: 1, wantAudits: 1},
		{name: "repository admin removes outside collaborator", operation: "remove", canAdmin: true, wantRemove: 1, wantAudits: 1},
		{name: "member without admin cannot assign collaborator", operation: "put", role: RepositoryRoleRead, wantErr: ErrForbidden},
		{name: "invalid role rejected", operation: "put", role: RepositoryRoleNone, wantErr: ErrValidation},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			store := &memoryCollaboratorStore{canAdmin: testCase.canAdmin}
			service := NewCollaboratorService(store, fixedClock{time.Date(2026, 8, 13, 0, 0, 0, 0, time.UTC)})
			var err error
			if testCase.operation == "put" {
				_, err = service.Put(context.Background(), organizationID, repositoryID, "did:plc:owner", "did:plc:outside", testCase.role)
			} else {
				err = service.Remove(context.Background(), organizationID, repositoryID, "did:plc:owner", "did:plc:outside")
			}
			if !errors.Is(err, testCase.wantErr) {
				t.Fatalf("mutation error = %v, want %v", err, testCase.wantErr)
			}
			if store.putCalls != testCase.wantPut || store.removeCalls != testCase.wantRemove || len(store.audits) != testCase.wantAudits {
				t.Fatalf("put/remove/audits = %d/%d/%d, want %d/%d/%d", store.putCalls, store.removeCalls, len(store.audits), testCase.wantPut, testCase.wantRemove, testCase.wantAudits)
			}
		})
	}
}
