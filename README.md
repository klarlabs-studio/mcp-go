# mcp-go

[![Go Reference](https://pkg.go.dev/badge/go.klarlabs.de/mcp.svg)](https://pkg.go.dev/go.klarlabs.de/mcp)
[![Go Report Card](https://goreportcard.com/badge/go.klarlabs.de/mcp)](https://goreportcard.com/report/go.klarlabs.de/mcp)
[![CI](https://github.com/klarlabs-studio/mcp-go/actions/workflows/ci.yml/badge.svg)](https://github.com/klarlabs-studio/mcp-go/actions/workflows/ci.yml)
![Coverage](https://raw.githubusercontent.com/klarlabs-studio/mcp-go/badges/.badges/main/coverage.svg)
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

    "go.klarlabs.de/mcp"
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
- **gRPC** (high-performance service-to-service)

```go
// Stdio for CLI tools
mcp.ServeStdio(ctx, srv)

// HTTP + SSE for web services
mcp.ServeHTTP(ctx, srv, ":8080")

// WebSocket for bidirectional communication
mcp.ServeWebSocket(ctx, srv, ":8081")

// gRPC for high-performance service-to-service
mcp.ServeGRPC(ctx, srv, ":9090")
```

### MCP Apps Support

Build tools with interactive UIs using the [MCP Apps extension](https://modelcontextprotocol.io/specification/2025-06-18/extensions/apps). Tools declare a `ui://` resource URI via `UIResource()`, and the linked HTML resource is rendered as a sandboxed iframe in supported hosts like Claude Desktop.

```go
srv.Tool("visualize").
    Description("Visualize data interactively").
    UIResource("ui://my-app/visualizer").
    Handler(func(input VisualizeInput) (any, error) {
        return getData(input.ID), nil
    })

// Serve the UI as a resource with the MCP Apps MIME type
srv.Resource("ui://my-app/visualizer").
    Name("Visualizer").
    MimeType("text/html;profile=mcp-app").
    Handler(func(ctx context.Context, uri string, params map[string]string) (*mcp.ResourceContent, error) {
        return &mcp.ResourceContent{
            URI:      uri,
            MimeType: "text/html;profile=mcp-app",
            Text:     visualizerHTML,
        }, nil
    })
```

`UIResource()` sets `_meta.ui.resourceUri` on both `tools/list` and `tools/call` responses, telling MCP hosts to render the linked resource as an interactive app alongside tool results.

#### Building the HTML resource

The HTML resource must be a **single self-contained file** (inline CSS, JS, no external requests). The recommended stack:

- **[`@modelcontextprotocol/ext-apps`](https://www.npmjs.com/package/@modelcontextprotocol/ext-apps)** — Client SDK for the MCP Apps postMessage protocol (handles `ui/initialize` handshake, receives tool results)
- **Vite + [`vite-plugin-singlefile`](https://www.npmjs.com/package/vite-plugin-singlefile)** — Bundles everything into one HTML file
- **Go `embed.FS`** — Embeds the built HTML files into the Go binary

#### Common pitfalls

**Vue/React string templates and the runtime compiler.** If you use Vue's `defineComponent` with a `template` string (instead of `.vue` SFCs), Vite's default Vue build is **runtime-only** and cannot compile templates at runtime. The iframe will render empty with no error. Fix this by aliasing Vue to the full build in `vite.config.ts`:

```ts
// vite.config.ts
export default defineConfig({
  resolve: {
    alias: {
      vue: "vue/dist/vue.esm-bundler.js",
    },
  },
});
```

**No TypeScript in runtime-compiled templates.** Vue's runtime template compiler only understands JavaScript. TypeScript syntax like `as any` or `: string` in template expressions will throw `SyntaxError: Unexpected identifier`. Move type assertions to `setup()` or use computed properties.

**Resource MIME type.** Use `text/html;profile=mcp-app` (not plain `text/html`) so hosts recognize the resource as an MCP App.

#### Working example

[**Roady**](https://github.com/felixgeelhaar/roady) ships 14 MCP Apps (D3.js charts, task boards, interactive dashboards) built with mcp-go. See [`app/`](https://github.com/felixgeelhaar/roady/tree/main/app) for the Vue + Vite + singlefile setup and [`internal/infrastructure/mcp/`](https://github.com/felixgeelhaar/roady/tree/main/internal/infrastructure/mcp) for the Go resource registration.

---

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

- **[Roady](https://github.com/felixgeelhaar/roady)** — Planning-first system of record with 14 interactive MCP Apps (D3.js visualizations, task boards, dashboards)
- **Obvia** — Incident automation & AIOps tooling
- Internal MCP services and experiments

Want to add your project? Open a PR!

---

## Installation

```bash
go get go.klarlabs.de/mcp
```

Requires Go 1.25 or later.

---

## Documentation

- [API Reference](https://pkg.go.dev/go.klarlabs.de/mcp)
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

#### Return Type Flexibility

Tool handlers can return any JSON-serializable type. The framework automatically handles serialization:

```go
// String returns are used as-is
srv.Tool("greet").
    Handler(func(input GreetInput) (string, error) {
        return "Hello, " + input.Name, nil
    })

// Structs are automatically JSON-serialized
type StatusResult struct {
    Status  string `json:"status"`
    Count   int    `json:"count"`
    Healthy bool   `json:"healthy"`
}

srv.Tool("status").
    Handler(func(input StatusInput) (StatusResult, error) {
        return StatusResult{
            Status:  "ok",
            Count:   42,
            Healthy: true,
        }, nil
    })
// Response text: {"status":"ok","count":42,"healthy":true}

// Maps and slices work too
srv.Tool("list").
    Handler(func(input ListInput) (map[string]any, error) {
        return map[string]any{
            "items": []string{"a", "b", "c"},
            "total": 3,
        }, nil
    })
```

This ensures compliance with the MCP specification which requires the `text` field to always be a string.

#### Structured Content

Tools can return typed structured data alongside text content blocks using `OutputSchema` and `StructuredResult`:

```go
type TableOutput struct {
    Headers []string   `json:"headers"`
    Rows    [][]string `json:"rows"`
}

srv.Tool("extract_table").
    Description("Extract table data from a page").
    OutputSchema(TableOutput{}).
    Handler(func(ctx context.Context, input ExtractInput) (mcp.StructuredResult, error) {
        return mcp.StructuredResult{
            Content:           []mcp.Content{mcp.NewTextContent("Found 3 rows")},
            StructuredContent: map[string]any{"headers": []string{"name", "age"}, "rows": [][]string{{"Alice", "30"}}},
        }, nil
    })
```

The response includes both `content` (text blocks for display) and `structuredContent` (typed data matching the output schema). Clients that understand `structuredContent` can render it natively (tables, trees, etc.).

#### Dynamic Tool Registration

Add and remove tools at runtime, then notify connected clients:

```go
// Add a tool dynamically
srv.Tool("fill_form").Handler(fillFormHandler)

// Notify clients that the tool list changed
session := mcp.SessionFromContext(ctx)
session.NotifyToolListChanged()

// Remove a tool when it's no longer relevant
srv.RemoveTool("fill_form")
session.NotifyToolListChanged()

// Same pattern works for resources and prompts
srv.RemoveResource("config://temp")
session.NotifyResourceListChanged()

srv.RemovePrompt("onboarding")
session.NotifyPromptListChanged()
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

### Tool Metadata

Attach arbitrary metadata to tools via `_meta`, or use the `UIResource` shorthand for MCP Apps:

```go
// Arbitrary metadata
srv.Tool("my-tool").
    Meta(map[string]any{"custom": "data"}).
    Handler(myHandler)

// MCP Apps shorthand — sets _meta.ui.resourceUri
srv.Tool("visualize").
    UIResource("ui://my-app/dashboard").
    Handler(vizHandler)
```

The `_meta` field is included in both `tools/list` and `tools/call` JSON-RPC responses.

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
- `OTel()` - OpenTelemetry tracing and metrics
- `Audit()` - Request/response audit logging
- `Tracing()` - Correlation and trace ID propagation
- `OAuth2()` - JWT-based OAuth 2.0 authentication

### Enterprise Features

#### Horizontal Scaling with SessionStore

Persist sessions across server restarts for load-balanced deployments:

```go
// Redis-backed session store
store := redis.NewSessionStore(redisClient, 24*time.Hour)

mcp.ServeHTTP(ctx, srv, ":8080",
    mcp.WithSessionStore(store),
)

// In-memory store for single-instance deployments
mcp.ServeHTTP(ctx, srv, ":8080",
    mcp.WithSessionStore(mcp.NewInMemorySessionStore()),
)
```

#### Server Discovery

Clients can discover MCP servers via `/.well-known/mcp`:

```go
mcp.ServeHTTP(ctx, srv, ":8080",
    mcp.WithDiscovery(mcp.ServerDiscovery{
        Name:        "my-server",
        Description: "My MCP server",
    }),
)
```

#### Tasks for Long-Running Operations

Register async tasks that can be created, monitored, and canceled:

```go
srv.RegisterTask("long-task", "A long running task", func(ctx context.Context, input map[string]any) (*mcp.TaskResult, error) {
    // Task runs asynchronously
    return &mcp.TaskResult{Data: "completed"}, nil
})

// Create a task
task, _ := srv.Tasks().CreateTask(ctx, mcp.CreateTaskRequest{
    Name:   "long-task",
    Params: map[string]any{"key": "value"},
})

// List and cancel tasks
tasks, _ := srv.Tasks().ListTasks(10, "")
srv.Tasks().CancelTask(task.ID)
```

### Tool Annotations

Mark tools with behavioral hints for clients:

```go
srv.Tool("read-config").
    Description("Read configuration").
    ReadOnly().
    Handler(readHandler)

srv.Tool("delete-user").
    Description("Delete a user account").
    Destructive().
    Handler(deleteHandler)

srv.Tool("update-setting").
    Description("Update a setting").
    Idempotent().
    Handler(updateHandler)
```

### Completion Support

Provide autocomplete suggestions for prompt arguments and resource URIs:

```go
srv.PromptCompletion("code-review").
    Argument("language", func(ctx context.Context, value string) (*mcp.CompletionResult, error) {
        return &mcp.CompletionResult{
            Values: []string{"go", "python", "typescript"},
        }, nil
    })
```

### Bidirectional Communication

Servers can make requests back to clients via sessions:

```go
// Request LLM completion from client (sampling)
session := mcp.SessionFromContext(ctx)
result, _ := session.CreateMessage(ctx, mcp.CreateMessageRequest{
    Messages: []mcp.SamplingMessage{{Role: mcp.User, Content: mcp.Content{Type: "text", Text: "Summarize this"}}},
})

// Query client workspace roots
roots, _ := session.ListRoots(ctx)

// Send log messages to client
session.LogMessage(ctx, mcp.LoggingMessage{Level: mcp.Info, Data: "Processing complete"})

// Notify clients of resource changes
session.NotifyResourceUpdated(ctx, "config://app")

// Elicitation — ask the user for structured input mid-task
elicitor := mcp.ElicitFromContext(ctx)
if elicitor != nil {
    result, _ := elicitor.Elicit(ctx, &mcp.ElicitRequest{
        Message: "Multiple fields match 'Name'. Which one?",
        RequestedSchema: map[string]any{
            "type": "object",
            "properties": map[string]any{
                "field": map[string]any{"type": "string", "enum": []string{"First Name", "Last Name"}},
            },
        },
    })
    if result.Action == "accept" {
        selectedField := result.Content["field"]
        // Use selectedField...
    }
}

// Channels — push messages proactively into the AI session
channel := mcp.ChannelFromContext(ctx)
if channel != nil {
    channel.SendText("navigation", "Page navigated to /dashboard")
    channel.Send(&mcp.ChannelMessage{
        Channel:  "network",
        Content:  mcp.NewTextContent("API response received"),
        Data:     map[string]any{"status": 200, "url": "/api/users"},
        Priority: "high",
    })
}
```

### HTTP Transport

Serve over HTTP with Server-Sent Events:

```go
mcp.ServeHTTP(ctx, srv, ":8080",
    mcp.WithReadTimeout(30*time.Second),
    mcp.WithWriteTimeout(30*time.Second),
)
```

### WebSocket Transport

Serve over WebSocket for full-duplex communication:

```go
mcp.ServeWebSocket(ctx, srv, ":8081",
    mcp.WithWebSocketReadTimeout(30*time.Second),
    mcp.WithWebSocketWriteTimeout(30*time.Second),
)
```

### gRPC Transport

Serve over gRPC with Protocol Buffers for high-performance service-to-service communication:

```go
mcp.ServeGRPC(ctx, srv, ":9090",
    mcp.WithGRPCShutdownTimeout(10*time.Second),
)
```

### Client SDK

Consume MCP servers programmatically:

```go
transport, _ := client.NewStdioTransport("./my-server")
c := client.New(transport, client.WithTimeout(10*time.Second))

info, _ := c.Initialize(ctx)
tools, _ := c.ListTools(ctx)
result, _ := c.CallTool(ctx, "search", SearchInput{Query: "hello"})
```

### Testing

Test MCP servers without transport overhead:

```go
func TestMyServer(t *testing.T) {
    srv := mcp.NewServer(mcp.ServerInfo{Name: "test", Version: "1.0.0"})
    srv.Tool("greet").Handler(greetHandler)

    tc := testutil.NewTestClient(t, srv)
    defer tc.Close()

    result, err := tc.CallTool("greet", map[string]any{"name": "World"})
    if err != nil {
        t.Fatal(err)
    }
    // result == "Hello, World!"
}
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
