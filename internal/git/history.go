package git

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/adenosine-dev/adenosine/internal/repository"
)

const (
	maxCommitListOutput = 16 * 1024
	maxCommitOutput     = 4 * 1024 * 1024
	maxDiffOutput       = 8 * 1024 * 1024
	maxDiffFiles        = 10000
)

// CommitIdentity identifies an author or committer at the time of a commit.
type CommitIdentity struct {
	Name  string
	Email string
	Time  time.Time
}

// CommitSummary is the history-list representation of a commit.
type CommitSummary struct {
	SHA       string
	Parents   []string
	Author    CommitIdentity
	Committer CommitIdentity
	Summary   string
}

// Commit is the detailed representation of a commit, including its full message.
type Commit struct {
	SHA       string
	Parents   []string
	Author    CommitIdentity
	Committer CommitIdentity
	Summary   string
	Message   string
}

// DiffFile describes one path-level change. Binary files have nil line counts.
type DiffFile struct {
	Status    string
	OldPath   string
	NewPath   string
	Additions *int
	Deletions *int
}

// Diff contains resolved endpoints, per-file statistics, and a bounded patch.
type Diff struct {
	BaseSHA string
	HeadSHA string
	Files   []DiffFile
	Patch   string
}

// Commits returns up to limit commits reachable from ref, newest first.
func (service *Service) Commits(ctx context.Context, id repository.ID, ref string, limit int) ([]CommitSummary, error) {
	if err := validateRevision(ref); err != nil {
		return nil, err
	}
	if limit < 1 || limit > 100 {
		return nil, fmt.Errorf("commit limit must be between 1 and 100: %w", ErrInvalidInput)
	}
	repositoryPath, err := service.repositoryPath(ctx, id)
	if err != nil {
		return nil, err
	}
	commitSHA, err := service.resolveCommit(ctx, repositoryPath, ref)
	if err != nil {
		return nil, fmt.Errorf("resolve history ref: %w", err)
	}
	output, err := service.runBounded(ctx, maxCommitListOutput, []string{
		"--git-dir=" + repositoryPath,
		"rev-list",
		"--max-count=" + strconv.Itoa(limit),
		"--end-of-options",
		commitSHA,
		"--",
	})
	if err != nil {
		return nil, fmt.Errorf("list commits: %w", err)
	}
	shas, err := parseSHALines(output)
	if err != nil {
		return nil, fmt.Errorf("parse commit list: %w", err)
	}
	commits := make([]CommitSummary, 0, len(shas))
	for _, sha := range shas {
		commit, err := service.readCommit(ctx, repositoryPath, sha)
		if err != nil {
			return nil, fmt.Errorf("read commit %s: %w", sha, err)
		}
		commits = append(commits, CommitSummary{
			SHA:       commit.SHA,
			Parents:   commit.Parents,
			Author:    commit.Author,
			Committer: commit.Committer,
			Summary:   commit.Summary,
		})
	}
	return commits, nil
}

// Commit resolves revision and returns detailed commit metadata and its full message.
func (service *Service) Commit(ctx context.Context, id repository.ID, revision string) (Commit, error) {
	if err := validateRevision(revision); err != nil {
		return Commit{}, err
	}
	repositoryPath, err := service.repositoryPath(ctx, id)
	if err != nil {
		return Commit{}, err
	}
	sha, err := service.resolveCommit(ctx, repositoryPath, revision)
	if err != nil {
		return Commit{}, fmt.Errorf("resolve commit: %w", err)
	}
	commit, err := service.readCommit(ctx, repositoryPath, sha)
	if err != nil {
		return Commit{}, fmt.Errorf("read commit: %w", err)
	}
	return commit, nil
}

