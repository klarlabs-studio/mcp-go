package mcp

import (
	"context"
	"encoding/json"
	"sync"
	"testing"

	"go.klarlabs.de/mcp/protocol"
	"go.klarlabs.de/mcp/transport"
)

type recordingNotifier struct {
	mu    sync.Mutex
	calls []struct{ clientID, method string }
}

func (r *recordingNotifier) NotifyClient(clientID, method string, _ any) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls = append(r.calls, struct{ clientID, method string }{clientID, method})
	return nil
}

func subscribeEnabledServer() *Server {
	return NewServer(ServerInfo{
		Name:         "test",
		Version:      "1.0.0",
		Capabilities: Capabilities{Resources: true, ResourceSubscribe: true},
	})
}

func subscribeReq(t *testing.T, method, uri string) *protocol.Request {
	t.Helper()
	return &protocol.Request{
		JSONRPC: protocol.JSONRPCVersion,
		ID:      json.RawMessage(`1`),
		Method:  method,
		Params:  json.RawMessage(`{"uri":"` + uri + `"}`),
	}
}

func TestSubscribeRoutesAndNotifies(t *testing.T) {
	srv := subscribeEnabledServer()
	notifier := &recordingNotifier{}
	srv.SetResourceNotifier(notifier)
	h := newRequestHandler(srv)

	ctx := transport.ContextWithClientID(context.Background(), "client-1")
	resp, err := h.HandleRequest(ctx, subscribeReq(t, protocol.MethodResourcesSubscribe, "email://inbox"))
	if err != nil || resp.Error != nil {
		t.Fatalf("subscribe failed: err=%v respErr=%v", err, resp.Error)
	}

	if err := srv.NotifyResourceUpdated("email://inbox"); err != nil {
		t.Fatalf("notify: %v", err)
	}
	if len(notifier.calls) != 1 || notifier.calls[0].clientID != "client-1" {
		t.Fatalf("notifications = %+v, want one to client-1", notifier.calls)
	}
	if notifier.calls[0].method != protocol.MethodResourceUpdated {
		t.Errorf("method = %q, want %q", notifier.calls[0].method, protocol.MethodResourceUpdated)
	}
}

func TestUnsubscribeStopsNotifications(t *testing.T) {
	srv := subscribeEnabledServer()
	notifier := &recordingNotifier{}
	srv.SetResourceNotifier(notifier)
	h := newRequestHandler(srv)
	ctx := transport.ContextWithClientID(context.Background(), "client-1")

	if _, err := h.HandleRequest(ctx, subscribeReq(t, protocol.MethodResourcesSubscribe, "email://inbox")); err != nil {
		t.Fatalf("subscribe: %v", err)
	}
	if _, err := h.HandleRequest(ctx, subscribeReq(t, protocol.MethodResourcesUnsubscribe, "email://inbox")); err != nil {
		t.Fatalf("unsubscribe: %v", err)
	}
	if err := srv.NotifyResourceUpdated("email://inbox"); err != nil {
		t.Fatalf("notify: %v", err)
	}
	if len(notifier.calls) != 0 {
		t.Errorf("got %d notifications after unsubscribe, want 0", len(notifier.calls))
	}
}

func TestSubscribeWithoutClientIDIsRejected(t *testing.T) {
	srv := subscribeEnabledServer()
	h := newRequestHandler(srv)
	// No client id in context (no SSE stream).
	resp, err := h.HandleRequest(context.Background(), subscribeReq(t, protocol.MethodResourcesSubscribe, "email://inbox"))
	if err == nil && (resp == nil || resp.Error == nil) {
		t.Fatal("expected subscribe without a client stream to be rejected")
	}
}

func TestSubscribeDisabledReturnsMethodNotFound(t *testing.T) {
	// Resources present but subscribe capability NOT enabled.
	srv := NewServer(ServerInfo{Name: "t", Version: "1", Capabilities: Capabilities{Resources: true}})
	h := newRequestHandler(srv)
	ctx := transport.ContextWithClientID(context.Background(), "client-1")
	resp, err := h.HandleRequest(ctx, subscribeReq(t, protocol.MethodResourcesSubscribe, "email://inbox"))
	if err == nil && (resp == nil || resp.Error == nil) {
		t.Fatal("expected method-not-found when subscriptions are disabled")
	}
}

func TestInitializeAdvertisesSubscribeCapability(t *testing.T) {
	srv := subscribeEnabledServer()
	h := newRequestHandler(srv)
	resp, err := h.HandleRequest(context.Background(), &protocol.Request{
		JSONRPC: protocol.JSONRPCVersion,
		ID:      json.RawMessage(`1`),
		Method:  protocol.MethodInitialize,
		Params:  json.RawMessage(`{}`),
	})
	if err != nil {
		t.Fatalf("initialize: %v", err)
	}
	result, _ := resp.Result.(map[string]any)
	caps, _ := result["capabilities"].(map[string]any)
	resources, _ := caps["resources"].(map[string]any)
	if sub, _ := resources["subscribe"].(bool); !sub {
		t.Errorf("resources capability did not advertise subscribe: %+v", resources)
	}
}
