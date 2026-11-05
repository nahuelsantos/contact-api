package observability

import (
	"context"
	"fmt"
	"log/slog"
	"os"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.24.0"
	"go.opentelemetry.io/otel/trace"
)

// InitTracing initializes OpenTelemetry tracing. version is the build
// revision, recorded as service.version.
func InitTracing(serviceName, version string) (func(), error) {
	res, err := newResource(serviceName, version)
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
		return nil, fmt.Errorf("create otlp trace exporter: %w", err)
	}

	// Create tracer provider
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter),
		sdktrace.WithResource(res),
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

func newResource(serviceName, version string) (*resource.Resource, error) {
	res, err := resource.New(context.Background(),
		resource.WithAttributes(
			semconv.ServiceName(serviceName),
			semconv.ServiceVersion(version),
		),
	)
	if err != nil {
		return nil, fmt.Errorf("create otel resource: %w", err)
	}

	return res, nil
}

// redactedKeys identify the visitor (or the hop in front of the service).
// The spec allows the client IP only in 403/429 logs, never on spans.
var redactedKeys = map[attribute.Key]bool{
	"client.address":       true,
	"client.port":          true,
	"user_agent.original":  true,
	"network.peer.address": true,
	"network.peer.port":    true,
}

// RedactingTracerProvider wraps tp so spans never start with the visitor's
// address or user agent. otelgin sets those at span start and offers no
// option to drop them, so its tracer is wrapped instead.
func RedactingTracerProvider(tp trace.TracerProvider) trace.TracerProvider {
	return redactingProvider{tp}
}

type redactingProvider struct{ trace.TracerProvider }

func (p redactingProvider) Tracer(name string, opts ...trace.TracerOption) trace.Tracer {
	return redactingTracer{p.TracerProvider.Tracer(name, opts...)}
}

type redactingTracer struct{ trace.Tracer }

func (t redactingTracer) Start(ctx context.Context, name string, opts ...trace.SpanStartOption) (context.Context, trace.Span) {
	cfg := trace.NewSpanStartConfig(opts...)
	attrs := make([]attribute.KeyValue, 0, len(cfg.Attributes()))
	for _, kv := range cfg.Attributes() {
		if !redactedKeys[kv.Key] {
			attrs = append(attrs, kv)
		}
	}
	clean := []trace.SpanStartOption{
		trace.WithAttributes(attrs...),
		trace.WithSpanKind(cfg.SpanKind()),
		trace.WithLinks(cfg.Links()...),
	}
	if ts := cfg.Timestamp(); !ts.IsZero() {
		clean = append(clean, trace.WithTimestamp(ts))
	}
	if cfg.NewRoot() {
		clean = append(clean, trace.WithNewRoot())
	}

	return t.Tracer.Start(ctx, name, clean...) //nolint:spancheck // the caller ends the span
}
