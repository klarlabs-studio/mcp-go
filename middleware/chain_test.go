package middleware

import (
	"context"
	"testing"

	"go.klarlabs.de/mcp/protocol"
)

func TestChain(t *testing.T) {
	t.Run("empty chain returns handler unchanged", func(t *testing.T) {
		called := false
		handler := HandlerFunc(func(ctx context.Context, req *protocol.Request) (*protocol.Response, error) {
			called = true
			return protocol.NewResponse(req.ID, "ok"), nil
		})

		chained := Chain()(handler)
		_, err := chained(context.Background(), &protocol.Request{Method: "test"})

		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !called {
			t.Error("handler was not called")
		}
	})

	t.Run("single middleware wraps handler", func(t *testing.T) {
		order := []string{}

		middleware := func(next HandlerFunc) HandlerFunc {
			return func(ctx context.Context, req *protocol.Request) (*protocol.Response, error) {
				order = append(order, "before")
				resp, err := next(ctx, req)
				order = append(order, "after")
				return resp, err
			}
		}

		handler := HandlerFunc(func(ctx context.Context, req *protocol.Request) (*protocol.Response, error) {
			order = append(order, "handler")
			return protocol.NewResponse(req.ID, "ok"), nil
		})

		chained := Chain(middleware)(handler)
		_, _ = chained(context.Background(), &protocol.Request{Method: "test"})

		expected := []string{"before", "handler", "after"}
		if len(order) != len(expected) {
			t.Fatalf("order = %v, want %v", order, expected)
		}
		for i, v := range expected {
			if order[i] != v {
				t.Errorf("order[%d] = %q, want %q", i, order[i], v)
			}
		}
	})

	t.Run("multiple middleware execute in order", func(t *testing.T) {
		order := []string{}

		middleware1 := func(next HandlerFunc) HandlerFunc {
			return func(ctx context.Context, req *protocol.Request) (*protocol.Response, error) {
				order = append(order, "m1-before")
				resp, err := next(ctx, req)
				order = append(order, "m1-after")
				return resp, err
			}
		}

		middleware2 := func(next HandlerFunc) HandlerFunc {
			return func(ctx context.Context, req *protocol.Request) (*protocol.Response, error) {
				order = append(order, "m2-before")
				resp, err := next(ctx, req)
				order = append(order, "m2-after")
				return resp, err
			}
		}

		middleware3 := func(next HandlerFunc) HandlerFunc {
			return func(ctx context.Context, req *protocol.Request) (*protocol.Response, error) {
				order = append(order, "m3-before")
				resp, err := next(ctx, req)
				order = append(order, "m3-after")
				return resp, err
			}
		}

		handler := HandlerFunc(func(ctx context.Context, req *protocol.Request) (*protocol.Response, error) {
			order = append(order, "handler")
			return protocol.NewResponse(req.ID, "ok"), nil
		})

		chained := Chain(middleware1, middleware2, middleware3)(handler)
		_, _ = chained(context.Background(), &protocol.Request{Method: "test"})

		// Middleware should wrap in order: m1 wraps m2 wraps m3 wraps handler
		expected := []string{"m1-before", "m2-before", "m3-before", "handler", "m3-after", "m2-after", "m1-after"}
		if len(order) != len(expected) {
			t.Fatalf("order = %v, want %v", order, expected)
		}
		for i, v := range expected {
			if order[i] != v {
				t.Errorf("order[%d] = %q, want %q", i, order[i], v)
			}
		}
	})

	t.Run("middleware can short-circuit chain", func(t *testing.T) {
		handlerCalled := false

		blockingMiddleware := func(next HandlerFunc) HandlerFunc {
			return func(ctx context.Context, req *protocol.Request) (*protocol.Response, error) {
				// Don't call next, return early
				return nil, protocol.NewUnauthorized("blocked")
			}
		}

		handler := HandlerFunc(func(ctx context.Context, req *protocol.Request) (*protocol.Response, error) {
			handlerCalled = true
			return protocol.NewResponse(req.ID, "ok"), nil
		})

		chained := Chain(blockingMiddleware)(handler)
		_, err := chained(context.Background(), &protocol.Request{Method: "test"})

		if err == nil {
			t.Error("expected error from blocking middleware")
		}
		if handlerCalled {
			t.Error("handler should not have been called")
		}
	})
}

func TestUse(t *testing.T) {
	t.Run("appends middleware to existing chain", func(t *testing.T) {
		order := []string{}

		m1 := func(next HandlerFunc) HandlerFunc {
			return func(ctx context.Context, req *protocol.Request) (*protocol.Response, error) {
				order = append(order, "m1")
				return next(ctx, req)
			}
		}

		m2 := func(next HandlerFunc) HandlerFunc {
			return func(ctx context.Context, req *protocol.Request) (*protocol.Response, error) {
				order = append(order, "m2")
				return next(ctx, req)
			}
		}

		handler := HandlerFunc(func(ctx context.Context, req *protocol.Request) (*protocol.Response, error) {
			order = append(order, "handler")
			return protocol.NewResponse(req.ID, "ok"), nil
		})

		// Build chain incrementally
		chain := Use(m1)
		chain = chain.Append(m2)
		chained := chain.Then(handler)

		_, _ = chained(context.Background(), &protocol.Request{Method: "test"})

		expected := []string{"m1", "m2", "handler"}
		if len(order) != len(expected) {
			t.Fatalf("order = %v, want %v", order, expected)
		}
		for i, v := range expected {
			if order[i] != v {
				t.Errorf("order[%d] = %q, want %q", i, order[i], v)
			}
		}
	})
}
