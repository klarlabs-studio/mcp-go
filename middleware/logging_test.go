package middleware

import (
	"context"
	"errors"
	"testing"
	"time"

	"go.klarlabs.de/mcp/protocol"
)

// mockLogger captures log calls for testing.
type mockLogger struct {
	entries []logEntry
}

type logEntry struct {
	level   string
	message string
	fields  []Field
}

func (l *mockLogger) Info(msg string, fields ...Field) {
	l.entries = append(l.entries, logEntry{level: "info", message: msg, fields: fields})
}

func (l *mockLogger) Error(msg string, fields ...Field) {
	l.entries = append(l.entries, logEntry{level: "error", message: msg, fields: fields})
}

func (l *mockLogger) Debug(msg string, fields ...Field) {
	l.entries = append(l.entries, logEntry{level: "debug", message: msg, fields: fields})
}

func (l *mockLogger) Warn(msg string, fields ...Field) {
	l.entries = append(l.entries, logEntry{level: "warn", message: msg, fields: fields})
}

func TestLogging(t *testing.T) {
	t.Run("logs successful requests", func(t *testing.T) {
		logger := &mockLogger{}

		handler := HandlerFunc(func(ctx context.Context, req *protocol.Request) (*protocol.Response, error) {
			return protocol.NewResponse(req.ID, "ok"), nil
		})

		wrapped := Logging(logger)(handler)
		_, _ = wrapped(context.Background(), &protocol.Request{Method: "test/method"})

		if len(logger.entries) != 1 {
			t.Fatalf("expected 1 log entry, got %d", len(logger.entries))
		}

		entry := logger.entries[0]
		if entry.level != "info" {
			t.Errorf("level = %q, want %q", entry.level, "info")
		}
		if entry.message != "request completed" {
			t.Errorf("message = %q, want %q", entry.message, "request completed")
		}

		// Check for expected fields
		hasMethod := false
		hasDuration := false
		for _, f := range entry.fields {
			if f.Key == "method" && f.Value == "test/method" {
				hasMethod = true
			}
			if f.Key == "duration" {
				if _, ok := f.Value.(time.Duration); ok {
					hasDuration = true
				}
			}
		}
		if !hasMethod {
			t.Error("expected 'method' field in log")
		}
		if !hasDuration {
			t.Error("expected 'duration' field in log")
		}
	})

	t.Run("logs errors at error level", func(t *testing.T) {
		logger := &mockLogger{}
		expectedErr := errors.New("handler failed")

		handler := HandlerFunc(func(ctx context.Context, req *protocol.Request) (*protocol.Response, error) {
			return nil, expectedErr
		})

		wrapped := Logging(logger)(handler)
		_, _ = wrapped(context.Background(), &protocol.Request{Method: "test/method"})

		if len(logger.entries) != 1 {
			t.Fatalf("expected 1 log entry, got %d", len(logger.entries))
		}

		entry := logger.entries[0]
		if entry.level != "error" {
			t.Errorf("level = %q, want %q", entry.level, "error")
		}

		hasError := false
		for _, f := range entry.fields {
			if f.Key == "error" {
				hasError = true
			}
		}
		if !hasError {
			t.Error("expected 'error' field in log")
		}
	})

	t.Run("includes request ID if present", func(t *testing.T) {
		logger := &mockLogger{}

		handler := HandlerFunc(func(ctx context.Context, req *protocol.Request) (*protocol.Response, error) {
			return protocol.NewResponse(req.ID, "ok"), nil
		})

		ctx := ContextWithRequestID(context.Background(), "test-request-123")
		wrapped := Logging(logger)(handler)
		_, _ = wrapped(ctx, &protocol.Request{Method: "test/method"})

		if len(logger.entries) != 1 {
			t.Fatalf("expected 1 log entry, got %d", len(logger.entries))
		}

		hasRequestID := false
		for _, f := range logger.entries[0].fields {
			if f.Key == "request_id" && f.Value == "test-request-123" {
				hasRequestID = true
			}
		}
		if !hasRequestID {
			t.Error("expected 'request_id' field in log")
		}
	})
}

func TestField(t *testing.T) {
	t.Run("creates field with key and value", func(t *testing.T) {
		f := F("key", "value")
		if f.Key != "key" {
			t.Errorf("Key = %q, want %q", f.Key, "key")
		}
		if f.Value != "value" {
			t.Errorf("Value = %v, want %q", f.Value, "value")
		}
	})
}
