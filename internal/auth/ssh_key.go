package auth

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"golang.org/x/crypto/ssh"
)

// SSHKey is persisted public-key credential metadata.
type SSHKey struct {
	ID          uuid.UUID
	AccountDID  string
	Name        string
	Algorithm   string
	PublicKey   string
	Fingerprint string
	CreatedAt   time.Time
	LastUsedAt  *time.Time
	RevokedAt   *time.Time
}

// CreateSSHKeyInput describes a user public key in OpenSSH authorized-key format.
type CreateSSHKeyInput struct {
	AccountDID    string
	Name          string
	AuthorizedKey string
}

type sshKeyStore interface {
	GetActiveSSHKeyByFingerprint(context.Context, string) (SSHKey, error)
	TouchSSHKey(context.Context, uuid.UUID, time.Time) error
}

type sshKeyServiceStore interface {
	CreateSSHKey(context.Context, SSHKey) (SSHKey, error)
	ListSSHKeys(context.Context, string) ([]SSHKey, error)
	RevokeSSHKey(context.Context, string, uuid.UUID, time.Time) error
}

// SSHKeyService creates user SSH public-key credentials.
type SSHKeyService struct {
	store sshKeyServiceStore
	clock tokenClock
	ids   tokenIDGenerator
}

// NewSSHKeyService constructs a user SSH public-key service.
func NewSSHKeyService(store sshKeyServiceStore, clock tokenClock, ids tokenIDGenerator) *SSHKeyService {
	return &SSHKeyService{store: store, clock: clock, ids: ids}
}

// CreateSSHKey validates, canonicalizes, and stores public key material.
func (service *SSHKeyService) CreateSSHKey(ctx context.Context, input CreateSSHKeyInput) (SSHKey, error) {
	input.AccountDID = strings.TrimSpace(input.AccountDID)
	input.Name = strings.TrimSpace(input.Name)
	input.AuthorizedKey = strings.TrimSpace(input.AuthorizedKey)
	if err := input.validate(); err != nil {
		return SSHKey{}, fmt.Errorf("%w: %v", ErrValidation, err)
	}
	publicKey, canonical, err := parseUserPublicKey(input.AuthorizedKey)
	if err != nil {
		return SSHKey{}, fmt.Errorf("%w: %v", ErrValidation, err)
	}
	id, err := service.ids.New()
	if err != nil {
		return SSHKey{}, fmt.Errorf("generate SSH key ID: %w", err)
	}
	key := SSHKey{
		ID:          id,
		AccountDID:  input.AccountDID,
		Name:        input.Name,
		Algorithm:   publicKey.Type(),
		PublicKey:   canonical,
		Fingerprint: ssh.FingerprintSHA256(publicKey),
		CreatedAt:   service.clock.Now().UTC(),
	}
	key, err = service.store.CreateSSHKey(ctx, key)
	if err != nil {
		return SSHKey{}, fmt.Errorf("store SSH key: %w", err)
	}
	return key, nil
}

// ListSSHKeys returns active SSH credentials owned by an account.
func (service *SSHKeyService) ListSSHKeys(ctx context.Context, accountDID string) ([]SSHKey, error) {
	keys, err := service.store.ListSSHKeys(ctx, strings.TrimSpace(accountDID))
	if err != nil {
		return nil, fmt.Errorf("list SSH keys: %w", err)
	}
	return keys, nil
}

// RevokeSSHKey soft-revokes an active SSH credential owned by an account.
func (service *SSHKeyService) RevokeSSHKey(ctx context.Context, accountDID string, id uuid.UUID) error {
	if err := service.store.RevokeSSHKey(ctx, strings.TrimSpace(accountDID), id, service.clock.Now().UTC()); err != nil {
		return fmt.Errorf("revoke SSH key: %w", err)
	}
	return nil
}

func (input CreateSSHKeyInput) validate() error {
	if strings.TrimSpace(input.AccountDID) == "" {
		return fmt.Errorf("account DID must not be empty")
	}
	if strings.TrimSpace(input.Name) == "" || len(input.Name) > 255 {
		return fmt.Errorf("name must contain between 1 and 255 characters")
	}
	if strings.TrimSpace(input.AuthorizedKey) == "" {
		return fmt.Errorf("authorized key must not be empty")
	}
	return nil
}

func parseUserPublicKey(line string) (ssh.PublicKey, string, error) {
	publicKey, _, _, rest, err := ssh.ParseAuthorizedKey([]byte(line))
	if err != nil {
		return nil, "", fmt.Errorf("invalid authorized-key line: %w", err)
	}
	if len(bytes.TrimSpace(rest)) != 0 {
		return nil, "", fmt.Errorf("authorized-key input must contain exactly one key")
	}
	if _, certificate := publicKey.(*ssh.Certificate); certificate {
		return nil, "", fmt.Errorf("SSH certificates are not supported")
	}
	if publicKey.Type() == ssh.KeyAlgoDSA {
		return nil, "", fmt.Errorf("legacy DSA keys are not supported")
	}
	canonical := strings.TrimSpace(string(ssh.MarshalAuthorizedKey(publicKey)))
	return publicKey, canonical, nil
}

// SSHKeyAuthenticator authenticates presented SSH public keys.
type SSHKeyAuthenticator struct {
	store sshKeyStore
	clock tokenClock
}

// SSHIdentity identifies the account and credential associated with a verified public key.
type SSHIdentity struct {
	KeyID      uuid.UUID
	AccountDID string
}

// NewSSHKeyAuthenticator constructs a public-key authenticator.
func NewSSHKeyAuthenticator(store sshKeyStore, clock tokenClock) *SSHKeyAuthenticator {
	return &SSHKeyAuthenticator{store: store, clock: clock}
}

// Authenticate resolves an active credential and returns its account DID.
func (authenticator *SSHKeyAuthenticator) Authenticate(ctx context.Context, presented ssh.PublicKey) (string, error) {
	identity, err := authenticator.Lookup(ctx, presented)
	if err != nil {
		return "", err
	}
	if err := authenticator.RecordUse(ctx, identity.KeyID); err != nil {
		return "", err
	}
	return identity.AccountDID, nil
}

// Lookup resolves a presented public key without recording it as successfully used.
func (authenticator *SSHKeyAuthenticator) Lookup(ctx context.Context, presented ssh.PublicKey) (SSHIdentity, error) {
	if presented == nil {
		return SSHIdentity{}, ErrUnauthorized
	}
	fingerprint := ssh.FingerprintSHA256(presented)
	key, err := authenticator.store.GetActiveSSHKeyByFingerprint(ctx, fingerprint)
	if err != nil {
		if errors.Is(err, ErrUnauthorized) {
			return SSHIdentity{}, ErrUnauthorized
		}
		return SSHIdentity{}, fmt.Errorf("get SSH key credential: %w", err)
	}
	presentedCanonical := bytes.TrimSpace(ssh.MarshalAuthorizedKey(presented))
	if !bytes.Equal([]byte(key.PublicKey), presentedCanonical) {
		return SSHIdentity{}, ErrUnauthorized
	}
	return SSHIdentity{KeyID: key.ID, AccountDID: key.AccountDID}, nil
}

// RecordUse updates usage metadata after the client proves possession of its private key.
func (authenticator *SSHKeyAuthenticator) RecordUse(ctx context.Context, keyID uuid.UUID) error {
	if err := authenticator.store.TouchSSHKey(ctx, keyID, authenticator.clock.Now().UTC()); err != nil {
		return fmt.Errorf("record SSH key use: %w", err)
	}
	return nil
}
