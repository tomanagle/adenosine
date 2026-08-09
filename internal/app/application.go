// Package app owns process lifecycle after dependencies are composed.
package app

import (
	"context"
	"errors"
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
}

type sshServer interface {
	Address() string
	ListenAndServe() error
	Shutdown(context.Context) error
}

// New constructs an application from already initialized dependencies.
func New(httpServer *http.Server, sshServer sshServer, logger *slog.Logger, shutdownTimeout time.Duration, cleanup ...func(context.Context) error) *Application {
	return &Application{httpServer: httpServer, sshServer: sshServer, logger: logger, shutdownTimeout: shutdownTimeout, cleanup: cleanup}
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

	var runErr error
	select {
	case <-ctx.Done():
	case err := <-serverErrors:
		if !errors.Is(err, http.ErrServerClosed) && !errors.Is(err, net.ErrClosed) {
			runErr = err
		}
	}

	shutdownCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), a.shutdownTimeout)
	defer cancel()
	shutdownErr := errors.Join(a.sshServer.Shutdown(shutdownCtx), a.httpServer.Shutdown(shutdownCtx))
	for i := len(a.cleanup) - 1; i >= 0; i-- {
		shutdownErr = errors.Join(shutdownErr, a.cleanup[i](shutdownCtx))
	}
	return errors.Join(runErr, shutdownErr)
}
