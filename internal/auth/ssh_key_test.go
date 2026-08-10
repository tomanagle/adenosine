package auth

import (
	"context"
	"crypto/dsa"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"golang.org/x/crypto/ssh"
)

type memorySSHKeyStore struct {
	key               SSHKey
	lookupErr         error
	touchErr          error
	touchedID         uuid.UUID
	touchedAt         time.Time
	lookupFingerprint string
	keys              []SSHKey
	listErr           error
	revokeErr         error
	listedDID         string
	revokedDID        string
	revokedID         uuid.UUID
	revokedAt         time.Time
}

func (store *memorySSHKeyStore) CreateSSHKey(_ context.Context, key SSHKey) (SSHKey, error) {
	store.key = key
	return key, nil
}

func (store *memorySSHKeyStore) GetActiveSSHKeyByFingerprint(_ context.Context, fingerprint string) (SSHKey, error) {
	store.lookupFingerprint = fingerprint
	if store.lookupErr != nil {
		return SSHKey{}, store.lookupErr
	}
	return store.key, nil
}

func (store *memorySSHKeyStore) TouchSSHKey(_ context.Context, id uuid.UUID, usedAt time.Time) error {
	store.touchedID = id
	store.touchedAt = usedAt
	return store.touchErr
}

func (store *memorySSHKeyStore) ListSSHKeys(_ context.Context, accountDID string) ([]SSHKey, error) {
	store.listedDID = accountDID
	return store.keys, store.listErr
}

func (store *memorySSHKeyStore) RevokeSSHKey(_ context.Context, accountDID string, id uuid.UUID, revokedAt time.Time) error {
	store.revokedDID = accountDID
	store.revokedID = id
	store.revokedAt = revokedAt
	return store.revokeErr
}

func TestSSHKeyServiceCreatesCanonicalPublicCredential(t *testing.T) {
	t.Parallel()
	testCases := []struct{ name string }{{name: "canonical credential"}}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			publicKey, _ := testEd25519Key(t, 1)
			authorizedKey := strings.TrimSpace(string(ssh.MarshalAuthorizedKey(publicKey)))
			inputLine := `restrict ` + authorizedKey + ` alice@example.com`
			now := time.Date(2026, time.August, 9, 12, 34, 56, 123, time.FixedZone("test", 3600))
			id := uuid.MustParse("0198a851-2a89-7ae2-a370-dc68883e3af3")
			store := &memorySSHKeyStore{}
			service := NewSSHKeyService(store, fixedTokenClock{now: now}, fixedTokenIDs{id: id})

			key, err := service.CreateSSHKey(context.Background(), CreateSSHKeyInput{
				AccountDID:    "did:plc:alice",
				Name:          "workstation",
				AuthorizedKey: inputLine,
			})
			if err != nil {
				t.Fatalf("create SSH key: %v", err)
			}
			if key.ID != id {
				t.Fatalf("ID = %s, want %s", key.ID, id)
			}
			if key.AccountDID != "did:plc:alice" || key.Name != "workstation" {
				t.Fatalf("identity metadata = %#v", key)
			}
			if key.Algorithm != ssh.KeyAlgoED25519 {
				t.Fatalf("algorithm = %q, want %q", key.Algorithm, ssh.KeyAlgoED25519)
			}
			if key.Fingerprint != ssh.FingerprintSHA256(publicKey) {
				t.Fatalf("fingerprint = %q, want %q", key.Fingerprint, ssh.FingerprintSHA256(publicKey))
			}
			if key.PublicKey != authorizedKey {
				t.Fatalf("public key = %q, want canonical %q", key.PublicKey, authorizedKey)
			}
			if strings.Contains(key.PublicKey, "alice@example.com") || strings.Contains(key.PublicKey, "restrict") {
				t.Fatalf("public key retained authorized-key metadata: %q", key.PublicKey)
			}
			if !key.CreatedAt.Equal(now.UTC()) || key.CreatedAt.Location() != time.UTC {
				t.Fatalf("created at = %v, want %v", key.CreatedAt, now.UTC())
			}
			if store.key != key {
				t.Fatal("service did not persist the returned public credential")
			}
		})
	}
}

func TestSSHKeyServiceNormalizesMetadata(t *testing.T) {
	t.Parallel()
	testCases := []struct{ name string }{{name: "whitespace"}}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			publicKey, _ := testEd25519Key(t, 8)
			store := &memorySSHKeyStore{}
			service := NewSSHKeyService(store, fixedTokenClock{now: time.Now()}, fixedTokenIDs{id: uuid.New()})

			key, err := service.CreateSSHKey(context.Background(), CreateSSHKeyInput{
				AccountDID:    "  did:plc:alice\t",
				Name:          " workstation ",
				AuthorizedKey: " \n" + string(ssh.MarshalAuthorizedKey(publicKey)) + "\t",
			})
			if err != nil {
				t.Fatalf("create SSH key: %v", err)
			}
			if key.AccountDID != "did:plc:alice" || key.Name != "workstation" {
				t.Fatalf("normalized metadata = (%q, %q)", key.AccountDID, key.Name)
			}
		})
	}
}

