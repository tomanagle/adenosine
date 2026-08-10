package passkey

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/adenosine-dev/adenosine/internal/auth"
	"github.com/go-webauthn/webauthn/protocol"
	"github.com/go-webauthn/webauthn/protocol/webauthncbor"
	"github.com/go-webauthn/webauthn/protocol/webauthncose"
	"github.com/go-webauthn/webauthn/webauthn"
	"github.com/google/uuid"
)

var testNow = time.Date(2026, 8, 9, 12, 0, 0, 0, time.UTC)

type fakeClock struct{ now time.Time }

func (clock *fakeClock) Now() time.Time { return clock.now }

type fakeIDs struct{ id uuid.UUID }

func (ids fakeIDs) New() (uuid.UUID, error) { return ids.id, nil }

type fakeSecrets struct {
	values []string
	index  int
}

func (secrets *fakeSecrets) New() (string, error) {
	if secrets.index >= len(secrets.values) {
		return "", errors.New("no fake secret")
	}
	value := secrets.values[secrets.index]
	secrets.index++
	return value, nil
}

type fakePasskeyStore struct {
	users               map[string]User
	credentials         map[string]Credential
	ceremonies          map[string]Ceremony
	events              []string
	getUserCalls        int
	credentialLookups   int
	lastCreatedCeremony Ceremony
}

func newFakePasskeyStore() *fakePasskeyStore {
	return &fakePasskeyStore{users: map[string]User{}, credentials: map[string]Credential{}, ceremonies: map[string]Ceremony{}}
}

func (store *fakePasskeyStore) CreateUser(_ context.Context, rpID string, user User, _ time.Time) (User, error) {
	store.users[rpID+user.AccountDID] = user
	return user, nil
}

func (store *fakePasskeyStore) GetUser(_ context.Context, rpID, accountDID string) (User, error) {
	store.getUserCalls++
	user, ok := store.users[rpID+accountDID]
	if !ok {
		return User{}, auth.ErrUnauthorized
	}
	credentials := make([]Credential, 0)
	for _, credential := range store.credentials {
		if credential.AccountDID == accountDID {
			credentials = append(credentials, credential)
		}
	}
	user.Credentials = credentials
	return user, nil
}

func (store *fakePasskeyStore) CreateCredential(_ context.Context, _ string, credential Credential) (Credential, error) {
	store.credentials[string(credential.CredentialID)] = credential
	return credential, nil
}

func (store *fakePasskeyStore) GetCredentialByCredentialID(_ context.Context, _ string, credentialID []byte) (Credential, error) {
	store.credentialLookups++
	credential, ok := store.credentials[string(credentialID)]
	if !ok {
		return Credential{}, auth.ErrUnauthorized
	}
	return credential, nil
}

func (store *fakePasskeyStore) UpdateCredential(_ context.Context, _ string, credential Credential, usedAt time.Time) (Credential, error) {
	store.events = append(store.events, "credential")
	credential.LastUsedAt = &usedAt
	store.credentials[string(credential.CredentialID)] = credential
	return credential, nil
}

func (store *fakePasskeyStore) ListCredentials(_ context.Context, _ string, accountDID string) ([]Credential, error) {
	result := []Credential{}
	for _, credential := range store.credentials {
		if credential.AccountDID == accountDID {
			result = append(result, credential)
		}
	}
	return result, nil
}

func (store *fakePasskeyStore) RevokeCredential(_ context.Context, _ string, accountDID string, id uuid.UUID, _ time.Time) error {
	for key, credential := range store.credentials {
		if credential.ID == id && credential.AccountDID == accountDID {
			delete(store.credentials, key)
			return nil
		}
	}
	return auth.ErrNotFound
}

func (store *fakePasskeyStore) CreateCeremony(_ context.Context, ceremony Ceremony) (Ceremony, error) {
	store.lastCreatedCeremony = ceremony
	store.ceremonies[string(ceremony.TokenHash)] = ceremony
	return ceremony, nil
}

