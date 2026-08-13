package git

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/adenosine-dev/adenosine/internal/branchprotection"
	"github.com/adenosine-dev/adenosine/internal/repository"
)

const (
	maxMergeMessageLength = 1024 * 1024
	maxIdentityLength     = 1024
)

var (
	// ErrMergeInput indicates malformed or unsafe merge input.
	ErrMergeInput = errors.New("invalid Git merge input")
	// ErrMergeConflict indicates content conflicts or unrelated histories.
	ErrMergeConflict = errors.New("Git merge conflict")
	// ErrMergeRefConflict indicates that the target ref does not match the expected SHA.
	ErrMergeRefConflict = errors.New("Git merge target ref changed")
	// ErrMergeRefRejected indicates that branch policy rejected the exact proposed ref update.
	ErrMergeRefRejected = errors.New("Git merge rejected by branch protection")
)

// MergeStrategy determines the parent shape of the commit produced by Merge.
type MergeStrategy string

const (
	MergeCommit MergeStrategy = "merge-commit"
	MergeSquash MergeStrategy = "squash"
)

// MergeIdentity is one deterministic Git commit identity supplied by the caller.
type MergeIdentity struct {
	Name  string
	Email string
	Time  time.Time
}

// MergeRequest declares immutable merge inputs and the target ref compare-and-swap value.
type MergeRequest struct {
	TargetBranch      string
	ExpectedTargetSHA string
	HeadSHA           string
	Strategy          MergeStrategy
	Message           string
	Author            MergeIdentity
	Committer         MergeIdentity
}

// MergeResult describes the commit and ref update produced by Merge.
type MergeResult struct {
	OldSHA    string
	NewSHA    string
	HeadSHA   string
	TreeSHA   string
	TargetRef string
	Strategy  MergeStrategy
}

