// Package main demonstrates prompt usage in an MCP server.
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"go.klarlabs.de/mcp"
)

func main() {
	// Create server with prompts capability
	srv := mcp.NewServer(mcp.ServerInfo{
		Name:    "prompts-example",
		Version: "0.1.0",
		Capabilities: mcp.Capabilities{
			Prompts: true,
		},
	})

	// Register a simple greeting prompt
	srv.Prompt("greet").
		Description("Generate a personalized greeting").
		Argument("name", "Name of the person to greet", true).
		Argument("style", "Greeting style: formal or casual", false).
		Handler(func(ctx context.Context, args map[string]string) (*mcp.PromptResult, error) {
			name := args["name"]
			style := args["style"]
			if style == "" {
				style = "casual"
			}

			var greeting string
			if style == "formal" {
				greeting = fmt.Sprintf("Good day, %s. It is a pleasure to meet you.", name)
			} else {
				greeting = fmt.Sprintf("Hey %s! How's it going?", name)
			}

			return &mcp.PromptResult{
				Description: fmt.Sprintf("A %s greeting for %s", style, name),
				Messages: []mcp.PromptMessage{
					{
						Role:    "user",
						Content: mcp.TextContent{Type: "text", Text: greeting},
					},
				},
			}, nil
		})

	// Register a code review prompt
	srv.Prompt("code-review").
		Description("Generate a code review prompt").
		Argument("language", "Programming language", true).
		Argument("focus", "Review focus: security, performance, or style", false).
		Handler(func(ctx context.Context, args map[string]string) (*mcp.PromptResult, error) {
			language := args["language"]
			focus := args["focus"]
			if focus == "" {
				focus = "general"
			}

			prompt := fmt.Sprintf(`Please review the following %s code with a focus on %s.

Provide feedback on:
1. Code quality and readability
2. Potential bugs or issues
3. Suggestions for improvement

Here is the code to review:`, language, focus)

			return &mcp.PromptResult{
				Description: fmt.Sprintf("Code review prompt for %s (%s focus)", language, focus),
				Messages: []mcp.PromptMessage{
					{
						Role:    "user",
						Content: mcp.TextContent{Type: "text", Text: prompt},
					},
				},
			}, nil
		})

	// Register a multi-message prompt for conversation context
	srv.Prompt("explain-concept").
		Description("Generate a teaching prompt for explaining concepts").
		Argument("concept", "The concept to explain", true).
		Argument("level", "Difficulty level: beginner, intermediate, or advanced", false).
		Handler(func(ctx context.Context, args map[string]string) (*mcp.PromptResult, error) {
			concept := args["concept"]
			level := args["level"]
			if level == "" {
				level = "beginner"
			}

			levelDesc := map[string]string{
				"beginner":     "a complete beginner with no prior knowledge",
				"intermediate": "someone with basic understanding who wants to go deeper",
				"advanced":     "an experienced practitioner looking for nuanced insights",
			}

			return &mcp.PromptResult{
				Description: fmt.Sprintf("Explanation of %s for %s level", concept, level),
				Messages: []mcp.PromptMessage{
					{
						Role: "system",
						Content: mcp.TextContent{
							Type: "text",
							Text: fmt.Sprintf("You are an expert teacher who explains concepts clearly to %s.", levelDesc[level]),
						},
					},
					{
						Role: "user",
						Content: mcp.TextContent{
							Type: "text",
							Text: fmt.Sprintf("Please explain %s to me.", strings.ToLower(concept)),
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

	fmt.Println("Starting MCP server with prompts...")
	fmt.Println("Available prompts:")
	fmt.Println("  - greet (name*, style)")
	fmt.Println("  - code-review (language*, focus)")
	fmt.Println("  - explain-concept (concept*, level)")
	fmt.Println("(* = required)")

	if err := mcp.ServeStdio(ctx, srv); err != nil && err != context.Canceled {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}
