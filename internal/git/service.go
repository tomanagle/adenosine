package git

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net"
	"strings"

	"github.com/adenosine-dev/adenosine/internal/repository"
)

type repositoryPaths interface {
	Prepare(context.Context, repository.ID) (string, error)
	Path(context.Context, repository.ID) (string, error)
}

type repositoryLifecyclePaths interface {
	Quarantine(context.Context, repository.ID) error
	Restore(context.Context, repository.ID) error
	Purge(context.Context, repository.ID) error
}

// Service performs repository operations using native Git.
type Service struct {
	runner         *Runner
	paths          repositoryPaths
	resolver       Resolver
	allowIP        func(net.IP) bool
	beforeMergeCAS func()
}

// NewService constructs the native Git service.
func NewService(runner *Runner, paths repositoryPaths) *Service {
	return &Service{runner: runner, paths: paths, resolver: net.DefaultResolver, allowIP: isPublicIP}
}

// NewServiceWithResolver constructs a service with an alternate DNS boundary.
// allowIP is intended for isolated tests; nil always uses the secure production policy.
func NewServiceWithResolver(runner *Runner, paths repositoryPaths, resolver Resolver, allowIP func(net.IP) bool) *Service {
	if resolver == nil {
		resolver = net.DefaultResolver
	}
	if allowIP == nil {
		allowIP = isPublicIP
	}
	return &Service{runner: runner, paths: paths, resolver: resolver, allowIP: allowIP}
}

// Init creates a bare repository with the requested initial branch.
func (service *Service) Init(ctx context.Context, id repository.ID, defaultBranch string) error {
	if err := service.runner.run(ctx, []string{"check-ref-format", "--branch", defaultBranch}, nil, nil); err != nil {
		return fmt.Errorf("validate default branch: %w", err)
	}
	path, err := service.paths.Prepare(ctx, id)
	if err != nil {
		return fmt.Errorf("prepare repository path: %w", err)
	}
	if err := service.runner.run(ctx, []string{"init", "--bare", "--initial-branch=" + defaultBranch, path}, nil, nil); err != nil {
		return fmt.Errorf("initialize repository: %w", err)
	}
	return nil
}

// SetDefaultBranch validates an existing branch (or an unborn branch in an empty repository) and updates HEAD.
func (service *Service) SetDefaultBranch(ctx context.Context, id repository.ID, branch string) error {
	if err := service.runner.run(ctx, []string{"check-ref-format", "--branch", branch}, nil, nil); err != nil {
		return fmt.Errorf("validate default branch: %w", err)
	}
	refs, err := service.Refs(ctx, id)
	if err != nil {
		return err
	}
	hasBranches, found := false, false
	for _, ref := range refs {
		if strings.HasPrefix(ref.Name, "refs/heads/") {
			hasBranches = true
			if ref.Name == "refs/heads/"+branch {
				found = true
			}
		}
	}
	if hasBranches && !found {
		return fmt.Errorf("branch %q does not exist", branch)
	}
	path, err := service.paths.Path(ctx, id)
	if err != nil {
		return fmt.Errorf("resolve repository path: %w", err)
	}
	if err := service.runner.run(ctx, []string{"--git-dir=" + path, "symbolic-ref", "HEAD", "refs/heads/" + branch}, nil, nil); err != nil {
		return fmt.Errorf("update repository HEAD: %w", err)
	}
	return nil
}

// Quarantine removes a repository from live Git routing without deleting it.
func (service *Service) Quarantine(ctx context.Context, id repository.ID) error {
	paths, ok := service.paths.(repositoryLifecyclePaths)
	if !ok {
		return fmt.Errorf("repository storage does not support quarantine")
	}
	return paths.Quarantine(ctx, id)
}

// Restore returns a quarantined repository to live Git routing.
func (service *Service) Restore(ctx context.Context, id repository.ID) error {
	paths, ok := service.paths.(repositoryLifecyclePaths)
	if !ok {
		return fmt.Errorf("repository storage does not support restoration")
	}
	return paths.Restore(ctx, id)
}

// Purge permanently deletes a validated quarantined repository.
func (service *Service) Purge(ctx context.Context, id repository.ID) error {
	paths, ok := service.paths.(repositoryLifecyclePaths)
	if !ok {
		return fmt.Errorf("repository storage does not support purge")
	}
	return paths.Purge(ctx, id)
}

// SetReceiveProtection configures native receive-pack force-push and deletion guards.
func (service *Service) SetReceiveProtection(ctx context.Context, id repository.ID, denyForcePush, denyDeletes bool) error {
	path, err := service.paths.Path(ctx, id)
	if err != nil {
		return fmt.Errorf("resolve repository path: %w", err)
	}
	settings := []struct {
		key   string
		value bool
	}{{"receive.denyNonFastForwards", denyForcePush}, {"receive.denyDeletes", denyDeletes}}
	for _, setting := range settings {
		if err := service.runner.run(ctx, []string{"--git-dir=" + path, "config", "--bool", setting.key, fmt.Sprintf("%t", setting.value)}, nil, nil); err != nil {
			return fmt.Errorf("configure %s: %w", setting.key, err)
		}
	}
	return nil
}

