// Package main provides a basic example of an MCP server.
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

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
	// Create server with all capabilities
	srv := mcp.NewServer(mcp.ServerInfo{
		Name:    "example-server",
		Version: "0.1.0",
		Capabilities: mcp.Capabilities{
			Tools:     true,
			Resources: true,
			Prompts:   true,
		},
	})

	// Register tools
	srv.Tool("search").
		Description("Search for items").
		Handler(searchHandler)

	srv.Tool("echo").
		Description("Echo the input back").
		Handler(echoHandler)

	// Register resources
	srv.Resource("users://{id}").
		Name("User Profile").
		Description("Get user profile by ID").
		MimeType("application/json").
		Handler(userResourceHandler)

	srv.Resource("config://settings").
		Name("Application Settings").
		Description("Get application configuration").
		MimeType("application/json").
		Handler(configResourceHandler)

	// Register prompts
	srv.Prompt("greet").
		Description("Generate a greeting message").
		Argument("name", "Name of the person to greet", true).
		Argument("style", "Style of greeting (formal/casual)", false).
		Handler(greetPromptHandler)

	srv.Prompt("summarize").
		Description("Generate a summarization prompt").
		Argument("topic", "Topic to summarize", true).
		Handler(summarizePromptHandler)

	// Handle graceful shutdown
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		<-sigCh
		cancel()
	}()

	// Run server
	if err := mcp.ServeStdio(ctx, srv); err != nil && err != context.Canceled {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func searchHandler(input SearchInput) ([]SearchResult, error) {
	// Simulated search results
	limit := input.Limit
	if limit <= 0 {
		limit = 10
	}

	results := []SearchResult{
		{Title: fmt.Sprintf("Result for: %s", input.Query), URL: "https://example.com/1"},
		{Title: "Another result", URL: "https://example.com/2"},
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

func userResourceHandler(ctx context.Context, uri string, params map[string]string) (*mcp.ResourceContent, error) {
	userID := params["id"]
	content := fmt.Sprintf(`{"id": "%s", "name": "User %s", "email": "user%s@example.com"}`, userID, userID, userID)

	return &mcp.ResourceContent{
		URI:      uri,
		MimeType: "application/json",
		Text:     content,
	}, nil
}

func configResourceHandler(ctx context.Context, uri string, params map[string]string) (*mcp.ResourceContent, error) {
	return &mcp.ResourceContent{
		URI:      uri,
		MimeType: "application/json",
		Text:     `{"theme": "dark", "language": "en", "notifications": true}`,
	}, nil
}

func greetPromptHandler(ctx context.Context, args map[string]string) (*mcp.PromptResult, error) {
	name := args["name"]
	style := args["style"]
	if style == "" {
		style = "casual"
	}

	var greeting string
	if style == "formal" {
		greeting = fmt.Sprintf("Dear %s, I hope this message finds you well.", name)
	} else {
		greeting = fmt.Sprintf("Hey %s! How's it going?", name)
	}

	return &mcp.PromptResult{
		Messages: []mcp.PromptMessage{
			{
				Role: "user",
				Content: mcp.TextContent{
					Type: "text",
					Text: greeting,
				},
			},
		},
	}, nil
}

func summarizePromptHandler(ctx context.Context, args map[string]string) (*mcp.PromptResult, error) {
	topic := args["topic"]

	return &mcp.PromptResult{
		Description: "A prompt for summarizing content",
		Messages: []mcp.PromptMessage{
			{
				Role: "user",
				Content: mcp.TextContent{
					Type: "text",
					Text: fmt.Sprintf("Please provide a concise summary of the following topic: %s", topic),
				},
			},
		},
	}, nil
}
