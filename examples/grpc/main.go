// Package main provides an example of an MCP server using gRPC transport.
//
// This example demonstrates how to use gRPC as the transport layer for MCP,
// enabling binary encoding, built-in flow control, and native integration
// with enterprise gRPC infrastructure.
//
// Run the server:
//
//	go run ./examples/grpc
//
// Connect using the client example or grpcurl:
//
//	grpcurl -plaintext localhost:50051 mcp.v1.MCP/Connect
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"go.klarlabs.de/mcp"
)

// SearchInput is the input for the search tool.
type SearchInput struct {
	Query string `json:"query" jsonschema:"required,description=Search query string"`
	Limit int    `json:"limit" jsonschema:"description=Maximum results to return"`
}

// SearchResult is a single search result.
type SearchResult struct {
	Title string `json:"title"`
	URL   string `json:"url"`
}

func main() {
	// Create server with tools capability
	srv := mcp.NewServer(mcp.ServerInfo{
		Name:    "grpc-example-server",
		Version: "1.0.0",
		Capabilities: mcp.Capabilities{
			Tools:     true,
			Resources: true,
		},
	})

	// Register tools
	srv.Tool("search").
		Description("Search for items").
		Handler(searchHandler)

	srv.Tool("echo").
		Description("Echo the input back").
		Handler(echoHandler)

	srv.Tool("slow_operation").
		Description("A slow operation that reports progress").
		Handler(slowOperationHandler)

	// Register resources
	srv.Resource("status://health").
		Name("Health Status").
		Description("Get server health status").
		MimeType("application/json").
		Handler(healthResourceHandler)

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

	addr := ":50051"
	fmt.Printf("Starting gRPC MCP server on %s\n", addr)
	fmt.Println("Press Ctrl+C to stop")

	// Run server with gRPC transport
	if err := mcp.ServeGRPC(ctx, srv, addr,
		mcp.WithGRPCShutdownTimeout(10*time.Second),
		mcp.WithGRPCDrainDelay(2*time.Second),
	); err != nil && err != context.Canceled {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("Server stopped")
}

func searchHandler(input SearchInput) ([]SearchResult, error) {
	limit := input.Limit
	if limit <= 0 {
		limit = 10
	}

	results := []SearchResult{
		{Title: fmt.Sprintf("Result for: %s", input.Query), URL: "https://example.com/1"},
		{Title: "Another result", URL: "https://example.com/2"},
		{Title: "Third result", URL: "https://example.com/3"},
	}

	if len(results) > limit {
		results = results[:limit]
	}

	return results, nil
}

type EchoInput struct {
	Message string `json:"message" jsonschema:"required"`
}

func echoHandler(input EchoInput) (string, error) {
	return input.Message, nil
}

type SlowInput struct {
	Duration int `json:"duration" jsonschema:"description=Duration in seconds (default 5)"`
}

func slowOperationHandler(ctx context.Context, input SlowInput) (string, error) {
	duration := input.Duration
	if duration <= 0 {
		duration = 5
	}

	// Get progress reporter from context
	progress := mcp.ProgressFromContext(ctx)
	total := float64(duration)

	for i := 0; i < duration; i++ {
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(time.Second):
			// Report progress if available
			if progress != nil {
				progress.Report(float64(i+1), &total)
			}
		}
	}

	return fmt.Sprintf("Completed after %d seconds", duration), nil
}

func healthResourceHandler(ctx context.Context, uri string, params map[string]string) (*mcp.ResourceContent, error) {
	return &mcp.ResourceContent{
		URI:      uri,
		MimeType: "application/json",
		Text:     `{"status": "healthy", "transport": "grpc", "uptime": "running"}`,
	}, nil
}
