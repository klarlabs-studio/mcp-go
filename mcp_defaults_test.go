package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"log"
	"strings"
	"testing"
	"time"

	"go.klarlabs.de/mcp/protocol"
)

// TestServerUseMiddlewareIsApplied proves that middleware registered via
// Server.Use is actually executed. Previously s.middleware was never read by
// the serve path, so Use(...) was a silent no-op.
func TestServerUseMiddlewareIsApplied(t *testing.T) {
	srv := NewServer(ServerInfo{Name: "test", Version: "1.0.0"})

	called := false
	srv.Use(func(next MiddlewareHandlerFunc) MiddlewareHandlerFunc {
		return func(ctx context.Context, req *protocol.Request) (*protocol.Response, error) {
			called = true
			return next(ctx, req)
		}
	})

	type Input struct{}
	srv.Tool("ping").Handler(func(_ Input) (string, error) { return "pong", nil })

	handler := newRequestHandler(srv)
	req := &protocol.Request{
		JSONRPC: "2.0",
		ID:      json.RawMessage(`1`),
		Method:  "tools/call",
		Params:  json.RawMessage(`{"name":"ping","arguments":{}}`),
	}
	if _, err := handler.HandleRequest(context.Background(), req); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !called {
		t.Fatal("middleware registered via Server.Use was not applied")
	}
}

// TestRecoverIsOnByDefault proves a panicking handler no longer crashes the
// process on a plain server (no explicitly-added middleware), and that the
// panic detail is not leaked to the peer.
func TestRecoverIsOnByDefault(t *testing.T) {
	srv := NewServer(ServerInfo{Name: "test", Version: "1.0.0"})

	type Input struct{}
	srv.Tool("boom").Handler(func(_ Input) (string, error) {
		panic("secret detail: /etc/shadow")
	})

	handler := newRequestHandler(srv) // no WithMiddleware, no Use
	req := &protocol.Request{
		JSONRPC: "2.0",
		ID:      json.RawMessage(`1`),
		Method:  "tools/call",
		Params:  json.RawMessage(`{"name":"boom","arguments":{}}`),
	}

	// Must not panic (the test process would crash); must return an error.
	_, err := handler.HandleRequest(context.Background(), req)
	if err == nil {
		t.Fatal("expected an error from a panicking handler, got nil")
	}
	// The panic value must not reach the client.
	if strings.Contains(err.Error(), "shadow") || strings.Contains(err.Error(), "secret") {
		t.Fatalf("panic detail leaked to client: %v", err)
	}
	if !strings.Contains(strings.ToLower(err.Error()), "internal error") {
		t.Fatalf("expected a generic internal error, got: %v", err)
	}
}

// TestRawHandlerErrorIsSanitized proves that a handler returning a raw error
// with secret detail yields a GENERIC internal error to the peer, while the
// detail is logged server-side. This is the non-panic counterpart to
// TestRecoverIsOnByDefault.
func TestRawHandlerErrorIsSanitized(t *testing.T) {
	srv := NewServer(ServerInfo{Name: "test", Version: "1.0.0"})

	const secret = "failed reading /etc/shadow: permission denied"
	type Input struct{}
	srv.Tool("leaky").Handler(func(_ Input) (string, error) {
		return "", errorsNew(secret)
	})

	// Capture server-side log output.
	var logBuf bytes.Buffer
	restore := log.Writer()
	flags := log.Flags()
	log.SetOutput(&logBuf)
	log.SetFlags(0)
	defer func() {
		log.SetOutput(restore)
		log.SetFlags(flags)
	}()

	handler := newRequestHandler(srv)
	req := &protocol.Request{
		JSONRPC: "2.0",
		ID:      json.RawMessage(`1`),
		Method:  "tools/call",
		Params:  json.RawMessage(`{"name":"leaky","arguments":{}}`),
	}

	_, err := handler.HandleRequest(context.Background(), req)
	if err == nil {
		t.Fatal("expected an error from a failing handler, got nil")
	}
	if strings.Contains(err.Error(), "shadow") || strings.Contains(err.Error(), "permission") {
		t.Fatalf("internal detail leaked to client: %v", err)
	}
	if !strings.Contains(strings.ToLower(err.Error()), "internal error") {
		t.Fatalf("expected a generic internal error, got: %v", err)
	}
	if !strings.Contains(logBuf.String(), "shadow") {
		t.Fatalf("expected the internal detail to be logged server-side, log = %q", logBuf.String())
	}
}

// TestCancelledNotificationCancelsInFlight proves that an incoming
// notifications/cancelled cancels the context of a matching in-flight request.
func TestCancelledNotificationCancelsInFlight(t *testing.T) {
	srv := NewServer(ServerInfo{Name: "test", Version: "1.0.0"})

	started := make(chan struct{})
	type Input struct{}
	srv.Tool("block").Handler(func(ctx context.Context, _ Input) (string, error) {
		close(started)
		<-ctx.Done() // unblocks only when the request context is canceled
		return "", ctx.Err()
	})

	handler := newRequestHandler(srv)

	returned := make(chan struct{})
	go func() {
		_, _ = handler.HandleRequest(context.Background(), &protocol.Request{
			JSONRPC: "2.0",
			ID:      json.RawMessage(`1`),
			Method:  "tools/call",
			Params:  json.RawMessage(`{"name":"block","arguments":{}}`),
		})
		close(returned)
	}()

	<-started

	// Deliver notifications/cancelled for request id 1 (a notification: no ID).
	if _, err := handler.HandleRequest(context.Background(), &protocol.Request{
		JSONRPC: "2.0",
		Method:  "notifications/cancelled",
		Params:  json.RawMessage(`{"requestId":1,"reason":"user requested"}`),
	}); err != nil {
		t.Fatalf("cancelled notification returned error: %v", err)
	}

	select {
	case <-returned:
	case <-time.After(2 * time.Second):
		t.Fatal("notifications/cancelled did not cancel the in-flight request")
	}
}

// errorsNew returns a plain (non-protocol) error carrying the given message,
// exercising the sanitization path for library-user handler failures.
func errorsNew(msg string) error { return &plainError{msg} }

type plainError struct{ msg string }

func (e *plainError) Error() string { return e.msg }
