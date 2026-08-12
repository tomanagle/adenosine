package git

import (
	"bytes"
	"context"
	"encoding/pem"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/adenosine-dev/adenosine/internal/repository"
	"github.com/adenosine-dev/adenosine/internal/storage"
	"github.com/google/uuid"
)

type staticResolver struct {
	addresses []net.IP
	err       error
}

func (resolver staticResolver) LookupIP(context.Context, string, string) ([]net.IP, error) {
	return resolver.addresses, resolver.err
}

func TestValidateRemoteFetch(t *testing.T) {
	t.Parallel()
	valid := RemoteFetch{
		SourceURL:    "https://git.example.com/alice/project.git",
		SourceBranch: "feature/work",
		ExpectedHead: strings.Repeat("a", 40),
		Destination:  "pr-123",
	}
	testCases := []struct {
		name   string
		change func(*RemoteFetch)
	}{
		{name: "accepts canonical HTTPS", change: func(*RemoteFetch) {}},
		{name: "accepts DID path component", change: func(value *RemoteFetch) { value.SourceURL = "https://git.example.com/did:plc:alice/project.git" }},
		{name: "rejects HTTP", change: func(value *RemoteFetch) { value.SourceURL = "http://git.example.com/alice/project.git" }},
		{name: "rejects user info", change: func(value *RemoteFetch) { value.SourceURL = "https://user@git.example.com/alice/project.git" }},
		{name: "rejects query", change: func(value *RemoteFetch) { value.SourceURL += "?x=1" }},
		{name: "rejects fragment", change: func(value *RemoteFetch) { value.SourceURL += "#x" }},
		{name: "rejects escaped path", change: func(value *RemoteFetch) { value.SourceURL = "https://git.example.com/alice/%70roject.git" }},
		{name: "rejects escaped host", change: func(value *RemoteFetch) { value.SourceURL = "https://git%2eexample.com/alice/project.git" }},
		{name: "rejects uppercase host", change: func(value *RemoteFetch) { value.SourceURL = "https://Git.Example.com/alice/project.git" }},
		{name: "rejects default port", change: func(value *RemoteFetch) { value.SourceURL = "https://git.example.com:443/alice/project.git" }},
		{name: "rejects dot path", change: func(value *RemoteFetch) { value.SourceURL = "https://git.example.com/alice/../project.git" }},
		{name: "rejects ref injection", change: func(value *RemoteFetch) { value.SourceBranch = "main:refs/heads/owned" }},
		{name: "rejects option branch", change: func(value *RemoteFetch) { value.SourceBranch = "--upload-pack=evil" }},
		{name: "rejects destination slash", change: func(value *RemoteFetch) { value.Destination = "1/../../heads/main" }},
		{name: "rejects uppercase SHA", change: func(value *RemoteFetch) { value.ExpectedHead = strings.Repeat("A", 40) }},
		{name: "rejects abbreviated SHA", change: func(value *RemoteFetch) { value.ExpectedHead = "abc123" }},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			request := valid
			testCase.change(&request)
			_, err := validateRemoteFetch(request)
			if strings.HasPrefix(testCase.name, "accepts ") {
				if err != nil {
					t.Fatalf("validate request: %v", err)
				}
				return
			}
			if !errors.Is(err, ErrRemoteInput) {
				t.Fatalf("error = %v, want ErrRemoteInput", err)
			}
		})
	}
}

func TestPublicIPPolicy(t *testing.T) {
	t.Parallel()
	testCases := []struct {
		name    string
		address string
		allowed bool
	}{
		{name: "public IPv4", address: "8.8.8.8", allowed: true},
		{name: "public IPv6", address: "2606:4700:4700::1111", allowed: true},
		{name: "loopback", address: "127.0.0.1"},
		{name: "private", address: "10.0.0.1"},
		{name: "link local", address: "169.254.1.1"},
		{name: "carrier NAT", address: "100.64.0.1"},
		{name: "benchmark", address: "198.18.0.1"},
		{name: "documentation IPv4", address: "203.0.113.1"},
		{name: "documentation IPv6", address: "2001:db8::1"},
		{name: "multicast", address: "224.0.0.1"},
		{name: "unspecified", address: "::"},
		{name: "unique local", address: "fd00::1"},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			if got := isPublicIP(net.ParseIP(testCase.address)); got != testCase.allowed {
				t.Fatalf("isPublicIP(%s) = %t, want %t", testCase.address, got, testCase.allowed)
			}
		})
	}
}

