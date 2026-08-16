package git

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/adenosine-dev/adenosine/internal/branchprotection"
	"github.com/adenosine-dev/adenosine/internal/repository"
)

const (
	maxCommitObject        = 1024 * 1024
	maxProtectedCommitList = 32 * 1024 * 1024
)

type refAuthorizer interface {
	Authorize(context.Context, repository.ID, []branchprotection.RefUpdate) error
}

// ConfigureRefAuthorizer applies the same policy evaluator to direct ref mutations.
func (service *Service) ConfigureRefAuthorizer(authorizer refAuthorizer) error {
	if authorizer == nil {
		return errors.New("ref authorizer is required")
	}
	service.refAuthorizer = authorizer
	return nil
}

// IsAncestor reports whether oldSHA is reachable from newSHA.
func (service *Service) IsAncestor(ctx context.Context, id repository.ID, oldSHA, newSHA string) (bool, error) {
	path, err := service.paths.Path(ctx, id)
	if err != nil {
		return false, fmt.Errorf("resolve repository path: %w", err)
	}
	err = service.runner.runWithEnv(ctx, []string{"--git-dir=" + path, "merge-base", "--is-ancestor", oldSHA, newSHA}, nil, nil, service.objectEnv)
	if err == nil {
		return true, nil
	}
	var commandErr *CommandError
	if errors.As(err, &commandErr) && commandErr.ExitCode == 1 {
		return false, nil
	}
	return false, fmt.Errorf("test commit ancestry: %w", err)
}

// NewCommits lists commits newly reachable from the proposed branch tip.
func (service *Service) NewCommits(ctx context.Context, id repository.ID, oldSHA, newSHA string) ([]string, error) {
	if zeroObjectID(newSHA) {
		return []string{}, nil
	}
	path, err := service.paths.Path(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("resolve repository path: %w", err)
	}
	args := []string{"--git-dir=" + path, "rev-list", newSHA}
	if !zeroObjectID(oldSHA) {
		args = append(args, "^"+oldSHA)
	}
	output := &limitedBuffer{limit: maxProtectedCommitList}
	if err := service.runner.runWithEnv(ctx, args, nil, output, service.objectEnv); err != nil {
		return nil, fmt.Errorf("list newly reachable commits: %w", err)
	}
	if output.truncated {
		return nil, ErrOutputLimit
	}
	commits := strings.Fields(output.String())
	for _, commit := range commits {
		if err := validateFullSHA(commit); err != nil {
			return nil, fmt.Errorf("parse newly reachable commit: %w", err)
		}
	}
	return commits, nil
}

// VerifySSHSignatures requires every commit to have an SSH signature trusted by
// the current active Adenosine key set.
func (service *Service) VerifySSHSignatures(ctx context.Context, id repository.ID, commits []string, signers []branchprotection.AllowedSigner) error {
	if len(commits) == 0 {
		return nil
	}
	if len(signers) == 0 {
		return errors.New("no trusted SSH commit signers are configured")
	}
	allowed, err := os.CreateTemp("", "adenosine-allowed-signers-*")
	if err != nil {
		return fmt.Errorf("create allowed signers file: %w", err)
	}
	allowedPath := allowed.Name()
	defer func() { _ = os.Remove(allowedPath) }()
	if err := allowed.Chmod(0o600); err != nil {
		_ = allowed.Close()
		return fmt.Errorf("restrict allowed signers file: %w", err)
	}
	for _, signer := range signers {
		fields := strings.Fields(signer.PublicKey)
		if strings.TrimSpace(signer.Principal) == "" || strings.ContainsAny(signer.Principal, " ,\r\n\x00") || len(fields) < 2 {
			_ = allowed.Close()
			return errors.New("trusted SSH signer metadata is invalid")
		}
		if _, err := fmt.Fprintf(allowed, "%s %s %s\n", signer.Principal, fields[0], fields[1]); err != nil {
			_ = allowed.Close()
			return fmt.Errorf("write allowed signers file: %w", err)
		}
	}
	if err := allowed.Close(); err != nil {
		return fmt.Errorf("close allowed signers file: %w", err)
	}
	path, err := service.paths.Path(ctx, id)
	if err != nil {
		return fmt.Errorf("resolve repository path: %w", err)
	}
	for _, commit := range commits {
		contents := &limitedBuffer{limit: maxCommitObject}
		if err := service.runner.runWithEnv(ctx, []string{"--git-dir=" + path, "cat-file", "commit", commit}, nil, contents, service.objectEnv); err != nil {
			return fmt.Errorf("inspect commit signature: %w", err)
		}
		if contents.truncated || !bytes.Contains(contents.buffer.Bytes(), []byte("-----BEGIN SSH SIGNATURE-----")) {
			return fmt.Errorf("commit %s does not contain an SSH signature", commit)
		}
		if err := service.runner.runWithEnv(ctx, []string{
			"-c", "gpg.format=ssh", "-c", "gpg.ssh.allowedSignersFile=" + allowedPath,
			"--git-dir=" + path, "verify-commit", commit,
		}, nil, nil, service.objectEnv); err != nil {
			return fmt.Errorf("verify commit %s SSH signature: %w", commit, err)
		}
	}
	return nil
}

func zeroObjectID(value string) bool { return value != "" && strings.Trim(value, "0") == "" }
