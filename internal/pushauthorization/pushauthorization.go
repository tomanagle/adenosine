// Package pushauthorization is the managed Git pre-receive entrypoint.
package pushauthorization

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/adenosine-dev/adenosine/internal/branchprotection"
	"github.com/adenosine-dev/adenosine/internal/database"
	gitservice "github.com/adenosine-dev/adenosine/internal/git"
	"github.com/adenosine-dev/adenosine/internal/repository"
	"github.com/google/uuid"
)

const maxRefUpdates = 100

// Run evaluates one receive-pack command set and returns only client-safe errors.
func Run(ctx context.Context, input io.Reader) error {
	cfg, err := loadConfig()
	if err != nil {
		return errors.New("branch protection configuration is unavailable")
	}
	updates, err := parseUpdates(input)
	if err != nil {
		return errors.New("the proposed ref update is malformed")
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	db, err := database.Open(ctx, cfg.databaseURL)
	if err != nil {
		return errors.New("branch protection could not reach its policy store")
	}
	defer db.Close()
	git, err := gitservice.NewHookService(gitservice.NewRunner(cfg.gitBinary), cfg.repositoryPath)
	if err != nil {
		return errors.New("branch protection could not inspect the proposed commits")
	}
	service := branchprotection.NewService(db.Queries(), git)
	err = service.Authorize(ctx, cfg.repositoryID, updates)
	var rejection *branchprotection.Rejection
	if errors.As(err, &rejection) {
		return rejection
	}
	if err != nil {
		return errors.New("branch protection could not evaluate this update")
	}
	return nil
}

type hookConfig struct {
	databaseURL    string
	gitBinary      string
	repositoryID   repository.ID
	repositoryPath string
}

func loadConfig() (hookConfig, error) {
	id, err := uuid.Parse(strings.TrimSpace(os.Getenv("ADENOSINE_HOOK_REPOSITORY_ID")))
	if err != nil {
		return hookConfig{}, err
	}
	directory := strings.TrimSpace(os.Getenv("GIT_DIR"))
	if directory == "" {
		return hookConfig{}, errors.New("GIT_DIR is required")
	}
	directory, err = filepath.Abs(directory)
	if err != nil {
		return hookConfig{}, err
	}
	cfg := hookConfig{
		databaseURL:  strings.TrimSpace(os.Getenv("ADENOSINE_HOOK_DATABASE_URL")),
		gitBinary:    strings.TrimSpace(os.Getenv("ADENOSINE_HOOK_GIT_BINARY")),
		repositoryID: repository.ID(id), repositoryPath: filepath.Clean(directory),
	}
	if cfg.databaseURL == "" || cfg.gitBinary == "" {
		return hookConfig{}, errors.New("hook database and Git configuration are required")
	}
	return cfg, nil
}

func parseUpdates(input io.Reader) ([]branchprotection.RefUpdate, error) {
	scanner := bufio.NewScanner(io.LimitReader(input, 128*1024))
	scanner.Buffer(make([]byte, 1024), 2048)
	updates := []branchprotection.RefUpdate{}
	for scanner.Scan() {
		if len(updates) == maxRefUpdates {
			return nil, errors.New("too many ref updates")
		}
		fields := strings.Fields(scanner.Text())
		if len(fields) != 3 {
			return nil, fmt.Errorf("invalid pre-receive tuple")
		}
		updates = append(updates, branchprotection.RefUpdate{OldSHA: fields[0], NewSHA: fields[1], Ref: fields[2]})
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	if len(updates) == 0 {
		return nil, errors.New("no ref updates")
	}
	return updates, nil
}
