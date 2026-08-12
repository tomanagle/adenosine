package observability

import (
	"context"
	"strings"
	"testing"

	"go.opentelemetry.io/otel/attribute"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.34.0"
	"go.opentelemetry.io/otel/trace"
)

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
