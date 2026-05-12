package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/felixgeelhaar/mcp-go/protocol"
	"github.com/felixgeelhaar/mcp-go/transport"
)

func TestNewServer(t *testing.T) {
	srv := NewServer(ServerInfo{
		Name:    "test-server",
		Version: "1.0.0",
	})

	if srv == nil {
		t.Fatal("expected server to be created")
	}

	info := srv.Info()
	if info.Name != "test-server" {
		t.Errorf("Name = %q, want %q", info.Name, "test-server")
	}
}

func TestServeStdio_Initialize(t *testing.T) {
	srv := NewServer(ServerInfo{
		Name:    "test-server",
		Version: "1.0.0",
		Capabilities: Capabilities{
			Tools: true,
		},
	})

	// Prepare initialize request
	initReq := map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "initialize",
		"params": map[string]any{
			"protocolVersion": "2024-11-05",
			"clientInfo": map[string]any{
				"name":    "test-client",
				"version": "1.0.0",
			},
		},
	}
	initBytes, _ := json.Marshal(initReq)

	in := bytes.NewBuffer(append(initBytes, '\n'))
	out := &bytes.Buffer{}

	// Create stdio transport with custom streams
	tr := transport.NewStdio(
		transport.WithStdin(in),
		transport.WithStdout(out),
	)

	handler := newRequestHandler(srv)

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	_ = tr.Serve(ctx, handler)

	output := out.String()
	if !strings.Contains(output, `"protocolVersion"`) {
		t.Errorf("expected protocolVersion in response, got %q", output)
	}
	if !strings.Contains(output, `"test-server"`) {
		t.Errorf("expected server name in response, got %q", output)
	}
}

func TestServeStdio_Initialize_WithMetadata(t *testing.T) {
	srv := NewServer(
		ServerInfo{
			Name:    "metadata-test",
			Version: "2.0.0",
		},
		WithTitle("Metadata Test Server"),
		WithDescription("A server with full metadata"),
		WithWebsiteURL("https://example.com/docs"),
		WithIcons(Icon{URI: "https://example.com/icon.png", MimeType: "image/png", Size: 64}),
		WithBuildInfo("abc123def", "2025-01-03"),
	)

	// Prepare initialize request
	initReq := map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "initialize",
		"params": map[string]any{
			"protocolVersion": "2024-11-05",
			"clientInfo": map[string]any{
				"name":    "test-client",
				"version": "1.0.0",
			},
		},
	}
	initBytes, _ := json.Marshal(initReq)

	in := bytes.NewBuffer(append(initBytes, '\n'))
	out := &bytes.Buffer{}

	tr := transport.NewStdio(
		transport.WithStdin(in),
		transport.WithStdout(out),
	)

	handler := newRequestHandler(srv)

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	_ = tr.Serve(ctx, handler)

	output := out.String()

	// Verify required fields
	if !strings.Contains(output, `"name":"metadata-test"`) {
		t.Errorf("expected server name in response, got %q", output)
	}
	if !strings.Contains(output, `"version":"2.0.0"`) {
		t.Errorf("expected version in response, got %q", output)
	}

	// Verify optional metadata fields
	if !strings.Contains(output, `"title":"Metadata Test Server"`) {
		t.Errorf("expected title in response, got %q", output)
	}
	if !strings.Contains(output, `"description":"A server with full metadata"`) {
		t.Errorf("expected description in response, got %q", output)
	}
	if !strings.Contains(output, `"websiteUrl":"https://example.com/docs"`) {
		t.Errorf("expected websiteUrl in response, got %q", output)
	}
	if !strings.Contains(output, `"icons":[`) {
		t.Errorf("expected icons array in response, got %q", output)
	}
	if !strings.Contains(output, `"uri":"https://example.com/icon.png"`) {
		t.Errorf("expected icon URI in response, got %q", output)
	}
	if !strings.Contains(output, `"buildInfo":{`) {
		t.Errorf("expected buildInfo in response, got %q", output)
	}
	if !strings.Contains(output, `"commit":"abc123def"`) {
		t.Errorf("expected commit in buildInfo, got %q", output)
	}
}

