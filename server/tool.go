package server

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"

	"go.klarlabs.de/mcp/protocol"
	"go.klarlabs.de/mcp/schema"
)

// StructuredResult allows tool handlers to return structured content
// alongside text content blocks. When a handler returns this type,
// the response includes both content and structuredContent fields.
type StructuredResult struct {
	// Content contains text/image content blocks for display.
	Content []Content `json:"content,omitempty"`
	// StructuredContent contains typed data matching the tool's outputSchema.
	StructuredContent map[string]any `json:"structuredContent"`
	// IsError indicates whether the result represents an error.
	IsError bool `json:"isError,omitempty"`
}

// Tool represents a callable function exposed via MCP.
type Tool struct {
	name           string
	description    string
	inputType      reflect.Type
	inputSchema    any
	outputSchema   any
	validatable    *schema.Schema
	skipValidation bool
	handler        any
	hasContext     bool
	annotations    *ToolAnnotations
	meta           map[string]any
	icons          []Icon
}

// ToolBuilder provides a fluent API for building tools.
type ToolBuilder struct {
	tool   *Tool
	server *Server
	err    error
}

// Description sets the tool description.
func (b *ToolBuilder) Description(desc string) *ToolBuilder {
	if b.err != nil {
		return b
	}
	b.tool.description = desc
	return b
}

// OutputSchema sets the output schema for structured content responses.
// Pass a zero-value instance of the output type (e.g., OutputSchema(MyOutput{})).
// When set, the tool advertises an outputSchema in its listing and handlers
// can return StructuredResult with typed structured content.
func (b *ToolBuilder) OutputSchema(example any) *ToolBuilder {
	if b.err != nil {
		return b
	}
	s, err := schema.Generate(example)
	if err != nil {
		b.err = fmt.Errorf("failed to generate output schema: %w", err)
		return b
	}
	b.tool.outputSchema = s
	return b
}

// ValidateInput is a no-op retained for backward compatibility. Input
// validation against the generated JSON Schema now runs by default before
// every handler invocation, so this method has no effect beyond documenting
// intent. To opt out of validation, use SkipValidation.
//
// Deprecated: validation is on by default; calling ValidateInput is
// unnecessary. Use SkipValidation to disable validation.
func (b *ToolBuilder) ValidateInput() *ToolBuilder {
	return b
}

// SkipValidation disables runtime JSON Schema validation of tool inputs for
// this tool. By default, inputs are validated against the generated schema
// (required/min/max/enum) before the handler is called, and invalid inputs
// are rejected with an InvalidParams error so they never reach business
// logic. SkipValidation is an escape hatch for tools that need to accept
// inputs the generated schema would reject; it is not recommended.
func (b *ToolBuilder) SkipValidation() *ToolBuilder {
	if b.err != nil {
		return b
	}
	b.tool.skipValidation = true
	return b
}

// Meta sets arbitrary metadata on the tool, serialized as _meta in the tool listing.
func (b *ToolBuilder) Meta(meta map[string]any) *ToolBuilder {
	if b.err != nil {
		return b
	}
	b.tool.meta = meta
	return b
}

// Icons sets optional icons advertised for this tool in tools/list, per the
// MCP 2025-11-25 spec (SEP-973). Icons are for UI display and are purely
// informational metadata.
func (b *ToolBuilder) Icons(icons ...Icon) *ToolBuilder {
	if b.err != nil {
		return b
	}
	b.tool.icons = icons
	return b
}

// UIResource marks this tool as having an associated MCP App UI resource.
// The given URI is included in both the nested _meta.ui.resourceUri field
// (preferred) and the flat _meta["ui/resourceUri"] key (legacy) for maximum
// host compatibility per the MCP Apps extension spec.
func (b *ToolBuilder) UIResource(uri string) *ToolBuilder {
	if b.err != nil {
		return b
	}
	b.tool.meta = map[string]any{
		"ui": map[string]any{
			"resourceUri": uri,
		},
		"ui/resourceUri": uri,
	}
	return b
}

