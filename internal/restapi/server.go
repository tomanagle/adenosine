// Package restapi adapts the public HTTP protocol to application services.
package restapi

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	"github.com/adenosine-dev/adenosine/api"
	generated "github.com/adenosine-dev/adenosine/api/generated/go"
	"github.com/adenosine-dev/adenosine/internal/requestcontext"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
)

type readinessChecker interface {
	Ping(context.Context) error
}

// NewServer creates the public HTTP server with health and telemetry middleware.
func NewServer(addr, baseURL string, readiness readinessChecker, logger *slog.Logger, deps Dependencies, gitHTTP http.Handler) (*http.Server, error) {
	meter := otel.Meter("github.com/adenosine-dev/adenosine/internal/restapi")
	requestCount, err := meter.Int64Counter("adenosine.http.server.requests")
	if err != nil {
		return nil, err
	}
	requestDuration, err := meter.Float64Histogram("adenosine.http.server.duration", metric.WithUnit("s"))
	if err != nil {
		return nil, err
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /openapi.json", serveOpenAPI("application/json"))
	mux.HandleFunc("GET /openapi.yaml", serveOpenAPI("application/yaml"))
	mux.HandleFunc("GET /docs/api", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(apiDocsHTML))
	})
	if deps.Federation != nil && deps.Federation.Processor != nil {
		mux.Handle("/internal/federation/tap", newTapWebhookHandler(
			deps.Federation.Processor,
			deps.Federation.TapAdminPassword,
		))
	}
	handler := newAPIHandler(baseURL, readiness, logger, deps)
	generated.HandlerWithOptions(handler, generated.StdHTTPServerOptions{
		BaseRouter: mux,
		ErrorHandlerFunc: func(w http.ResponseWriter, r *http.Request, err error) {
			handler.writeMalformed(w, r, err)
		},
	})
	if gitHTTP != nil {
		mux.Handle("/", gitHTTP)
	}

	rootHandler := requestMiddleware(logger, requestCount, requestDuration, mux)
	rootHandler = otelhttp.NewHandler(rootHandler, "http.server")
	return &http.Server{
		Addr:              addr,
		Handler:           rootHandler,
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       60 * time.Second,
	}, nil
}

func serveOpenAPI(contentType string) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", contentType)
		_, _ = w.Write(api.OpenAPI)
	}
}

const apiDocsHTML = `<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>Adenosine API</title>
</head>
<body>
  <script id="api-reference" data-url="/openapi.json"></script>
  <script src="https://cdn.jsdelivr.net/npm/@scalar/api-reference"></script>
</body>
</html>`

func requestMiddleware(logger *slog.Logger, count metric.Int64Counter, duration metric.Float64Histogram, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		started := time.Now()
		requestID := newRequestID()
		w.Header().Set("X-Request-ID", requestID)
		response := &responseWriter{ResponseWriter: w, status: http.StatusOK}
		requestContext := context.WithValue(r.Context(), requestIDContextKey{}, requestID)
		requestContext = requestcontext.WithRequestID(requestContext, requestID)
		request := r.WithContext(requestContext)
		next.ServeHTTP(response, request)

		route := request.Pattern
		if route == "" {
			route = "unmatched"
		}

		attrs := metric.WithAttributes(
			attribute.String("http.request.method", r.Method),
			attribute.Int("http.response.status_code", response.status),
			attribute.String("http.route", route),
		)
		count.Add(request.Context(), 1, attrs)
		duration.Record(request.Context(), time.Since(started).Seconds(), attrs)

		spanContext := trace.SpanContextFromContext(request.Context())
		logger.InfoContext(request.Context(), "http request",
			"component", "restapi",
			"request_id", requestID,
			"trace_id", spanContext.TraceID().String(),
			"span_id", spanContext.SpanID().String(),
			"method", r.Method,
			"route", route,
			"status", response.status,
			"duration_ms", time.Since(started).Milliseconds(),
		)
	})
}

type requestIDContextKey struct{}

func requestIDFromContext(ctx context.Context) string {
	requestID, _ := ctx.Value(requestIDContextKey{}).(string)
	return requestID
}

type responseWriter struct {
	http.ResponseWriter
	status      int
	wroteHeader bool
}

func (w *responseWriter) WriteHeader(status int) {
	if w.wroteHeader {
		return
	}
	w.status = status
	w.wroteHeader = true
	w.ResponseWriter.WriteHeader(status)
}

func (w *responseWriter) Write(body []byte) (int, error) {
	if !w.wroteHeader {
		w.WriteHeader(http.StatusOK)
	}
	return w.ResponseWriter.Write(body)
}

func (w *responseWriter) Unwrap() http.ResponseWriter {
	return w.ResponseWriter
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func newRequestID() string {
	var bytes [16]byte
	if _, err := rand.Read(bytes[:]); err != nil {
		return "unavailable"
	}
	return hex.EncodeToString(bytes[:])
}