func TestSSHKeyServiceListsAndRevokesByOwner(t *testing.T) {
	t.Parallel()
	testCases := []struct{ name string }{{name: "owned key"}}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			now := time.Date(2026, time.August, 9, 12, 0, 0, 0, time.UTC)
			id := uuid.New()
			store := &memorySSHKeyStore{keys: []SSHKey{{ID: id, AccountDID: "did:plc:alice"}}, revokeErr: ErrNotFound}
			service := NewSSHKeyService(store, fixedTokenClock{now: now}, fixedTokenIDs{})

			keys, err := service.ListSSHKeys(context.Background(), " did:plc:alice ")
			if err != nil {
				t.Fatalf("list SSH keys: %v", err)
			}
			if len(keys) != 1 || store.listedDID != "did:plc:alice" {
				t.Fatalf("listed keys = %#v, DID = %q", keys, store.listedDID)
			}
			err = service.RevokeSSHKey(context.Background(), " did:plc:alice ", id)
			if !errors.Is(err, ErrNotFound) {
				t.Fatalf("error = %v, want ErrNotFound", err)
			}
			if store.revokedDID != "did:plc:alice" || store.revokedID != id || !store.revokedAt.Equal(now) {
				t.Fatalf("revoke scope = (%q, %s, %v)", store.revokedDID, store.revokedID, store.revokedAt)
			}
		})
	}
}

func TestSSHKeyServiceValidatesMetadata(t *testing.T) {
	t.Parallel()
	publicKey, _ := testEd25519Key(t, 2)
	line := string(ssh.MarshalAuthorizedKey(publicKey))
	testCases := []struct {
		name  string
		input CreateSSHKeyInput
	}{
		{name: "empty DID", input: CreateSSHKeyInput{Name: "key", AccountDID: "  ", AuthorizedKey: line}},
		{name: "empty name", input: CreateSSHKeyInput{Name: "\t", AccountDID: "did:plc:alice", AuthorizedKey: line}},
		{name: "long name", input: CreateSSHKeyInput{Name: strings.Repeat("a", 256), AccountDID: "did:plc:alice", AuthorizedKey: line}},
		{name: "empty key", input: CreateSSHKeyInput{Name: "key", AccountDID: "did:plc:alice", AuthorizedKey: "\n"}},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			service := NewSSHKeyService(&memorySSHKeyStore{}, fixedTokenClock{}, fixedTokenIDs{})
			if _, err := service.CreateSSHKey(context.Background(), testCase.input); err == nil {
				t.Fatal("CreateSSHKey() error = nil")
			}
		})
	}
}

func TestSSHKeyServiceRejectsInvalidKeyMaterial(t *testing.T) {
	publicKey, privateKey := testEd25519Key(t, 3)
	validLine := strings.TrimSpace(string(ssh.MarshalAuthorizedKey(publicKey)))
	privateBlock, err := ssh.MarshalPrivateKey(privateKey, "test")
	if err != nil {
		t.Fatalf("marshal private key: %v", err)
	}
	certificate := &ssh.Certificate{
		Key:         publicKey,
		CertType:    ssh.UserCert,
		KeyId:       "alice",
		ValidBefore: ssh.CertTimeInfinity,
	}
	signer, err := ssh.NewSignerFromKey(privateKey)
	if err != nil {
		t.Fatalf("create signer: %v", err)
	}
	if err := certificate.SignCert(rand.Reader, signer); err != nil {
		t.Fatalf("sign certificate: %v", err)
	}
	dsaLine := testDSAAuthorizedKey(t)

	testCases := []struct {
		name string
		line string
	}{
		{name: "malformed", line: "ssh-ed25519 not-base64"},
		{name: "private", line: string(pem.EncodeToMemory(privateBlock))},
		{name: "certificate", line: string(ssh.MarshalAuthorizedKey(certificate))},
		{name: "legacy DSA", line: dsaLine},
		{name: "multiple keys", line: validLine + "\n" + validLine},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			service := NewSSHKeyService(&memorySSHKeyStore{}, fixedTokenClock{}, fixedTokenIDs{})
			_, err := service.CreateSSHKey(context.Background(), CreateSSHKeyInput{
				AccountDID:    "did:plc:alice",
				Name:          "key",
				AuthorizedKey: testCase.line,
			})
			if err == nil {
				t.Fatal("CreateSSHKey() error = nil")
			}
		})
	}
}

