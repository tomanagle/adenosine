package observability

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"go.opentelemetry.io/otel/attribute"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.34.0"
	"go.opentelemetry.io/otel/trace"
)

func TestMetricsRecordDurationsAndOutcomes(t *testing.T) {
	cause := errors.New("failed")
	testCases := []struct {
		name       string
		metricName string
		record     func(*Metrics)
		want       map[attribute.Key]string
	}{
		{name: "HTTP success includes client errors", metricName: "http.server.request.duration", record: func(metrics *Metrics) {
			metrics.RecordHTTPRequest(context.Background(), http.MethodGet, "GET /api/v1/profiles/{did}", http.StatusNotFound, 25*time.Millisecond)
		}, want: map[attribute.Key]string{"http.request.method": http.MethodGet, "http.route": "GET /api/v1/profiles/{did}", "outcome": outcomeSuccess}},
		{name: "HTTP server error", metricName: "http.server.request.duration", record: func(metrics *Metrics) {
			metrics.RecordHTTPRequest(context.Background(), http.MethodPost, "POST /api/v1/repositories", http.StatusServiceUnavailable, 50*time.Millisecond)
		}, want: map[attribute.Key]string{"error.type": "503", "outcome": outcomeError}},
		{name: "unknown HTTP method is bounded", metricName: "http.server.request.duration", record: func(metrics *Metrics) {
			metrics.RecordHTTPRequest(context.Background(), "BREW", "unmatched", http.StatusNotFound, time.Millisecond)
		}, want: map[attribute.Key]string{"http.request.method": "_OTHER", "outcome": outcomeSuccess}},
		{name: "database success", metricName: "db.client.operation.duration", record: func(metrics *Metrics) {
			metrics.RecordDatabaseCall(context.Background(), "GetProfile", "select", 5*time.Millisecond, nil)
		}, want: map[attribute.Key]string{"adenosine.db.caller": "GetProfile", "db.operation.name": "select", "db.system.name": "postgresql", "outcome": outcomeSuccess}},
		{name: "database error", metricName: "db.client.operation.duration", record: func(metrics *Metrics) {
			metrics.RecordDatabaseCall(context.Background(), "UpdateProfile", "update", time.Second, cause)
		}, want: map[attribute.Key]string{"error.type": "database_error", "outcome": outcomeError}},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			reader := sdkmetric.NewManualReader()
			provider := sdkmetric.NewMeterProvider(
				sdkmetric.WithReader(reader),
				sdkmetric.WithView(durationHistogramView("http.server.request.duration", httpDurationBoundaries,
					"http.request.method", "http.route", "http.response.status_code", "error.type", "outcome")),
				sdkmetric.WithView(durationHistogramView("db.client.operation.duration", databaseDurationBoundaries,
					"db.system.name", "db.operation.name", "adenosine.db.caller", "error.type", "outcome")),
			)
			t.Cleanup(func() { _ = provider.Shutdown(context.Background()) })
			metrics, err := NewMetrics(provider.Meter("test"))
			if err != nil {
				t.Fatalf("NewMetrics() error = %v", err)
			}
			testCase.record(metrics)

			var collected metricdata.ResourceMetrics
			if err := reader.Collect(context.Background(), &collected); err != nil {
				t.Fatalf("collect metrics: %v", err)
			}
			point, unit := histogramPoint(collected, testCase.metricName)
			if point == nil {
				t.Fatalf("metric %q has no histogram point", testCase.metricName)
			}
			if unit != "s" || point.Count != 1 || len(point.Bounds) == 0 {
				t.Fatalf("metric unit/count/bounds = %q/%d/%v", unit, point.Count, point.Bounds)
			}
			for key, want := range testCase.want {
				got, found := point.Attributes.Value(key)
				if !found || got.Emit() != want {
					t.Errorf("attribute %q = %q, want %q", key, got.Emit(), want)
				}
			}
		})
	}
}

func TestPrometheusHandlerExposesApplicationHistograms(t *testing.T) {
	testCases := []struct{ name string }{{name: "OpenMetrics scrape"}}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "")
			telemetry, err := setup(context.Background())
			if err != nil {
				t.Fatalf("setup() error = %v", err)
			}
			t.Cleanup(func() { _ = telemetry.Shutdown(context.Background()) })
			telemetry.Metrics.RecordHTTPRequest(context.Background(), http.MethodGet, "GET /health/ready", http.StatusOK, 10*time.Millisecond)
			telemetry.Metrics.RecordDatabaseCall(context.Background(), "Ping", "select", 2*time.Millisecond, nil)

			request := httptest.NewRequest(http.MethodGet, "/metrics", nil)
			request.Header.Set("Accept", "application/openmetrics-text")
			response := httptest.NewRecorder()
			telemetry.PrometheusHandler.ServeHTTP(response, request)
			body := response.Body.String()
			if response.Code != http.StatusOK {
				t.Fatalf("scrape status = %d: %s", response.Code, body)
			}
			for _, fragment := range []string{
				"http_server_request_duration_seconds_bucket",
				`http_route="GET /health/ready"`,
				`outcome="success"`,
				"db_client_operation_duration_seconds_bucket",
				`adenosine_db_caller="Ping"`,
			} {
				if !strings.Contains(body, fragment) {
					t.Errorf("scrape does not contain %q", fragment)
				}
			}
		})
	}
}

