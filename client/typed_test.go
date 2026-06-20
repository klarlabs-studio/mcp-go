package client_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"go.klarlabs.de/mcp/client"
	"go.klarlabs.de/mcp/protocol"
)

// greetIn is a typed tool input used across the typed-client tests.
type greetIn struct {
	Name string `json:"name"`
}

// greetOut is a typed tool output used across the typed-client tests.
type greetOut struct {
	Message string `json:"message"`
	Count   int    `json:"count"`
}

// textResponse builds a mock JSON-RPC response whose tool result carries a
// single text content block, mirroring how the server serializes typed
// results.
func textResponse(text string) protocol.Response {
	return protocol.Response{
		JSONRPC: "2.0",
		ID:      json.RawMessage(`1`),
		Result: map[string]any{
			"content": []any{
				map[string]any{
					"type": "text",
					"text": text,
				},
			},
		},
	}
}

// resultResponse builds a mock JSON-RPC response from a raw tool result map,
// allowing tests to exercise structuredContent, isError, and arbitrary
// content shapes that the convenience helpers do not cover.
func resultResponse(result map[string]any) protocol.Response {
	return protocol.Response{
		JSONRPC: "2.0",
		ID:      json.RawMessage(`1`),
		Result:  result,
	}
}

// errorResultResponse builds a mock tool result with isError set and a single
// text content block carrying the error message, mirroring how the server
// serializes a failed typed handler.
func errorResultResponse(text string) protocol.Response {
	return resultResponse(map[string]any{
		"isError": true,
		"content": []any{
			map[string]any{"type": "text", "text": text},
		},
	})
}

