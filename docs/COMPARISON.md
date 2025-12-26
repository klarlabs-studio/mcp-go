# Comparison: MCP Go SDK vs mark3labs/mcp-go vs mcp-go

This document explains the differences between the three main ways to build Model Context Protocol (MCP) servers in Go, and helps you choose the right abstraction level for your use case.

---

## TL;DR

- **MCP Go SDK** → protocol correctness (lowest-level)
- **mark3labs/mcp-go** → community SDK convenience (still SDK-style)
- **mcp-go** → application framework (typed DX + production defaults)

If you know HTTP:

- MCP Go SDK ≈ `net/http`
- mcp-go ≈ **Gin**

---

## The MCP stack (layered model)

```
┌───────────────────────────────┐
│ mcp-go (this project)         │ ← Framework
├───────────────────────────────┤
│ mark3labs/mcp-go              │ ← Community SDK
├───────────────────────────────┤
│ MCP Go SDK                    │ ← Protocol SDK
└───────────────────────────────┘
```

These projects are complementary, not mutually exclusive.
Each exists at a different layer of abstraction.

---

## 1) "Hello Tool" — registering a tool

### MCP Go SDK (protocol-level)

You typically:

- decode MCP messages
- switch on message types
- dispatch tool calls manually

```go
// PSEUDO: protocol SDK style (manual dispatch)
for {
    raw := readMessage()
    msg := mcp.Decode(raw)

    switch msg.Type {
    case "tools/call":
        name := msg.Params["name"].(string)
        args := msg.Params["arguments"].(map[string]any)

        switch name {
        case "hello":
            // manually decode args
            // call handler
            // encode response
        default:
            // return tool not found error
        }
    }
}
```

You get correctness + control, but you write the app framework yourself.

---

### mark3labs/mcp-go (SDK convenience)

You typically get:

- a server object
- a way to register tools

…but handler inputs are commonly still untyped and validation is manual.

```go
// PSEUDO: SDK style
srv := mcp.NewServer()

srv.RegisterTool("hello", func(ctx context.Context, args map[string]any) (any, error) {
    name, _ := args["name"].(string)
    return "Hello " + name, nil
})

srv.ServeStdio()
```

### mcp-go (this project — framework)

You write:

- typed input struct
- handler receives typed data
- schema + validation happen automatically

```go
srv := mcp.NewServer(mcp.ServerInfo{Name: "example", Version: "0.1.0"})

type HelloInput struct {
    Name string `json:"name"`
}

srv.Tool("hello").
    Description("Say hello").
    Handler(func(ctx context.Context, in HelloInput) (any, error) {
        return "Hello " + in.Name, nil
    })

mcp.ServeStdio(ctx, srv)
```

---

## 2) Typed input + validation

### MCP Go SDK

You validate manually.

```go
args := msg.Params["arguments"].(map[string]any)

name, ok := args["name"].(string)
if !ok || name == "" {
    return mcp.ErrorInvalidParams("name is required")
}
```

### mark3labs/mcp-go

Still typically manual (or you add your own validation library).

```go
srv.RegisterTool("hello", func(ctx context.Context, args map[string]any) (any, error) {
    name, _ := args["name"].(string)
    if name == "" {
        return nil, errors.New("name is required")
    }
    return "Hello " + name, nil
})
```

### mcp-go

Use struct tags + schema generation, and invalid input never reaches your handler.

```go
type SearchInput struct {
    Query string `json:"query" jsonschema:"minLength=1"`
    Limit int    `json:"limit" jsonschema:"minimum=1,maximum=100"`
}

srv.Tool("search").
    Handler(func(ctx context.Context, in SearchInput) ([]Result, error) {
        // in.Query is guaranteed non-empty
        // in.Limit is guaranteed 1..100
        return svc.Search(ctx, in.Query, in.Limit)
    })
```

---

## 3) Middleware (auth, timeouts, logging)

### MCP Go SDK

No standard middleware concept; you wrap handlers yourself.

```go
handler := func(ctx context.Context, msg mcp.Message) (mcp.Message, error) {
    // ...
}

handler = withTimeout(5*time.Second, handler)
handler = withAuth(authz, handler)
handler = withLogging(log, handler)
```

### mark3labs/mcp-go

Often similar: you compose wrappers around your registrations.

```go
srv.RegisterTool("search", withAuth(func(ctx context.Context, args map[string]any) (any, error) {
    // ...
}))
```

### mcp-go

First-class middleware chain (Gin-style).

```go
srv.Use(
    middleware.Recover(),
    middleware.RequestID(),
    middleware.Timeout(5*time.Second),
    middleware.Logging(logger),
    middleware.Auth(func(ctx context.Context, r *mcp.Request) (*mcp.Principal, error) {
        return verifyBearer(ctx, r.Headers.Get("Authorization"))
    }),
)
```

---

## 4) Transport (stdio vs HTTP)

### MCP Go SDK

You usually wire your own transport loop or pick one helper.

```go
// PSEUDO
t := mcp.NewStdioTransport(os.Stdin, os.Stdout)
t.Serve(ctx, handler)
```

### mark3labs/mcp-go

Typically provides stdio server helpers; HTTP may be separate or DIY.

