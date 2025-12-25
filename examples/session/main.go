// Package main demonstrates v1.1 session features: sampling, roots, and logging.
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/felixgeelhaar/mcp-go"
)

// AnalyzeInput is the input for the analyze tool.
type AnalyzeInput struct {
	Topic string `json:"topic" jsonschema:"required,description=Topic to analyze"`
}

func main() {
	srv := mcp.NewServer(mcp.ServerInfo{
		Name:    "session-example",
		Version: "1.0.0",
		Capabilities: mcp.Capabilities{
			Tools: true,
		},
	})

	// Register a tool that uses session features
	srv.Tool("analyze").
		Description("Analyze a topic using client LLM (sampling)").
		Handler(func(ctx context.Context, input AnalyzeInput) (string, error) {
			// Get session from context
			session := mcp.SessionFromContext(ctx)
			if session == nil {
				return "", fmt.Errorf("no session available")
			}

			// Log progress
			session.Info("analyze", fmt.Sprintf("Starting analysis of: %s", input.Topic))

			// Check if client supports sampling
			if !session.SupportsFeature("sampling") {
				session.Warning("analyze", "Client doesn't support sampling, returning static response")
				return fmt.Sprintf("Analysis of '%s': (sampling not available)", input.Topic), nil
			}

			// Request LLM completion from client
			result, err := session.CreateMessage(ctx, &mcp.CreateMessageRequest{
				Messages: []mcp.SamplingMessage{
					{
						Role:    mcp.RoleUser,
						Content: mcp.NewTextContent(fmt.Sprintf("Provide a brief analysis of: %s", input.Topic)),
					},
				},
				MaxTokens: 200,
			})
			if err != nil {
				session.Error("analyze", fmt.Sprintf("Sampling failed: %v", err))
				return "", fmt.Errorf("sampling failed: %w", err)
			}

			session.Info("analyze", "Analysis complete")
			return result.Content.Text, nil
		})

	// Register a tool that lists client roots
	srv.Tool("list-workspace").
		Description("List the client's workspace roots").
		Handler(func(ctx context.Context, _ struct{}) ([]mcp.Root, error) {
			session := mcp.SessionFromContext(ctx)
			if session == nil {
				return nil, fmt.Errorf("no session available")
			}

			if !session.SupportsFeature("roots") {
				return nil, fmt.Errorf("client doesn't support roots")
			}

			result, err := session.ListRoots(ctx)
			if err != nil {
				return nil, err
			}

			return result.Roots, nil
		})

	// Handle graceful shutdown
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		<-sigCh
		cancel()
	}()

	fmt.Fprintln(os.Stderr, "Starting MCP server with session features...")
	fmt.Fprintln(os.Stderr, "Features: sampling, roots, logging")

	if err := mcp.ServeStdio(ctx, srv); err != nil && err != context.Canceled {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}
