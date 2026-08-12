package observability

import (
	"context"
	"log/slog"

	"go.opentelemetry.io/otel/trace"
)

// CorrelationAttrs returns log fields for the valid span in ctx.
func CorrelationAttrs(ctx context.Context) []any {
	span := trace.SpanContextFromContext(ctx)
	if !span.IsValid() {
		return nil
	}
	return []any{slog.String("trace_id", span.TraceID().String()), slog.String("span_id", span.SpanID().String())}
}