func (store *fakePasskeyStore) ConsumeCeremony(_ context.Context, tokenHash []byte, now time.Time) (Ceremony, error) {
	key := string(tokenHash)
	ceremony, ok := store.ceremonies[key]
	if !ok || !ceremony.ExpiresAt.After(now) {
		return Ceremony{}, auth.ErrUnauthorized
	}
	delete(store.ceremonies, key)
	return ceremony, nil
}

func (store *fakePasskeyStore) PurgeExpiredCeremonies(_ context.Context, now time.Time) error {
	for key, ceremony := range store.ceremonies {
		if !ceremony.ExpiresAt.After(now) {
			delete(store.ceremonies, key)
		}
	}
	return nil
}

func (store *fakePasskeyStore) TouchAccountLogin(_ context.Context, _ string, _ time.Time) error {
	store.events = append(store.events, "account")
	return nil
}

type fakeSessionIssuer struct {
	events *[]string
	did    string
}

func (issuer *fakeSessionIssuer) CreateSession(_ context.Context, did string) (auth.Session, string, error) {
	*issuer.events = append(*issuer.events, "session")
	issuer.did = did
	return auth.Session{ID: uuid.MustParse("0198a851-2a89-7ae2-a370-dc68883e3af3"), AccountDID: did, ExpiresAt: testNow.Add(time.Hour)}, "session-secret", nil
}

func TestBuild(t *testing.T) {
	testCases := []struct {
		name       string
		baseURL    string
		wantRPID   string
		wantOrigin string
		wantErr    bool
	}{
		{name: "production HTTPS", baseURL: "https://Example.COM:8443/", wantRPID: "example.com", wantOrigin: "https://example.com:8443"},
		{name: "canonical default port", baseURL: "https://Example.COM:443", wantRPID: "example.com", wantOrigin: "https://example.com"},
		{name: "IPv4 loopback HTTP", baseURL: "http://127.0.0.1:8080", wantRPID: "127.0.0.1", wantOrigin: "http://127.0.0.1:8080"},
		{name: "IPv6 loopback HTTP", baseURL: "http://[::1]:8080", wantRPID: "::1", wantOrigin: "http://[::1]:8080"},
		{name: "localhost HTTP", baseURL: "http://localhost:8080/", wantRPID: "localhost", wantOrigin: "http://localhost:8080"},
		{name: "production HTTP", baseURL: "http://example.com", wantErr: true},
		{name: "userinfo", baseURL: "https://user@example.com", wantErr: true},
		{name: "query", baseURL: "https://example.com/?x=1", wantErr: true},
		{name: "fragment", baseURL: "https://example.com/#x", wantErr: true},
		{name: "non-root path", baseURL: "https://example.com/app", wantErr: true},
		{name: "relative", baseURL: "example.com", wantErr: true},
		{name: "non HTTP scheme", baseURL: "ftp://example.com", wantErr: true},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			rpID, origin, err := relyingParty(testCase.baseURL)
			if (err != nil) != testCase.wantErr {
				t.Fatalf("relyingParty() error = %v, wantErr %v", err, testCase.wantErr)
			}
			if rpID != testCase.wantRPID || origin != testCase.wantOrigin {
				t.Fatalf("relyingParty() = %q, %q", rpID, origin)
			}
		})
	}
}

