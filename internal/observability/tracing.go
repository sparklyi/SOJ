package observability

import (
	"context"
	"fmt"
	"net/url"
	"strings"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
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

	res, err := newTracingResource(ctx, serviceName, opts.ResourceAttributes)
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

// newTracingResource builds the resource in precedence order: environment
// attributes, then configured resource attributes, then the SOJ service name.
// Malformed configured attributes are ignored as a whole so that no partial
// config is applied; the raw value is never logged or returned.
func newTracingResource(ctx context.Context, serviceName, resourceAttributes string) (*resource.Resource, error) {
	configAttrs, err := parseResourceAttributes(resourceAttributes)
	if err != nil {
		configAttrs = nil
	}
	return resource.New(ctx,
		resource.WithFromEnv(),
		resource.WithTelemetrySDK(),
		resource.WithProcess(),
		resource.WithHost(),
		resource.WithAttributes(configAttrs...),
		resource.WithAttributes(semconv.ServiceName(serviceName)),
	)
}

// parseResourceAttributes parses comma-separated key=value pairs. Keys and
// values are percent-decoded so that delimiters such as ',' and '=' can appear
// in values as %2C and %3D. Blank input yields no attributes.
func parseResourceAttributes(raw string) ([]attribute.KeyValue, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	var attrs []attribute.KeyValue
	for _, pair := range strings.Split(raw, ",") {
		pair = strings.TrimSpace(pair)
		if pair == "" {
			continue
		}
		key, value, ok := strings.Cut(pair, "=")
		if !ok {
			return nil, fmt.Errorf("resource attribute entry missing '='")
		}
		key, err := url.PathUnescape(strings.TrimSpace(key))
		if err != nil {
			return nil, fmt.Errorf("resource attribute key: invalid escape")
		}
		value, err = url.PathUnescape(strings.TrimSpace(value))
		if err != nil {
			return nil, fmt.Errorf("resource attribute value: invalid escape")
		}
		if key == "" {
			return nil, fmt.Errorf("resource attribute entry has empty key")
		}
		attrs = append(attrs, attribute.String(key, value))
	}
	return attrs, nil
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
