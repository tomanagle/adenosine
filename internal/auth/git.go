package auth

import (
	"context"
	"crypto/sha256"
	"fmt"
	"net/http"
	"slices"

	"github.com/adenosine-dev/adenosine/internal/repository"
)

type repositoryPermissionChecker interface {
	CanWriteRepository(context.Context, string, repository.ID) (bool, error)
}

// GitAuthorizer authenticates Git HTTP credentials and checks local repository permissions.
type GitAuthorizer struct {
	tokens      accessTokenStore
	permissions repositoryPermissionChecker
	clock       tokenClock
}

// NewGitAuthorizer constructs a credential-backed Git authorizer.
func NewGitAuthorizer(tokens accessTokenStore, permissions repositoryPermissionChecker, clock tokenClock) *GitAuthorizer {
	return &GitAuthorizer{tokens: tokens, permissions: permissions, clock: clock}
}

// AuthorizeWrite accepts a personal access token as the HTTP Basic password.
func (authorizer *GitAuthorizer) AuthorizeWrite(ctx context.Context, request *http.Request, repo repository.Repository) error {
	_, plaintext, ok := request.BasicAuth()
	if !ok || plaintext == "" {
		return ErrUnauthorized
	}
	hash := sha256.Sum256([]byte(plaintext))
	token, err := authorizer.tokens.GetActiveTokenByHash(ctx, hash[:])
	if err != nil {
		return fmt.Errorf("authenticate Git token: %w", err)
	}
	if !slices.Contains(token.Scopes, ScopeRepositoryWrite) {
		return ErrForbidden
	}
	if token.RepositoryID != nil && *token.RepositoryID != repo.ID {
		return ErrForbidden
	}
	allowed, err := authorizer.permissions.CanWriteRepository(ctx, token.AccountDID, repo.ID)
	if err != nil {
		return fmt.Errorf("authorize repository write: %w", err)
	}
	if !allowed {
		return ErrForbidden
	}
	if err := authorizer.tokens.TouchToken(ctx, token.ID, authorizer.clock.Now().UTC()); err != nil {
		return fmt.Errorf("record token use: %w", err)
	}
	return nil
}
