package client

import (
	"context"
	"encoding/json"
	"fmt"
)

// Call invokes a tool on the server with a typed input and decodes the typed
// output. The input is marshaled to the tool arguments, the tool is invoked
// over the standard CallTool path, and the first text content block of the
// result is decoded into Out.
//
// When Out is a string, the raw text of the first content block is returned
// unchanged; otherwise the text is treated as JSON and unmarshaled into Out.
// This mirrors how the server serializes typed tool results.
//
// Call is the recommended primary API for invoking tools. For a reusable,
// pre-bound handle, see NewClientTool.
//
// Example:
//
//	type GreetIn struct{ Name string `json:"name"` }
//	type GreetOut struct{ Message string `json:"message"` }
//
//	out, err := client.Call[GreetIn, GreetOut](ctx, c, "greet", GreetIn{Name: "World"})
//	if err != nil {
//	    return err
//	}
//	fmt.Println(out.Message)
func Call[In, Out any](ctx context.Context, c *Client, name string, in In) (Out, error) {
	var out Out

	result, err := c.CallTool(ctx, name, in)
	if err != nil {
		return out, err
	}

	if result.IsError {
		return out, fmt.Errorf("call tool %q: %w: %s", name, ErrToolError, toolErrorText(result))
	}

	if len(result.Content) == 0 {
		return out, fmt.Errorf("call tool %q: %w", name, ErrNoContent)
	}

	text := result.Content[0].Text

	// A string output receives the raw text directly so callers can opt out
	// of JSON decoding for plain-text tools.
	if s, ok := any(&out).(*string); ok {
		*s = text
		return out, nil
	}

	if err := json.Unmarshal([]byte(text), &out); err != nil {
		return out, fmt.Errorf("call tool %q: decode result: %w", name, err)
	}

	return out, nil
}

// ClientTool is a reusable, typed handle bound to a single tool on a client.
// Create one with NewClientTool and invoke it repeatedly via Call.
type ClientTool[In, Out any] struct {
	client *Client
	name   string
}

// NewClientTool returns a reusable typed handle for the named tool. Each Call
// on the handle delegates to the package-level Call, so the handle is safe to
// reuse for the lifetime of the client.
//
// Example:
//
//	greet := client.NewClientTool[GreetIn, GreetOut](c, "greet")
//	out, err := greet.Call(ctx, GreetIn{Name: "World"})
func NewClientTool[In, Out any](c *Client, name string) *ClientTool[In, Out] {
	return &ClientTool[In, Out]{client: c, name: name}
}

// Name returns the tool name this handle is bound to.
func (t *ClientTool[In, Out]) Name() string {
	return t.name
}

// Call invokes the bound tool with the typed input and returns the typed
// output. It delegates to the package-level Call.
func (t *ClientTool[In, Out]) Call(ctx context.Context, in In) (Out, error) {
	return Call[In, Out](ctx, t.client, t.name, in)
}

// DynamicTool is a dynamically typed escape hatch for invoking a tool with raw
// JSON arguments and receiving the raw JSON result.
//
// Prefer the typed Call and NewClientTool APIs. DynamicTool is provided only
// for cases where the input and output shapes are not known at compile time
// (for example, proxying tool calls). It forgoes the compile-time guarantees
// that make the typed API the recommended choice.
type DynamicTool interface {
	// Name returns the tool name.
	Name() string
	// Call invokes the tool with raw JSON arguments and returns the first
	// text content block of the result as raw JSON.
	Call(ctx context.Context, in json.RawMessage) (json.RawMessage, error)
}

// dynamicTool is the default DynamicTool implementation backed by a Client.
type dynamicTool struct {
	client *Client
	name   string
}

// NewDynamicTool returns a DynamicTool for the named tool.
//
// This is the dynamically typed escape hatch and is not recommended for
// general use; prefer Call or NewClientTool.
func NewDynamicTool(c *Client, name string) DynamicTool {
	return &dynamicTool{client: c, name: name}
}

// Name returns the tool name this handle is bound to.
func (t *dynamicTool) Name() string {
	return t.name
}

// Call invokes the tool with raw JSON arguments. The arguments are decoded so
// they reach the server as a structured object, and the first text content
// block of the result is returned as raw JSON.
func (t *dynamicTool) Call(ctx context.Context, in json.RawMessage) (json.RawMessage, error) {
	var args any
	if len(in) > 0 {
		if err := json.Unmarshal(in, &args); err != nil {
			return nil, fmt.Errorf("call tool %q: decode arguments: %w", t.name, err)
		}
	}

	result, err := t.client.CallTool(ctx, t.name, args)
	if err != nil {
		return nil, err
	}

	if result.IsError {
		return nil, fmt.Errorf("call tool %q: %w: %s", t.name, ErrToolError, toolErrorText(result))
	}

	if len(result.Content) == 0 {
		return nil, fmt.Errorf("call tool %q: %w", t.name, ErrNoContent)
	}

	return json.RawMessage(result.Content[0].Text), nil
}

// toolErrorText extracts a human-readable error message from a tool result
// flagged with isError. It returns the first text content block, falling back
// to a generic phrase when the server provided no text.
func toolErrorText(result *ToolResult) string {
	for _, item := range result.Content {
		if item.Type == "text" && item.Text != "" {
			return item.Text
		}
	}
	return "no error detail provided"
}