func histogramPoint(metrics metricdata.ResourceMetrics, name string) (*metricdata.HistogramDataPoint[float64], string) {
	for _, scope := range metrics.ScopeMetrics {
		for _, current := range scope.Metrics {
			if current.Name != name {
				continue
			}
			if histogram, ok := current.Data.(metricdata.Histogram[float64]); ok && len(histogram.DataPoints) != 0 {
				return &histogram.DataPoints[0], current.Unit
			}
		}
	}
	return nil, ""
}

func TestSamplerFromEnv(t *testing.T) {
	testCases := []struct {
		name       string
		sampler    string
		argument   string
		parent     *trace.SpanContext
		wantSample bool
		wantErr    string
	}{
		{name: "default parent based always on", wantSample: true},
		{name: "always off", sampler: "always_off"},
		{name: "ratio one", sampler: "traceidratio", argument: "1", wantSample: true},
		{name: "ratio zero", sampler: "traceidratio", argument: "0"},
		{name: "parent based honors sampled parent", sampler: "parentbased_always_off", parent: sampledParent(), wantSample: true},
		{name: "missing ratio", sampler: "traceidratio", wantErr: "OTEL_TRACES_SAMPLER_ARG"},
		{name: "invalid ratio", sampler: "traceidratio", argument: "2", wantErr: "OTEL_TRACES_SAMPLER_ARG"},
		{name: "NaN ratio", sampler: "traceidratio", argument: "NaN", wantErr: "OTEL_TRACES_SAMPLER_ARG"},
		{name: "positive infinity ratio", sampler: "traceidratio", argument: "+Inf", wantErr: "OTEL_TRACES_SAMPLER_ARG"},
		{name: "negative infinity ratio", sampler: "traceidratio", argument: "-Inf", wantErr: "OTEL_TRACES_SAMPLER_ARG"},
		{name: "unsupported sampler", sampler: "jaeger_remote", wantErr: "unsupported OTEL_TRACES_SAMPLER"},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Setenv("OTEL_TRACES_SAMPLER", testCase.sampler)
			t.Setenv("OTEL_TRACES_SAMPLER_ARG", testCase.argument)
			sampler, err := samplerFromEnv()
			if testCase.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), testCase.wantErr) {
					t.Fatalf("samplerFromEnv() error = %v", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("samplerFromEnv() error = %v", err)
			}
			ctx := context.Background()
			if testCase.parent != nil {
				ctx = trace.ContextWithRemoteSpanContext(ctx, *testCase.parent)
			}
			result := sampler.ShouldSample(sdktrace.SamplingParameters{ParentContext: ctx, TraceID: trace.TraceID{1}, Name: "test"})
			if sampled := result.Decision == sdktrace.RecordAndSample; sampled != testCase.wantSample {
				t.Fatalf("sampled = %t, want %t", sampled, testCase.wantSample)
			}
		})
	}
}

func TestResourceEnvironmentPrecedence(t *testing.T) {
	testCases := []struct {
		name            string
		attributes      string
		serviceName     string
		wantService     string
		wantVersion     string
		wantInstance    string
		wantEnvironment string
	}{
		{name: "resource attributes override fallbacks", attributes: "service.name=resource-name,service.version=resource-version,service.instance.id=resource-instance,deployment.environment.name=production", wantService: "resource-name", wantVersion: "resource-version", wantInstance: "resource-instance", wantEnvironment: "production"},
		{name: "service name variable has highest precedence", attributes: "service.name=resource-name,service.version=resource-version", serviceName: "explicit-name", wantService: "explicit-name", wantVersion: "resource-version"},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Setenv("OTEL_RESOURCE_ATTRIBUTES", testCase.attributes)
			t.Setenv("OTEL_SERVICE_NAME", testCase.serviceName)
			res, err := buildResource(context.Background())
			if err != nil {
				t.Fatalf("buildResource() error = %v", err)
			}
			values := resourceValues(res)
			if values[string(semconv.ServiceNameKey)] != testCase.wantService || values[string(semconv.ServiceVersionKey)] != testCase.wantVersion {
				t.Fatalf("service identity = %#v", values)
			}
			if testCase.wantInstance != "" && values[string(semconv.ServiceInstanceIDKey)] != testCase.wantInstance {
				t.Fatalf("instance = %q", values[string(semconv.ServiceInstanceIDKey)])
			}
			if testCase.wantEnvironment != "" && values[string(semconv.DeploymentEnvironmentNameKey)] != testCase.wantEnvironment {
				t.Fatalf("environment = %q", values[string(semconv.DeploymentEnvironmentNameKey)])
			}
		})
	}
}

func resourceValues(res interface{ Attributes() []attribute.KeyValue }) map[string]string {
	values := map[string]string{}
	for _, value := range res.Attributes() {
		if value.Value.Type() == attribute.STRING {
			values[string(value.Key)] = value.Value.AsString()
		}
	}
	return values
}

func sampledParent() *trace.SpanContext {
	value := trace.NewSpanContext(trace.SpanContextConfig{TraceID: trace.TraceID{1}, SpanID: trace.SpanID{1}, TraceFlags: trace.FlagsSampled, Remote: true})
	return &value
}
