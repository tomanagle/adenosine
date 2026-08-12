package git

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/adenosine-dev/adenosine/internal/repository"
)

func TestCommitsAndCommitDetail(t *testing.T) {
	t.Parallel()
	testCases := []struct {
		name string
	}{{
		name: "commits and commit detail",
	}}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			fixture := newHistoryFixture(t)
			ctx := context.Background()

			commits, err := fixture.service.Commits(ctx, fixture.id, "main", 2)
			if err != nil {
				t.Fatalf("list commits: %v", err)
			}
			if len(commits) != 2 || commits[0].SHA != fixture.mergeSHA || commits[1].SHA != fixture.secondSHA {
				t.Fatalf("commits = %+v, want merge then second", commits)
			}
			if len(commits[0].Parents) != 2 || commits[0].Summary != "merge feature" {
				t.Errorf("merge summary = %+v", commits[0])
			}
			if commits, err := fixture.service.Commits(ctx, fixture.id, "main", 1); err != nil || len(commits) != 1 {
				t.Errorf("limit one commits = %+v, error = %v", commits, err)
			}
			if commits, err := fixture.service.Commits(ctx, fixture.id, "main", 100); err != nil || len(commits) != 4 {
				t.Errorf("limit 100 commits = %+v, error = %v", commits, err)
			}

			commit, err := fixture.service.Commit(ctx, fixture.id, fixture.initialSHA[:12])
			if err != nil {
				t.Fatalf("read commit: %v", err)
			}
			if commit.SHA != fixture.initialSHA || len(commit.Parents) != 0 {
				t.Errorf("commit identity = %+v", commit)
			}
			if commit.Author.Name != "Alice Author" || commit.Author.Email != "alice@example.com" {
				t.Errorf("author = %+v", commit.Author)
			}
			if commit.Committer.Name != "Casey Committer" || commit.Committer.Email != "casey@example.com" {
				t.Errorf("committer = %+v", commit.Committer)
			}
			wantAuthorTime := time.Date(2024, 1, 2, 3, 4, 5, 0, time.FixedZone("", 2*60*60))
			wantCommitterTime := time.Date(2024, 1, 3, 4, 5, 6, 0, time.FixedZone("", -7*60*60))
			if !commit.Author.Time.Equal(wantAuthorTime) || commit.Author.Time.Format("-07:00") != "+02:00" {
				t.Errorf("author time = %s", commit.Author.Time)
			}
			if !commit.Committer.Time.Equal(wantCommitterTime) || commit.Committer.Time.Format("-07:00") != "-07:00" {
				t.Errorf("committer time = %s", commit.Committer.Time)
			}
			if commit.Summary != "Initial café" || commit.Message != fixture.initialMessage {
				t.Errorf("message = summary %q, full %q", commit.Summary, commit.Message)
			}
		})
	}
}

func TestDiffFilesPatchAndDisabledHelpers(t *testing.T) {
	t.Parallel()
	testCases := []struct{ name string }{{name: "diff files patch and disabled helpers"}}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			fixture := newHistoryFixture(t)
			marker := filepath.Join(fixture.root, "helper-ran")
			helper := filepath.Join(fixture.root, "diff-helper.sh")
			if err := os.WriteFile(helper, []byte("#!/bin/sh\ntouch \""+marker+"\"\nexit 9\n"), 0o700); err != nil {
				t.Fatalf("write diff helper: %v", err)
			}
			fixture.git(t, "config", "diff.unsafe.command", helper)
			fixture.git(t, "config", "diff.unsafe.textconv", helper)
			if err := os.WriteFile(filepath.Join(fixture.worktree, ".git", "info", "attributes"), []byte("*.txt diff=unsafe\n"), 0o600); err != nil {
				t.Fatalf("write attributes: %v", err)
			}

			diff, err := fixture.service.Diff(context.Background(), fixture.id, fixture.initialSHA, fixture.secondSHA)
			if err != nil {
				t.Fatalf("diff commits: %v", err)
			}
			if diff.BaseSHA != fixture.initialSHA || diff.HeadSHA != fixture.secondSHA {
				t.Errorf("diff endpoints = %s..%s", diff.BaseSHA, diff.HeadSHA)
			}
			byStatusAndPath := make(map[string]DiffFile, len(diff.Files))
			for _, file := range diff.Files {
				byStatusAndPath[file.Status+":"+file.OldPath+":"+file.NewPath] = file
			}
			assertCounts(t, byStatusAndPath["A::added.txt"], 1, 0)
			assertCounts(t, byStatusAndPath["D:deleted.txt:"], 0, 1)
			assertCounts(t, byStatusAndPath["M:edited.txt:edited.txt"], 1, 1)
			assertCounts(t, byStatusAndPath["R:old-name.txt:new-name.txt"], 0, 0)
			binary := byStatusAndPath["M:image.bin:image.bin"]
			if binary.Additions != nil || binary.Deletions != nil {
				t.Errorf("binary counts = %+v, want nil", binary)
			}
			if len(diff.Files) != 5 {
				t.Errorf("files = %+v, want five", diff.Files)
			}
			if !strings.Contains(diff.Patch, "diff --git a/edited.txt b/edited.txt") ||
				!strings.Contains(diff.Patch, "rename from old-name.txt") ||
				!strings.Contains(diff.Patch, "Binary files a/image.bin and b/image.bin differ") {
				t.Errorf("patch lacks expected sections:\n%s", diff.Patch)
			}
			if _, err := os.Stat(marker); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("repository-controlled diff helper executed: %v", err)
			}
		})
	}
}

