// Package git provides safe access to the native Git executable.
package git

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
)

const defaultStderrLimit = 32 * 1024

// CommandError is a sanitized native Git process failure.
type CommandError struct {
	Operation string
	ExitCode  int
	Stderr    string
	Truncated bool
}

func (err *CommandError) Error() string {
	if err.Stderr == "" {
		return fmt.Sprintf("git %s failed with exit code %d", err.Operation, err.ExitCode)
	}
	return fmt.Sprintf("git %s failed with exit code %d: %s", err.Operation, err.ExitCode, err.Stderr)
}

// Runner centralizes native Git process execution.
type Runner struct {
	binary      string
	stderrLimit int
	httpCAInfo  string
}

// NewRunner constructs a native Git command runner.
func NewRunner(binary string) *Runner {
	return &Runner{binary: binary, stderrLimit: defaultStderrLimit}
}

// NewRunnerWithHTTPCAInfo constructs a runner that explicitly trusts one local
// CA bundle for HTTPS Git operations. This is intended for trusted composition
// boundaries; normal runners continue to use the system trust store.
func NewRunnerWithHTTPCAInfo(binary, caInfo string) (*Runner, error) {
	if caInfo == "" || strings.ContainsRune(caInfo, 0) || !filepath.IsAbs(caInfo) || filepath.Clean(caInfo) != caInfo {
		return nil, fmt.Errorf("HTTP CA path must be a clean absolute path")
	}
	canonical, err := filepath.EvalSymlinks(caInfo)
	if err != nil {
		return nil, fmt.Errorf("resolve HTTP CA path: %w", err)
	}
	info, err := os.Stat(canonical)
	if err != nil {
		return nil, fmt.Errorf("inspect HTTP CA path: %w", err)
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("HTTP CA path must identify a regular file")
	}
	runner := NewRunner(binary)
	runner.httpCAInfo = canonical
	return runner, nil
}

func (runner *Runner) run(ctx context.Context, args []string, input io.Reader, output io.Writer) error {
	return runner.runWithEnv(ctx, args, input, output, nil)
}

func (runner *Runner) runWithEnv(ctx context.Context, args []string, input io.Reader, output io.Writer, environment []string) error {
	operation := "unknown"
	if len(args) > 0 {
		operation = args[0]
	}
	ctx, span := otel.Tracer("github.com/adenosine-dev/adenosine/internal/git").Start(ctx, "git."+operation)
	defer span.End()
	span.SetAttributes(attribute.String("git.operation", operation))

	stderr := &limitedBuffer{limit: runner.stderrLimit}
	cmd := exec.CommandContext(ctx, runner.binary, args...)
	cmd.Stdin = input
	cmd.Stdout = output
	cmd.Stderr = stderr
	cmd.Env = []string{
		"GIT_CONFIG_NOSYSTEM=1",
		"GIT_CONFIG_GLOBAL=/dev/null",
		"GIT_CONFIG_SYSTEM=/dev/null",
		"GIT_TERMINAL_PROMPT=0",
		"GIT_ASKPASS=",
		"SSH_ASKPASS=",
		"LC_ALL=C",
	}
	cmd.Env = append(cmd.Env, environment...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return nil
		}
		err := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		if errors.Is(err, syscall.ESRCH) {
			return nil
		}
		return err
	}
	cmd.WaitDelay = 5 * time.Second

	if err := cmd.Run(); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "git command failed")
		if ctx.Err() != nil {
			return fmt.Errorf("git %s: %w", operation, ctx.Err())
		}
		exitCode := -1
		var exitError *exec.ExitError
		if errors.As(err, &exitError) {
			exitCode = exitError.ExitCode()
		}
		return &CommandError{
			Operation: operation,
			ExitCode:  exitCode,
			Stderr:    stderr.String(),
			Truncated: stderr.truncated,
		}
	}
	return nil
}

type limitedBuffer struct {
	buffer    bytes.Buffer
	limit     int
	truncated bool
}

func (buffer *limitedBuffer) Write(value []byte) (int, error) {
	remaining := buffer.limit - buffer.buffer.Len()
	if remaining > 0 {
		written := len(value)
		if written > remaining {
			written = remaining
		}
		_, _ = buffer.buffer.Write(value[:written])
	}
	if len(value) > remaining {
		buffer.truncated = true
	}
	return len(value), nil
}

func (buffer *limitedBuffer) String() string {
	return buffer.buffer.String()
}