func TestBeginRegistration(t *testing.T) {
	browserSessionID := uuid.MustParse("0198a851-2a89-7ae2-a370-dc68883e3af1")
	existingCredentialID := []byte("existing-credential")
	testCases := []struct {
		name     string
		existing bool
		wantErr  bool
	}{
		{name: "creates stable user and bound ceremony"},
		{name: "excludes existing credentials", existing: true},
		{name: "rejects missing binding", wantErr: true},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			store := newFakePasskeyStore()
			secrets := &fakeSecrets{values: []string{testSecret(1), testSecret(2)}}
			if testCase.existing {
				store.users["example.comdid:plc:alice"] = User{AccountDID: "did:plc:alice", Handle: testBytes(3), Name: "Alice", DisplayName: "Alice"}
				store.credentials[string(existingCredentialID)] = Credential{AccountDID: "did:plc:alice", CredentialID: existingCredentialID}
				secrets.values = []string{testSecret(2)}
			}
			service := testService(store, secrets)
			sessionID := browserSessionID
			if testCase.wantErr {
				sessionID = uuid.Nil
			}
			result, err := service.BeginRegistration(context.Background(), "did:plc:alice", sessionID, "Laptop")
			if (err != nil) != testCase.wantErr {
				t.Fatalf("BeginRegistration() error = %v, wantErr %v", err, testCase.wantErr)
			}
			if testCase.wantErr {
				return
			}
			if result.Token != testSecret(2) || len(result.Options) == 0 {
				t.Fatalf("result = %#v", result)
			}
			if len(store.lastCreatedCeremony.TokenHash) != sha256.Size || string(store.lastCreatedCeremony.TokenHash) == result.Token {
				t.Fatalf("persisted token = %q", store.lastCreatedCeremony.TokenHash)
			}
			if store.lastCreatedCeremony.BrowserSessionID == nil || *store.lastCreatedCeremony.BrowserSessionID != browserSessionID || store.lastCreatedCeremony.ExpiresAt.Sub(store.lastCreatedCeremony.CreatedAt) != ceremonyLifetime {
				t.Fatalf("ceremony = %#v", store.lastCreatedCeremony)
			}
			var options protocol.CredentialCreation
			if err := json.Unmarshal(result.Options, &options); err != nil {
				t.Fatal(err)
			}
			if options.Response.AuthenticatorSelection.ResidentKey != protocol.ResidentKeyRequirementRequired || options.Response.AuthenticatorSelection.UserVerification != protocol.VerificationRequired || options.Response.Attestation != protocol.PreferNoAttestation || options.Response.Timeout != int(ceremonyLifetime.Milliseconds()) {
				t.Fatalf("registration options = %#v", options.Response)
			}
			if testCase.existing && (len(options.Response.CredentialExcludeList) != 1 || string(options.Response.CredentialExcludeList[0].CredentialID) != string(existingCredentialID)) {
				t.Fatalf("exclusions = %#v", options.Response.CredentialExcludeList)
			}
		})
	}
}

func TestFinishRegistrationConsumesBeforeValidation(t *testing.T) {
	browserSessionID := uuid.MustParse("0198a851-2a89-7ae2-a370-dc68883e3af1")
	testCases := []struct {
		name        string
		callerDID   string
		callerSID   uuid.UUID
		advance     time.Duration
		wantLookups int
	}{
		{name: "invalid response is single use", callerDID: "did:plc:alice", callerSID: browserSessionID, wantLookups: 1},
		{name: "DID injection cannot select another account", callerDID: "did:plc:mallory", callerSID: browserSessionID},
		{name: "browser session mismatch", callerDID: "did:plc:alice", callerSID: uuid.New()},
		{name: "expired ceremony", callerDID: "did:plc:alice", callerSID: browserSessionID, advance: ceremonyLifetime},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			store := newFakePasskeyStore()
			store.users["example.comdid:plc:alice"] = User{AccountDID: "did:plc:alice", Handle: testBytes(3), Name: "Alice", DisplayName: "Alice"}
			clock := &fakeClock{now: testNow}
			service := testServiceWithClock(store, &fakeSecrets{values: []string{testSecret(1)}}, clock)
			state, _ := json.Marshal(ceremonyState{Session: zeroSession(testNow.Add(ceremonyLifetime)), CredentialName: "Laptop"})
			token := testSecret(1)
			hash := sha256.Sum256([]byte(token))
			store.ceremonies[string(hash[:])] = Ceremony{TokenHash: hash[:], Kind: ceremonyRegistration, RPID: "example.com", AccountDID: "did:plc:alice", BrowserSessionID: &browserSessionID, SessionData: state, CreatedAt: testNow, ExpiresAt: testNow.Add(ceremonyLifetime)}
			clock.now = clock.now.Add(testCase.advance)
			_, err := service.FinishRegistration(context.Background(), testCase.callerDID, testCase.callerSID, token, []byte("not JSON"))
			if !errors.Is(err, auth.ErrValidation) {
				t.Fatalf("FinishRegistration() error = %v", err)
			}
			if store.getUserCalls != testCase.wantLookups {
				t.Fatalf("GetUser calls = %d, want %d", store.getUserCalls, testCase.wantLookups)
			}
			_, replayErr := service.FinishRegistration(context.Background(), "did:plc:alice", browserSessionID, token, nil)
			if !errors.Is(replayErr, auth.ErrValidation) {
				t.Fatalf("replay error = %v", replayErr)
			}
		})
	}
}