// Diff returns path statistics and patch text between two resolved commits.
func (service *Service) Diff(ctx context.Context, id repository.ID, base, head string) (Diff, error) {
	if err := validateRevision(base); err != nil {
		return Diff{}, err
	}
	if err := validateRevision(head); err != nil {
		return Diff{}, err
	}
	repositoryPath, err := service.repositoryPath(ctx, id)
	if err != nil {
		return Diff{}, err
	}
	baseSHA, err := service.resolveCommit(ctx, repositoryPath, base)
	if err != nil {
		return Diff{}, fmt.Errorf("resolve diff base: %w", err)
	}
	headSHA, err := service.resolveCommit(ctx, repositoryPath, head)
	if err != nil {
		return Diff{}, fmt.Errorf("resolve diff head: %w", err)
	}

	diffCommand := []string{
		"--git-dir=" + repositoryPath,
		"-c", "diff.renames=true",
		"-c", "diff.renameLimit=" + strconv.Itoa(maxDiffFiles),
		"diff",
	}
	commonArgs := []string{
		"--no-ext-diff",
		"--no-textconv",
		"--find-renames=50%",
		"--no-color",
		"--diff-algorithm=myers",
		"--no-indent-heuristic",
		"-O" + os.DevNull,
	}
	statusArgs := append(append([]string{}, diffCommand...), commonArgs...)
	statusArgs = append(statusArgs, "--name-status", "-z", baseSHA, headSHA, "--")
	statusOutput, err := service.runBounded(ctx, maxDiffOutput, statusArgs)
	if err != nil {
		return Diff{}, fmt.Errorf("read diff status: %w", err)
	}
	files, err := parseDiffStatus(statusOutput)
	if err != nil {
		return Diff{}, fmt.Errorf("parse diff status: %w", err)
	}

	numstatArgs := append(append([]string{}, diffCommand...), commonArgs...)
	numstatArgs = append(numstatArgs, "--numstat", "-z", baseSHA, headSHA, "--")
	numstatOutput, err := service.runBounded(ctx, maxDiffOutput, numstatArgs)
	if err != nil {
		return Diff{}, fmt.Errorf("read diff statistics: %w", err)
	}
	statistics, err := parseDiffNumstat(numstatOutput)
	if err != nil {
		return Diff{}, fmt.Errorf("parse diff statistics: %w", err)
	}
	for index := range files {
		oldPath, newPath := files[index].OldPath, files[index].NewPath
		if oldPath == "" {
			oldPath = newPath
		}
		if newPath == "" {
			newPath = oldPath
		}
		counts, ok := statistics[diffPathKey(oldPath, newPath)]
		if !ok {
			return Diff{}, fmt.Errorf("missing statistics for %q", files[index].NewPath)
		}
		files[index].Additions = counts.additions
		files[index].Deletions = counts.deletions
	}
	if len(statistics) != len(files) {
		return Diff{}, fmt.Errorf("diff status and statistics file counts differ")
	}

	patchArgs := append(append([]string{}, diffCommand...), commonArgs...)
	patchArgs = append(patchArgs, "--patch", "--unified=3", "--full-index", "--src-prefix=a/", "--dst-prefix=b/", baseSHA, headSHA, "--")
	patchOutput, err := service.runBounded(ctx, maxDiffOutput, patchArgs)
	if err != nil {
		return Diff{}, fmt.Errorf("read diff patch: %w", err)
	}
	if !utf8.Valid(patchOutput) {
		return Diff{}, fmt.Errorf("diff patch is not valid UTF-8: %w", ErrInvalidInput)
	}
	return Diff{BaseSHA: baseSHA, HeadSHA: headSHA, Files: files, Patch: string(patchOutput)}, nil
}

// MergeBase returns a full commit SHA shared by both revisions.
func (service *Service) MergeBase(ctx context.Context, id repository.ID, a, b string) (string, error) {
	if err := validateRevision(a); err != nil {
		return "", err
	}
	if err := validateRevision(b); err != nil {
		return "", err
	}
	repositoryPath, err := service.repositoryPath(ctx, id)
	if err != nil {
		return "", err
	}
	aSHA, err := service.resolveCommit(ctx, repositoryPath, a)
	if err != nil {
		return "", fmt.Errorf("resolve first merge-base revision: %w", err)
	}
	bSHA, err := service.resolveCommit(ctx, repositoryPath, b)
	if err != nil {
		return "", fmt.Errorf("resolve second merge-base revision: %w", err)
	}
	output, err := service.runBounded(ctx, maxMetadataOutput, []string{
		"--git-dir=" + repositoryPath,
		"merge-base",
		"--end-of-options",
		aSHA,
		bSHA,
	})
	if err != nil {
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		if errors.Is(err, ErrOutputLimit) {
			return "", err
		}
		var commandError *CommandError
		if errors.As(err, &commandError) && commandError.ExitCode == 1 {
			return "", ErrObjectNotFound
		}
		return "", fmt.Errorf("calculate merge base: %w", err)
	}
	sha, err := singleLine(output)
	if err != nil {
		return "", fmt.Errorf("parse merge base: %w", err)
	}
	if err := validateFullSHA(sha); err != nil {
		return "", fmt.Errorf("parse merge base: %w", err)
	}
	return sha, nil
}

