package middleware

import (
	"context"
	"encoding/json"
	"testing"

	"go.klarlabs.de/mcp/protocol"
)

func TestTracingMiddleware(t *testing.T) {
	t.Run("generates tracing IDs", func(t *testing.T) {
		mw := Tracing()
		handler := mw(func(ctx context.Context, req *protocol.Request) (*protocol.Response, error) {
			corrID := CorrelationIDFromContext(ctx)
			traceID := TraceIDFromContext(ctx)
			if corrID == "" {
				t.Error("expected correlation ID")
			}
			if traceID == "" {
				t.Error("expected trace ID")
			}
			return protocol.NewResponse(req.ID, nil), nil
		})

		handler(context.Background(), &protocol.Request{
			JSONRPC: "2.0",
			ID:      json.RawMessage(`1`),
			Method:  "ping",
		})
	})

	t.Run("preserves existing correlation ID", func(t *testing.T) {
		mw := Tracing()
		handler := mw(func(ctx context.Context, req *protocol.Request) (*protocol.Response, error) {
			corrID := CorrelationIDFromContext(ctx)
			if corrID != "existing-corr-id" {
				t.Errorf("CorrelationID = %q, want existing-corr-id", corrID)
			}
			return protocol.NewResponse(req.ID, nil), nil
		})

		ctx := ContextWithCorrelationID(context.Background(), "existing-corr-id")
		handler(ctx, &protocol.Request{
			JSONRPC: "2.0",
			ID:      json.RawMessage(`1`),
			Method:  "ping",
		})
	})
}

func TestFormatTracingHeaders(t *testing.T) {
	t.Run("formats headers", func(t *testing.T) {
		ctx := ContextWithCorrelationID(context.Background(), "corr-123")
		ctx = ContextWithTracing(ctx, "trace-456")

		headers := FormatTracingHeaders(ctx)

		if headers[CorrelationIDHeader] != "corr-123" {
			t.Errorf("CorrelationIDHeader = %q, want corr-123", headers[CorrelationIDHeader])
		}
		if headers[TraceIDHeader] != "trace-456" {
			t.Errorf("TraceIDHeader = %q, want trace-456", headers[TraceIDHeader])
		}
	})
}

func TestParseTracingHeaders(t *testing.T) {
	t.Run("parses headers case-insensitive", func(t *testing.T) {
		corrID, traceID := ParseTracingHeaders(map[string]string{
			"x-correlation-id": "corr-123",
			"X-TRACE-ID":       "trace-456",
		})

		if corrID != "corr-123" {
			t.Errorf("correlationID = %q, want corr-123", corrID)
		}
		if traceID != "trace-456" {
			t.Errorf("traceID = %q, want trace-456", traceID)
		}
	})
}
