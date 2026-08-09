// Package atproto adapts Indigo's AT Protocol OAuth client.
package atproto

import "errors"

var (
	// ErrInvalidIdentifier indicates that login input is not an AT Protocol handle or DID.
	ErrInvalidIdentifier = errors.New("invalid AT Protocol identifier")
	// ErrProviderFailure indicates that the OAuth provider could not start a login.
	ErrProviderFailure = errors.New("AT Protocol provider failure")
	// ErrCallbackFailure indicates that an OAuth callback could not be verified or completed.
	ErrCallbackFailure = errors.New("AT Protocol callback failure")
	// ErrStateNotFound indicates that OAuth state is absent, expired, or already consumed.
	ErrStateNotFound = errors.New("OAuth state not found")
	// ErrStateInvalid indicates that encrypted OAuth state failed authentication or validation.
	ErrStateInvalid = errors.New("OAuth state invalid")
	// ErrSessionNotFound indicates that an OAuth session is absent.
	ErrSessionNotFound = errors.New("OAuth session not found")
	// ErrSessionInvalid indicates that encrypted OAuth credentials failed authentication or validation.
	ErrSessionInvalid = errors.New("OAuth session invalid")
)

// ProviderError preserves a provider failure for errors.Is/errors.As without
// rendering provider details that could contain sensitive values.
type ProviderError struct {
	Operation string
	Err       error
}

func (err *ProviderError) Error() string {
	return ErrProviderFailure.Error() + " during " + err.Operation
}

func (err *ProviderError) Unwrap() []error { return []error{ErrProviderFailure, err.Err} }

// CallbackError preserves a callback failure without rendering callback values.
type CallbackError struct {
	Operation string
	Err       error
}

func (err *CallbackError) Error() string {
	return ErrCallbackFailure.Error() + " during " + err.Operation
}

func (err *CallbackError) Unwrap() []error { return []error{ErrCallbackFailure, err.Err} }
