package mcp_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	mcp "go.klarlabs.de/mcp"
	"go.klarlabs.de/mcp/protocol"
)

// jsonRPCResult writes a JSON-RPC response wrapping a tools/call result whose
// first text content block is the given JSON payload.
func toolCallServer(t *testing.T, payload string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req protocol.Request
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("decode request: %v", err)
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		result := map[string]any{
			"content": []map[string]any{
				{"type": "text", "text": payload},
			},
		}
		_ = json.NewEncoder(w).Encode(protocol.NewResponse(req.ID, result))
	}))
}

type greetIn struct {
	Name string `json:"name"`
}

type greetOut struct {
	Message string `json:"message"`
}

func TestNewClient_TypedCall(t *testing.T) {
	srv := toolCallServer(t, `{"message":"hello World"}`)
	defer srv.Close()

	c, err := mcp.NewClient(srv.URL)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	defer func() { _ = c.Close() }()

	out, err := mcp.Call[greetIn, greetOut](context.Background(), c, "greet", greetIn{Name: "World"})
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	if out.Message != "hello World" {
		t.Errorf("message = %q, want %q", out.Message, "hello World")
	}
}

func TestNewClient_WithHTTPClient(t *testing.T) {
	srv := toolCallServer(t, `{"message":"ok"}`)
	defer srv.Close()

	// A caller-supplied http.Client is the only auth/transport hook.
	hc := &http.Client{}
	c, err := mcp.NewClient(srv.URL, mcp.WithHTTPClient(hc))
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	defer func() { _ = c.Close() }()

	out, err := mcp.Call[greetIn, greetOut](context.Background(), c, "greet", greetIn{Name: "x"})
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	if out.Message != "ok" {
		t.Errorf("message = %q, want %q", out.Message, "ok")
	}
}

func TestNewClientTool_TopLevel(t *testing.T) {
	srv := toolCallServer(t, `{"message":"bound"}`)
	defer srv.Close()

	c, err := mcp.NewClient(srv.URL)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	defer func() { _ = c.Close() }()

	tool := mcp.NewClientTool[greetIn, greetOut](c, "greet")
	out, err := tool.Call(context.Background(), greetIn{Name: "y"})
	if err != nil {
		t.Fatalf("tool.Call: %v", err)
	}
	if out.Message != "bound" {
		t.Errorf("message = %q, want %q", out.Message, "bound")
	}
}

func TestClient_CallRaw_TopLevel(t *testing.T) {
	srv := toolCallServer(t, `{"message":"raw"}`)
	defer srv.Close()

	c, err := mcp.NewClient(srv.URL)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	defer func() { _ = c.Close() }()

	raw, err := c.CallRaw(context.Background(), "greet", json.RawMessage(`{"name":"z"}`))
	if err != nil {
		t.Fatalf("CallRaw: %v", err)
	}
	var out greetOut
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if out.Message != "raw" {
		t.Errorf("message = %q, want %q", out.Message, "raw")
	}
}

// toolInterface documents that mcp.Tool is the dynamic escape-hatch interface.
func toolInterface(c *mcp.Client) mcp.Tool {
	return mcp.NewDynamicTool(c, "greet")
}

func TestMCPTool_InterfaceName(t *testing.T) {
	srv := toolCallServer(t, `{"message":"ok"}`)
	defer srv.Close()

	c, err := mcp.NewClient(srv.URL)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	defer func() { _ = c.Close() }()

	tool := toolInterface(c)
	if tool.Name() != "greet" {
		t.Errorf("name = %q, want %q", tool.Name(), "greet")
	}
}

func TestNewStdioClient_Validation(t *testing.T) {
	// A non-existent command must surface an error rather than panic.
	if _, err := mcp.NewStdioClient("definitely-not-a-real-binary-xyz"); err == nil {
		t.Fatal("expected error for non-existent command")
	}
}
