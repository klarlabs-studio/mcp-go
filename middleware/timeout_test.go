package middleware

import (
	"context"
	"errors"
	"testing"
	"time"

	"go.klarlabs.de/mcp/protocol"
)

func TestTimeout(t *testing.T) {
	t.Run("allows fast requests through", func(t *testing.T) {
		handler := HandlerFunc(func(ctx context.Context, req *protocol.Request) (*protocol.Response, error) {
			return protocol.NewResponse(req.ID, "fast"), nil
		})

		wrapped := Timeout(time.Second)(handler)
		resp, err := wrapped(context.Background(), &protocol.Request{Method: "test"})

		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if resp == nil {
			t.Fatal("expected response")
		}
	})

	t.Run("sets deadline on context", func(t *testing.T) {
		var receivedCtx context.Context

		handler := HandlerFunc(func(ctx context.Context, req *protocol.Request) (*protocol.Response, error) {
			receivedCtx = ctx
			return protocol.NewResponse(req.ID, "ok"), nil
		})

		wrapped := Timeout(100 * time.Millisecond)(handler)
		_, _ = wrapped(context.Background(), &protocol.Request{Method: "test"})

		deadline, ok := receivedCtx.Deadline()
		if !ok {
			t.Fatal("expected context to have deadline")
		}
		if deadline.Before(time.Now()) {
			t.Error("deadline should be in the future")
		}
	})

	t.Run("times out slow requests", func(t *testing.T) {
		handler := HandlerFunc(func(ctx context.Context, req *protocol.Request) (*protocol.Response, error) {
			select {
			case <-time.After(500 * time.Millisecond):
				return protocol.NewResponse(req.ID, "slow"), nil
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		})

		wrapped := Timeout(50 * time.Millisecond)(handler)
		_, err := wrapped(context.Background(), &protocol.Request{Method: "test"})

		if err == nil {
			t.Fatal("expected timeout error")
		}
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Errorf("error = %v, want context.DeadlineExceeded", err)
		}
	})

	t.Run("respects parent context cancellation", func(t *testing.T) {
		handler := HandlerFunc(func(ctx context.Context, req *protocol.Request) (*protocol.Response, error) {
			<-ctx.Done()
			return nil, ctx.Err()
		})

		parentCtx, cancel := context.WithCancel(context.Background())

		wrapped := Timeout(10 * time.Second)(handler)

		errCh := make(chan error, 1)
		go func() {
			_, err := wrapped(parentCtx, &protocol.Request{Method: "test"})
			errCh <- err
		}()

		// Cancel parent context
		time.Sleep(10 * time.Millisecond)
		cancel()

		select {
		case err := <-errCh:
			if !errors.Is(err, context.Canceled) {
				t.Errorf("error = %v, want context.Canceled", err)
			}
		case <-time.After(time.Second):
			t.Fatal("handler did not respond to cancellation")
		}
	})

	t.Run("bounds a non-cooperative handler", func(t *testing.T) {
		// A handler that ignores ctx entirely. The old implementation blocked
		// until it returned (500ms); the fixed one returns at the deadline.
		handler := HandlerFunc(func(_ context.Context, req *protocol.Request) (*protocol.Response, error) {
			time.Sleep(500 * time.Millisecond)
			return protocol.NewResponse(req.ID, "slow"), nil
		})

		wrapped := Timeout(50 * time.Millisecond)(handler)
		start := time.Now()
		_, err := wrapped(context.Background(), &protocol.Request{Method: "test"})
		elapsed := time.Since(start)

		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("error = %v, want context.DeadlineExceeded", err)
		}
		if elapsed > 250*time.Millisecond {
			t.Fatalf("timeout did not bound a non-cooperative handler: took %v", elapsed)
		}
	})

	t.Run("passes through handler errors", func(t *testing.T) {
		expectedErr := protocol.NewInvalidParams("bad params")
		handler := HandlerFunc(func(ctx context.Context, req *protocol.Request) (*protocol.Response, error) {
			return nil, expectedErr
		})

		wrapped := Timeout(time.Second)(handler)
		_, err := wrapped(context.Background(), &protocol.Request{Method: "test"})

		if !errors.Is(err, expectedErr) {
			t.Errorf("error = %v, want %v", err, expectedErr)
		}
	})
}
