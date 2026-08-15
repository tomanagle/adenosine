package transfer

import (
	"context"
	"errors"
	"fmt"
	"time"

	dbgen "github.com/adenosine-dev/adenosine/internal/database/generated"
	"github.com/adenosine-dev/adenosine/internal/repository"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
)

// PostgresStore persists transfer state and resolves destination owners.
type PostgresStore struct{ queries *dbgen.Queries }

func NewPostgresStore(queries *dbgen.Queries) *PostgresStore { return &PostgresStore{queries: queries} }

func (store *PostgresStore) ResolveOwner(ctx context.Context, alias string) (Owner, error) {
	row, err := store.queries.ResolveRepositoryTransferOwner(ctx, alias)
	if err != nil {
		return Owner{}, mapError(err)
	}
	owner := Owner{Alias: row.Alias}
	switch row.Kind {
	case string(OwnerAccount):
		if !row.AccountDid.Valid {
			return Owner{}, fmt.Errorf("resolve transfer owner: %w", ErrNotFound)
		}
		owner.Kind, owner.AccountDID, owner.RecordAuthorDID = OwnerAccount, row.AccountDid.String, row.AccountDid.String
	case string(OwnerOrganization):
		if !row.OrganizationID.Valid || !row.CreatorDid.Valid {
			return Owner{}, fmt.Errorf("resolve transfer owner: %w", ErrNotFound)
		}
		id := uuid.UUID(row.OrganizationID.Bytes)
		owner.Kind, owner.OrganizationID = OwnerOrganization, &id
		owner.AccountDID, owner.RecordAuthorDID = row.CreatorDid.String, row.CreatorDid.String
		if row.OrganizationUri.Valid && row.OrganizationCid.Valid {
			owner.Organization = &repository.ATIdentity{URI: row.OrganizationUri.String, CID: row.OrganizationCid.String}
		}
	default:
		return Owner{}, fmt.Errorf("resolve transfer owner: %w", ErrNotFound)
	}
	return owner, nil
}

func (store *PostgresStore) ResolveOrganizationIdentity(ctx context.Context, id uuid.UUID) (repository.ATIdentity, error) {
	row, err := store.queries.GetOrganizationByID(ctx, requiredUUID(id))
	if err != nil {
		return repository.ATIdentity{}, mapError(err)
	}
	if !row.AtUri.Valid || !row.AtCid.Valid {
		return repository.ATIdentity{}, fmt.Errorf("resolve transfer organization: %w", ErrValidation)
	}
	return repository.ATIdentity{URI: row.AtUri.String, CID: row.AtCid.String}, nil
}

func (store *PostgresStore) CanInitiate(ctx context.Context, repositoryID repository.ID, actorDID string) (bool, error) {
	allowed, err := store.queries.CanInitiateRepositoryTransfer(ctx, dbgen.CanInitiateRepositoryTransferParams{
		RepositoryID: requiredUUID(uuid.UUID(repositoryID)), AccountDid: actorDID,
	})
	if err != nil {
		return false, fmt.Errorf("check source ownership: %w", err)
	}
	return allowed, nil
}

func (store *PostgresStore) ResolveSourceAlias(ctx context.Context, repositoryID repository.ID) (string, error) {
	alias, err := store.queries.ResolveRepositoryTransferSourceAlias(ctx, requiredUUID(uuid.UUID(repositoryID)))
	if err != nil {
		return "", mapError(err)
	}
	return alias, nil
}

func (store *PostgresStore) CanAccept(ctx context.Context, owner Owner, actorDID string) (bool, error) {
	allowed, err := store.queries.CanAcceptRepositoryTransfer(ctx, dbgen.CanAcceptRepositoryTransferParams{
		OrganizationID: optionalUUID(owner.OrganizationID), AccountDid: actorDID, DestinationOwnerDid: owner.AccountDID,
	})
	if err != nil {
		return false, fmt.Errorf("check destination ownership: %w", err)
	}
	return allowed, nil
}

