# mcp-go

[![Go Reference](https://pkg.go.dev/badge/github.com/felixgeelhaar/mcp-go.svg)](https://pkg.go.dev/github.com/felixgeelhaar/mcp-go)
[![Go Report Card](https://goreportcard.com/badge/github.com/felixgeelhaar/mcp-go)](https://goreportcard.com/report/github.com/felixgeelhaar/mcp-go)
[![CI](https://github.com/felixgeelhaar/mcp-go/actions/workflows/ci.yml/badge.svg)](https://github.com/felixgeelhaar/mcp-go/actions/workflows/ci.yml)
![Coverage](https://raw.githubusercontent.com/felixgeelhaar/mcp-go/badges/.badges/main/coverage.svg)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](https://opensource.org/licenses/MIT)

A Go framework for building [Model Context Protocol (MCP)](https://modelcontextprotocol.io/) servers. mcp-go aims to be the "Gin framework" for MCP servers, providing a simple, intuitive API with production-ready defaults.

## Features

- **Typed Handlers** - Define tool inputs as Go structs with automatic JSON Schema generation
- **Fluent API** - Gin-style builder pattern for tools, resources, and prompts
- **Middleware Chain** - Composable middleware for logging, recovery, timeouts, and more
- **Multiple Transports** - Stdio for CLI tools, HTTP+SSE for web services
- **Production Ready** - Built-in panic recovery, request IDs, and structured logging

## Installation

```bash
go get github.com/felixgeelhaar/mcp-go
```

Requires Go 1.23 or later.

## Quick Start

```go
package main

import (
    "context"
    "log"

    "github.com/felixgeelhaar/mcp-go"
)

type SearchInput struct {
    Query string `json:"query" jsonschema:"required,description=Search query"`
    Limit int    `json:"limit" jsonschema:"description=Max results to return"`
}

func main() {
    srv := mcp.NewServer(mcp.ServerInfo{
        Name:    "my-server",
        Version: "1.0.0",
        Capabilities: mcp.Capabilities{
            Tools: true,
        },
    })

    srv.Tool("search").
        Description("Search for items").
        Handler(func(ctx context.Context, input SearchInput) ([]string, error) {
            // Your search logic here
            return []string{"result1", "result2"}, nil
        })

    if err := mcp.ServeStdio(context.Background(), srv); err != nil {
        log.Fatal(err)
    }
}
```

## Documentation

- [API Reference](https://pkg.go.dev/github.com/felixgeelhaar/mcp-go)
- [Examples](./examples/)
- [Migration Guide](./docs/MIGRATION.md) - Migrating from modelcontextprotocol/go-sdk
- [MCP Specification](https://spec.modelcontextprotocol.io/)

## Examples

### Tools

Tools are functions that can be called by the AI model:

```go
type CalculateInput struct {
    Operation string  `json:"operation" jsonschema:"required"`
    A         float64 `json:"a" jsonschema:"required"`
    B         float64 `json:"b" jsonschema:"required"`
}

srv.Tool("calculate").
    Description("Perform arithmetic operations").
    Handler(func(input CalculateInput) (float64, error) {
        switch input.Operation {
        case "add":
            return input.A + input.B, nil
        case "multiply":
            return input.A * input.B, nil
        default:
            return 0, fmt.Errorf("unknown operation: %s", input.Operation)
        }
    })
```

### Resources

Resources expose data via URI templates:

```go
srv.Resource("file://{path}").
    Name("File").
    Description("Read file content").
    MimeType("text/plain").
    Handler(func(ctx context.Context, uri string, params map[string]string) (*mcp.ResourceContent, error) {
        content, err := os.ReadFile(params["path"])
        if err != nil {
            return nil, err
        }
        return &mcp.ResourceContent{
            URI:      uri,
            MimeType: "text/plain",
            Text:     string(content),
        }, nil
    })
```

### Prompts

Prompts are parameterized message templates:

```go
srv.Prompt("code-review").
    Description("Generate a code review prompt").
    Argument("language", "Programming language", true).
    Handler(func(ctx context.Context, args map[string]string) (*mcp.PromptResult, error) {
        return &mcp.PromptResult{
            Messages: []mcp.PromptMessage{
                {
                    Role:    "user",
                    Content: mcp.TextContent{Type: "text", Text: fmt.Sprintf("Review this %s code:", args["language"])},
                },
            },
        }, nil
    })
```

### Middleware

Add cross-cutting concerns with middleware:

```go
// Use default production middleware stack
middleware := mcp.DefaultMiddlewareWithTimeout(logger, 30*time.Second)

mcp.ServeStdio(ctx, srv, mcp.WithMiddleware(middleware...))
```

Built-in middleware:
- `Recover()` - Catch panics and convert to errors
- `RequestID()` - Inject unique request IDs
- `Timeout(d)` - Enforce request deadlines
- `Logging(logger)` - Structured request logging

### HTTP Transport

Serve over HTTP with Server-Sent Events:

```go
mcp.ServeHTTP(ctx, srv, ":8080",
    mcp.WithReadTimeout(30*time.Second),
    mcp.WithWriteTimeout(30*time.Second),
)
```

## Type Reference

### ResourceContent

Return type for resource handlers:

```go
&mcp.ResourceContent{
    URI:      uri,           // The resource URI
    MimeType: "application/json",
    Text:     string(data),  // Text content
    // OR for binary:
    // Blob: base64EncodedData,
}
```

### Tool Return Types

Tools can return any JSON-serializable type:

```go
// Simple string
func(input Input) (string, error)

// Struct (auto-serialized to JSON)
func(input Input) (MyResult, error)

// Slice
func(input Input) ([]Item, error)

// Map
func(input Input) (map[string]any, error)
```

### PromptResult

Return type for prompt handlers:

```go
&mcp.PromptResult{
    Description: "Optional description",
    Messages: []mcp.PromptMessage{
        {Role: "user", Content: mcp.TextContent{Type: "text", Text: "..."}},
        {Role: "assistant", Content: mcp.TextContent{Type: "text", Text: "..."}},
    },
}
```

### Session (v1.1)

Access bidirectional communication features:

```go
session := mcp.SessionFromContext(ctx)

// Logging
session.Info("logger", "message")
session.Error("logger", map[string]any{"error": err.Error()})

// Sampling (request LLM completion from client)
result, err := session.CreateMessage(ctx, &mcp.CreateMessageRequest{
    Messages: []mcp.SamplingMessage{{Role: mcp.RoleUser, Content: mcp.NewTextContent("...")}},
    MaxTokens: 100,
})

// Roots (workspace awareness)
roots, err := session.ListRoots(ctx)
```

## JSON Schema Tags

Use struct tags to define JSON Schema for tool inputs:

```go
type SearchInput struct {
    Query    string   `json:"query" jsonschema:"required,description=Search query"`
    Limit    int      `json:"limit" jsonschema:"description=Max results,default=10"`
    Tags     []string `json:"tags" jsonschema:"description=Filter by tags"`
    MinScore float64  `json:"minScore" jsonschema:"minimum=0,maximum=1"`
}
```

Supported tags:
- `required` - Field is required
- `description=...` - Field description
- `default=...` - Default value
- `minimum=N` / `maximum=N` - Numeric bounds
- `minLength=N` / `maxLength=N` - String length bounds
- `enum=a|b|c` - Allowed values

## Common Patterns

### Returning JSON from Resources

```go
srv.Resource("data://users").Handler(func(ctx context.Context, uri string, params map[string]string) (*mcp.ResourceContent, error) {
    users := getUsers()
    data, err := json.MarshalIndent(users, "", "  ")
    if err != nil {
        return nil, err
    }
    return &mcp.ResourceContent{
        URI:      uri,
        MimeType: "application/json",
        Text:     string(data),
    }, nil
})
```

### Error Handling in Tools

```go
srv.Tool("fetch").Handler(func(ctx context.Context, input FetchInput) (Result, error) {
    if input.URL == "" {
        return Result{}, fmt.Errorf("URL is required")  // Becomes MCP error
    }
    // Errors are automatically formatted for MCP clients
    return doFetch(input.URL)
})
```

### Using Context for Cancellation

```go
srv.Tool("long-operation").Handler(func(ctx context.Context, input Input) (string, error) {
    for i := 0; i < 100; i++ {
        select {
        case <-ctx.Done():
            return "", ctx.Err()  // Request was cancelled
        default:
            doWork()
        }
    }
    return "done", nil
})
```

### Progress Reporting

```go
srv.Tool("process").Handler(func(ctx context.Context, input Input) (string, error) {
    progress := mcp.ProgressFromContext(ctx)
    total := 100.0
    for i := 0; i < 100; i++ {
        progress.Report(float64(i), &total)
        doWork()
    }
    return "complete", nil
})
```

## Benchmarks

```
BenchmarkToolExecution-8           1913233    987.6 ns/op    328 B/op     9 allocs/op
BenchmarkMiddlewareChain-8         1411394    892.3 ns/op    721 B/op    10 allocs/op
BenchmarkJSONParsing-8              967858   1193 ns/op      424 B/op    11 allocs/op
```

## Contributing

Contributions are welcome! Please read our [Contributing Guide](CONTRIBUTING.md).

## License

MIT License - see [LICENSE](LICENSE) for details.