func TestFetchRemoteRejectsUnsafeDNSAnswers(t *testing.T) {
	t.Parallel()
	testCases := []struct {
		name      string
		addresses []net.IP
	}{
		{name: "private only", addresses: []net.IP{net.ParseIP("10.0.0.1")}},
		{name: "mixed public and private", addresses: []net.IP{net.ParseIP("8.8.8.8"), net.ParseIP("127.0.0.1")}},
		{name: "documentation", addresses: []net.IP{net.ParseIP("2001:db8::1")}},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			service := NewServiceWithResolver(NewRunner("git"), nil, staticResolver{addresses: testCase.addresses}, nil)
			err := service.FetchRemote(context.Background(), repository.ID{}, RemoteFetch{
				SourceURL: "https://git.example.com/alice/project.git", SourceBranch: "main",
				ExpectedHead: strings.Repeat("a", 40), Destination: "1",
			})
			if !errors.Is(err, ErrRemoteAddress) {
				t.Fatalf("error = %v, want ErrRemoteAddress", err)
			}
		})
	}
}

func TestFetchRemoteWithRealGit(t *testing.T) {
	t.Parallel()
	testCases := []struct {
		name     string
		mismatch bool
	}{
		{name: "promotes matching commit"},
		{name: "cleans quarantine on mismatch", mismatch: true},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			fixture := newRemoteFixture(t)
			expected := fixture.heads["main"]
			if testCase.mismatch {
				expected = strings.Repeat("a", 40)
			}
			err := fixture.service.FetchRemote(context.Background(), fixture.id, RemoteFetch{
				SourceURL: fixture.url, SourceBranch: "main", ExpectedHead: expected, Destination: "42",
			})
			if testCase.mismatch {
				if !errors.Is(err, ErrHeadMismatch) {
					t.Fatalf("error = %v, want ErrHeadMismatch", err)
				}
				if got := fixture.ref("refs/adenosine/pull/42/head"); got != "" {
					t.Fatalf("destination = %s, want absent", got)
				}
			} else {
				if err != nil {
					t.Fatalf("fetch remote: %v", err)
				}
				if got := fixture.ref("refs/adenosine/pull/42/head"); got != expected {
					t.Fatalf("destination = %s, want %s", got, expected)
				}
			}
			if got := fixture.refs("refs/adenosine/quarantine/"); got != "" {
				t.Fatalf("quarantine refs remain: %q", got)
			}
		})
	}
}

func TestControlledHead(t *testing.T) {
	t.Parallel()
	testCases := []struct {
		name    string
		seed    bool
		wantSHA bool
	}{
		{name: "missing destination"},
		{name: "existing destination", seed: true, wantSHA: true},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			fixture := newRemoteFixture(t)
			if testCase.seed {
				if err := fixture.service.FetchRemote(context.Background(), fixture.id, RemoteFetch{SourceURL: fixture.url, SourceBranch: "main", ExpectedHead: fixture.heads["main"], Destination: "lookup"}); err != nil {
					t.Fatalf("seed controlled head: %v", err)
				}
			}
			got, err := fixture.service.ControlledHead(context.Background(), fixture.id, "lookup")
			if err != nil {
				t.Fatalf("ControlledHead() error = %v", err)
			}
			want := ""
			if testCase.wantSHA {
				want = fixture.heads["main"]
			}
			if got != want {
				t.Fatalf("ControlledHead() = %q, want %q", got, want)
			}
		})
	}
}

func TestFetchRemoteCASAllowsOnlyOneConcurrentRefresh(t *testing.T) {
	t.Parallel()
	testCases := []struct{ name string }{{name: "same prior head"}}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			fixture := newRemoteFixture(t)
			base := fixture.heads["main"]
			if err := fixture.service.FetchRemote(context.Background(), fixture.id, RemoteFetch{SourceURL: fixture.url, SourceBranch: "main", ExpectedHead: base, Destination: "7"}); err != nil {
				t.Fatalf("seed destination: %v", err)
			}

			type result struct {
				head string
				err  error
			}
			results := make(chan result, 2)
			var start sync.WaitGroup
			start.Add(1)
			for _, branch := range []string{"one", "two"} {
				branch := branch
				go func() {
					start.Wait()
					head := fixture.heads[branch]
					err := fixture.service.FetchRemote(context.Background(), fixture.id, RemoteFetch{SourceURL: fixture.url, SourceBranch: branch, ExpectedHead: head, Destination: "7", PriorHead: base})
					results <- result{head: head, err: err}
				}()
			}
			start.Done()
			successes := 0
			conflicts := 0
			winner := ""
			for range 2 {
				result := <-results
				if result.err == nil {
					successes++
					winner = result.head
				} else if errors.Is(result.err, ErrRefConflict) {
					conflicts++
				} else {
					t.Fatalf("refresh error = %v", result.err)
				}
			}
			if successes != 1 || conflicts != 1 {
				t.Fatalf("successes = %d, conflicts = %d, want 1 each", successes, conflicts)
			}
			if got := fixture.ref("refs/adenosine/pull/7/head"); got != winner {
				t.Fatalf("destination = %s, want winner %s", got, winner)
			}
			if got := fixture.refs("refs/adenosine/quarantine/"); got != "" {
				t.Fatalf("quarantine refs remain: %q", got)
			}
		})
	}
}

