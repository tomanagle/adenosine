package git

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/adenosine-dev/adenosine/internal/repository"
)

func TestMergeStrategies(t *testing.T) {
	testCases := []struct {
		name       string
		strategy   MergeStrategy
		parentHead bool
	}{
		{name: "merge commit", strategy: MergeCommit, parentHead: true},
		{name: "squash", strategy: MergeSquash, parentHead: false},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			fixture := newMergeFixture(t, "sha1", false)
			request := fixture.request(testCase.strategy)
			result, err := fixture.service.Merge(context.Background(), fixture.id, request)
			if err != nil {
				t.Fatalf("Merge() error = %v", err)
			}
			wantParents := request.ExpectedTargetSHA
			if testCase.parentHead {
				wantParents += " " + request.HeadSHA
			}
			if parents := fixture.git("show", "-s", "--format=%P", result.NewSHA); parents != wantParents {
				t.Errorf("parents = %q, want %q", parents, wantParents)
			}
			if tree := fixture.git("show", "-s", "--format=%T", result.NewSHA); tree != result.TreeSHA {
				t.Errorf("commit tree = %q, result tree %q", tree, result.TreeSHA)
			}
			if content := fixture.git("show", result.NewSHA+":source.txt"); content != "source" {
				t.Errorf("source.txt = %q", content)
			}
			if content := fixture.git("show", result.NewSHA+":target.txt"); content != "target" {
				t.Errorf("target.txt = %q", content)
			}
			if branch := fixture.git("rev-parse", "refs/heads/main"); branch != result.NewSHA {
				t.Errorf("main = %q, want %q", branch, result.NewSHA)
			}
			if result.OldSHA != request.ExpectedTargetSHA || result.HeadSHA != request.HeadSHA || result.TargetRef != "refs/heads/main" || result.Strategy != testCase.strategy {
				t.Errorf("result = %+v", result)
			}
			if metadata := fixture.git("show", "-s", "--format=%an%x00%ae%x00%aI%x00%cn%x00%ce%x00%cI%x00%B", result.NewSHA); metadata != "Merge Author\x00author@example.com\x002024-03-01T10:20:30+02:00\x00Merge Committer\x00committer@example.com\x002024-03-02T11:21:31-03:00\x00Merge feature\n\nDeterministic body." {
				t.Errorf("commit metadata = %q", metadata)
			}
		})
	}
}

func TestMergeConflictLeavesBranchUnchanged(t *testing.T) {
	testCases := []struct {
		name     string
		strategy MergeStrategy
	}{
		{name: "merge commit", strategy: MergeCommit},
		{name: "squash", strategy: MergeSquash},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			fixture := newMergeFixture(t, "sha1", true)
			before := fixture.git("rev-parse", "refs/heads/main")
			_, err := fixture.service.Merge(context.Background(), fixture.id, fixture.request(testCase.strategy))
			if !errors.Is(err, ErrMergeConflict) {
				t.Fatalf("Merge() error = %v, want ErrMergeConflict", err)
			}
			if after := fixture.git("rev-parse", "refs/heads/main"); after != before {
				t.Errorf("main changed from %s to %s", before, after)
			}
			fixture.assertNoTransientRefs()
		})
	}
}

func TestMergeUnrelatedHistoryIsConflict(t *testing.T) {
	fixture := newMergeFixture(t, "sha1", false)
	fixture.workGit(nil, "checkout", "--orphan", "unrelated")
	fixture.workGit(nil, "rm", "-rf", "--ignore-unmatch", "--", ".")
	fixture.write("unrelated.txt", "unrelated\n")
	fixture.workGit(nil, "add", "--", ".")
	fixture.workGit(nil, "commit", "-m", "unrelated")
	fixture.headSHA = fixture.workGit(nil, "rev-parse", "HEAD")
	fixture.workGit(nil, "push", "--force", fixture.bare, "HEAD:refs/heads/feature")
	before := fixture.git("rev-parse", "refs/heads/main")

	_, err := fixture.service.Merge(context.Background(), fixture.id, fixture.request(MergeCommit))
	if !errors.Is(err, ErrMergeConflict) {
		t.Fatalf("Merge() error = %v, want ErrMergeConflict", err)
	}
	if after := fixture.git("rev-parse", "refs/heads/main"); after != before {
		t.Errorf("main changed from %s to %s", before, after)
	}
}