func TestFinishRegistrationStoresCompleteCredential(t *testing.T) {
	testCases := []struct {
		name string
	}{
		{name: "verified no-attestation credential"},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			store := newFakePasskeyStore()
			credentialRecordID := uuid.MustParse("0198a851-2a89-7ae2-a370-dc68883e3af2")
			// go-webauthn checks registration expiry against the wall clock.
			clock := &fakeClock{now: time.Now().UTC()}
			service := Must(
				"https://example.com", store, &fakeSessionIssuer{events: &store.events}, clock,
				fakeIDs{id: credentialRecordID}, &fakeSecrets{values: []string{testSecret(1), testSecret(2)}},
			)
			browserSessionID := uuid.MustParse("0198a851-2a89-7ae2-a370-dc68883e3af1")
			begin, err := service.BeginRegistration(context.Background(), "did:plc:alice", browserSessionID, "Laptop")
			if err != nil {
				t.Fatal(err)
			}
			var options protocol.CredentialCreation
			if err := json.Unmarshal(begin.Options, &options); err != nil {
				t.Fatal(err)
			}
			credentialID := []byte("new-credential-id")
			_, publicKey := testCredentialKey(t)
			response := registrationResponse(t, credentialID, publicKey, options.Response.Challenge.String())
			summary, err := service.FinishRegistration(context.Background(), "did:plc:alice", browserSessionID, begin.Token, response)
			if err != nil {
				t.Fatal(err)
			}
			stored := store.credentials[string(credentialID)]
			if summary.ID != credentialRecordID || summary.Name != "Laptop" || stored.AccountDID != "did:plc:alice" || len(stored.PublicKey) == 0 || stored.AttestationType != "none" || len(stored.AAGUID) != 16 || len(stored.AttestationClientDataJSON) == 0 || len(stored.AttestationClientDataHash) != sha256.Size || len(stored.AttestationAuthenticatorData) == 0 || stored.AttestationPublicKeyAlgorithm != int64(webauthncose.AlgES256) || len(stored.AttestationObject) == 0 {
				t.Fatalf("summary/stored credential = %#v / %#v", summary, stored)
			}
		})
	}
}

