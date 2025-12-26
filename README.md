# mcp-go

[![Go Reference](https://pkg.go.dev/badge/github.com/felixgeelhaar/mcp-go.svg)](https://pkg.go.dev/github.com/felixgeelhaar/mcp-go)
[![Go Report Card](https://goreportcard.com/badge/github.com/felixgeelhaar/mcp-go)](https://goreportcard.com/report/github.com/felixgeelhaar/mcp-go)
[![CI](https://github.com/felixgeelhaar/mcp-go/actions/workflows/ci.yml/badge.svg)](https://github.com/felixgeelhaar/mcp-go/actions/workflows/ci.yml)
![Coverage](https://raw.githubusercontent.com/felixgeelhaar/mcp-go/badges/.badges/main/coverage.svg)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](https://opensource.org/licenses/MIT)

**A Go framework for building Model Context Protocol (MCP) servers — like Gin, but for MCP.**

mcp-go is an opinionated, production-ready framework for building MCP servers in Go with typed handlers, automatic schema generation, middleware, and multiple transports.

> If the MCP SDK gives you protocol correctness,
> and mark3labs/mcp-go gives you SDK convenience,
> **mcp-go gives you application structure and production defaults.**

---

## Why mcp-go?

Building an MCP server directly on top of SDKs means repeatedly solving the same problems:

- input decoding & validation
- schema generation
- error handling
- middleware (auth, timeouts, logging)
- transport wiring (stdio vs HTTP)

**mcp-go solves these once — idiomatically, safely, and with great DX.**

---

## Quickstart (5 minutes)

```go
package main

import (
    "context"
    "log"

    "github.com/felixgeelhaar/mcp-go"
)

func main() {
    srv := mcp.NewServer(mcp.ServerInfo{
        Name:    "example",
        Version: "0.1.0",
    })

    srv.Tool("hello").
        Description("Say hello").
        Handler(func(ctx context.Context, in struct{ Name string `json:"name"` }) (string, error) {
            return "Hello " + in.Name, nil
        })

    log.Fatal(mcp.ServeStdio(context.Background(), srv))
}
```

**That's it:**

- Typed input
- Automatic JSON Schema
- MCP-compliant responses
- No manual routing or validation

---

## Core Features

### Typed MCP tools

Define tools using strongly-typed Go structs. Invalid input never reaches your business logic.

```go
type SearchInput struct {
    Query string `json:"query" jsonschema:"required"`
    Limit int    `json:"limit"`
}

srv.Tool("search").
    Description("Search for items").
    Handler(func(ctx context.Context, input SearchInput) ([]string, error) {
        // Your search logic here
        return []string{"result1", "result2"}, nil
    })
```

### Automatic JSON Schema

Schemas are derived from Go structs and:

- validated automatically
- exposed via MCP introspection
- kept in sync with code

No manual schema maintenance.

---

### Gin-style middleware

Apply cross-cutting concerns consistently:

```go
srv.Use(
    middleware.Recover(),
    middleware.Timeout(5*time.Second),
    middleware.RequestID(),
    middleware.Logging(logger),
)
```

**Use cases:**

- auth / principals
- rate limiting
- tracing
- metrics
- panic recovery

---

### Multiple transports

Run the same server over:

- **stdio** (CLI / agent use)
- **HTTP + SSE** (service deployments)
- **WebSocket** (bidirectional communication)

```go
// Stdio for CLI tools
mcp.ServeStdio(ctx, srv)

// HTTP for web services
mcp.ServeHTTP(ctx, srv, ":8080")
```

### Production-ready defaults

- strict JSON decoding
- safe error mapping
- graceful shutdown
- context propagation everywhere

You can opt out — but safety is the default.

---

## How this fits into the MCP ecosystem

| Project | What it is |
|---------|------------|
| MCP Go SDK | Low-level protocol implementation |
| mark3labs/mcp-go | Community SDK / helpers |
| **mcp-go** | Full Go framework for MCP servers |

**Think:**

- MCP SDK → `net/http`
- mcp-go → **Gin**

We build on top of the MCP spec — not instead of it.

---

## When should you use mcp-go?

**Use mcp-go if you:**

- are building real MCP services, not just experiments
- want typed APIs and validation
- need auth, limits, observability
- deploy MCP servers in production

If you want raw protocol access only, an SDK may be a better fit.

---

## Used by

- **Obvia** (incident automation & AIOps tooling)
- Internal MCP services and experiments
- OSS projects building on MCP

Want to add your project? Open a PR!

---

## Installation

```bash
go get github.com/felixgeelhaar/mcp-go
```

Requires Go 1.23 or later.

---

## Documentation

- [API Reference](https://pkg.go.dev/github.com/felixgeelhaar/mcp-go)
- [Getting Started](./docs/GETTING_STARTED.md)
- [Migration from mark3labs/mcp-go](./docs/MIGRATION.md)
- [Comparison: MCP SDK vs mark3labs vs mcp-go](./docs/COMPARISON.md)
- [Examples](./examples/)
- [MCP Specification](https://spec.modelcontextprotocol.io/)

---

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
- `Auth()` - API key and Bearer token authentication
- `RateLimit()` - Request throttling
- `SizeLimit()` - Request size limits

### HTTP Transport

Serve over HTTP with Server-Sent Events:

```go
mcp.ServeHTTP(ctx, srv, ":8080",
    mcp.WithReadTimeout(30*time.Second),
    mcp.WithWriteTimeout(30*time.Second),
)
```

---

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

---

## Philosophy

- **Typed > dynamic**
- **Safe defaults > flexibility**
- **Frameworks create ecosystems**

mcp-go aims to be the default Go framework for MCP — boring, predictable, and a joy to use.

---

## Contributing

Contributions are welcome! Please read our [Contributing Guide](CONTRIBUTING.md).

## License

MIT License - see [LICENSE](LICENSE) for details.
