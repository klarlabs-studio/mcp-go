// Package e2e provides end-to-end compliance tests for the MCP implementation.
package e2e

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"go.klarlabs.de/mcp"
	"go.klarlabs.de/mcp/protocol"
)

// TestMCPCompliance_Initialize tests the initialize handshake.
func TestMCPCompliance_Initialize(t *testing.T) {
	srv := mcp.NewServer(mcp.ServerInfo{
		Name:    "compliance-test",
		Version: "1.0.0",
		Capabilities: mcp.Capabilities{
			Tools:     true,
			Resources: true,
			Prompts:   true,
		},
	})

	t.Run("returns correct protocol version", func(t *testing.T) {
		resp := executeRequest(t, srv, &protocol.Request{
			JSONRPC: "2.0",
			ID:      json.RawMessage(`1`),
			Method:  "initialize",
			Params:  json.RawMessage(`{"protocolVersion":"2024-11-05","clientInfo":{"name":"test-client","version":"1.0.0"}}`),
		})

		if resp.Error != nil {
			t.Fatalf("unexpected error: %v", resp.Error)
		}

		result := resp.Result.(map[string]any)
		if result["protocolVersion"] != protocol.MCPVersion {
			t.Errorf("protocolVersion = %v, want %v", result["protocolVersion"], protocol.MCPVersion)
		}
	})

	t.Run("returns server info", func(t *testing.T) {
		resp := executeRequest(t, srv, &protocol.Request{
			JSONRPC: "2.0",
			ID:      json.RawMessage(`1`),
			Method:  "initialize",
		})

		result := resp.Result.(map[string]any)
		serverInfo := result["serverInfo"].(map[string]any)

		if serverInfo["name"] != "compliance-test" {
			t.Errorf("serverInfo.name = %v, want %q", serverInfo["name"], "compliance-test")
		}
		if serverInfo["version"] != "1.0.0" {
			t.Errorf("serverInfo.version = %v, want %q", serverInfo["version"], "1.0.0")
		}
	})

	t.Run("returns capabilities", func(t *testing.T) {
		resp := executeRequest(t, srv, &protocol.Request{
			JSONRPC: "2.0",
			ID:      json.RawMessage(`1`),
			Method:  "initialize",
		})

		result := resp.Result.(map[string]any)
		capabilities := result["capabilities"].(map[string]any)

		if _, ok := capabilities["tools"]; !ok {
			t.Error("expected tools capability")
		}
		if _, ok := capabilities["resources"]; !ok {
			t.Error("expected resources capability")
		}
		if _, ok := capabilities["prompts"]; !ok {
			t.Error("expected prompts capability")
		}
	})
}

// TestMCPCompliance_Tools tests tool operations.
func TestMCPCompliance_Tools(t *testing.T) {
	type AddInput struct {
		A int `json:"a"`
		B int `json:"b"`
	}

	srv := mcp.NewServer(mcp.ServerInfo{
		Name:    "compliance-test",
		Version: "1.0.0",
		Capabilities: mcp.Capabilities{
			Tools: true,
		},
	})

	srv.Tool("add").
		Description("Add two numbers").
		Handler(func(input AddInput) (int, error) {
			return input.A + input.B, nil
		})

	t.Run("tools/list returns registered tools", func(t *testing.T) {
		resp := executeRequest(t, srv, &protocol.Request{
			JSONRPC: "2.0",
			ID:      json.RawMessage(`1`),
			Method:  "tools/list",
		})

		if resp.Error != nil {
			t.Fatalf("unexpected error: %v", resp.Error)
		}

		result := resp.Result.(map[string]any)
		tools := result["tools"].([]any)

		if len(tools) != 1 {
			t.Fatalf("expected 1 tool, got %d", len(tools))
		}

		tool := tools[0].(map[string]any)
		if tool["name"] != "add" {
			t.Errorf("tool.name = %v, want %q", tool["name"], "add")
		}
		if tool["description"] != "Add two numbers" {
			t.Errorf("tool.description = %v, want %q", tool["description"], "Add two numbers")
		}
		if tool["inputSchema"] == nil {
			t.Error("expected inputSchema")
		}
	})

	t.Run("tools/call executes tool", func(t *testing.T) {
		resp := executeRequest(t, srv, &protocol.Request{
			JSONRPC: "2.0",
			ID:      json.RawMessage(`1`),
			Method:  "tools/call",
			Params:  json.RawMessage(`{"name":"add","arguments":{"a":2,"b":3}}`),
		})

		if resp.Error != nil {
			t.Fatalf("unexpected error: %v", resp.Error)
		}

		result := resp.Result.(map[string]any)
		content := result["content"].([]any)

		if len(content) != 1 {
			t.Fatalf("expected 1 content item, got %d", len(content))
		}

		item := content[0].(map[string]any)
		if item["type"] != "text" {
			t.Errorf("content.type = %v, want %q", item["type"], "text")
		}
		// Result is 5 (2+3)
		if item["text"] != float64(5) {
			t.Errorf("content.text = %v, want %v", item["text"], 5)
		}
	})

	t.Run("tools/call returns error for unknown tool", func(t *testing.T) {
		resp := executeRequest(t, srv, &protocol.Request{
			JSONRPC: "2.0",
			ID:      json.RawMessage(`1`),
			Method:  "tools/call",
			Params:  json.RawMessage(`{"name":"unknown","arguments":{}}`),
		})

		if resp.Error == nil {
			t.Fatal("expected error for unknown tool")
		}

		if resp.Error.Code != protocol.CodeNotFound {
			t.Errorf("error.code = %d, want %d", resp.Error.Code, protocol.CodeNotFound)
		}
	})
}

