package middleware

import (
	"context"
	"errors"
	"testing"

	"go.klarlabs.de/mcp/protocol"
)

func TestRecover(t *testing.T) {
	t.Run("passes through normal responses", func(t *testing.T) {
		handler := HandlerFunc(func(ctx context.Context, req *protocol.Request) (*protocol.Response, error) {
			return protocol.NewResponse(req.ID, "success"), nil
		})

		wrapped := Recover()(handler)
		resp, err := wrapped(context.Background(), &protocol.Request{Method: "test"})

		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if resp == nil {
			t.Fatal("expected response")
		}
	})

	t.Run("passes through errors", func(t *testing.T) {
		expectedErr := errors.New("handler error")
		handler := HandlerFunc(func(ctx context.Context, req *protocol.Request) (*protocol.Response, error) {
			return nil, expectedErr
		})

		wrapped := Recover()(handler)
		_, err := wrapped(context.Background(), &protocol.Request{Method: "test"})

		if !errors.Is(err, expectedErr) {
			t.Errorf("error = %v, want %v", err, expectedErr)
		}
	})

	t.Run("catches panic with string", func(t *testing.T) {
		handler := HandlerFunc(func(ctx context.Context, req *protocol.Request) (*protocol.Response, error) {
			panic("something went wrong")
		})

		wrapped := Recover()(handler)
		_, err := wrapped(context.Background(), &protocol.Request{Method: "test"})

		if err == nil {
			t.Fatal("expected error from panic")
		}

		var mcpErr *protocol.Error
		if !errors.As(err, &mcpErr) {
			t.Fatalf("expected protocol.Error, got %T", err)
		}

		if mcpErr.Code != protocol.CodeInternalError {
			t.Errorf("error code = %d, want %d", mcpErr.Code, protocol.CodeInternalError)
		}
	})

	t.Run("catches panic with error", func(t *testing.T) {
		panicErr := errors.New("panic error")
		handler := HandlerFunc(func(ctx context.Context, req *protocol.Request) (*protocol.Response, error) {
			panic(panicErr)
		})

		wrapped := Recover()(handler)
		_, err := wrapped(context.Background(), &protocol.Request{Method: "test"})

		if err == nil {
			t.Fatal("expected error from panic")
		}

		var mcpErr *protocol.Error
		if !errors.As(err, &mcpErr) {
			t.Fatalf("expected protocol.Error, got %T", err)
		}

		if mcpErr.Code != protocol.CodeInternalError {
			t.Errorf("error code = %d, want %d", mcpErr.Code, protocol.CodeInternalError)
		}
	})

	t.Run("catches panic with arbitrary value", func(t *testing.T) {
		handler := HandlerFunc(func(ctx context.Context, req *protocol.Request) (*protocol.Response, error) {
			panic(42) // arbitrary value
		})

		wrapped := Recover()(handler)
		_, err := wrapped(context.Background(), &protocol.Request{Method: "test"})

		if err == nil {
			t.Fatal("expected error from panic")
		}
	})
}

func TestRecoverWithHandler(t *testing.T) {
	t.Run("calls custom handler on panic", func(t *testing.T) {
		var capturedPanic any
		var capturedReq *protocol.Request

		customHandler := func(ctx context.Context, req *protocol.Request, panicVal any) (*protocol.Response, error) {
			capturedPanic = panicVal
			capturedReq = req
			return nil, protocol.NewInternalError("custom: handled panic")
		}

		handler := HandlerFunc(func(ctx context.Context, req *protocol.Request) (*protocol.Response, error) {
			panic("test panic")
		})

		req := &protocol.Request{Method: "test/method"}
		wrapped := RecoverWithHandler(customHandler)(handler)
		_, err := wrapped(context.Background(), req)

		if err == nil {
			t.Fatal("expected error")
		}
		if capturedPanic != "test panic" {
			t.Errorf("capturedPanic = %v, want %q", capturedPanic, "test panic")
		}
		if capturedReq != req {
			t.Error("request was not passed to handler")
		}
	})
}
