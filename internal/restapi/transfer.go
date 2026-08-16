package restapi

import (
	"net/http"
	"slices"

	generated "github.com/adenosine-dev/adenosine/api/generated/go"
	"github.com/adenosine-dev/adenosine/internal/auth"
	"github.com/adenosine-dev/adenosine/internal/repository"
	"github.com/adenosine-dev/adenosine/internal/transfer"
	"github.com/google/uuid"
	openapi_types "github.com/oapi-codegen/runtime/types"
)

func (handler *apiHandler) ListRepositoryTransfers(w http.ResponseWriter, r *http.Request, owner generated.RepositoryOwnerPath, slug generated.RepositorySlugPath, params generated.ListRepositoryTransfersParams) {
	identity, err := handler.transferIdentity(r, false)
	if err != nil {
		handler.writeError(w, r, err)
		return
	}
	repo, err := handler.transferRepository(r, identity, string(owner), string(slug))
	if err != nil {
		handler.writeError(w, r, err)
		return
	}
	limit, encodedCursor := collectionParameters(params.Limit, params.Cursor)
	scope := "repository-transfers:" + uuid.UUID(repo.ID).String()
	cursor, err := decodeUUIDCollectionCursor(encodedCursor, scope)
	if err != nil {
		handler.writeMalformed(w, r, err)
		return
	}
	page, err := handler.deps.Transfers.Page(r.Context(), repo.ID, identity.accountDID, cursor, limit)
	if err != nil {
		handler.writeError(w, r, err)
		return
	}
	items := make([]generated.RepositoryTransfer, len(page.Items))
	for index, value := range page.Items {
		items[index] = repositoryTransferResponse(value)
	}
	nextCursor := (*string)(nil)
	if page.NextCursor != nil {
		nextCursor, err = encodeCollectionCursor(scope, page.NextCursor.String())
		if err != nil {
			handler.writeError(w, r, err)
			return
		}
	}
	writeJSON(w, http.StatusOK, generated.RepositoryTransferList{Items: items, Page: generated.Page{NextCursor: nextCursor}})
}

func (handler *apiHandler) CreateRepositoryTransfer(w http.ResponseWriter, r *http.Request, owner generated.RepositoryOwnerPath, slug generated.RepositorySlugPath, _ generated.CreateRepositoryTransferParams) {
	identity, err := handler.transferIdentity(r, true)
	if err != nil {
		handler.writeError(w, r, err)
		return
	}
	repo, err := handler.transferRepository(r, identity, string(owner), string(slug))
	if err != nil {
		handler.writeError(w, r, err)
		return
	}
	var request generated.CreateRepositoryTransferRequest
	if err := decodeJSON(w, r, &request); err != nil {
		handler.writeMalformed(w, r, err)
		return
	}
	created, err := handler.deps.Transfers.Initiate(r.Context(), repo, identity.accountDID, request.DestinationOwner)
	if err != nil {
		handler.writeError(w, r, err)
		return
	}
	w.Header().Set("Location", "/api/v1/repository-transfers/"+created.ID.String())
	writeJSON(w, http.StatusAccepted, repositoryTransferResponse(created))
}

func (handler *apiHandler) GetRepositoryTransfer(w http.ResponseWriter, r *http.Request, id openapi_types.UUID) {
	identity, err := handler.transferIdentity(r, false)
	if err != nil {
		handler.writeError(w, r, err)
		return
	}
	value, err := handler.deps.Transfers.Get(r.Context(), uuid.UUID(id), identity.accountDID)
	if err == nil {
		err = validateTransferTokenRepository(identity, value.RepositoryID)
	}
	if err != nil {
		handler.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, repositoryTransferResponse(value))
}

