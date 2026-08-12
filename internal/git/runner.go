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
	"go.opentelemetry.io/otel/metric"
)

const (
	defaultStderrLimit     = 32 * 1024
	defaultMaxConcurrency  = 16
	defaultAdmissionWait   = 5 * time.Second
	defaultCommandDuration = 30 * time.Minute
)

type transportContextKey struct{}

// WithTransport marks a Git operation with a bounded ingress transport.
func WithTransport(ctx context.Context, transport string) context.Context {
	if transport != "http" && transport != "ssh" && transport != "internal" {
		transport = "internal"
	}
	return context.WithValue(ctx, transportContextKey{}, transport)
}

// CommandError is a sanitized native Git process failure.
type CommandError struct {
	Operation string
	ExitCode  int
	Stderr    string
	Truncated bool
}

// FailureAttrs returns bounded log details without exposing Git stderr.
func FailureAttrs(err error) []any {
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		return []any{"error_class", "timeout"}
	case errors.Is(err, context.Canceled):
		return []any{"error_class", "canceled"}
	}
	var commandError *CommandError
	if errors.As(err, &commandError) {
		return []any{"error_class", "native_git", "exit_code", commandError.ExitCode, "stderr_truncated", commandError.Truncated}
	}
	return []any{"error_class", "internal"}
}

func (err *CommandError) Error() string {
	if err.Stderr == "" {
		return fmt.Sprintf("git %s failed with exit code %d", err.Operation, err.ExitCode)
	}
	return fmt.Sprintf("git %s failed with exit code %d: %s", err.Operation, err.ExitCode, err.Stderr)
}

// Runner centralizes native Git process execution.
type Runner struct {
	binary        string
	stderrLimit   int
	httpCAInfo    string
	semaphore     chan struct{}
	admissionWait time.Duration
	maxDuration   time.Duration
	active        metric.Int64UpDownCounter
	duration      metric.Float64Histogram
	commands      metric.Int64Counter
	bytes         metric.Int64Counter
}

// NewRunner constructs a native Git command runner.
func NewRunner(binary string) *Runner {
	meter := otel.Meter("github.com/adenosine-dev/adenosine/internal/git")
	active, _ := meter.Int64UpDownCounter("adenosine.git.commands.active")
	duration, _ := meter.Float64Histogram("adenosine.git.command.duration", metric.WithUnit("s"))
	commands, _ := meter.Int64Counter("adenosine.git.commands")
	bytes, _ := meter.Int64Counter("adenosine.git.bytes", metric.WithUnit("By"))
	return &Runner{
		binary: binary, stderrLimit: defaultStderrLimit,
		semaphore: make(chan struct{}, defaultMaxConcurrency), admissionWait: defaultAdmissionWait, maxDuration: defaultCommandDuration,
		active: active, duration: duration, commands: commands, bytes: bytes,
	}
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
	ctx, cancel := context.WithTimeout(ctx, runner.maxDuration)
	defer cancel()
	operation := commandOperation(args)
	transport, _ := ctx.Value(transportContextKey{}).(string)
	if transport == "" {
		transport = "internal"
	}
	attrs := []attribute.KeyValue{attribute.String("git.operation", operation), attribute.String("git.transport", transport)}
	admission := time.NewTimer(runner.admissionWait)
	defer admission.Stop()
	select {
	case runner.semaphore <- struct{}{}:
		defer func() { <-runner.semaphore }()
	case <-admission.C:
		runner.commands.Add(ctx, 1, metric.WithAttributes(append(attrs, attribute.String("git.result", "admission_timeout"))...))
		return fmt.Errorf("wait for git %s execution slot: %w", operation, context.DeadlineExceeded)
	case <-ctx.Done():
		result := "canceled"
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			result = "timeout"
		}
		runner.commands.Add(ctx, 1, metric.WithAttributes(append(attrs, attribute.String("git.result", result))...))
		return fmt.Errorf("wait for git %s execution slot: %w", operation, ctx.Err())
	}

	ctx, span := otel.Tracer("github.com/adenosine-dev/adenosine/internal/git").Start(ctx, "git."+operation)
	defer span.End()
	span.SetAttributes(attrs...)
	started := time.Now()
	runner.active.Add(ctx, 1, metric.WithAttributes(attrs...))
	defer runner.active.Add(ctx, -1, metric.WithAttributes(attrs...))

	countedInput := &countingReader{reader: input}
	countedOutput := &metricCountingWriter{writer: output}
	if input != nil {
		input = countedInput
	}
	if output != nil {
		output = countedOutput
	}
	result := "success"
	defer func() {
		resultAttrs := append(attrs, attribute.String("git.result", result))
		runner.duration.Record(ctx, time.Since(started).Seconds(), metric.WithAttributes(resultAttrs...))
		runner.commands.Add(ctx, 1, metric.WithAttributes(resultAttrs...))
		if countedInput.count > 0 {
			runner.bytes.Add(ctx, countedInput.count, metric.WithAttributes(append(attrs, attribute.String("git.direction", "in"))...))
		}
		if countedOutput.count > 0 {
			runner.bytes.Add(ctx, countedOutput.count, metric.WithAttributes(append(attrs, attribute.String("git.direction", "out"))...))
		}
	}()

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
		result = "failure"
		span.RecordError(err)
		span.SetStatus(codes.Error, "git command failed")
		if ctx.Err() != nil {
			if errors.Is(ctx.Err(), context.DeadlineExceeded) {
				result = "timeout"
			} else {
				result = "canceled"
			}
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

func commandOperation(args []string) string {
	for index := 0; index < len(args); index++ {
		value := args[index]
		if value == "-c" {
			index++
			continue
		}
		if strings.HasPrefix(value, "--git-dir=") || strings.HasPrefix(value, "--work-tree=") || value == "--bare" {
			continue
		}
		switch value {
		case "archive", "branch", "cat-file", "check-ref-format", "clone", "commit-tree", "diff", "fetch", "for-each-ref", "init", "log", "ls-tree", "merge-base", "merge-tree", "read-tree", "receive-pack", "rev-list", "rev-parse", "show", "symbolic-ref", "update-ref", "upload-pack", "write-tree":
			return value
		default:
			return "other"
		}
	}
	return "other"
}

type countingReader struct {
	reader io.Reader
	count  int64
}

func (reader *countingReader) Read(value []byte) (int, error) {
	written, err := reader.reader.Read(value)
	reader.count += int64(written)
	return written, err
}

type metricCountingWriter struct {
	writer io.Writer
	count  int64
}

func (writer *metricCountingWriter) Write(value []byte) (int, error) {
	written, err := writer.writer.Write(value)
	writer.count += int64(written)
	return written, err
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
