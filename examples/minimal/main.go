// Package main provides a minimal MCP server example.
// This is the simplest possible MCP server with a single tool.
package main

import (
	"context"
	"os"

	"github.com/felixgeelhaar/mcp-go"
)

// EchoInput is the input for the echo tool.
type EchoInput struct {
	Message string `json:"message" jsonschema:"required,description=Message to echo"`
}

func main() {
	srv := mcp.NewServer(mcp.ServerInfo{
		Name:    "minimal",
		Version: "1.0.0",
	})

	srv.Tool("echo").
		Description("Echo the input back").
		Handler(func(input EchoInput) (string, error) {
			return input.Message, nil
		})

	ctx := context.Background()
	if err := mcp.ServeStdio(ctx, srv); err != nil {
		os.Exit(1)
	}
}
