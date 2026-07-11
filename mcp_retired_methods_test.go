package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"go.klarlabs.de/mcp/protocol"
)

// TestRetiredMethods_GatedOffForModern confirms the lifecycle/legacy methods the
// stateless 2026-07-28 redesign removes (initialize, ping, logging/setLevel, the
// resources subscribe/unsubscribe pair) return MethodNotFound for a modern
// caller, while legacy callers keep the methods (back-compat probe). server/
// discover + per-request _meta + subscriptions/listen replace them.
func TestRetiredMethods_GatedOffForModern(t *testing.T) {
	srv := NewServer(ServerInfo{Name: "s", Version: "1"})
	handler := newRequestHandler(srv)

	retired := []string{
		protocol.MethodInitialize,
		protocol.MethodPing,
		protocol.MethodLoggingSetLevel,
		protocol.MethodResourcesSubscribe,
		protocol.MethodResourcesUnsubscribe,
	}

	for _, method := range retired {
		t.Run("modern/"+method, func(t *testing.T) {
			req := &protocol.Request{
				JSONRPC: "2.0", ID: json.RawMessage(`1`), Method: method,
				Params: modernParams(t, protocol.DraftVersion, nil),
			}
			_, err := handler.HandleRequest(context.Background(), req)
			var mcpErr *protocol.Error
			if !errors.As(err, &mcpErr) || mcpErr.Code != protocol.CodeMethodNotFound {
				t.Fatalf("modern %s: got %v, want MethodNotFound", method, err)
			}
		})
	}
}

// TestRetiredMethods_ServedForLegacy confirms a legacy (no modern _meta) caller
// still reaches the retired methods — the back-compat path is untouched.
func TestRetiredMethods_ServedForLegacy(t *testing.T) {
	srv := NewServer(ServerInfo{Name: "s", Version: "1"})
	handler := newRequestHandler(srv)

	// ping is the cheapest retired method with a handler that succeeds with no
	// params; a legacy request must be served (not MethodNotFound).
	legacy := &protocol.Request{JSONRPC: "2.0", ID: json.RawMessage(`2`), Method: protocol.MethodPing}
	if _, err := handler.HandleRequest(context.Background(), legacy); err != nil {
		t.Fatalf("legacy ping should be served: %v", err)
	}
}
