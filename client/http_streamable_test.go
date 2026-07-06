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

// A server must not be able to answer an awaited request with an SSE frame
// carrying a different id: that would let it deliver B's result as A's response.
func TestResponseFromSSE_MismatchedIDRejected(t *testing.T) {
	body := "data: {\"jsonrpc\":\"2.0\",\"id\":999,\"result\":{\"ok\":true}}\n\n"
	if _, err := responseFromSSE([]byte(body), json.RawMessage("1")); err == nil {
		t.Fatal("expected error for id mismatch, got nil (mismatched frame accepted)")
	}
}

// The correct frame must be selected even when other-id frames precede it.
func TestResponseFromSSE_MatchingIDSelected(t *testing.T) {
	body := "data: {\"jsonrpc\":\"2.0\",\"id\":2,\"result\":{\"n\":2}}\n\n" +
		"data: {\"jsonrpc\":\"2.0\",\"id\":1,\"result\":{\"n\":1}}\n\n"
	resp, err := responseFromSSE([]byte(body), json.RawMessage("1"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if strings.TrimSpace(string(resp.ID)) != "1" {
		t.Fatalf("selected id %s, want 1", resp.ID)
	}
}

// A mismatched-id SSE frame must surface as a Send error, not a wrong response.
func TestHTTPTransport_SSEMismatchedIDSendError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		// Client will await id 1; server answers with id 42.
		fmt.Fprint(w, "data: {\"jsonrpc\":\"2.0\",\"id\":42,\"result\":{\"ok\":true}}\n\n")
	}))
	defer srv.Close()

	tr, _ := NewHTTPTransport(srv.URL, WithEndpointPath(""))
	_, err := tr.Send(context.Background(), &protocol.Request{JSONRPC: jsonrpcVersion, ID: json.RawMessage("1"), Method: "x"})
	if err == nil {
		t.Fatal("expected error for mismatched SSE id, got nil")
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