// TestMCPCompliance_Resources tests resource operations.
func TestMCPCompliance_Resources(t *testing.T) {
	srv := mcp.NewServer(mcp.ServerInfo{
		Name:    "compliance-test",
		Version: "1.0.0",
		Capabilities: mcp.Capabilities{
			Resources: true,
		},
	})

	srv.Resource("file://{path}").
		Name("File").
		Description("Read a file").
		MimeType("text/plain").
		Handler(func(ctx context.Context, uri string, params map[string]string) (*mcp.ResourceContent, error) {
			return &mcp.ResourceContent{
				URI:      uri,
				MimeType: "text/plain",
				Text:     "Content of " + params["path"],
			}, nil
		})

	t.Run("resources/list returns registered resources", func(t *testing.T) {
		resp := executeRequest(t, srv, &protocol.Request{
			JSONRPC: "2.0",
			ID:      json.RawMessage(`1`),
			Method:  "resources/list",
		})

		if resp.Error != nil {
			t.Fatalf("unexpected error: %v", resp.Error)
		}

		result := resp.Result.(map[string]any)
		resources := result["resources"].([]any)

		if len(resources) != 1 {
			t.Fatalf("expected 1 resource, got %d", len(resources))
		}

		resource := resources[0].(map[string]any)
		if resource["name"] != "File" {
			t.Errorf("resource.name = %v, want %q", resource["name"], "File")
		}
	})

	t.Run("resources/read returns resource content", func(t *testing.T) {
		resp := executeRequest(t, srv, &protocol.Request{
			JSONRPC: "2.0",
			ID:      json.RawMessage(`1`),
			Method:  "resources/read",
			Params:  json.RawMessage(`{"uri":"file://test.txt"}`),
		})

		if resp.Error != nil {
			t.Fatalf("unexpected error: %v", resp.Error)
		}

		result := resp.Result.(map[string]any)
		contents := result["contents"].([]any)

		if len(contents) != 1 {
			t.Fatalf("expected 1 content item, got %d", len(contents))
		}

		content := contents[0].(map[string]any)
		if content["uri"] != "file://test.txt" {
			t.Errorf("content.uri = %v, want %q", content["uri"], "file://test.txt")
		}
		if content["text"] != "Content of test.txt" {
			t.Errorf("content.text = %v, want %q", content["text"], "Content of test.txt")
		}
	})

	t.Run("resources/read returns error for unknown resource", func(t *testing.T) {
		resp := executeRequest(t, srv, &protocol.Request{
			JSONRPC: "2.0",
			ID:      json.RawMessage(`1`),
			Method:  "resources/read",
			Params:  json.RawMessage(`{"uri":"unknown://resource"}`),
		})

		if resp.Error == nil {
			t.Fatal("expected error for unknown resource")
		}

		if resp.Error.Code != protocol.CodeNotFound {
			t.Errorf("error.code = %d, want %d", resp.Error.Code, protocol.CodeNotFound)
		}
	})
}

