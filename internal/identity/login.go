// Package identity orchestrates external identity login into local accounts and sessions.
package identity

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"time"

	"github.com/adenosine-dev/adenosine/internal/auth"
	"github.com/google/uuid"
)

var ErrInvalidReturnTo = errors.New("login return target is not supported")

// OAuthIdentity is the verified, credential-free result of provider authentication.
type OAuthIdentity struct {
	DID    string
	Handle string
}

// OAuthCredentialGrant is an opaque, single-use handle to provider credentials.
// Implementations keep raw credentials inside the provider package.
type OAuthCredentialGrant interface {
	Persist(context.Context) error
	Discard(context.Context) error
}

type oauthProvider interface {
	Start(context.Context, string) (string, error)
	Complete(context.Context, url.Values) (OAuthIdentity, OAuthCredentialGrant, error)
}

type accountLoginStore interface {
	UpsertLogin(context.Context, string, string, time.Time) (auth.Account, error)
}

type localSessionIssuer interface {
	CreateSession(context.Context, string) (auth.Session, string, error)
}

type loginClock interface {
	Now() time.Time
}

// LoginResult contains only verified identity and a newly issued local session.
type LoginResult struct {
	DID              string
	Handle           string
	SessionID        uuid.UUID
	SessionExpiresAt time.Time
	SessionPlaintext string
}

// LoginService turns one-time provider authentication into a local account session.
type LoginService struct {
	provider oauthProvider
	accounts accountLoginStore
	sessions localSessionIssuer
	clock    loginClock
}

// NewLoginService constructs the OAuth login orchestration service.
func NewLoginService(provider oauthProvider, accounts accountLoginStore, sessions localSessionIssuer, clock loginClock) *LoginService {
	return &LoginService{provider: provider, accounts: accounts, sessions: sessions, clock: clock}
}

// Start begins provider login. Return redirects are intentionally unsupported in this slice.
func (service *LoginService) Start(ctx context.Context, identifier, returnTo string) (string, error) {
	if returnTo != "" {
		return "", ErrInvalidReturnTo
	}
	redirectURL, err := service.provider.Start(ctx, identifier)
	if err != nil {
		return "", fmt.Errorf("start OAuth login: %w", err)
	}
	return redirectURL, nil
}

// Complete records the account, persists provider credentials, then issues a local session.
func (service *LoginService) Complete(ctx context.Context, values url.Values) (LoginResult, error) {
	externalIdentity, credentialGrant, err := service.provider.Complete(ctx, values)
	if err != nil {
		return LoginResult{}, fmt.Errorf("complete OAuth login: %w", err)
	}
	if credentialGrant == nil {
		return LoginResult{}, errors.New("complete OAuth login: provider returned no credential grant")
	}
	if externalIdentity.DID == "" {
		_ = credentialGrant.Discard(ctx)
		return LoginResult{}, errors.New("complete OAuth login: provider returned an empty DID")
	}
	if _, err := service.accounts.UpsertLogin(ctx, externalIdentity.DID, externalIdentity.Handle, service.clock.Now().UTC()); err != nil {
		_ = credentialGrant.Discard(ctx)
		return LoginResult{}, fmt.Errorf("record OAuth login: %w", err)
	}
	if err := credentialGrant.Persist(ctx); err != nil {
		_ = credentialGrant.Discard(ctx)
		return LoginResult{}, fmt.Errorf("persist OAuth credentials: %w", err)
	}
	session, plaintext, err := service.sessions.CreateSession(ctx, externalIdentity.DID)
	if err != nil {
		_ = credentialGrant.Discard(ctx)
		return LoginResult{}, fmt.Errorf("issue local session: %w", err)
	}
	return LoginResult{
		DID:              externalIdentity.DID,
		Handle:           externalIdentity.Handle,
		SessionID:        session.ID,
		SessionExpiresAt: session.ExpiresAt,
		SessionPlaintext: plaintext,
	}, nil
}