type remoteFixture struct {
	t       *testing.T
	binary  string
	path    string
	id      repository.ID
	service *Service
	url     string
	heads   map[string]string
}

func newRemoteFixture(t *testing.T) *remoteFixture {
	t.Helper()
	binary, err := exec.LookPath("git")
	if err != nil {
		t.Skip("git executable is unavailable")
	}
	root := t.TempDir()
	source := filepath.Join(root, "source.git")
	work := filepath.Join(root, "work")
	runGit(t, binary, "init", "--bare", source)
	runGit(t, binary, "init", "--initial-branch=main", work)
	runGit(t, binary, "-C", work, "config", "user.name", "Test")
	runGit(t, binary, "-C", work, "config", "user.email", "test@example.com")
	if err := os.WriteFile(filepath.Join(work, "file.txt"), []byte("base\n"), 0o600); err != nil {
		t.Fatalf("write worktree file: %v", err)
	}
	runGit(t, binary, "-C", work, "add", "file.txt")
	runGit(t, binary, "-C", work, "commit", "-m", "base")
	runGit(t, binary, "-C", work, "remote", "add", "origin", source)
	runGit(t, binary, "-C", work, "push", "origin", "main")
	heads := map[string]string{"main": gitOutput(t, binary, "-C", work, "rev-parse", "HEAD")}
	for _, branch := range []string{"one", "two"} {
		runGit(t, binary, "-C", work, "checkout", "-b", branch, "main")
		if err := os.WriteFile(filepath.Join(work, "file.txt"), []byte(branch+"\n"), 0o600); err != nil {
			t.Fatalf("write branch file: %v", err)
		}
		runGit(t, binary, "-C", work, "commit", "-am", branch)
		runGit(t, binary, "-C", work, "push", "origin", branch)
		heads[branch] = gitOutput(t, binary, "-C", work, "rev-parse", "HEAD")
	}
	runGit(t, binary, "--git-dir="+source, "update-server-info")

	server := httptest.NewTLSServer(http.FileServer(http.Dir(root)))
	t.Cleanup(server.Close)
	caInfo := filepath.Join(root, "server-ca.pem")
	certificate := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw})
	if err := os.WriteFile(caInfo, certificate, 0o600); err != nil {
		t.Fatalf("write test CA: %v", err)
	}
	paths, err := storage.NewFilesystem(filepath.Join(root, "repositories"))
	if err != nil {
		t.Fatalf("create filesystem storage: %v", err)
	}
	id := repository.ID(uuid.New())
	runner, err := NewRunnerWithHTTPCAInfo(binary, caInfo)
	if err != nil {
		t.Fatalf("create runner with test CA: %v", err)
	}
	service := NewServiceWithResolver(runner, paths, staticResolver{addresses: []net.IP{net.ParseIP("127.0.0.1")}}, func(net.IP) bool { return true })
	if err := service.Init(context.Background(), id, "main"); err != nil {
		t.Fatalf("initialize target: %v", err)
	}
	target, err := paths.Path(context.Background(), id)
	if err != nil {
		t.Fatalf("resolve target: %v", err)
	}
	return &remoteFixture{t: t, binary: binary, path: target, id: id, service: service, url: server.URL + "/source.git", heads: heads}
}

func (fixture *remoteFixture) ref(name string) string {
	fixture.t.Helper()
	command := exec.Command(fixture.binary, "--git-dir="+fixture.path, "rev-parse", "--verify", name)
	output, err := command.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(output))
}

func (fixture *remoteFixture) refs(prefix string) string {
	fixture.t.Helper()
	return gitOutput(fixture.t, fixture.binary, "--git-dir="+fixture.path, "for-each-ref", "--format=%(refname)", prefix)
}

