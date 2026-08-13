package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/adenosine-dev/adenosine/internal/config"
	"github.com/adenosine-dev/adenosine/internal/database/migration"
	"github.com/adenosine-dev/adenosine/internal/di"
	"github.com/adenosine-dev/adenosine/internal/pushauthorization"
)

func main() {
	command := "serve"
	if len(os.Args) > 1 {
		command = os.Args[1]
	}
	if command != "serve" && command != "migrate" && command != "authorize-push" {
		_, _ = fmt.Fprintf(os.Stderr, "usage: %s [serve|migrate|authorize-push]\n", os.Args[0])
		os.Exit(2)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if command == "authorize-push" {
		if err := pushauthorization.Run(ctx, os.Stdin); err != nil {
			_, _ = fmt.Fprintf(os.Stderr, "Adenosine: %s\n", err)
			os.Exit(1)
		}
		return
	}

	cfg := config.Must()
	if command == "migrate" {
		migration.Must(ctx, cfg.DatabaseURL)
		return
	}
	application := di.Must(ctx, cfg)
	if err := application.Run(ctx); err != nil {
		slog.Error("application stopped", "error", err)
		os.Exit(1)
	}

}
