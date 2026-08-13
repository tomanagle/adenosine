package git

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/adenosine-dev/adenosine/internal/repository"
)

// ErrForkDiverged means syncing would overwrite commits unique to the fork.
var ErrForkDiverged = errors.New("fork default branch has diverged from upstream")

// Fork initializes a bare repository and imports public branches and tags from its upstream.
func (service *Service) Fork(ctx context.Context, id repository.ID, source repository.ForkSource, defaultBranch string) error {
	if err := service.Init(ctx, id, defaultBranch); err != nil {
		return err
	}
	destination, err := service.repositoryPath(ctx, id)
	if err != nil {
		return err
	}
	args, sourceArgument, err := service.forkFetchArgs(ctx, destination, source)
	if err != nil {
		return err
	}
	args = append(args, "fetch", "--no-write-fetch-head", "--no-recurse-submodules", "--force", sourceArgument,
		"+refs/heads/*:refs/heads/*", "+refs/tags/*:refs/tags/*")
	if err := service.runner.run(ctx, args, nil, nil); err != nil {
		return fmt.Errorf("fetch fork source: %w", err)
	}
	return nil
}

// SyncFork advances the fork's default branch only when the update is a fast-forward.
func (service *Service) SyncFork(ctx context.Context, id repository.ID, source repository.ForkSource, defaultBranch string) (result repository.ForkSync, resultErr error) {
	if err := validateBranch(defaultBranch); err != nil {
		return result, err
	}
	destination, err := service.repositoryPath(ctx, id)
	if err != nil {
		return result, err
	}
	quarantine, err := randomQuarantineRef()
	if err != nil {
		return result, fmt.Errorf("create fork sync ref: %w", err)
	}
	defer func() {
		cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		if cleanupErr := service.runner.run(cleanupCtx, []string{"--git-dir=" + destination, "update-ref", "-d", quarantine}, nil, nil); cleanupErr != nil {
			resultErr = errors.Join(resultErr, fmt.Errorf("delete fork sync ref: %w", cleanupErr))
		}
	}()
	args, sourceArgument, err := service.forkFetchArgs(ctx, destination, source)
	if err != nil {
		return result, err
	}
	args = append(args, "fetch", "--no-tags", "--no-write-fetch-head", "--no-recurse-submodules", "--force", sourceArgument,
		"+refs/heads/"+defaultBranch+":"+quarantine)
	if err := service.runner.run(ctx, args, nil, nil); err != nil {
		return result, fmt.Errorf("fetch fork upstream: %w", err)
	}
	after, err := service.resolve(ctx, destination, quarantine)
	if err != nil {
		return result, fmt.Errorf("resolve fork upstream: %w", err)
	}
	branchRef := "refs/heads/" + defaultBranch
	before, err := service.resolve(ctx, destination, branchRef)
	if errors.Is(err, ErrObjectNotFound) {
		before = ""
	} else if err != nil {
		return result, fmt.Errorf("resolve fork branch: %w", err)
	}
	result = repository.ForkSync{BeforeSHA: before, AfterSHA: after, Updated: before != after}
	if !result.Updated {
		return result, nil
	}
	if before != "" {
		err = service.runner.run(ctx, []string{"--git-dir=" + destination, "merge-base", "--is-ancestor", before, after}, nil, nil)
		if err != nil {
			var commandErr *CommandError
			if errors.As(err, &commandErr) && commandErr.ExitCode == 1 {
				return repository.ForkSync{}, ErrForkDiverged
			}
			return repository.ForkSync{}, fmt.Errorf("verify fork fast-forward: %w", err)
		}
	}
	prior := before
	if prior == "" {
		prior = strings.Repeat("0", len(after))
	}
	if err := service.runner.run(ctx, []string{"--git-dir=" + destination, "update-ref", branchRef, after, prior}, nil, nil); err != nil {
		return repository.ForkSync{}, fmt.Errorf("advance fork branch: %w", err)
	}
	return result, nil
}

func (service *Service) forkFetchArgs(ctx context.Context, destination string, source repository.ForkSource) ([]string, string, error) {
	if source.LocalRepositoryID != nil {
		if *source.LocalRepositoryID == (repository.ID{}) {
			return nil, "", fmt.Errorf("local fork source is invalid: %w", ErrRemoteInput)
		}
		path, err := service.repositoryPath(ctx, *source.LocalRepositoryID)
		if err != nil {
			return nil, "", fmt.Errorf("resolve local fork source: %w", err)
		}
		return []string{"--git-dir=" + destination}, path, nil
	}
	request := RemoteFetch{SourceURL: source.GitHTTPS, SourceBranch: "main", ExpectedHead: strings.Repeat("0", 40), Destination: "fork"}
	endpoint, err := validateRemoteFetch(request)
	if err != nil {
		return nil, "", err
	}
	addresses, err := service.resolveRemote(ctx, endpoint.Hostname())
	if err != nil {
		return nil, "", err
	}
	return hardenedFetchArgs(destination, endpoint, addresses, service.runner.httpCAInfo), source.GitHTTPS, nil
}
