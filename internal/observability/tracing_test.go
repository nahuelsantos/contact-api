package observability

import (
	"context"
	"testing"

	"go.opentelemetry.io/otel/attribute"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
)

func TestNewResource_UsesBuildVersion(t *testing.T) {
	res, err := newResource("contact-api", "abc123")
	if err != nil {
		t.Fatal(err)
	}
	got := map[attribute.Key]string{}
	for _, kv := range res.Attributes() {
		got[kv.Key] = kv.Value.String()
	}
	if got["service.name"] != "contact-api" || got["service.version"] != "abc123" {
		t.Fatalf("resource = %v", got)
	}
}

func TestRedactingTracerProvider_DropsVisitorAttributes(t *testing.T) {
	rec := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec))
	_, span := RedactingTracerProvider(tp).Tracer("t").Start(context.Background(), "s",
		trace.WithSpanKind(trace.SpanKindServer),
		trace.WithAttributes(
			attribute.String("client.address", "203.0.113.9"),
			attribute.Int("client.port", 5555),
			attribute.String("user_agent.original", "Mozilla/5.0 visitor"),
			attribute.String("network.peer.address", "203.0.113.9"),
			attribute.Int("network.peer.port", 5555),
			attribute.String("http.route", "/api/v2/sites/:site/forms/:form"),
		))
	span.End()

	spans := rec.Ended()
	if len(spans) != 1 {
		t.Fatalf("spans = %d", len(spans))
	}
	s := spans[0]
	if s.SpanKind() != trace.SpanKindServer {
		t.Fatalf("span kind lost: %v", s.SpanKind())
	}
	keys := map[attribute.Key]bool{}
	for _, kv := range s.Attributes() {
		keys[kv.Key] = true
	}
	for _, k := range []attribute.Key{"client.address", "client.port", "user_agent.original", "network.peer.address", "network.peer.port"} {
		if keys[k] {
			t.Errorf("span keeps %s", k)
		}
	}
	if !keys["http.route"] {
		t.Error("span lost http.route")
	}
}