func TestMergeDoesNotRunRepositoryMergeDriver(t *testing.T) {
	fixture := newMergeFixture(t, "sha1", true)
	marker := filepath.Join(t.TempDir(), "driver-ran")
	fixture.git("config", "merge.untrusted.driver", "touch "+marker)

	_, err := fixture.service.Merge(context.Background(), fixture.id, fixture.request(MergeCommit))
	if !errors.Is(err, ErrMergeConflict) {
		t.Fatalf("Merge() error = %v, want ErrMergeConflict", err)
	}
	if _, statErr := os.Stat(marker); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("custom merge driver ran: %v", statErr)
	}
}

func TestMergeCASConflictLeavesBranchUnchanged(t *testing.T) {
	testCases := []struct {
		name          string
		mutateRequest func(*mergeFixture, *MergeRequest)
		want          error
	}{
		{
			name: "stale expected target",
			mutateRequest: func(fixture *mergeFixture, request *MergeRequest) {
				request.ExpectedTargetSHA = fixture.baseSHA
			},
			want: ErrMergeRefConflict,
		},
		{
			name: "target object missing",
			mutateRequest: func(fixture *mergeFixture, request *MergeRequest) {
				request.TargetBranch = "missing"
			},
			want: ErrObjectNotFound,
		},
		{
			name: "ref changes before CAS",
			mutateRequest: func(fixture *mergeFixture, _ *MergeRequest) {
				fixture.service.beforeMergeCAS = func() {
					fixture.git("update-ref", "refs/heads/main", fixture.headSHA, fixture.targetSHA)
				}
			},
			want: ErrMergeRefConflict,
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			fixture := newMergeFixture(t, "sha1", false)
			before := fixture.git("rev-parse", "refs/heads/main")
			request := fixture.request(MergeCommit)
			testCase.mutateRequest(fixture, &request)
			_, err := fixture.service.Merge(context.Background(), fixture.id, request)
			if !errors.Is(err, testCase.want) {
				t.Fatalf("Merge() error = %v, want %v", err, testCase.want)
			}
			after := fixture.git("rev-parse", "refs/heads/main")
			if testCase.name == "ref changes before CAS" {
				if after != fixture.headSHA {
					t.Errorf("concurrent update was overwritten: main = %s, want %s", after, fixture.headSHA)
				}
			} else if after != before {
				t.Errorf("main changed from %s to %s", before, after)
			}
			fixture.assertNoTransientRefs()
		})
	}
}

func TestMergeObjectErrors(t *testing.T) {
	fixture := newMergeFixture(t, "sha1", false)
	testCases := []struct {
		name string
		head string
		want error
	}{
		{name: "missing head", head: strings.Repeat("0", 40), want: ErrObjectNotFound},
		{name: "head is blob", head: fixture.git("rev-parse", fixture.targetSHA+":common.txt"), want: ErrMergeInput},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			request := fixture.request(MergeCommit)
			request.HeadSHA = testCase.head
			_, err := fixture.service.Merge(context.Background(), fixture.id, request)
			if !errors.Is(err, testCase.want) {
				t.Fatalf("Merge() error = %v, want %v", err, testCase.want)
			}
		})
	}
}