func (service *Service) resolveCommit(ctx context.Context, repositoryPath, revision string) (string, error) {
	sha, err := service.resolve(ctx, repositoryPath, revision+"^{commit}")
	if err == nil {
		if err := validateFullSHA(sha); err != nil {
			return "", fmt.Errorf("parse resolved commit: %w", err)
		}
		return sha, nil
	}
	if !errors.Is(err, ErrObjectNotFound) {
		return "", err
	}
	objectSHA, objectErr := service.resolve(ctx, repositoryPath, revision+"^{object}")
	if objectErr != nil {
		return "", err
	}
	objectType, typeErr := service.objectProperty(ctx, repositoryPath, "-t", objectSHA)
	if typeErr != nil {
		return "", typeErr
	}
	return "", fmt.Errorf("revision resolves to %s: %w", objectType, ErrUnsupportedObject)
}

func (service *Service) readCommit(ctx context.Context, repositoryPath, sha string) (Commit, error) {
	metadata, err := service.runBounded(ctx, maxMetadataOutput, []string{
		"--git-dir=" + repositoryPath,
		"show",
		"--no-ext-diff",
		"--no-textconv",
		"--no-patch",
		"--no-decorate",
		"--encoding=UTF-8",
		"--format=%H%x00%P%x00%an%x00%ae%x00%aI%x00%cn%x00%ce%x00%cI%x00%s%x00",
		"--end-of-options",
		sha,
		"--",
	})
	if err != nil {
		return Commit{}, err
	}
	fields := bytes.Split(bytes.TrimSuffix(metadata, []byte{'\n'}), []byte{0})
	if len(fields) != 10 || len(fields[9]) != 0 {
		return Commit{}, fmt.Errorf("unexpected commit metadata field count")
	}
	for _, field := range fields[:9] {
		if !utf8.Valid(field) {
			return Commit{}, fmt.Errorf("commit metadata is not valid UTF-8: %w", ErrInvalidInput)
		}
	}
	authorTime, err := time.Parse(time.RFC3339, string(fields[4]))
	if err != nil {
		return Commit{}, fmt.Errorf("parse author date %q: %w", fields[4], err)
	}
	committerTime, err := time.Parse(time.RFC3339, string(fields[7]))
	if err != nil {
		return Commit{}, fmt.Errorf("parse committer date %q: %w", fields[7], err)
	}
	parents := []string{}
	if len(fields[1]) != 0 {
		parents = strings.Fields(string(fields[1]))
		for _, parent := range parents {
			if err := validateFullSHA(parent); err != nil {
				return Commit{}, fmt.Errorf("parse parent SHA: %w", err)
			}
		}
	}
	raw, err := service.runBounded(ctx, maxCommitOutput, []string{
		"--git-dir=" + repositoryPath,
		"cat-file",
		"commit",
		sha,
	})
	if err != nil {
		return Commit{}, err
	}
	if !utf8.Valid(raw) {
		return Commit{}, fmt.Errorf("commit object is not valid UTF-8: %w", ErrInvalidInput)
	}
	separator := bytes.Index(raw, []byte("\n\n"))
	if separator < 0 {
		return Commit{}, fmt.Errorf("commit object has no message separator")
	}
	message := raw[separator+2:]
	if err := validateFullSHA(string(fields[0])); err != nil || string(fields[0]) != sha {
		return Commit{}, fmt.Errorf("unexpected commit SHA %q", fields[0])
	}
	return Commit{
		SHA:     string(fields[0]),
		Parents: parents,
		Author: CommitIdentity{
			Name: string(fields[2]), Email: string(fields[3]), Time: authorTime,
		},
		Committer: CommitIdentity{
			Name: string(fields[5]), Email: string(fields[6]), Time: committerTime,
		},
		Summary: string(fields[8]),
		Message: string(message),
	}, nil
}

type diffCounts struct {
	additions *int
	deletions *int
}