func TestServeStdio_Initialize_OmitsEmptyMetadata(t *testing.T) {
	// Server with NO optional metadata set
	srv := NewServer(ServerInfo{
		Name:    "minimal-server",
		Version: "1.0.0",
	})

	initReq := map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "initialize",
		"params": map[string]any{
			"protocolVersion": "2024-11-05",
			"clientInfo": map[string]any{
				"name":    "test-client",
				"version": "1.0.0",
			},
		},
	}
	initBytes, _ := json.Marshal(initReq)

	in := bytes.NewBuffer(append(initBytes, '\n'))
	out := &bytes.Buffer{}

	tr := transport.NewStdio(
		transport.WithStdin(in),
		transport.WithStdout(out),
	)

	handler := newRequestHandler(srv)

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	_ = tr.Serve(ctx, handler)

	output := out.String()

	// Verify required fields are present
	if !strings.Contains(output, `"name":"minimal-server"`) {
		t.Errorf("expected server name in response, got %q", output)
	}

	// Verify optional fields are NOT present (omitempty behavior)
	if strings.Contains(output, `"title"`) {
		t.Errorf("expected title to be omitted when empty, got %q", output)
	}
	if strings.Contains(output, `"description"`) {
		t.Errorf("expected description to be omitted when empty, got %q", output)
	}
	if strings.Contains(output, `"websiteUrl"`) {
		t.Errorf("expected websiteUrl to be omitted when empty, got %q", output)
	}
	if strings.Contains(output, `"icons"`) {
		t.Errorf("expected icons to be omitted when empty, got %q", output)
	}
	if strings.Contains(output, `"buildInfo"`) {
		t.Errorf("expected buildInfo to be omitted when nil, got %q", output)
	}
}

func TestServeStdio_ToolsList(t *testing.T) {
	srv := NewServer(ServerInfo{
		Name:    "test-server",
		Version: "1.0.0",
	})

	type SearchInput struct {
		Query string `json:"query"`
	}

	srv.Tool("search").
		Description("Search for items").
		Handler(func(input SearchInput) (string, error) {
			return "result", nil
		})

	// Prepare tools/list request
	listReq := map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "tools/list",
	}
	listBytes, _ := json.Marshal(listReq)

	in := bytes.NewBuffer(append(listBytes, '\n'))
	out := &bytes.Buffer{}

	tr := transport.NewStdio(
		transport.WithStdin(in),
		transport.WithStdout(out),
	)

	handler := newRequestHandler(srv)

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	_ = tr.Serve(ctx, handler)

	output := out.String()
	if !strings.Contains(output, `"search"`) {
		t.Errorf("expected tool name in response, got %q", output)
	}
	if !strings.Contains(output, `"Search for items"`) {
		t.Errorf("expected tool description in response, got %q", output)
	}
}

func TestServeStdio_ToolsCall(t *testing.T) {
	srv := NewServer(ServerInfo{
		Name:    "test-server",
		Version: "1.0.0",
	})

	type AddInput struct {
		A int `json:"a"`
		B int `json:"b"`
	}

	srv.Tool("add").
		Description("Add two numbers").
		Handler(func(input AddInput) (int, error) {
			return input.A + input.B, nil
		})

	// Prepare tools/call request
	callReq := map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "tools/call",
		"params": map[string]any{
			"name":      "add",
			"arguments": map[string]any{"a": 5, "b": 3},
		},
	}
	callBytes, _ := json.Marshal(callReq)

	in := bytes.NewBuffer(append(callBytes, '\n'))
	out := &bytes.Buffer{}

	tr := transport.NewStdio(
		transport.WithStdin(in),
		transport.WithStdout(out),
	)

	handler := newRequestHandler(srv)

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	_ = tr.Serve(ctx, handler)

	output := out.String()
	if !strings.Contains(output, `"content"`) {
		t.Errorf("expected content in response, got %q", output)
	}
	if !strings.Contains(output, "8") {
		t.Errorf("expected result 8 in response, got %q", output)
	}
}

