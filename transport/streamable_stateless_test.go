package transport

import (
	"encoding/json"
	"net/http"
	"testing"

	"go.klarlabs.de/mcp/protocol"
)

// TestStreamableStateless_NoSessionIDRequired confirms that in stateless mode a
// POST for a non-initialize method succeeds without an Mcp-Session-Id (the
// session lifecycle is dropped), as long as the required Mcp-Method header is
// present — in the default streamable mode this same request would be rejected
// with 404 (unknown/expired session).
func TestStreamableStateless_NoSessionIDRequired(t *testing.T) {
	_, ts := newStreamableServer(t, WithStreamableStateless())

	resp := postMCPWithHeaders(t, ts, "application/json", "", protocol.Request{
		JSONRPC: protocol.JSONRPCVersion,
		ID:      json.RawMessage(`1`),
		Method:  "ping",
	}, map[string]string{"Mcp-Method": "ping"})
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200 (no session required in stateless mode)", resp.StatusCode)
	}
	if errObj := decodeJSONRPCError(t, resp); errObj != nil {
		t.Fatalf("unexpected JSON-RPC error: %+v", errObj)
	}
}

// TestStreamableStateless_RequiresMcpMethodHeader confirms the Mcp-Method
// routing header is hard-required in stateless mode: a POST omitting it is
// rejected with -32020 (vs the default validate-when-present behavior, where an
// absent header is fine).
func TestStreamableStateless_RequiresMcpMethodHeader(t *testing.T) {
	_, ts := newStreamableServer(t, WithStreamableStateless())

	resp := postMCP(t, ts, "application/json", "", protocol.Request{
		JSONRPC: protocol.JSONRPCVersion,
		ID:      json.RawMessage(`1`),
		Method:  "ping",
	})
	defer func() { _ = resp.Body.Close() }()

	errObj := decodeJSONRPCError(t, resp)
	if errObj == nil {
		t.Fatal("expected a JSON-RPC error for missing Mcp-Method, got none")
	}
	if errObj.Code != protocol.CodeHeaderMismatch {
		t.Errorf("error code = %d, want %d (HeaderMismatch)", errObj.Code, protocol.CodeHeaderMismatch)
	}
}

// TestStreamableStateless_DoesNotMintSessionID confirms initialize does not mint
// or echo an Mcp-Session-Id in stateless mode.
func TestStreamableStateless_DoesNotMintSessionID(t *testing.T) {
	_, ts := newStreamableServer(t, WithStreamableStateless())

	resp := postMCPWithHeaders(t, ts, "application/json", "", protocol.Request{
		JSONRPC: protocol.JSONRPCVersion,
		ID:      json.RawMessage(`1`),
		Method:  protocol.MethodInitialize,
	}, map[string]string{"Mcp-Method": protocol.MethodInitialize})
	defer func() { _ = resp.Body.Close() }()

	if sid := resp.Header.Get("Mcp-Session-Id"); sid != "" {
		t.Errorf("stateless mode must not mint an Mcp-Session-Id, got %q", sid)
	}
}

// TestStreamableStateless_OffKeepsSessionLifecycle is a guard that the default
// (non-stateless) streamable path still mints and requires a session id.
func TestStreamableStateless_OffKeepsSessionLifecycle(t *testing.T) {
	_, ts := newStreamableServer(t)

	// initialize mints a session id.
	sid := initSession(t, ts)

	// A non-initialize POST without the session id is rejected (404).
	resp := postMCP(t, ts, "application/json", "", protocol.Request{
		JSONRPC: protocol.JSONRPCVersion,
		ID:      json.RawMessage(`2`),
		Method:  "ping",
	})
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode == http.StatusOK {
		t.Fatal("default streamable mode should require an Mcp-Session-Id on non-initialize POSTs")
	}
	_ = sid
}