func (store *PostgresStore) CanComplete(ctx context.Context, id uuid.UUID) (bool, error) {
	allowed, err := store.queries.CanCompleteRepositoryTransfer(ctx, requiredUUID(id))
	if err != nil {
		return false, fmt.Errorf("check destination route: %w", err)
	}
	return allowed, nil
}

func (store *PostgresStore) GetRepository(ctx context.Context, id repository.ID) (repository.Repository, error) {
	row, err := store.queries.GetRepositoryForTransfer(ctx, pgtype.UUID{Bytes: uuid.UUID(id), Valid: true})
	if err != nil {
		return repository.Repository{}, mapError(err)
	}
	return repositoryFromRow(row), nil
}

func (store *PostgresStore) GetPending(ctx context.Context, id repository.ID) (Transfer, error) {
	row, err := store.queries.GetPendingRepositoryTransfer(ctx, pgtype.UUID{Bytes: uuid.UUID(id), Valid: true})
	if err != nil {
		return Transfer{}, mapError(err)
	}
	return transferFromRow(row), nil
}

func (store *PostgresStore) Create(ctx context.Context, value Transfer) (Transfer, error) {
	row, err := store.queries.CreateRepositoryTransfer(ctx, dbgen.CreateRepositoryTransferParams{
		ID: requiredUUID(value.ID), RepositoryID: requiredUUID(uuid.UUID(value.RepositoryID)),
		SourceOwnerDid: value.SourceOwnerDID, SourceOrganizationID: optionalUUID(value.SourceOrganizationID),
		SourceOwnerAlias: value.SourceOwnerAlias, SourceRepositoryUri: identityURI(value.SourceRepository),
		SourceRepositoryCid: identityCID(value.SourceRepository), DestinationOwnerDid: value.Destination.AccountDID,
		DestinationOrganizationID: optionalUUID(value.Destination.OrganizationID), DestinationOwnerAlias: value.Destination.Alias,
		InitiatedByDid: value.InitiatedByDID, CreatedAt: requiredTime(value.CreatedAt), ExpiresAt: requiredTime(value.ExpiresAt),
	})
	if err != nil {
		return Transfer{}, mapError(err)
	}
	return transferFromRow(row), nil
}

func (store *PostgresStore) Get(ctx context.Context, id uuid.UUID) (Transfer, error) {
	row, err := store.queries.GetRepositoryTransfer(ctx, requiredUUID(id))
	if err != nil {
		return Transfer{}, mapError(err)
	}
	return transferFromRow(row), nil
}

func (store *PostgresStore) Page(ctx context.Context, repositoryID repository.ID, after *uuid.UUID, limit int32) ([]Transfer, error) {
	rows, err := store.queries.PageRepositoryTransfers(ctx, dbgen.PageRepositoryTransfersParams{
		RepositoryID: requiredUUID(uuid.UUID(repositoryID)), AfterID: optionalUUID(after), PageLimit: limit,
	})
	if err != nil {
		return nil, fmt.Errorf("query transfer history: %w", err)
	}
	values := make([]Transfer, len(rows))
	for index, row := range rows {
		values[index] = transferFromRow(row)
	}
	return values, nil
}

func (store *PostgresStore) SetProposal(ctx context.Context, id uuid.UUID, identity Identity) (Transfer, error) {
	row, err := store.queries.SetRepositoryTransferProposal(ctx, dbgen.SetRepositoryTransferProposalParams{ID: requiredUUID(id), Uri: requiredText(identity.URI), Cid: requiredText(identity.CID)})
	return mappedTransfer(row, err)
}

func (store *PostgresStore) SetSuccessor(ctx context.Context, id uuid.UUID, identity Identity) (Transfer, error) {
	row, err := store.queries.SetRepositoryTransferSuccessor(ctx, dbgen.SetRepositoryTransferSuccessorParams{ID: requiredUUID(id), Uri: requiredText(identity.URI), Cid: requiredText(identity.CID)})
	return mappedTransfer(row, err)
}