func TestFinishLogin(t *testing.T) {
	testCases := []struct {
		name       string
		userHandle []byte
		wantErr    bool
	}{
		{name: "discoverable callback persists before session", userHandle: testBytes(3)},
		{name: "foreign user handle is unauthorized", userHandle: testBytes(4), wantErr: true},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			store := newFakePasskeyStore()
			secrets := &fakeSecrets{values: []string{testSecret(1)}}
			service := testService(store, secrets)
			begin, err := service.BeginLogin(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			var options protocol.CredentialAssertion
			if err := json.Unmarshal(begin.Options, &options); err != nil {
				t.Fatal(err)
			}
			credentialID := []byte("credential-id")
			privateKey, publicKey := testCredentialKey(t)
			store.users["example.comdid:plc:alice"] = User{AccountDID: "did:plc:alice", Handle: testBytes(3), Name: "Alice", DisplayName: "Alice"}
			store.credentials[string(credentialID)] = Credential{ID: uuid.New(), AccountDID: "did:plc:alice", CredentialID: credentialID, PublicKey: publicKey, Flags: 5, AAGUID: make([]byte, 16)}
			response := signedAssertion(t, privateKey, credentialID, testCase.userHandle, options.Response.Challenge.String())
			result, err := service.FinishLogin(context.Background(), begin.Token, response)
			if (err != nil) != testCase.wantErr || (err != nil && !errors.Is(err, auth.ErrUnauthorized)) {
				t.Fatalf("FinishLogin() error = %v, wantErr %v", err, testCase.wantErr)
			}
			if store.credentialLookups != 1 {
				t.Fatalf("credential callback lookups = %d", store.credentialLookups)
			}
			if testCase.wantErr {
				return
			}
			if result.DID != "did:plc:alice" || result.SessionPlaintext != "session-secret" {
				t.Fatalf("result = %#v", result)
			}
			wantEvents := []string{"credential", "account", "session"}
			if stringJSON(store.events) != stringJSON(wantEvents) {
				t.Fatalf("events = %v, want %v", store.events, wantEvents)
			}
			if _, replayErr := service.FinishLogin(context.Background(), begin.Token, response); !errors.Is(replayErr, auth.ErrUnauthorized) {
				t.Fatalf("replay error = %v", replayErr)
			}
		})
	}
}

func TestListAndRevoke(t *testing.T) {
	aliceID, malloryID := uuid.New(), uuid.New()
	testCases := []struct {
		name    string
		did     string
		id      uuid.UUID
		wantErr error
	}{
		{name: "owner revokes", did: "did:plc:alice", id: aliceID},
		{name: "other account cannot revoke", did: "did:plc:alice", id: malloryID, wantErr: auth.ErrNotFound},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			store := newFakePasskeyStore()
			store.credentials["alice"] = Credential{ID: aliceID, AccountDID: "did:plc:alice", CredentialID: []byte("alice"), Name: "Alice key"}
			store.credentials["mallory"] = Credential{ID: malloryID, AccountDID: "did:plc:mallory", CredentialID: []byte("mallory"), Name: "Mallory key"}
			service := testService(store, &fakeSecrets{})
			listed, err := service.List(context.Background(), "did:plc:alice")
			if err != nil || len(listed) != 1 || listed[0].ID != aliceID {
				t.Fatalf("List() = %#v, %v", listed, err)
			}
			err = service.Revoke(context.Background(), testCase.did, testCase.id)
			if !errors.Is(err, testCase.wantErr) {
				t.Fatalf("Revoke() error = %v, want %v", err, testCase.wantErr)
			}
		})
	}
}

func testService(store *fakePasskeyStore, secrets SecretGenerator) *Service {
	return testServiceWithClock(store, secrets, &fakeClock{now: testNow})
}

func testServiceWithClock(store *fakePasskeyStore, secrets SecretGenerator, clock *fakeClock) *Service {
	sessions := &fakeSessionIssuer{events: &store.events}
	return Must("https://example.com", store, sessions, clock, fakeIDs{id: uuid.New()}, secrets)
}

func testBytes(value byte) []byte {
	result := make([]byte, 32)
	for index := range result {
		result[index] = value
	}
	return result
}

func testSecret(value byte) string { return base64.RawURLEncoding.EncodeToString(testBytes(value)) }

func zeroSession(expires time.Time) webauthn.SessionData {
	return webauthn.SessionData{Expires: expires}
}

