package passkey

import (
	"context"
	"encoding/json"
	"time"

	"github.com/adenosine-dev/adenosine/internal/auth"
	"github.com/go-webauthn/webauthn/protocol"
	"github.com/go-webauthn/webauthn/webauthn"
	"github.com/google/uuid"
)

const (
	ceremonyRegistration   = "registration"
	ceremonyAuthentication = "authentication"
)

// Store owns durable WebAuthn users, credentials, and one-time ceremonies.
type Store interface {
	CreateUser(context.Context, string, User, time.Time) (User, error)
	GetUser(context.Context, string, string) (User, error)
	CreateCredential(context.Context, string, Credential) (Credential, error)
	GetCredentialByCredentialID(context.Context, string, []byte) (Credential, error)
	UpdateCredential(context.Context, string, Credential, time.Time) (Credential, error)
	ListCredentials(context.Context, string, string) ([]Credential, error)
	RevokeCredential(context.Context, string, string, uuid.UUID, time.Time) error
	CreateCeremony(context.Context, Ceremony) (Ceremony, error)
	ConsumeCeremony(context.Context, []byte, time.Time) (Ceremony, error)
	PurgeExpiredCeremonies(context.Context, time.Time) error
	TouchAccountLogin(context.Context, string, time.Time) error
}

// SessionIssuer issues the application's existing local browser session.
type SessionIssuer interface {
	CreateSession(context.Context, string) (auth.Session, string, error)
}

// Clock supplies domain time.
type Clock interface {
	Now() time.Time
}

// IDGenerator creates credential record identifiers.
type IDGenerator interface {
	New() (uuid.UUID, error)
}

// SecretGenerator creates 256-bit, unpadded base64url secrets.
type SecretGenerator interface {
	New() (string, error)
}

// BeginResult contains browser-consumable WebAuthn JSON and its opaque one-time token.
type BeginResult struct {
	Options json.RawMessage
	Token   string
}

// CredentialSummary is the owner-visible passkey metadata.
type CredentialSummary struct {
	ID         uuid.UUID
	Name       string
	CreatedAt  time.Time
	LastUsedAt *time.Time
}

// LoginResult contains the verified DID and newly issued local session.
type LoginResult struct {
	DID              string
	SessionID        uuid.UUID
	SessionPlaintext string
	SessionExpiresAt time.Time
}

type ceremonyState struct {
	Session        webauthn.SessionData `json:"session"`
	CredentialName string               `json:"credential_name,omitempty"`
}

type webAuthnUser struct{ User }

func (user webAuthnUser) WebAuthnID() []byte          { return user.Handle }
func (user webAuthnUser) WebAuthnName() string        { return user.Name }
func (user webAuthnUser) WebAuthnDisplayName() string { return user.DisplayName }
func (user webAuthnUser) WebAuthnCredentials() []webauthn.Credential {
	credentials := make([]webauthn.Credential, len(user.Credentials))
	for index, credential := range user.Credentials {
		credentials[index] = credential.webAuthn()
	}
	return credentials
}

func (credential Credential) webAuthn() webauthn.Credential {
	transports := make([]protocol.AuthenticatorTransport, len(credential.Transports))
	for index, transport := range credential.Transports {
		transports[index] = protocol.AuthenticatorTransport(transport)
	}
	return webauthn.Credential{
		ID: credential.CredentialID, PublicKey: credential.PublicKey,
		AttestationType: credential.AttestationType, Transport: transports,
		Flags: webauthn.NewCredentialFlags(protocol.AuthenticatorFlags(credential.Flags)),
		Authenticator: webauthn.Authenticator{
			AAGUID: credential.AAGUID, SignCount: credential.SignCount,
			CloneWarning: credential.CloneWarning, Attachment: protocol.AuthenticatorAttachment(credential.Attachment),
		},
		Attestation: webauthn.CredentialAttestation{
			ClientDataJSON: credential.AttestationClientDataJSON, ClientDataHash: credential.AttestationClientDataHash,
			AuthenticatorData:  credential.AttestationAuthenticatorData,
			PublicKeyAlgorithm: credential.AttestationPublicKeyAlgorithm, Object: credential.AttestationObject,
		},
	}
}

func credentialSummary(credential Credential) CredentialSummary {
	return CredentialSummary{ID: credential.ID, Name: credential.Name, CreatedAt: credential.CreatedAt, LastUsedAt: credential.LastUsedAt}
}
