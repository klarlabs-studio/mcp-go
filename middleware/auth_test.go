package middleware_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"go.klarlabs.de/mcp/middleware"
	"go.klarlabs.de/mcp/protocol"
)

func TestAuth(t *testing.T) {
	validIdentity := &middleware.Identity{
		ID:   "user-123",
		Name: "Test User",
	}

	authenticator := func(ctx context.Context, req *protocol.Request) (*middleware.Identity, error) {
		key := protocol.GetRequestMeta(ctx, "X-API-Key")
		if key == "valid-key" {
			return validIdentity, nil
		}
		if key == "error-key" {
			return nil, errors.New("auth error")
		}
		return nil, nil
	}

	t.Run("allows authenticated requests", func(t *testing.T) {
		m := middleware.Auth(authenticator)

		handler := m(func(ctx context.Context, req *protocol.Request) (*protocol.Response, error) {
			identity := middleware.IdentityFromContext(ctx)
			if identity == nil {
				t.Error("expected identity in context")
			} else if identity.ID != "user-123" {
				t.Errorf("expected ID 'user-123', got %q", identity.ID)
			}
			return protocol.NewResponse(req.ID, "ok"), nil
		})

		ctx := protocol.ContextWithRequestMeta(context.Background(), protocol.RequestMeta{
			"X-API-Key": "valid-key",
		})
		req := &protocol.Request{
			JSONRPC: "2.0",
			ID:      json.RawMessage(`1`),
			Method:  "tools/call",
		}

		resp, err := handler(ctx, req)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if resp == nil {
			t.Fatal("expected response")
		}
	})

	t.Run("rejects unauthenticated requests", func(t *testing.T) {
		m := middleware.Auth(authenticator)

		handler := m(func(ctx context.Context, req *protocol.Request) (*protocol.Response, error) {
			t.Error("handler should not be called")
			return nil, nil
		})

		req := &protocol.Request{
			JSONRPC: "2.0",
			ID:      json.RawMessage(`1`),
			Method:  "tools/call",
		}

		_, err := handler(context.Background(), req)
		if err == nil {
			t.Fatal("expected error")
		}

		protoErr, ok := err.(*protocol.Error)
		if !ok {
			t.Fatalf("expected protocol.Error, got %T", err)
		}

		if protoErr.Code != protocol.CodeUnauthorized {
			t.Errorf("expected code %d, got %d", protocol.CodeUnauthorized, protoErr.Code)
		}
	})

	t.Run("rejects on auth error", func(t *testing.T) {
		m := middleware.Auth(authenticator)

		handler := m(func(ctx context.Context, req *protocol.Request) (*protocol.Response, error) {
			t.Error("handler should not be called")
			return nil, nil
		})

		ctx := protocol.ContextWithRequestMeta(context.Background(), protocol.RequestMeta{
			"X-API-Key": "error-key",
		})
		req := &protocol.Request{
			JSONRPC: "2.0",
			ID:      json.RawMessage(`1`),
			Method:  "tools/call",
		}

		_, err := handler(ctx, req)
		if err == nil {
			t.Fatal("expected error")
		}

		protoErr, ok := err.(*protocol.Error)
		if !ok {
			t.Fatalf("expected protocol.Error, got %T", err)
		}

		if protoErr.Code != protocol.CodeUnauthorized {
			t.Errorf("expected code %d, got %d", protocol.CodeUnauthorized, protoErr.Code)
		}
	})

	t.Run("skips initialize method", func(t *testing.T) {
		m := middleware.Auth(authenticator)

		handlerCalled := false
		handler := m(func(ctx context.Context, req *protocol.Request) (*protocol.Response, error) {
			handlerCalled = true
			return protocol.NewResponse(req.ID, "ok"), nil
		})

		req := &protocol.Request{
			JSONRPC: "2.0",
			ID:      json.RawMessage(`1`),
			Method:  protocol.MethodInitialize,
		}

		_, err := handler(context.Background(), req)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !handlerCalled {
			t.Error("handler should have been called")
		}
	})

	t.Run("skips ping method", func(t *testing.T) {
		m := middleware.Auth(authenticator)

		handlerCalled := false
		handler := m(func(ctx context.Context, req *protocol.Request) (*protocol.Response, error) {
			handlerCalled = true
			return protocol.NewResponse(req.ID, "ok"), nil
		})

		req := &protocol.Request{
			JSONRPC: "2.0",
			ID:      json.RawMessage(`1`),
			Method:  protocol.MethodPing,
		}

		_, err := handler(context.Background(), req)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !handlerCalled {
			t.Error("handler should have been called")
		}
	})

	t.Run("custom skip methods", func(t *testing.T) {
		m := middleware.Auth(authenticator, middleware.WithAuthSkipMethods("custom/method"))

		handlerCalled := false
		handler := m(func(ctx context.Context, req *protocol.Request) (*protocol.Response, error) {
			handlerCalled = true
			return protocol.NewResponse(req.ID, "ok"), nil
		})

		req := &protocol.Request{
			JSONRPC: "2.0",
			ID:      json.RawMessage(`1`),
			Method:  "custom/method",
		}

		_, err := handler(context.Background(), req)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !handlerCalled {
			t.Error("handler should have been called")
		}
	})

	t.Run("custom error message", func(t *testing.T) {
		m := middleware.Auth(authenticator, middleware.WithAuthErrorMessage("custom error"))

		handler := m(func(ctx context.Context, req *protocol.Request) (*protocol.Response, error) {
			return nil, nil
		})

		req := &protocol.Request{
			JSONRPC: "2.0",
			ID:      json.RawMessage(`1`),
			Method:  "tools/call",
		}

		_, err := handler(context.Background(), req)
		protoErr := err.(*protocol.Error)
		if protoErr.Message != "custom error" {
			t.Errorf("expected 'custom error', got %q", protoErr.Message)
		}
	})
}

