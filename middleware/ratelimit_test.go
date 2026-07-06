package middleware_test

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"go.klarlabs.de/mcp/middleware"
	"go.klarlabs.de/mcp/protocol"
)

func TestRateLimit(t *testing.T) {
	t.Run("allows requests within limit", func(t *testing.T) {
		m := middleware.RateLimit(10, 10)

		handler := m(func(ctx context.Context, req *protocol.Request) (*protocol.Response, error) {
			return protocol.NewResponse(req.ID, "ok"), nil
		})

		req := &protocol.Request{
			JSONRPC: "2.0",
			ID:      json.RawMessage(`1`),
			Method:  "test",
		}

		// Should allow requests within limit
		for i := 0; i < 5; i++ {
			resp, err := handler(context.Background(), req)
			if err != nil {
				t.Fatalf("request %d: unexpected error: %v", i, err)
			}
			if resp == nil {
				t.Fatalf("request %d: expected response", i)
			}
		}
	})

	t.Run("rejects requests exceeding limit", func(t *testing.T) {
		// Very low limit
		m := middleware.RateLimit(1, 1)

		handler := m(func(ctx context.Context, req *protocol.Request) (*protocol.Response, error) {
			return protocol.NewResponse(req.ID, "ok"), nil
		})

		req := &protocol.Request{
			JSONRPC: "2.0",
			ID:      json.RawMessage(`1`),
			Method:  "test",
		}

		// First request should succeed
		resp, err := handler(context.Background(), req)
		if err != nil {
			t.Fatalf("first request failed: %v", err)
		}
		if resp == nil {
			t.Fatal("expected response for first request")
		}

		// Second request should be rate limited
		_, err = handler(context.Background(), req)
		if err == nil {
			t.Fatal("expected rate limit error")
		}

		protoErr, ok := err.(*protocol.Error)
		if !ok {
			t.Fatalf("expected protocol.Error, got %T", err)
		}

		if protoErr.Code != protocol.CodeRateLimited {
			t.Errorf("expected code %d, got %d", protocol.CodeRateLimited, protoErr.Code)
		}
	})

	t.Run("respects burst capacity", func(t *testing.T) {
		// Rate 1/s, burst 5
		m := middleware.RateLimit(1, 5)

		handler := m(func(ctx context.Context, req *protocol.Request) (*protocol.Response, error) {
			return protocol.NewResponse(req.ID, "ok"), nil
		})

		req := &protocol.Request{
			JSONRPC: "2.0",
			ID:      json.RawMessage(`1`),
			Method:  "test",
		}

		// All 5 burst requests should succeed
		for i := 0; i < 5; i++ {
			_, err := handler(context.Background(), req)
			if err != nil {
				t.Fatalf("burst request %d failed: %v", i, err)
			}
		}

		// 6th request should fail
		_, err := handler(context.Background(), req)
		if err == nil {
			t.Fatal("expected rate limit error after burst")
		}
	})
}

func TestRateLimitByMethod(t *testing.T) {
	t.Run("limits each method separately", func(t *testing.T) {
		m := middleware.RateLimitByMethod(1, 1)

		handler := m(func(ctx context.Context, req *protocol.Request) (*protocol.Response, error) {
			return protocol.NewResponse(req.ID, "ok"), nil
		})

		method1 := &protocol.Request{
			JSONRPC: "2.0",
			ID:      json.RawMessage(`1`),
			Method:  "method1",
		}

		method2 := &protocol.Request{
			JSONRPC: "2.0",
			ID:      json.RawMessage(`2`),
			Method:  "method2",
		}

		// First method1 should succeed
		_, err := handler(context.Background(), method1)
		if err != nil {
			t.Fatalf("method1 first request failed: %v", err)
		}

		// method2 should also succeed (different key)
		_, err = handler(context.Background(), method2)
		if err != nil {
			t.Fatalf("method2 first request failed: %v", err)
		}

		// Second method1 should be limited
		_, err = handler(context.Background(), method1)
		if err == nil {
			t.Fatal("expected method1 to be rate limited")
		}
	})
}

func TestRateLimitByClient(t *testing.T) {
	t.Run("limits each client separately", func(t *testing.T) {
		m := middleware.RateLimitByClient(1, 1, func(req *protocol.Request) string {
			// Extract client ID from params
			var params map[string]string
			if req.Params != nil {
				json.Unmarshal(req.Params, &params)
			}
			return params["client_id"]
		})

		handler := m(func(ctx context.Context, req *protocol.Request) (*protocol.Response, error) {
			return protocol.NewResponse(req.ID, "ok"), nil
		})

		client1 := &protocol.Request{
			JSONRPC: "2.0",
			ID:      json.RawMessage(`1`),
			Method:  "test",
			Params:  json.RawMessage(`{"client_id": "client1"}`),
		}

		client2 := &protocol.Request{
			JSONRPC: "2.0",
			ID:      json.RawMessage(`2`),
			Method:  "test",
			Params:  json.RawMessage(`{"client_id": "client2"}`),
		}

		// First client1 should succeed
		_, err := handler(context.Background(), client1)
		if err != nil {
			t.Fatalf("client1 first request failed: %v", err)
		}

		// client2 should also succeed (different key)
		_, err = handler(context.Background(), client2)
		if err != nil {
			t.Fatalf("client2 first request failed: %v", err)
		}

		// Second client1 should be limited
		_, err = handler(context.Background(), client1)
		if err == nil {
			t.Fatal("expected client1 to be rate limited")
		}
	})
}

