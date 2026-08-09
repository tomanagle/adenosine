package auth

import (
	"context"
	"net/http"

	"github.com/adenosine-dev/adenosine/internal/repository"
)

// DenyWrites rejects mutations until a credential-backed authorizer is configured.
type DenyWrites struct{}

// AuthorizeWrite requires authentication for every repository write.
func (DenyWrites) AuthorizeWrite(context.Context, *http.Request, repository.Repository) error {
	return ErrUnauthorized
}
