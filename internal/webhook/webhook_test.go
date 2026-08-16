package webhook

import (
	"errors"
	"testing"
)

func TestValidateInput(t *testing.T) {
	t.Parallel()
	testCases := []struct {
		name    string
		url     string
		secret  string
		events  []string
		wantErr error
	}{
		{name: "public HTTPS", url: "https://hooks.example/adenosine", secret: "0123456789abcdef", events: []string{"push"}},
		{name: "external CI events", url: "https://hooks.example/adenosine", secret: "0123456789abcdef", events: []string{"check_run", "status"}},
		{name: "HTTP rejected", url: "http://hooks.example/adenosine", secret: "0123456789abcdef", events: []string{"push"}, wantErr: ErrValidation},
		{name: "loopback rejected", url: "https://127.0.0.1/hook", secret: "0123456789abcdef", events: []string{"push"}, wantErr: ErrValidation},
		{name: "localhost rejected", url: "https://localhost/hook", secret: "0123456789abcdef", events: []string{"push"}, wantErr: ErrValidation},
		{name: "weak secret", url: "https://hooks.example/adenosine", secret: "short", events: []string{"push"}, wantErr: ErrValidation},
		{name: "unknown event", url: "https://hooks.example/adenosine", secret: "0123456789abcdef", events: []string{"release"}, wantErr: ErrValidation},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			err := validateInput(testCase.url, testCase.secret, testCase.events)
			if !errors.Is(err, testCase.wantErr) {
				t.Fatalf("validateInput() error = %v, want %v", err, testCase.wantErr)
			}
		})
	}
}

func TestWebhookEventType(t *testing.T) {
	t.Parallel()
	testCases := []struct {
		name      string
		eventType string
		want      string
	}{
		{name: "status", eventType: "status.created", want: "status"},
		{name: "check run", eventType: "check_run.updated", want: "check_run"},
		{name: "unknown", eventType: "release.created"},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			if got := webhookEventType(testCase.eventType); got != testCase.want {
				t.Fatalf("webhookEventType(%q) = %q, want %q", testCase.eventType, got, testCase.want)
			}
		})
	}
}

func TestSecretEncryption(t *testing.T) {
	t.Parallel()
	testCases := []struct {
		name   string
		secret string
	}{
		{name: "round trip", secret: "0123456789abcdef"},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			service, err := NewService(nil, make([]byte, 32))
			if err != nil {
				t.Fatalf("NewService(): %v", err)
			}
			ciphertext, err := service.encrypt([]byte(testCase.secret))
			if err != nil {
				t.Fatalf("encrypt(): %v", err)
			}
			plaintext, err := service.decrypt(ciphertext)
			if err != nil {
				t.Fatalf("decrypt(): %v", err)
			}
			if string(plaintext) != testCase.secret {
				t.Fatalf("plaintext = %q", plaintext)
			}
		})
	}
}