func TestMergeRejectsMaliciousInput(t *testing.T) {
	fixture := newMergeFixture(t, "sha1", false)
	testCases := []struct {
		name   string
		mutate func(*MergeRequest)
	}{
		{name: "branch option", mutate: func(request *MergeRequest) { request.TargetBranch = "-c" }},
		{name: "branch traversal", mutate: func(request *MergeRequest) { request.TargetBranch = "main..evil" }},
		{name: "message NUL", mutate: func(request *MergeRequest) { request.Message = "merge\x00injected" }},
		{name: "message invalid UTF-8", mutate: func(request *MergeRequest) { request.Message = string([]byte{0xff}) }},
		{name: "author newline", mutate: func(request *MergeRequest) { request.Author.Name = "Alice\nGIT_COMMITTER_NAME=Eve" }},
		{name: "author control", mutate: func(request *MergeRequest) { request.Author.Name = "Alice\tInjected" }},
		{name: "email bracket", mutate: func(request *MergeRequest) { request.Author.Email = "alice@example.com> <eve@example.com" }},
		{name: "committer NUL", mutate: func(request *MergeRequest) { request.Committer.Name = "Eve\x00Injected" }},
		{name: "zero time", mutate: func(request *MergeRequest) { request.Author.Time = time.Time{} }},
		{name: "wrong SHA length", mutate: func(request *MergeRequest) { request.HeadSHA = strings.Repeat("a", 64) }},
		{name: "uppercase SHA", mutate: func(request *MergeRequest) { request.HeadSHA = strings.ToUpper(request.HeadSHA) }},
		{name: "unknown strategy", mutate: func(request *MergeRequest) { request.Strategy = "rebase" }},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			request := fixture.request(MergeCommit)
			testCase.mutate(&request)
			_, err := fixture.service.Merge(context.Background(), fixture.id, request)
			if !errors.Is(err, ErrMergeInput) {
				t.Fatalf("Merge() error = %v, want ErrMergeInput", err)
			}
		})
	}
}