func TestMergeBaseForMergeDivergenceAndUnrelatedHistory(t *testing.T) {
	t.Parallel()
	testCases := []struct{ name string }{{name: "merge divergence and unrelated history"}}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			fixture := newHistoryFixture(t)
			ctx := context.Background()

			base, err := fixture.service.MergeBase(ctx, fixture.id, fixture.secondSHA, fixture.featureSHA)
			if err != nil {
				t.Fatalf("merge base: %v", err)
			}
			if base != fixture.initialSHA {
				t.Errorf("merge base = %s, want %s", base, fixture.initialSHA)
			}
			base, err = fixture.service.MergeBase(ctx, fixture.id, "main", "feature")
			if err != nil {
				t.Fatalf("merged branch merge base: %v", err)
			}
			if base != fixture.featureSHA {
				t.Errorf("merged branch base = %s, want %s", base, fixture.featureSHA)
			}
			if _, err := fixture.service.MergeBase(ctx, fixture.id, "main", "unrelated"); !errors.Is(err, ErrObjectNotFound) {
				t.Errorf("unrelated merge base error = %v, want ErrObjectNotFound", err)
			}
		})
	}
}

func TestHistoryInputAndObjectErrors(t *testing.T) {
	t.Parallel()
	testCases := []struct{ name string }{{name: "input and object errors"}}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			fixture := newHistoryFixture(t)
			ctx := context.Background()
			invalid := []string{"", "-main", "main\nother", "main\x00other", string([]byte{0xff})}
			for _, revision := range invalid {
				if _, err := fixture.service.Commits(ctx, fixture.id, revision, 1); !errors.Is(err, ErrInvalidInput) {
					t.Errorf("Commits(%q) error = %v", revision, err)
				}
				if _, err := fixture.service.Commit(ctx, fixture.id, revision); !errors.Is(err, ErrInvalidInput) {
					t.Errorf("Commit(%q) error = %v", revision, err)
				}
				if _, err := fixture.service.Diff(ctx, fixture.id, revision, "main"); !errors.Is(err, ErrInvalidInput) {
					t.Errorf("Diff(%q) error = %v", revision, err)
				}
				if _, err := fixture.service.MergeBase(ctx, fixture.id, "main", revision); !errors.Is(err, ErrInvalidInput) {
					t.Errorf("MergeBase(%q) error = %v", revision, err)
				}
			}
			for _, limit := range []int{-1, 0, 101} {
				if _, err := fixture.service.Commits(ctx, fixture.id, "main", limit); !errors.Is(err, ErrInvalidInput) {
					t.Errorf("Commits limit %d error = %v", limit, err)
				}
			}
			if _, err := fixture.service.Commit(ctx, fixture.id, "missing"); !errors.Is(err, ErrObjectNotFound) {
				t.Errorf("missing commit error = %v", err)
			}
			if _, err := fixture.service.Diff(ctx, fixture.id, "main", "missing"); !errors.Is(err, ErrObjectNotFound) {
				t.Errorf("missing diff head error = %v", err)
			}
			if _, err := fixture.service.MergeBase(ctx, fixture.id, "missing", "main"); !errors.Is(err, ErrObjectNotFound) {
				t.Errorf("missing merge-base revision error = %v", err)
			}
			blobSHA := fixture.git(t, "rev-parse", fixture.initialSHA+":edited.txt")
			fixture.git(t, "tag", "blob-object", blobSHA)
			if _, err := fixture.service.Commit(ctx, fixture.id, "blob-object"); !errors.Is(err, ErrUnsupportedObject) {
				t.Errorf("blob commit error = %v", err)
			}
			if _, err := fixture.service.Commits(ctx, fixture.id, "blob-object", 1); !errors.Is(err, ErrUnsupportedObject) {
				t.Errorf("blob history error = %v", err)
			}
			canceled, cancel := context.WithCancel(ctx)
			cancel()
			if _, err := fixture.service.Commit(canceled, fixture.id, "main"); !errors.Is(err, context.Canceled) {
				t.Errorf("canceled commit error = %v, want context.Canceled", err)
			}
		})
	}
}

