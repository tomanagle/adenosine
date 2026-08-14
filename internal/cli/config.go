package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

type hostCredential struct {
	Token string `json:"token"`
}

type credentialFile struct {
	DefaultHost string                    `json:"default_host"`
	Hosts       map[string]hostCredential `json:"hosts"`
}

type credentialStore interface {
	Load() (credentialFile, error)
	Save(credentialFile) error
}

type fileCredentialStore struct{ path string }

func defaultCredentialStore() (fileCredentialStore, error) {
	directory := os.Getenv("ADENOSINE_CONFIG_DIR")
	if directory == "" {
		value, err := os.UserConfigDir()
		if err != nil {
			return fileCredentialStore{}, fmt.Errorf("find user config directory: %w", err)
		}
		directory = filepath.Join(value, "adenosine")
	}
	return fileCredentialStore{path: filepath.Join(directory, "hosts.json")}, nil
}

func (store fileCredentialStore) Load() (credentialFile, error) {
	data, err := os.ReadFile(store.path)
	if errors.Is(err, os.ErrNotExist) {
		return credentialFile{Hosts: map[string]hostCredential{}}, nil
	}
	if err != nil {
		return credentialFile{}, fmt.Errorf("read credentials: %w", err)
	}
	var config credentialFile
	if err := json.Unmarshal(data, &config); err != nil {
		return credentialFile{}, fmt.Errorf("decode credentials: %w", err)
	}
	if config.Hosts == nil {
		config.Hosts = map[string]hostCredential{}
	}
	return config, nil
}

func (store fileCredentialStore) Save(config credentialFile) error {
	directory := filepath.Dir(store.path)
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return fmt.Errorf("create credential directory: %w", err)
	}
	if err := os.Chmod(directory, 0o700); err != nil {
		return fmt.Errorf("secure credential directory: %w", err)
	}
	data, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return fmt.Errorf("encode credentials: %w", err)
	}
	temporary, err := os.CreateTemp(directory, ".hosts-*")
	if err != nil {
		return fmt.Errorf("create credential file: %w", err)
	}
	temporaryPath := temporary.Name()
	defer func() { _ = os.Remove(temporaryPath) }()
	if err := temporary.Chmod(0o600); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("secure credential file: %w", err)
	}
	if _, err := temporary.Write(append(data, '\n')); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("write credentials: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close credentials: %w", err)
	}
	if err := os.Rename(temporaryPath, store.path); err != nil {
		return fmt.Errorf("install credentials: %w", err)
	}
	return nil
}
