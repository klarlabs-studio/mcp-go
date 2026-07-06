package mcp

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

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
