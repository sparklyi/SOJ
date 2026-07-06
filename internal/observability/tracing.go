package observability

import (
	"context"
	"fmt"
	"net/url"
	"strings"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.37.0"
	"go.opentelemetry.io/otel/trace"
	"go.opentelemetry.io/otel/trace/noop"
)

type TracingOptions struct {
	Enabled            bool
	ServiceName        string
	ResourceAttributes string
	ExporterEndpoint   string
}

type Tracing struct {
	enabled     bool
	serviceName string
	provider    *sdktrace.TracerProvider
}

func SetupTracing(ctx context.Context, opts TracingOptions) (Tracing, error) {
	serviceName := strings.TrimSpace(opts.ServiceName)
	if serviceName == "" {
		serviceName = "soj"
	}
	if !opts.Enabled {
		return Tracing{serviceName: serviceName}, nil
	}

	exporterEndpoint := strings.TrimSpace(opts.ExporterEndpoint)
	if exporterEndpoint != "" {
		if err := validateTracingExporterEndpoint(exporterEndpoint); err != nil {
			return Tracing{}, err
		}
	}

	res, err := resource.New(ctx,
		resource.WithFromEnv(),
		resource.WithTelemetrySDK(),
		resource.WithProcess(),
		resource.WithHost(),
		resource.WithAttributes(semconv.ServiceName(serviceName)),
	)
	if err != nil {
		return Tracing{}, fmt.Errorf("setup tracing resource: %w", err)
	}

	exporterOpts := []otlptracehttp.Option{}
	if exporterEndpoint != "" {
		exporterOpts = append(exporterOpts, otlptracehttp.WithEndpointURL(exporterEndpoint))
	}
	exporter, err := otlptracehttp.New(ctx, exporterOpts...)
	if err != nil {
		return Tracing{}, fmt.Errorf("setup tracing exporter: %w", err)
	}

	provider := sdktrace.NewTracerProvider(
		sdktrace.WithResource(res),
		sdktrace.WithSampler(sdktrace.ParentBased(sdktrace.TraceIDRatioBased(1.0))),
		sdktrace.WithBatcher(exporter),
	)
	otel.SetTracerProvider(provider)
	otel.SetTextMapPropagator(propagation.TraceContext{})

	return Tracing{enabled: true, serviceName: serviceName, provider: provider}, nil
}

func (t Tracing) Enabled() bool {
	return t.enabled
}

func (t Tracing) ServiceName() string {
	return t.serviceName
}

func (t Tracing) Shutdown(ctx context.Context) error {
	if t.provider == nil {
		return nil
	}
	err := t.provider.Shutdown(ctx)
	otel.SetTracerProvider(noop.NewTracerProvider())
	return err
}

func Tracer(name string) trace.Tracer {
	return otel.Tracer(name)
}

func validateTracingExporterEndpoint(endpoint string) error {
	parsed, err := url.ParseRequestURI(endpoint)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		if err == nil {
			err = fmt.Errorf("missing URL scheme or host")
		}
		return fmt.Errorf("tracing exporter endpoint %q: %w", endpoint, err)
	}
	return nil
}
