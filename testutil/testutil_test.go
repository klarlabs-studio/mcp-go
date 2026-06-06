package testutil_test

import (
	"context"
	"errors"
	"testing"

	"go.klarlabs.de/mcp"
	"go.klarlabs.de/mcp/server"
	"go.klarlabs.de/mcp/testutil"
)

func TestTestClient_Tools(t *testing.T) {
	srv := mcp.NewServer(mcp.ServerInfo{
		Name:    "test-server",
		Version: "1.0.0",
	})

	type GreetInput struct {
		Name string `json:"name" jsonschema:"required"`
	}

	srv.Tool("greet").
		Description("Greet someone").
		Handler(func(ctx context.Context, input GreetInput) (string, error) {
			return "Hello, " + input.Name + "!", nil
		})

	srv.Tool("error-tool").
		Description("Always fails").
		Handler(func(ctx context.Context, input struct{}) (string, error) {
			return "", errors.New("intentional error")
		})

	client := testutil.NewTestClient(t, srv)

	t.Run("Initialize", func(t *testing.T) {
		result, err := client.Initialize()
		if err != nil {
			t.Fatalf("Initialize failed: %v", err)
		}

		serverInfo, ok := result["serverInfo"].(map[string]any)
		if !ok {
			t.Fatal("expected serverInfo in result")
		}

		if serverInfo["name"] != "test-server" {
			t.Errorf("expected name 'test-server', got %v", serverInfo["name"])
		}
	})

	t.Run("ListTools", func(t *testing.T) {
		tools, err := client.ListTools()
		if err != nil {
			t.Fatalf("ListTools failed: %v", err)
		}

		if len(tools) != 2 {
			t.Errorf("expected 2 tools, got %d", len(tools))
		}

		// Check greet tool exists
		found := false
		for _, tool := range tools {
			if tool["name"] == "greet" {
				found = true
				if tool["description"] != "Greet someone" {
					t.Errorf("expected description 'Greet someone', got %v", tool["description"])
				}
				break
			}
		}
		if !found {
			t.Error("greet tool not found")
		}
	})

	t.Run("CallTool success", func(t *testing.T) {
		result, err := client.CallTool("greet", map[string]string{"name": "World"})
		if err != nil {
			t.Fatalf("CallTool failed: %v", err)
		}

		if result != "Hello, World!" {
			t.Errorf("expected 'Hello, World!', got %q", result)
		}
	})

	t.Run("CallTool error", func(t *testing.T) {
		_, err := client.CallTool("error-tool", struct{}{})
		if err == nil {
			t.Fatal("expected error")
		}

		if err.Error() != "intentional error" {
			t.Errorf("unexpected error message: %v", err)
		}
	})

	t.Run("CallTool not found", func(t *testing.T) {
		_, err := client.CallTool("nonexistent", nil)
		if err == nil {
			t.Fatal("expected error for nonexistent tool")
		}
	})

	t.Run("Ping", func(t *testing.T) {
		err := client.Ping()
		if err != nil {
			t.Fatalf("Ping failed: %v", err)
		}
	})
}

func TestTestClient_Resources(t *testing.T) {
	srv := mcp.NewServer(mcp.ServerInfo{
		Name:    "test-server",
		Version: "1.0.0",
	})

	srv.Resource("file:///{path}").
		Name("file").
		Description("Read files").
		Handler(func(ctx context.Context, uri string, params map[string]string) (*server.ResourceContent, error) {
			return &server.ResourceContent{
				URI:      uri,
				MimeType: "text/plain",
				Text:     "content of " + uri,
			}, nil
		})

	client := testutil.NewTestClient(t, srv)

	t.Run("ListResources", func(t *testing.T) {
		resources, err := client.ListResources()
		if err != nil {
			t.Fatalf("ListResources failed: %v", err)
		}

		if len(resources) != 1 {
			t.Errorf("expected 1 resource, got %d", len(resources))
		}
	})

	t.Run("ReadResource", func(t *testing.T) {
		content, err := client.ReadResource("file:///test.txt")
		if err != nil {
			t.Fatalf("ReadResource failed: %v", err)
		}

		expected := "content of file:///test.txt"
		if content != expected {
			t.Errorf("expected %q, got %q", expected, content)
		}
	})

	t.Run("ReadResource not found", func(t *testing.T) {
		_, err := client.ReadResource("unknown://resource")
		if err == nil {
			t.Fatal("expected error for unknown resource")
		}
	})
}

