// Package main demonstrates HTTP transport usage in an MCP server.
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/felixgeelhaar/mcp-go"
	"github.com/felixgeelhaar/mcp-go/server"
	"github.com/felixgeelhaar/mcp-go/transport"
)

// CalculateInput is the input for the calculate tool.
type CalculateInput struct {
	Operation string  `json:"operation" jsonschema:"required,description=Operation: add, subtract, multiply, divide"`
	A         float64 `json:"a" jsonschema:"required,description=First operand"`
	B         float64 `json:"b" jsonschema:"required,description=Second operand"`
}

func main() {
	// Create server with all capabilities
	srv := mcp.NewServer(mcp.ServerInfo{
		Name:    "http-example",
		Version: "0.1.0",
		Capabilities: mcp.Capabilities{
			Tools:     true,
			Resources: true,
			Prompts:   true,
		},
	})

	// Register a calculator tool
	srv.Tool("calculate").
		Description("Perform arithmetic calculations").
		Handler(func(ctx context.Context, input CalculateInput) (float64, error) {
			switch input.Operation {
			case "add":
				return input.A + input.B, nil
			case "subtract":
				return input.A - input.B, nil
			case "multiply":
				return input.A * input.B, nil
			case "divide":
				if input.B == 0 {
					return 0, fmt.Errorf("division by zero")
				}
				return input.A / input.B, nil
			default:
				return 0, fmt.Errorf("unknown operation: %s", input.Operation)
			}
		})

	// Register a status resource
	srv.Resource("status://server").
		Name("Server Status").
		Description("Get server status information").
		MimeType("application/json").
		Handler(func(ctx context.Context, uri string, params map[string]string) (*mcp.ResourceContent, error) {
			status := fmt.Sprintf(`{"status": "healthy", "uptime": "%s", "version": "0.1.0"}`, time.Now().Format(time.RFC3339))
			return &mcp.ResourceContent{
				URI:      uri,
				MimeType: "application/json",
				Text:     status,
			}, nil
		})

	// Register a help prompt
	srv.Prompt("help").
		Description("Get help with the calculator").
		Handler(func(ctx context.Context, args map[string]string) (*mcp.PromptResult, error) {
			return &mcp.PromptResult{
				Description: "Calculator help information",
				Messages: []mcp.PromptMessage{
					{
						Role: "assistant",
						Content: mcp.TextContent{
							Type: "text",
							Text: `I can help you with calculations!

Available operations:
- add: Add two numbers (a + b)
- subtract: Subtract two numbers (a - b)
- multiply: Multiply two numbers (a × b)
- divide: Divide two numbers (a ÷ b)

Example: {"operation": "add", "a": 5, "b": 3} returns 8`,
						},
					},
				},
			}, nil
		})

	// Handle graceful shutdown
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		<-sigCh
		fmt.Println("\nShutting down...")
		cancel()
	}()

	addr := ":8080"
	fmt.Println("Starting MCP HTTP server...")
	fmt.Printf("Server listening on http://localhost%s\n", addr)
	fmt.Println("Endpoints:")
	fmt.Println("  GET  /.well-known/mcp - Server discovery metadata")
	fmt.Println("  POST /mcp            - JSON-RPC requests")
	fmt.Println("  GET  /mcp/sse        - Server-Sent Events")
	fmt.Println("  GET  /health          - Health check")
	fmt.Println("  GET  /healthz        - Readiness check")

	discovery := transport.NewServerDiscovery(&server.Manifest{
		Name:            srv.Manifest().Name,
		Version:         srv.Manifest().Version,
		ProtocolVersion: srv.Manifest().ProtocolVersion,
		Capabilities:    srv.Manifest().Capabilities,
		Title:           "HTTP Example",
		Description:     "Calculator server with HTTP transport",
		WebsiteURL:      "https://example.com",
	}, transport.WithDiscoveryEndpoints(transport.ServerEndpoint{
		StreamableHTTP: fmt.Sprintf("http://localhost%s/mcp", addr),
		SSE:            fmt.Sprintf("http://localhost%s/mcp/sse", addr),
	}))

	if err := mcp.ServeHTTP(ctx, srv, addr,
		mcp.WithReadTimeout(30*time.Second),
		mcp.WithWriteTimeout(30*time.Second),
		mcp.WithDiscovery(discovery),
	); err != nil && err != context.Canceled {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}