func TestStaticAPIKeys(t *testing.T) {
	keys := map[string]*middleware.Identity{
		"key-1": {ID: "user-1", Name: "User One"},
		"key-2": {ID: "user-2", Name: "User Two"},
	}

	validator := middleware.StaticAPIKeys(keys)

	t.Run("valid key", func(t *testing.T) {
		identity := validator("key-1")
		if identity == nil {
			t.Fatal("expected identity")
		}
		if identity.ID != "user-1" {
			t.Errorf("expected ID 'user-1', got %q", identity.ID)
		}
	})

	t.Run("invalid key", func(t *testing.T) {
		identity := validator("invalid")
		if identity != nil {
			t.Error("expected nil for invalid key")
		}
	})
}

func TestChainAuthenticators(t *testing.T) {
	auth1 := func(ctx context.Context, req *protocol.Request) (*middleware.Identity, error) {
		if protocol.GetRequestMeta(ctx, "Auth1") == "valid" {
			return &middleware.Identity{ID: "auth1-user"}, nil
		}
		return nil, nil
	}

	auth2 := func(ctx context.Context, req *protocol.Request) (*middleware.Identity, error) {
		if protocol.GetRequestMeta(ctx, "Auth2") == "valid" {
			return &middleware.Identity{ID: "auth2-user"}, nil
		}
		return nil, nil
	}

	chained := middleware.ChainAuthenticators(auth1, auth2)

	t.Run("first authenticator succeeds", func(t *testing.T) {
		ctx := protocol.ContextWithRequestMeta(context.Background(), protocol.RequestMeta{
			"Auth1": "valid",
		})
		identity, err := chained(ctx, nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if identity == nil || identity.ID != "auth1-user" {
			t.Error("expected auth1-user identity")
		}
	})

	t.Run("second authenticator succeeds", func(t *testing.T) {
		ctx := protocol.ContextWithRequestMeta(context.Background(), protocol.RequestMeta{
			"Auth2": "valid",
		})
		identity, err := chained(ctx, nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if identity == nil || identity.ID != "auth2-user" {
			t.Error("expected auth2-user identity")
		}
	})

	t.Run("no authenticator succeeds", func(t *testing.T) {
		identity, err := chained(context.Background(), nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if identity != nil {
			t.Error("expected nil identity")
		}
	})
}

func TestIdentityContext(t *testing.T) {
	t.Run("get identity from context", func(t *testing.T) {
		identity := &middleware.Identity{
			ID:       "test-id",
			Name:     "Test",
			Metadata: map[string]any{"role": "admin"},
		}

		ctx := middleware.ContextWithIdentity(context.Background(), identity)
		got := middleware.IdentityFromContext(ctx)

		if got == nil {
			t.Fatal("expected identity")
		}
		if got.ID != "test-id" {
			t.Errorf("expected ID 'test-id', got %q", got.ID)
		}
		if got.Metadata["role"] != "admin" {
			t.Error("expected role admin in metadata")
		}
	})

	t.Run("no identity in context", func(t *testing.T) {
		got := middleware.IdentityFromContext(context.Background())
		if got != nil {
			t.Error("expected nil identity")
		}
	})
}
