package client_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/felixgeelhaar/mcp-go/client"
	"github.com/felixgeelhaar/mcp-go/protocol"
)

func TestNew(t *testing.T) {
	t.Run("creates client with transport", func(t *testing.T) {
		transport := &mockTransport{}
		c := client.New(transport)

		if c == nil {
			t.Fatal("expected client to be created")
		}
	})

	t.Run("creates client with options", func(t *testing.T) {
		transport := &mockTransport{}
		c := client.New(transport,
			client.WithTimeout(5*time.Second),
			client.WithClientInfo("test-client", "1.0.0"),
		)

		if c == nil {
			t.Fatal("expected client to be created")
		}
	})
}

func TestClient_Initialize(t *testing.T) {
	t.Run("performs handshake with server", func(t *testing.T) {
		transport := &mockTransport{
			responses: []protocol.Response{
				{
					JSONRPC: "2.0",
					ID:      json.RawMessage(`1`),
					Result: map[string]any{
						"protocolVersion": "2024-11-05",
						"serverInfo": map[string]any{
							"name":    "test-server",
							"version": "1.0.0",
						},
						"capabilities": map[string]any{
							"tools": map[string]any{},
						},
					},
				},
			},
		}

		c := client.New(transport)
		info, err := c.Initialize(context.Background())

		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if info.Name != "test-server" {
			t.Errorf("server name = %q, want %q", info.Name, "test-server")
		}

		if info.Version != "1.0.0" {
			t.Errorf("server version = %q, want %q", info.Version, "1.0.0")
		}

		if !info.Capabilities.Tools {
			t.Error("expected tools capability")
		}
	})

	t.Run("returns error on failed handshake", func(t *testing.T) {
		transport := &mockTransport{
			responses: []protocol.Response{
				{
					JSONRPC: "2.0",
					ID:      json.RawMessage(`1`),
					Error: &protocol.Error{
						Code:    -32600,
						Message: "invalid request",
					},
				},
			},
		}

		c := client.New(transport)
		_, err := c.Initialize(context.Background())

		if err == nil {
			t.Fatal("expected error")
		}
	})

	t.Run("parses extended metadata fields", func(t *testing.T) {
		transport := &mockTransport{
			responses: []protocol.Response{
				{
					JSONRPC: "2.0",
					ID:      json.RawMessage(`1`),
					Result: map[string]any{
						"protocolVersion": "2024-11-05",
						"serverInfo": map[string]any{
							"name":        "metadata-server",
							"version":     "2.0.0",
							"title":       "Metadata Test Server",
							"description": "A server with full metadata",
							"websiteUrl":  "https://example.com/docs",
							"icons": []any{
								map[string]any{
									"uri":      "https://example.com/icon.png",
									"mimeType": "image/png",
									"size":     float64(64),
								},
							},
							"buildInfo": map[string]any{
								"commit":    "abc123def",
								"buildDate": "2025-01-03",
							},
						},
						"capabilities": map[string]any{},
					},
				},
			},
		}

		c := client.New(transport)
		info, err := c.Initialize(context.Background())

		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if info.Title != "Metadata Test Server" {
			t.Errorf("Title = %q, want %q", info.Title, "Metadata Test Server")
		}
		if info.Description != "A server with full metadata" {
			t.Errorf("Description = %q, want %q", info.Description, "A server with full metadata")
		}
		if info.WebsiteURL != "https://example.com/docs" {
			t.Errorf("WebsiteURL = %q, want %q", info.WebsiteURL, "https://example.com/docs")
		}
		if len(info.Icons) != 1 {
			t.Fatalf("Icons length = %d, want 1", len(info.Icons))
		}
		if info.Icons[0].URI != "https://example.com/icon.png" {
			t.Errorf("Icons[0].URI = %q, want %q", info.Icons[0].URI, "https://example.com/icon.png")
		}
		if info.Icons[0].MimeType != "image/png" {
			t.Errorf("Icons[0].MimeType = %q, want %q", info.Icons[0].MimeType, "image/png")
		}
		if info.Icons[0].Size != 64 {
			t.Errorf("Icons[0].Size = %d, want 64", info.Icons[0].Size)
		}
		if info.BuildInfo == nil {
			t.Fatal("BuildInfo is nil, want non-nil")
		}
		if info.BuildInfo.Commit != "abc123def" {
			t.Errorf("BuildInfo.Commit = %q, want %q", info.BuildInfo.Commit, "abc123def")
		}
		if info.BuildInfo.BuildDate != "2025-01-03" {
			t.Errorf("BuildInfo.BuildDate = %q, want %q", info.BuildInfo.BuildDate, "2025-01-03")
		}
	})
}

func TestClient_ListTools(t *testing.T) {
	t.Run("returns list of tools", func(t *testing.T) {
		transport := &mockTransport{
			responses: []protocol.Response{
				{
					JSONRPC: "2.0",
					ID:      json.RawMessage(`1`),
					Result: map[string]any{
						"tools": []any{
							map[string]any{
								"name":        "search",
								"description": "Search for items",
								"inputSchema": map[string]any{
									"type": "object",
									"properties": map[string]any{
										"query": map[string]any{"type": "string"},
									},
								},
							},
						},
					},
				},
			},
		}

		c := client.New(transport)
		tools, err := c.ListTools(context.Background())

		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if len(tools) != 1 {
			t.Fatalf("expected 1 tool, got %d", len(tools))
		}

		if tools[0].Name != "search" {
			t.Errorf("tool name = %q, want %q", tools[0].Name, "search")
		}
	})
}

