// Package moderation manages account-local filtering preferences.
package moderation

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/adenosine-dev/adenosine/internal/issue"
	"github.com/bluesky-social/indigo/atproto/syntax"
)

// BlockedDID is an account-local blocked identity.
type BlockedDID struct {
	DID       string
	CreatedAt time.Time
}

// HiddenRecord is an account-local hidden AT Protocol record.
type HiddenRecord struct {
	URI       string
	CreatedAt time.Time
}

type store interface {
	PutBlock(context.Context, string, string, time.Time) error
	DeleteBlock(context.Context, string, string) error
	ListBlocks(context.Context, string) ([]BlockedDID, error)
	PutHidden(context.Context, string, string, time.Time) error
	DeleteHidden(context.Context, string, string) error
	ListHidden(context.Context, string) ([]HiddenRecord, error)
}

type clock interface{ Now() time.Time }

// Service manages local moderation preferences without mutating network records.
type Service struct {
	store store
	clock clock
}

// NewService constructs the moderation application service.
func NewService(store store, clock clock) *Service { return &Service{store: store, clock: clock} }

// Block idempotently excludes a DID from an account's authenticated projections.
func (service *Service) Block(ctx context.Context, accountDID, blockedDID string) error {
	if err := validateDID(accountDID, "accountDID"); err != nil {
		return err
	}
	if err := validateDID(blockedDID, "blockedDID"); err != nil {
		return err
	}
	if accountDID == blockedDID {
		return &issue.ValidationError{Field: "blockedDID", Problem: "must differ from accountDID"}
	}
	return wrap("block DID", service.store.PutBlock(ctx, accountDID, blockedDID, service.clock.Now().UTC()))
}

// Unblock idempotently removes an account's DID filter.
func (service *Service) Unblock(ctx context.Context, accountDID, blockedDID string) error {
	if err := validateDID(accountDID, "accountDID"); err != nil {
		return err
	}
	if err := validateDID(blockedDID, "blockedDID"); err != nil {
		return err
	}
	return wrap("unblock DID", service.store.DeleteBlock(ctx, accountDID, blockedDID))
}

// ListBlocks returns the account's blocked DIDs.
func (service *Service) ListBlocks(ctx context.Context, accountDID string) ([]BlockedDID, error) {
	if err := validateDID(accountDID, "accountDID"); err != nil {
		return nil, err
	}
	values, err := service.store.ListBlocks(ctx, accountDID)
	if err != nil {
		return nil, wrap("list blocked DIDs", err)
	}
	if values == nil {
		values = []BlockedDID{}
	}
	return values, nil
}

// Hide idempotently excludes a record from an account's authenticated projections.
func (service *Service) Hide(ctx context.Context, accountDID, recordURI string) error {
	if err := validateDID(accountDID, "accountDID"); err != nil {
		return err
	}
	if err := validateRecordURI(recordURI); err != nil {
		return err
	}
	return wrap("hide record", service.store.PutHidden(ctx, accountDID, recordURI, service.clock.Now().UTC()))
}

// Unhide idempotently removes an account's record filter.
func (service *Service) Unhide(ctx context.Context, accountDID, recordURI string) error {
	if err := validateDID(accountDID, "accountDID"); err != nil {
		return err
	}
	if err := validateRecordURI(recordURI); err != nil {
		return err
	}
	return wrap("unhide record", service.store.DeleteHidden(ctx, accountDID, recordURI))
}

// ListHidden returns the account's hidden records.
func (service *Service) ListHidden(ctx context.Context, accountDID string) ([]HiddenRecord, error) {
	if err := validateDID(accountDID, "accountDID"); err != nil {
		return nil, err
	}
	values, err := service.store.ListHidden(ctx, accountDID)
	if err != nil {
		return nil, wrap("list hidden records", err)
	}
	if values == nil {
		values = []HiddenRecord{}
	}
	return values, nil
}

func validateDID(value, field string) error {
	did, err := syntax.ParseDID(value)
	if err != nil || did.String() != value {
		return &issue.ValidationError{Field: field, Problem: "must be a canonical AT Protocol DID"}
	}
	return nil
}

func validateRecordURI(value string) error {
	uri, err := syntax.ParseATURI(value)
	if err != nil || uri.String() != value || uri.Collection().String() == "" || uri.RecordKey().String() == "" {
		return &issue.ValidationError{Field: "recordURI", Problem: "must be a canonical AT Protocol record URI"}
	}
	did, err := uri.Authority().AsDID()
	if err != nil || did.String() != uri.Authority().String() {
		return &issue.ValidationError{Field: "recordURI", Problem: "must use a canonical DID authority"}
	}
	return nil
}

func wrap(operation string, err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, issue.ErrNotFound) {
		return issue.ErrNotFound
	}
	return fmt.Errorf("%s: %w", operation, err)
}
