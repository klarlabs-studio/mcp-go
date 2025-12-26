package middleware

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/felixgeelhaar/mcp-go/protocol"
)

func TestOTelMiddleware(t *testing.T) {
	t.Run("creates span for request", func(t *testing.T) {
		exporter := tracetest.NewInMemoryExporter()
		tp := sdktrace.NewTracerProvider(
			sdktrace.WithSyncer(exporter),
		)
		defer tp.Shutdown(context.Background())

		middleware := OTel(WithTracerProvider(tp))

		handler := middleware(func(ctx context.Context, req *protocol.Request) (*protocol.Response, error) {
			return &protocol.Response{ID: req.ID}, nil
		})

		req := &protocol.Request{ID: json.RawMessage("1"), Method: "tools/list"}
		_, err := handler(context.Background(), req)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		spans := exporter.GetSpans()
		if len(spans) != 1 {
			t.Fatalf("expected 1 span, got %d", len(spans))
		}

		span := spans[0]
		if span.Name != "mcp.tools/list" {
			t.Errorf("expected span name 'mcp.tools/list', got %q", span.Name)
		}
	})

	t.Run("records error on failure", func(t *testing.T) {
		exporter := tracetest.NewInMemoryExporter()
		tp := sdktrace.NewTracerProvider(
			sdktrace.WithSyncer(exporter),
		)
		defer tp.Shutdown(context.Background())

		middleware := OTel(WithTracerProvider(tp))

		expectedErr := errors.New("handler failed")
		handler := middleware(func(ctx context.Context, req *protocol.Request) (*protocol.Response, error) {
			return nil, expectedErr
		})

		req := &protocol.Request{ID: json.RawMessage("1"), Method: "tools/call"}
		_, err := handler(context.Background(), req)
		if err == nil {
			t.Fatal("expected error")
		}

		spans := exporter.GetSpans()
		if len(spans) != 1 {
			t.Fatalf("expected 1 span, got %d", len(spans))
		}

		span := spans[0]
		if len(span.Events) == 0 {
			t.Error("expected error event on span")
		}
	})

	t.Run("records protocol error code", func(t *testing.T) {
		exporter := tracetest.NewInMemoryExporter()
		tp := sdktrace.NewTracerProvider(
			sdktrace.WithSyncer(exporter),
		)
		defer tp.Shutdown(context.Background())

		middleware := OTel(WithTracerProvider(tp))

		handler := middleware(func(ctx context.Context, req *protocol.Request) (*protocol.Response, error) {
			return nil, protocol.NewNotFound("tool not found")
		})

		req := &protocol.Request{ID: json.RawMessage("1"), Method: "tools/call"}
		handler(context.Background(), req)

		spans := exporter.GetSpans()
		if len(spans) != 1 {
			t.Fatalf("expected 1 span, got %d", len(spans))
		}

		span := spans[0]
		found := false
		for _, attr := range span.Attributes {
			if attr.Key == "mcp.error_code" {
				found = true
				if attr.Value.AsInt64() != int64(protocol.CodeNotFound) {
					t.Errorf("expected error code %d, got %d", protocol.CodeNotFound, attr.Value.AsInt64())
				}
			}
		}
		if !found {
			t.Error("expected mcp.error_code attribute")
		}
	})

	t.Run("skips configured methods", func(t *testing.T) {
		exporter := tracetest.NewInMemoryExporter()
		tp := sdktrace.NewTracerProvider(
			sdktrace.WithSyncer(exporter),
		)
		defer tp.Shutdown(context.Background())

		middleware := OTel(
			WithTracerProvider(tp),
			WithOTelSkipMethods("ping"),
		)

		handler := middleware(func(ctx context.Context, req *protocol.Request) (*protocol.Response, error) {
			return &protocol.Response{ID: req.ID}, nil
		})

		req := &protocol.Request{ID: json.RawMessage("1"), Method: "ping"}
		_, err := handler(context.Background(), req)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		spans := exporter.GetSpans()
		if len(spans) != 0 {
			t.Errorf("expected 0 spans for skipped method, got %d", len(spans))
		}
	})

	t.Run("uses custom service name", func(t *testing.T) {
		exporter := tracetest.NewInMemoryExporter()
		tp := sdktrace.NewTracerProvider(
			sdktrace.WithSyncer(exporter),
		)
		defer tp.Shutdown(context.Background())

		middleware := OTel(
			WithTracerProvider(tp),
			WithOTelServiceName("my-mcp-server"),
		)

		handler := middleware(func(ctx context.Context, req *protocol.Request) (*protocol.Response, error) {
			return &protocol.Response{ID: req.ID}, nil
		})

		req := &protocol.Request{ID: json.RawMessage("1"), Method: "tools/list"}
		handler(context.Background(), req)

		spans := exporter.GetSpans()
		if len(spans) != 1 {
			t.Fatalf("expected 1 span, got %d", len(spans))
		}

		span := spans[0]
		found := false
		for _, attr := range span.Attributes {
			if attr.Key == "service.name" && attr.Value.AsString() == "my-mcp-server" {
				found = true
			}
		}
		if !found {
			t.Error("expected service.name attribute with custom value")
		}
	})

	t.Run("uses global providers by default", func(t *testing.T) {
		// Ensure middleware can be created without options
		middleware := OTel()
		if middleware == nil {
			t.Fatal("expected non-nil middleware")
		}
	})

	t.Run("uses custom meter provider", func(t *testing.T) {
		mp := sdkmetric.NewMeterProvider()
		defer mp.Shutdown(context.Background())

		middleware := OTel(WithMeterProvider(mp))

		handler := middleware(func(ctx context.Context, req *protocol.Request) (*protocol.Response, error) {
			return &protocol.Response{ID: req.ID}, nil
		})

		req := &protocol.Request{ID: json.RawMessage("1"), Method: "tools/list"}
		_, err := handler(context.Background(), req)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})
}