func TestCommitRejectsInvalidUTF8Message(t *testing.T) {
	t.Parallel()
	testCases := []struct{ name string }{{name: "invalid UTF-8 message"}}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			fixture := newHistoryFixture(t)
			tree := fixture.git(t, "rev-parse", fixture.initialSHA+"^{tree}")
			raw := []byte("tree " + tree + "\nauthor Test Author <author@example.com> 1700000000 +0000\ncommitter Test Committer <committer@example.com> 1700000000 +0000\n\nbad \xff message\n")
			invalidSHA := fixture.gitInput(t, raw, nil, "hash-object", "-t", "commit", "-w", "--stdin")
			fixture.git(t, "update-ref", "refs/heads/invalid-message", invalidSHA)

			if _, err := fixture.service.Commit(context.Background(), fixture.id, "invalid-message"); !errors.Is(err, ErrInvalidInput) {
				t.Fatalf("invalid UTF-8 commit error = %v, want ErrInvalidInput", err)
			}
		})
	}
}

func TestDiffParsersEnforceFileLimitAndBinaryCounts(t *testing.T) {
	t.Parallel()
	testCases := []struct{ name string }{{name: "file limit and binary counts"}}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			var status bytes.Buffer
			for index := 0; index <= maxDiffFiles; index++ {
				fmt.Fprintf(&status, "A\x00file-%05d\x00", index)
			}
			if _, err := parseDiffStatus(status.Bytes()); !errors.Is(err, ErrOutputLimit) {
				t.Fatalf("status file limit error = %v, want ErrOutputLimit", err)
			}
			statistics, err := parseDiffNumstat([]byte("-\t-\timage.bin\x00"))
			if err != nil {
				t.Fatalf("parse binary numstat: %v", err)
			}
			counts := statistics[diffPathKey("image.bin", "image.bin")]
			if counts.additions != nil || counts.deletions != nil {
				t.Fatalf("binary counts = %+v, want nil", counts)
			}
		})
	}
}

func assertCounts(t *testing.T, file DiffFile, additions, deletions int) {
	t.Helper()
	if file.Additions == nil || file.Deletions == nil || *file.Additions != additions || *file.Deletions != deletions {
		t.Errorf("counts for %+v, want +%d -%d", file, additions, deletions)
	}
}

type historyFixture struct {
	t              *testing.T
	root           string
	worktree       string
	binary         string
	service        *Service
	id             repository.ID
	initialSHA     string
	secondSHA      string
	featureSHA     string
	mergeSHA       string
	initialMessage string
}

type fixedRepositoryPath struct {
	path string
}

func (paths fixedRepositoryPath) Prepare(context.Context, repository.ID) (string, error) {
	return paths.path, nil
}

func (paths fixedRepositoryPath) Path(context.Context, repository.ID) (string, error) {
	return paths.path, nil
}

