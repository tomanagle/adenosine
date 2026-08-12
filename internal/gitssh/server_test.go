package gitssh

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"io"
	"log/slog"
	"net"
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
	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
)

type fixedSSHKeys struct {
	publicKey ssh.PublicKey
	touched   bool
}

func (keys *fixedSSHKeys) Lookup(_ context.Context, presented ssh.PublicKey) (auth.SSHIdentity, error) {
	if string(presented.Marshal()) != string(keys.publicKey.Marshal()) {
		return auth.SSHIdentity{}, auth.ErrUnauthorized
	}
	return auth.SSHIdentity{KeyID: uuid.MustParse("0198a851-2a89-7ae2-a370-dc68883e3af3"), AccountDID: "did:plc:alice"}, nil
}

func (keys *fixedSSHKeys) RecordUse(context.Context, uuid.UUID) error {
	keys.touched = true
	return nil
}

type fixedSSHRepository struct{ repository repository.Repository }

func (resolver fixedSSHRepository) GetByOwnerSlug(_ context.Context, owner, slug string) (repository.Repository, error) {
	if owner != "alice" || slug != resolver.repository.Slug {
		return repository.Repository{}, repository.ErrNotFound
	}
	return resolver.repository, nil
}

type allowSSHRepository struct{}

func (allowSSHRepository) CanReadRepository(context.Context, string, repository.ID) (bool, error) {
	return true, nil
}

func (allowSSHRepository) CanWriteRepository(context.Context, string, repository.ID) (bool, error) {
	return true, nil
}

type countPushEvents struct{ count int }

func (events *countPushEvents) GitPushReceived(context.Context, repository.Repository) error {
	events.count++
	return nil
}

func TestServerSupportsRealCloneAndPush(t *testing.T) {
	testCases := []struct{ name string }{{name: "real clone and push"}}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			binary, err := exec.LookPath("git")
			if err != nil {
				t.Skip("git executable is unavailable")
			}
			if _, err := exec.LookPath("ssh"); err != nil {
				t.Skip("ssh executable is unavailable")
			}
			filesystem, err := storage.NewFilesystem(t.TempDir())
			if err != nil {
				t.Fatalf("create repository storage: %v", err)
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
				t.Fatalf("resolve bare path: %v", err)
			}
			source := filepath.Join(t.TempDir(), "source")
			runSSHGit(t, nil, binary, "init", "--initial-branch=main", source)
			if err := os.WriteFile(filepath.Join(source, "README.md"), []byte("# Hello\n"), 0o600); err != nil {
				t.Fatalf("write README: %v", err)
			}
			runSSHGit(t, nil, binary, "-C", source, "add", "README.md")
			runSSHGit(t, nil, binary, "-C", source, "-c", "user.name=Adenosine Test", "-c", "user.email=test@example.com", "commit", "-m", "initial")
			runSSHGit(t, nil, binary, "-C", source, "push", barePath, "main")

			hostSigner := testSigner(t)
			userSigner := testSigner(t)
			keys := &fixedSSHKeys{publicKey: userSigner.PublicKey()}
			events := &countPushEvents{}
			server := NewServer("127.0.0.1:0", hostSigner, slog.New(slog.NewTextHandler(io.Discard, nil)), keys, fixedSSHRepository{repository: repo}, allowSSHRepository{}, git, events)
			listener, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatalf("listen: %v", err)
			}
			serveDone := make(chan error, 1)
			go func() { serveDone <- server.serve(listener) }()
			t.Cleanup(func() {
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				if err := server.Shutdown(ctx); err != nil {
					t.Errorf("shutdown SSH server: %v", err)
				}
				<-serveDone
			})

			credentials := t.TempDir()
			privateKeyPath := filepath.Join(credentials, "id_ed25519")
			privateBlock, err := ssh.MarshalPrivateKey(userSigner.(interface{ PrivateKey() any }).PrivateKey(), "test")
			if err != nil {
				t.Fatalf("marshal user private key: %v", err)
			}
			if err := os.WriteFile(privateKeyPath, pem.EncodeToMemory(privateBlock), 0o600); err != nil {
				t.Fatalf("write private key: %v", err)
			}
			knownHostsPath := filepath.Join(credentials, "known_hosts")
			knownHost := knownhosts.Line([]string{listener.Addr().String()}, hostSigner.PublicKey()) + "\n"
			if err := os.WriteFile(knownHostsPath, []byte(knownHost), 0o600); err != nil {
				t.Fatalf("write known hosts: %v", err)
			}
			sshCommand := strings.Join([]string{
				"ssh", "-F", "/dev/null", "-i", privateKeyPath,
				"-o", "IdentitiesOnly=yes", "-o", "UserKnownHostsFile=" + knownHostsPath,
				"-o", "StrictHostKeyChecking=yes",
			}, " ")
			environment := append(os.Environ(), "GIT_SSH_COMMAND="+sshCommand)
			remoteURL := "ssh://git@" + listener.Addr().String() + "/alice/hello-world.git"
			clone := filepath.Join(t.TempDir(), "clone")
			runSSHGit(t, environment, binary, "clone", remoteURL, clone)
			if err := os.WriteFile(filepath.Join(source, "README.md"), []byte("# Hello over SSH\n"), 0o600); err != nil {
				t.Fatalf("update README: %v", err)
			}
			runSSHGit(t, nil, binary, "-C", source, "add", "README.md")
			runSSHGit(t, nil, binary, "-C", source, "-c", "user.name=Adenosine Test", "-c", "user.email=test@example.com", "commit", "-m", "SSH update")
			runSSHGit(t, environment, binary, "-C", source, "push", remoteURL, "main")
			if !keys.touched {
				t.Fatal("successful SSH authentication did not touch the user key")
			}
			if events.count != 1 {
				t.Fatalf("push events = %d, want 1", events.count)
			}
		})
	}
}

