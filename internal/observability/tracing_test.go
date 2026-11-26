package observability

import (
	"context"
	"strings"
	"testing"

	"go.opentelemetry.io/otel/attribute"
)

func TestSetupTracingDisabledReturnsNoopShutdown(t *testing.T) {
	tracing, err := SetupTracing(context.Background(), TracingOptions{
		Enabled:     false,
		ServiceName: "soj-api",
	})
	if err != nil {
		t.Fatalf("SetupTracing() error = %v", err)
	}
	if tracing.Enabled() {
		t.Fatal("tracing.Enabled() = true, want false")
	}
	if tracing.ServiceName() != "soj-api" {
		t.Fatalf("tracing.ServiceName() = %q, want soj-api", tracing.ServiceName())
	}
	if err := tracing.Shutdown(context.Background()); err != nil {
		t.Fatalf("Shutdown() error = %v", err)
	}
}

func TestSetupTracingEnabledReturnsActiveLifecycle(t *testing.T) {
	tracing, err := SetupTracing(context.Background(), TracingOptions{
		Enabled:          true,
		ServiceName:      "soj-worker",
		ExporterEndpoint: "http://127.0.0.1:4318/v1/traces",
	})
	if err != nil {
		t.Fatalf("SetupTracing() error = %v", err)
	}
	t.Cleanup(func() {
		if err := tracing.Shutdown(context.Background()); err != nil {
			t.Fatalf("Shutdown() error = %v", err)
		}
	})

	if !tracing.Enabled() {
		t.Fatal("tracing.Enabled() = false, want true")
	}
	if tracing.ServiceName() != "soj-worker" {
		t.Fatalf("tracing.ServiceName() = %q, want soj-worker", tracing.ServiceName())
	}
}

func TestSetupTracingEnabledRejectsInvalidExporterEndpoint(t *testing.T) {
	_, err := SetupTracing(context.Background(), TracingOptions{
		Enabled:          true,
		ServiceName:      "soj-judge-agent",
		ExporterEndpoint: "://not-a-url",
	})
	if err == nil {
		t.Fatal("SetupTracing() error = nil, want invalid endpoint error")
	}
	if !strings.Contains(err.Error(), "tracing exporter endpoint") {
		t.Fatalf("SetupTracing() error = %v, want tracing exporter endpoint error", err)
	}
}

func TestNewTracingResourceAppliesConfiguredAttributes(t *testing.T) {
	t.Setenv("OTEL_RESOURCE_ATTRIBUTES", "env.only=from-env,shared=from-env")

	res, err := newTracingResource(context.Background(), "soj-api",
		"deployment.environment=trial,shared=from-config,note=a%2Cb%3Dc")
	if err != nil {
		t.Fatalf("newTracingResource() error = %v", err)
	}

	want := map[string]string{
		"deployment.environment": "trial",
		"env.only":               "from-env",
		"shared":                 "from-config",
		"note":                   "a,b=c",
		"service.name":           "soj-api",
	}
	for key, value := range want {
		got, ok := res.Set().Value(attribute.Key(key))
		if !ok || got.AsString() != value {
			t.Fatalf("resource attribute %q = %q (present=%v), want %q", key, got.AsString(), ok, value)
		}
	}
}