func TestSSHKeyAuthenticatorReturnsDIDAndTouchesCredential(t *testing.T) {
	t.Parallel()
	testCases := []struct{ name string }{{name: "valid key"}}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			publicKey, _ := testEd25519Key(t, 4)
			id := uuid.MustParse("0198a851-2a89-7ae2-a370-dc68883e3af3")
			now := time.Date(2026, time.August, 9, 12, 34, 56, 0, time.FixedZone("test", -3600))
			store := &memorySSHKeyStore{key: SSHKey{
				ID:          id,
				AccountDID:  "did:plc:alice",
				PublicKey:   strings.TrimSpace(string(ssh.MarshalAuthorizedKey(publicKey))),
				Fingerprint: ssh.FingerprintSHA256(publicKey),
			}}
			authenticator := NewSSHKeyAuthenticator(store, fixedTokenClock{now: now})

			did, err := authenticator.Authenticate(context.Background(), publicKey)
			if err != nil {
				t.Fatalf("authenticate: %v", err)
			}
			if did != "did:plc:alice" {
				t.Fatalf("DID = %q, want did:plc:alice", did)
			}
			if store.lookupFingerprint != ssh.FingerprintSHA256(publicKey) {
				t.Fatalf("lookup fingerprint = %q", store.lookupFingerprint)
			}
			if store.touchedID != id || !store.touchedAt.Equal(now.UTC()) || store.touchedAt.Location() != time.UTC {
				t.Fatalf("touch = (%s, %v), want (%s, %v)", store.touchedID, store.touchedAt, id, now.UTC())
			}
		})
	}
}

func TestSSHKeyAuthenticatorRejectsUnknownAndMismatchedKeys(t *testing.T) {
	t.Parallel()
	presented, _ := testEd25519Key(t, 5)
	other, _ := testEd25519Key(t, 6)
	testCases := []struct {
		name  string
		store *memorySSHKeyStore
	}{
		{name: "unknown", store: &memorySSHKeyStore{lookupErr: ErrUnauthorized}},
		{name: "fingerprint collision", store: &memorySSHKeyStore{key: SSHKey{
			ID:        uuid.New(),
			PublicKey: strings.TrimSpace(string(ssh.MarshalAuthorizedKey(other))),
		}}},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			authenticator := NewSSHKeyAuthenticator(testCase.store, fixedTokenClock{now: time.Now()})
			if _, err := authenticator.Authenticate(context.Background(), presented); !errors.Is(err, ErrUnauthorized) {
				t.Fatalf("error = %v, want ErrUnauthorized", err)
			}
			if testCase.store.touchedID != uuid.Nil {
				t.Fatal("rejected credential was touched")
			}
		})
	}
}

func TestSSHKeyAuthenticatorReturnsTouchFailure(t *testing.T) {
	t.Parallel()
	testCases := []struct{ name string }{{name: "touch failure"}}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			publicKey, _ := testEd25519Key(t, 7)
			wantErr := errors.New("database unavailable")
			store := &memorySSHKeyStore{
				key: SSHKey{
					ID:        uuid.New(),
					PublicKey: strings.TrimSpace(string(ssh.MarshalAuthorizedKey(publicKey))),
				},
				touchErr: wantErr,
			}
			authenticator := NewSSHKeyAuthenticator(store, fixedTokenClock{now: time.Now()})

			if _, err := authenticator.Authenticate(context.Background(), publicKey); !errors.Is(err, wantErr) {
				t.Fatalf("error = %v, want touch failure", err)
			}
		})
	}
}

func testEd25519Key(t *testing.T, marker byte) (ssh.PublicKey, ed25519.PrivateKey) {
	t.Helper()
	seed := make([]byte, ed25519.SeedSize)
	seed[len(seed)-1] = marker
	privateKey := ed25519.NewKeyFromSeed(seed)
	publicKey, err := ssh.NewPublicKey(privateKey.Public())
	if err != nil {
		t.Fatalf("create SSH public key: %v", err)
	}
	return publicKey, privateKey
}

func testDSAAuthorizedKey(t *testing.T) string {
	t.Helper()
	var parameters dsa.Parameters
	if err := dsa.GenerateParameters(&parameters, rand.Reader, dsa.L1024N160); err != nil {
		t.Fatalf("generate DSA parameters: %v", err)
	}
	privateKey := &dsa.PrivateKey{PublicKey: dsa.PublicKey{Parameters: parameters}}
	if err := dsa.GenerateKey(privateKey, rand.Reader); err != nil {
		t.Fatalf("generate DSA key: %v", err)
	}
	publicKey, err := ssh.NewPublicKey(&privateKey.PublicKey)
	if err != nil {
		t.Fatalf("create DSA SSH key: %v", err)
	}
	return string(ssh.MarshalAuthorizedKey(publicKey))
}
