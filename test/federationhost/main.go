package main

import (
	"bytes"
	"context"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/adenosine-dev/adenosine/internal/auth"
	"github.com/adenosine-dev/adenosine/internal/database"
	gitservice "github.com/adenosine-dev/adenosine/internal/git"
	"github.com/adenosine-dev/adenosine/internal/repository"
	"github.com/adenosine-dev/adenosine/internal/storage"
	"github.com/google/uuid"
)

const (
	hostedDID       = "did:plc:cccccccccccccccccccccccc"
	hostedRepoID    = "0198a851-2a89-7ae2-a370-dc68883e3af5"
	sourceDID       = "did:plc:bbbbbbbbbbbbbbbbbbbbbbbb"
	sourceRepoID    = "0198a851-2a89-7ae2-a370-dc68883e3af8"
	repoCID         = "bafybeigdyrzt5sfp7udm7hu76uh7y26nf3efuylqabf3oclgtqy55fbzdi"
	readme          = "# Hosted Federation Repository\n"
	featureFile     = "Fetched from the federated feature branch on B.\n"
	expectedBaseSHA = "5e8f4658bd4277bfe9033c4562efba862b1a8466"
	expectedHeadSHA = "6f072a30c8d42d61fc35099dd8cc01e6d86d2c05"
	databaseA       = "postgres://adenosine:federation-a@postgres-a:5432/adenosine?sslmode=disable"
	databaseB       = "postgres://adenosine:federation-b@postgres-b:5432/adenosine?sslmode=disable"
)

var fixedTime = time.Date(2026, 8, 9, 12, 0, 0, 0, time.UTC)

type fixedClock struct{}

func (fixedClock) Now() time.Time { return fixedTime }

type fixedIDGenerator struct{ id string }

func (generator fixedIDGenerator) New() (repository.ID, error) {
	return repository.ID(uuid.MustParse(generator.id)), nil
}

type hostConfig struct {
	name, ownerDID, repoID, repoSlug, displayName, description string
	baseURL, sshHost, databaseURL, uri                         string
	feature                                                    bool
}

type deterministicPublisher struct{ config hostConfig }

func (publisher deterministicPublisher) Publish(_ context.Context, publication repository.Publication) (repository.ATIdentity, error) {
	config := publisher.config
	wantHTTPS := config.baseURL + "/" + config.ownerDID + "/" + config.repoSlug + ".git"
	wantWeb := strings.TrimSuffix(wantHTTPS, ".git")
	wantSSH := "ssh://git@" + config.sshHost + ":2222/" + config.ownerDID + "/" + config.repoSlug + ".git"
	if publication.ID.String() != config.repoID || publication.OwnerDID != config.ownerDID || publication.Slug != config.repoSlug ||
		publication.Name != config.displayName || publication.Description != config.description ||
		publication.DefaultBranch != "main" || publication.GitHTTPS != wantHTTPS || publication.GitSSH != wantSSH || publication.Web != wantWeb ||
		!publication.CreatedAt.Equal(fixedTime) || !publication.UpdatedAt.Equal(fixedTime) {
		return repository.ATIdentity{}, fmt.Errorf("unexpected publication: %+v", publication)
	}
	return repository.ATIdentity{URI: config.uri, CID: repoCID}, nil
}

func main() {
	instance := flag.String("instance", "a", "federation host fixture: a or b")
	flag.Parse()
	if err := run(context.Background(), *instance); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(ctx context.Context, instance string) error {
	config, err := configFor(instance)
	if err != nil {
		return err
	}
	databaseURL := valueOrDefault("DATABASE_URL", config.databaseURL)
	repositoryRoot := valueOrDefault("ADENOSINE_REPO_ROOT", "/var/lib/adenosine/repos")
	gitBinary := valueOrDefault("ADENOSINE_GIT_BINARY", "git")

	db, err := database.Open(ctx, databaseURL, nil)
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}
	defer db.Close()

	accounts := auth.NewPostgresStore(db.Queries())
	if _, err := accounts.UpsertLogin(ctx, config.ownerDID, config.name+".example", fixedTime); err != nil {
		return fmt.Errorf("create repository owner: %w", err)
	}

	filesystem, err := storage.NewFilesystem(repositoryRoot)
	if err != nil {
		return fmt.Errorf("open repository storage: %w", err)
	}
	nativeGit := gitservice.NewService(gitservice.NewRunner(gitBinary), filesystem)
	repositories := repository.NewService(
		repository.NewPostgresStore(db.Queries()), nativeGit, fixedClock{}, fixedIDGenerator{id: config.repoID}, deterministicPublisher{config: config},
		repository.Must(config.baseURL, config.sshHost, 2222),
	)
	created, err := repositories.Create(ctx, repository.CreateInput{
		OwnerDID: config.ownerDID, Slug: config.repoSlug, DisplayName: config.displayName, Description: config.description,
		Visibility: repository.VisibilityPublic, DefaultBranch: "main",
	})
	if err != nil {
		return fmt.Errorf("create hosted repository: %w", err)
	}
	if created.State != repository.StateActive || created.ATURI != config.uri || created.ATCID != repoCID {
		return fmt.Errorf("created repository is not active with expected identity: %+v", created)
	}

	path, err := filesystem.Path(ctx, created.ID)
	if err != nil {
		return fmt.Errorf("resolve repository path: %w", err)
	}
	base, err := writeInitialCommit(ctx, gitBinary, path)
	if err != nil {
		return err
	}
	if base != expectedBaseSHA {
		return fmt.Errorf("deterministic main commit = %s, want %s", base, expectedBaseSHA)
	}
	if config.feature {
		head, err := writeFeatureCommit(ctx, gitBinary, path, base)
		if err != nil {
			return err
		}
		if head != expectedHeadSHA {
			return fmt.Errorf("deterministic feature commit = %s, want %s", head, expectedHeadSHA)
		}
	}
	fmt.Printf("created %s at %s\n", created.ATURI, path)
	return nil
}

