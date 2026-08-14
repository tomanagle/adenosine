package cli_test

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/adenosine-dev/adenosine/internal/auth"
	"github.com/adenosine-dev/adenosine/internal/repository"
	"github.com/adenosine-dev/adenosine/internal/restapi"
	"github.com/google/uuid"
)

type readyInstance struct{}

func (readyInstance) Ping(context.Context) error { return nil }

type fixedTokenAuthenticator struct{}

func (fixedTokenAuthenticator) Authenticate(_ context.Context, token string) (auth.AccessToken, error) {
	if token != "black-box-token" {
		return auth.AccessToken{}, auth.ErrUnauthorized
	}
	return auth.AccessToken{AccountDID: "did:plc:alice", Scopes: []string{auth.ScopeRepositoryRead}}, nil
}

type fixedRepositoryManager struct{ value repository.Repository }

func (manager fixedRepositoryManager) Create(context.Context, repository.CreateInput) (repository.Repository, error) {
	return repository.Repository{}, nil
}

func (manager fixedRepositoryManager) GetByOwnerSlug(_ context.Context, owner, slug string) (repository.Repository, error) {
	if owner != "alice" || slug != manager.value.Slug {
		return repository.Repository{}, repository.ErrNotFound
	}
	return manager.value, nil
}

func (fixedRepositoryManager) ListByOrganization(context.Context, uuid.UUID) ([]repository.Repository, error) {
	return []repository.Repository{}, nil
}

func TestExecutableAgainstAdenosineInstance(t *testing.T) {
	testCases := []struct {
		name string
	}{
		{name: "login and repository view"},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			now := time.Date(2026, 8, 14, 0, 0, 0, 0, time.UTC)
			manager := fixedRepositoryManager{value: repository.Repository{
				ID: repository.ID(uuid.MustParse("b63865c6-f733-4a77-a204-f517db8a69b8")), OwnerDID: "did:plc:alice",
				Slug: "project", Visibility: repository.VisibilityPublic, State: repository.StateActive,
				DefaultBranch: "main", ATURI: "at://did:plc:alice/dev.adenosine.repo/project", ATCID: "bafyrepo",
				CreatedAt: now, UpdatedAt: now,
			}}
			apiServer, err := restapi.NewServer(":0", "http://adenosine.test", readyInstance{}, slog.New(slog.NewTextHandler(io.Discard, nil)), restapi.Dependencies{
				TokenAuth: fixedTokenAuthenticator{}, Repositories: manager,
			}, nil)
			if err != nil {
				t.Fatalf("NewServer() error = %v", err)
			}
			instance := httptest.NewServer(apiServer.Handler)
			t.Cleanup(instance.Close)

			root, err := filepath.Abs(filepath.Join("..", ".."))
			if err != nil {
				t.Fatalf("repository root: %v", err)
			}
			binary := filepath.Join(t.TempDir(), "adenosine")
			if runtime.GOOS == "windows" {
				binary += ".exe"
			}
			build := exec.Command("go", "build", "-o", binary, "./cmd/adenosine")
			build.Dir = root
			if output, err := build.CombinedOutput(); err != nil {
				t.Fatalf("build CLI: %v\n%s", err, output)
			}

			configDirectory := t.TempDir()
			login := exec.Command(binary, "login", "--host", instance.URL, "--token-stdin")
			login.Env = append(os.Environ(), "ADENOSINE_CONFIG_DIR="+configDirectory)
			login.Stdin = strings.NewReader("black-box-token\n")
			if output, err := login.CombinedOutput(); err != nil {
				t.Fatalf("login: %v\n%s", err, output)
			}

			view := exec.Command(binary, "repo", "view", "--json", "alice/project")
			view.Env = append(os.Environ(), "ADENOSINE_CONFIG_DIR="+configDirectory)
			var output bytes.Buffer
			view.Stdout, view.Stderr = &output, &output
			if err := view.Run(); err != nil {
				t.Fatalf("repository view: %v\n%s", err, output.String())
			}
			if !strings.Contains(output.String(), `"slug": "project"`) || strings.Contains(output.String(), "black-box-token") {
				t.Fatalf("repository output = %q", output.String())
			}
		})
	}
}