// Handler sets the tool handler function.
// Handler signature must be one of:
//   - func(input T) (R, error)
//   - func(ctx context.Context, input T) (R, error)
func (b *ToolBuilder) Handler(fn any) *ToolBuilder {
	if b.err != nil {
		return b
	}

	if err := b.validateHandler(fn); err != nil {
		b.err = err
		return b
	}

	b.tool.handler = fn
	b.server.registerTool(b.tool)
	return b
}

// validateHandler validates the handler function signature.
func (b *ToolBuilder) validateHandler(fn any) error {
	fnType := reflect.TypeOf(fn)

	if fnType.Kind() != reflect.Func {
		return fmt.Errorf("handler must be a function, got %s", fnType.Kind())
	}

	// Check number of inputs
	numIn := fnType.NumIn()
	if numIn < 1 || numIn > 2 {
		return fmt.Errorf("handler must have 1 or 2 parameters, got %d", numIn)
	}

	// Check for context as first param
	var inputParamIdx int
	if numIn == 2 {
		if !fnType.In(0).Implements(reflect.TypeOf((*context.Context)(nil)).Elem()) {
			return fmt.Errorf("first parameter must be context.Context when using 2 parameters")
		}
		b.tool.hasContext = true
		inputParamIdx = 1
	} else {
		inputParamIdx = 0
	}

	// Store input type
	inputType := fnType.In(inputParamIdx)
	if inputType.Kind() == reflect.Pointer {
		inputType = inputType.Elem()
	}
	b.tool.inputType = inputType

	// Generate input schema
	inputSchema, err := schema.GenerateFromType(inputType)
	if err != nil {
		return fmt.Errorf("failed to generate input schema: %w", err)
	}
	b.tool.inputSchema = inputSchema
	b.tool.validatable = inputSchema // Store for validation

	// Check outputs
	if fnType.NumOut() != 2 {
		return fmt.Errorf("handler must return (result, error), got %d return values", fnType.NumOut())
	}

	// Second return must be error
	errType := reflect.TypeOf((*error)(nil)).Elem()
	if !fnType.Out(1).Implements(errType) {
		return fmt.Errorf("second return value must be error")
	}

	return nil
}

// Meta returns the tool's metadata map, used for _meta in MCP responses.
func (t *Tool) Meta() map[string]any {
	return t.meta
}

// OutputSchema returns the tool's output schema, or nil if not set.
func (t *Tool) OutputSchema() any {
	return t.outputSchema
}

// Icons returns the tool's icons, used for the icons field in tools/list.
// Returns nil when no icons were set.
func (t *Tool) Icons() []Icon {
	return t.icons
}

// Execute runs the tool handler with the given JSON input.
func (t *Tool) Execute(ctx context.Context, input json.RawMessage) (any, error) {
	// Validate input against the generated schema before the handler runs,
	// so invalid-per-schema input never reaches business logic. Validation is
	// on by default; SkipValidation opts a tool out.
	if !t.skipValidation && t.validatable != nil {
		if err := t.validatable.Validate(input); err != nil {
			return nil, protocol.NewInvalidParams(fmt.Sprintf("input validation failed: %v", err))
		}
	}

	// Create input value
	inputPtr := reflect.New(t.inputType)
	if err := json.Unmarshal(input, inputPtr.Interface()); err != nil {
		return nil, protocol.NewInvalidParams(fmt.Sprintf("failed to parse input: %v", err))
	}

	// Build arguments
	fnVal := reflect.ValueOf(t.handler)
	var args []reflect.Value

	if t.hasContext {
		args = append(args, reflect.ValueOf(ctx))
	}

	// Use the value, not pointer, for the input
	args = append(args, inputPtr.Elem())

	// Call handler
	results := fnVal.Call(args)

	// Extract result and error
	resultVal := results[0].Interface()
	errVal := results[1].Interface()

	if errVal != nil {
		return nil, errVal.(error)
	}

	return resultVal, nil
}
