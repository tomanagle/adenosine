// Package observability configures vendor-neutral OpenTelemetry providers.
package observability

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"net/http"
	"os"
	"runtime/debug"
	"strconv"
	"strings"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetrichttp"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	otelprometheus "go.opentelemetry.io/otel/exporters/prometheus"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	"go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.34.0"
)

const shutdownTimeout = 5 * time.Second

// Shutdown flushes all telemetry providers.
type Shutdown func(context.Context) error

// Telemetry contains the process-wide observability dependencies composed at
// startup. Consumers depend on their own small metrics interfaces rather than
// this concrete type.
type Telemetry struct {
	Logger            *slog.Logger
	Metrics           *Metrics
	PrometheusHandler http.Handler
	Shutdown          Shutdown
}

// Must configures telemetry or panics because the service cannot start with a
// partially initialized telemetry pipeline.
func Must(ctx context.Context) *Telemetry {
	telemetry, err := setup(ctx)
	if err != nil {
		panic(err)
	}
	return telemetry
}

func setup(ctx context.Context) (*Telemetry, error) {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(logger)

	res, err := buildResource(ctx)
	if err != nil {
		return nil, err
	}

	sampler, err := samplerFromEnv()
	if err != nil {
		return nil, err
	}
	registry := prometheus.NewRegistry()
	prometheusExporter, err := otelprometheus.New(otelprometheus.WithRegisterer(registry))
	if err != nil {
		return nil, fmt.Errorf("create Prometheus exporter: %w", err)
	}
	traceOptions := []trace.TracerProviderOption{trace.WithResource(res), trace.WithSampler(sampler)}
	metricOptions := []metric.Option{
		metric.WithResource(res),
		metric.WithReader(prometheusExporter),
		metric.WithView(durationHistogramView("http.server.request.duration", httpDurationBoundaries,
			"http.request.method", "http.route", "http.response.status_code", "error.type", "outcome")),
		metric.WithView(durationHistogramView("db.client.operation.duration", databaseDurationBoundaries,
			"db.system.name", "db.operation.name", "adenosine.db.caller", "error.type", "outcome")),
	}
	if os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT") != "" {
		traceExporter, err := otlptracehttp.New(ctx)
		if err != nil {
			return nil, err
		}
		traceOptions = append(traceOptions, trace.WithBatcher(traceExporter))
		if strings.ToLower(strings.TrimSpace(os.Getenv("OTEL_METRICS_EXPORTER"))) != "prometheus" {
			metricExporter, err := otlpmetrichttp.New(ctx)
			if err != nil {
				return nil, err
			}
			metricOptions = append(metricOptions, metric.WithReader(metric.NewPeriodicReader(metricExporter,
				metric.WithInterval(10*time.Second),
			)))
		}
	}

	tracerProvider := trace.NewTracerProvider(traceOptions...)
	meterProvider := metric.NewMeterProvider(metricOptions...)
	metrics, err := NewMetrics(meterProvider.Meter("github.com/adenosine-dev/adenosine/internal/observability"))
	if err != nil {
		_ = meterProvider.Shutdown(ctx)
		_ = tracerProvider.Shutdown(ctx)
		return nil, err
	}
	otel.SetTracerProvider(tracerProvider)
	otel.SetMeterProvider(meterProvider)
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))

	shutdown := func(ctx context.Context) error {
		ctx, cancel := context.WithTimeout(ctx, shutdownTimeout)
		defer cancel()
		return errors.Join(
			meterProvider.Shutdown(ctx),
			tracerProvider.Shutdown(ctx),
		)
	}
	return &Telemetry{
		Logger:  logger,
		Metrics: metrics,
		PrometheusHandler: promhttp.HandlerFor(registry, promhttp.HandlerOpts{
			EnableOpenMetrics: true,
		}),
		Shutdown: shutdown,
	}, nil
}

func durationHistogramView(name string, boundaries []float64, keys ...attribute.Key) metric.View {
	return metric.NewView(
		metric.Instrument{Name: name, Kind: metric.InstrumentKindHistogram},
		metric.Stream{
			Aggregation:     metric.AggregationExplicitBucketHistogram{Boundaries: boundaries, NoMinMax: true},
			AttributeFilter: attribute.NewAllowKeysFilter(keys...),
		},
	)
}

func buildResource(ctx context.Context) (*resource.Resource, error) {
	instanceID, err := instanceID()
	if err != nil {
		return nil, err
	}
	res, err := resource.New(ctx,
		resource.WithAttributes(
			semconv.ServiceName("adenosine"),
			semconv.ServiceVersion(serviceVersion()),
			semconv.ServiceInstanceID(instanceID),
			semconv.DeploymentEnvironmentName(environment()),
		),
		resource.WithFromEnv(),
		resource.WithTelemetrySDK(),
	)
	if err != nil {
		return nil, err
	}
	if serviceName := strings.TrimSpace(os.Getenv("OTEL_SERVICE_NAME")); serviceName != "" {
		res, err = resource.Merge(res, resource.NewSchemaless(semconv.ServiceName(serviceName)))
		if err != nil {
			return nil, fmt.Errorf("apply OTEL_SERVICE_NAME: %w", err)
		}
	}
	return res, nil
}

func serviceVersion() string {
	info, ok := debug.ReadBuildInfo()
	if ok && info.Main.Version != "" && info.Main.Version != "(devel)" {
		return info.Main.Version
	}
	if ok {
		for _, setting := range info.Settings {
			if setting.Key == "vcs.revision" && setting.Value != "" {
				return setting.Value
			}
		}
	}
	return "unknown"
}

func instanceID() (string, error) {
	hostname, err := os.Hostname()
	if err != nil {
		return "", errors.New("determine telemetry service instance ID")
	}
	return hostname, nil
}

func environment() string {
	return "development"
}

func samplerFromEnv() (trace.Sampler, error) {
	name := strings.ToLower(strings.TrimSpace(os.Getenv("OTEL_TRACES_SAMPLER")))
	if name == "" {
		name = "parentbased_always_on"
	}
	switch name {
	case "always_on":
		return trace.AlwaysSample(), nil
	case "always_off":
		return trace.NeverSample(), nil
	case "parentbased_always_on":
		return trace.ParentBased(trace.AlwaysSample()), nil
	case "parentbased_always_off":
		return trace.ParentBased(trace.NeverSample()), nil
	case "traceidratio", "parentbased_traceidratio":
		ratio, err := strconv.ParseFloat(strings.TrimSpace(os.Getenv("OTEL_TRACES_SAMPLER_ARG")), 64)
		if err != nil || math.IsNaN(ratio) || math.IsInf(ratio, 0) || ratio < 0 || ratio > 1 {
			return nil, fmt.Errorf("OTEL_TRACES_SAMPLER_ARG must be a number between 0 and 1 for %s", name)
		}
		sampler := trace.TraceIDRatioBased(ratio)
		if name == "parentbased_traceidratio" {
			return trace.ParentBased(sampler), nil
		}
		return sampler, nil
	default:
		return nil, fmt.Errorf("unsupported OTEL_TRACES_SAMPLER %q", name)
	}
}
