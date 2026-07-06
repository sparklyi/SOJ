package observability

import (
	"context"
	"strings"
	"testing"
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
