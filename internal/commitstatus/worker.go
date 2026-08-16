package commitstatus

import (
	"context"
	"fmt"
	"time"

	dbgen "github.com/adenosine-dev/adenosine/internal/database/generated"
)

// RetentionWorker removes expired history while preserving the latest result per context.
type RetentionWorker struct {
	queries *dbgen.Queries
	now     func() time.Time
	period  time.Duration
}

func NewRetentionWorker(queries *dbgen.Queries) *RetentionWorker {
	return &RetentionWorker{queries: queries, now: time.Now, period: 24 * time.Hour}
}

func (worker *RetentionWorker) Run(ctx context.Context) error {
	if err := worker.cleanup(ctx); err != nil {
		return err
	}
	ticker := time.NewTicker(worker.period)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			if err := worker.cleanup(ctx); err != nil {
				return err
			}
		}
	}
}

func (worker *RetentionWorker) cleanup(ctx context.Context) error {
	cutoff := pgTime(worker.now().UTC())
	if _, err := worker.queries.DeleteExpiredCommitStatuses(ctx, cutoff); err != nil {
		return fmt.Errorf("delete expired commit status history: %w", err)
	}
	if _, err := worker.queries.DeleteExpiredCheckRuns(ctx, cutoff); err != nil {
		return fmt.Errorf("delete expired check run history: %w", err)
	}
	return nil
}
