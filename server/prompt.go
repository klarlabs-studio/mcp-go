package server

import (
	"context"
	"fmt"
)

// TextContent represents text content in a prompt message.
//
// Prefer ContentBlock (via NewTextContent) in new code: ContentBlock is the
// single canonical content-block union across tools, prompts, and sampling.
// This standalone type is retained for backward compatibility and serializes
// identically.
type TextContent struct {
	Type string `json:"type"` // Always "text"
	Text string `json:"text"`
}

// ImageContent represents image content in a prompt message.
//
// Prefer ContentBlock (via NewImageContent) in new code. See TextContent.
type ImageContent struct {
	Type     string `json:"type"` // Always "image"
	Data     string `json:"data"` // Base64 encoded
	MimeType string `json:"mimeType"`
}

// PromptMessage represents a message in a prompt result.
type PromptMessage struct {
	Role    string `json:"role"` // "user" or "assistant"
	Content any    `json:"content"`
}

// PromptResult is the result of getting a prompt.
type PromptResult struct {
	Description string          `json:"description,omitempty"`
	Messages    []PromptMessage `json:"messages"`
}

// PromptArgument describes an argument for a prompt.
type PromptArgument struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Required    bool   `json:"required,omitempty"`
}

// PromptHandler is the function signature for prompt handlers.
type PromptHandler func(ctx context.Context, args map[string]string) (*PromptResult, error)

// Prompt represents a prompt template exposed via MCP.
type Prompt struct {
	name        string
	title       string
	description string
	arguments   []PromptArgument
	handler     PromptHandler
	annotations *PromptAnnotations
	icons       []Icon
}

// Icons returns the prompt's icons, used for the icons field in prompts/list.
// Returns nil when no icons were set.
func (p *Prompt) Icons() []Icon { return p.icons }

// PromptInfo represents metadata about a registered prompt.
type PromptInfo struct {
	Name        string
	Title       string
	Description string
	Arguments   []PromptArgument
	Annotations *PromptAnnotations
	Icons       []Icon
}

// PromptBuilder provides a fluent API for building prompts.
type PromptBuilder struct {
	prompt *Prompt
	server *Server
	err    error
}

// Description sets the prompt description.
func (b *PromptBuilder) Description(desc string) *PromptBuilder {
	if b.err != nil {
		return b
	}
	b.prompt.description = desc
	return b
}

// Title sets a human-readable display title, advertised as the top-level
// `title` field (MCP 2025-06-18). `name` remains the programmatic identifier.
func (b *PromptBuilder) Title(title string) *PromptBuilder {
	if b.err != nil {
		return b
	}
	b.prompt.title = title
	return b
}

// Argument adds an argument to the prompt.
func (b *PromptBuilder) Argument(name, description string, required bool) *PromptBuilder {
	if b.err != nil {
		return b
	}
	b.prompt.arguments = append(b.prompt.arguments, PromptArgument{
		Name:        name,
		Description: description,
		Required:    required,
	})
	return b
}

// Icons sets optional icons advertised for this prompt in prompts/list, per the
// MCP 2025-11-25 spec (SEP-973). Icons are for UI display and are purely
// informational metadata.
func (b *PromptBuilder) Icons(icons ...Icon) *PromptBuilder {
	if b.err != nil {
		return b
	}
	b.prompt.icons = icons
	return b
}

// Handler sets the prompt handler function.
func (b *PromptBuilder) Handler(fn PromptHandler) *PromptBuilder {
	if b.err != nil {
		return b
	}

	b.prompt.handler = fn
	b.server.registerPrompt(b.prompt)
	return b
}

// Get executes the prompt handler with the given arguments.
func (p *Prompt) Get(ctx context.Context, args map[string]string) (*PromptResult, error) {
	// Validate required arguments
	for _, arg := range p.arguments {
		if arg.Required {
			if args == nil || args[arg.Name] == "" {
				return nil, fmt.Errorf("missing required argument: %s", arg.Name)
			}
		}
	}

	return p.handler(ctx, args)
}