func TestSpanHelpers(t *testing.T) {
	t.Run("SpanFromContext returns span", func(t *testing.T) {
		exporter := tracetest.NewInMemoryExporter()
		tp := sdktrace.NewTracerProvider(
			sdktrace.WithSyncer(exporter),
		)
		defer tp.Shutdown(context.Background())
		otel.SetTracerProvider(tp)

		tracer := tp.Tracer("test")
		ctx, span := tracer.Start(context.Background(), "test-span")
		defer span.End()

		got := SpanFromContext(ctx)
		if got != span {
			t.Error("expected same span from context")
		}
	})

	t.Run("AddSpanEvent adds event", func(t *testing.T) {
		exporter := tracetest.NewInMemoryExporter()
		tp := sdktrace.NewTracerProvider(
			sdktrace.WithSyncer(exporter),
		)
		defer tp.Shutdown(context.Background())

		tracer := tp.Tracer("test")
		ctx, span := tracer.Start(context.Background(), "test-span")

		AddSpanEvent(ctx, "test-event", attribute.String("key", "value"))
		span.End()

		spans := exporter.GetSpans()
		if len(spans) != 1 {
			t.Fatalf("expected 1 span, got %d", len(spans))
		}

		if len(spans[0].Events) != 1 {
			t.Fatalf("expected 1 event, got %d", len(spans[0].Events))
		}

		event := spans[0].Events[0]
		if event.Name != "test-event" {
			t.Errorf("expected event name 'test-event', got %q", event.Name)
		}
	})

	t.Run("SetSpanAttribute sets various types", func(t *testing.T) {
		exporter := tracetest.NewInMemoryExporter()
		tp := sdktrace.NewTracerProvider(
			sdktrace.WithSyncer(exporter),
		)
		defer tp.Shutdown(context.Background())

		tracer := tp.Tracer("test")
		ctx, span := tracer.Start(context.Background(), "test-span")

		SetSpanAttribute(ctx, "string_key", "value")
		SetSpanAttribute(ctx, "int_key", 42)
		SetSpanAttribute(ctx, "int64_key", int64(100))
		SetSpanAttribute(ctx, "float_key", 3.14)
		SetSpanAttribute(ctx, "bool_key", true)
		SetSpanAttribute(ctx, "slice_key", []string{"a", "b"})
		span.End()

		spans := exporter.GetSpans()
		if len(spans) != 1 {
			t.Fatalf("expected 1 span, got %d", len(spans))
		}

		// Check that attributes were set (they should be in the span)
		attrMap := make(map[string]bool)
		for _, attr := range spans[0].Attributes {
			attrMap[string(attr.Key)] = true
		}

		expectedKeys := []string{"string_key", "int_key", "int64_key", "float_key", "bool_key", "slice_key"}
		for _, key := range expectedKeys {
			if !attrMap[key] {
				t.Errorf("expected attribute %q to be set", key)
			}
		}
	})
}