func (store *PostgresStore) StartAcceptance(ctx context.Context, id uuid.UUID, startedAt time.Time) (Transfer, error) {
	row, err := store.queries.StartRepositoryTransferAcceptance(ctx, dbgen.StartRepositoryTransferAcceptanceParams{ID: requiredUUID(id), StartedAt: requiredTime(startedAt)})
	if errors.Is(err, pgx.ErrNoRows) {
		return Transfer{}, ErrConflict
	}
	return mappedTransfer(row, err)
}

func (store *PostgresStore) SetAcceptance(ctx context.Context, id uuid.UUID, identity Identity) (Transfer, error) {
	row, err := store.queries.SetRepositoryTransferAcceptance(ctx, dbgen.SetRepositoryTransferAcceptanceParams{ID: requiredUUID(id), Uri: requiredText(identity.URI), Cid: requiredText(identity.CID)})
	return mappedTransfer(row, err)
}

func (store *PostgresStore) SetSourceRedirect(ctx context.Context, id uuid.UUID, cid string) (Transfer, error) {
	row, err := store.queries.SetRepositoryTransferSourceRedirect(ctx, dbgen.SetRepositoryTransferSourceRedirectParams{ID: requiredUUID(id), Cid: requiredText(cid)})
	return mappedTransfer(row, err)
}

func (store *PostgresStore) Cancel(ctx context.Context, id uuid.UUID, cancelledAt time.Time) (Transfer, error) {
	row, err := store.queries.CancelRepositoryTransfer(ctx, dbgen.CancelRepositoryTransferParams{ID: requiredUUID(id), CancelledAt: requiredTime(cancelledAt)})
	return mappedTransfer(row, err)
}

func (store *PostgresStore) Complete(ctx context.Context, id, aliasID, sourceDIDAliasID uuid.UUID, actorDID string, acceptedAt time.Time) (Transfer, error) {
	row, err := store.queries.CompleteRepositoryTransfer(ctx, dbgen.CompleteRepositoryTransferParams{ID: requiredUUID(id), AliasID: requiredUUID(aliasID), SourceDidAliasID: requiredUUID(sourceDIDAliasID), AcceptedByDid: requiredText(actorDID), AcceptedAt: requiredTime(acceptedAt)})
	if errors.Is(err, pgx.ErrNoRows) {
		return Transfer{}, ErrConflict
	}
	return mappedTransfer(row, err)
}

func (store *PostgresStore) CompletePrivate(ctx context.Context, id, aliasID, sourceDIDAliasID uuid.UUID, actorDID string, acceptedAt time.Time) (Transfer, error) {
	row, err := store.queries.CompletePrivateRepositoryTransfer(ctx, dbgen.CompletePrivateRepositoryTransferParams{ID: requiredUUID(id), AliasID: requiredUUID(aliasID), SourceDidAliasID: requiredUUID(sourceDIDAliasID), AcceptedByDid: requiredText(actorDID), AcceptedAt: requiredTime(acceptedAt)})
	if errors.Is(err, pgx.ErrNoRows) {
		return Transfer{}, ErrConflict
	}
	return mappedTransfer(row, err)
}

func mappedTransfer(row dbgen.CoreRepositoryTransfer, err error) (Transfer, error) {
	if err != nil {
		return Transfer{}, mapError(err)
	}
	return transferFromRow(row), nil
}

