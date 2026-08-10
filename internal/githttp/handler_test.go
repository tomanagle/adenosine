package githttp

import (
	"context"
	"crypto/sha256"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/adenosine-dev/adenosine/internal/auth"
	gitservice "github.com/adenosine-dev/adenosine/internal/git"
	"github.com/adenosine-dev/adenosine/internal/repository"
	"github.com/adenosine-dev/adenosine/internal/storage"
	"github.com/google/uuid"
)

type fixedResolver struct {
	repository repository.Repository
}

type tokenStore struct {
	token auth.AccessToken
}

func (store tokenStore) CreateToken(context.Context, auth.AccessToken) (auth.AccessToken, error) {
	return auth.AccessToken{}, nil
}

func (store tokenStore) GetActiveTokenByHash(_ context.Context, hash []byte) (auth.AccessToken, error) {
	if string(hash) != string(store.token.Hash) {
		return auth.AccessToken{}, auth.ErrUnauthorized
	}
	return store.token, nil
}

func (store tokenStore) TouchToken(context.Context, uuid.UUID, time.Time) error { return nil }

type allowRepositoryWrite struct{}

func (allowRepositoryWrite) CanWriteRepository(context.Context, string, repository.ID) (bool, error) {
	return true, nil
}

type authClock struct{}

func (authClock) Now() time.Time { return time.Now() }

type pushEvents struct{ count int }

func (events *pushEvents) GitPushReceived(context.Context, repository.Repository) error {
	events.count++
	return nil
}

func (resolver fixedResolver) GetByOwnerSlug(_ context.Context, owner, slug string) (repository.Repository, error) {
	if owner != "alice" || slug != resolver.repository.Slug {
		return repository.Repository{}, repository.ErrNotFound
	}
	return resolver.repository, nil
}

func TestUploadPackSupportsRealClone(t *testing.T) {
	t.Parallel()
	testCases := []struct{ name string }{{name: "real clone and push"}}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			binary, err := exec.LookPath("git")
			if err != nil {
				t.Skip("git executable is unavailable")
			}
			filesystem, err := storage.NewFilesystem(t.TempDir())
			if err != nil {
				t.Fatalf("create filesystem storage: %v", err)
			}
			git := gitservice.NewService(gitservice.NewRunner(binary), filesystem)
			repo := repository.Repository{
				ID:            repository.ID(uuid.MustParse("0198a851-2a89-7ae2-a370-dc68883e3af1")),
				Slug:          "hello-world",
				Visibility:    repository.VisibilityPublic,
				State:         repository.StateActive,
				DefaultBranch: "main",
			}
			if err := git.Init(context.Background(), repo.ID, repo.DefaultBranch); err != nil {
				t.Fatalf("initialize bare repository: %v", err)
			}
			barePath, err := filesystem.Path(context.Background(), repo.ID)
			if err != nil {
				t.Fatalf("resolve bare repository path: %v", err)
			}

			source := filepath.Join(t.TempDir(), "source")
			runGit(t, binary, "init", "--initial-branch=main", source)
			if err := os.WriteFile(filepath.Join(source, "README.md"), []byte("# Hello\n"), 0o600); err != nil {
				t.Fatalf("write source file: %v", err)
			}
			runGit(t, binary, "-C", source, "add", "README.md")
			runGit(t, binary, "-C", source, "-c", "user.name=Adenosine Test", "-c", "user.email=test@example.com", "commit", "-m", "initial")
			runGit(t, binary, "-C", source, "push", barePath, "main")

			plaintextToken := "adn_pat_http_integration"
			tokenHash := sha256.Sum256([]byte(plaintextToken))
			authorizer := auth.NewGitAuthorizer(
				tokenStore{token: auth.AccessToken{
					ID:         uuid.MustParse("0198a851-2a89-7ae2-a370-dc68883e3af3"),
					AccountDID: "did:plc:alice",
					Hash:       tokenHash[:],
					Scopes:     []string{auth.ScopeRepositoryWrite},
				}},
				allowRepositoryWrite{},
				authClock{},
			)
			events := &pushEvents{}
			server := httptest.NewServer(NewHandler(fixedResolver{repository: repo}, git, authorizer, events))
			defer server.Close()
			writeURL := strings.Replace(server.URL, "http://", "http://alice:"+plaintextToken+"@", 1) + "/alice/hello-world.git"
			clone := filepath.Join(t.TempDir(), "clone")
			runGit(t, binary, "-c", "protocol.version=2", "clone", server.URL+"/alice/hello-world.git", clone)

			contents, err := os.ReadFile(filepath.Join(clone, "README.md"))
			if err != nil {
				t.Fatalf("read cloned file: %v", err)
			}
			if string(contents) != "# Hello\n" {
				t.Fatalf("cloned content = %q", contents)
			}

			if err := os.WriteFile(filepath.Join(source, "README.md"), []byte("# Hello again\n"), 0o600); err != nil {
				t.Fatalf("update source file: %v", err)
			}
			runGit(t, binary, "-C", source, "add", "README.md")
			runGit(t, binary, "-C", source, "-c", "user.name=Adenosine Test", "-c", "user.email=test@example.com", "commit", "-m", "update")
			runGit(t, binary, "-C", source, "push", writeURL, "main")
			runGit(t, binary, "-C", source, "tag", "v1.0.0")
			runGit(t, binary, "-C", source, "push", writeURL, "v1.0.0")
			runGit(t, binary, "-C", source, "push", writeURL, ":refs/tags/v1.0.0")
			if events.count != 3 {
				t.Fatalf("push events = %d, want 3", events.count)
			}
		})
	}
}

func TestParsePath(t *testing.T) {
	t.Parallel()
	testCases := []struct {
		name      string
		path      string
		operation string
		ok        bool
	}{
		{name: "info refs", path: "/alice/repo.git/info/refs", operation: "info/refs", ok: true},
		{name: "upload pack", path: "/alice/repo.git/git-upload-pack", operation: "git-upload-pack", ok: true},
		{name: "receive pack", path: "/alice/repo.git/git-receive-pack", operation: "git-receive-pack", ok: true},
		{name: "missing git suffix", path: "/alice/repo/info/refs"},
		{name: "traversal", path: "/../repo.git/info/refs"},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			_, _, operation, ok := parsePath(testCase.path)
			if ok != testCase.ok || operation != testCase.operation {
				t.Fatalf("parsePath(%q) = operation %q, ok %v", testCase.path, operation, ok)
			}
		})
	}
}

func runGit(t *testing.T, binary string, arguments ...string) {
	t.Helper()
	command := exec.Command(binary, arguments...)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v: %s", arguments, err, output)
	}
}