func newHistoryFixture(t *testing.T) *historyFixture {
	t.Helper()
	binary, err := exec.LookPath("git")
	if err != nil {
		t.Skip("git executable is unavailable")
	}
	root := t.TempDir()
	worktree := filepath.Join(root, "repository")
	command := exec.Command(binary, "init", "--initial-branch=main", worktree)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("initialize repository: %v: %s", err, output)
	}
	fixture := &historyFixture{
		t:              t,
		root:           root,
		worktree:       worktree,
		binary:         binary,
		id:             repository.ID{},
		initialMessage: "Initial café\n\nA multiline body.\nSecond body line.\n",
	}
	fixture.service = NewService(NewRunner(binary), fixedRepositoryPath{path: filepath.Join(worktree, ".git")})
	writeHistoryFile(t, worktree, "edited.txt", []byte("before\n"))
	writeHistoryFile(t, worktree, "deleted.txt", []byte("delete me\n"))
	writeHistoryFile(t, worktree, "old-name.txt", []byte("rename me\n"))
	writeHistoryFile(t, worktree, "image.bin", []byte{0, 1, 2, 3})
	fixture.git(t, "add", "--", ".")
	initialEnv := []string{
		"GIT_AUTHOR_NAME=Alice Author",
		"GIT_AUTHOR_EMAIL=alice@example.com",
		"GIT_AUTHOR_DATE=2024-01-02T03:04:05+02:00",
		"GIT_COMMITTER_NAME=Casey Committer",
		"GIT_COMMITTER_EMAIL=casey@example.com",
		"GIT_COMMITTER_DATE=2024-01-03T04:05:06-07:00",
	}
	fixture.gitInput(t, []byte(fixture.initialMessage), initialEnv, "commit", "-F", "-")
	fixture.initialSHA = fixture.git(t, "rev-parse", "HEAD")
	fixture.git(t, "branch", "feature", fixture.initialSHA)

	writeHistoryFile(t, worktree, "edited.txt", []byte("after\n"))
	writeHistoryFile(t, worktree, "added.txt", []byte("added\n"))
	writeHistoryFile(t, worktree, "image.bin", []byte{0, 9, 8, 7})
	if err := os.Remove(filepath.Join(worktree, "deleted.txt")); err != nil {
		t.Fatalf("delete file: %v", err)
	}
	fixture.git(t, "mv", "--", "old-name.txt", "new-name.txt")
	fixture.git(t, "add", "--", ".")
	fixture.gitInput(t, nil, testIdentityEnv(), "commit", "-m", "second")
	fixture.secondSHA = fixture.git(t, "rev-parse", "HEAD")

	fixture.git(t, "checkout", "feature")
	writeHistoryFile(t, worktree, "feature.txt", []byte("feature\n"))
	fixture.git(t, "add", "--", "feature.txt")
	fixture.gitInput(t, nil, testIdentityEnv(), "commit", "-m", "feature")
	fixture.featureSHA = fixture.git(t, "rev-parse", "HEAD")
	fixture.git(t, "checkout", "main")
	fixture.gitInput(t, nil, testIdentityEnv(), "merge", "--no-ff", "-m", "merge feature", "feature")
	fixture.mergeSHA = fixture.git(t, "rev-parse", "HEAD")

	fixture.git(t, "checkout", "--orphan", "unrelated")
	fixture.git(t, "rm", "-rf", "--ignore-unmatch", "--", ".")
	writeHistoryFile(t, worktree, "unrelated.txt", []byte("root\n"))
	fixture.git(t, "add", "--", "unrelated.txt")
	fixture.gitInput(t, nil, testIdentityEnv(), "commit", "-m", "unrelated root")
	fixture.git(t, "checkout", "main")
	return fixture
}

func (fixture *historyFixture) git(t *testing.T, arguments ...string) string {
	t.Helper()
	return fixture.gitInput(t, nil, nil, arguments...)
}

func (fixture *historyFixture) gitInput(t *testing.T, input []byte, environment []string, arguments ...string) string {
	t.Helper()
	command := exec.Command(fixture.binary, append([]string{"-c", "commit.gpgSign=false", "-c", "tag.gpgSign=false", "-C", fixture.worktree}, arguments...)...)
	command.Stdin = bytes.NewReader(input)
	command.Env = append(command.Environ(), environment...)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v: %s", arguments, err, output)
	}
	lines := strings.Split(strings.TrimSpace(string(output)), "\n")
	return lines[len(lines)-1]
}

func testIdentityEnv() []string {
	return []string{
		"GIT_AUTHOR_NAME=Adenosine Test",
		"GIT_AUTHOR_EMAIL=test@example.com",
		"GIT_AUTHOR_DATE=2024-02-01T12:00:00Z",
		"GIT_COMMITTER_NAME=Adenosine Test",
		"GIT_COMMITTER_EMAIL=test@example.com",
		"GIT_COMMITTER_DATE=2024-02-01T12:00:00Z",
	}
}

func writeHistoryFile(t *testing.T, root, name string, content []byte) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(root, name), content, 0o600); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
}