func TestMergeCancellationLeavesNoRefs(t *testing.T) {
	fixture := newMergeFixture(t, "sha1", false)
	before := fixture.git("rev-parse", "refs/heads/main")
	temporaryBefore, err := filepath.Glob(filepath.Join(os.TempDir(), "adenosine-merge-*"))
	if err != nil {
		t.Fatalf("list temporary merge repositories: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	fixture.service.beforeMergeCAS = cancel
	_, err = fixture.service.Merge(ctx, fixture.id, fixture.request(MergeCommit))
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Merge() error = %v, want context.Canceled", err)
	}
	if after := fixture.git("rev-parse", "refs/heads/main"); after != before {
		t.Errorf("main changed from %s to %s", before, after)
	}
	fixture.assertNoTransientRefs()
	temporaryAfter, err := filepath.Glob(filepath.Join(os.TempDir(), "adenosine-merge-*"))
	if err != nil {
		t.Fatalf("list temporary merge repositories: %v", err)
	}
	if len(temporaryAfter) != len(temporaryBefore) {
		t.Errorf("temporary merge repositories before = %v, after = %v", temporaryBefore, temporaryAfter)
	}
}

func TestMergeSHA256(t *testing.T) {
	fixture := newMergeFixture(t, "sha256", false)
	request := fixture.request(MergeCommit)
	result, err := fixture.service.Merge(context.Background(), fixture.id, request)
	if err != nil {
		t.Fatalf("Merge() error = %v", err)
	}
	if len(result.NewSHA) != 64 || len(result.TreeSHA) != 64 {
		t.Errorf("SHA-256 result = %+v", result)
	}
	if parents := fixture.git("show", "-s", "--format=%P", result.NewSHA); parents != request.ExpectedTargetSHA+" "+request.HeadSHA {
		t.Errorf("parents = %q", parents)
	}
}

type mergeFixture struct {
	t         *testing.T
	binary    string
	worktree  string
	bare      string
	id        repository.ID
	service   *Service
	baseSHA   string
	targetSHA string
	headSHA   string
}

func newMergeFixture(t *testing.T, objectFormat string, conflicting bool) *mergeFixture {
	t.Helper()
	binary, err := exec.LookPath("git")
	if err != nil {
		t.Skip("git executable is unavailable")
	}
	root := t.TempDir()
	worktree := filepath.Join(root, "source")
	initArgs := []string{"init", "--initial-branch=main"}
	if objectFormat == "sha256" {
		initArgs = append(initArgs, "--object-format=sha256")
	}
	initArgs = append(initArgs, worktree)
	command := exec.Command(binary, initArgs...)
	if output, initErr := command.CombinedOutput(); initErr != nil {
		if objectFormat == "sha256" {
			t.Skipf("host Git does not support SHA-256 repositories: %v: %s", initErr, output)
		}
		t.Fatalf("initialize repository: %v: %s", initErr, output)
	}
	fixture := &mergeFixture{t: t, binary: binary, worktree: worktree, bare: filepath.Join(root, "target.git"), id: repository.ID{}}
	fixture.write("common.txt", "base\n")
	fixture.write(".gitattributes", "common.txt merge=untrusted\n")
	fixture.workGit(nil, "add", "--", ".")
	fixture.workGit(nil, "commit", "-m", "base")
	fixture.baseSHA = fixture.workGit(nil, "rev-parse", "HEAD")
	fixture.workGit(nil, "checkout", "-b", "feature")
	if conflicting {
		fixture.write("common.txt", "source\n")
	} else {
		fixture.write("source.txt", "source\n")
	}
	fixture.workGit(nil, "add", "--", ".")
	fixture.workGit(nil, "commit", "-m", "feature")
	fixture.headSHA = fixture.workGit(nil, "rev-parse", "HEAD")
	fixture.workGit(nil, "checkout", "main")
	if conflicting {
		fixture.write("common.txt", "target\n")
	} else {
		fixture.write("target.txt", "target\n")
	}
	fixture.workGit(nil, "add", "--", ".")
	fixture.workGit(nil, "commit", "-m", "target")
	fixture.targetSHA = fixture.workGit(nil, "rev-parse", "HEAD")
	clone := exec.Command(binary, "clone", "--bare", worktree, fixture.bare)
	if output, cloneErr := clone.CombinedOutput(); cloneErr != nil {
		t.Fatalf("clone bare repository: %v: %s", cloneErr, output)
	}
	fixture.service = NewService(NewRunner(binary), fixedRepositoryPath{path: fixture.bare})
	return fixture
}

func (fixture *mergeFixture) request(strategy MergeStrategy) MergeRequest {
	authorTime := time.Date(2024, 3, 1, 10, 20, 30, 0, time.FixedZone("author", 2*60*60))
	committerTime := time.Date(2024, 3, 2, 11, 21, 31, 0, time.FixedZone("committer", -3*60*60))
	return MergeRequest{
		TargetBranch: "main", ExpectedTargetSHA: fixture.targetSHA, HeadSHA: fixture.headSHA,
		Strategy: strategy, Message: "Merge feature\n\nDeterministic body.\n",
		Author:    MergeIdentity{Name: "Merge Author", Email: "author@example.com", Time: authorTime},
		Committer: MergeIdentity{Name: "Merge Committer", Email: "committer@example.com", Time: committerTime},
	}
}

func (fixture *mergeFixture) workGit(input []byte, arguments ...string) string {
	fixture.t.Helper()
	prefix := []string{"-C", fixture.worktree, "-c", "commit.gpgSign=false"}
	command := exec.Command(fixture.binary, append(prefix, arguments...)...)
	command.Stdin = bytes.NewReader(input)
	command.Env = append(command.Environ(), testIdentityEnv()...)
	output, err := command.CombinedOutput()
	if err != nil {
		fixture.t.Fatalf("git %v: %v: %s", arguments, err, output)
	}
	return strings.TrimSpace(string(output))
}

func (fixture *mergeFixture) git(arguments ...string) string {
	fixture.t.Helper()
	command := exec.Command(fixture.binary, append([]string{"--git-dir=" + fixture.bare}, arguments...)...)
	output, err := command.CombinedOutput()
	if err != nil {
		fixture.t.Fatalf("git %v: %v: %s", arguments, err, output)
	}
	return strings.TrimSpace(string(output))
}

func (fixture *mergeFixture) write(name, content string) {
	fixture.t.Helper()
	if !utf8.ValidString(content) {
		fixture.t.Fatal("test content is not UTF-8")
	}
	if err := os.WriteFile(filepath.Join(fixture.worktree, name), []byte(content), 0o600); err != nil {
		fixture.t.Fatalf("write fixture file: %v", err)
	}
}

func (fixture *mergeFixture) assertNoTransientRefs() {
	fixture.t.Helper()
	if refs := fixture.git("for-each-ref", "--format=%(refname)", "refs/adenosine/"); refs != "" {
		fixture.t.Errorf("transient refs remain: %q", refs)
	}
}
