// Package mcp provides benchmarks for key operations.
package mcp_test

import (
	"context"
	"encoding/json"
	"testing"

	"go.klarlabs.de/mcp"
	"go.klarlabs.de/mcp/middleware"
	"go.klarlabs.de/mcp/protocol"
)

// BenchmarkToolExecution measures tool execution performance.
func BenchmarkToolExecution(b *testing.B) {
	type AddInput struct {
		A int `json:"a"`
		B int `json:"b"`
	}

	srv := mcp.NewServer(mcp.ServerInfo{
		Name:    "benchmark-test",
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

	tool, _ := srv.GetTool("add")
	input := json.RawMessage(`{"a":2,"b":3}`)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := tool.Execute(context.Background(), input)
		if err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkToolExecution_WithContext measures tool execution with context parameter.
func BenchmarkToolExecution_WithContext(b *testing.B) {
	type AddInput struct {
		A int `json:"a"`
		B int `json:"b"`
	}

	srv := mcp.NewServer(mcp.ServerInfo{
		Name:    "benchmark-test",
		Version: "1.0.0",
		Capabilities: mcp.Capabilities{
			Tools: true,
		},
	})

	srv.Tool("add").
		Description("Add two numbers").
		Handler(func(ctx context.Context, input AddInput) (int, error) {
			return input.A + input.B, nil
		})

	tool, _ := srv.GetTool("add")
	input := json.RawMessage(`{"a":2,"b":3}`)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := tool.Execute(context.Background(), input)
		if err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkMiddlewareChain measures middleware chain overhead.
func BenchmarkMiddlewareChain(b *testing.B) {
	baseHandler := func(ctx context.Context, req *protocol.Request) (*protocol.Response, error) {
		return protocol.NewResponse(req.ID, map[string]any{"status": "ok"}), nil
	}

	b.Run("no_middleware", func(b *testing.B) {
		req := &protocol.Request{
			JSONRPC: "2.0",
			ID:      json.RawMessage(`1`),
			Method:  "test",
		}

		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_, err := baseHandler(context.Background(), req)
			if err != nil {
				b.Fatal(err)
			}
		}
	})

	b.Run("single_middleware", func(b *testing.B) {
		chain := middleware.Chain(middleware.RequestID())
		handler := chain(baseHandler)

		req := &protocol.Request{
			JSONRPC: "2.0",
			ID:      json.RawMessage(`1`),
			Method:  "test",
		}

		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_, err := handler(context.Background(), req)
			if err != nil {
				b.Fatal(err)
			}
		}
	})

	b.Run("default_stack", func(b *testing.B) {
		stack := middleware.DefaultStack(middleware.NopLogger{})
		chain := middleware.Chain(stack...)
		handler := chain(baseHandler)

		req := &protocol.Request{
			JSONRPC: "2.0",
			ID:      json.RawMessage(`1`),
			Method:  "test",
		}

		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_, err := handler(context.Background(), req)
			if err != nil {
				b.Fatal(err)
			}
		}
	})
}

// BenchmarkJSONParsing measures JSON marshaling/unmarshaling performance.
func BenchmarkJSONParsing(b *testing.B) {
	b.Run("request_unmarshal", func(b *testing.B) {
		data := []byte(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"add","arguments":{"a":2,"b":3}}}`)

		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			var req protocol.Request
			if err := json.Unmarshal(data, &req); err != nil {
				b.Fatal(err)
			}
		}
	})

	b.Run("response_marshal", func(b *testing.B) {
		resp := protocol.NewResponse(json.RawMessage(`1`), map[string]any{
			"content": []map[string]any{
				{"type": "text", "text": "Hello, World!"},
			},
		})

		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_, err := json.Marshal(resp)
			if err != nil {
				b.Fatal(err)
			}
		}
	})
}

// BenchmarkSchemaGeneration measures JSON schema generation.
func BenchmarkSchemaGeneration(b *testing.B) {
	// Each benchmark creates a new server and tool to measure registration time
	type ComplexInput struct {
		Name        string   `json:"name" jsonschema:"required"`
		Count       int      `json:"count"`
		Tags        []string `json:"tags"`
		Description string   `json:"description,omitempty" jsonschema:"description=Optional description"`
	}

	b.Run("simple_struct", func(b *testing.B) {
		type SimpleInput struct {
			A int `json:"a"`
			B int `json:"b"`
		}

		for i := 0; i < b.N; i++ {
			srv := mcp.NewServer(mcp.ServerInfo{
				Name:    "benchmark",
				Version: "1.0.0",
				Capabilities: mcp.Capabilities{
					Tools: true,
				},
			})
			srv.Tool("test").
				Handler(func(input SimpleInput) (int, error) {
					return 0, nil
				})
		}
	})

	b.Run("complex_struct", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			srv := mcp.NewServer(mcp.ServerInfo{
				Name:    "benchmark",
				Version: "1.0.0",
				Capabilities: mcp.Capabilities{
					Tools: true,
				},
			})
			srv.Tool("test").
				Handler(func(input ComplexInput) (int, error) {
					return 0, nil
				})
		}
	})
}

// BenchmarkResourceRead measures resource read performance.
func BenchmarkResourceRead(b *testing.B) {
	srv := mcp.NewServer(mcp.ServerInfo{
		Name:    "benchmark-test",
		Version: "1.0.0",
		Capabilities: mcp.Capabilities{
			Resources: true,
		},
	})

	srv.Resource("file://{path}").
		Name("File").
		Description("Read file content").
		Handler(func(ctx context.Context, uri string, params map[string]string) (*mcp.ResourceContent, error) {
			return &mcp.ResourceContent{
				URI:      uri,
				MimeType: "text/plain",
				Text:     "Hello, World!",
			}, nil
		})

	resource, _ := srv.FindResourceForURI("file://test.txt")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := resource.Read(context.Background(), "file://test.txt")
		if err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkPromptGet measures prompt execution performance.
func BenchmarkPromptGet(b *testing.B) {
	srv := mcp.NewServer(mcp.ServerInfo{
		Name:    "benchmark-test",
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
					{Role: "user", Content: mcp.TextContent{Type: "text", Text: "Hello, " + args["name"]}},
				},
			}, nil
		})

	prompt, _ := srv.GetPrompt("greet")
	args := map[string]string{"name": "World"}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := prompt.Get(context.Background(), args)
		if err != nil {
			b.Fatal(err)
		}
	}
}
