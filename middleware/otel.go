package middleware

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"

	"go.klarlabs.de/mcp/protocol"
)

const (
	instrumentationName = "go.klarlabs.de/mcp"
)

// OTelOption configures the OpenTelemetry middleware.
type OTelOption func(*otelConfig)

type otelConfig struct {
	tracerProvider trace.TracerProvider
	meterProvider  metric.MeterProvider
	propagator     propagation.TextMapPropagator
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

// WithOTelPropagator sets the text-map propagator used to extract W3C Trace
// Context (traceparent/tracestate/baggage) carried in a modern request's
// _meta. It defaults to the globally-registered propagator
// (otel.GetTextMapPropagator), so an application that installs a propagator at
// startup gets remote-parent joining for free. Supply one explicitly to avoid
// depending on global state.
func WithOTelPropagator(p propagation.TextMapPropagator) OTelOption {
	return func(c *otelConfig) {
		c.propagator = p
	}
}

// OTel returns middleware that adds OpenTelemetry tracing and metrics.
// It creates spans for each request and records request counts and latency.
func OTel(opts ...OTelOption) Middleware {
	cfg := &otelConfig{
		tracerProvider: otel.GetTracerProvider(),
		meterProvider:  otel.GetMeterProvider(),
		propagator:     otel.GetTextMapPropagator(),
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

	// Create metrics instruments.
	// Errors are intentionally ignored: the OTel SDK returns no-op instruments on failure,
	// allowing the middleware to function without metrics rather than failing entirely.
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

			// Join the client's distributed trace: a modern (stateless) request
			// carries W3C Trace Context in its _meta. Extracting it here — before
			// the span is started — makes the server span a child of the caller's
			// remote span. Absent trace context, ctx is unchanged and the span is
			// a root.
			ctx = extractModernTraceContext(ctx, req, cfg.propagator)

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
			switch {
			case err != nil:
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
			case resp != nil && resp.Error != nil:
				span.SetStatus(codes.Error, resp.Error.Message)
				span.SetAttributes(attribute.Int("mcp.error_code", resp.Error.Code))
				errorCounter.Add(ctx, 1, metric.WithAttributes(
					append(attrs, attribute.Int("mcp.error_code", resp.Error.Code))...,
				))
			default:
				span.SetStatus(codes.Ok, "")
			}

			return resp, err
		}
	}
}

// extractModernTraceContext reads W3C Trace Context (traceparent, tracestate,
// baggage) from a modern request's _meta and, when present, returns a context
// carrying the remote SpanContext so a span started next parents onto the
// caller's trace. It returns ctx unchanged for a legacy request or one without
// trace context. A nil propagator (or the no-op default) simply yields ctx.
func extractModernTraceContext(ctx context.Context, req *protocol.Request, propagator propagation.TextMapPropagator) context.Context {
	if propagator == nil || req == nil {
		return ctx
	}
	carrier, ok := traceCarrierFromMeta(req.Params)
	if !ok {
		return ctx
	}
	return propagator.Extract(ctx, carrier)
}

// traceCarrierFromMeta pulls the reserved trace-context keys out of a request's
// params _meta into a propagation carrier. It returns (carrier, false) when no
// trace-context key is present, so callers can skip extraction entirely.
func traceCarrierFromMeta(params json.RawMessage) (propagation.MapCarrier, bool) {
	if len(params) == 0 {
		return nil, false
	}
	var envelope struct {
		Meta map[string]json.RawMessage `json:"_meta"`
	}
	if err := json.Unmarshal(params, &envelope); err != nil {
		return nil, false
	}
	carrier := propagation.MapCarrier{}
	put := func(metaKey, carrierKey string) {
		if raw, ok := envelope.Meta[metaKey]; ok {
			var v string
			if json.Unmarshal(raw, &v) == nil && v != "" {
				carrier[carrierKey] = v
			}
		}
	}
	put(protocol.MetaKeyTraceparent, "traceparent")
	put(protocol.MetaKeyTracestate, "tracestate")
	put(protocol.MetaKeyBaggage, "baggage")
	if len(carrier) == 0 {
		return nil, false
	}
	return carrier, true
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
