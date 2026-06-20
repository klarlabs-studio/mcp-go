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
// pre-bound handle, see NewTypedTool.
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
		// Add a frame for the typed-call layer so the error path is as
		// self-describing as the decode path below; %w keeps the wrapped
		// transport/server error inspectable via errors.As/errors.Is.
		return out, fmt.Errorf("typed call tool %q: %w", name, err)
	}

	if result.IsError {
		return out, fmt.Errorf("call tool %q: %w: %s", name, ErrToolError, toolErrorText(result))
	}

	// Prefer the canonical typed channel: when the server emits
	// structuredContent, decode that rather than the display text. This also
	// covers results that carry structuredContent with empty Content.
	if len(result.StructuredContent) > 0 {
		if err := json.Unmarshal(result.StructuredContent, &out); err != nil {
			return out, fmt.Errorf("call tool %q: decode structuredContent: %w", name, err)
		}
		return out, nil
	}

	// Select the first text content block rather than blindly taking
	// Content[0], which may be an image or other non-text block.
	text, ok := firstTextContent(result)
	if !ok {
		return out, fmt.Errorf("call tool %q: %w", name, ErrNoToolContent)
	}

	// A string output receives the raw text directly so callers can opt out
	// of JSON decoding for plain-text tools.
	//
	// WARNING: when the tool serialized a JSON value into the text block (the
	// usual case for typed handlers), that block is a JSON-encoded string with
	// surrounding quotes. Because the raw text is returned unchanged here, the
	// quotes are included verbatim rather than JSON-decoded. Use a struct (or
	// json.RawMessage) Out if you need decoded values.
	if s, ok := any(&out).(*string); ok {
		*s = text
		return out, nil
	}

	if err := json.Unmarshal([]byte(text), &out); err != nil {
		return out, fmt.Errorf("call tool %q: decode result: %w", name, err)
	}

	return out, nil
}

// firstTextContent returns the text of the first content block whose type is
// "text". The boolean is false when no such block exists, so callers can
// distinguish "no text content" from a legitimately empty text body.
func firstTextContent(result *ToolResult) (string, bool) {
	for _, item := range result.Content {
		if item.Type == "text" {
			return item.Text, true
		}
	}
	return "", false
}

// TypedTool is a reusable, typed handle bound to a single tool on a client.
// Create one with NewTypedTool and invoke it repeatedly via Call.
type TypedTool[In, Out any] struct {
	client *Client
	name   string
}

// NewTypedTool returns a reusable typed handle for the named tool. Each Call
// on the handle delegates to the package-level Call, so the handle is safe to
// reuse for the lifetime of the client.
//
// Example:
//
//	greet := client.NewTypedTool[GreetIn, GreetOut](c, "greet")
//	out, err := greet.Call(ctx, GreetIn{Name: "World"})
func NewTypedTool[In, Out any](c *Client, name string) *TypedTool[In, Out] {
	return &TypedTool[In, Out]{client: c, name: name}
}

// NewClientTool returns a reusable typed handle for the named tool. It is an
// alias of NewTypedTool, matching the top-level mcp.NewClientTool name.
func NewClientTool[In, Out any](c *Client, name string) *TypedTool[In, Out] {
	return NewTypedTool[In, Out](c, name)
}

// Name returns the tool name this handle is bound to.
func (t *TypedTool[In, Out]) Name() string {
	return t.name
}

// Call invokes the bound tool with the typed input and returns the typed
// output. It delegates to the package-level Call.
func (t *TypedTool[In, Out]) Call(ctx context.Context, in In) (Out, error) {
	return Call[In, Out](ctx, t.client, t.name, in)
}

// Tool is a dynamically typed escape hatch for invoking a tool with raw JSON
// arguments and receiving the raw JSON result.
//
// NOT RECOMMENDED. Prefer the typed Call and NewTypedTool/NewClientTool APIs,
// which are the moat: the Go type system marshals input and unmarshals output
// for you, with compile-time guarantees. Tool is provided only for cases where
// the input and output shapes are not known at compile time (for example,
// proxying tool calls) and forgoes those guarantees.
type Tool interface {
	// Name returns the tool name.
	Name() string
	// Call invokes the tool with raw JSON arguments and returns the first
	// text content block of the result as raw JSON.
	Call(ctx context.Context, in json.RawMessage) (json.RawMessage, error)
}

// DynamicTool is a deprecated alias for the Tool interface.
//
// Deprecated: use Tool. Retained for backward compatibility.
type DynamicTool = Tool

// dynamicTool is the default Tool implementation backed by a Client.
type dynamicTool struct {
	client *Client
	name   string
}

// NewDynamicTool returns a Tool (the raw-JSON escape hatch) for the named tool.
//
// NOT RECOMMENDED. This is the dynamically typed escape hatch; prefer Call or
// NewTypedTool/NewClientTool.
func NewDynamicTool(c *Client, name string) Tool {
	return &dynamicTool{client: c, name: name}
}

// Name returns the tool name this handle is bound to.
func (t *dynamicTool) Name() string {
	return t.name
}

// Call invokes the tool with raw JSON arguments. It delegates to
// (*Client).CallRaw.
func (t *dynamicTool) Call(ctx context.Context, in json.RawMessage) (json.RawMessage, error) {
	return t.client.CallRaw(ctx, t.name, in)
}

// CallRaw invokes the named tool with raw JSON arguments and returns the first
// text content block of the result as raw JSON. It is the dynamic/untyped
// escape hatch.
//
// NOT RECOMMENDED. Prefer the typed Call / NewClientTool APIs, which give you
// compile-time-checked input and output. CallRaw exists only for cases where
// the input and output shapes are not known at compile time.
//
// The arguments are passed through as a json.RawMessage rather than being
// decoded into map[string]any and re-encoded. CallTool marshals its arguments,
// and json.Marshal of a json.RawMessage emits the bytes unchanged, so this
// path is lossless: large int64 values keep full precision (no float64
// rounding) and object field ordering is preserved. Decoding to a map would
// lose both, which defeats the purpose of a raw passthrough.
func (c *Client) CallRaw(ctx context.Context, name string, in json.RawMessage) (json.RawMessage, error) {
	// A nil/empty payload must remain nil so CallTool omits the arguments
	// field entirely rather than sending a null. A non-empty payload is still
	// validated as well-formed JSON to fail fast on malformed input (matching
	// the previous behavior and preserving the syntax-error detail), but the
	// original bytes are forwarded untouched rather than the re-encoded form.
	var args any
	if len(in) > 0 {
		if err := json.Unmarshal(in, new(json.RawMessage)); err != nil {
			return nil, fmt.Errorf("call tool %q: decode arguments: %w", name, err)
		}
		args = in
	}

	result, err := c.CallTool(ctx, name, args)
	if err != nil {
		return nil, err
	}

	if result.IsError {
		return nil, fmt.Errorf("call tool %q: %w: %s", name, ErrToolError, toolErrorText(result))
	}

	// Select the first text content block rather than blindly taking
	// Content[0], which may be an image or other non-text block.
	text, ok := firstTextContent(result)
	if !ok {
		return nil, fmt.Errorf("call tool %q: %w", name, ErrNoToolContent)
	}

	return json.RawMessage(text), nil
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