func transferFromRow(row dbgen.CoreRepositoryTransfer) Transfer {
	value := Transfer{
		ID: uuid.UUID(row.ID.Bytes), RepositoryID: repository.ID(row.RepositoryID.Bytes),
		SourceOwnerDID: row.SourceOwnerDid, SourceOwnerAlias: row.SourceOwnerAlias,
		Destination:    Owner{Alias: row.DestinationOwnerAlias, AccountDID: row.DestinationOwnerDid, RecordAuthorDID: row.DestinationOwnerDid},
		InitiatedByDID: row.InitiatedByDid, AcceptedByDID: row.AcceptedByDid.String, SourceRedirectCID: row.SourceRedirectCid.String,
		Status: Status(row.Status), CreatedAt: row.CreatedAt.Time, ExpiresAt: row.ExpiresAt.Time,
	}
	if row.SourceOrganizationID.Valid {
		id := uuid.UUID(row.SourceOrganizationID.Bytes)
		value.SourceOrganizationID = &id
	}
	if row.DestinationOrganizationID.Valid {
		id := uuid.UUID(row.DestinationOrganizationID.Bytes)
		value.Destination.Kind, value.Destination.OrganizationID = OwnerOrganization, &id
	} else {
		value.Destination.Kind = OwnerAccount
	}
	value.SourceRepository = optionalIdentity(row.SourceRepositoryUri, row.SourceRepositoryCid)
	value.Proposal = optionalIdentity(row.ProposalUri, row.ProposalCid)
	value.Successor = optionalIdentity(row.SuccessorUri, row.SuccessorCid)
	value.Acceptance = optionalIdentity(row.AcceptanceUri, row.AcceptanceCid)
	if row.AcceptanceStartedAt.Valid {
		startedAt := row.AcceptanceStartedAt.Time
		value.AcceptanceStartedAt = &startedAt
	}
	if row.AcceptedAt.Valid {
		acceptedAt := row.AcceptedAt.Time
		value.AcceptedAt = &acceptedAt
	}
	if row.CancelledAt.Valid {
		cancelledAt := row.CancelledAt.Time
		value.CancelledAt = &cancelledAt
	}
	return value
}

func repositoryFromRow(row dbgen.CoreRepository) repository.Repository {
	value := repository.Repository{
		ID: repository.ID(row.ID.Bytes), OwnerDID: row.OwnerDid, Slug: row.Slug,
		DisplayName: row.DisplayName.String, Description: row.Description.String,
		Visibility: repository.Visibility(row.Visibility), State: repository.State(row.State),
		DefaultBranch: row.DefaultBranch, StorageKey: row.StorageKey, ATURI: row.AtUri.String,
		ATCID: row.AtCid.String, ForkCount: row.ForkCount, CreatedAt: row.CreatedAt.Time, UpdatedAt: row.UpdatedAt.Time,
	}
	if row.OrganizationID.Valid {
		id := uuid.UUID(row.OrganizationID.Bytes)
		value.OrganizationID = &id
	}
	if row.TransferredFromUri.Valid && row.TransferredFromCid.Valid {
		value.TransferredFrom = &repository.ATIdentity{URI: row.TransferredFromUri.String, CID: row.TransferredFromCid.String}
	}
	if row.ArchivedAt.Valid {
		archivedAt := row.ArchivedAt.Time
		value.ArchivedAt = &archivedAt
	}
	return value
}

func optionalIdentity(uri, cid pgtype.Text) *Identity {
	if !uri.Valid || !cid.Valid {
		return nil
	}
	return &Identity{URI: uri.String, CID: cid.String}
}

func mapError(err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	var postgresError *pgconn.PgError
	if errors.As(err, &postgresError) && postgresError.Code == "23505" {
		return ErrConflict
	}
	return fmt.Errorf("repository transfer query: %w", err)
}

func requiredUUID(value uuid.UUID) pgtype.UUID { return pgtype.UUID{Bytes: value, Valid: true} }
func optionalUUID(value *uuid.UUID) pgtype.UUID {
	if value == nil {
		return pgtype.UUID{}
	}
	return requiredUUID(*value)
}
func requiredText(value string) pgtype.Text { return pgtype.Text{String: value, Valid: true} }
func requiredTime(value time.Time) pgtype.Timestamptz {
	return pgtype.Timestamptz{Time: value.UTC(), Valid: true}
}
func identityURI(value *Identity) pgtype.Text {
	if value == nil {
		return pgtype.Text{}
	}
	return requiredText(value.URI)
}
func identityCID(value *Identity) pgtype.Text {
	if value == nil {
		return pgtype.Text{}
	}
	return requiredText(value.CID)
}