func TestClient_CallTool(t *testing.T) {
	t.Run("executes tool and returns result", func(t *testing.T) {
		transport := &mockTransport{
			responses: []protocol.Response{
				{
					JSONRPC: "2.0",
					ID:      json.RawMessage(`1`),
					Result: map[string]any{
						"content": []any{
							map[string]any{
								"type": "text",
								"text": "Hello, World!",
							},
						},
					},
				},
			},
		}

		c := client.New(transport)
		result, err := c.CallTool(context.Background(), "greet", map[string]any{"name": "World"})

		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if len(result.Content) != 1 {
			t.Fatalf("expected 1 content item, got %d", len(result.Content))
		}

		if result.Content[0].Text != "Hello, World!" {
			t.Errorf("text = %q, want %q", result.Content[0].Text, "Hello, World!")
		}
	})

	t.Run("returns error for unknown tool", func(t *testing.T) {
		transport := &mockTransport{
			responses: []protocol.Response{
				{
					JSONRPC: "2.0",
					ID:      json.RawMessage(`1`),
					Error: &protocol.Error{
						Code:    -32001,
						Message: "tool not found",
					},
				},
			},
		}

		c := client.New(transport)
		_, err := c.CallTool(context.Background(), "unknown", nil)

		if err == nil {
			t.Fatal("expected error")
		}
	})
}

func TestClient_ListResources(t *testing.T) {
	t.Run("returns list of resources", func(t *testing.T) {
		transport := &mockTransport{
			responses: []protocol.Response{
				{
					JSONRPC: "2.0",
					ID:      json.RawMessage(`1`),
					Result: map[string]any{
						"resources": []any{
							map[string]any{
								"uri":         "file://{path}",
								"name":        "File",
								"description": "Read file content",
								"mimeType":    "text/plain",
							},
						},
					},
				},
			},
		}

		c := client.New(transport)
		resources, err := c.ListResources(context.Background())

		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if len(resources) != 1 {
			t.Fatalf("expected 1 resource, got %d", len(resources))
		}

		if resources[0].Name != "File" {
			t.Errorf("resource name = %q, want %q", resources[0].Name, "File")
		}
	})
}

func TestClient_ReadResource(t *testing.T) {
	t.Run("reads resource content", func(t *testing.T) {
		transport := &mockTransport{
			responses: []protocol.Response{
				{
					JSONRPC: "2.0",
					ID:      json.RawMessage(`1`),
					Result: map[string]any{
						"contents": []any{
							map[string]any{
								"uri":      "file://test.txt",
								"mimeType": "text/plain",
								"text":     "Hello, World!",
							},
						},
					},
				},
			},
		}

		c := client.New(transport)
		content, err := c.ReadResource(context.Background(), "file://test.txt")

		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if content.Text != "Hello, World!" {
			t.Errorf("text = %q, want %q", content.Text, "Hello, World!")
		}
	})
}

func TestClient_ListPrompts(t *testing.T) {
	t.Run("returns list of prompts", func(t *testing.T) {
		transport := &mockTransport{
			responses: []protocol.Response{
				{
					JSONRPC: "2.0",
					ID:      json.RawMessage(`1`),
					Result: map[string]any{
						"prompts": []any{
							map[string]any{
								"name":        "greet",
								"description": "Generate a greeting",
								"arguments": []any{
									map[string]any{
										"name":     "name",
										"required": true,
									},
								},
							},
						},
					},
				},
			},
		}

		c := client.New(transport)
		prompts, err := c.ListPrompts(context.Background())

		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if len(prompts) != 1 {
			t.Fatalf("expected 1 prompt, got %d", len(prompts))
		}

		if prompts[0].Name != "greet" {
			t.Errorf("prompt name = %q, want %q", prompts[0].Name, "greet")
		}
	})
}

func TestClient_GetPrompt(t *testing.T) {
	t.Run("gets prompt messages", func(t *testing.T) {
		transport := &mockTransport{
			responses: []protocol.Response{
				{
					JSONRPC: "2.0",
					ID:      json.RawMessage(`1`),
					Result: map[string]any{
						"messages": []any{
							map[string]any{
								"role": "user",
								"content": map[string]any{
									"type": "text",
									"text": "Hello, World!",
								},
							},
						},
					},
				},
			},
		}

		c := client.New(transport)
		result, err := c.GetPrompt(context.Background(), "greet", map[string]string{"name": "World"})

		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if len(result.Messages) != 1 {
			t.Fatalf("expected 1 message, got %d", len(result.Messages))
		}
	})
}

func TestClient_Ping(t *testing.T) {
	t.Run("pings server successfully", func(t *testing.T) {
		transport := &mockTransport{
			responses: []protocol.Response{
				{
					JSONRPC: "2.0",
					ID:      json.RawMessage(`1`),
					Result:  map[string]any{},
				},
			},
		}

		c := client.New(transport)
		err := c.Ping(context.Background())

		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})
}

// mockTransport implements client.Transport for testing.
type mockTransport struct {
	responses []protocol.Response
	requests  []protocol.Request
	idx       int
}

func (m *mockTransport) Send(ctx context.Context, req *protocol.Request) (*protocol.Response, error) {
	m.requests = append(m.requests, *req)
	if m.idx >= len(m.responses) {
		return nil, context.DeadlineExceeded
	}
	resp := m.responses[m.idx]
	m.idx++
	return &resp, nil
}

func (m *mockTransport) Close() error {
	return nil
}
