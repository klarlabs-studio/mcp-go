// Package main demonstrates resource usage in an MCP server.
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"go.klarlabs.de/mcp"
)

func main() {
	// Create server with resources capability
	srv := mcp.NewServer(mcp.ServerInfo{
		Name:    "resources-example",
		Version: "0.1.0",
		Capabilities: mcp.Capabilities{
			Resources: true,
		},
	})

	// Register a static resource
	srv.Resource("config://settings").
		Name("Settings").
		Description("Application settings").
		MimeType("application/json").
		Handler(func(ctx context.Context, uri string, params map[string]string) (*mcp.ResourceContent, error) {
			return &mcp.ResourceContent{
				URI:      uri,
				MimeType: "application/json",
				Text:     `{"theme": "dark", "language": "en"}`,
			}, nil
		})

	// Register a parameterized resource with URI template
	srv.Resource("file://{path}").
		Name("File").
		Description("Read file content from the filesystem").
		MimeType("text/plain").
		Handler(func(ctx context.Context, uri string, params map[string]string) (*mcp.ResourceContent, error) {
			path := params["path"]
			if path == "" {
				return nil, fmt.Errorf("path parameter is required")
			}

			// In a real implementation, you would read the file
			// For this example, we return mock content
			content := fmt.Sprintf("Content of file: %s\n(This is simulated content)", path)

			return &mcp.ResourceContent{
				URI:      uri,
				MimeType: "text/plain",
				Text:     content,
			}, nil
		})

	// Register a database resource
	srv.Resource("db://users/{id}").
		Name("User").
		Description("Get user by ID from database").
		MimeType("application/json").
		Handler(func(ctx context.Context, uri string, params map[string]string) (*mcp.ResourceContent, error) {
			id := params["id"]
			if id == "" {
				return nil, fmt.Errorf("id parameter is required")
			}

			// Mock user data
			user := fmt.Sprintf(`{"id": "%s", "name": "User %s", "email": "user%s@example.com"}`, id, id, id)

			return &mcp.ResourceContent{
				URI:      uri,
				MimeType: "application/json",
				Text:     user,
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

	fmt.Println("Starting MCP server with resources...")
	fmt.Println("Available resources:")
	fmt.Println("  - config://settings")
	fmt.Println("  - file://{path}")
	fmt.Println("  - db://users/{id}")

	if err := mcp.ServeStdio(ctx, srv); err != nil && err != context.Canceled {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}
