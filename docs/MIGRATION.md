# Migration Guide

This guide helps you migrate to mcp-go from other MCP libraries.

- [Migrating from mark3labs/mcp-go](#migrating-from-mark3labsmcp-go)
- [Migrating from modelcontextprotocol/go-sdk](#migrating-from-modelcontextprotocolgo-sdk)

---

## Migrating from mark3labs/mcp-go

### Why Migrate?

mcp-go builds on top of mark3labs/mcp-go concepts but adds:

- **Typed Handlers** — Receive Go structs, not `map[string]any`
- **Automatic JSON Schema** — Generated from struct tags, always in sync
- **Input Validation** — Invalid input rejected before your handler runs
- **Middleware Chain** — Gin-style middleware for auth, logging, timeouts
- **Production Defaults** — Panic recovery, request IDs, graceful shutdown

### Quick Comparison

**Before (mark3labs/mcp-go):**
```go
srv := server.NewMCPServer("my-server", "1.0.0")

srv.AddTool(mcp.Tool{
    Name:        "search",
    Description: "Search for items",
    InputSchema: mcp.ToolInputSchema{
        Type: "object",
        Properties: map[string]any{
            "query": map[string]any{"type": "string"},
        },
        Required: []string{"query"},
    },
}, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
    args := req.Params.Arguments
    query, _ := args["query"].(string)

    results := performSearch(query)
    return mcp.NewToolResultText(strings.Join(results, "\n")), nil
})
```

**After (mcp-go):**
```go
srv := mcp.NewServer(mcp.ServerInfo{
    Name:    "my-server",
    Version: "1.0.0",
})

type SearchInput struct {
    Query string `json:"query" jsonschema:"required"`
}

srv.Tool("search").
    Description("Search for items").
    Handler(func(input SearchInput) ([]string, error) {
        return performSearch(input.Query), nil
    })
```

### API Mapping

#### Server Creation

| mark3labs/mcp-go | mcp-go |
|------------------|--------|
| `server.NewMCPServer(name, version)` | `mcp.NewServer(mcp.ServerInfo{Name: name, Version: version})` |
| `server.WithToolCapabilities(...)` | `mcp.Capabilities{Tools: true}` |
| `server.ServeStdio(srv)` | `mcp.ServeStdio(ctx, srv)` |

#### Tools

| mark3labs/mcp-go | mcp-go |
|------------------|--------|
| `srv.AddTool(mcp.Tool{...}, handler)` | `srv.Tool("name").Description("...").Handler(fn)` |
| `mcp.ToolInputSchema{Properties: ...}` | Struct with `jsonschema` tags |
| `req.Params.Arguments["key"].(type)` | Direct struct field access |
| `mcp.NewToolResultText(text)` | Return any type (auto-serialized) |

#### Resources

| mark3labs/mcp-go | mcp-go |
|------------------|--------|
| `srv.AddResource(mcp.Resource{...}, handler)` | `srv.Resource("uri://{param}").Handler(fn)` |
| Manual URI parsing | `params["param"]` from URI template |
| `mcp.NewResourceContents(...)` | `*mcp.ResourceContent{URI, MimeType, Text}` |

#### Prompts

| mark3labs/mcp-go | mcp-go |
|------------------|--------|
| `srv.AddPrompt(mcp.Prompt{...}, handler)` | `srv.Prompt("name").Argument(...).Handler(fn)` |
| `req.Params.Arguments["key"]` | `args["key"]` with declared arguments |
| `mcp.NewPromptMessage(...)` | `*mcp.PromptResult{Messages: [...]}` |

### Handler Signature Changes

#### Tools

**Before:**
```go
func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
    query, _ := req.Params.Arguments["query"].(string)
    // ...
}
```

**After:**
```go
func(ctx context.Context, input SearchInput) ([]string, error) {
    // input.Query is already typed and validated
    // ...
}
```

#### Resources

**Before:**
```go
func(ctx context.Context, req mcp.ReadResourceRequest) (string, error) {
    uri := req.Params.URI
    // manual parsing...
}
```

**After:**
```go
func(ctx context.Context, uri string, params map[string]string) (*mcp.ResourceContent, error) {
    id := params["id"]  // extracted from URI template
    // ...
}
```

#### Prompts

**Before:**
```go
func(ctx context.Context, req mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
    name, _ := req.Params.Arguments["name"].(string)
    // ...
}
```

**After:**
```go
func(ctx context.Context, args map[string]string) (*mcp.PromptResult, error) {
    name := args["name"]  // declared with .Argument()
    // ...
}
```

### Complete Migration Example

**Before (mark3labs/mcp-go):**
```go
package main

import (
    "context"
    "fmt"
    "strings"

    "github.com/mark3labs/mcp-go/mcp"
    "github.com/mark3labs/mcp-go/server"
)

func main() {
    srv := server.NewMCPServer("my-server", "1.0.0",
        server.WithToolCapabilities(true),
        server.WithResourceCapabilities(true, false),
    )

    srv.AddTool(mcp.Tool{
        Name:        "greet",
        Description: "Generate a greeting",
        InputSchema: mcp.ToolInputSchema{
            Type: "object",
            Properties: map[string]any{
                "name": map[string]any{
                    "type":        "string",
                    "description": "Name to greet",
                },
            },
            Required: []string{"name"},
        },
    }, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
        name, _ := req.Params.Arguments["name"].(string)
        return mcp.NewToolResultText(fmt.Sprintf("Hello, %s!", name)), nil
    })

    srv.AddResource(mcp.Resource{
        URI:         "config://settings",
        Name:        "Settings",
        Description: "Application settings",
        MimeType:    "application/json",
    }, func(ctx context.Context, req mcp.ReadResourceRequest) (string, error) {
        return `{"theme": "dark"}`, nil
    })

    if err := server.ServeStdio(srv); err != nil {
        panic(err)
    }
}
```

**After (mcp-go):**
```go
package main

import (
    "context"
    "fmt"
    "log"

    "github.com/felixgeelhaar/mcp-go"
)

type GreetInput struct {
    Name string `json:"name" jsonschema:"required,description=Name to greet"`
}

func main() {
    srv := mcp.NewServer(mcp.ServerInfo{
        Name:    "my-server",
        Version: "1.0.0",
    })

    srv.Tool("greet").
        Description("Generate a greeting").
        Handler(func(input GreetInput) (string, error) {
            return fmt.Sprintf("Hello, %s!", input.Name), nil
        })

    srv.Resource("config://settings").
        Name("Settings").
        Description("Application settings").
        MimeType("application/json").
        Handler(func(ctx context.Context, uri string, params map[string]string) (*mcp.ResourceContent, error) {
            return &mcp.ResourceContent{
                URI:      uri,
                MimeType: "application/json",
                Text:     `{"theme": "dark"}`,
            }, nil
        })

    if err := mcp.ServeStdio(context.Background(), srv); err != nil {
        log.Fatal(err)
    }
}
```

### Key Differences

| Aspect | mark3labs/mcp-go | mcp-go |
|--------|------------------|--------|
| Input handling | `map[string]any` with type assertions | Typed structs |
| Schema | Manual `ToolInputSchema` definition | Auto-generated from struct tags |
| Validation | Manual in handler | Automatic before handler |
| URI templates | Manual parsing | Built-in with `{param}` syntax |
| Middleware | Not built-in | First-class support |
| Return values | Wrapper types (`NewToolResultText`) | Any JSON-serializable type |

### Adding Middleware (New Feature)

mcp-go includes production-ready middleware:

```go
import "time"

logger := &myLogger{}
middleware := mcp.DefaultMiddlewareWithTimeout(logger, 30*time.Second)

mcp.ServeStdio(ctx, srv, mcp.WithMiddleware(middleware...))
```

This adds:
- Panic recovery
- Request ID injection
- Timeout enforcement
- Structured logging

---

## Migrating from modelcontextprotocol/go-sdk

### Why Migrate?

mcp-go provides:
- **Simpler API** — Fluent builder pattern instead of struct-heavy configuration
- **Typed Handlers** — Automatic JSON Schema generation from Go structs
- **Built-in Middleware** — Recovery, logging, timeouts out of the box
- **Less Boilerplate** — Focus on business logic, not protocol details

### Quick Comparison

**Before (official SDK):**
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

**After (mcp-go):**
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

### API Mapping

#### Server Creation

| Official SDK | mcp-go |
|--------------|--------|
| `mcp.NewServer(&Implementation{...}, &ServerOptions{...})` | `mcp.NewServer(mcp.ServerInfo{...})` |
| `ServerCapabilities{Tools: &ToolCapabilities{}}` | `mcp.Capabilities{Tools: true}` |
| `transport.Run(ctx, transport)` | `mcp.ServeStdio(ctx, srv)` |

#### Tools

| Official SDK | mcp-go |
|--------------|--------|
| `mcp.AddTool(server, &mcp.Tool{...}, handler)` | `srv.Tool("name").Description("...").Handler(fn)` |
| Manual JSON Schema in `InputSchema` | Auto-generated from struct tags |
| `*mcp.CallToolRequest` in handler | Just the input struct |
| `*mcp.CallToolResult` return | Any JSON-serializable type |

#### Resources

| Official SDK | mcp-go |
|--------------|--------|
| `server.AddResource(&mcp.Resource{...}, handler)` | `srv.Resource("uri").Name("...").Handler(fn)` |
| `*mcp.ReadResourceRequest` in handler | `(ctx, uri string, params map[string]string)` |
| `*mcp.ReadResourceResult` return | `*mcp.ResourceContent` |

#### Prompts

| Official SDK | mcp-go |
|--------------|--------|
| `server.AddPrompt(&mcp.Prompt{...}, handler)` | `srv.Prompt("name").Argument(...).Handler(fn)` |
| `*mcp.GetPromptRequest` in handler | `(ctx, args map[string]string)` |
| `*mcp.GetPromptResult` return | `*mcp.PromptResult` |

### Handler Signature Changes

#### Tools

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

#### Resources

**Before:**
```go
func(ctx context.Context, req *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error)
```

**After:**
```go
func(ctx context.Context, uri string, params map[string]string) (*mcp.ResourceContent, error)
```

#### Prompts

**Before:**
```go
func(ctx context.Context, req *mcp.GetPromptRequest) (*mcp.GetPromptResult, error)
```

**After:**
```go
func(ctx context.Context, args map[string]string) (*mcp.PromptResult, error)
```

### Complete Migration Example

**Before (Official SDK):**

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

**After (mcp-go):**

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

### Common Gotchas

#### 1. EOF Handling is Automatic

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

#### 2. Capabilities Declaration

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

#### 3. JSON Schema is Generated

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

#### 4. Handler Return Values

**Before:** Tuple return with explicit result wrapper
```go
return &mcp.CallToolResult{Content: []*mcp.ToolContent{...}}, output, nil
```

**After:** Just return your data
```go
return output, nil  // Automatically wrapped
```

#### 5. Error Handling

**Before:** Manual error wrapping
```go
return nil, nil, &mcp.Error{Code: -32600, Message: "Invalid request"}
```

**After:** Just return errors
```go
return nil, fmt.Errorf("invalid request")  // Automatically converted to MCP error
```

---

## Feature Parity

| Feature | Official SDK | mark3labs/mcp-go | mcp-go |
|---------|--------------|------------------|--------|
| Tools | ✓ | ✓ | ✓ |
| Resources | ✓ | ✓ | ✓ |
| Prompts | ✓ | ✓ | ✓ |
| Stdio Transport | ✓ | ✓ | ✓ |
| HTTP+SSE Transport | ✓ | ✓ | ✓ |
| Progress Reporting | ✓ | ✓ | ✓ |
| Sampling (v1.1) | ✓ | ✓ | ✓ |
| Roots (v1.1) | ✓ | ✓ | ✓ |
| Logging (v1.1) | ✓ | ✓ | ✓ |
| Middleware | ✗ | ✗ | ✓ |
| Typed Handlers | ✗ | ✗ | ✓ |
| Auto JSON Schema | ✗ | ✗ | ✓ |
| Input Validation | ✗ | ✗ | ✓ |

---

## Getting Help

- [mcp-go Documentation](https://pkg.go.dev/github.com/felixgeelhaar/mcp-go)
- [Comparison Guide](./COMPARISON.md)
- [Examples](../examples/)
- [Issues](https://github.com/felixgeelhaar/mcp-go/issues)