```go
srv := mcp.NewServer()
// register tools...
srv.ServeStdio()
```

### mcp-go

Same server, multiple transports.

```go
// stdio
mcp.ServeStdio(ctx, srv)

// HTTP + SSE
mcp.ServeHTTP(ctx, srv, ":8080")
```

---

## 5) Resources (URI templates & resource responses)

### MCP Go SDK (protocol-level)

You typically:

- parse the URI yourself
- extract path segments manually
- construct the resource response by hand

```go
// PSEUDO: protocol SDK style
if msg.Type == "resources/read" {
    uri := msg.Params["uri"].(string)

    // manual parsing
    id := strings.TrimPrefix(uri, "incidents://")

    data, err := loadIncident(id)
    if err != nil {
        return mcp.ErrorNotFound("incident not found")
    }

    return mcp.Resource{
        URI:  uri,
        MIME: "application/json",
        Data: data,
    }, nil
}
```

### mark3labs/mcp-go (SDK convenience)

You usually register a handler, but URI parsing and validation are still manual.

```go
// PSEUDO: SDK style
srv.RegisterResource("incidents", func(ctx context.Context, uri string) (mcp.Resource, error) {
    id := strings.TrimPrefix(uri, "incidents://")

    inc, err := loadIncident(id)
    if err != nil {
        return mcp.Resource{}, err
    }

    return mcp.Resource{
        URI:  uri,
        MIME: "application/json",
        Data: inc,
    }, nil
})
```

### mcp-go (this project — framework)

You declare a URI template, get structured params, and return a typed resource.

```go
srv.Resource("incidents://{id}").
    Name("Incident").
    Description("Incident resource").
    MimeType("application/json").
    Handler(func(ctx context.Context, uri string, params map[string]string) (*mcp.ResourceContent, error) {
        id := params["id"]

        inc, err := loadIncident(id)
        if err != nil {
            return nil, err
        }

        data, _ := json.Marshal(inc)
        return &mcp.ResourceContent{
            URI:      uri,
            MimeType: "application/json",
            Text:     string(data),
        }, nil
    })
```

**Key difference:**

- No manual URI parsing
- Clear URI contracts via templates
- Consistent error handling

---

## 6) Prompts (structured, reusable prompts)

### MCP Go SDK (protocol-level)

You manage prompt definitions and parameters yourself.

```go
// PSEUDO: protocol SDK style
if msg.Type == "prompts/get" && msg.Params["name"] == "summarize_incident" {
    return mcp.Prompt{
        Name:    "summarize_incident",
        Content: "Summarize incident {{id}} with severity {{severity}}",
    }, nil
}
```

### mark3labs/mcp-go (SDK convenience)

Prompt registration is usually lightweight but still unstructured.

```go
// PSEUDO: SDK style
srv.RegisterPrompt("summarize_incident", func(ctx context.Context, args map[string]any) (string, error) {
    return fmt.Sprintf(
        "Summarize incident %v with severity %v",
        args["id"],
        args["severity"],
    ), nil
})
```

### mcp-go (this project — framework)

Prompts are first-class, with declared arguments and self-documenting.

```go
srv.Prompt("summarize_incident").
    Description("Summarize an incident for an LLM").
    Argument("id", "Incident ID", true).
    Argument("severity", "Incident severity level", true).
    Handler(func(ctx context.Context, args map[string]string) (*mcp.PromptResult, error) {
        return &mcp.PromptResult{
            Description: fmt.Sprintf("Summary for incident %s", args["id"]),
            Messages: []mcp.PromptMessage{
                {
                    Role: "user",
                    Content: mcp.TextContent{
                        Type: "text",
                        Text: fmt.Sprintf("Summarize incident %s with severity %s", args["id"], args["severity"]),
                    },
                },
            },
        }, nil
    })
```

**Key difference:**

- Declared arguments with descriptions and required flags
- Automatic schema exposure for introspection
- Reusable across tools, resources, and agents

---

## Feature comparison (summary)

| Capability | MCP Go SDK | mark3labs/mcp-go | mcp-go |
|------------|------------|------------------|--------|
| Abstraction level | Protocol | SDK | Framework |
| Typed handlers | ❌ | ❌ | ✅ |
| Auto JSON Schema | ❌ | ❌ | ✅ |
| Input validation | ❌ | Manual | Automatic |
| URI template resources | ❌ | ❌ | ✅ |
| Structured prompts | ❌ | ❌ | ✅ |
| Middleware | ❌ | ❌ | ✅ |
| Auth / rate limit hooks | ❌ | ❌ | ✅ |
| Transport abstraction | ❌ | Limited | Pluggable |
| Production defaults | ❌ | ❌ | ✅ |
| Best for | SDK authors | SDK users | App developers |

---

## When to choose what

### Choose MCP Go SDK if you…

- need maximum control
- want to stay closest to the spec
- are building another SDK / client

### Choose mark3labs/mcp-go if you…

- want a popular community SDK
- are experimenting / prototyping quickly
- prefer "bring your own architecture"

### Choose mcp-go if you…

- want typed handlers + schema + validation
- need middleware & production defaults
- deploy MCP servers as real services

---

## Migration

Coming from mark3labs/mcp-go?

See: [Migration Guide](./MIGRATION.md)