func TestServerConnectionDeadlinesReleaseSlots(t *testing.T) {
	testCases := []struct {
		name      string
		handshake bool
	}{
		{name: "handshake timeout", handshake: true},
		{name: "authenticated pre-exec idle timeout"},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			hostSigner := testSigner(t)
			userSigner := testSigner(t)
			keys := &fixedSSHKeys{publicKey: userSigner.PublicKey()}
			server := NewServer("", hostSigner, slog.New(slog.NewTextHandler(io.Discard, nil)), keys, nil, nil, nil, nil)
			server.handshakeTimeout = time.Second
			if testCase.handshake {
				server.handshakeTimeout = 30 * time.Millisecond
			}
			server.sessionIdleTimeout = 30 * time.Millisecond
			listener, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatalf("listen: %v", err)
			}
			defer listener.Close()
			done := make(chan struct{})
			go func() {
				connection, acceptErr := listener.Accept()
				if acceptErr == nil {
					server.connectionSlots <- struct{}{}
					server.wait.Add(1)
					server.handleConnection(connection)
				}
				close(done)
			}()
			clientSide, err := net.Dial("tcp", listener.Addr().String())
			if err != nil {
				t.Fatalf("dial: %v", err)
			}

			if !testCase.handshake {
				clientConnection, channels, requests, err := ssh.NewClientConn(clientSide, listener.Addr().String(), &ssh.ClientConfig{
					User: "git", Auth: []ssh.AuthMethod{ssh.PublicKeys(userSigner)}, HostKeyCallback: ssh.InsecureIgnoreHostKey(), Timeout: time.Second,
				})
				if err != nil {
					t.Fatalf("authenticate client: %v", err)
				}
				client := ssh.NewClient(clientConnection, channels, requests)
				defer client.Close()
				channel, channelRequests, err := client.OpenChannel("session", nil)
				if err != nil {
					t.Fatalf("open session: %v", err)
				}
				defer channel.Close()
				go ssh.DiscardRequests(channelRequests)
				waitForSlot(t, server.sessionSlots)
			}

			select {
			case <-done:
			case <-time.After(time.Second):
				t.Fatal("timed connection did not close")
			}
			_ = clientSide.Close()
			if len(server.connectionSlots) != 0 || len(server.sessionSlots) != 0 {
				t.Fatalf("slots after cleanup = connection:%d session:%d", len(server.connectionSlots), len(server.sessionSlots))
			}
		})
	}
}

func waitForSlot(t *testing.T, slots chan struct{}) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for len(slots) == 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if len(slots) == 0 {
		t.Fatal("session slot was not acquired")
	}
}

func testSigner(t *testing.T) ssh.Signer {
	t.Helper()
	_, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	signer, err := ssh.NewSignerFromKey(privateKey)
	if err != nil {
		t.Fatalf("create signer: %v", err)
	}
	return &privateSigner{Signer: signer, privateKey: privateKey}
}

type privateSigner struct {
	ssh.Signer
	privateKey ed25519.PrivateKey
}

func (signer *privateSigner) PrivateKey() any { return signer.privateKey }

func runSSHGit(t *testing.T, environment []string, binary string, args ...string) {
	t.Helper()
	command := exec.Command(binary, args...)
	if environment != nil {
		command.Env = environment
	}
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, output)
	}
}
