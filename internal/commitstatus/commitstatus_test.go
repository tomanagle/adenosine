package commitstatus

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func TestNormalizeStatusInput(t *testing.T) {
	t.Parallel()
	validSHA := strings.Repeat("a", 40)
	testCases := []struct {
		name    string
		sha     string
		creator string
		input   StatusInput
		wantErr error
	}{
		{name: "valid", sha: validSHA, creator: "did:plc:ci", input: StatusInput{Context: "ci/test", State: StateSuccess, ExternalID: "build-1"}},
		{name: "invalid SHA", sha: "main", creator: "did:plc:ci", input: StatusInput{Context: "ci/test", State: StateSuccess, ExternalID: "build-1"}, wantErr: ErrValidation},
		{name: "missing context", sha: validSHA, creator: "did:plc:ci", input: StatusInput{State: StateSuccess, ExternalID: "build-1"}, wantErr: ErrValidation},
		{name: "invalid state", sha: validSHA, creator: "did:plc:ci", input: StatusInput{Context: "ci/test", State: "passed", ExternalID: "build-1"}, wantErr: ErrValidation},
		{name: "missing external ID", sha: validSHA, creator: "did:plc:ci", input: StatusInput{Context: "ci/test", State: StateSuccess}, wantErr: ErrValidation},
		{name: "non HTTPS URL", sha: validSHA, creator: "did:plc:ci", input: StatusInput{Context: "ci/test", State: StateSuccess, ExternalID: "build-1", TargetURL: stringPointer("http://ci.example/build/1")}, wantErr: ErrValidation},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			_, err := normalizeStatusInput(testCase.sha, testCase.creator, testCase.input)
			if !errors.Is(err, testCase.wantErr) {
				t.Fatalf("normalizeStatusInput() error = %v, want %v", err, testCase.wantErr)
			}
		})
	}
}

func TestNormalizeCheckLifecycle(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 8, 14, 0, 0, 0, 0, time.UTC)
	success := ConclusionSuccess
	testCases := []struct {
		name        string
		from        CheckStatus
		input       CheckRunUpdate
		wantErr     error
		wantStarted bool
		wantDone    bool
	}{
		{name: "queued to running", from: CheckQueued, input: CheckRunUpdate{ExpectedVersion: 1, Status: CheckInProgress}, wantStarted: true},
		{name: "queued to completed", from: CheckQueued, input: CheckRunUpdate{ExpectedVersion: 1, Status: CheckCompleted, Conclusion: &success}, wantStarted: true, wantDone: true},
		{name: "completed cannot restart", from: CheckCompleted, input: CheckRunUpdate{ExpectedVersion: 2, Status: CheckInProgress}, wantErr: ErrConflict},
		{name: "completed needs conclusion", from: CheckInProgress, input: CheckRunUpdate{ExpectedVersion: 2, Status: CheckCompleted}, wantErr: ErrValidation},
		{name: "version required", from: CheckQueued, input: CheckRunUpdate{Status: CheckInProgress}, wantErr: ErrValidation},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			current := CheckRun{Status: testCase.from}
			value, err := normalizeCheckRunUpdate(current, testCase.input, now)
			if !errors.Is(err, testCase.wantErr) {
				t.Fatalf("normalizeCheckRunUpdate() error = %v, want %v", err, testCase.wantErr)
			}
			if err == nil && ((value.StartedAt != nil) != testCase.wantStarted || (value.CompletedAt != nil) != testCase.wantDone) {
				t.Fatalf("timestamps = started:%v completed:%v", value.StartedAt, value.CompletedAt)
			}
		})
	}
}

func TestCombineState(t *testing.T) {
	t.Parallel()
	testCases := []struct {
		name      string
		states    []State
		wantState State
	}{
		{name: "all success", states: []State{StateSuccess, StateSuccess}, wantState: StateSuccess},
		{name: "pending dominates success", states: []State{StateSuccess, StatePending}, wantState: StatePending},
		{name: "failure dominates pending", states: []State{StatePending, StateFailure}, wantState: StateFailure},
		{name: "error dominates failure", states: []State{StateFailure, StateError}, wantState: StateError},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			state := StateSuccess
			for _, candidate := range testCase.states {
				state = combineState(state, candidate)
			}
			if state != testCase.wantState {
				t.Fatalf("combineState() = %q, want %q", state, testCase.wantState)
			}
		})
	}
}

func stringPointer(value string) *string { return &value }
