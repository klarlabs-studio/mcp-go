package mcp_test

import (
	"context"
	"fmt"

	"github.com/felixgeelhaar/mcp-go"
)

// ExampleNewServer demonstrates creating an MCP server with tools, resources, and prompts.
func Example() {
	// Create server with capabilities and instructions
	srv := mcp.NewServer(mcp.ServerInfo{
		Name:    "example-server",
		Version: "1.0.0",
		Capabilities: mcp.Capabilities{
			Tools:     true,
			Resources: true,
			Prompts:   true,
		},
	}, mcp.WithInstructions("Use search to find documents."))

	// Register a typed tool
	type SearchInput struct {
		Query string `json:"query" jsonschema:"required"`
		Limit int    `json:"limit" jsonschema:"maximum=100"`
	}

	srv.Tool("search").
		Description("Search for documents").
		Handler(func(ctx context.Context, input SearchInput) ([]string, error) {
			return []string{"result1", "result2"}, nil
		})

	// Register a resource with URI template
	srv.Resource("users://{id}").
		Name("User").
		MimeType("application/json").
		Handler(func(ctx context.Context, uri string, params map[string]string) (*mcp.ResourceContent, error) {
			id := params["id"] // extracted from template
			return &mcp.ResourceContent{
				URI:      uri,
				MimeType: "application/json",
				Text:     fmt.Sprintf(`{"id": "%s"}`, id),
			}, nil
		})

	// Register a prompt
	srv.Prompt("greet").
		Description("Generate a greeting").
		Argument("name", "Name to greet", true).
		Handler(func(ctx context.Context, args map[string]string) (*mcp.PromptResult, error) {
			return &mcp.PromptResult{
				Messages: []mcp.PromptMessage{
					{
						Role:    "user",
						Content: mcp.TextContent{Type: "text", Text: "Hello, " + args["name"]},
					},
				},
			}, nil
		})

	fmt.Println("Server created with tools, resources, and prompts")
	// Output: Server created with tools, resources, and prompts
}

// ExampleProgressFromContext demonstrates progress reporting in tool handlers.
func ExampleProgressFromContext() {
	srv := mcp.NewServer(mcp.ServerInfo{Name: "server", Version: "1.0.0"})

	type ProcessInput struct {
		Items int `json:"items"`
	}

	srv.Tool("process").Handler(func(ctx context.Context, input ProcessInput) (string, error) {
		progress := mcp.ProgressFromContext(ctx)
		total := float64(input.Items)

		for i := 0; i < input.Items; i++ {
			progress.Report(float64(i), &total) // error typically ignored
			// do work...
		}

		return "done", nil
	})

	fmt.Println("Tool with progress reporting registered")
	// Output: Tool with progress reporting registered
}

// ExampleDefaultMiddlewareWithTimeout shows using the production middleware stack.
func ExampleDefaultMiddlewareWithTimeout() {
	srv := mcp.NewServer(mcp.ServerInfo{Name: "server", Version: "1.0.0"})

	// Create a logger (implement mcp.Logger interface)
	var logger mcp.Logger // = yourLogger

	// Use default production middleware: recover, request ID, timeout, logging
	_ = logger
	_ = srv
	// mcp.ServeStdio(ctx, srv, mcp.WithMiddleware(
	//     mcp.DefaultMiddlewareWithTimeout(logger, 30*time.Second)...,
	// ))

	fmt.Println("Server configured with default middleware")
	// Output: Server configured with default middleware
}
