package main

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/adenosine-dev/adenosine/internal/auth"
	"github.com/adenosine-dev/adenosine/internal/database"
	"github.com/adenosine-dev/adenosine/internal/repository"
	"github.com/adenosine-dev/adenosine/internal/transfer"
	"github.com/google/uuid"
)

const (
	hostedDID      = "did:plc:cccccccccccccccccccccccc"
	destinationDID = "did:plc:bbbbbbbbbbbbbbbbbbbbbbbb"
	repositorySlug = "hosted-repo"
	testCID        = "bafybeigdyrzt5sfp7udm7hu76uh7y26nf3efuylqabf3oclgtqy55fbzdi"
	hostedURI      = "at://" + hostedDID + "/dev.adenosine.repo/0198a8512a897ae2a370dc68883e3af5"
	proposalURI    = "at://" + hostedDID + "/dev.adenosine.repositoryTransfer/0198a8512a897ae2a370dc68883e3af6"
	successorURI   = "at://" + destinationDID + "/dev.adenosine.repo/0198a8512a897ae2a370dc68883e3af5"
)

var transferTime = time.Date(2026, 8, 9, 16, 0, 0, 0, time.UTC)

type fixedClock struct{}

func (fixedClock) Now() time.Time { return transferTime }

type fixedIDs struct{}

func (fixedIDs) New() (uuid.UUID, error) {
	return uuid.MustParse("0198a851-2a89-7ae2-a370-dc68883e3af6"), nil
}

type portablePublisher struct{}

func (portablePublisher) PublishProposal(_ context.Context, value transfer.ProposalPublication) (transfer.Identity, error) {
	if value.ActorDID != hostedDID || value.Repository != (transfer.Identity{URI: hostedURI, CID: testCID}) || value.DestinationDID != destinationDID || value.DestinationOwnerAlias != "bob.example" {
		return transfer.Identity{}, fmt.Errorf("unexpected proposal publication: %+v", value)
	}
	return transfer.Identity{URI: proposalURI, CID: testCID}, nil
}

func (portablePublisher) DeleteProposal(context.Context, transfer.ProposalPublication, transfer.Identity) error {
	return nil
}

func (portablePublisher) PublishAcceptance(_ context.Context, value transfer.AcceptancePublication) (transfer.Identity, error) {
	if value.ActorDID != destinationDID || value.Proposal != (transfer.Identity{URI: proposalURI, CID: testCID}) || value.Repository != (transfer.Identity{URI: successorURI, CID: testCID}) {
		return transfer.Identity{}, fmt.Errorf("unexpected acceptance publication: %+v", value)
	}
	return transfer.Identity{URI: "at://" + destinationDID + "/dev.adenosine.repositoryTransferAcceptance/" + transfer.AcceptanceRecordKey(proposalURI), CID: testCID}, nil
}

type repositoryPublisher struct{}

func (repositoryPublisher) Publish(_ context.Context, value repository.Publication) (repository.ATIdentity, error) {
	switch {
	case value.TransferredFrom != nil:
		if value.OwnerDID != destinationDID || *value.TransferredFrom != (repository.ATIdentity{URI: hostedURI, CID: testCID}) {
			return repository.ATIdentity{}, fmt.Errorf("unexpected successor publication: %+v", value)
		}
		return repository.ATIdentity{URI: successorURI, CID: testCID}, nil
	case value.TransferredTo != nil:
		if value.OwnerDID != hostedDID || *value.TransferredTo != (repository.ATIdentity{URI: successorURI, CID: testCID}) {
			return repository.ATIdentity{}, fmt.Errorf("unexpected source redirect publication: %+v", value)
		}
		return repository.ATIdentity{URI: hostedURI, CID: testCID}, nil
	default:
		return repository.ATIdentity{}, fmt.Errorf("transfer relationship missing from publication: %+v", value)
	}
}

func main() {
	if err := run(context.Background()); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Println("local repository transfer integration passed")
}

func run(ctx context.Context) error {
	databaseURL := strings.TrimSpace(os.Getenv("DATABASE_URL"))
	if databaseURL == "" {
		return fmt.Errorf("DATABASE_URL is required")
	}
	db, err := database.Open(ctx, databaseURL)
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}
	defer db.Close()

	accounts := auth.NewPostgresStore(db.Queries())
	if _, err := accounts.UpsertLogin(ctx, destinationDID, "bob.example", transferTime); err != nil {
		return fmt.Errorf("create destination account: %w", err)
	}
	repositories := repository.NewPostgresStore(db.Queries())
	before, err := repositories.GetByOwnerSlug(ctx, hostedDID, repositorySlug)
	if err != nil {
		return fmt.Errorf("load source repository: %w", err)
	}
	service := transfer.NewService(
		transfer.NewPostgresStore(db.Queries()), portablePublisher{}, repositoryPublisher{},
		repository.Must("https://adenosine-a-tls", "adenosine-a", 2222), fixedIDs{}, fixedClock{},
	)
	created, err := service.Initiate(ctx, before, hostedDID, "bob.example")
	if err != nil {
		return fmt.Errorf("initiate transfer: %w", err)
	}
	completed, err := service.Accept(ctx, created.ID, destinationDID)
	if err != nil {
		return fmt.Errorf("accept transfer: %w", err)
	}
	if completed.Status != transfer.StatusCompleted || completed.AcceptedByDID != destinationDID {
		return fmt.Errorf("unexpected completed transfer: %+v", completed)
	}

	for _, owner := range []string{hostedDID, "hosted.example", destinationDID, "bob.example"} {
		resolved, err := repositories.GetByOwnerSlug(ctx, owner, repositorySlug)
		if err != nil {
			return fmt.Errorf("resolve retained route %s: %w", owner, err)
		}
		if resolved.ID != before.ID || resolved.StorageKey != before.StorageKey || resolved.OwnerDID != destinationDID || resolved.ATURI != successorURI {
			return fmt.Errorf("route %s resolved unexpected repository: %+v", owner, resolved)
		}
	}
	return nil
}
