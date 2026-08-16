// Package config loads and validates process configuration.
package config

import (
	"encoding/base64"
	"fmt"
	"net"
	"net/url"
	"os"
	"path"
	"strconv"
	"strings"
	"time"
)

// Config is immutable process configuration assembled at startup.
type Config struct {
	BaseURL                     string
	ListenAddr                  string
	DatabaseURL                 string
	ElectricURL                 string
	ElectricSecret              string
	RepositoryRoot              string
	ReleaseAssetRoot            string
	ReleaseAssetMaxBytes        int64
	ReleaseMaxBytes             int64
	RepositoryReleaseMaxBytes   int64
	GitBinary                   string
	SSHListenAddr               string
	SSHHost                     string
	SSHPort                     uint16
	SSHHostKeyPath              string
	OAuthStateKey               []byte
	OAuthCredentialKey          []byte
	TapConsumer                 string
	TapAdminPassword            string
	SessionLifetime             time.Duration
	ShutdownTimeout             time.Duration
	RepositoryDeletionRetention time.Duration
}

// Must loads valid process configuration or panics during startup.
func Must() Config {
	cfg, err := load()
	if err != nil {
		panic(err)
	}
	return cfg
}

func load() (Config, error) {
	cfg := Config{
		BaseURL:          valueOrDefault("ADENOSINE_BASE_URL", "http://127.0.0.1:8080"),
		ListenAddr:       listenAddrOrDefault("ADENOSINE_LISTEN_ADDR", ":8080"),
		DatabaseURL:      strings.TrimSpace(os.Getenv("DATABASE_URL")),
		ElectricURL:      strings.TrimSpace(os.Getenv("ADENOSINE_ELECTRIC_URL")),
		ElectricSecret:   strings.TrimSpace(os.Getenv("ADENOSINE_ELECTRIC_SECRET")),
		RepositoryRoot:   valueOrDefault("ADENOSINE_REPO_ROOT", "/var/lib/adenosine/repos"),
		ReleaseAssetRoot: valueOrDefault("ADENOSINE_RELEASE_ASSET_ROOT", "/var/lib/adenosine/state/release-assets"),
		GitBinary:        valueOrDefault("ADENOSINE_GIT_BINARY", "git"),
		SSHListenAddr:    listenAddrOrDefault("ADENOSINE_SSH_LISTEN_ADDR", ":2222"),
		SSHHost:          valueOrDefault("ADENOSINE_SSH_HOST", "localhost"),
		SSHPort:          2222,
		SSHHostKeyPath:   valueOrDefault("ADENOSINE_SSH_HOST_KEY_PATH", "/var/lib/adenosine/state/ssh_host_ed25519_key"),
		TapAdminPassword: strings.TrimSpace(os.Getenv("ADENOSINE_TAP_ADMIN_PASSWORD")),
		SessionLifetime:  30 * 24 * time.Hour,
		ShutdownTimeout:  10 * time.Second,
	}
	releaseAssetMaxBytes, err := positiveInt64OrDefault("ADENOSINE_RELEASE_ASSET_MAX_BYTES", 100*1024*1024)
	if err != nil {
		return Config{}, err
	}
	cfg.ReleaseAssetMaxBytes = releaseAssetMaxBytes
	releaseMaxBytes, err := positiveInt64OrDefault("ADENOSINE_RELEASE_MAX_BYTES", 1024*1024*1024)
	if err != nil {
		return Config{}, err
	}
	cfg.ReleaseMaxBytes = releaseMaxBytes
	repositoryReleaseMaxBytes, err := positiveInt64OrDefault("ADENOSINE_REPOSITORY_RELEASE_MAX_BYTES", 10*1024*1024*1024)
	if err != nil {
		return Config{}, err
	}
	cfg.RepositoryReleaseMaxBytes = repositoryReleaseMaxBytes
	repositoryDeletionRetention, err := durationOrDefault("ADENOSINE_REPOSITORY_DELETION_RETENTION", 7*24*time.Hour)
	if err != nil {
		return Config{}, err
	}
	cfg.RepositoryDeletionRetention = repositoryDeletionRetention
	cfg.TapConsumer = strings.TrimSpace(os.Getenv("ADENOSINE_TAP_CONSUMER"))
	if cfg.ElectricURL != "" {
		canonical, err := canonicalElectricURL(cfg.ElectricURL)
		if err != nil {
			return Config{}, err
		}
		cfg.ElectricURL = canonical
	}
	if value := strings.TrimSpace(os.Getenv("ADENOSINE_SSH_PORT")); value != "" {
		port, err := strconv.ParseUint(value, 10, 16)
		if err != nil || port == 0 {
			return Config{}, fmt.Errorf("ADENOSINE_SSH_PORT must be a valid TCP port")
		}
		cfg.SSHPort = uint16(port)
	}
	oauthStateKey, err := base64.StdEncoding.DecodeString(strings.TrimSpace(os.Getenv("ADENOSINE_OAUTH_STATE_KEY")))
	if err != nil {
		return Config{}, fmt.Errorf("ADENOSINE_OAUTH_STATE_KEY must be base64 encoded: %w", err)
	}
	cfg.OAuthStateKey = oauthStateKey
	oauthCredentialKey, err := base64.StdEncoding.DecodeString(strings.TrimSpace(os.Getenv("ADENOSINE_OAUTH_CREDENTIAL_KEY")))
	if err != nil {
		return Config{}, fmt.Errorf("ADENOSINE_OAUTH_CREDENTIAL_KEY must be base64 encoded: %w", err)
	}
	cfg.OAuthCredentialKey = oauthCredentialKey

	if err := cfg.Validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

// Validate checks startup configuration without performing I/O.
func (c Config) Validate() error {
	baseURL, err := url.ParseRequestURI(c.BaseURL)
	if err != nil || baseURL.Host == "" || (baseURL.Scheme != "http" && baseURL.Scheme != "https") {
		return fmt.Errorf("ADENOSINE_BASE_URL must be an absolute HTTP or HTTPS URL")
	}
	if !validTCPListenAddr(c.ListenAddr) {
		return fmt.Errorf("ADENOSINE_LISTEN_ADDR must be a valid TCP host/port address")
	}
	if strings.TrimSpace(c.DatabaseURL) == "" {
		return fmt.Errorf("DATABASE_URL must not be empty")
	}
	if (c.ElectricURL == "") != (c.ElectricSecret == "") {
		return fmt.Errorf("ADENOSINE_ELECTRIC_URL and ADENOSINE_ELECTRIC_SECRET must be configured together")
	}
	if c.ElectricURL != "" {
		canonical, err := canonicalElectricURL(c.ElectricURL)
		if err != nil {
			return err
		}
		if canonical != c.ElectricURL {
			return fmt.Errorf("ADENOSINE_ELECTRIC_URL must be canonical (for example %q)", canonical)
		}
		if strings.TrimSpace(c.ElectricSecret) != c.ElectricSecret {
			return fmt.Errorf("ADENOSINE_ELECTRIC_SECRET must not have surrounding whitespace")
		}
	}
	if strings.TrimSpace(c.RepositoryRoot) == "" {
		return fmt.Errorf("ADENOSINE_REPO_ROOT must not be empty")
	}
	if strings.TrimSpace(c.ReleaseAssetRoot) == "" {
		return fmt.Errorf("ADENOSINE_RELEASE_ASSET_ROOT must not be empty")
	}
	if c.ReleaseAssetMaxBytes <= 0 || c.ReleaseMaxBytes < c.ReleaseAssetMaxBytes || c.RepositoryReleaseMaxBytes < c.ReleaseMaxBytes {
		return fmt.Errorf("release asset byte limits must be positive and monotonically increasing")
	}
	if strings.TrimSpace(c.GitBinary) == "" {
		return fmt.Errorf("ADENOSINE_GIT_BINARY must not be empty")
	}
	if !validTCPListenAddr(c.SSHListenAddr) {
		return fmt.Errorf("ADENOSINE_SSH_LISTEN_ADDR must be a valid TCP host/port address")
	}
	if !validSSHHost(c.SSHHost) {
		return fmt.Errorf("ADENOSINE_SSH_HOST must be a host name or IP address")
	}
	if c.SSHPort == 0 {
		return fmt.Errorf("ADENOSINE_SSH_PORT must be a valid TCP port")
	}
	if strings.TrimSpace(c.SSHHostKeyPath) == "" {
		return fmt.Errorf("ADENOSINE_SSH_HOST_KEY_PATH must not be empty")
	}
	if len(c.OAuthStateKey) != 32 {
		return fmt.Errorf("ADENOSINE_OAUTH_STATE_KEY must decode to 32 bytes")
	}
	if len(c.OAuthCredentialKey) != 32 {
		return fmt.Errorf("ADENOSINE_OAUTH_CREDENTIAL_KEY must decode to 32 bytes")
	}
	if c.TapConsumer != "" && strings.TrimSpace(c.TapAdminPassword) == "" {
		return fmt.Errorf("ADENOSINE_TAP_ADMIN_PASSWORD must not be empty when ADENOSINE_TAP_CONSUMER is configured")
	}
	if c.SessionLifetime <= 0 {
		return fmt.Errorf("session lifetime must be positive")
	}
	if c.ShutdownTimeout <= 0 {
		return fmt.Errorf("shutdown timeout must be positive")
	}
	if c.RepositoryDeletionRetention < 0 {
		return fmt.Errorf("ADENOSINE_REPOSITORY_DELETION_RETENTION must not be negative")
	}
	return nil
}

func positiveInt64OrDefault(name string, fallback int64) (int64, error) {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return fallback, nil
	}
	parsed, err := strconv.ParseInt(value, 10, 64)
	if err != nil || parsed <= 0 {
		return 0, fmt.Errorf("%s must be a positive integer", name)
	}
	return parsed, nil
}