func TestTestClient_Prompts(t *testing.T) {
	srv := mcp.NewServer(mcp.ServerInfo{
		Name:    "test-server",
		Version: "1.0.0",
	})

	srv.Prompt("summarize").
		Description("Summarize content").
		Argument("content", "Content to summarize", true).
		Handler(func(ctx context.Context, args map[string]string) (*server.PromptResult, error) {
			return &server.PromptResult{
				Description: "Summary prompt",
				Messages: []server.PromptMessage{
					{
						Role: "user",
						Content: server.TextContent{
							Type: "text",
							Text: "Please summarize: " + args["content"],
						},
					},
				},
			}, nil
		})

	client := testutil.NewTestClient(t, srv)

	t.Run("ListPrompts", func(t *testing.T) {
		prompts, err := client.ListPrompts()
		if err != nil {
			t.Fatalf("ListPrompts failed: %v", err)
		}

		if len(prompts) != 1 {
			t.Errorf("expected 1 prompt, got %d", len(prompts))
		}

		if prompts[0]["name"] != "summarize" {
			t.Errorf("expected 'summarize', got %v", prompts[0]["name"])
		}
	})

	t.Run("GetPrompt", func(t *testing.T) {
		result, err := client.GetPrompt("summarize", map[string]string{"content": "test text"})
		if err != nil {
			t.Fatalf("GetPrompt failed: %v", err)
		}

		if result["description"] != "Summary prompt" {
			t.Errorf("expected 'Summary prompt', got %v", result["description"])
		}

		messages, ok := result["messages"].([]server.PromptMessage)
		if !ok {
			t.Fatal("expected messages in result")
		}

		if len(messages) != 1 {
			t.Errorf("expected 1 message, got %d", len(messages))
		}
	})

	t.Run("GetPrompt not found", func(t *testing.T) {
		_, err := client.GetPrompt("nonexistent", nil)
		if err == nil {
			t.Fatal("expected error for nonexistent prompt")
		}
	})
}

func TestMockTransport(t *testing.T) {
	t.Run("basic request/response", func(t *testing.T) {
		mock := testutil.NewMockTransport()

		// Send a ping request
		err := mock.SendRequest("ping", nil)
		if err != nil {
			t.Fatalf("SendRequest failed: %v", err)
		}

		// Read the request back
		req, err := mock.ReadRequest()
		if err != nil {
			t.Fatalf("ReadRequest failed: %v", err)
		}

		if req.Method != "ping" {
			t.Errorf("expected method 'ping', got %q", req.Method)
		}

		// Write a response
		err = mock.WriteResponse(map[string]any{}, nil)
		if err != nil {
			t.Fatalf("WriteResponse failed: %v", err)
		}

		// Read the response
		resp, err := mock.ReadResponse()
		if err != nil {
			t.Fatalf("ReadResponse failed: %v", err)
		}

		if resp.Error != nil {
			t.Errorf("unexpected error: %v", resp.Error)
		}
	})

	t.Run("error response", func(t *testing.T) {
		mock := testutil.NewMockTransport()

		err := mock.WriteResponse(nil, errors.New("test error"))
		if err != nil {
			t.Fatalf("WriteResponse failed: %v", err)
		}

		resp, err := mock.ReadResponse()
		if err != nil {
			t.Fatalf("ReadResponse failed: %v", err)
		}

		if resp.Error == nil {
			t.Fatal("expected error in response")
		}

		if resp.Error.Message != "test error" {
			t.Errorf("expected 'test error', got %q", resp.Error.Message)
		}
	})
}

func TestMockTransportRecorder(t *testing.T) {
	t.Run("recorded requests", func(t *testing.T) {
		mock := testutil.NewMockTransportRecorder()

		_ = mock.SendRequest("method1", nil)
		_ = mock.SendRequest("method2", map[string]string{"key": "value"})

		requests := mock.RecordedRequests()
		if len(requests) != 2 {
			t.Errorf("expected 2 requests, got %d", len(requests))
		}

		if requests[0].Method != "method1" {
			t.Errorf("expected method1, got %s", requests[0].Method)
		}

		if requests[1].Method != "method2" {
			t.Errorf("expected method2, got %s", requests[1].Method)
		}
	})

	t.Run("reset", func(t *testing.T) {
		mock := testutil.NewMockTransportRecorder()

		_ = mock.SendRequest("test", nil)
		mock.Reset()

		requests := mock.RecordedRequests()
		if len(requests) != 0 {
			t.Errorf("expected 0 requests after reset, got %d", len(requests))
		}
	})
}

func TestAssertToolExists(t *testing.T) {
	srv := mcp.NewServer(mcp.ServerInfo{
		Name:    "test-server",
		Version: "1.0.0",
	})

	srv.Tool("existing-tool").
		Description("Exists").
		Handler(func(ctx context.Context, input struct{}) (string, error) {
			return "ok", nil
		})

	client := testutil.NewTestClient(t, srv)

	// This should not fail
	client.AssertToolExists("existing-tool")
}

func TestAssertResourceExists(t *testing.T) {
	srv := mcp.NewServer(mcp.ServerInfo{
		Name:    "test-server",
		Version: "1.0.0",
	})

	srv.Resource("test://resource").
		Name("test").
		Handler(func(ctx context.Context, uri string, params map[string]string) (*server.ResourceContent, error) {
			return &server.ResourceContent{}, nil
		})

	client := testutil.NewTestClient(t, srv)

	// This should not fail
	client.AssertResourceExists("test://resource")
}

func TestAssertPromptExists(t *testing.T) {
	srv := mcp.NewServer(mcp.ServerInfo{
		Name:    "test-server",
		Version: "1.0.0",
	})

	srv.Prompt("test-prompt").
		Description("Test").
		Handler(func(ctx context.Context, args map[string]string) (*server.PromptResult, error) {
			return &server.PromptResult{}, nil
		})

	client := testutil.NewTestClient(t, srv)

	// This should not fail
	client.AssertPromptExists("test-prompt")
}
