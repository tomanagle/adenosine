package git

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"go.opentelemetry.io/otel"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

func TestRunnerBoundsExecution(t *testing.T) {
	testCases := []struct {
		name          string
		occupySlot    bool
		admissionWait time.Duration
		maxDuration   time.Duration
		wantContext   error
		wantElapsed   time.Duration
	}{
		{name: "bounds admission without caller deadline", occupySlot: true, admissionWait: 25 * time.Millisecond, maxDuration: time.Second, wantContext: context.DeadlineExceeded, wantElapsed: 25 * time.Millisecond},
		{name: "total deadline includes admission", occupySlot: true, admissionWait: time.Second, maxDuration: 25 * time.Millisecond, wantContext: context.DeadlineExceeded, wantElapsed: 25 * time.Millisecond},
		{name: "terminates command at duration limit", admissionWait: time.Second, maxDuration: 25 * time.Millisecond, wantContext: context.DeadlineExceeded, wantElapsed: 25 * time.Millisecond},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			runner := NewRunner("/bin/sh")
			runner.semaphore = make(chan struct{}, 1)
			runner.admissionWait = testCase.admissionWait
			runner.maxDuration = testCase.maxDuration
			if testCase.occupySlot {
				runner.semaphore <- struct{}{}
				defer func() { <-runner.semaphore }()
			}
			started := time.Now()
			err := runner.run(context.Background(), []string{"-c", "exec /bin/sleep 5"}, nil, nil)
			if !errors.Is(err, testCase.wantContext) {
				t.Fatalf("run() error = %v, want %v", err, testCase.wantContext)
			}
			if elapsed := time.Since(started); elapsed < testCase.wantElapsed || elapsed > 2*time.Second {
				t.Fatalf("run() elapsed = %s, want bounded near %s", elapsed, testCase.wantElapsed)
			}
			if got := len(runner.semaphore); got != btoi(testCase.occupySlot) {
				t.Fatalf("semaphore occupancy = %d after run", got)
			}
		})
	}
}

func btoi(value bool) int {
	if value {
		return 1
	}
	return 0
}

func TestFailureAttrsRedactStderr(t *testing.T) {
	testCases := []struct {
		name      string
		err       error
		wantClass string
	}{
		{name: "native command", err: fmt.Errorf("receive pack: %w", &CommandError{Operation: "receive-pack", ExitCode: 1, Stderr: "credential-secret", Truncated: true}), wantClass: "native_git"},
		{name: "timeout", err: context.DeadlineExceeded, wantClass: "timeout"},
		{name: "internal", err: errors.New("credential-secret"), wantClass: "internal"},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			attrs := fmt.Sprint(FailureAttrs(testCase.err))
			if !strings.Contains(attrs, testCase.wantClass) || strings.Contains(attrs, "credential-secret") {
				t.Fatalf("FailureAttrs() = %s", attrs)
			}
		})
	}
}

func TestRunnerMetrics(t *testing.T) {
	testCases := []struct {
		name string
		want []string
	}{
		{name: "records bounded command telemetry", want: []string{
			"adenosine.git.commands.active", "adenosine.git.commands", "adenosine.git.command.duration",
		}},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			reader := sdkmetric.NewManualReader()
			provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
			previous := otel.GetMeterProvider()
			otel.SetMeterProvider(provider)
			defer func() {
				otel.SetMeterProvider(previous)
				_ = provider.Shutdown(context.Background())
			}()
			runner := NewRunner("/usr/bin/true")
			if err := runner.run(WithTransport(context.Background(), "http"), []string{"status"}, nil, nil); err != nil {
				t.Fatalf("run() error = %v", err)
			}
			var metrics metricdata.ResourceMetrics
			if err := reader.Collect(context.Background(), &metrics); err != nil {
				t.Fatalf("collect metrics: %v", err)
			}
			names := map[string]bool{}
			for _, scope := range metrics.ScopeMetrics {
				for _, value := range scope.Metrics {
					names[value.Name] = true
				}
			}
			for _, name := range testCase.want {
				if !names[name] {
					t.Errorf("metric %q was not recorded", name)
				}
			}
		})
	}
}

func TestCommandOperationIsBounded(t *testing.T) {
	testCases := []struct {
		name string
		args []string
		want string
	}{
		{name: "pack after configuration", args: []string{"-c", "transfer.hideRefs=refs/adenosine", "receive-pack", "/repo"}, want: "receive-pack"},
		{name: "unknown command", args: []string{"user-controlled-value"}, want: "other"},
		{name: "git directory option", args: []string{"--git-dir=/repo", "for-each-ref"}, want: "for-each-ref"},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			if got := commandOperation(testCase.args); got != testCase.want {
				t.Fatalf("commandOperation() = %q, want %q", got, testCase.want)
			}
		})
	}
}