func TestCall(t *testing.T) {
	t.Run("happy path typed round-trip", func(t *testing.T) {
		transport := &mockTransport{
			responses: []protocol.Response{
				textResponse(`{"message":"Hello, World!","count":2}`),
			},
		}
		c := client.New(transport)

		out, err := client.Call[greetIn, greetOut](
			context.Background(), c, "greet", greetIn{Name: "World"},
		)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if out.Message != "Hello, World!" {
			t.Errorf("message = %q, want %q", out.Message, "Hello, World!")
		}
		if out.Count != 2 {
			t.Errorf("count = %d, want %d", out.Count, 2)
		}

		// The marshaled input must reach the server as the tool arguments.
		if len(transport.requests) != 1 {
			t.Fatalf("expected 1 request, got %d", len(transport.requests))
		}
		var params struct {
			Name      string  `json:"name"`
			Arguments greetIn `json:"arguments"`
		}
		if err := json.Unmarshal(transport.requests[0].Params, &params); err != nil {
			t.Fatalf("unmarshal request params: %v", err)
		}
		if params.Name != "greet" {
			t.Errorf("tool name = %q, want %q", params.Name, "greet")
		}
		if params.Arguments.Name != "World" {
			t.Errorf("argument name = %q, want %q", params.Arguments.Name, "World")
		}
	})

	t.Run("string output returns raw text", func(t *testing.T) {
		transport := &mockTransport{
			responses: []protocol.Response{textResponse("Hello, World!")},
		}
		c := client.New(transport)

		out, err := client.Call[greetIn, string](
			context.Background(), c, "greet", greetIn{Name: "World"},
		)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if out != "Hello, World!" {
			t.Errorf("out = %q, want %q", out, "Hello, World!")
		}
	})

	t.Run("tool not found returns server error", func(t *testing.T) {
		transport := &mockTransport{
			responses: []protocol.Response{
				{
					JSONRPC: "2.0",
					ID:      json.RawMessage(`1`),
					Error:   &protocol.Error{Code: -32601, Message: "tool not found"},
				},
			},
		}
		c := client.New(transport)

		_, err := client.Call[greetIn, greetOut](
			context.Background(), c, "unknown", greetIn{Name: "x"},
		)
		if err == nil {
			t.Fatal("expected error")
		}
		var mcpErr *protocol.Error
		if !errors.As(err, &mcpErr) {
			t.Fatalf("expected *protocol.Error, got %T: %v", err, err)
		}
	})

	t.Run("server error result is reported", func(t *testing.T) {
		transport := &mockTransport{
			responses: []protocol.Response{
				{
					JSONRPC: "2.0",
					ID:      json.RawMessage(`1`),
					Error:   &protocol.Error{Code: -32000, Message: "boom"},
				},
			},
		}
		c := client.New(transport)

		_, err := client.Call[greetIn, greetOut](
			context.Background(), c, "greet", greetIn{Name: "x"},
		)
		if err == nil {
			t.Fatal("expected error")
		}
	})

	t.Run("output unmarshal mismatch returns error", func(t *testing.T) {
		transport := &mockTransport{
			responses: []protocol.Response{
				// count is declared int but the server sends a string.
				textResponse(`{"message":"hi","count":"not-a-number"}`),
			},
		}
		c := client.New(transport)

		_, err := client.Call[greetIn, greetOut](
			context.Background(), c, "greet", greetIn{Name: "x"},
		)
		if err == nil {
			t.Fatal("expected unmarshal error")
		}
	})

	t.Run("no content returns error", func(t *testing.T) {
		transport := &mockTransport{
			responses: []protocol.Response{
				{
					JSONRPC: "2.0",
					ID:      json.RawMessage(`1`),
					Result:  map[string]any{"content": []any{}},
				},
			},
		}
		c := client.New(transport)

		_, err := client.Call[greetIn, greetOut](
			context.Background(), c, "greet", greetIn{Name: "x"},
		)
		if err == nil {
			t.Fatal("expected error for empty content")
		}
		if !errors.Is(err, client.ErrNoToolContent) {
			t.Errorf("error = %v, want ErrNoToolContent", err)
		}
	})

	t.Run("content block selection", func(t *testing.T) {
		tests := []struct {
			name        string
			content     []any
			wantMessage string
			wantCount   int
		}{
			{
				name: "image first, first text block selected",
				content: []any{
					map[string]any{"type": "image", "data": "deadbeef"},
					map[string]any{"type": "text", "text": `{"message":"after-image","count":3}`},
				},
				wantMessage: "after-image",
				wantCount:   3,
			},
			{
				name: "multi-content, first text block selected",
				content: []any{
					map[string]any{"type": "text", "text": `{"message":"first-text","count":1}`},
					map[string]any{"type": "text", "text": `{"message":"second-text","count":2}`},
				},
				wantMessage: "first-text",
				wantCount:   1,
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				transport := &mockTransport{
					responses: []protocol.Response{resultResponse(map[string]any{"content": tt.content})},
				}
				c := client.New(transport)

				out, err := client.Call[greetIn, greetOut](
					context.Background(), c, "greet", greetIn{Name: "x"},
				)
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				if out.Message != tt.wantMessage {
					t.Errorf("message = %q, want %q", out.Message, tt.wantMessage)
				}
				if out.Count != tt.wantCount {
					t.Errorf("count = %d, want %d", out.Count, tt.wantCount)
				}
			})
		}
	})

	t.Run("non-text content without structuredContent returns ErrNoToolContent", func(t *testing.T) {
		transport := &mockTransport{
			responses: []protocol.Response{
				resultResponse(map[string]any{
					"content": []any{
						map[string]any{"type": "image", "data": "deadbeef"},
					},
				}),
			},
		}
		c := client.New(transport)

		_, err := client.Call[greetIn, greetOut](
			context.Background(), c, "greet", greetIn{Name: "x"},
		)
		if !errors.Is(err, client.ErrNoToolContent) {
			t.Errorf("error = %v, want ErrNoToolContent", err)
		}
	})

	t.Run("string output on non-text content returns ErrNoToolContent", func(t *testing.T) {
		transport := &mockTransport{
			responses: []protocol.Response{
				resultResponse(map[string]any{
					"content": []any{
						map[string]any{"type": "image", "data": "deadbeef"},
					},
				}),
			},
		}
		c := client.New(transport)

		_, err := client.Call[greetIn, string](
			context.Background(), c, "greet", greetIn{Name: "x"},
		)
		if !errors.Is(err, client.ErrNoToolContent) {
			t.Errorf("error = %v, want ErrNoToolContent", err)
		}
	})

	t.Run("isError result surfaces as ErrToolError", func(t *testing.T) {
		transport := &mockTransport{
			responses: []protocol.Response{
				errorResultResponse("tool blew up: bad input"),
			},
		}
		c := client.New(transport)

		out, err := client.Call[greetIn, greetOut](
			context.Background(), c, "greet", greetIn{Name: "x"},
		)
		if err == nil {
			t.Fatal("expected error for isError result")
		}
		if !errors.Is(err, client.ErrToolError) {
			t.Errorf("error = %v, want ErrToolError", err)
		}
		if (out != greetOut{}) {
			t.Errorf("out = %+v, want zero value on error", out)
		}
		if !strings.Contains(err.Error(), "tool blew up: bad input") {
			t.Errorf("error %q does not carry the tool error text", err.Error())
		}
	})

	t.Run("structuredContent is the typed channel", func(t *testing.T) {
		tests := []struct {
			name        string
			result      map[string]any
			wantMessage string
			wantCount   int
		}{
			{
				name: "structuredContent only, empty content",
				result: map[string]any{
					"content":           []any{},
					"structuredContent": map[string]any{"message": "from-sc", "count": 9},
				},
				wantMessage: "from-sc",
				wantCount:   9,
			},
			{
				name: "structuredContent preferred over display text",
				result: map[string]any{
					// Display text is intentionally different from the
					// canonical typed channel to prove which one wins.
					"content": []any{
						map[string]any{"type": "text", "text": `{"message":"display-only","count":1}`},
					},
					"structuredContent": map[string]any{"message": "from-sc", "count": 9},
				},
				wantMessage: "from-sc",
				wantCount:   9,
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				transport := &mockTransport{
					responses: []protocol.Response{resultResponse(tt.result)},
				}
				c := client.New(transport)

				out, err := client.Call[greetIn, greetOut](
					context.Background(), c, "greet", greetIn{Name: "x"},
				)
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				if out.Message != tt.wantMessage {
					t.Errorf("message = %q, want %q", out.Message, tt.wantMessage)
				}
				if out.Count != tt.wantCount {
					t.Errorf("count = %d, want %d", out.Count, tt.wantCount)
				}
			})
		}
	})

	t.Run("input marshal edge cases", func(t *testing.T) {
		type nested struct {
			Tags  []string       `json:"tags"`
			Meta  map[string]int `json:"meta"`
			Inner *greetIn       `json:"inner"`
			Empty *greetIn       `json:"empty"`
		}

		tests := []struct {
			name string
			in   nested
		}{
			{
				name: "nested with null pointer",
				in: nested{
					Tags:  []string{"a", "b"},
					Meta:  map[string]int{"x": 1},
					Inner: &greetIn{Name: "deep"},
					Empty: nil,
				},
			},
			{
				name: "empty array and map",
				in: nested{
					Tags: []string{},
					Meta: map[string]int{},
				},
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				transport := &mockTransport{
					responses: []protocol.Response{textResponse(`{"message":"ok","count":1}`)},
				}
				c := client.New(transport)

				_, err := client.Call[nested, greetOut](
					context.Background(), c, "complex", tt.in,
				)
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}

				var params struct {
					Arguments nested `json:"arguments"`
				}
				if err := json.Unmarshal(transport.requests[0].Params, &params); err != nil {
					t.Fatalf("unmarshal params: %v", err)
				}
				if len(params.Arguments.Tags) != len(tt.in.Tags) {
					t.Errorf("tags len = %d, want %d", len(params.Arguments.Tags), len(tt.in.Tags))
				}
				if (params.Arguments.Inner == nil) != (tt.in.Inner == nil) {
					t.Errorf("inner nullability mismatch")
				}
			})
		}
	})
}

