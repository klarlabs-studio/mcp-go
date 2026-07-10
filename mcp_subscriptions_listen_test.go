package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"go.klarlabs.de/mcp/protocol"
	"go.klarlabs.de/mcp/server"
)

// TestSubscriptionsListen_ReturnsSubscriptionID verifies that a modern
// subscriptions/listen request yields a non-empty subscriptionId and, being a
// modern request, gets resultType:"complete" stamped.
func TestSubscriptionsListen_ReturnsSubscriptionID(t *testing.T) {
	srv := NewServer(ServerInfo{Name: "s", Version: "1"})
	handler := newRequestHandler(srv)

	req := &protocol.Request{
		JSONRPC: "2.0", ID: json.RawMessage(`1`), Method: protocol.MethodSubscriptionsListen,
		Params: modernParams(t, protocol.DraftVersion, map[string]any{
			"notifications": []string{protocol.MethodResourceUpdated},
			"uris":          []string{"email://inbox"},
		}),
	}
	resp, err := handler.HandleRequest(context.Background(), req)
	if err != nil {
		t.Fatalf("subscriptions/listen: %v", err)
	}
	result, _ := resp.Result.(map[string]any)
	if id, _ := result["subscriptionId"].(string); id == "" {
		t.Fatalf("expected a non-empty subscriptionId, got %v", result)
	}
	if result["resultType"] != protocol.ResultTypeComplete {
		t.Errorf("expected resultType complete, got %v", result["resultType"])
	}
}

// TestSubscriptionsListen_RegistersURIs verifies the requested resource URIs are
// registered on the session's SubscriptionManager, reusing the same machinery
// resources/subscribe drives. The handler is invoked directly with a manually
// attached session so the registration can be observed after the call.
func TestSubscriptionsListen_RegistersURIs(t *testing.T) {
	srv := NewServer(ServerInfo{Name: "s", Version: "1"})
	handler := newRequestHandler(srv)

	session := server.NewSession("modern", nil, nil)
	ctx := server.ContextWithSession(context.Background(), session)

	req := &protocol.Request{
		JSONRPC: "2.0", ID: json.RawMessage(`1`), Method: protocol.MethodSubscriptionsListen,
		Params: mustParams(t, map[string]any{
			"notifications": []string{protocol.MethodResourceUpdated},
			"uris":          []string{"email://inbox", "docs://readme"},
		}),
	}
	if _, err := handler.handleSubscriptionsListen(ctx, req); err != nil {
		t.Fatalf("handleSubscriptionsListen: %v", err)
	}

	mgr := session.SubscriptionManager()
	for _, uri := range []string{"email://inbox", "docs://readme"} {
		if !mgr.IsSubscribed(session.ID(), uri) {
			t.Errorf("URI %q was not registered on the subscription manager", uri)
		}
	}
}

// TestSubscriptionsListen_NoURIsStillReturnsID verifies a listen with no URIs
// (notification-type opt-in only) still returns a subscription id and registers
// nothing.
func TestSubscriptionsListen_NoURIsStillReturnsID(t *testing.T) {
	srv := NewServer(ServerInfo{Name: "s", Version: "1"})
	handler := newRequestHandler(srv)

	session := server.NewSession("modern", nil, nil)
	ctx := server.ContextWithSession(context.Background(), session)

	req := &protocol.Request{
		JSONRPC: "2.0", ID: json.RawMessage(`1`), Method: protocol.MethodSubscriptionsListen,
		Params: mustParams(t, map[string]any{
			"notifications": []string{protocol.MethodToolListChanged},
		}),
	}
	resp, err := handler.handleSubscriptionsListen(ctx, req)
	if err != nil {
		t.Fatalf("handleSubscriptionsListen: %v", err)
	}
	if id, _ := resp.Result.(map[string]any)["subscriptionId"].(string); id == "" {
		t.Fatalf("expected a non-empty subscriptionId, got %v", resp.Result)
	}
	if n := session.SubscriptionManager().SubscriptionCount(); n != 0 {
		t.Errorf("expected no subscriptions registered, got %d", n)
	}
}

// TestSubscriptionsListen_EmptyURIRejected verifies an empty URI in the list is
// rejected with -32602 rather than silently registered.
func TestSubscriptionsListen_EmptyURIRejected(t *testing.T) {
	srv := NewServer(ServerInfo{Name: "s", Version: "1"})
	handler := newRequestHandler(srv)

	session := server.NewSession("modern", nil, nil)
	ctx := server.ContextWithSession(context.Background(), session)

	req := &protocol.Request{
		JSONRPC: "2.0", ID: json.RawMessage(`1`), Method: protocol.MethodSubscriptionsListen,
		Params: mustParams(t, map[string]any{"uris": []string{""}}),
	}
	_, err := handler.handleSubscriptionsListen(ctx, req)
	var mcpErr *protocol.Error
	if !errors.As(err, &mcpErr) || mcpErr.Code != protocol.CodeInvalidParams {
		t.Fatalf("expected -32602 for empty uri, got %v", err)
	}
}

// TestSubscriptionsListen_NoSessionRejected verifies that a request without a
// session in context (i.e. a non-modern request) is rejected with a protocol
// error instead of panicking.
func TestSubscriptionsListen_NoSessionRejected(t *testing.T) {
	srv := NewServer(ServerInfo{Name: "s", Version: "1"})
	handler := newRequestHandler(srv)

	// Legacy request (no modern _meta) → no session attached by the dispatcher.
	req := &protocol.Request{
		JSONRPC: "2.0", ID: json.RawMessage(`1`), Method: protocol.MethodSubscriptionsListen,
		Params: json.RawMessage(`{"uris":["email://inbox"]}`),
	}
	_, err := handler.HandleRequest(context.Background(), req)
	var mcpErr *protocol.Error
	if !errors.As(err, &mcpErr) || mcpErr.Code != protocol.CodeInvalidParams {
		t.Fatalf("expected -32602 when no session in context, got %v", err)
	}
}