func TestServeStdio_ToolsCall_StructResult(t *testing.T) {
	srv := NewServer(ServerInfo{
		Name:    "test-server",
		Version: "1.0.0",
	})

	type StatusInput struct{}
	type StatusResult struct {
		Status  string `json:"status"`
		Message string `json:"message"`
		Count   int    `json:"count"`
	}

	srv.Tool("status").
		Description("Get status").
		Handler(func(input StatusInput) (StatusResult, error) {
			return StatusResult{
				Status:  "ok",
				Message: "All systems operational",
				Count:   42,
			}, nil
		})

	// Prepare tools/call request
	callReq := map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "tools/call",
		"params": map[string]any{
			"name":      "status",
			"arguments": map[string]any{},
		},
	}
	callBytes, _ := json.Marshal(callReq)

	in := bytes.NewBuffer(append(callBytes, '\n'))
	out := &bytes.Buffer{}

	tr := transport.NewStdio(
		transport.WithStdin(in),
		transport.WithStdout(out),
	)

	handler := newRequestHandler(srv)

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	_ = tr.Serve(ctx, handler)

	output := out.String()

	// Verify the response contains properly serialized JSON
	if !strings.Contains(output, `"content"`) {
		t.Errorf("expected content in response, got %q", output)
	}

	// The text field should be a JSON string, not a nested object
	// Parse the response and verify the text field is a string
	var resp struct {
		Result struct {
			Content []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"content"`
		} `json:"result"`
	}

	// Find the JSON response line (skip any empty lines)
	for _, line := range strings.Split(output, "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		if err := json.Unmarshal([]byte(line), &resp); err == nil {
			break
		}
	}

	if len(resp.Result.Content) == 0 {
		t.Fatalf("expected content array, got empty")
	}

	text := resp.Result.Content[0].Text
	if text == "" {
		t.Fatalf("expected text to be non-empty string")
	}

	// The text should be valid JSON that can be parsed
	var result StatusResult
	if err := json.Unmarshal([]byte(text), &result); err != nil {
		t.Fatalf("text should be valid JSON: %v, got: %q", err, text)
	}

	if result.Status != "ok" {
		t.Errorf("Status = %q, want %q", result.Status, "ok")
	}
	if result.Message != "All systems operational" {
		t.Errorf("Message = %q, want %q", result.Message, "All systems operational")
	}
	if result.Count != 42 {
		t.Errorf("Count = %d, want %d", result.Count, 42)
	}
}

func TestServeStdio_Ping(t *testing.T) {
	srv := NewServer(ServerInfo{
		Name:    "test-server",
		Version: "1.0.0",
	})

	// Prepare ping request
	pingReq := map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "ping",
	}
	pingBytes, _ := json.Marshal(pingReq)

	in := bytes.NewBuffer(append(pingBytes, '\n'))
	out := &bytes.Buffer{}

	tr := transport.NewStdio(
		transport.WithStdin(in),
		transport.WithStdout(out),
	)

	handler := newRequestHandler(srv)

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	_ = tr.Serve(ctx, handler)

	output := out.String()
	if !strings.Contains(output, `"result"`) {
		t.Errorf("expected result in response, got %q", output)
	}
}

func TestServeStdio_Initialize_AutoDetectCapabilities(t *testing.T) {
	// Create server WITHOUT explicit capabilities
	srv := NewServer(ServerInfo{
		Name:    "test-server",
		Version: "1.0.0",
		// Note: No Capabilities set - should auto-detect from registered handlers
	})

	// Register a tool (without setting Capabilities.Tools = true)
	type Input struct {
		Query string `json:"query"`
	}
	srv.Tool("search").
		Description("Search for items").
		Handler(func(input Input) (string, error) {
			return "result", nil
		})

	// Register a resource (without setting Capabilities.Resources = true)
	srv.Resource("data://info").
		Name("Info").
		Handler(func(ctx context.Context, uri string, params map[string]string) (*ResourceContent, error) {
			return &ResourceContent{URI: uri, Text: "info"}, nil
		})

	// Register a prompt (without setting Capabilities.Prompts = true)
	srv.Prompt("greeting").
		Description("A greeting prompt").
		Handler(func(ctx context.Context, args map[string]string) (*PromptResult, error) {
			return &PromptResult{
				Messages: []PromptMessage{{Role: "assistant", Content: TextContent{Type: "text", Text: "Hello"}}},
			}, nil
		})

	// Prepare initialize request
	initReq := map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "initialize",
		"params": map[string]any{
			"protocolVersion": "2024-11-05",
			"clientInfo": map[string]any{
				"name":    "test-client",
				"version": "1.0.0",
			},
		},
	}
	initBytes, _ := json.Marshal(initReq)

	in := bytes.NewBuffer(append(initBytes, '\n'))
	out := &bytes.Buffer{}

	tr := transport.NewStdio(
		transport.WithStdin(in),
		transport.WithStdout(out),
	)

	handler := newRequestHandler(srv)

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	_ = tr.Serve(ctx, handler)

	output := out.String()

	// Parse the response to verify capabilities
	var resp struct {
		Result struct {
			Capabilities map[string]any `json:"capabilities"`
		} `json:"result"`
	}

	for _, line := range strings.Split(output, "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		if err := json.Unmarshal([]byte(line), &resp); err == nil {
			break
		}
	}

	// Verify all three capabilities are present (auto-detected from registered handlers)
	if _, ok := resp.Result.Capabilities["tools"]; !ok {
		t.Errorf("expected tools capability to be advertised, got capabilities: %v", resp.Result.Capabilities)
	}
	if _, ok := resp.Result.Capabilities["resources"]; !ok {
		t.Errorf("expected resources capability to be advertised, got capabilities: %v", resp.Result.Capabilities)
	}
	if _, ok := resp.Result.Capabilities["prompts"]; !ok {
		t.Errorf("expected prompts capability to be advertised, got capabilities: %v", resp.Result.Capabilities)
	}
}

func TestServeStdio_Initialize_EmptyCapabilities(t *testing.T) {
	// Create server with no tools, resources, or prompts
	srv := NewServer(ServerInfo{
		Name:    "test-server",
		Version: "1.0.0",
	})

	// Prepare initialize request
	initReq := map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "initialize",
		"params": map[string]any{
			"protocolVersion": "2024-11-05",
			"clientInfo": map[string]any{
				"name":    "test-client",
				"version": "1.0.0",
			},
		},
	}
	initBytes, _ := json.Marshal(initReq)

	in := bytes.NewBuffer(append(initBytes, '\n'))
	out := &bytes.Buffer{}

	tr := transport.NewStdio(
		transport.WithStdin(in),
		transport.WithStdout(out),
	)

	handler := newRequestHandler(srv)

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	_ = tr.Serve(ctx, handler)

	output := out.String()

	// Parse the response to verify capabilities
	var resp struct {
		Result struct {
			Capabilities map[string]any `json:"capabilities"`
		} `json:"result"`
	}

	for _, line := range strings.Split(output, "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		if err := json.Unmarshal([]byte(line), &resp); err == nil {
			break
		}
	}

	// Verify capabilities is empty when nothing is registered
	if len(resp.Result.Capabilities) != 0 {
		t.Errorf("expected empty capabilities when nothing registered, got: %v", resp.Result.Capabilities)
	}
}

func TestHandleToolsList_WithOutputSchema(t *testing.T) {
	srv := NewServer(ServerInfo{Name: "test", Version: "1.0.0"})

	type Input struct {
		URL string `json:"url"`
	}
	type TableOutput struct {
		Headers []string   `json:"headers"`
		Rows    [][]string `json:"rows"`
	}

	srv.Tool("extract_table").
		Description("Extract table data").
		OutputSchema(TableOutput{}).
		Handler(func(input Input) (StructuredResult, error) {
			return StructuredResult{
				Content:           []Content{NewTextContent("Found table")},
				StructuredContent: map[string]any{"headers": []string{"a"}},
			}, nil
		})

	handler := newRequestHandler(srv)

	listReq := &protocol.Request{
		JSONRPC: "2.0",
		ID:      json.RawMessage(`1`),
		Method:  "tools/list",
	}

	resp, err := handler.HandleRequest(context.Background(), listReq)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	result, _ := json.Marshal(resp.Result)
	if !strings.Contains(string(result), `"outputSchema"`) {
		t.Errorf("expected outputSchema in tools/list response, got: %s", result)
	}
}

func TestHandleToolsCall_StructuredResult(t *testing.T) {
	srv := NewServer(ServerInfo{Name: "test", Version: "1.0.0"})

	type Input struct{}

	srv.Tool("structured").
		Handler(func(input Input) (StructuredResult, error) {
			return StructuredResult{
				Content: []Content{NewTextContent("Found 3 rows")},
				StructuredContent: map[string]any{
					"headers": []string{"name", "age"},
					"rows":    [][]string{{"Alice", "30"}},
				},
			}, nil
		})

	handler := newRequestHandler(srv)

	callReq := &protocol.Request{
		JSONRPC: "2.0",
		ID:      json.RawMessage(`1`),
		Method:  "tools/call",
		Params:  json.RawMessage(`{"name":"structured","arguments":{}}`),
	}

	resp, err := handler.HandleRequest(context.Background(), callReq)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	result, _ := json.Marshal(resp.Result)
	resultStr := string(result)

	if !strings.Contains(resultStr, `"structuredContent"`) {
		t.Errorf("expected structuredContent in response, got: %s", resultStr)
	}
	if !strings.Contains(resultStr, `"content"`) {
		t.Errorf("expected content in response, got: %s", resultStr)
	}
	if !strings.Contains(resultStr, "Found 3 rows") {
		t.Errorf("expected text content in response, got: %s", resultStr)
	}
}

// TestHandleToolsCall_TypedStructResultPopulatesStructuredContent
// regression-tests strict-client failures of the form
// `Tool ... has an output schema but did not return structured content`.
//
// When a tool declares OutputSchema and the handler returns a typed
// struct (not StructuredResult), the server must populate
// structuredContent on the response so the payload satisfies the spec.
// Text content is preserved for backward compatibility with clients that
// only read content[].text.
func TestHandleToolsCall_TypedStructResultPopulatesStructuredContent(t *testing.T) {
	srv := NewServer(ServerInfo{Name: "test", Version: "1.0.0"})

	type Input struct{}
	type Output struct {
		Answer string   `json:"answer"`
		Tags   []string `json:"tags"`
	}

	srv.Tool("query").
		OutputSchema(Output{}).
		Handler(func(input Input) (Output, error) {
			return Output{Answer: "42", Tags: []string{"a", "b"}}, nil
		})

	handler := newRequestHandler(srv)

	callReq := &protocol.Request{
		JSONRPC: "2.0",
		ID:      json.RawMessage(`1`),
		Method:  "tools/call",
		Params:  json.RawMessage(`{"name":"query","arguments":{}}`),
	}

	resp, err := handler.HandleRequest(context.Background(), callReq)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	result, _ := json.Marshal(resp.Result)
	var parsed map[string]any
	if err := json.Unmarshal(result, &parsed); err != nil {
		t.Fatalf("unmarshal: %v; raw=%s", err, result)
	}

	sc, ok := parsed["structuredContent"].(map[string]any)
	if !ok {
		t.Fatalf("expected structuredContent object; raw=%s", result)
	}
	if sc["answer"] != "42" {
		t.Errorf("structuredContent.answer = %v, want %q; raw=%s", sc["answer"], "42", result)
	}
	tags, ok := sc["tags"].([]any)
	if !ok || len(tags) != 2 {
		t.Errorf("structuredContent.tags = %v, want 2-element array; raw=%s", sc["tags"], result)
	}

	// Text content is still present for legacy clients.
	if !strings.Contains(string(result), `"content"`) {
		t.Errorf("expected content array for backward compat; raw=%s", result)
	}
}

// TestHandleToolsCall_TypedStructResultWithoutSchemaOmitsStructured
// guards against regressions in the other direction: when no
// OutputSchema is declared the legacy text-only path must remain.
func TestHandleToolsCall_TypedStructResultWithoutSchemaOmitsStructured(t *testing.T) {
	srv := NewServer(ServerInfo{Name: "test", Version: "1.0.0"})

	type Input struct{}
	type Output struct {
		Answer string `json:"answer"`
	}

	srv.Tool("query_no_schema").
		Handler(func(input Input) (Output, error) {
			return Output{Answer: "42"}, nil
		})

	handler := newRequestHandler(srv)

	callReq := &protocol.Request{
		JSONRPC: "2.0",
		ID:      json.RawMessage(`1`),
		Method:  "tools/call",
		Params:  json.RawMessage(`{"name":"query_no_schema","arguments":{}}`),
	}

	resp, err := handler.HandleRequest(context.Background(), callReq)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	result, _ := json.Marshal(resp.Result)
	if strings.Contains(string(result), `"structuredContent"`) {
		t.Errorf("no outputSchema declared, should not emit structuredContent; raw=%s", result)
	}
}

func TestHandleToolsCall_LegacyStringResult(t *testing.T) {
	srv := NewServer(ServerInfo{Name: "test", Version: "1.0.0"})

	type Input struct{}

	srv.Tool("simple").
		Handler(func(input Input) (string, error) {
			return "hello world", nil
		})

	handler := newRequestHandler(srv)

	callReq := &protocol.Request{
		JSONRPC: "2.0",
		ID:      json.RawMessage(`1`),
		Method:  "tools/call",
		Params:  json.RawMessage(`{"name":"simple","arguments":{}}`),
	}

	resp, err := handler.HandleRequest(context.Background(), callReq)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	result, _ := json.Marshal(resp.Result)
	resultStr := string(result)

	// Should NOT have structuredContent
	if strings.Contains(resultStr, `"structuredContent"`) {
		t.Errorf("legacy handler should not return structuredContent, got: %s", resultStr)
	}
	if !strings.Contains(resultStr, "hello world") {
		t.Errorf("expected text content, got: %s", resultStr)
	}
}

func TestServer_RemoveTool_Integration(t *testing.T) {
	srv := NewServer(ServerInfo{Name: "test", Version: "1.0.0"})

	type Input struct{}

	srv.Tool("a").Handler(func(input Input) (string, error) { return "a", nil })
	srv.Tool("b").Handler(func(input Input) (string, error) { return "b", nil })

	handler := newRequestHandler(srv)

	// List tools - should have 2
	listReq := &protocol.Request{
		JSONRPC: "2.0",
		ID:      json.RawMessage(`1`),
		Method:  "tools/list",
	}

	resp, _ := handler.HandleRequest(context.Background(), listReq)
	result, _ := json.Marshal(resp.Result)

	var listResult struct {
		Tools []map[string]any `json:"tools"`
	}
	json.Unmarshal(result, &listResult)

	if len(listResult.Tools) != 2 {
		t.Fatalf("expected 2 tools, got %d", len(listResult.Tools))
	}

	// Remove tool "a"
	srv.RemoveTool("a")

	// List tools - should have 1
	resp, _ = handler.HandleRequest(context.Background(), listReq)
	result, _ = json.Marshal(resp.Result)
	json.Unmarshal(result, &listResult)

	if len(listResult.Tools) != 1 {
		t.Fatalf("expected 1 tool after removal, got %d", len(listResult.Tools))
	}
	if listResult.Tools[0]["name"] != "b" {
		t.Errorf("expected remaining tool 'b', got %q", listResult.Tools[0]["name"])
	}
}

func TestInitialize_ListChangedCapability(t *testing.T) {
	srv := NewServer(ServerInfo{Name: "test", Version: "1.0.0"})

	type Input struct{}
	srv.Tool("t").Handler(func(input Input) (string, error) { return "", nil })

	handler := newRequestHandler(srv)

	initReq := &protocol.Request{
		JSONRPC: "2.0",
		ID:      json.RawMessage(`1`),
		Method:  "initialize",
		Params:  json.RawMessage(`{"protocolVersion":"2024-11-05","clientInfo":{"name":"test","version":"1.0.0"}}`),
	}

	resp, err := handler.HandleRequest(context.Background(), initReq)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	result, _ := json.Marshal(resp.Result)
	resultStr := string(result)

	if !strings.Contains(resultStr, `"listChanged":true`) {
		t.Errorf("expected listChanged:true in capabilities, got: %s", resultStr)
	}
}
