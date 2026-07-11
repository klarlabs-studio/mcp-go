package transport

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"go.klarlabs.de/mcp/protocol"
)

// TestStreamableHTTP_DefaultsToStateless confirms that WithStreamable() now
// enables the stateless (MCP 2026-07-28) model by default: the Mcp-Method
// routing header is hard-required (absent → -32020) and initialize does not mint
// an Mcp-Session-Id. WithStreamableStateful() opts back into the session model.
func TestStreamableHTTP_DefaultsToStateless(t *testing.T) {
	h := NewHTTP("127.0.0.1:0", WithStreamable())
	ts := httptest.NewServer(h.createHandler(streamableTestHandler()))
	t.Cleanup(ts.Close)

	// Missing Mcp-Method is hard-rejected in the stateless default.
	resp := postMCP(t, ts, "application/json", "", protocol.Request{
		JSONRPC: protocol.JSONRPCVersion,
		ID:      json.RawMessage(`1`),
		Method:  "ping",
	})
	defer func() { _ = resp.Body.Close() }()
	errObj := decodeJSONRPCError(t, resp)
	if errObj == nil {
		t.Fatal("WithStreamable() should default to stateless (require Mcp-Method), got no error")
	}
	if errObj.Code != protocol.CodeHeaderMismatch {
		t.Errorf("error code = %d, want %d (HeaderMismatch)", errObj.Code, protocol.CodeHeaderMismatch)
	}

	// initialize does not mint a session id in the stateless default.
	resp2 := postMCPWithHeaders(t, ts, "application/json", "", protocol.Request{
		JSONRPC: protocol.JSONRPCVersion,
		ID:      json.RawMessage(`2`),
		Method:  protocol.MethodInitialize,
	}, map[string]string{"Mcp-Method": protocol.MethodInitialize})
	defer func() { _ = resp2.Body.Close() }()
	if sid := resp2.Header.Get("Mcp-Session-Id"); sid != "" {
		t.Errorf("stateless default must not mint an Mcp-Session-Id, got %q", sid)
	}
}

// TestStreamableStateful_KeepsSessionLifecycle confirms WithStreamableStateful()
// restores the legacy session-negotiated (MCP 2025-03-26) streamable model:
// initialize mints an Mcp-Session-Id and a non-initialize POST without it is
// rejected.
func TestStreamableStateful_KeepsSessionLifecycle(t *testing.T) {
	h := NewHTTP("127.0.0.1:0", WithStreamableStateful())
	ts := httptest.NewServer(h.createHandler(streamableTestHandler()))
	t.Cleanup(ts.Close)

	sid := initSession(t, ts) // fails if no Mcp-Session-Id is minted

	resp := postMCP(t, ts, "application/json", "", protocol.Request{
		JSONRPC: protocol.JSONRPCVersion,
		ID:      json.RawMessage(`2`),
		Method:  "ping",
	})
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode == http.StatusOK {
		t.Fatal("stateful streamable mode should require an Mcp-Session-Id on non-initialize POSTs")
	}
	_ = sid
}
