package gitssh

import (
	"fmt"
	"os"

	"golang.org/x/crypto/ssh"
)

// MustHostSigner loads a required SSH host key or panics during startup.
func MustHostSigner(path string) ssh.Signer {
	signer, err := loadHostSigner(path)
	if err != nil {
		panic(err)
	}
	return signer
}

func loadHostSigner(path string) (ssh.Signer, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("stat SSH host key: %w", err)
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("SSH host key must be a regular file")
	}
	if info.Mode().Perm()&0o077 != 0 {
		return nil, fmt.Errorf("SSH host key permissions must not grant group or other access")
	}
	contents, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read SSH host key: %w", err)
	}
	signer, err := ssh.ParsePrivateKey(contents)
	if err != nil {
		return nil, fmt.Errorf("parse SSH host key: %w", err)
	}
	return signer, nil
}
