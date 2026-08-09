package config

import (
	"encoding/base64"
	"strings"
	"testing"
	"time"
)

func TestConfigValidate(t *testing.T) {
	t.Parallel()

	valid := Config{
		BaseURL:            "https://code.example.com",
		ListenAddr:         ":8080",
		DatabaseURL:        "postgres://postgres:postgres@localhost/adenosine",
		ElectricURL:        "https://electric.example.com/sync",
		ElectricSecret:     "electric-secret",
		RepositoryRoot:     "/var/lib/adenosine/repos",
		GitBinary:          "git",
		SSHListenAddr:      ":2222",
		SSHHost:            "code.example.com",
		SSHPort:            22,
		SSHHostKeyPath:     "/var/lib/adenosine/state/ssh_host_ed25519_key",
		OAuthStateKey:      make([]byte, 32),
		OAuthCredentialKey: make([]byte, 32),
		TapConsumer:        "tap:dev.adenosine:v1",
		TapAdminPassword:   "tap-secret",
		SessionLifetime:    time.Hour,
		ShutdownTimeout:    time.Second,
	}

	testCases := []struct {
		name   string
		mutate func(*Config)
	}{
		{name: "invalid base URL", mutate: func(c *Config) { c.BaseURL = "code.example.com" }},
		{name: "empty listen address", mutate: func(c *Config) { c.ListenAddr = "" }},
		{name: "empty database URL", mutate: func(c *Config) { c.DatabaseURL = "" }},
		{name: "Electric URL without secret", mutate: func(c *Config) { c.ElectricSecret = "" }},
		{name: "Electric secret without URL", mutate: func(c *Config) { c.ElectricURL = "" }},
		{name: "relative Electric URL", mutate: func(c *Config) { c.ElectricURL = "/electric" }},
		{name: "Electric URL userinfo", mutate: func(c *Config) { c.ElectricURL = "https://user@electric.example.com" }},
		{name: "Electric URL query", mutate: func(c *Config) { c.ElectricURL = "https://electric.example.com?secret=value" }},
		{name: "Electric URL fragment", mutate: func(c *Config) { c.ElectricURL = "https://electric.example.com#fragment" }},
		{name: "noncanonical Electric URL", mutate: func(c *Config) { c.ElectricURL = "https://ELECTRIC.example.com/sync/" }},
		{name: "Electric secret whitespace", mutate: func(c *Config) { c.ElectricSecret = " secret" }},
		{name: "empty repository root", mutate: func(c *Config) { c.RepositoryRoot = "" }},
		{name: "empty Git binary", mutate: func(c *Config) { c.GitBinary = "" }},
		{name: "empty SSH listen address", mutate: func(c *Config) { c.SSHListenAddr = "" }},
		{name: "empty SSH host", mutate: func(c *Config) { c.SSHHost = "" }},
		{name: "invalid SSH port", mutate: func(c *Config) { c.SSHPort = 0 }},
		{name: "empty SSH host key path", mutate: func(c *Config) { c.SSHHostKeyPath = "" }},
		{name: "invalid OAuth state key", mutate: func(c *Config) { c.OAuthStateKey = nil }},
		{name: "invalid OAuth credential key", mutate: func(c *Config) { c.OAuthCredentialKey = nil }},
		{name: "empty Tap admin password", mutate: func(c *Config) { c.TapAdminPassword = "" }},
		{name: "invalid session lifetime", mutate: func(c *Config) { c.SessionLifetime = 0 }},
		{name: "invalid shutdown timeout", mutate: func(c *Config) { c.ShutdownTimeout = 0 }},
	}

	if err := valid.Validate(); err != nil {
		t.Fatalf("valid config rejected: %v", err)
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			cfg := valid
			testCase.mutate(&cfg)
			if err := cfg.Validate(); err == nil {
				t.Fatal("expected validation error")
			}
		})
	}
}

func TestConfigValidateAllowsElectricDisabled(t *testing.T) {
	valid := Config{
		BaseURL: "https://code.example.com", ListenAddr: ":8080", DatabaseURL: "postgres://localhost/adenosine",
		RepositoryRoot: "/repos", GitBinary: "git", SSHListenAddr: ":2222", SSHHost: "code.example.com",
		SSHPort: 22, SSHHostKeyPath: "/host-key", OAuthStateKey: make([]byte, 32), OAuthCredentialKey: make([]byte, 32),
		SessionLifetime: time.Hour, ShutdownTimeout: time.Second,
	}
	if err := valid.Validate(); err != nil {
		t.Fatalf("disabled Electric rejected: %v", err)
	}
}

func TestLoadElectricConfiguration(t *testing.T) {
	key := base64.StdEncoding.EncodeToString(make([]byte, 32))
	t.Setenv("DATABASE_URL", "postgres://localhost/adenosine")
	t.Setenv("ADENOSINE_OAUTH_STATE_KEY", key)
	t.Setenv("ADENOSINE_OAUTH_CREDENTIAL_KEY", key)

	testCases := []struct {
		name, rawURL, secret, wantURL, wantError string
	}{
		{name: "disabled"},
		{name: "root canonicalized", rawURL: "https://ELECTRIC.example.com/", secret: "secret", wantURL: "https://electric.example.com"},
		{name: "path canonicalized", rawURL: "https://electric.example.com/base/", secret: "secret", wantURL: "https://electric.example.com/base"},
		{name: "missing secret", rawURL: "https://electric.example.com", wantError: "configured together"},
		{name: "query rejected", rawURL: "https://electric.example.com?x=1", secret: "secret", wantError: "without userinfo"},
	}
	for _, tt := range testCases {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("ADENOSINE_ELECTRIC_URL", tt.rawURL)
			t.Setenv("ADENOSINE_ELECTRIC_SECRET", tt.secret)
			cfg, err := load()
			if tt.wantError != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantError) {
					t.Fatalf("error = %v, want containing %q", err, tt.wantError)
				}
				return
			}
			if err != nil || cfg.ElectricURL != tt.wantURL || cfg.ElectricSecret != tt.secret {
				t.Fatalf("config = %#v, error = %v", cfg, err)
			}
		})
	}
}

func TestLoadOAuthEncryptionKeys(t *testing.T) {
	key := make([]byte, 32)
	for index := range key {
		key[index] = byte(index + 1)
	}
	encoded := base64.StdEncoding.EncodeToString(key)
	t.Setenv("DATABASE_URL", "postgres://localhost/adenosine")
	t.Setenv("ADENOSINE_OAUTH_STATE_KEY", encoded)
	t.Setenv("ADENOSINE_OAUTH_CREDENTIAL_KEY", encoded)
	cfg, err := load()
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if string(cfg.OAuthStateKey) != string(key) || string(cfg.OAuthCredentialKey) != string(key) {
		t.Fatal("OAuth encryption keys were not decoded")
	}

	t.Setenv("ADENOSINE_OAUTH_CREDENTIAL_KEY", "not-base64")
	_, err = load()
	if err == nil || !strings.Contains(err.Error(), "ADENOSINE_OAUTH_CREDENTIAL_KEY") {
		t.Fatalf("invalid credential key error = %v", err)
	}
}
