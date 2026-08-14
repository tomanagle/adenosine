// Package cli implements the public-REST-only Adenosine command-line client.
package cli

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"strings"

	generated "github.com/adenosine-dev/adenosine/api/generated/go"
)

const maxTokenBytes = 16 * 1024

// ErrUsage indicates invalid command-line input.
var ErrUsage = errors.New("invalid command usage")

type gitRunner interface {
	Run(context.Context, ...string) error
}

type execGit struct{}

func (execGit) Run(ctx context.Context, args ...string) error {
	command := exec.CommandContext(ctx, "git", args...)
	command.Stdin, command.Stdout, command.Stderr = os.Stdin, os.Stdout, os.Stderr
	return command.Run()
}

// Run executes one client command.
func Run(ctx context.Context, args []string, stdin io.Reader, stdout, stderr io.Writer) error {
	store, err := defaultCredentialStore()
	if err != nil {
		return err
	}
	runner := &runner{stdin: stdin, stdout: stdout, stderr: stderr, credentials: store, git: execGit{}, newClient: newAPIClient}
	return runner.run(ctx, args)
}

type runner struct {
	stdin       io.Reader
	stdout      io.Writer
	stderr      io.Writer
	credentials credentialStore
	git         gitRunner
	newClient   func(string, string) (*generated.ClientWithResponses, error)
}

func (runner *runner) run(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("%w: expected login, repo, issue, or pr", ErrUsage)
	}
	switch args[0] {
	case "login":
		return runner.login(ctx, args[1:])
	case "repo":
		return runner.repo(ctx, args[1:])
	case "issue":
		return runner.issue(ctx, args[1:])
	case "pr":
		return runner.pullRequest(ctx, args[1:])
	default:
		return fmt.Errorf("%w: unknown command %q", ErrUsage, args[0])
	}
}

func newAPIClient(host, token string) (*generated.ClientWithResponses, error) {
	return generated.NewClientWithResponses(host, generated.WithRequestEditorFn(func(_ context.Context, request *http.Request) error {
		if token != "" {
			request.Header.Set("Authorization", "Bearer "+token)
		}
		return nil
	}))
}

func (runner *runner) login(ctx context.Context, args []string) error {
	flags := flag.NewFlagSet("login", flag.ContinueOnError)
	flags.SetOutput(runner.stderr)
	host := flags.String("host", "", "Adenosine server URL")
	tokenStdin := flags.Bool("token-stdin", false, "read token from standard input")
	if err := flags.Parse(args); err != nil {
		return fmt.Errorf("%w: %v", ErrUsage, err)
	}
	if *host == "" || !*tokenStdin || flags.NArg() != 0 {
		return fmt.Errorf("%w: login requires --host and --token-stdin", ErrUsage)
	}
	normalizedHost, err := normalizeHost(*host)
	if err != nil {
		return err
	}
	tokenBytes, err := io.ReadAll(io.LimitReader(runner.stdin, maxTokenBytes+1))
	if err != nil {
		return fmt.Errorf("read token: %w", err)
	}
	if len(tokenBytes) > maxTokenBytes {
		return errors.New("token exceeds 16 KiB")
	}
	token := strings.TrimSpace(string(tokenBytes))
	if token == "" || strings.ContainsAny(token, " \t\r\n") {
		return errors.New("token is empty or malformed")
	}
	client, err := runner.newClient(normalizedHost, token)
	if err != nil {
		return err
	}
	response, err := client.GetCurrentIdentityWithResponse(ctx)
	if err != nil {
		return fmt.Errorf("validate credentials: %w", err)
	}
	if response.JSON200 == nil {
		return responseError(response.StatusCode(), response.Body)
	}
	config, err := runner.credentials.Load()
	if err != nil {
		return err
	}
	config.DefaultHost = normalizedHost
	config.Hosts[normalizedHost] = hostCredential{Token: token}
	if err := runner.credentials.Save(config); err != nil {
		return err
	}
	_, err = fmt.Fprintf(runner.stdout, "Logged in to %s as %s\n", normalizedHost, response.JSON200.Did)
	return err
}

func (runner *runner) client(flags *flag.FlagSet) (*generated.ClientWithResponses, bool, error) {
	hostFlag := flags.Lookup("host")
	jsonFlag := flags.Lookup("json")
	config, err := runner.credentials.Load()
	if err != nil {
		return nil, false, err
	}
	host := config.DefaultHost
	if hostFlag != nil && hostFlag.Value.String() != "" {
		host = hostFlag.Value.String()
	}
	if host == "" {
		return nil, false, errors.New("no host configured; run adenosine login")
	}
	host, err = normalizeHost(host)
	if err != nil {
		return nil, false, err
	}
	credential, ok := config.Hosts[host]
	if !ok {
		return nil, false, fmt.Errorf("no credentials for %s; run adenosine login", host)
	}
	client, err := runner.newClient(host, credential.Token)
	jsonOutput := jsonFlag != nil && jsonFlag.Value.String() == "true"
	return client, jsonOutput, err
}

func addCommonFlags(flags *flag.FlagSet) {
	flags.String("host", "", "Adenosine server URL")
	flags.Bool("json", false, "write stable JSON output")
}

func normalizeHost(value string) (string, error) {
	parsed, err := url.Parse(strings.TrimSpace(value))
	if err != nil || parsed.Scheme == "" || parsed.Host == "" || parsed.User != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return "", fmt.Errorf("invalid Adenosine host %q", value)
	}
	parsed.Path, parsed.RawPath, parsed.RawQuery, parsed.Fragment = strings.TrimSuffix(parsed.Path, "/"), "", "", ""
	return strings.TrimSuffix(parsed.String(), "/"), nil
}

func responseError(status int, body []byte) error {
	var value generated.ErrorResponse
	if json.Unmarshal(body, &value) == nil && value.Error.Code != "" {
		return fmt.Errorf("API %s: %s (request %s)", value.Error.Code, value.Error.Message, value.Error.RequestId)
	}
	return fmt.Errorf("API returned HTTP %d", status)
}

func writeJSON(output io.Writer, value any) error {
	encoder := json.NewEncoder(output)
	encoder.SetIndent("", "  ")
	return encoder.Encode(value)
}

func splitRepository(value string) (string, string, error) {
	parts := strings.Split(value, "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", fmt.Errorf("%w: repository must be OWNER/REPO", ErrUsage)
	}
	return parts[0], parts[1], nil
}
