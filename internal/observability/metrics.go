package observability

import (
	"context"
	"strconv"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

const (
	outcomeSuccess = "success"
	outcomeError   = "error"
)

var (
	httpDurationBoundaries     = []float64{0.005, 0.01, 0.025, 0.05, 0.075, 0.1, 0.25, 0.5, 0.75, 1, 2.5, 5, 7.5, 10}
	databaseDurationBoundaries = []float64{0.001, 0.005, 0.01, 0.05, 0.1, 0.5, 1, 5, 10}
)

// Metrics is the process-wide implementation of the consumer-owned HTTP and
// database metrics interfaces.
type Metrics struct {
	httpDuration     metric.Float64Histogram
	databaseDuration metric.Float64Histogram
}

// NewMetrics constructs the bounded HTTP and database metric instruments.
func NewMetrics(meter metric.Meter) (*Metrics, error) {
	httpDuration, err := meter.Float64Histogram(
		"http.server.request.duration",
		metric.WithDescription("Duration of HTTP server requests."),
		metric.WithUnit("s"),
	)
	if err != nil {
		return nil, err
	}
	databaseDuration, err := meter.Float64Histogram(
		"db.client.operation.duration",
		metric.WithDescription("Duration of database client operations."),
		metric.WithUnit("s"),
	)
	if err != nil {
		return nil, err
	}
	return &Metrics{httpDuration: httpDuration, databaseDuration: databaseDuration}, nil
}

// RecordHTTPRequest records one completed request. HTTP 5xx responses are
// errors; 1xx through 4xx responses are successful server handling outcomes.
func (metrics *Metrics) RecordHTTPRequest(ctx context.Context, method, route string, status int, elapsed time.Duration) {
	outcome := outcomeSuccess
	attributes := []attribute.KeyValue{
		attribute.String("http.request.method", boundedHTTPMethod(method)),
		attribute.String("http.route", route),
		attribute.Int("http.response.status_code", status),
	}
	if status >= 500 {
		outcome = outcomeError
		attributes = append(attributes, attribute.String("error.type", strconv.Itoa(status)))
	}
	attributes = append(attributes, attribute.String("outcome", outcome))
	metrics.httpDuration.Record(ctx, elapsed.Seconds(), metric.WithAttributes(attributes...))
}

func boundedHTTPMethod(method string) string {
	switch method {
	case "CONNECT", "DELETE", "GET", "HEAD", "OPTIONS", "PATCH", "POST", "PUT", "TRACE":
		return method
	default:
		return "_OTHER"
	}
}

// RecordDatabaseCall records one completed database call. Caller and operation
// are bounded values derived at the shared database boundary; SQL is never an
// attribute.
func (metrics *Metrics) RecordDatabaseCall(ctx context.Context, caller, operation string, elapsed time.Duration, callErr error) {
	outcome := outcomeSuccess
	attributes := []attribute.KeyValue{
		attribute.String("db.system.name", "postgresql"),
		attribute.String("db.operation.name", operation),
		attribute.String("adenosine.db.caller", caller),
	}
	if callErr != nil {
		outcome = outcomeError
		attributes = append(attributes, attribute.String("error.type", "database_error"))
	}
	attributes = append(attributes, attribute.String("outcome", outcome))
	metrics.databaseDuration.Record(ctx, elapsed.Seconds(), metric.WithAttributes(attributes...))
}