// PackOptions controls stateless Git pack protocol execution.
type PackOptions struct {
	AdvertiseRefs bool
	Protocol      string
}

// UploadPack streams clone/fetch protocol data between a client and Git.
func (service *Service) UploadPack(ctx context.Context, id repository.ID, input io.Reader, output io.Writer, options PackOptions) error {
	return service.runPack(ctx, "upload-pack", id, input, output, options)
}

// ReceivePack streams push protocol data between a client and Git.
func (service *Service) ReceivePack(ctx context.Context, id repository.ID, input io.Reader, output io.Writer, options PackOptions) error {
	return service.runPack(ctx, "receive-pack", id, input, output, options)
}

// UploadPackSession streams the stateful pack protocol used by SSH sessions.
func (service *Service) UploadPackSession(ctx context.Context, id repository.ID, input io.Reader, output io.Writer, protocol string) error {
	return service.runSessionPack(ctx, "upload-pack", id, input, output, protocol)
}

// ReceivePackSession streams the stateful pack protocol used by SSH sessions.
func (service *Service) ReceivePackSession(ctx context.Context, id repository.ID, input io.Reader, output io.Writer, protocol string) error {
	return service.runSessionPack(ctx, "receive-pack", id, input, output, protocol)
}

func (service *Service) runPack(ctx context.Context, operation string, id repository.ID, input io.Reader, output io.Writer, options PackOptions) error {
	path, err := service.paths.Path(ctx, id)
	if err != nil {
		return fmt.Errorf("resolve repository path: %w", err)
	}
	args := packArgs(operation, "--stateless-rpc")
	if options.AdvertiseRefs {
		args = append(args, "--advertise-refs")
	}
	args = append(args, path)
	environment := []string{}
	if options.Protocol != "" {
		if options.Protocol != "version=1" && options.Protocol != "version=2" {
			return fmt.Errorf("unsupported Git protocol %q", options.Protocol)
		}
		environment = append(environment, "GIT_PROTOCOL="+options.Protocol)
	}
	if err := service.runner.runWithEnv(ctx, args, input, output, environment); err != nil {
		return fmt.Errorf("git %s: %w", operation, err)
	}
	return nil
}

func (service *Service) runSessionPack(ctx context.Context, operation string, id repository.ID, input io.Reader, output io.Writer, protocol string) error {
	path, err := service.paths.Path(ctx, id)
	if err != nil {
		return fmt.Errorf("resolve repository path: %w", err)
	}
	environment := []string{}
	if protocol != "" {
		if protocol != "version=1" && protocol != "version=2" {
			return fmt.Errorf("unsupported Git protocol %q", protocol)
		}
		environment = append(environment, "GIT_PROTOCOL="+protocol)
	}
	if err := service.runner.runWithEnv(ctx, append(packArgs(operation), path), input, output, environment); err != nil {
		return fmt.Errorf("git %s session: %w", operation, err)
	}
	return nil
}

func packArgs(operation string, args ...string) []string {
	result := []string{
		"-c", "transfer.hideRefs=refs/adenosine",
		"-c", "uploadpack.hideRefs=refs/adenosine",
		"-c", "receive.hideRefs=refs/adenosine",
		operation,
	}
	return append(result, args...)
}

// Ref is one advertised repository reference.
type Ref struct {
	SHA  string
	Name string
	Type string
}

// Refs lists repository refs using a locale-independent NUL-delimited format.
func (service *Service) Refs(ctx context.Context, id repository.ID) ([]Ref, error) {
	path, err := service.paths.Path(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("resolve repository path: %w", err)
	}
	output, err := service.runBounded(ctx, maxRefOutput, []string{
		"--git-dir=" + path,
		"for-each-ref",
		"--format=%(objectname)%00%(refname)%00%(objecttype)",
	})
	if err != nil {
		return nil, fmt.Errorf("list repository refs: %w", err)
	}
	refs := []Ref{}
	scanner := bufio.NewScanner(strings.NewReader(string(output)))
	for scanner.Scan() {
		fields := strings.Split(scanner.Text(), "\x00")
		if len(fields) != 3 {
			return nil, fmt.Errorf("parse repository refs: unexpected field count %d", len(fields))
		}
		refs = append(refs, Ref{SHA: fields[0], Name: fields[1], Type: fields[2]})
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scan repository refs: %w", err)
	}
	return refs, nil
}
