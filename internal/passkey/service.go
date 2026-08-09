package passkey

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/url"
	"strings"
	"time"

	"github.com/adenosine-dev/adenosine/internal/auth"
	"github.com/go-webauthn/webauthn/protocol"
	"github.com/go-webauthn/webauthn/webauthn"
	"github.com/google/uuid"
)

const ceremonyLifetime = 5 * time.Minute

// Service performs passkey registration and discoverable login ceremonies.
type Service struct {
	rpID     string
	store    Store
	sessions SessionIssuer
	clock    Clock
	ids      IDGenerator
	secrets  SecretGenerator
	webAuthn *webauthn.WebAuthn
}

// RandomSecretGenerator creates 256-bit, unpadded base64url secrets.
type RandomSecretGenerator struct{}

func (RandomSecretGenerator) New() (string, error) {
	value := make([]byte, 32)
	if _, err := rand.Read(value); err != nil {
		return "", fmt.Errorf("read random secret: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(value), nil
}

// Must constructs the required passkey service or panics on invalid startup configuration.
func Must(baseURL string, store Store, sessions SessionIssuer, clock Clock, ids IDGenerator, secrets SecretGenerator) *Service {
	service, err := build(baseURL, store, sessions, clock, ids, secrets)
	if err != nil {
		panic(err)
	}
	return service
}

func build(baseURL string, store Store, sessions SessionIssuer, clock Clock, ids IDGenerator, secrets SecretGenerator) (*Service, error) {
	if store == nil || sessions == nil || clock == nil || ids == nil || secrets == nil {
		return nil, errors.New("build passkey service: dependencies must not be nil")
	}
	rpID, origin, err := relyingParty(baseURL)
	if err != nil {
		return nil, fmt.Errorf("build passkey service: %w", err)
	}
	w, err := webauthn.New(&webauthn.Config{
		RPID: rpID, RPDisplayName: "Adenosine", RPOrigins: []string{origin},
		AttestationPreference: protocol.PreferNoAttestation,
		AuthenticatorSelection: protocol.AuthenticatorSelection{
			ResidentKey:        protocol.ResidentKeyRequirementRequired,
			RequireResidentKey: protocol.ResidentKeyRequired(),
			UserVerification:   protocol.VerificationRequired,
		},
		Timeouts: webauthn.TimeoutsConfig{
			Login:        webauthn.TimeoutConfig{Enforce: true, Timeout: ceremonyLifetime, TimeoutUVD: ceremonyLifetime},
			Registration: webauthn.TimeoutConfig{Enforce: true, Timeout: ceremonyLifetime, TimeoutUVD: ceremonyLifetime},
		},
	})
	if err != nil {
		return nil, fmt.Errorf("configure WebAuthn: %w", err)
	}
	return &Service{rpID: rpID, store: store, sessions: sessions, clock: clock, ids: ids, secrets: secrets, webAuthn: w}, nil
}

func relyingParty(baseURL string) (rpID, origin string, err error) {
	parsed, err := url.Parse(baseURL)
	if err != nil || !parsed.IsAbs() || parsed.Opaque != "" || parsed.Host == "" {
		return "", "", errors.New("BaseURL must be an absolute HTTP origin")
	}
	if parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" || (parsed.Path != "" && parsed.Path != "/") || parsed.RawPath != "" {
		return "", "", errors.New("BaseURL must not contain userinfo, query, fragment, or a non-root path")
	}
	hostname := strings.ToLower(parsed.Hostname())
	loopback := hostname == "localhost"
	if ip := net.ParseIP(hostname); ip != nil {
		loopback = ip.IsLoopback()
	}
	scheme := strings.ToLower(parsed.Scheme)
	if scheme != "https" && !(scheme == "http" && loopback) {
		return "", "", errors.New("BaseURL must use HTTPS except for loopback HTTP")
	}
	port := parsed.Port()
	if (scheme == "https" && port == "443") || (scheme == "http" && port == "80") {
		port = ""
	}
	originHost := hostname
	if strings.Contains(hostname, ":") {
		originHost = "[" + hostname + "]"
	}
	if port != "" {
		originHost = net.JoinHostPort(hostname, port)
	}
	return hostname, scheme + "://" + originHost, nil
}

// BeginRegistration starts an authenticated, browser-session-bound registration.
func (service *Service) BeginRegistration(ctx context.Context, accountDID string, browserSessionID uuid.UUID, name string) (BeginResult, error) {
	accountDID, name = strings.TrimSpace(accountDID), strings.TrimSpace(name)
	if accountDID == "" || browserSessionID == uuid.Nil || name == "" || len(name) > 255 {
		return BeginResult{}, fmt.Errorf("%w: DID, browser session, and passkey name are required", auth.ErrValidation)
	}
	now := service.clock.Now().UTC()
	user, err := service.store.GetUser(ctx, service.rpID, accountDID)
	if errors.Is(err, auth.ErrUnauthorized) || errors.Is(err, auth.ErrNotFound) {
		handle, secretErr := service.newSecretBytes()
		if secretErr != nil {
			return BeginResult{}, fmt.Errorf("generate WebAuthn user handle: %w", secretErr)
		}
		user, err = service.store.CreateUser(ctx, service.rpID, User{AccountDID: accountDID, Handle: handle, Name: accountDID, DisplayName: accountDID}, now)
		if errors.Is(err, auth.ErrConflict) {
			user, err = service.store.GetUser(ctx, service.rpID, accountDID)
		}
	}
	if err != nil {
		return BeginResult{}, fmt.Errorf("load WebAuthn user: %w", err)
	}
	if user.AccountDID != accountDID || len(user.Handle) != 32 {
		return BeginResult{}, errors.New("load WebAuthn user: store returned invalid identity")
	}
	creation, session, err := service.webAuthn.BeginRegistration(webAuthnUser{user}, webauthn.WithExclusions(webauthn.Credentials(webAuthnUser{user}.WebAuthnCredentials()).CredentialDescriptors()))
	if err != nil {
		return BeginResult{}, fmt.Errorf("begin WebAuthn registration: %w", err)
	}
	session.Expires = now.Add(ceremonyLifetime)
	return service.saveCeremony(ctx, creation, ceremonyState{Session: *session, CredentialName: name}, Ceremony{
		Kind: ceremonyRegistration, RPID: service.rpID, AccountDID: accountDID, BrowserSessionID: &browserSessionID,
		CreatedAt: now, ExpiresAt: now.Add(ceremonyLifetime),
	})
}

// FinishRegistration consumes and validates a browser-session-bound registration.
func (service *Service) FinishRegistration(ctx context.Context, accountDID string, browserSessionID uuid.UUID, token string, response []byte) (CredentialSummary, error) {
	ceremony, err := service.consume(ctx, token)
	if err != nil {
		if errors.Is(err, auth.ErrUnauthorized) || errors.Is(err, auth.ErrNotFound) {
			return CredentialSummary{}, registrationProtocolError()
		}
		return CredentialSummary{}, fmt.Errorf("consume registration ceremony: %w", err)
	}
	if ceremony.Kind != ceremonyRegistration || ceremony.RPID != service.rpID || ceremony.AccountDID != accountDID || ceremony.BrowserSessionID == nil || *ceremony.BrowserSessionID != browserSessionID {
		return CredentialSummary{}, registrationProtocolError()
	}
	state, err := decodeCeremony(ceremony)
	if err != nil {
		return CredentialSummary{}, registrationProtocolError()
	}
	user, err := service.store.GetUser(ctx, service.rpID, ceremony.AccountDID)
	if errors.Is(err, auth.ErrUnauthorized) || errors.Is(err, auth.ErrNotFound) || (err == nil && user.AccountDID != ceremony.AccountDID) {
		return CredentialSummary{}, registrationProtocolError()
	}
	if err != nil {
		return CredentialSummary{}, fmt.Errorf("load registration user: %w", err)
	}
	parsed, err := protocol.ParseCredentialCreationResponseBytes(response)
	if err != nil {
		return CredentialSummary{}, registrationProtocolError()
	}
	verified, err := service.webAuthn.CreateCredential(webAuthnUser{user}, state.Session, parsed)
	if err != nil {
		return CredentialSummary{}, registrationProtocolError()
	}
	id, err := service.ids.New()
	if err != nil {
		return CredentialSummary{}, fmt.Errorf("generate passkey ID: %w", err)
	}
	credential := credentialFromWebAuthn(id, ceremony.AccountDID, state.CredentialName, *verified, service.clock.Now().UTC())
	stored, err := service.store.CreateCredential(ctx, service.rpID, credential)
	if err != nil {
		return CredentialSummary{}, fmt.Errorf("store passkey credential: %w", err)
	}
	return credentialSummary(stored), nil
}

// BeginLogin starts an account-agnostic discoverable passkey login.
func (service *Service) BeginLogin(ctx context.Context) (BeginResult, error) {
	now := service.clock.Now().UTC()
	if err := service.store.PurgeExpiredCeremonies(ctx, now); err != nil {
		return BeginResult{}, fmt.Errorf("purge expired passkey ceremonies: %w", err)
	}
	assertion, session, err := service.webAuthn.BeginDiscoverableLogin()
	if err != nil {
		return BeginResult{}, fmt.Errorf("begin passkey login: %w", err)
	}
	session.Expires = now.Add(ceremonyLifetime)
	return service.saveCeremony(ctx, assertion, ceremonyState{Session: *session}, Ceremony{
		Kind: ceremonyAuthentication, RPID: service.rpID, CreatedAt: now, ExpiresAt: now.Add(ceremonyLifetime),
	})
}

// FinishLogin validates a discoverable passkey, persists its mutable state, then issues a local session.
func (service *Service) FinishLogin(ctx context.Context, token string, response []byte) (LoginResult, error) {
	ceremony, err := service.consume(ctx, token)
	if err != nil {
		if errors.Is(err, auth.ErrUnauthorized) || errors.Is(err, auth.ErrNotFound) {
			return LoginResult{}, auth.ErrUnauthorized
		}
		return LoginResult{}, fmt.Errorf("consume login ceremony: %w", err)
	}
	if ceremony.Kind != ceremonyAuthentication || ceremony.RPID != service.rpID || ceremony.AccountDID != "" || ceremony.BrowserSessionID != nil {
		return LoginResult{}, auth.ErrUnauthorized
	}
	state, err := decodeCeremony(ceremony)
	if err != nil {
		return LoginResult{}, auth.ErrUnauthorized
	}
	parsed, err := protocol.ParseCredentialRequestResponseBytes(response)
	if err != nil {
		return LoginResult{}, auth.ErrUnauthorized
	}
	var loaded Credential
	var callbackErr error
	validatedUser, validatedCredential, err := service.webAuthn.ValidatePasskeyLogin(func(rawID, userHandle []byte) (webauthn.User, error) {
		credential, lookupErr := service.store.GetCredentialByCredentialID(ctx, service.rpID, rawID)
		if lookupErr != nil {
			if !errors.Is(lookupErr, auth.ErrUnauthorized) && !errors.Is(lookupErr, auth.ErrNotFound) {
				callbackErr = lookupErr
			}
			return nil, auth.ErrUnauthorized
		}
		user, lookupErr := service.store.GetUser(ctx, service.rpID, credential.AccountDID)
		if lookupErr != nil {
			if !errors.Is(lookupErr, auth.ErrUnauthorized) && !errors.Is(lookupErr, auth.ErrNotFound) {
				callbackErr = lookupErr
			}
			return nil, auth.ErrUnauthorized
		}
		if user.AccountDID != credential.AccountDID || !equalBytes(user.Handle, userHandle) {
			return nil, auth.ErrUnauthorized
		}
		loaded = credential
		user.Credentials = []Credential{credential}
		return webAuthnUser{user}, nil
	}, state.Session, parsed)
	if err != nil {
		if callbackErr != nil {
			return LoginResult{}, fmt.Errorf("load passkey login credential: %w", callbackErr)
		}
		return LoginResult{}, auth.ErrUnauthorized
	}
	user, ok := validatedUser.(webAuthnUser)
	if !ok || user.AccountDID == "" || loaded.AccountDID != user.AccountDID {
		return LoginResult{}, auth.ErrUnauthorized
	}
	now := service.clock.Now().UTC()
	mutable := loaded
	mutable.SignCount = validatedCredential.Authenticator.SignCount
	mutable.CloneWarning = validatedCredential.Authenticator.CloneWarning
	mutable.Flags = updatedCredentialFlags(mutable.Flags, validatedCredential.Flags)
	if _, err := service.store.UpdateCredential(ctx, service.rpID, mutable, now); err != nil {
		return LoginResult{}, fmt.Errorf("update passkey credential: %w", err)
	}
	if err := service.store.TouchAccountLogin(ctx, user.AccountDID, now); err != nil {
		return LoginResult{}, fmt.Errorf("touch passkey account login: %w", err)
	}
	session, plaintext, err := service.sessions.CreateSession(ctx, user.AccountDID)
	if err != nil {
		return LoginResult{}, fmt.Errorf("issue local session: %w", err)
	}
	return LoginResult{DID: user.AccountDID, SessionID: session.ID, SessionPlaintext: plaintext, SessionExpiresAt: session.ExpiresAt}, nil
}

// List returns active passkeys owned by an account.
func (service *Service) List(ctx context.Context, accountDID string) ([]CredentialSummary, error) {
	credentials, err := service.store.ListCredentials(ctx, service.rpID, accountDID)
	if err != nil {
		return nil, fmt.Errorf("list passkey credentials: %w", err)
	}
	summaries := make([]CredentialSummary, len(credentials))
	for index, credential := range credentials {
		summaries[index] = credentialSummary(credential)
	}
	return summaries, nil
}

// Revoke revokes an active passkey owned by an account.
func (service *Service) Revoke(ctx context.Context, accountDID string, id uuid.UUID) error {
	if err := service.store.RevokeCredential(ctx, service.rpID, accountDID, id, service.clock.Now().UTC()); err != nil {
		return fmt.Errorf("revoke passkey credential: %w", err)
	}
	return nil
}

func (service *Service) saveCeremony(ctx context.Context, options any, state ceremonyState, ceremony Ceremony) (BeginResult, error) {
	optionsJSON, err := json.Marshal(options)
	if err != nil {
		return BeginResult{}, fmt.Errorf("marshal WebAuthn options: %w", err)
	}
	ceremony.SessionData, err = json.Marshal(state)
	if err != nil {
		return BeginResult{}, fmt.Errorf("marshal WebAuthn ceremony: %w", err)
	}
	token, err := service.secrets.New()
	if err != nil {
		return BeginResult{}, fmt.Errorf("generate ceremony token: %w", err)
	}
	if _, err := decodeSecret(token); err != nil {
		return BeginResult{}, fmt.Errorf("generate ceremony token: %w", err)
	}
	hash := sha256.Sum256([]byte(token))
	ceremony.TokenHash = hash[:]
	if _, err := service.store.CreateCeremony(ctx, ceremony); err != nil {
		return BeginResult{}, fmt.Errorf("store WebAuthn ceremony: %w", err)
	}
	return BeginResult{Options: optionsJSON, Token: token}, nil
}

func (service *Service) consume(ctx context.Context, token string) (Ceremony, error) {
	hash := sha256.Sum256([]byte(token))
	return service.store.ConsumeCeremony(ctx, hash[:], service.clock.Now().UTC())
}

func (service *Service) newSecretBytes() ([]byte, error) {
	secret, err := service.secrets.New()
	if err != nil {
		return nil, err
	}
	return decodeSecret(secret)
}

func decodeSecret(secret string) ([]byte, error) {
	value, err := base64.RawURLEncoding.DecodeString(secret)
	if err != nil || len(value) != 32 || base64.RawURLEncoding.EncodeToString(value) != secret {
		return nil, errors.New("secret generator must return exactly 256 bits as unpadded base64url")
	}
	return value, nil
}

func decodeCeremony(ceremony Ceremony) (ceremonyState, error) {
	var state ceremonyState
	err := json.Unmarshal(ceremony.SessionData, &state)
	return state, err
}

func registrationProtocolError() error {
	return fmt.Errorf("%w: invalid passkey registration", auth.ErrValidation)
}

func equalBytes(left, right []byte) bool {
	if len(left) != len(right) {
		return false
	}
	var difference byte
	for index := range left {
		difference |= left[index] ^ right[index]
	}
	return difference == 0
}

func updatedCredentialFlags(stored byte, flags webauthn.CredentialFlags) byte {
	mutableMask := byte(protocol.FlagUserPresent | protocol.FlagUserVerified | protocol.FlagBackupEligible | protocol.FlagBackupState)
	updated := stored &^ mutableMask
	if flags.UserPresent {
		updated |= byte(protocol.FlagUserPresent)
	}
	if flags.UserVerified {
		updated |= byte(protocol.FlagUserVerified)
	}
	if flags.BackupEligible {
		updated |= byte(protocol.FlagBackupEligible)
	}
	if flags.BackupState {
		updated |= byte(protocol.FlagBackupState)
	}
	return updated
}

func credentialFromWebAuthn(id uuid.UUID, accountDID, name string, source webauthn.Credential, createdAt time.Time) Credential {
	transports := make([]string, len(source.Transport))
	for index, transport := range source.Transport {
		transports[index] = string(transport)
	}
	return Credential{
		ID: id, AccountDID: accountDID, Name: name, CredentialID: source.ID, PublicKey: source.PublicKey,
		AttestationType: source.AttestationType, Transports: transports, Flags: byte(source.Flags.ProtocolValue()),
		AAGUID: source.Authenticator.AAGUID, SignCount: source.Authenticator.SignCount,
		CloneWarning: source.Authenticator.CloneWarning, Attachment: string(source.Authenticator.Attachment),
		AttestationClientDataJSON: source.Attestation.ClientDataJSON, AttestationClientDataHash: source.Attestation.ClientDataHash,
		AttestationAuthenticatorData:  source.Attestation.AuthenticatorData,
		AttestationPublicKeyAlgorithm: source.Attestation.PublicKeyAlgorithm, AttestationObject: source.Attestation.Object,
		CreatedAt: createdAt,
	}
}
