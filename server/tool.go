package server

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"

	"github.com/felixgeelhaar/mcp-go/protocol"
	"github.com/felixgeelhaar/mcp-go/schema"
)

// Tool represents a callable function exposed via MCP.
type Tool struct {
	name          string
	description   string
	inputType     reflect.Type
	inputSchema   any
	validatable   *schema.Schema
	validateInput bool
	handler       any
	hasContext    bool
	annotations   *ToolAnnotations
	meta          map[string]any
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

// ValidateInput enables runtime schema validation of tool inputs.
// When enabled, inputs are validated against the JSON Schema before
// the handler is called. Invalid inputs result in an InvalidParams error.
func (b *ToolBuilder) ValidateInput() *ToolBuilder {
	if b.err != nil {
		return b
	}
	b.tool.validateInput = true
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
	if inputType.Kind() == reflect.Ptr {
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

// Execute runs the tool handler with the given JSON input.
func (t *Tool) Execute(ctx context.Context, input json.RawMessage) (any, error) {
	// Validate input against schema if enabled
	if t.validateInput && t.validatable != nil {
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
