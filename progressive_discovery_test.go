package mcp

import (
	"context"
	"encoding/json"
	"testing"

	"go.klarlabs.de/mcp/protocol"
)

func TestToolsList_ProgressiveDiscovery(t *testing.T) {
	srv := NewServer(ServerInfo{Name: "prog", Version: "1.0.0", Capabilities: Capabilities{Tools: true}})
	type In struct {
		Q string `json:"q" jsonschema:"required"`
	}
	srv.Tool("billing.create").
		Description("Create invoice").
		Group("billing").
		Tags("write", "finance").
		DeferSchema().
		Handler(func(_ In) (string, error) { return "ok", nil })
	srv.Tool("billing.list").
		Description("List invoices").
		Group("billing").
		Tags("read", "finance").
		Handler(func(_ In) (string, error) { return "ok", nil })
	srv.Tool("search").
		Description("Search catalog").
		Tags("read").
		Handler(func(_ In) (string, error) { return "ok", nil })

	h := newRequestHandler(srv)

	t.Run("filter by group", func(t *testing.T) {
		tools := listTools(t, h, map[string]any{"group": "billing"})
		if len(tools) != 2 {
			t.Fatalf("got %d tools: %#v", len(tools), tools)
		}
	})

	t.Run("filter by tags AND", func(t *testing.T) {
		tools := listTools(t, h, map[string]any{"tags": []string{"read", "finance"}})
		if len(tools) != 1 || tools[0]["name"] != "billing.list" {
			t.Fatalf("got %#v", tools)
		}
	})

	t.Run("detail names omits schemas", func(t *testing.T) {
		tools := listTools(t, h, map[string]any{"detail": "names"})
		for _, tool := range tools {
			if _, ok := tool["inputSchema"]; ok {
				t.Fatalf("names detail should omit inputSchema: %#v", tool)
			}
		}
	})

	t.Run("DeferSchema omits schema by default", func(t *testing.T) {
		tools := listTools(t, h, map[string]any{"group": "billing"})
		var create, list map[string]any
		for _, tool := range tools {
			switch tool["name"] {
			case "billing.create":
				create = tool
			case "billing.list":
				list = tool
			}
		}
		if create == nil || list == nil {
			t.Fatalf("missing tools: %#v", tools)
		}
		if _, ok := create["inputSchema"]; ok {
			t.Fatal("DeferSchema tool should omit inputSchema by default")
		}
		if _, ok := list["inputSchema"]; !ok {
			t.Fatal("non-deferred tool should include inputSchema")
		}
	})

	t.Run("detail full restores deferred schemas", func(t *testing.T) {
		tools := listTools(t, h, map[string]any{"detail": "full", "group": "billing"})
		for _, tool := range tools {
			if tool["name"] == "billing.create" {
				if _, ok := tool["inputSchema"]; !ok {
					t.Fatal("detail=full should include deferred schema")
				}
			}
		}
	})
}

func listTools(t *testing.T, h *requestHandler, params map[string]any) []map[string]any {
	t.Helper()
	raw, err := json.Marshal(params)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := h.HandleRequest(context.Background(), &protocol.Request{
		JSONRPC: "2.0",
		ID:      json.RawMessage(`1`),
		Method:  protocol.MethodToolsList,
		Params:  raw,
	})
	if err != nil {
		t.Fatalf("tools/list: %v", err)
	}
	result, ok := resp.Result.(map[string]any)
	if !ok {
		t.Fatalf("result type %T", resp.Result)
	}
	items, ok := result["tools"].([]map[string]any)
	if !ok {
		t.Fatalf("tools type %T", result["tools"])
	}
	return items
}
