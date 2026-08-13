package repository

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
)

type deletionPurgeStore interface {
	ListDueDeletions(context.Context, time.Time, *uuid.UUID, int32) ([]Deletion, error)
	MarkPurged(context.Context, uuid.UUID, time.Time) error
}

type gitPurger interface {
	Purge(context.Context, ID) error
}

// PurgeWorker permanently removes quarantined Git data after retention expires.
type PurgeWorker struct {
	store deletionPurgeStore
	git   gitPurger
	now   func() time.Time
}

func NewPurgeWorker(store deletionPurgeStore, git gitPurger) *PurgeWorker {
	return &PurgeWorker{store: store, git: git, now: time.Now}
}

func (worker *PurgeWorker) Run(ctx context.Context) error {
	ticker := time.NewTicker(time.Hour)
	defer ticker.Stop()
	for {
		if err := worker.purge(ctx); err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return err
		}
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		}
	}
}

func (worker *PurgeWorker) purge(ctx context.Context) error {
	var after *uuid.UUID
	for {
		deletions, err := worker.store.ListDueDeletions(ctx, worker.now().UTC(), after, 100)
		if err != nil {
			return fmt.Errorf("list repository deletions due for purge: %w", err)
		}
		for _, deletion := range deletions {
			if err := worker.git.Purge(ctx, deletion.RepositoryID); err != nil {
				return fmt.Errorf("purge repository %s: %w", deletion.RepositoryID.String(), err)
			}
			if err := worker.store.MarkPurged(ctx, deletion.ID, worker.now().UTC()); err != nil {
				return fmt.Errorf("complete repository purge: %w", err)
			}
		}
		if len(deletions) < 100 {
			return nil
		}
		value := deletions[len(deletions)-1].ID
		after = &value
	}
}