func TestRateLimit_Concurrent(t *testing.T) {
	t.Run("handles concurrent requests", func(t *testing.T) {
		// 10 requests per second, burst of 10
		m := middleware.RateLimit(10, 10)

		handler := m(func(ctx context.Context, req *protocol.Request) (*protocol.Response, error) {
			return protocol.NewResponse(req.ID, "ok"), nil
		})

		var wg sync.WaitGroup
		var allowed, denied int
		var mu sync.Mutex

		// Fire 20 concurrent requests
		for i := 0; i < 20; i++ {
			wg.Add(1)
			go func(id int) {
				defer wg.Done()

				req := &protocol.Request{
					JSONRPC: "2.0",
					ID:      json.RawMessage(`1`),
					Method:  "test",
				}

				_, err := handler(context.Background(), req)

				mu.Lock()
				if err == nil {
					allowed++
				} else {
					denied++
				}
				mu.Unlock()
			}(i)
		}

		wg.Wait()

		// Should have allowed burst (10) and denied the rest
		if allowed < 5 || allowed > 15 {
			t.Errorf("expected around 10 allowed, got %d", allowed)
		}

		if denied < 5 || denied > 15 {
			t.Errorf("expected around 10 denied, got %d", denied)
		}
	})
}

func TestRateLimit_Recovery(t *testing.T) {
	t.Run("recovers tokens over time", func(t *testing.T) {
		// 10 requests per second, burst 1
		m := middleware.RateLimit(10, 1)

		handler := m(func(ctx context.Context, req *protocol.Request) (*protocol.Response, error) {
			return protocol.NewResponse(req.ID, "ok"), nil
		})

		req := &protocol.Request{
			JSONRPC: "2.0",
			ID:      json.RawMessage(`1`),
			Method:  "test",
		}

		// First request should succeed
		_, err := handler(context.Background(), req)
		if err != nil {
			t.Fatalf("first request failed: %v", err)
		}

		// Second request should be limited
		_, err = handler(context.Background(), req)
		if err == nil {
			t.Fatal("expected rate limit")
		}

		// Wait for token recovery (100ms for 10/s rate)
		time.Sleep(150 * time.Millisecond)

		// Should now succeed
		_, err = handler(context.Background(), req)
		if err != nil {
			t.Fatalf("after recovery: %v", err)
		}
	})
}

func TestRateLimit_DefaultPerClientFromContext(t *testing.T) {
	// With a client id on the context, the default bucket key isolates clients:
	// one client exhausting its budget must not rate-limit another.
	m := middleware.RateLimit(1, 1)
	handler := m(func(ctx context.Context, req *protocol.Request) (*protocol.Response, error) {
		return protocol.NewResponse(req.ID, "ok"), nil
	})
	req := &protocol.Request{JSONRPC: "2.0", ID: json.RawMessage(`1`), Method: "test"}

	ctxA := middleware.ContextWithClientID(context.Background(), "clientA")
	ctxB := middleware.ContextWithClientID(context.Background(), "clientB")

	if _, err := handler(ctxA, req); err != nil {
		t.Fatalf("clientA first request: %v", err)
	}
	// clientB is a distinct bucket and must still be allowed.
	if _, err := handler(ctxB, req); err != nil {
		t.Fatalf("clientB first request should be independent: %v", err)
	}
	// clientA's second request is over its own budget.
	if _, err := handler(ctxA, req); err == nil {
		t.Fatal("expected clientA to be rate limited on its own bucket")
	}
}

func TestRateLimit_DefaultGlobalWithoutClientID(t *testing.T) {
	// Without a client id, the documented fallback is a single global bucket.
	m := middleware.RateLimit(1, 1)
	handler := m(func(ctx context.Context, req *protocol.Request) (*protocol.Response, error) {
		return protocol.NewResponse(req.ID, "ok"), nil
	})
	req := &protocol.Request{JSONRPC: "2.0", ID: json.RawMessage(`1`), Method: "test"}

	if _, err := handler(context.Background(), req); err != nil {
		t.Fatalf("first request: %v", err)
	}
	if _, err := handler(context.Background(), req); err == nil {
		t.Fatal("expected global bucket to rate limit the second request")
	}
}
