package client

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"go.klarlabs.de/mcp/protocol"
)

// A streamable-HTTP server answers POSTs with text/event-stream and assigns a
// session id the client must echo on later requests.
func TestHTTPTransportStreamableSSE(t *testing.T) {
	var mu sync.Mutex
	var calls int
	var secondSession string
	var sawAccept string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		calls++
		n := calls
		mu.Unlock()
		if n == 1 {
			sawAccept = r.Header.Get("Accept")
		}
		if n == 2 {
			secondSession = r.Header.Get("Mcp-Session-Id")
		}
		var req protocol.Request
		_ = json.NewDecoder(r.Body).Decode(&req)

		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Mcp-Session-Id", "sess-123")
		w.WriteHeader(http.StatusOK)
		fmt.Fprintf(w, "event: message\ndata: {\"jsonrpc\":\"2.0\",\"id\":%s,\"result\":{\"ok\":true}}\n\n", string(req.ID))
	}))
	defer srv.Close()

	tr, err := NewHTTPTransport(srv.URL, WithEndpointPath(""))
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()

	resp, err := tr.Send(ctx, &protocol.Request{JSONRPC: jsonrpcVersion, ID: json.RawMessage("1"), Method: "initialize"})
	if err != nil {
		t.Fatalf("first send: %v", err)
	}
	if resp.Result == nil || strings.TrimSpace(string(resp.ID)) != "1" {
		t.Fatalf("unexpected response: %+v", resp)
	}
	if !strings.Contains(sawAccept, "application/json") || !strings.Contains(sawAccept, "text/event-stream") {
		t.Fatalf("Accept header must advertise both types, got %q", sawAccept)
	}

	if _, err := tr.Send(ctx, &protocol.Request{JSONRPC: jsonrpcVersion, ID: json.RawMessage("2"), Method: "tools/list"}); err != nil {
		t.Fatalf("second send: %v", err)
	}
	if secondSession != "sess-123" {
		t.Fatalf("session id not echoed on second request, got %q", secondSession)
	}
}

// A plain-JSON server still works (back-compat).
func TestHTTPTransportPlainJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req protocol.Request
		_ = json.NewDecoder(r.Body).Decode(&req)
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"jsonrpc":"2.0","id":%s,"result":{"ok":true}}`, string(req.ID))
	}))
	defer srv.Close()

	tr, _ := NewHTTPTransport(srv.URL, WithEndpointPath(""))
	resp, err := tr.Send(context.Background(), &protocol.Request{JSONRPC: jsonrpcVersion, ID: json.RawMessage("7"), Method: "x"})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Result == nil || strings.TrimSpace(string(resp.ID)) != "7" {
		t.Fatalf("unexpected response: %+v", resp)
	}
}
