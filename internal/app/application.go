// Package app owns process lifecycle after dependencies are composed.
package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"time"
)

// Application is the runnable Adenosine process.
type Application struct {
	httpServer      *http.Server
	sshServer       sshServer
	logger          *slog.Logger
	shutdownTimeout time.Duration
	cleanup         []func(context.Context) error
	workers         []Worker
	workerRetry     time.Duration
}

type sshServer interface {
	Address() string
	ListenAndServe() error
	Shutdown(context.Context) error
}

// Worker is a required background process tied to the application lifecycle.
type Worker interface{ Run(context.Context) error }

// New constructs an application from already initialized dependencies.
func New(httpServer *http.Server, sshServer sshServer, logger *slog.Logger, shutdownTimeout time.Duration, cleanup ...func(context.Context) error) *Application {
	return &Application{httpServer: httpServer, sshServer: sshServer, logger: logger, shutdownTimeout: shutdownTimeout, cleanup: cleanup, workerRetry: time.Second}
}

// NewWithWorkers constructs an application with supervised background workers.
func NewWithWorkers(httpServer *http.Server, sshServer sshServer, logger *slog.Logger, shutdownTimeout time.Duration, workers []Worker, cleanup ...func(context.Context) error) *Application {
	return &Application{httpServer: httpServer, sshServer: sshServer, logger: logger, shutdownTimeout: shutdownTimeout, workers: workers, cleanup: cleanup, workerRetry: time.Second}
}

// Run serves until cancellation or a server failure, then closes dependencies.
func (a *Application) Run(ctx context.Context) error {
	serverErrors := make(chan error, 2)
	go func() {
		a.logger.Info("adenosine listening", "component", "http", "address", a.httpServer.Addr)
		serverErrors <- a.httpServer.ListenAndServe()
	}()
	go func() {
		serverErrors <- a.sshServer.ListenAndServe()
	}()
	workerCtx, stopWorkers := context.WithCancel(ctx)
	defer stopWorkers()
	for _, backgroundWorker := range a.workers {
		go a.superviseWorker(workerCtx, backgroundWorker)
	}

	var runErr error
	select {
	case <-ctx.Done():
	case err := <-serverErrors:
		if !errors.Is(err, http.ErrServerClosed) && !errors.Is(err, net.ErrClosed) {
			runErr = err
		}
	}
	stopWorkers()

	shutdownCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), a.shutdownTimeout)
	defer cancel()
	shutdownErr := errors.Join(a.sshServer.Shutdown(shutdownCtx), a.httpServer.Shutdown(shutdownCtx))
	for i := len(a.cleanup) - 1; i >= 0; i-- {
		shutdownErr = errors.Join(shutdownErr, a.cleanup[i](shutdownCtx))
	}
	return errors.Join(runErr, shutdownErr)
}

func (a *Application) superviseWorker(ctx context.Context, worker Worker) {
	name := fmt.Sprintf("%T", worker)
	for {
		err := worker.Run(ctx)
		if ctx.Err() != nil {
			return
		}
		if err != nil {
			a.logger.Error("background worker stopped", "component", "worker", "worker", name, "error", err)
		} else {
			a.logger.Error("background worker stopped unexpectedly", "component", "worker", "worker", name)
		}

		timer := time.NewTimer(a.workerRetry)
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				<-timer.C
			}
			return
		case <-timer.C:
		}
	}
}
