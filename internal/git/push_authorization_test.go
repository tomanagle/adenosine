package git

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/adenosine-dev/adenosine/internal/branchprotection"
	"github.com/adenosine-dev/adenosine/internal/repository"
	"github.com/adenosine-dev/adenosine/internal/storage"
	"github.com/google/uuid"
)

func TestManagedPreReceiveHookRejectsBeforeRefMutation(t *testing.T) {
	testCases := []struct{ name string }{{name: "actionable atomic rejection"}}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			binary, err := exec.LookPath("git")
			if err != nil {
				t.Skip("git executable is unavailable")
			}
			root := t.TempDir()
			paths, err := storage.NewFilesystem(filepath.Join(root, "repositories"))
			if err != nil {
				t.Fatalf("NewFilesystem() error = %v", err)
			}
			id := repository.ID(uuid.MustParse("0198a851-2a89-7ae2-a370-dc68883e3af1"))
			service := NewService(NewRunner(binary), paths)
			fakeExecutable := filepath.Join(root, "authorize")
			marker := filepath.Join(root, "updates")
			script := "#!/bin/sh\nset -eu\ntest \"$1\" = authorize-push\ntest -n \"$ADENOSINE_HOOK_DATABASE_URL\"\ntest -n \"$ADENOSINE_HOOK_REPOSITORY_ID\"\ncat > \"$ADENOSINE_TEST_MARKER\"\necho 'required status ci/test is not successful' >&2\nexit 1\n"
			if err := os.WriteFile(fakeExecutable, []byte(script), 0o700); err != nil {
				t.Fatalf("write fake executable: %v", err)
			}
			if err := service.ConfigurePushAuthorization(PushAuthorizationConfig{Executable: fakeExecutable, DatabaseURL: "postgres://policy", GitBinary: binary}); err != nil {
				t.Fatalf("ConfigurePushAuthorization() error = %v", err)
			}
			if err := service.Init(context.Background(), id, "main"); err != nil {
				t.Fatalf("Init() error = %v", err)
			}
			if err := service.InstallPushAuthorization(context.Background(), id); err != nil {
				t.Fatalf("InstallPushAuthorization() error = %v", err)
			}
			bare, err := paths.Path(context.Background(), id)
			if err != nil {
				t.Fatalf("Path() error = %v", err)
			}
			work := filepath.Join(root, "work")
			runGitTestCommand(t, binary, "", "init", "--initial-branch=main", work)
			if err := os.WriteFile(filepath.Join(work, "README.md"), []byte("protected\n"), 0o600); err != nil {
				t.Fatalf("write fixture: %v", err)
			}
			runGitTestCommand(t, binary, work, "add", "README.md")
			runGitTestCommand(t, binary, work, "-c", "user.name=Alice", "-c", "user.email=alice@example.com", "commit", "-m", "initial")
			newSHA := runGitTestCommand(t, binary, work, "rev-parse", "HEAD")
			command := exec.Command(binary, "-C", work, "push", bare, "HEAD:refs/heads/main")
			command.Env = append(os.Environ(),
				"ADENOSINE_HOOK_EXECUTABLE="+fakeExecutable,
				"ADENOSINE_HOOK_DATABASE_URL=postgres://policy",
				"ADENOSINE_HOOK_GIT_BINARY="+binary,
				"ADENOSINE_HOOK_REPOSITORY_ID="+id.String(),
				"ADENOSINE_TEST_MARKER="+marker,
			)
			output, err := command.CombinedOutput()
			if err == nil || !strings.Contains(string(output), "required status ci/test is not successful") {
				t.Fatalf("push error/output = %v/%s", err, output)
			}
			if err := exec.Command(binary, "--git-dir="+bare, "show-ref", "--verify", "--quiet", "refs/heads/main").Run(); err == nil {
				t.Fatal("rejected push mutated refs/heads/main")
			}
			contents, err := os.ReadFile(marker)
			if err != nil || !strings.Contains(string(contents), strings.Repeat("0", 40)+" "+newSHA+" refs/heads/main") {
				t.Fatalf("pre-receive tuple = %q, error %v", contents, err)
			}
		})
	}
}

func TestVerifySSHSignaturesUsesActiveAdenosineKeys(t *testing.T) {
	testCases := []struct {
		name    string
		signed  bool
		wantErr bool
	}{
		{name: "trusted SSH signature", signed: true},
		{name: "unsigned commit", wantErr: true},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			binary, err := exec.LookPath("git")
			if err != nil {
				t.Skip("git executable is unavailable")
			}
			sshKeygen, err := exec.LookPath("ssh-keygen")
			if err != nil {
				t.Skip("ssh-keygen executable is unavailable")
			}
			root := t.TempDir()
			paths, err := storage.NewFilesystem(filepath.Join(root, "repositories"))
			if err != nil {
				t.Fatalf("NewFilesystem() error = %v", err)
			}
			id := repository.ID(uuid.New())
			service := NewService(NewRunner(binary), paths)
			if err := service.Init(context.Background(), id, "main"); err != nil {
				t.Fatalf("Init() error = %v", err)
			}
			bare, _ := paths.Path(context.Background(), id)
			work := filepath.Join(root, "work")
			runGitTestCommand(t, binary, "", "init", "--initial-branch=main", work)
			key := filepath.Join(root, "signing-key")
			if output, err := exec.Command(sshKeygen, "-q", "-t", "ed25519", "-N", "", "-f", key).CombinedOutput(); err != nil {
				t.Fatalf("create signing key: %v: %s", err, output)
			}
			if err := os.WriteFile(filepath.Join(work, "README.md"), []byte("signed\n"), 0o600); err != nil {
				t.Fatalf("write fixture: %v", err)
			}
			runGitTestCommand(t, binary, work, "add", "README.md")
			arguments := []string{"-c", "user.name=Alice", "-c", "user.email=alice@example.com"}
			if testCase.signed {
				arguments = append(arguments,
					"-c", "gpg.format=ssh",
					"-c", "gpg.ssh.program="+sshKeygen,
					"-c", "user.signingKey="+key+".pub",
					"-c", "commit.gpgSign=true",
				)
			}
			arguments = append(arguments, "commit", "-m", "fixture")
			runGitTestCommand(t, binary, work, arguments...)
			runGitTestCommand(t, binary, work, "push", bare, "HEAD:refs/heads/main")
			sha := runGitTestCommand(t, binary, work, "rev-parse", "HEAD")
			publicKey, err := os.ReadFile(key + ".pub")
			if err != nil {
				t.Fatalf("read public key: %v", err)
			}
			err = service.VerifySSHSignatures(context.Background(), id, []string{sha}, []branchprotection.AllowedSigner{{Principal: "did:plc:alice", PublicKey: strings.TrimSpace(string(publicKey))}})
			if (err != nil) != testCase.wantErr {
				t.Fatalf("VerifySSHSignatures() error = %v, want error %v", err, testCase.wantErr)
			}
		})
	}
}

func runGitTestCommand(t *testing.T, binary, directory string, arguments ...string) string {
	t.Helper()
	if directory != "" {
		arguments = append([]string{"-C", directory}, arguments...)
	}
	command := exec.Command(binary, arguments...)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v: %s", arguments, err, output)
	}
	return strings.TrimSpace(string(output))
}
