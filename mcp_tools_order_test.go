package mcp

import (
	"context"
	"encoding/json"
	"slices"
	"testing"

	"go.klarlabs.de/mcp/protocol"
)

// TestHandleToolsList_DeterministicOrder verifies that tools/list returns tools
// sorted by name ascending, and that the order is identical across repeated
// calls. Server.Tools() is backed by a Go map with randomized iteration order,
// so the handler must sort to guarantee determinism (MCP 2026-07-28).
func TestHandleToolsList_DeterministicOrder(t *testing.T) {
	srv := NewServer(ServerInfo{Name: "test", Version: "1.0.0"})

	type Input struct {
		Q string `json:"q"`
	}

	// Register in deliberately out-of-order names.
	for _, name := range []string{"zebra", "apple", "mango", "banana", "cherry"} {
		srv.Tool(name).
			Description("tool " + name).
			Handler(func(input Input) (string, error) { return input.Q, nil })
	}

	handler := newRequestHandler(srv)

	listNames := func() []string {
		req := &protocol.Request{
			JSONRPC: "2.0",
			ID:      json.RawMessage(`1`),
			Method:  "tools/list",
		}
		resp, err := handler.HandleRequest(context.Background(), req)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		result, ok := resp.Result.(map[string]any)
		if !ok {
			t.Fatalf("unexpected result type: %T", resp.Result)
		}
		toolList, ok := result["tools"].([]map[string]any)
		if !ok {
			t.Fatalf("unexpected tools type: %T", result["tools"])
		}
		names := make([]string, 0, len(toolList))
		for _, item := range toolList {
			names = append(names, item[fieldName].(string))
		}
		return names
	}

	want := []string{"apple", "banana", "cherry", "mango", "zebra"}

	first := listNames()
	if !slices.Equal(first, want) {
		t.Fatalf("tools/list not sorted ascending: got %v, want %v", first, want)
	}

	// Call several more times; order must be stable and identical each time.
	for i := 0; i < 5; i++ {
		got := listNames()
		if !slices.Equal(got, first) {
			t.Fatalf("tools/list order not deterministic on call %d: got %v, want %v", i, got, first)
		}
	}
}
