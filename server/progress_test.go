package server

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
)

// mockNotifier records notifications for testing.
type mockNotifier struct {
	mu            sync.Mutex
	notifications []mockNotification
}

type mockNotification struct {
	Method string
	Params any
}

func (m *mockNotifier) SendNotification(method string, params any) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.notifications = append(m.notifications, mockNotification{
		Method: method,
		Params: params,
	})
	return nil
}

func (m *mockNotifier) getNotifications() []mockNotification {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]mockNotification(nil), m.notifications...)
}

func TestProgressReporter(t *testing.T) {
	t.Run("sends progress notifications", func(t *testing.T) {
		notifier := &mockNotifier{}
		reporter := NewProgressReporter("token-123", notifier)

		total := 100.0
		err := reporter.Report(50, &total)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		notifications := notifier.getNotifications()
		if len(notifications) != 1 {
			t.Fatalf("expected 1 notification, got %d", len(notifications))
		}

		if notifications[0].Method != "notifications/progress" {
			t.Errorf("expected method notifications/progress, got %s", notifications[0].Method)
		}

		params := notifications[0].Params.(map[string]any)
		if params["progressToken"] != "token-123" {
			t.Errorf("expected token token-123, got %v", params["progressToken"])
		}
		if params["progress"] != 50.0 {
			t.Errorf("expected progress 50, got %v", params["progress"])
		}
		if params["total"] != 100.0 {
			t.Errorf("expected total 100, got %v", params["total"])
		}
	})

	t.Run("omits total when nil", func(t *testing.T) {
		notifier := &mockNotifier{}
		reporter := NewProgressReporter("token-123", notifier)

		err := reporter.Report(25, nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		notifications := notifier.getNotifications()
		params := notifications[0].Params.(map[string]any)
		if _, ok := params["total"]; ok {
			t.Error("expected total to be omitted")
		}
	})

	t.Run("includes message when provided", func(t *testing.T) {
		notifier := &mockNotifier{}
		reporter := NewProgressReporter("token-123", notifier)

		err := reporter.ReportWithMessage(75, nil, "Processing...")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		notifications := notifier.getNotifications()
		params := notifications[0].Params.(map[string]any)
		if params["message"] != "Processing..." {
			t.Errorf("expected message 'Processing...', got %v", params["message"])
		}
	})

	t.Run("progress must increase", func(t *testing.T) {
		notifier := &mockNotifier{}
		reporter := NewProgressReporter("token-123", notifier)

		reporter.Report(50, nil)
		reporter.Report(40, nil) // Should be adjusted to > 50

		notifications := notifier.getNotifications()
		if len(notifications) != 2 {
			t.Fatalf("expected 2 notifications, got %d", len(notifications))
		}

		params1 := notifications[0].Params.(map[string]any)
		params2 := notifications[1].Params.(map[string]any)

		if params2["progress"].(float64) <= params1["progress"].(float64) {
			t.Error("second progress should be greater than first")
		}
	})

	t.Run("no-op when token is empty", func(t *testing.T) {
		notifier := &mockNotifier{}
		reporter := NewProgressReporter("", notifier)

		err := reporter.Report(50, nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if len(notifier.getNotifications()) != 0 {
			t.Error("expected no notifications for empty token")
		}
	})

	t.Run("no-op when notifier is nil", func(t *testing.T) {
		reporter := NewProgressReporter("token-123", nil)

		// Should not panic
		err := reporter.Report(50, nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("returns token", func(t *testing.T) {
		reporter := NewProgressReporter("my-token", nil)
		if reporter.Token() != "my-token" {
			t.Errorf("expected token 'my-token', got %s", reporter.Token())
		}
	})
}

func TestProgressContext(t *testing.T) {
	t.Run("stores and retrieves reporter", func(t *testing.T) {
		notifier := &mockNotifier{}
		reporter := NewProgressReporter("ctx-token", notifier)

		ctx := ContextWithProgress(context.Background(), reporter)
		retrieved := ProgressFromContext(ctx)

		if retrieved.Token() != "ctx-token" {
			t.Errorf("expected token 'ctx-token', got %s", retrieved.Token())
		}
	})

	t.Run("returns noop reporter when not set", func(t *testing.T) {
		reporter := ProgressFromContext(context.Background())

		// Should not panic and return empty token
		if reporter.Token() != "" {
			t.Errorf("expected empty token, got %s", reporter.Token())
		}

		// Should not error
		err := reporter.Report(50, nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})
}

func TestExtractProgressToken(t *testing.T) {
	t.Run("extracts token from _meta", func(t *testing.T) {
		params := json.RawMessage(`{"_meta": {"progressToken": "abc123"}, "name": "test"}`)
		token := ExtractProgressToken(params)
		if token != "abc123" {
			t.Errorf("expected token 'abc123', got %s", token)
		}
	})

	t.Run("returns empty for missing _meta", func(t *testing.T) {
		params := json.RawMessage(`{"name": "test"}`)
		token := ExtractProgressToken(params)
		if token != "" {
			t.Errorf("expected empty token, got %s", token)
		}
	})

	t.Run("returns empty for nil params", func(t *testing.T) {
		token := ExtractProgressToken(nil)
		if token != "" {
			t.Errorf("expected empty token, got %s", token)
		}
	})

	t.Run("returns empty for invalid JSON", func(t *testing.T) {
		params := json.RawMessage(`invalid`)
		token := ExtractProgressToken(params)
		if token != "" {
			t.Errorf("expected empty token, got %s", token)
		}
	})
}

func TestExtractProgressToken_AcceptsBothTypes(t *testing.T) {
	t.Run("accepts integer token", func(t *testing.T) {
		// MCP permits progressToken to be an integer; it must not be dropped.
		params := json.RawMessage(`{"_meta": {"progressToken": 42}, "name": "test"}`)
		token := ExtractProgressToken(params)
		if token != "42" {
			t.Errorf("expected token '42', got %q", token)
		}
	})

	t.Run("accepts string token", func(t *testing.T) {
		params := json.RawMessage(`{"_meta": {"progressToken": "abc"}}`)
		token := ExtractProgressToken(params)
		if token != "abc" {
			t.Errorf("expected token 'abc', got %q", token)
		}
	})

	t.Run("returns empty for null token", func(t *testing.T) {
		params := json.RawMessage(`{"_meta": {"progressToken": null}}`)
		token := ExtractProgressToken(params)
		if token != "" {
			t.Errorf("expected empty token, got %q", token)
		}
	})

	t.Run("returns empty for absent token", func(t *testing.T) {
		params := json.RawMessage(`{"_meta": {}}`)
		token := ExtractProgressToken(params)
		if token != "" {
			t.Errorf("expected empty token, got %q", token)
		}
	})
}
