package git

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/adenosine-dev/adenosine/internal/repository"
	"github.com/adenosine-dev/adenosine/internal/storage"
	"github.com/google/uuid"
)

func TestForkCopiesPublicRefs(t *testing.T) {
	t.Parallel()
	testCases := []struct {
		name string
	}{
		{name: "copies branches and tags without internal refs"},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			fixture := newForkFixture(t)
			upstreamHead := fixture.upstreamHead(t)
			runGit(t, fixture.binary, "--git-dir="+fixture.upstreamPath, "tag", "v1", upstreamHead)
			runGit(t, fixture.binary, "--git-dir="+fixture.upstreamPath, "update-ref", "refs/adenosine/private", upstreamHead)

			if err := fixture.service.Fork(context.Background(), fixture.forkID, fixture.source(), "main"); err != nil {
				t.Fatalf("Fork() error = %v", err)
			}

			if got := fixture.forkRef(t, "refs/heads/main"); got != upstreamHead {
				t.Fatalf("main = %q, want %q", got, upstreamHead)
			}
			if got := fixture.forkRef(t, "refs/tags/v1"); got != upstreamHead {
				t.Fatalf("tag = %q, want %q", got, upstreamHead)
			}
			if got := fixture.forkRef(t, "refs/adenosine/private"); got != "" {
				t.Fatalf("internal ref = %q, want absent", got)
			}
		})
	}
}

func TestSyncFork(t *testing.T) {
	t.Parallel()
	testCases := []struct {
		name      string
		diverge   bool
		wantError error
	}{
		{name: "fast-forwards the default branch"},
		{name: "rejects a divergent default branch", diverge: true, wantError: ErrForkDiverged},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			fixture := newForkFixture(t)
			if err := fixture.service.Fork(context.Background(), fixture.forkID, fixture.source(), "main"); err != nil {
				t.Fatalf("Fork() error = %v", err)
			}
			before := fixture.forkRef(t, "refs/heads/main")
			if testCase.diverge {
				fixture.advance(t, fixture.forkPath, "fork change")
			}
			after := fixture.advance(t, fixture.upstreamPath, "upstream change")

			result, err := fixture.service.SyncFork(context.Background(), fixture.forkID, fixture.source(), "main")
			if !errors.Is(err, testCase.wantError) {
				t.Fatalf("SyncFork() error = %v, want %v", err, testCase.wantError)
			}
			if testCase.wantError != nil {
				if got := fixture.forkRef(t, "refs/heads/main"); got == after {
					t.Fatalf("divergent fork advanced to %q", got)
				}
				return
			}
			if !result.Updated || result.BeforeSHA != before || result.AfterSHA != after {
				t.Fatalf("SyncFork() = %+v, want update %s -> %s", result, before, after)
			}
			if got := fixture.forkRef(t, "refs/heads/main"); got != after {
				t.Fatalf("main = %q, want %q", got, after)
			}
		})
	}
}

type forkFixture struct {
	binary       string
	service      *Service
	upstreamID   repository.ID
	forkID       repository.ID
	upstreamPath string
	forkPath     string
}

func newForkFixture(t *testing.T) *forkFixture {
	t.Helper()
	binary, err := exec.LookPath("git")
	if err != nil {
		t.Skip("git executable is unavailable")
	}
	paths, err := storage.NewFilesystem(filepath.Join(t.TempDir(), "repositories"))
	if err != nil {
		t.Fatalf("create filesystem storage: %v", err)
	}
	service := NewService(NewRunner(binary), paths)
	upstreamID := repository.ID(uuid.New())
	forkID := repository.ID(uuid.New())
	if err := service.Init(context.Background(), upstreamID, "main"); err != nil {
		t.Fatalf("initialize upstream: %v", err)
	}
	upstreamPath, err := paths.Path(context.Background(), upstreamID)
	if err != nil {
		t.Fatalf("resolve upstream: %v", err)
	}
	forkPath, err := paths.Path(context.Background(), forkID)
	if err != nil {
		t.Fatalf("resolve fork: %v", err)
	}
	fixture := &forkFixture{
		binary: binary, service: service, upstreamID: upstreamID, forkID: forkID,
		upstreamPath: upstreamPath, forkPath: forkPath,
	}
	fixture.advance(t, upstreamPath, "initial commit")
	return fixture
}

func (fixture *forkFixture) source() repository.ForkSource {
	return repository.ForkSource{
		URI: "at://did:plc:alice/dev.adenosine.repo/upstream", CID: "bafyupstream",
		LocalRepositoryID: &fixture.upstreamID,
	}
}

func (fixture *forkFixture) upstreamHead(t *testing.T) string {
	t.Helper()
	return gitOutput(t, fixture.binary, "--git-dir="+fixture.upstreamPath, "rev-parse", "refs/heads/main")
}

func (fixture *forkFixture) forkRef(t *testing.T, name string) string {
	t.Helper()
	command := exec.Command(fixture.binary, "--git-dir="+fixture.forkPath, "rev-parse", "--verify", name)
	output, err := command.Output()
	if err != nil {
		return ""
	}
	return stringTrimSpace(output)
}

func (fixture *forkFixture) advance(t *testing.T, remotePath, message string) string {
	t.Helper()
	work := filepath.Join(t.TempDir(), "work")
	runGit(t, fixture.binary, "clone", remotePath, work)
	runGit(t, fixture.binary, "-C", work, "config", "user.name", "Fork Test")
	runGit(t, fixture.binary, "-C", work, "config", "user.email", "fork@example.com")
	file := filepath.Join(work, "history.txt")
	content := []byte(message + "\n")
	if existing, err := os.ReadFile(file); err == nil {
		content = append(existing, content...)
	}
	if err := os.WriteFile(file, content, 0o600); err != nil {
		t.Fatalf("write commit file: %v", err)
	}
	runGit(t, fixture.binary, "-C", work, "add", "history.txt")
	runGit(t, fixture.binary, "-C", work, "commit", "-m", message)
	runGit(t, fixture.binary, "-C", work, "push", "origin", "HEAD:main")
	return gitOutput(t, fixture.binary, "-C", work, "rev-parse", "HEAD")
}

func stringTrimSpace(value []byte) string {
	for len(value) > 0 && (value[len(value)-1] == '\n' || value[len(value)-1] == '\r') {
		value = value[:len(value)-1]
	}
	return string(value)
}
