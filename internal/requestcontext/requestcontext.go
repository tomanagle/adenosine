// Package requestcontext carries bounded request metadata into application services.
package requestcontext

import "context"

type requestIDKey struct{}

// WithRequestID returns a context carrying the public request correlation identifier.
func WithRequestID(ctx context.Context, requestID string) context.Context {
	return context.WithValue(ctx, requestIDKey{}, requestID)
}

// RequestID returns the correlation identifier when the operation originated in HTTP.
func RequestID(ctx context.Context) string {
	requestID, _ := ctx.Value(requestIDKey{}).(string)
	return requestID
}