func TestNewClientTool(t *testing.T) {
	t.Run("reusable handle called multiple times", func(t *testing.T) {
		transport := &mockTransport{
			responses: []protocol.Response{
				textResponse(`{"message":"first","count":1}`),
				textResponse(`{"message":"second","count":2}`),
			},
		}
		c := client.New(transport)

		greet := client.NewClientTool[greetIn, greetOut](c, "greet")
		if greet.Name() != "greet" {
			t.Errorf("name = %q, want %q", greet.Name(), "greet")
		}

		out1, err := greet.Call(context.Background(), greetIn{Name: "a"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if out1.Message != "first" {
			t.Errorf("out1.Message = %q, want %q", out1.Message, "first")
		}

		out2, err := greet.Call(context.Background(), greetIn{Name: "b"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if out2.Count != 2 {
			t.Errorf("out2.Count = %d, want %d", out2.Count, 2)
		}

		if len(transport.requests) != 2 {
			t.Fatalf("expected 2 requests, got %d", len(transport.requests))
		}
	})

	t.Run("propagates errors", func(t *testing.T) {
		transport := &mockTransport{
			responses: []protocol.Response{
				{
					JSONRPC: "2.0",
					ID:      json.RawMessage(`1`),
					Error:   &protocol.Error{Code: -32601, Message: "tool not found"},
				},
			},
		}
		c := client.New(transport)

		greet := client.NewClientTool[greetIn, greetOut](c, "missing")
		if _, err := greet.Call(context.Background(), greetIn{Name: "x"}); err == nil {
			t.Fatal("expected error")
		}
	})
}

func TestDynamicTool(t *testing.T) {
	t.Run("raw escape hatch round-trips json", func(t *testing.T) {
		transport := &mockTransport{
			responses: []protocol.Response{textResponse(`{"message":"raw","count":7}`)},
		}
		c := client.New(transport)

		tool := client.NewDynamicTool(c, "greet")
		if tool.Name() != "greet" {
			t.Errorf("name = %q, want %q", tool.Name(), "greet")
		}

		raw, err := tool.Call(context.Background(), json.RawMessage(`{"name":"World"}`))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		var out greetOut
		if err := json.Unmarshal(raw, &out); err != nil {
			t.Fatalf("unmarshal raw result: %v", err)
		}
		if out.Message != "raw" || out.Count != 7 {
			t.Errorf("out = %+v, want {raw 7}", out)
		}

		// The raw input must arrive as the tool arguments unchanged.
		var params struct {
			Arguments greetIn `json:"arguments"`
		}
		if err := json.Unmarshal(transport.requests[0].Params, &params); err != nil {
			t.Fatalf("unmarshal params: %v", err)
		}
		if params.Arguments.Name != "World" {
			t.Errorf("argument name = %q, want %q", params.Arguments.Name, "World")
		}
	})

	t.Run("propagates server error", func(t *testing.T) {
		transport := &mockTransport{
			responses: []protocol.Response{
				{
					JSONRPC: "2.0",
					ID:      json.RawMessage(`1`),
					Error:   &protocol.Error{Code: -32000, Message: "boom"},
				},
			},
		}
		c := client.New(transport)

		tool := client.NewDynamicTool(c, "greet")
		if _, err := tool.Call(context.Background(), json.RawMessage(`{}`)); err == nil {
			t.Fatal("expected error")
		}
	})

	t.Run("rejects malformed raw arguments", func(t *testing.T) {
		c := client.New(&mockTransport{})

		tool := client.NewDynamicTool(c, "greet")
		if _, err := tool.Call(context.Background(), json.RawMessage(`{not json`)); err == nil {
			t.Fatal("expected decode error")
		}
	})

	t.Run("nil arguments are allowed", func(t *testing.T) {
		transport := &mockTransport{
			responses: []protocol.Response{textResponse(`{"message":"ok","count":1}`)},
		}
		c := client.New(transport)

		tool := client.NewDynamicTool(c, "greet")
		raw, err := tool.Call(context.Background(), nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(raw) == 0 {
			t.Fatal("expected raw result")
		}
	})

	t.Run("no content returns error", func(t *testing.T) {
		transport := &mockTransport{
			responses: []protocol.Response{
				{
					JSONRPC: "2.0",
					ID:      json.RawMessage(`1`),
					Result:  map[string]any{"content": []any{}},
				},
			},
		}
		c := client.New(transport)

		tool := client.NewDynamicTool(c, "greet")
		_, err := tool.Call(context.Background(), json.RawMessage(`{"name":"x"}`))
		if !errors.Is(err, client.ErrNoToolContent) {
			t.Errorf("error = %v, want ErrNoToolContent", err)
		}
	})

	t.Run("selects first text block past non-text content", func(t *testing.T) {
		transport := &mockTransport{
			responses: []protocol.Response{
				resultResponse(map[string]any{
					"content": []any{
						map[string]any{"type": "image", "data": "deadbeef"},
						map[string]any{"type": "text", "text": `{"message":"raw","count":7}`},
					},
				}),
			},
		}
		c := client.New(transport)

		tool := client.NewDynamicTool(c, "greet")
		raw, err := tool.Call(context.Background(), json.RawMessage(`{"name":"x"}`))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		var out greetOut
		if err := json.Unmarshal(raw, &out); err != nil {
			t.Fatalf("unmarshal raw result: %v", err)
		}
		if out.Message != "raw" || out.Count != 7 {
			t.Errorf("out = %+v, want {raw 7}", out)
		}
	})

	t.Run("non-text content returns ErrNoToolContent", func(t *testing.T) {
		transport := &mockTransport{
			responses: []protocol.Response{
				resultResponse(map[string]any{
					"content": []any{
						map[string]any{"type": "image", "data": "deadbeef"},
					},
				}),
			},
		}
		c := client.New(transport)

		tool := client.NewDynamicTool(c, "greet")
		_, err := tool.Call(context.Background(), json.RawMessage(`{"name":"x"}`))
		if !errors.Is(err, client.ErrNoToolContent) {
			t.Errorf("error = %v, want ErrNoToolContent", err)
		}
	})

	t.Run("isError result surfaces as ErrToolError", func(t *testing.T) {
		transport := &mockTransport{
			responses: []protocol.Response{
				errorResultResponse("dynamic boom"),
			},
		}
		c := client.New(transport)

		tool := client.NewDynamicTool(c, "greet")
		raw, err := tool.Call(context.Background(), json.RawMessage(`{"name":"x"}`))
		if !errors.Is(err, client.ErrToolError) {
			t.Errorf("error = %v, want ErrToolError", err)
		}
		if raw != nil {
			t.Errorf("raw = %s, want nil on error", raw)
		}
		if !strings.Contains(err.Error(), "dynamic boom") {
			t.Errorf("error %q does not carry the tool error text", err.Error())
		}
	})
}
