package middleware

import (
	"context"
	"errors"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

	"github.com/felixgeelhaar/mcp-go/protocol"
)

const (
	instrumentationName = "github.com/felixgeelhaar/mcp-go"
)

// OTelOption configures the OpenTelemetry middleware.
type OTelOption func(*otelConfig)

type otelConfig struct {
	tracerProvider trace.TracerProvider
	meterProvider  metric.MeterProvider
	serviceName    string
	skipMethods    map[string]bool
}

// WithTracerProvider sets a custom tracer provider.
func WithTracerProvider(tp trace.TracerProvider) OTelOption {
	return func(c *otelConfig) {
		c.tracerProvider = tp
	}
}

// WithMeterProvider sets a custom meter provider.
func WithMeterProvider(mp metric.MeterProvider) OTelOption {
	return func(c *otelConfig) {
		c.meterProvider = mp
	}
}

// WithOTelServiceName sets the service name for telemetry.
func WithOTelServiceName(name string) OTelOption {
	return func(c *otelConfig) {
		c.serviceName = name
	}
}

// WithOTelSkipMethods specifies methods to skip for tracing.
func WithOTelSkipMethods(methods ...string) OTelOption {
	return func(c *otelConfig) {
		for _, m := range methods {
			c.skipMethods[m] = true
		}
	}
}

// OTel returns middleware that adds OpenTelemetry tracing and metrics.
// It creates spans for each request and records request counts and latency.
func OTel(opts ...OTelOption) Middleware {
	cfg := &otelConfig{
		tracerProvider: otel.GetTracerProvider(),
		meterProvider:  otel.GetMeterProvider(),
		serviceName:    "mcp-server",
		skipMethods:    make(map[string]bool),
	}
	for _, opt := range opts {
		opt(cfg)
	}

	tracer := cfg.tracerProvider.Tracer(
		instrumentationName,
		trace.WithInstrumentationVersion("1.0.0"),
	)

	meter := cfg.meterProvider.Meter(
		instrumentationName,
		metric.WithInstrumentationVersion("1.0.0"),
	)

	// Create metrics instruments
	requestCounter, _ := meter.Int64Counter(
		"mcp.server.requests",
		metric.WithDescription("Total number of MCP requests"),
		metric.WithUnit("{request}"),
	)

	requestDuration, _ := meter.Float64Histogram(
		"mcp.server.request.duration",
		metric.WithDescription("Duration of MCP requests"),
		metric.WithUnit("ms"),
	)

	errorCounter, _ := meter.Int64Counter(
		"mcp.server.errors",
		metric.WithDescription("Total number of MCP errors"),
		metric.WithUnit("{error}"),
	)

	return func(next HandlerFunc) HandlerFunc {
		return func(ctx context.Context, req *protocol.Request) (*protocol.Response, error) {
			// Skip tracing for certain methods
			if cfg.skipMethods[req.Method] {
				return next(ctx, req)
			}

			// Start span
			spanName := "mcp." + req.Method
			ctx, span := tracer.Start(ctx, spanName,
				trace.WithSpanKind(trace.SpanKindServer),
				trace.WithAttributes(
					attribute.String("mcp.method", req.Method),
					attribute.String("service.name", cfg.serviceName),
				),
			)
			defer span.End()

			// Add request ID if present
			if reqID := RequestIDFromContext(ctx); reqID != "" {
				span.SetAttributes(attribute.String("mcp.request_id", reqID))
			}

			// Record start time for duration metric
			startTime := time.Now()

			// Common metric attributes
			attrs := []attribute.KeyValue{
				attribute.String("mcp.method", req.Method),
				attribute.String("service.name", cfg.serviceName),
			}

			// Increment request counter
			requestCounter.Add(ctx, 1, metric.WithAttributes(attrs...))

			// Execute handler
			resp, err := next(ctx, req)

			// Record duration
			duration := float64(time.Since(startTime).Milliseconds())
			requestDuration.Record(ctx, duration, metric.WithAttributes(attrs...))

			// Record result
			if err != nil {
				span.RecordError(err)
				span.SetStatus(codes.Error, err.Error())

				var mcpErr *protocol.Error
				if errors.As(err, &mcpErr) {
					span.SetAttributes(attribute.Int("mcp.error_code", mcpErr.Code))
					errorCounter.Add(ctx, 1, metric.WithAttributes(
						append(attrs, attribute.Int("mcp.error_code", mcpErr.Code))...,
					))
				} else {
					errorCounter.Add(ctx, 1, metric.WithAttributes(attrs...))
				}
			} else if resp != nil && resp.Error != nil {
				span.SetStatus(codes.Error, resp.Error.Message)
				span.SetAttributes(attribute.Int("mcp.error_code", resp.Error.Code))
				errorCounter.Add(ctx, 1, metric.WithAttributes(
					append(attrs, attribute.Int("mcp.error_code", resp.Error.Code))...,
				))
			} else {
				span.SetStatus(codes.Ok, "")
			}

			return resp, err
		}
	}
}

// SpanFromContext returns the current span from context.
// Returns a no-op span if no span is present.
func SpanFromContext(ctx context.Context) trace.Span {
	return trace.SpanFromContext(ctx)
}

// AddSpanEvent adds an event to the current span.
func AddSpanEvent(ctx context.Context, name string, attrs ...attribute.KeyValue) {
	span := trace.SpanFromContext(ctx)
	span.AddEvent(name, trace.WithAttributes(attrs...))
}

// SetSpanAttribute sets an attribute on the current span.
func SetSpanAttribute(ctx context.Context, key string, value any) {
	span := trace.SpanFromContext(ctx)
	switch v := value.(type) {
	case string:
		span.SetAttributes(attribute.String(key, v))
	case int:
		span.SetAttributes(attribute.Int(key, v))
	case int64:
		span.SetAttributes(attribute.Int64(key, v))
	case float64:
		span.SetAttributes(attribute.Float64(key, v))
	case bool:
		span.SetAttributes(attribute.Bool(key, v))
	case []string:
		span.SetAttributes(attribute.StringSlice(key, v))
	}
}
