// Package auth contains authentication and authorization capabilities shared by transports.
package auth

import "errors"

var (
	// ErrUnauthorized indicates that valid authentication is required.
	ErrUnauthorized = errors.New("authentication required")
	// ErrForbidden indicates that an authenticated identity lacks permission.
	ErrForbidden = errors.New("permission denied")
	// ErrNotFound indicates that a requested account or owned active credential was not found.
	ErrNotFound = errors.New("credential not found")
	// ErrConflict indicates that credential material is already registered.
	ErrConflict = errors.New("credential conflict")
	// ErrValidation indicates invalid credential input.
	ErrValidation = errors.New("credential validation failed")
)