func durationOrDefault(name string, fallback time.Duration) (time.Duration, error) {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return fallback, nil
	}
	duration, err := time.ParseDuration(value)
	if err != nil {
		return 0, fmt.Errorf("%s must be a valid duration: %w", name, err)
	}
	return duration, nil
}

func canonicalElectricURL(value string) (string, error) {
	parsed, err := url.ParseRequestURI(value)
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") ||
		parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" || parsed.ForceQuery || parsed.RawPath != "" {
		return "", fmt.Errorf("ADENOSINE_ELECTRIC_URL must be an absolute HTTP or HTTPS URL without userinfo, query, or fragment")
	}
	parsed.Scheme = strings.ToLower(parsed.Scheme)
	parsed.Host = strings.ToLower(parsed.Host)
	if parsed.Path == "" || parsed.Path == "/" {
		parsed.Path = ""
	} else {
		parsed.Path = path.Clean(parsed.Path)
	}
	return parsed.String(), nil
}

func validSSHHost(host string) bool {
	if host == "" || strings.TrimSpace(host) != host || strings.ContainsAny(host, "/@") {
		return false
	}
	if net.ParseIP(host) != nil {
		return true
	}
	parsed, err := url.Parse("ssh://" + host)
	return err == nil && parsed.Hostname() == host && parsed.Port() == ""
}

func validTCPListenAddr(address string) bool {
	if address == "" || strings.TrimSpace(address) != address {
		return false
	}
	host, port, err := net.SplitHostPort(address)
	if err != nil || (host != "" && !validSSHHost(host)) {
		return false
	}
	value, err := strconv.ParseUint(port, 10, 16)
	return err == nil && value != 0
}

func listenAddrOrDefault(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}

func valueOrDefault(name, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(name)); value != "" {
		return value
	}
	return fallback
}
