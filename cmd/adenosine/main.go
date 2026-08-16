package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/adenosine-dev/adenosine/internal/cli"
	"github.com/adenosine-dev/adenosine/internal/config"
	"github.com/adenosine-dev/adenosine/internal/database/migration"
	"github.com/adenosine-dev/adenosine/internal/di"
)

func main() {
	command := "serve"
	if len(os.Args) > 1 {
		command = os.Args[1]
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if command != "serve" && command != "migrate" {
		if err := cli.Run(ctx, os.Args[1:], os.Stdin, os.Stdout, os.Stderr); err != nil {
			_, _ = fmt.Fprintln(os.Stderr, err)
			if errors.Is(err, cli.ErrUsage) {
				os.Exit(2)
			}
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