func testCredentialKey(t *testing.T) (*ecdsa.PrivateKey, []byte) {
	t.Helper()
	privateKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	publicKey, err := webauthncbor.Marshal(webauthncose.EC2PublicKeyData{
		PublicKeyData: webauthncose.PublicKeyData{KeyType: int64(webauthncose.EllipticKey), Algorithm: int64(webauthncose.AlgES256)},
		Curve:         1, XCoord: privateKey.X.FillBytes(make([]byte, 32)), YCoord: privateKey.Y.FillBytes(make([]byte, 32)),
	})
	if err != nil {
		t.Fatal(err)
	}
	return privateKey, publicKey
}

func signedAssertion(t *testing.T, privateKey *ecdsa.PrivateKey, credentialID, userHandle []byte, challenge string) []byte {
	t.Helper()
	clientData, err := json.Marshal(map[string]any{"type": "webauthn.get", "challenge": challenge, "origin": "https://example.com", "crossOrigin": false})
	if err != nil {
		t.Fatal(err)
	}
	rpIDHash := sha256.Sum256([]byte("example.com"))
	authenticatorData := make([]byte, 37)
	copy(authenticatorData, rpIDHash[:])
	authenticatorData[32] = 5
	binary.BigEndian.PutUint32(authenticatorData[33:], 1)
	clientHash := sha256.Sum256(clientData)
	signed := append(append([]byte{}, authenticatorData...), clientHash[:]...)
	digest := sha256.Sum256(signed)
	signature, err := ecdsa.SignASN1(rand.Reader, privateKey, digest[:])
	if err != nil {
		t.Fatal(err)
	}
	encodedID := base64.RawURLEncoding.EncodeToString(credentialID)
	response, err := json.Marshal(map[string]any{
		"id": encodedID, "rawId": encodedID, "type": "public-key",
		"response": map[string]any{
			"clientDataJSON":    base64.RawURLEncoding.EncodeToString(clientData),
			"authenticatorData": base64.RawURLEncoding.EncodeToString(authenticatorData),
			"signature":         base64.RawURLEncoding.EncodeToString(signature),
			"userHandle":        base64.RawURLEncoding.EncodeToString(userHandle),
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	return response
}

func registrationResponse(t *testing.T, credentialID, publicKey []byte, challenge string) []byte {
	t.Helper()
	clientData, err := json.Marshal(map[string]any{"type": "webauthn.create", "challenge": challenge, "origin": "https://example.com", "crossOrigin": false})
	if err != nil {
		t.Fatal(err)
	}
	rpIDHash := sha256.Sum256([]byte("example.com"))
	authenticatorData := make([]byte, 55+len(credentialID)+len(publicKey))
	copy(authenticatorData, rpIDHash[:])
	authenticatorData[32] = byte(protocol.FlagUserPresent | protocol.FlagUserVerified | protocol.FlagAttestedCredentialData)
	copy(authenticatorData[37:53], testBytes(9)[:16])
	binary.BigEndian.PutUint16(authenticatorData[53:55], uint16(len(credentialID)))
	copy(authenticatorData[55:], credentialID)
	copy(authenticatorData[55+len(credentialID):], publicKey)
	attestationObject, err := webauthncbor.Marshal(map[string]any{"fmt": "none", "authData": authenticatorData, "attStmt": map[string]any{}})
	if err != nil {
		t.Fatal(err)
	}
	encodedID := base64.RawURLEncoding.EncodeToString(credentialID)
	response, err := json.Marshal(map[string]any{
		"id": encodedID, "rawId": encodedID, "type": "public-key", "authenticatorAttachment": "platform",
		"response": map[string]any{
			"clientDataJSON":     base64.RawURLEncoding.EncodeToString(clientData),
			"attestationObject":  base64.RawURLEncoding.EncodeToString(attestationObject),
			"authenticatorData":  base64.RawURLEncoding.EncodeToString(authenticatorData),
			"publicKey":          base64.RawURLEncoding.EncodeToString(publicKey),
			"publicKeyAlgorithm": int64(webauthncose.AlgES256),
			"transports":         []string{"internal", "hybrid"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	return response
}

func stringJSON(value any) string {
	encoded, _ := json.Marshal(value)
	return string(encoded)
}
