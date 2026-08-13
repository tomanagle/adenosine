package app

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sync/atomic"
	"testing"
	"time"
)

type retryWorker struct {
	calls atomic.Int32
}

func (worker *retryWorker) Run(ctx context.Context) error {
	if worker.calls.Add(1) == 1 {
		return errors.New("temporary database outage")
	}
	<-ctx.Done()
	return nil
}

func TestApplicationSuperviseWorker(t *testing.T) {
	t.Parallel()
	testCases := []struct {
		name      string
		wantCalls int32
	}{
		{name: "restarts a transiently failed worker", wantCalls: 2},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			worker := &retryWorker{}
			application := &Application{
				logger:      slog.New(slog.NewTextHandler(io.Discard, nil)),
				workerRetry: time.Millisecond,
			}
			done := make(chan struct{})
			go func() {
				application.superviseWorker(ctx, worker)
				close(done)
			}()

			deadline := time.NewTimer(time.Second)
			defer deadline.Stop()
			for worker.calls.Load() < testCase.wantCalls {
				select {
				case <-deadline.C:
					t.Fatalf("worker calls = %d, want at least %d", worker.calls.Load(), testCase.wantCalls)
				default:
					time.Sleep(time.Millisecond)
				}
			}
			cancel()
			select {
			case <-done:
			case <-deadline.C:
				t.Fatal("worker supervisor did not stop after cancellation")
			}
		})
	}
}
