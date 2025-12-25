# Migration Guide: modelcontextprotocol/go-sdk to mcp-go

This guide helps you migrate from the official [modelcontextprotocol/go-sdk](https://github.com/modelcontextprotocol/go-sdk) to mcp-go.

## Why Migrate?

mcp-go provides:
- **Simpler API** - Fluent builder pattern instead of struct-heavy configuration
- **Typed Handlers** - Automatic JSON Schema generation from Go structs
- **Built-in Middleware** - Recovery, logging, timeouts out of the box
- **Less Boilerplate** - Focus on business logic, not protocol details

## Quick Comparison

### Before (official SDK)
```go
server := mcp.NewServer(&Implementation{
    Name:    "my-server",
    Version: "1.0.0",
}, &ServerOptions{
    Capabilities: ServerCapabilities{
        Tools: &ToolCapabilities{},
    },
})

mcp.AddTool(server, &mcp.Tool{
    Name:        "search",
    Description: "Search for items",
    InputSchema: json.RawMessage(`{"type":"object","properties":{"query":{"type":"string"}},"required":["query"]}`),
}, searchHandler)
```

### After (mcp-go)
```go
srv := mcp.NewServer(mcp.ServerInfo{
    Name:    "my-server",
    Version: "1.0.0",
    Capabilities: mcp.Capabilities{Tools: true},
})

srv.Tool("search").
    Description("Search for items").
    Handler(func(input SearchInput) ([]Result, error) {
        return search(input.Query), nil
    })
```

## API Mapping

### Server Creation

| Official SDK | mcp-go |
|--------------|--------|
| `mcp.NewServer(&Implementation{...}, &ServerOptions{...})` | `mcp.NewServer(mcp.ServerInfo{...})` |
| `ServerCapabilities{Tools: &ToolCapabilities{}}` | `mcp.Capabilities{Tools: true}` |
| `transport.Run(ctx, transport)` | `mcp.ServeStdio(ctx, srv)` |

### Tools

| Official SDK | mcp-go |
|--------------|--------|
| `mcp.AddTool(server, &mcp.Tool{...}, handler)` | `srv.Tool("name").Description("...").Handler(fn)` |
| Manual JSON Schema in `InputSchema` | Auto-generated from struct tags |
| `*mcp.CallToolRequest` in handler | Just the input struct |
| `*mcp.CallToolResult` return | Any JSON-serializable type |

### Resources

| Official SDK | mcp-go |
|--------------|--------|
| `server.AddResource(&mcp.Resource{...}, handler)` | `srv.Resource("uri").Name("...").Handler(fn)` |
| `*mcp.ReadResourceRequest` in handler | `(ctx, uri string, params map[string]string)` |
| `*mcp.ReadResourceResult` return | `*mcp.ResourceContent` |

### Prompts

| Official SDK | mcp-go |
|--------------|--------|
| `server.AddPrompt(&mcp.Prompt{...}, handler)` | `srv.Prompt("name").Argument(...).Handler(fn)` |
| `*mcp.GetPromptRequest` in handler | `(ctx, args map[string]string)` |
| `*mcp.GetPromptResult` return | `*mcp.PromptResult` |

## Handler Signature Changes

### Tools

**Before:**
```go
func(ctx context.Context, req *mcp.CallToolRequest, input T) (*mcp.CallToolResult, Output, error)
```

**After:**
```go
func(ctx context.Context, input T) (Output, error)
// or without context:
func(input T) (Output, error)
```

### Resources

**Before:**
```go
func(ctx context.Context, req *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error)
```

**After:**
```go
func(ctx context.Context, uri string, params map[string]string) (*mcp.ResourceContent, error)
```

### Prompts

**Before:**
```go
func(ctx context.Context, req *mcp.GetPromptRequest) (*mcp.GetPromptResult, error)
```

**After:**
```go
func(ctx context.Context, args map[string]string) (*mcp.PromptResult, error)
```

## Complete Migration Example

### Before (Official SDK)

```go
package main

import (
    "context"
    "encoding/json"
    "os"

    "github.com/modelcontextprotocol/go-sdk/pkg/mcp"
    "github.com/modelcontextprotocol/go-sdk/pkg/transport"
)

type SearchInput struct {
    Query string `json:"query"`
}

func main() {
    server := mcp.NewServer(&mcp.Implementation{
        Name:    "my-server",
        Version: "1.0.0",
    }, &mcp.ServerOptions{
        Capabilities: mcp.ServerCapabilities{
            Tools: &mcp.ToolCapabilities{},
        },
    })

    mcp.AddTool(server, &mcp.Tool{
        Name:        "search",
        Description: "Search for items",
        InputSchema: json.RawMessage(`{
            "type": "object",
            "properties": {"query": {"type": "string", "description": "Search query"}},
            "required": ["query"]
        }`),
    }, func(ctx context.Context, req *mcp.CallToolRequest, input SearchInput) (*mcp.CallToolResult, []string, error) {
        results := performSearch(input.Query)
        return nil, results, nil
    })

    trans := transport.NewStdioTransport()
    if err := transport.Run(context.Background(), trans, server); err != nil {
        if !isGracefulShutdown(err) {
            os.Exit(1)
        }
    }
}
```

### After (mcp-go)

```go
package main

import (
    "context"
    "os"

    "github.com/felixgeelhaar/mcp-go"
)

type SearchInput struct {
    Query string `json:"query" jsonschema:"required,description=Search query"`
}

func main() {
    srv := mcp.NewServer(mcp.ServerInfo{
        Name:    "my-server",
        Version: "1.0.0",
        Capabilities: mcp.Capabilities{Tools: true},
    })

    srv.Tool("search").
        Description("Search for items").
        Handler(func(input SearchInput) ([]string, error) {
            return performSearch(input.Query), nil
        })

    if err := mcp.ServeStdio(context.Background(), srv); err != nil {
        os.Exit(1)
    }
}
```

## Common Gotchas

### 1. EOF Handling is Automatic

**Before:** Manual `isGracefulShutdown` checks
```go
if err := transport.Run(ctx, trans, server); err != nil {
    if !isGracefulShutdown(err) {
        os.Exit(1)
    }
}
```

**After:** Just check for context cancellation
```go
if err := mcp.ServeStdio(ctx, srv); err != nil && err != context.Canceled {
    os.Exit(1)
}
```

### 2. Capabilities Declaration

**Before:** Nested capability structs
```go
ServerCapabilities{
    Tools:     &ToolCapabilities{},
    Resources: &ResourceCapabilities{Subscribe: true},
}
```

**After:** Simple boolean flags
```go
mcp.Capabilities{
    Tools:     true,
    Resources: true,
}
```

### 3. JSON Schema is Generated

**Before:** Manual JSON Schema strings
```go
InputSchema: json.RawMessage(`{"type":"object","properties":{"query":{"type":"string"}},"required":["query"]}`)
```

**After:** Struct tags
```go
type Input struct {
    Query string `json:"query" jsonschema:"required,description=Search query"`
}
```

### 4. Handler Return Values

**Before:** Tuple return with explicit result wrapper
```go
return &mcp.CallToolResult{Content: []*mcp.ToolContent{...}}, output, nil
```

**After:** Just return your data
```go
return output, nil  // Automatically wrapped
```

### 5. Error Handling

**Before:** Manual error wrapping
```go
return nil, nil, &mcp.Error{Code: -32600, Message: "Invalid request"}
```

**After:** Just return errors
```go
return nil, fmt.Errorf("invalid request")  // Automatically converted to MCP error
```

## Feature Parity

| Feature | Official SDK | mcp-go |
|---------|--------------|--------|
| Tools | ✓ | ✓ |
| Resources | ✓ | ✓ |
| Prompts | ✓ | ✓ |
| Stdio Transport | ✓ | ✓ |
| HTTP+SSE Transport | ✓ | ✓ |
| Progress Reporting | ✓ | ✓ |
| Sampling (v1.1) | ✓ | ✓ |
| Roots (v1.1) | ✓ | ✓ |
| Logging (v1.1) | ✓ | ✓ |
| Middleware | ✗ | ✓ |
| Typed Handlers | ✗ | ✓ |
| Auto JSON Schema | ✗ | ✓ |

## Getting Help

- [mcp-go Documentation](https://pkg.go.dev/github.com/felixgeelhaar/mcp-go)
- [Examples](../examples/)
- [Issues](https://github.com/felixgeelhaar/mcp-go/issues)
