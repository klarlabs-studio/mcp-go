// Package main demonstrates middleware usage in an MCP server.
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

// simpleLogger implements mcp.Logger for demonstration.
type simpleLogger struct{}

func (l *simpleLogger) Info(msg string, fields ...mcp.LogField) {
	fmt.Printf("[INFO] %s %v\n", msg, formatFields(fields))
}

func (l *simpleLogger) Error(msg string, fields ...mcp.LogField) {
	fmt.Printf("[ERROR] %s %v\n", msg, formatFields(fields))
}

func (l *simpleLogger) Debug(msg string, fields ...mcp.LogField) {
	fmt.Printf("[DEBUG] %s %v\n", msg, formatFields(fields))
}

func (l *simpleLogger) Warn(msg string, fields ...mcp.LogField) {
	fmt.Printf("[WARN] %s %v\n", msg, formatFields(fields))
}

func formatFields(fields []mcp.LogField) string {
	if len(fields) == 0 {
		return ""
	}
	result := "{"
	for i, f := range fields {
		if i > 0 {
			result += ", "
		}
		result += fmt.Sprintf("%s=%v", f.Key, f.Value)
	}
	return result + "}"
}

// EchoInput is the input for the echo tool.
type EchoInput struct {
	Message string `json:"message" jsonschema:"required"`
}

// SlowInput is the input for the slow tool.
type SlowInput struct {
	Delay int `json:"delay" jsonschema:"description=Delay in milliseconds"`
}

// PanicInput is the input for the panic tool (for testing recovery).
type PanicInput struct {
	Message string `json:"message"`
}

func main() {
	logger := &simpleLogger{}

	// Create server
	srv := mcp.NewServer(mcp.ServerInfo{
		Name:    "middleware-example",
		Version: "0.1.0",
		Capabilities: mcp.Capabilities{
			Tools: true,
		},
	})

	// Register tools
	srv.Tool("echo").
		Description("Echo the input back").
		Handler(func(input EchoInput) (string, error) {
			return input.Message, nil
		})

	srv.Tool("slow").
		Description("A slow tool for testing timeouts").
		Handler(func(ctx context.Context, input SlowInput) (string, error) {
			delay := time.Duration(input.Delay) * time.Millisecond
			select {
			case <-time.After(delay):
				return fmt.Sprintf("Completed after %v", delay), nil
			case <-ctx.Done():
				return "", ctx.Err()
			}
		})

	srv.Tool("panic").
		Description("A tool that panics (for testing recovery)").
		Handler(func(input PanicInput) (string, error) {
			panic("intentional panic: " + input.Message)
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

	// Run server with middleware
	// The middleware chain provides:
	// - Panic recovery (prevents crashes)
	// - Request ID injection (for tracing)
	// - Request timeout (prevents hung requests)
	// - Logging (observability)
	middleware := mcp.DefaultMiddlewareWithTimeout(logger, 30*time.Second)

	fmt.Println("Starting MCP server with middleware...")
	fmt.Println("Middleware stack: Recover -> RequestID -> Timeout -> Logging")

	if err := mcp.ServeStdio(ctx, srv, mcp.WithMiddleware(middleware...)); err != nil && err != context.Canceled {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}
