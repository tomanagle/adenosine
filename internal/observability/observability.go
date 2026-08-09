// Package observability configures vendor-neutral OpenTelemetry providers.
package observability

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetrichttp"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	"go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.34.0"
)

// Shutdown flushes all telemetry providers.
type Shutdown func(context.Context) error

// Setup configures structured stdout logging and optional OTLP export. Export
// failures are reported at startup but never make request handling depend on
// the collector's availability.
func Setup(ctx context.Context) (*slog.Logger, Shutdown, error) {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(logger)

	serviceName := os.Getenv("OTEL_SERVICE_NAME")
	if serviceName == "" {
		serviceName = "adenosine"
	}
	res, err := resource.New(ctx,
		resource.WithFromEnv(),
		resource.WithTelemetrySDK(),
		resource.WithAttributes(semconv.ServiceName(serviceName)),
	)
	if err != nil {
		return nil, nil, err
	}

	traceOptions := []trace.TracerProviderOption{trace.WithResource(res)}
	metricOptions := []metric.Option{metric.WithResource(res)}
	if os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT") != "" {
		traceExporter, err := otlptracehttp.New(ctx)
		if err != nil {
			return nil, nil, err
		}
		metricExporter, err := otlpmetrichttp.New(ctx)
		if err != nil {
			return nil, nil, err
		}
		traceOptions = append(traceOptions, trace.WithBatcher(traceExporter))
		metricOptions = append(metricOptions, metric.WithReader(metric.NewPeriodicReader(metricExporter,
			metric.WithInterval(10*time.Second),
		)))
	}

	tracerProvider := trace.NewTracerProvider(traceOptions...)
	meterProvider := metric.NewMeterProvider(metricOptions...)
	otel.SetTracerProvider(tracerProvider)
	otel.SetMeterProvider(meterProvider)
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))

	shutdown := func(ctx context.Context) error {
		return errors.Join(
			meterProvider.Shutdown(ctx),
			tracerProvider.Shutdown(ctx),
		)
	}
	return logger, shutdown, nil
}
