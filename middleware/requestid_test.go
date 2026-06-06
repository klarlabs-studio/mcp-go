package middleware

import (
	"context"
	"testing"

	"go.klarlabs.de/mcp/protocol"
)

func TestRequestID(t *testing.T) {
	t.Run("injects request ID into context", func(t *testing.T) {
		var receivedID string

		handler := HandlerFunc(func(ctx context.Context, req *protocol.Request) (*protocol.Response, error) {
			receivedID = RequestIDFromContext(ctx)
			return protocol.NewResponse(req.ID, "ok"), nil
		})

		wrapped := RequestID()(handler)
		_, _ = wrapped(context.Background(), &protocol.Request{Method: "test"})

		if receivedID == "" {
			t.Error("expected request ID to be set")
		}
	})

	t.Run("generates unique IDs for each request", func(t *testing.T) {
		ids := make(map[string]bool)

		handler := HandlerFunc(func(ctx context.Context, req *protocol.Request) (*protocol.Response, error) {
			id := RequestIDFromContext(ctx)
			ids[id] = true
			return protocol.NewResponse(req.ID, "ok"), nil
		})

		wrapped := RequestID()(handler)

		for i := 0; i < 100; i++ {
			_, _ = wrapped(context.Background(), &protocol.Request{Method: "test"})
		}

		if len(ids) != 100 {
			t.Errorf("expected 100 unique IDs, got %d", len(ids))
		}
	})

	t.Run("preserves existing request ID from context", func(t *testing.T) {
		existingID := "existing-request-id"
		ctx := ContextWithRequestID(context.Background(), existingID)

		var receivedID string
		handler := HandlerFunc(func(ctx context.Context, req *protocol.Request) (*protocol.Response, error) {
			receivedID = RequestIDFromContext(ctx)
			return protocol.NewResponse(req.ID, "ok"), nil
		})

		wrapped := RequestID()(handler)
		_, _ = wrapped(ctx, &protocol.Request{Method: "test"})

		if receivedID != existingID {
			t.Errorf("request ID = %q, want %q", receivedID, existingID)
		}
	})
}

func TestRequestIDFromContext(t *testing.T) {
	t.Run("returns empty string for context without ID", func(t *testing.T) {
		id := RequestIDFromContext(context.Background())
		if id != "" {
			t.Errorf("expected empty string, got %q", id)
		}
	})

	t.Run("returns ID from context", func(t *testing.T) {
		expectedID := "test-request-id"
		ctx := ContextWithRequestID(context.Background(), expectedID)

		id := RequestIDFromContext(ctx)
		if id != expectedID {
			t.Errorf("id = %q, want %q", id, expectedID)
		}
	})
}

func TestRequestIDWithGenerator(t *testing.T) {
	t.Run("uses custom generator", func(t *testing.T) {
		customID := "custom-id-123"
		generator := func() string {
			return customID
		}

		var receivedID string
		handler := HandlerFunc(func(ctx context.Context, req *protocol.Request) (*protocol.Response, error) {
			receivedID = RequestIDFromContext(ctx)
			return protocol.NewResponse(req.ID, "ok"), nil
		})

		wrapped := RequestIDWithGenerator(generator)(handler)
		_, _ = wrapped(context.Background(), &protocol.Request{Method: "test"})

		if receivedID != customID {
			t.Errorf("request ID = %q, want %q", receivedID, customID)
		}
	})
}