func writeInitialCommit(ctx context.Context, binary, gitDir string) (string, error) {
	blob, err := git(ctx, binary, gitDir, []string{"hash-object", "-w", "--stdin"}, readme)
	if err != nil {
		return "", fmt.Errorf("write README blob: %w", err)
	}
	tree, err := git(ctx, binary, gitDir, []string{"mktree"}, "100644 blob "+blob+"\tREADME.md\n")
	if err != nil {
		return "", fmt.Errorf("write initial tree: %w", err)
	}
	commit, err := git(ctx, binary, gitDir, []string{"commit-tree", tree}, "Initial commit\n")
	if err != nil {
		return "", fmt.Errorf("write initial commit: %w", err)
	}
	if _, err := git(ctx, binary, gitDir, []string{"update-ref", "refs/heads/main", commit}, ""); err != nil {
		return "", fmt.Errorf("update main branch: %w", err)
	}
	return commit, nil
}

func writeFeatureCommit(ctx context.Context, binary, gitDir, parent string) (string, error) {
	readmeBlob, err := git(ctx, binary, gitDir, []string{"hash-object", "-w", "--stdin"}, readme)
	if err != nil {
		return "", fmt.Errorf("write feature README blob: %w", err)
	}
	featureBlob, err := git(ctx, binary, gitDir, []string{"hash-object", "-w", "--stdin"}, featureFile)
	if err != nil {
		return "", fmt.Errorf("write feature blob: %w", err)
	}
	treeInput := "100644 blob " + readmeBlob + "\tREADME.md\n100644 blob " + featureBlob + "\tFEDERATED.md\n"
	tree, err := git(ctx, binary, gitDir, []string{"mktree"}, treeInput)
	if err != nil {
		return "", fmt.Errorf("write feature tree: %w", err)
	}
	commit, err := gitAt(ctx, binary, gitDir, []string{"commit-tree", tree, "-p", parent}, "Federated feature change\n", fixedTime.Add(time.Minute))
	if err != nil {
		return "", fmt.Errorf("write feature commit: %w", err)
	}
	if _, err := git(ctx, binary, gitDir, []string{"update-ref", "refs/heads/feature", commit}, ""); err != nil {
		return "", fmt.Errorf("update feature branch: %w", err)
	}
	return commit, nil
}

func git(ctx context.Context, binary, gitDir string, args []string, input string) (string, error) {
	return gitAt(ctx, binary, gitDir, args, input, fixedTime)
}

func gitAt(ctx context.Context, binary, gitDir string, args []string, input string, timestamp time.Time) (string, error) {
	commandArgs := append([]string{"--git-dir=" + gitDir}, args...)
	command := exec.CommandContext(ctx, binary, commandArgs...)
	command.Stdin = strings.NewReader(input)
	command.Env = append(os.Environ(),
		"GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null", "GIT_AUTHOR_NAME=Adenosine Acceptance",
		"GIT_AUTHOR_EMAIL=acceptance@adenosine.test", "GIT_AUTHOR_DATE="+timestamp.Format(time.RFC3339),
		"GIT_COMMITTER_NAME=Adenosine Acceptance", "GIT_COMMITTER_EMAIL=acceptance@adenosine.test", "GIT_COMMITTER_DATE="+timestamp.Format(time.RFC3339),
	)
	var stdout, stderr bytes.Buffer
	command.Stdout, command.Stderr = &stdout, &stderr
	if err := command.Run(); err != nil {
		return "", fmt.Errorf("git %s: %w: %s", args[0], err, strings.TrimSpace(stderr.String()))
	}
	return strings.TrimSpace(stdout.String()), nil
}

func configFor(instance string) (hostConfig, error) {
	testCases := map[string]hostConfig{
		"a": {name: "hosted", ownerDID: hostedDID, repoID: hostedRepoID, repoSlug: "hosted-repo", displayName: "Hosted repository", description: "Cloned through isolated federation acceptance", baseURL: "https://adenosine-a-tls", sshHost: "adenosine-a", databaseURL: databaseA, uri: "at://" + hostedDID + "/dev.adenosine.repo/0198a8512a897ae2a370dc68883e3af5"},
		"b": {name: "bob", ownerDID: sourceDID, repoID: sourceRepoID, repoSlug: "b-only", displayName: "B only", description: "Federated pull request source repository", baseURL: "https://adenosine-b-tls", sshHost: "adenosine-b", databaseURL: databaseB, uri: "at://" + sourceDID + "/dev.adenosine.repo/b-only", feature: true},
	}
	config, ok := testCases[instance]
	if !ok {
		return hostConfig{}, fmt.Errorf("unknown federation host instance %q", instance)
	}
	return config, nil
}

func valueOrDefault(name, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(name)); value != "" {
		return value
	}
	return fallback
}
