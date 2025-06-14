package observability

import (
	"context"
	"log/slog"
	"os"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/sdk/resource"
	"go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.24.0"
)

// InitTracing initializes OpenTelemetry tracing
func InitTracing(serviceName string) (func(), error) {
	// Create resource with service name
	res, err := resource.New(context.Background(),
		resource.WithAttributes(
			semconv.ServiceName(serviceName),
			semconv.ServiceVersion("1.0.0"),
		),
	)
	if err != nil {
		return nil, err
	}

	// Check if OTEL_EXPORTER_OTLP_ENDPOINT is set
	otlpEndpoint := os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT")
	if otlpEndpoint == "" {
		// If no OTLP endpoint is configured, use a no-op tracer
		slog.Info("No OTEL_EXPORTER_OTLP_ENDPOINT configured, using no-op tracer")
		return func() {}, nil
	}

	// Create OTLP HTTP exporter
	exporter, err := otlptracehttp.New(context.Background())
	if err != nil {
		return nil, err
	}

	// Create tracer provider
	tp := trace.NewTracerProvider(
		trace.WithBatcher(exporter),
		trace.WithResource(res),
	)

	// Set global tracer provider
	otel.SetTracerProvider(tp)

	slog.Info("OpenTelemetry tracing initialized", "service", serviceName, "endpoint", otlpEndpoint)

	// Return cleanup function
	return func() {
		if err := tp.Shutdown(context.Background()); err != nil {
			slog.Error("Error shutting down tracer provider", "error", err)
		}
	}, nil
}