// Merge computes a native Git merge commit and atomically advances one target branch.
func (service *Service) Merge(ctx context.Context, id repository.ID, request MergeRequest) (MergeResult, error) {
	if err := validateMergeRequest(request); err != nil {
		return MergeResult{}, err
	}
	repositoryPath, err := service.repositoryPath(ctx, id)
	if err != nil {
		return MergeResult{}, err
	}
	objectFormat, shaLength, err := service.objectFormat(ctx, repositoryPath)
	if err != nil {
		return MergeResult{}, err
	}
	if err := validateMergeSHA(request.ExpectedTargetSHA, shaLength); err != nil {
		return MergeResult{}, err
	}
	if err := validateMergeSHA(request.HeadSHA, shaLength); err != nil {
		return MergeResult{}, err
	}

	targetRef := "refs/heads/" + request.TargetBranch
	targetSHA, err := service.resolveMergeCommit(ctx, repositoryPath, targetRef)
	if err != nil {
		return MergeResult{}, fmt.Errorf("resolve merge target: %w", err)
	}
	if targetSHA != request.ExpectedTargetSHA {
		return MergeResult{}, ErrMergeRefConflict
	}
	headSHA, err := service.resolveMergeCommit(ctx, repositoryPath, request.HeadSHA)
	if err != nil {
		return MergeResult{}, fmt.Errorf("resolve merge head: %w", err)
	}
	if headSHA != request.HeadSHA {
		return MergeResult{}, fmt.Errorf("merge head did not resolve exactly: %w", ErrMergeInput)
	}

	isolatedGitDir, cleanup, err := isolatedBareRepository(repositoryPath, objectFormat)
	if err != nil {
		return MergeResult{}, fmt.Errorf("prepare isolated merge repository: %w", err)
	}
	defer cleanup()
	objectEnvironment := []string{"GIT_OBJECT_DIRECTORY=" + filepath.Join(repositoryPath, "objects")}
	mergeBaseOutput := &limitedBuffer{limit: maxMetadataOutput}
	if err := service.runner.runWithEnv(ctx, hardenedMergeArgs(isolatedGitDir, "merge-base", targetSHA, headSHA), nil, mergeBaseOutput, objectEnvironment); err != nil {
		if ctx.Err() != nil {
			return MergeResult{}, ctx.Err()
		}
		if commandExitCode(err) == 1 {
			return MergeResult{}, ErrMergeConflict
		}
		return MergeResult{}, fmt.Errorf("find merge base: %w", err)
	}
	if mergeBaseOutput.truncated {
		return MergeResult{}, ErrOutputLimit
	}
	mergeBaseSHA, err := singleLine(mergeBaseOutput.buffer.Bytes())
	if err != nil || validateMergeSHA(mergeBaseSHA, shaLength) != nil {
		return MergeResult{}, fmt.Errorf("parse merge base: %w", ErrInvalidInput)
	}

	mergeOutput := &limitedBuffer{limit: maxMetadataOutput}
	mergeArgs := hardenedMergeArgs(isolatedGitDir, "merge-tree", "--write-tree", "--no-messages", targetSHA, headSHA)
	if err := service.runner.runWithEnv(ctx, mergeArgs, nil, mergeOutput, objectEnvironment); err != nil {
		if ctx.Err() != nil {
			return MergeResult{}, ctx.Err()
		}
		if commandExitCode(err) == 1 {
			return MergeResult{}, ErrMergeConflict
		}
		return MergeResult{}, fmt.Errorf("compute merge tree: %w", err)
	}
	if mergeOutput.truncated {
		return MergeResult{}, ErrOutputLimit
	}
	treeSHA, err := singleLine(mergeOutput.buffer.Bytes())
	if err != nil || validateMergeSHA(treeSHA, shaLength) != nil {
		return MergeResult{}, fmt.Errorf("parse merge tree: %w", ErrInvalidInput)
	}

	parents := []string{"-p", targetSHA}
	if request.Strategy == MergeCommit {
		parents = append(parents, "-p", headSHA)
	}
	commitArgs := append(hardenedMergeArgs(isolatedGitDir, "commit-tree", treeSHA), parents...)
	commitArgs = append(commitArgs, "-F", "-")
	commitOutput := &limitedBuffer{limit: maxMetadataOutput}
	commitEnvironment := append(objectEnvironment, mergeIdentityEnvironment(request)...)
	if err := service.runner.runWithEnv(ctx, commitArgs, strings.NewReader(request.Message), commitOutput, commitEnvironment); err != nil {
		return MergeResult{}, fmt.Errorf("create merge commit: %w", err)
	}
	if commitOutput.truncated {
		return MergeResult{}, ErrOutputLimit
	}
	newSHA, err := singleLine(commitOutput.buffer.Bytes())
	if err != nil || validateMergeSHA(newSHA, shaLength) != nil {
		return MergeResult{}, fmt.Errorf("parse merge commit: %w", ErrInvalidInput)
	}
	if service.refAuthorizer != nil {
		if err := service.refAuthorizer.Authorize(ctx, id, []branchprotection.RefUpdate{{
			OldSHA: targetSHA, NewSHA: newSHA, Ref: targetRef, EvidenceSHA: headSHA,
		}}); err != nil {
			return MergeResult{}, fmt.Errorf("%w: %v", ErrMergeRefRejected, err)
		}
	}

	if service.beforeMergeCAS != nil {
		service.beforeMergeCAS()
	}
	if err := service.runner.run(ctx, []string{"--git-dir=" + repositoryPath, "update-ref", "--no-deref", targetRef, newSHA, targetSHA}, nil, nil); err != nil {
		if ctx.Err() != nil {
			return MergeResult{}, ctx.Err()
		}
		return MergeResult{}, fmt.Errorf("advance merge target: %w: %w", ErrMergeRefConflict, err)
	}
	return MergeResult{
		OldSHA: targetSHA, NewSHA: newSHA, HeadSHA: headSHA, TreeSHA: treeSHA,
		TargetRef: targetRef, Strategy: request.Strategy,
	}, nil
}

func validateMergeRequest(request MergeRequest) error {
	if err := validateBranch(request.TargetBranch); err != nil || !utf8.ValidString(request.TargetBranch) {
		return fmt.Errorf("invalid target branch: %w", ErrMergeInput)
	}
	if request.Strategy != MergeCommit && request.Strategy != MergeSquash {
		return fmt.Errorf("unsupported merge strategy: %w", ErrMergeInput)
	}
	if request.Message == "" || len(request.Message) > maxMergeMessageLength || !utf8.ValidString(request.Message) || strings.ContainsRune(request.Message, 0) {
		return fmt.Errorf("invalid merge message: %w", ErrMergeInput)
	}
	if err := validateMergeIdentity(request.Author); err != nil {
		return fmt.Errorf("invalid author: %w", err)
	}
	if err := validateMergeIdentity(request.Committer); err != nil {
		return fmt.Errorf("invalid committer: %w", err)
	}
	return nil
}

func validateMergeIdentity(identity MergeIdentity) error {
	if identity.Name == "" || identity.Email == "" || len(identity.Name) > maxIdentityLength || len(identity.Email) > maxIdentityLength || !utf8.ValidString(identity.Name) || !utf8.ValidString(identity.Email) {
		return ErrMergeInput
	}
	if strings.TrimSpace(identity.Name) != identity.Name || strings.TrimSpace(identity.Email) != identity.Email || strings.ContainsAny(identity.Name, "<>") || strings.ContainsAny(identity.Email, "<>") {
		return ErrMergeInput
	}
	for _, value := range identity.Name + identity.Email {
		if value < 0x20 || value == 0x7f {
			return ErrMergeInput
		}
	}
	if identity.Time.IsZero() || identity.Time.Year() < 1 || identity.Time.Year() > 9999 {
		return ErrMergeInput
	}
	_, offset := identity.Time.Zone()
	if offset < -14*60*60 || offset > 14*60*60 || offset%60 != 0 {
		return ErrMergeInput
	}
	return nil
}