// TestMCPCompliance_Prompts tests prompt operations.
func TestMCPCompliance_Prompts(t *testing.T) {
	srv := mcp.NewServer(mcp.ServerInfo{
		Name:    "compliance-test",
		Version: "1.0.0",
		Capabilities: mcp.Capabilities{
			Prompts: true,
		},
	})

	srv.Prompt("greet").
		Description("Generate a greeting").
		Argument("name", "Name to greet", true).
		Handler(func(ctx context.Context, args map[string]string) (*mcp.PromptResult, error) {
			return &mcp.PromptResult{
				Messages: []mcp.PromptMessage{
					{
						Role: "user",
						Content: mcp.TextContent{
							Type: "text",
							Text: "Hello, " + args["name"] + "!",
						},
					},
				},
			}, nil
		})

	t.Run("prompts/list returns registered prompts", func(t *testing.T) {
		resp := executeRequest(t, srv, &protocol.Request{
			JSONRPC: "2.0",
			ID:      json.RawMessage(`1`),
			Method:  "prompts/list",
		})

		if resp.Error != nil {
			t.Fatalf("unexpected error: %v", resp.Error)
		}

		result := resp.Result.(map[string]any)
		prompts := result["prompts"].([]any)

		if len(prompts) != 1 {
			t.Fatalf("expected 1 prompt, got %d", len(prompts))
		}

		prompt := prompts[0].(map[string]any)
		if prompt["name"] != "greet" {
			t.Errorf("prompt.name = %v, want %q", prompt["name"], "greet")
		}
	})

	t.Run("prompts/get returns prompt messages", func(t *testing.T) {
		resp := executeRequest(t, srv, &protocol.Request{
			JSONRPC: "2.0",
			ID:      json.RawMessage(`1`),
			Method:  "prompts/get",
			Params:  json.RawMessage(`{"name":"greet","arguments":{"name":"World"}}`),
		})

		if resp.Error != nil {
			t.Fatalf("unexpected error: %v", resp.Error)
		}

		result := resp.Result.(map[string]any)
		messages := result["messages"].([]any)

		if len(messages) != 1 {
			t.Fatalf("expected 1 message, got %d", len(messages))
		}

		message := messages[0].(map[string]any)
		if message["role"] != "user" {
			t.Errorf("message.role = %v, want %q", message["role"], "user")
		}

		content := message["content"].(map[string]any)
		if content["text"] != "Hello, World!" {
			t.Errorf("content.text = %v, want %q", content["text"], "Hello, World!")
		}
	})

	t.Run("prompts/get validates required arguments", func(t *testing.T) {
		resp := executeRequest(t, srv, &protocol.Request{
			JSONRPC: "2.0",
			ID:      json.RawMessage(`1`),
			Method:  "prompts/get",
			Params:  json.RawMessage(`{"name":"greet","arguments":{}}`),
		})

		if resp.Error == nil {
			t.Fatal("expected error for missing required argument")
		}
	})
}

// TestMCPCompliance_Ping tests the ping operation.
func TestMCPCompliance_Ping(t *testing.T) {
	srv := mcp.NewServer(mcp.ServerInfo{
		Name:    "compliance-test",
		Version: "1.0.0",
	})

	t.Run("ping returns empty response", func(t *testing.T) {
		resp := executeRequest(t, srv, &protocol.Request{
			JSONRPC: "2.0",
			ID:      json.RawMessage(`1`),
			Method:  "ping",
		})

		if resp.Error != nil {
			t.Fatalf("unexpected error: %v", resp.Error)
		}

		result := resp.Result.(map[string]any)
		if len(result) != 0 {
			t.Errorf("expected empty response, got %v", result)
		}
	})
}

// TestMCPCompliance_Errors tests error handling.
func TestMCPCompliance_Errors(t *testing.T) {
	srv := mcp.NewServer(mcp.ServerInfo{
		Name:    "compliance-test",
		Version: "1.0.0",
	})

	t.Run("unknown method returns MethodNotFound", func(t *testing.T) {
		resp := executeRequest(t, srv, &protocol.Request{
			JSONRPC: "2.0",
			ID:      json.RawMessage(`1`),
			Method:  "unknown/method",
		})

		if resp.Error == nil {
			t.Fatal("expected error for unknown method")
		}

		if resp.Error.Code != protocol.CodeMethodNotFound {
			t.Errorf("error.code = %d, want %d", resp.Error.Code, protocol.CodeMethodNotFound)
		}
	})

	t.Run("invalid tool input is a tool execution error (SEP-1303)", func(t *testing.T) {
		srv.Tool("test").Handler(func(input struct{ X int }) (int, error) { return input.X, nil })

		resp := executeRequest(t, srv, &protocol.Request{
			JSONRPC: "2.0",
			ID:      json.RawMessage(`1`),
			Method:  "tools/call",
			Params:  json.RawMessage(`{"name":"test","arguments":"invalid"}`),
		})

		// Per MCP 2025-11-25, invalid input is returned as an isError result so
		// the model can self-correct — not a -32602 protocol error.
		if resp.Error != nil {
			t.Fatalf("expected no protocol error, got %+v", resp.Error)
		}
		result, ok := resp.Result.(map[string]any)
		if !ok || result["isError"] != true {
			t.Errorf("expected isError result, got %v", resp.Result)
		}
	})
}