func runGit(t *testing.T, binary string, args ...string) {
	t.Helper()
	command := exec.Command(binary, append([]string{"-c", "commit.gpgSign=false", "-c", "tag.gpgSign=false"}, args...)...)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("git %s: %v: %s", strings.Join(args, " "), err, output)
	}
}

func gitOutput(t *testing.T, binary string, args ...string) string {
	t.Helper()
	command := exec.Command(binary, append([]string{"-c", "commit.gpgSign=false", "-c", "tag.gpgSign=false"}, args...)...)
	output, err := command.Output()
	if err != nil {
		t.Fatalf("git %s: %v", strings.Join(args, " "), err)
	}
	return strings.TrimSpace(string(output))
}

func TestHardenedFetchArgsPinIPv4AndIPv6(t *testing.T) {
	t.Parallel()
	testCases := []struct {
		name string
		ip   net.IP
		want string
	}{
		{name: "IPv4", ip: net.ParseIP("8.8.8.8"), want: "git.example.com:8443:8.8.8.8"},
		{name: "IPv6", ip: net.ParseIP("2606:4700:4700::1111"), want: "git.example.com:8443:[2606:4700:4700::1111]"},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			endpoint, err := validateRemoteFetch(RemoteFetch{SourceURL: "https://git.example.com:8443/project.git", SourceBranch: "main", ExpectedHead: strings.Repeat("a", 40), Destination: "1"})
			if err != nil {
				t.Fatalf("validate URL: %v", err)
			}
			args := strings.Join(hardenedFetchArgs("/repo", endpoint, []net.IP{testCase.ip}, ""), "\n")
			if !strings.Contains(args, testCase.want) {
				t.Fatalf("arguments do not contain %q:\n%s", testCase.want, args)
			}
		})
	}
}

func TestRunnerUsesHardenedEnvironment(t *testing.T) {
	testCases := []struct{ name string }{{name: "drops inherited transport and config variables"}}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			root := t.TempDir()
			script := filepath.Join(root, "print-environment")
			if err := os.WriteFile(script, []byte("#!/bin/sh\nexec /usr/bin/env\n"), 0o700); err != nil {
				t.Fatalf("write helper: %v", err)
			}
			t.Setenv("HTTPS_PROXY", "http://127.0.0.1:1")
			t.Setenv("GIT_CONFIG_COUNT", "1")
			var output bytes.Buffer
			if err := NewRunner(script).run(context.Background(), []string{"status"}, nil, &output); err != nil {
				t.Fatalf("run helper: %v", err)
			}
			environment := output.String()
			for _, forbidden := range []string{"HTTPS_PROXY=", "GIT_CONFIG_COUNT="} {
				if strings.Contains(environment, forbidden) {
					t.Fatalf("environment contains %q: %s", forbidden, environment)
				}
			}
			for _, required := range []string{"GIT_CONFIG_NOSYSTEM=1", "GIT_TERMINAL_PROMPT=0", "LC_ALL=C"} {
				if !strings.Contains(environment, required) {
					t.Fatalf("environment does not contain %q: %s", required, environment)
				}
			}
		})
	}
}

func TestNewRunnerWithHTTPCAInfo(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	caInfo := filepath.Join(root, "root.pem")
	if err := os.WriteFile(caInfo, []byte("test CA"), 0o600); err != nil {
		t.Fatalf("write CA fixture: %v", err)
	}
	canonicalCAInfo, err := filepath.EvalSymlinks(caInfo)
	if err != nil {
		t.Fatalf("resolve CA fixture: %v", err)
	}
	testCases := []struct {
		name    string
		path    string
		wantErr bool
	}{
		{name: "accepts absolute regular file", path: caInfo},
		{name: "rejects relative path", path: "root.pem", wantErr: true},
		{name: "rejects unclean path", path: filepath.Join(root, ".", "..", filepath.Base(root), "root.pem") + string(filepath.Separator) + "..", wantErr: true},
		{name: "rejects directory", path: root, wantErr: true},
		{name: "rejects missing file", path: filepath.Join(root, "missing.pem"), wantErr: true},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			runner, err := NewRunnerWithHTTPCAInfo("git", testCase.path)
			if (err != nil) != testCase.wantErr {
				t.Fatalf("NewRunnerWithHTTPCAInfo() error = %v, wantErr %t", err, testCase.wantErr)
			}
			if !testCase.wantErr && runner.httpCAInfo != canonicalCAInfo {
				t.Fatalf("HTTP CA path = %q, want %q", runner.httpCAInfo, canonicalCAInfo)
			}
		})
	}
}
