package middleware

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"go.klarlabs.de/mcp/protocol"
)

type testAuditLogger struct {
	mu     sync.Mutex
	events []AuditEvent
}

func (l *testAuditLogger) LogEvent(ctx context.Context, event AuditEvent) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.events = append(l.events, event)
}

func (l *testAuditLogger) Events() []AuditEvent {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]AuditEvent{}, l.events...)
}

func TestAuditMiddleware(t *testing.T) {
	t.Run("logs successful requests", func(t *testing.T) {
		logger := &testAuditLogger{}
		audit := NewAuditMiddleware(logger)

		mw := audit.Middleware()
		handler := mw(func(ctx context.Context, req *protocol.Request) (*protocol.Response, error) {
			return protocol.NewResponse(req.ID, map[string]string{"status": "ok"}), nil
		})

		req := &protocol.Request{
			JSONRPC: "2.0",
			ID:      json.RawMessage(`1`),
			Method:  "tools/list",
		}

		resp, err := handler(context.Background(), req)
		if err != nil {
			t.Fatalf("handler returned error: %v", err)
		}
		if resp == nil {
			t.Fatal("handler returned nil response")
		}

		events := logger.Events()
		if len(events) != 1 {
			t.Fatalf("expected 1 event, got %d", len(events))
		}

		event := events[0]
		if event.Action != "tools.list" {
			t.Errorf("action = %q, want tools.list", event.Action)
		}
		if event.Status != "success" {
			t.Errorf("status = %q, want success", event.Status)
		}
		if event.Method != "tools/list" {
			t.Errorf("method = %q, want tools/list", event.Method)
		}
		if event.Duration == 0 {
			t.Error("expected non-zero duration")
		}
	})

	t.Run("logs failed requests", func(t *testing.T) {
		logger := &testAuditLogger{}
		audit := NewAuditMiddleware(logger)

		mw := audit.Middleware()
		expectedErr := context.DeadlineExceeded
		handler := mw(func(ctx context.Context, req *protocol.Request) (*protocol.Response, error) {
			return nil, expectedErr
		})

		req := &protocol.Request{
			JSONRPC: "2.0",
			ID:      json.RawMessage(`1`),
			Method:  "tools/call",
		}

		_, err := handler(context.Background(), req)
		if err != expectedErr {
			t.Fatalf("expected error %v, got %v", expectedErr, err)
		}

		events := logger.Events()
		if len(events) != 1 {
			t.Fatalf("expected 1 event, got %d", len(events))
		}

		event := events[0]
		if event.Action != "tools.execute" {
			t.Errorf("action = %q, want tools.execute", event.Action)
		}
		if event.Status != "error" {
			t.Errorf("status = %q, want error", event.Status)
		}
		if event.Error == "" {
			t.Error("expected error message")
		}
	})

	t.Run("includes correlation ID from context", func(t *testing.T) {
		logger := &testAuditLogger{}
		audit := NewAuditMiddleware(logger)

		mw := audit.Middleware()
		handler := mw(func(ctx context.Context, req *protocol.Request) (*protocol.Response, error) {
			return protocol.NewResponse(req.ID, nil), nil
		})

		req := &protocol.Request{
			JSONRPC: "2.0",
			ID:      json.RawMessage(`1`),
			Method:  "ping",
		}

		ctx := ContextWithCorrelationID(context.Background(), "corr-123")
		handler(ctx, req)

		events := logger.Events()
		if events[0].CorrelationID != "corr-123" {
			t.Errorf("correlationID = %q, want corr-123", events[0].CorrelationID)
		}
	})

	t.Run("classifies actions correctly", func(t *testing.T) {
		tests := []struct {
			method string
			action string
		}{
			{"initialize", "session.start"},
			{"notifications/initialized", "session.ready"},
			{"tools/list", "tools.list"},
			{"tools/call", "tools.execute"},
			{"resources/list", "resources.list"},
			{"resources/read", "resources.read"},
			{"prompts/list", "prompts.list"},
			{"prompts/get", "prompts.get"},
			{"tasks/create", "tasks.create"},
			{"tasks/get", "tasks.get"},
			{"tasks/list", "tasks.list"},
			{"tasks/cancel", "tasks.cancel"},
		}

		for _, tt := range tests {
			t.Run(tt.method, func(t *testing.T) {
				action := classifyAction(tt.method)
				if action != tt.action {
					t.Errorf("classifyAction(%q) = %q, want %q", tt.method, action, tt.action)
				}
			})
		}
	})
}

func TestCorrelationID(t *testing.T) {
	t.Run("sets and retrieves correlation ID", func(t *testing.T) {
		ctx := ContextWithCorrelationID(context.Background(), "test-id")

		id := CorrelationIDFromContext(ctx)
		if id != "test-id" {
			t.Errorf("CorrelationIDFromContext() = %q, want test-id", id)
		}
	})

	t.Run("returns empty for missing correlation ID", func(t *testing.T) {
		id := CorrelationIDFromContext(context.Background())
		if id != "" {
			t.Errorf("CorrelationIDFromContext() = %q, want empty", id)
		}
	})
}

func TestJSONAuditLogger(t *testing.T) {
	logger := JSONAuditLogger{}

	event := AuditEvent{
		Timestamp: time.Now(),
		Method:    "tools/call",
		Action:    "tools.execute",
		Status:    "success",
		Duration:  100 * time.Millisecond,
	}

	logger.LogEvent(context.Background(), event)
}