func parseDiffStatus(output []byte) ([]DiffFile, error) {
	if len(output) == 0 {
		return []DiffFile{}, nil
	}
	fields := bytes.Split(output, []byte{0})
	if len(fields[len(fields)-1]) != 0 {
		return nil, fmt.Errorf("unterminated diff status")
	}
	fields = fields[:len(fields)-1]
	files := make([]DiffFile, 0, len(fields)/2)
	for index := 0; index < len(fields); {
		status := fields[index]
		index++
		if len(status) == 0 {
			return nil, fmt.Errorf("empty diff status")
		}
		code := status[0]
		if !bytes.ContainsRune([]byte("AMD RCT"), rune(code)) || code == ' ' {
			return nil, fmt.Errorf("unsupported diff status %q", status)
		}
		pathCount := 1
		if code == 'R' || code == 'C' {
			pathCount = 2
		}
		if index+pathCount > len(fields) {
			return nil, fmt.Errorf("missing path for diff status %q", status)
		}
		for _, path := range fields[index : index+pathCount] {
			if !utf8.Valid(path) {
				return nil, fmt.Errorf("diff path is not valid UTF-8: %w", ErrInvalidInput)
			}
		}
		file := DiffFile{Status: string(code)}
		switch code {
		case 'A':
			file.NewPath = string(fields[index])
		case 'D':
			file.OldPath = string(fields[index])
		case 'R', 'C':
			file.OldPath = string(fields[index])
			file.NewPath = string(fields[index+1])
		default:
			file.OldPath = string(fields[index])
			file.NewPath = string(fields[index])
		}
		files = append(files, file)
		if len(files) > maxDiffFiles {
			return nil, ErrOutputLimit
		}
		index += pathCount
	}
	return files, nil
}

func parseDiffNumstat(output []byte) (map[string]diffCounts, error) {
	statistics := make(map[string]diffCounts)
	if len(output) == 0 {
		return statistics, nil
	}
	fields := bytes.Split(output, []byte{0})
	if len(fields[len(fields)-1]) != 0 {
		return nil, fmt.Errorf("unterminated diff statistics")
	}
	fields = fields[:len(fields)-1]
	for index := 0; index < len(fields); index++ {
		header := bytes.Split(fields[index], []byte{'\t'})
		if len(header) != 3 {
			return nil, fmt.Errorf("unexpected diff statistics field count")
		}
		oldPath, newPath := string(header[2]), string(header[2])
		if len(header[2]) == 0 {
			if index+2 >= len(fields) {
				return nil, fmt.Errorf("missing renamed paths in diff statistics")
			}
			oldPath = string(fields[index+1])
			newPath = string(fields[index+2])
			if !utf8.Valid(fields[index+1]) || !utf8.Valid(fields[index+2]) {
				return nil, fmt.Errorf("diff path is not valid UTF-8: %w", ErrInvalidInput)
			}
			index += 2
		} else if !utf8.Valid(header[2]) {
			return nil, fmt.Errorf("diff path is not valid UTF-8: %w", ErrInvalidInput)
		}
		counts, err := parseDiffCounts(header[0], header[1])
		if err != nil {
			return nil, err
		}
		key := diffPathKey(oldPath, newPath)
		if _, exists := statistics[key]; exists {
			return nil, fmt.Errorf("duplicate diff statistics path")
		}
		statistics[key] = counts
		if len(statistics) > maxDiffFiles {
			return nil, ErrOutputLimit
		}
	}
	return statistics, nil
}

func parseDiffCounts(additionsText, deletionsText []byte) (diffCounts, error) {
	if bytes.Equal(additionsText, []byte{'-'}) && bytes.Equal(deletionsText, []byte{'-'}) {
		return diffCounts{}, nil
	}
	additions, err := strconv.Atoi(string(additionsText))
	if err != nil || additions < 0 {
		return diffCounts{}, fmt.Errorf("invalid addition count %q", additionsText)
	}
	deletions, err := strconv.Atoi(string(deletionsText))
	if err != nil || deletions < 0 {
		return diffCounts{}, fmt.Errorf("invalid deletion count %q", deletionsText)
	}
	return diffCounts{additions: &additions, deletions: &deletions}, nil
}

func diffPathKey(oldPath, newPath string) string {
	return oldPath + "\x00" + newPath
}

func parseSHALines(output []byte) ([]string, error) {
	if len(output) == 0 {
		return []string{}, nil
	}
	lines := bytes.Split(bytes.TrimSuffix(output, []byte{'\n'}), []byte{'\n'})
	shas := make([]string, len(lines))
	for index, line := range lines {
		shas[index] = string(line)
		if err := validateFullSHA(shas[index]); err != nil {
			return nil, err
		}
	}
	return shas, nil
}

func validateFullSHA(sha string) error {
	if len(sha) != 40 && len(sha) != 64 {
		return fmt.Errorf("invalid full object ID")
	}
	for _, character := range sha {
		if (character < '0' || character > '9') && (character < 'a' || character > 'f') {
			return fmt.Errorf("invalid full object ID")
		}
	}
	return nil
}
