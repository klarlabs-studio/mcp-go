package server

import (
	"context"
	"testing"

	"go.klarlabs.de/mcp/protocol"
)

func TestChain(t *testing.T) {
	t.Run("executes middleware in order", func(t *testing.T) {
		var order []int

		m1 := func(next HandlerFunc) HandlerFunc {
			return func(ctx context.Context, req *protocol.Request) (*protocol.Response, error) {
				order = append(order, 1)
				resp, err := next(ctx, req)
				order = append(order, -1)
				return resp, err
			}
		}

		m2 := func(next HandlerFunc) HandlerFunc {
			return func(ctx context.Context, req *protocol.Request) (*protocol.Response, error) {
				order = append(order, 2)
				resp, err := next(ctx, req)
				order = append(order, -2)
				return resp, err
			}
		}

		handler := func(ctx context.Context, req *protocol.Request) (*protocol.Response, error) {
			order = append(order, 0)
			return &protocol.Response{}, nil
		}

		chained := Chain(m1, m2)(handler)
		_, _ = chained(context.Background(), &protocol.Request{})

		expected := []int{1, 2, 0, -2, -1}
		if len(order) != len(expected) {
			t.Fatalf("order = %v, want %v", order, expected)
		}

		for i, v := range expected {
			if order[i] != v {
				t.Errorf("order[%d] = %d, want %d", i, order[i], v)
			}
		}
	})

	t.Run("empty chain returns handler unchanged", func(t *testing.T) {
		called := false
		handler := func(ctx context.Context, req *protocol.Request) (*protocol.Response, error) {
			called = true
			return &protocol.Response{}, nil
		}

		chained := Chain()(handler)
		_, _ = chained(context.Background(), &protocol.Request{})

		if !called {
			t.Error("expected handler to be called")
		}
	})
}