// TestMCPCompliance_JSONRPC tests JSON-RPC 2.0 compliance.
func TestMCPCompliance_JSONRPC(t *testing.T) {
	srv := mcp.NewServer(mcp.ServerInfo{
		Name:    "compliance-test",
		Version: "1.0.0",
	})

	t.Run("response includes jsonrpc version", func(t *testing.T) {
		resp := executeRequest(t, srv, &protocol.Request{
			JSONRPC: "2.0",
			ID:      json.RawMessage(`1`),
			Method:  "ping",
		})

		if resp.JSONRPC != "2.0" {
			t.Errorf("jsonrpc = %q, want %q", resp.JSONRPC, "2.0")
		}
	})

	t.Run("response includes request ID", func(t *testing.T) {
		resp := executeRequest(t, srv, &protocol.Request{
			JSONRPC: "2.0",
			ID:      json.RawMessage(`"test-id-123"`),
			Method:  "ping",
		})

		if string(resp.ID) != `"test-id-123"` {
			t.Errorf("id = %s, want %q", resp.ID, "test-id-123")
		}
	})

	t.Run("supports numeric request ID", func(t *testing.T) {
		resp := executeRequest(t, srv, &protocol.Request{
			JSONRPC: "2.0",
			ID:      json.RawMessage(`42`),
			Method:  "ping",
		})

		if string(resp.ID) != "42" {
			t.Errorf("id = %s, want %q", resp.ID, "42")
		}
	})
}

// executeRequest is a helper that executes a request and returns the response.
func executeRequest(t *testing.T, srv *mcp.Server, req *protocol.Request) *protocol.Response {
	t.Helper()

	// Create request data and output buffer
	reqData, _ := json.Marshal(req)
	output := new(bytes.Buffer)

	// Create a custom handler that writes to output
	handler := &testHandler{srv: srv, output: output}

	// Process single request
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := handler.processLine(ctx, reqData); err != nil {
		t.Fatalf("failed to process request: %v", err)
	}

	// Parse response
	line := strings.TrimSpace(output.String())
	var resp protocol.Response
	if err := json.Unmarshal([]byte(line), &resp); err != nil {
		t.Fatalf("failed to parse response: %v (response: %s)", err, line)
	}

	return &resp
}

// testHandler processes requests for testing.
type testHandler struct {
	srv    *mcp.Server
	output io.Writer
}

func (h *testHandler) processLine(ctx context.Context, line []byte) error {
	var req protocol.Request
	if err := json.Unmarshal(line, &req); err != nil {
		return err
	}

	// Create request handler and process
	resp, err := h.handleRequest(ctx, &req)
	if err != nil {
		resp = protocol.NewErrorResponse(req.ID, err.(*protocol.Error))
	}

	data, _ := json.Marshal(resp)
	_, writeErr := h.output.Write(append(data, '\n'))
	return writeErr
}

func (h *testHandler) handleRequest(ctx context.Context, req *protocol.Request) (*protocol.Response, error) {
	switch req.Method {
	case "initialize":
		return h.handleInitialize(req)
	case "tools/list":
		return h.handleToolsList(req)
	case "tools/call":
		return h.handleToolsCall(ctx, req)
	case "resources/list":
		return h.handleResourcesList(req)
	case "resources/read":
		return h.handleResourcesRead(ctx, req)
	case "prompts/list":
		return h.handlePromptsList(req)
	case "prompts/get":
		return h.handlePromptsGet(ctx, req)
	case "ping":
		return protocol.NewResponse(req.ID, map[string]any{}), nil
	default:
		return nil, protocol.NewMethodNotFound(req.Method)
	}
}

func (h *testHandler) handleInitialize(req *protocol.Request) (*protocol.Response, error) {
	manifest := h.srv.Manifest()
	capabilities := make(map[string]any)
	if manifest.Capabilities.Tools {
		capabilities["tools"] = map[string]any{}
	}
	if manifest.Capabilities.Resources {
		capabilities["resources"] = map[string]any{}
	}
	if manifest.Capabilities.Prompts {
		capabilities["prompts"] = map[string]any{}
	}

	result := map[string]any{
		"protocolVersion": manifest.ProtocolVersion,
		"serverInfo": map[string]any{
			"name":    manifest.Name,
			"version": manifest.Version,
		},
		"capabilities": capabilities,
	}
	return protocol.NewResponse(req.ID, result), nil
}

