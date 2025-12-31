package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

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
