# Getting Started with mcp-go

## What is mcp-go?

mcp-go is a Go framework for building [Model Context Protocol (MCP)](https://modelcontextprotocol.io/) servers. It is not an MCP SDK—it is a higher-level framework that provides typed handlers, automatic JSON Schema generation, input validation, middleware, and multiple transports. Think of it as "Gin for MCP": just as Gin builds on top of `net/http` to provide structure and convenience, mcp-go builds on top of the MCP protocol to give you a production-ready application framework.

---

## Prerequisites

- **Go 1.23** or later
- Basic Go knowledge (packages, structs, functions)
- No prior MCP knowledge required

---

## Installation

```bash
go get github.com/felixgeelhaar/mcp-go
```

---

## Your first MCP server

Create a file called `main.go`:

```go
package main

import (
    "context"
    "log"

    "github.com/felixgeelhaar/mcp-go"
)

// GreetInput defines the typed input for our tool.
// The struct tags control JSON serialization and schema generation.
type GreetInput struct {
    Name string `json:"name" jsonschema:"required,description=Name to greet"`
}

func main() {
    // Create a new MCP server with metadata.
    // This info is exposed to clients during the MCP handshake.
    srv := mcp.NewServer(mcp.ServerInfo{
        Name:    "hello-server",
        Version: "0.1.0",
    })

    // Register a tool with a typed handler.
    // The input struct is automatically converted to a JSON Schema
    // and validated before your handler is called.
    srv.Tool("greet").
        Description("Generate a greeting message").
        Handler(func(ctx context.Context, input GreetInput) (string, error) {
            return "Hello, " + input.Name + "!", nil
        })

    // Start the server using stdio transport.
    // This is the standard transport for CLI tools and AI agents.
    if err := mcp.ServeStdio(context.Background(), srv); err != nil {
        log.Fatal(err)
    }
}
```

---

## Running the server

Build and run:

```bash
go build -o hello-server
./hello-server
```

The server communicates over **stdio transport**—it reads JSON-RPC messages from stdin and writes responses to stdout. This is the standard transport for MCP servers used by CLI tools and AI agents like Claude Desktop.

To test it manually, you can pipe JSON-RPC requests to the server, but in practice, an MCP client (like an AI agent) handles this communication for you.

---

## What you get for free

By defining a typed handler, mcp-go automatically provides:

- **Typed input** — Your handler receives a strongly-typed Go struct, not `map[string]any`
- **Automatic JSON Schema** — The schema is generated from your struct and exposed via MCP introspection
- **Input validation** — Invalid input is rejected before your handler runs
- **MCP-compliant errors** — Errors are formatted according to the MCP specification
- **Context propagation** — Cancellation and deadlines work out of the box

---

## Adding middleware

Middleware wraps your handlers to add cross-cutting concerns. Use the built-in middleware stack for production defaults:

```go
package main

import (
    "context"
    "log"
    "time"

    "github.com/felixgeelhaar/mcp-go"
)

type GreetInput struct {
    Name string `json:"name" jsonschema:"required"`
}

func main() {
    srv := mcp.NewServer(mcp.ServerInfo{
        Name:    "hello-server",
        Version: "0.1.0",
    })

    srv.Tool("greet").
        Description("Generate a greeting message").
        Handler(func(ctx context.Context, input GreetInput) (string, error) {
            return "Hello, " + input.Name + "!", nil
        })

    // Create a logger (implement mcp.Logger interface)
    logger := &myLogger{}

    // DefaultMiddlewareWithTimeout provides:
    // - Panic recovery (prevents crashes)
    // - Request ID injection (for tracing)
    // - Timeout enforcement (prevents hung requests)
    // - Structured logging (observability)
    middleware := mcp.DefaultMiddlewareWithTimeout(logger, 30*time.Second)

    if err := mcp.ServeStdio(context.Background(), srv, mcp.WithMiddleware(middleware...)); err != nil {
        log.Fatal(err)
    }
}

// myLogger implements mcp.Logger
type myLogger struct{}

func (l *myLogger) Info(msg string, fields ...mcp.LogField)  { log.Println("[INFO]", msg) }
func (l *myLogger) Error(msg string, fields ...mcp.LogField) { log.Println("[ERROR]", msg) }
func (l *myLogger) Debug(msg string, fields ...mcp.LogField) { log.Println("[DEBUG]", msg) }
func (l *myLogger) Warn(msg string, fields ...mcp.LogField)  { log.Println("[WARN]", msg) }
```

---

## Running over HTTP

The same server can run over HTTP with Server-Sent Events. Just change the transport:

```go
// Instead of:
// mcp.ServeStdio(ctx, srv)

// Use:
mcp.ServeHTTP(ctx, srv, ":8080",
    mcp.WithReadTimeout(30*time.Second),
    mcp.WithWriteTimeout(30*time.Second),
)
```

This exposes:

- `POST /mcp` — JSON-RPC endpoint
- `GET /sse` — Server-Sent Events for streaming
- `GET /health` — Health check endpoint

The key point: **same server, different transport**. Your tools, resources, and prompts work identically regardless of how clients connect.

---

## Next steps

Now that you have a basic server running, explore more capabilities:

- **[Tools](../examples/basic/)** — Define more tools with complex input types
- **[Resources](../examples/resources/)** — Expose data via URI templates
- **[Prompts](../examples/prompts/)** — Create parameterized prompt templates
- **[Middleware](../examples/middleware/)** — Add auth, rate limiting, and custom middleware
- **[Comparison Guide](./COMPARISON.md)** — Understand how mcp-go differs from other MCP libraries