func (handler *apiHandler) DeleteRepositoryTransfer(w http.ResponseWriter, r *http.Request, id openapi_types.UUID, _ generated.DeleteRepositoryTransferParams) {
	identity, value, err := handler.authorizedTransferMutation(r, id)
	if err == nil {
		_, err = handler.deps.Transfers.Cancel(r.Context(), value.ID, identity.accountDID)
	}
	if err != nil {
		handler.writeError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (handler *apiHandler) CreateRepositoryTransferAcceptance(w http.ResponseWriter, r *http.Request, id openapi_types.UUID, _ generated.CreateRepositoryTransferAcceptanceParams) {
	identity, value, err := handler.authorizedTransferMutation(r, id)
	if err == nil {
		value, err = handler.deps.Transfers.Accept(r.Context(), value.ID, identity.accountDID)
	}
	if err != nil {
		handler.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, repositoryTransferResponse(value))
}

func (handler *apiHandler) authorizedTransferMutation(r *http.Request, id openapi_types.UUID) (principal, transfer.Transfer, error) {
	identity, err := handler.transferIdentity(r, true)
	if err != nil {
		return principal{}, transfer.Transfer{}, err
	}
	value, err := handler.deps.Transfers.Get(r.Context(), uuid.UUID(id), identity.accountDID)
	if err == nil {
		err = validateTransferTokenRepository(identity, value.RepositoryID)
	}
	return identity, value, err
}

func (handler *apiHandler) transferIdentity(r *http.Request, mutation bool) (principal, error) {
	identity, err := handler.authenticate(r)
	if err != nil {
		return principal{}, err
	}
	if identity.session {
		if mutation && !handler.validOrigin(r) {
			return principal{}, auth.ErrForbidden
		}
		return identity, nil
	}
	wantedScope := auth.ScopeRepositoryRead
	if mutation {
		wantedScope = auth.ScopeRepositoryWrite
	}
	if !slices.Contains(identity.scopes, wantedScope) && !(wantedScope == auth.ScopeRepositoryRead && slices.Contains(identity.scopes, auth.ScopeRepositoryWrite)) {
		return principal{}, auth.ErrForbidden
	}
	return identity, nil
}

func (handler *apiHandler) transferRepository(r *http.Request, identity principal, owner, slug string) (repository.Repository, error) {
	value, err := handler.deps.Repositories.GetByOwnerSlug(r.Context(), owner, slug)
	if err != nil || value.State != repository.StateActive {
		return repository.Repository{}, repository.ErrNotFound
	}
	if err := validateTransferTokenRepository(identity, value.ID); err != nil {
		return repository.Repository{}, err
	}
	return value, nil
}

func validateTransferTokenRepository(identity principal, repositoryID repository.ID) error {
	if identity.repositoryID != nil && *identity.repositoryID != repositoryID {
		return auth.ErrForbidden
	}
	return nil
}

func repositoryTransferResponse(value transfer.Transfer) generated.RepositoryTransfer {
	response := generated.RepositoryTransfer{
		Id: openapi_types.UUID(value.ID), RepositoryId: openapi_types.UUID(value.RepositoryID),
		SourceOwner: value.SourceOwnerAlias, DestinationOwner: value.Destination.Alias,
		Status: generated.RepositoryTransferStatus(value.Status), CreatedAt: value.CreatedAt,
		ExpiresAt: value.ExpiresAt, AcceptanceStartedAt: value.AcceptanceStartedAt,
		AcceptedAt: value.AcceptedAt, CancelledAt: value.CancelledAt,
	}
	response.SourceRepository = transferIdentityResponse(value.SourceRepository)
	if value.AcceptedByDID != "" {
		response.AcceptedBy = &value.AcceptedByDID
	}
	response.Proposal = transferIdentityResponse(value.Proposal)
	response.Successor = transferIdentityResponse(value.Successor)
	response.Acceptance = transferIdentityResponse(value.Acceptance)
	return response
}

func transferIdentityResponse(value *transfer.Identity) *generated.RepositoryStrongRef {
	if value == nil {
		return nil
	}
	return &generated.RepositoryStrongRef{Uri: value.URI, Cid: value.CID}
}
