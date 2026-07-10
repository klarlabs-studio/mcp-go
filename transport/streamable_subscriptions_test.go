package transport

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"go.klarlabs.de/mcp/protocol"
)

// TestStreamableHTTP_SubscriptionsListenStream drives the full subscriptions/listen
// long-lived POST stream: the first frame acknowledges the subscription (carrying
// the subscriptionId), and a notification pushed via NotifySubscription arrives on
// the same stream tagged with io.modelcontextprotocol/subscriptionId.
func TestStreamableHTTP_SubscriptionsListenStream(t *testing.T) {
	h, ts := newStreamableServer(t, WithStreamableStateless())

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	body, err := json.Marshal(protocol.Request{
		JSONRPC: protocol.JSONRPCVersion,
		ID:      json.RawMessage(`1`),
		Method:  protocol.MethodSubscriptionsListen,
	})
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, ts.URL+"/mcp", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("Mcp-Method", protocol.MethodSubscriptionsListen)

	resp, err := ts.Client().Do(req)
	if err != nil {
		t.Fatalf("do request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/event-stream") {
		t.Fatalf("Content-Type = %q, want text/event-stream", ct)
	}

	// First frame: the subscription acknowledgement carrying the subscriptionId.
	ack, err := readOneSSEData(resp.Body)
	if err != nil {
		t.Fatalf("read ack frame: %v", err)
	}
	if !strings.Contains(ack, "sub-test") {
		t.Fatalf("ack frame = %q, want it to carry subscriptionId sub-test", ack)
	}

	// Push a resource-updated notification onto the open stream. The stream is
	// registered synchronously before the ack was flushed, so it is present now.
	if err := h.NotifySubscription("sub-test", protocol.MethodResourceUpdated,
		map[string]any{"uri": "email://inbox"}); err != nil {
		t.Fatalf("NotifySubscription: %v", err)
	}

	// Second frame: the tagged notification.
	frame, err := readOneSSEData(resp.Body)
	if err != nil {
		t.Fatalf("read notification frame: %v", err)
	}
	if !strings.Contains(frame, "email://inbox") {
		t.Errorf("notification frame = %q, want it to carry the resource uri", frame)
	}
	if !strings.Contains(frame, protocol.MetaKeySubscriptionID) || !strings.Contains(frame, "sub-test") {
		t.Errorf("notification frame = %q, want it tagged with subscriptionId sub-test", frame)
	}
}

// TestStreamableHTTP_SubscriptionsListenNoSubID confirms that when the handler
// returns no subscriptionId (e.g. an error result), the endpoint replies with a
// single JSON object rather than opening a stream.
func TestStreamableHTTP_SubscriptionsListenNoSubID(t *testing.T) {
	h := NewHTTP("127.0.0.1:0", WithStreamableStateless())
	handler := HandlerFunc(func(_ context.Context, req *protocol.Request) (*protocol.Response, error) {
		// No subscriptionId in the result.
		return protocol.NewResponse(req.ID, map[string]any{"resultType": "complete"}), nil
	})
	ts := httptest.NewServer(h.createHandler(handler))
	t.Cleanup(ts.Close)

	resp := postMCPWithHeaders(t, ts, "text/event-stream", "", protocol.Request{
		JSONRPC: protocol.JSONRPCVersion,
		ID:      json.RawMessage(`1`),
		Method:  protocol.MethodSubscriptionsListen,
	}, map[string]string{"Mcp-Method": protocol.MethodSubscriptionsListen})
	defer func() { _ = resp.Body.Close() }()

	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Fatalf("Content-Type = %q, want application/json (no stream without subscriptionId)", ct)
	}
	_ = h
}