func (h *testHandler) handleToolsList(req *protocol.Request) (*protocol.Response, error) {
	tools := h.srv.Tools()
	toolList := make([]map[string]any, 0, len(tools))
	for _, t := range tools {
		toolList = append(toolList, map[string]any{
			"name":        t.Name,
			"description": t.Description,
			"inputSchema": t.InputSchema,
		})
	}
	return protocol.NewResponse(req.ID, map[string]any{"tools": toolList}), nil
}

func (h *testHandler) handleToolsCall(ctx context.Context, req *protocol.Request) (*protocol.Response, error) {
	var params struct {
		Name      string          `json:"name"`
		Arguments json.RawMessage `json:"arguments"`
	}
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return nil, protocol.NewInvalidParams(err.Error())
	}

	tool, ok := h.srv.GetTool(params.Name)
	if !ok {
		return nil, protocol.NewNotFound("tool not found: " + params.Name)
	}

	result, err := tool.Execute(ctx, params.Arguments)
	if err != nil {
		// SEP-1303: input problems are tool execution errors (isError result).
		var inputErr *mcp.ToolInputError
		if errors.As(err, &inputErr) {
			return protocol.NewResponse(req.ID, map[string]any{
				"content": []map[string]any{{"type": "text", "text": inputErr.Message}},
				"isError": true,
			}), nil
		}
		if mcpErr, ok := err.(*protocol.Error); ok {
			return nil, mcpErr
		}
		return nil, protocol.NewInternalError(err.Error())
	}

	return protocol.NewResponse(req.ID, map[string]any{
		"content": []map[string]any{
			{"type": "text", "text": result},
		},
	}), nil
}

func (h *testHandler) handleResourcesList(req *protocol.Request) (*protocol.Response, error) {
	resources := h.srv.Resources()
	resourceList := make([]map[string]any, 0, len(resources))
	for _, r := range resources {
		item := map[string]any{"uri": r.URITemplate, "name": r.Name}
		if r.Description != "" {
			item["description"] = r.Description
		}
		if r.MimeType != "" {
			item["mimeType"] = r.MimeType
		}
		resourceList = append(resourceList, item)
	}
	return protocol.NewResponse(req.ID, map[string]any{"resources": resourceList}), nil
}

func (h *testHandler) handleResourcesRead(ctx context.Context, req *protocol.Request) (*protocol.Response, error) {
	var params struct {
		URI string `json:"uri"`
	}
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return nil, protocol.NewInvalidParams(err.Error())
	}

	resource, ok := h.srv.FindResourceForURI(params.URI)
	if !ok {
		return nil, protocol.NewNotFound("resource not found: " + params.URI)
	}

	content, err := resource.Read(ctx, params.URI)
	if err != nil {
		return nil, protocol.NewInternalError(err.Error())
	}

	return protocol.NewResponse(req.ID, map[string]any{
		"contents": []map[string]any{
			{"uri": content.URI, "mimeType": content.MimeType, "text": content.Text},
		},
	}), nil
}

func (h *testHandler) handlePromptsList(req *protocol.Request) (*protocol.Response, error) {
	prompts := h.srv.Prompts()
	promptList := make([]map[string]any, 0, len(prompts))
	for _, p := range prompts {
		item := map[string]any{"name": p.Name}
		if p.Description != "" {
			item["description"] = p.Description
		}
		if len(p.Arguments) > 0 {
			args := make([]map[string]any, 0, len(p.Arguments))
			for _, arg := range p.Arguments {
				args = append(args, map[string]any{
					"name":        arg.Name,
					"description": arg.Description,
					"required":    arg.Required,
				})
			}
			item["arguments"] = args
		}
		promptList = append(promptList, item)
	}
	return protocol.NewResponse(req.ID, map[string]any{"prompts": promptList}), nil
}

func (h *testHandler) handlePromptsGet(ctx context.Context, req *protocol.Request) (*protocol.Response, error) {
	var params struct {
		Name      string            `json:"name"`
		Arguments map[string]string `json:"arguments"`
	}
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return nil, protocol.NewInvalidParams(err.Error())
	}

	prompt, ok := h.srv.GetPrompt(params.Name)
	if !ok {
		return nil, protocol.NewNotFound("prompt not found: " + params.Name)
	}

	result, err := prompt.Get(ctx, params.Arguments)
	if err != nil {
		return nil, protocol.NewInvalidParams(err.Error())
	}

	response := map[string]any{"messages": result.Messages}
	if result.Description != "" {
		response["description"] = result.Description
	}
	return protocol.NewResponse(req.ID, response), nil
}