func validateMergeSHA(sha string, expectedLength int) error {
	if len(sha) != expectedLength {
		return fmt.Errorf("SHA length does not match repository object format: %w", ErrMergeInput)
	}
	for _, character := range sha {
		if !((character >= '0' && character <= '9') || (character >= 'a' && character <= 'f')) {
			return fmt.Errorf("SHA must be lowercase hexadecimal: %w", ErrMergeInput)
		}
	}
	return nil
}

func (service *Service) objectFormat(ctx context.Context, repositoryPath string) (string, int, error) {
	output, err := service.runBounded(ctx, maxMetadataOutput, []string{"--git-dir=" + repositoryPath, "rev-parse", "--show-object-format"})
	if err != nil {
		return "", 0, fmt.Errorf("determine merge object format: %w", err)
	}
	format, err := singleLine(output)
	if err != nil {
		return "", 0, fmt.Errorf("parse merge object format: %w", err)
	}
	switch format {
	case "sha1":
		return format, 40, nil
	case "sha256":
		return format, 64, nil
	default:
		return "", 0, fmt.Errorf("unsupported object format %q: %w", format, ErrMergeInput)
	}
}

func (service *Service) resolveMergeCommit(ctx context.Context, repositoryPath, revision string) (string, error) {
	sha, err := service.resolve(ctx, repositoryPath, revision+"^{commit}")
	if err == nil {
		return sha, nil
	}
	if ctx.Err() != nil || !errors.Is(err, ErrObjectNotFound) {
		return "", err
	}
	if _, objectErr := service.resolve(ctx, repositoryPath, revision+"^{object}"); objectErr == nil {
		return "", fmt.Errorf("object is not a commit: %w", ErrMergeInput)
	}
	return "", ErrObjectNotFound
}

func isolatedBareRepository(repositoryPath, objectFormat string) (string, func(), error) {
	directory, err := os.MkdirTemp("", "adenosine-merge-*")
	if err != nil {
		return "", func() {}, err
	}
	cleanup := func() { _ = os.RemoveAll(directory) }
	if err := os.Mkdir(filepath.Join(directory, "objects"), 0o700); err != nil {
		cleanup()
		return "", func() {}, err
	}
	if err := os.Mkdir(filepath.Join(directory, "refs"), 0o700); err != nil {
		cleanup()
		return "", func() {}, err
	}
	config := "[core]\n\trepositoryformatversion = 0\n\tbare = true\n"
	if objectFormat == "sha256" {
		config = "[core]\n\trepositoryformatversion = 1\n\tbare = true\n[extensions]\n\tobjectformat = sha256\n"
	}
	if err := os.WriteFile(filepath.Join(directory, "config"), []byte(config), 0o600); err != nil {
		cleanup()
		return "", func() {}, err
	}
	if err := os.WriteFile(filepath.Join(directory, "HEAD"), []byte("ref: refs/heads/isolated\n"), 0o600); err != nil {
		cleanup()
		return "", func() {}, err
	}
	return directory, cleanup, nil
}

func hardenedMergeArgs(gitDir string, commandAndArgs ...string) []string {
	args := []string{
		"--git-dir=" + gitDir,
		"-c", "core.hooksPath=/dev/null",
		"-c", "core.attributesFile=/dev/null",
		"-c", "core.fsmonitor=false",
		"-c", "commit.gpgSign=false",
		"-c", "diff.external=",
		"-c", "diff.trustExitCode=false",
		"-c", "merge.renormalize=false",
		"-c", "merge.renames=false",
		"-c", "user.useConfigOnly=true",
	}
	return append(args, commandAndArgs...)
}

func mergeIdentityEnvironment(request MergeRequest) []string {
	return []string{
		"GIT_AUTHOR_NAME=" + request.Author.Name,
		"GIT_AUTHOR_EMAIL=" + request.Author.Email,
		"GIT_AUTHOR_DATE=" + gitIdentityTime(request.Author.Time),
		"GIT_COMMITTER_NAME=" + request.Committer.Name,
		"GIT_COMMITTER_EMAIL=" + request.Committer.Email,
		"GIT_COMMITTER_DATE=" + gitIdentityTime(request.Committer.Time),
	}
}

func gitIdentityTime(value time.Time) string {
	return strconv.FormatInt(value.Unix(), 10) + " " + value.Format("-0700")
}

func commandExitCode(err error) int {
	var commandError *CommandError
	if errors.As(err, &commandError) {
		return commandError.ExitCode
	}
	return -1
}
