# Deprecations

mcp-go tracks the MCP specification. The 2026-07-28 stateless revision retires
three server-initiated features. They remain **fully functional for a 12-month
window** so existing servers keep working — nothing is removed in v1. This page
documents each deprecation and its migration.

Deprecated symbols carry a Go `// Deprecated:` marker, so `gopls`, `staticcheck`,
and pkg.go.dev flag their use.

## Sampling — `Session.CreateMessage`, `Session.CreateMessageWithTools`

Server-initiated sampling asks the connected client to run an LLM completion on
the server's behalf. The stateless model removes server→client requests.

**Migrate to a provider API.** Call your LLM provider directly from the handler:

```go
srv.Tool("summarize").Handler(func(ctx context.Context, in Input) (string, error) {
    // Before: sess.CreateMessage(ctx, &mcp.CreateMessageRequest{...})
    // After: call the provider SDK you already control.
    resp, err := llm.Complete(ctx, in.Text) // your Anthropic/OpenAI/etc. client
    if err != nil {
        return "", err
    }
    return resp.Text, nil
})
```

This keeps the completion in-process, removes the client round-trip, and makes
the handler testable without a sampling-capable client.

## Roots — `Session.ListRoots`

Server-initiated roots asks the client for its workspace directories. The
stateless model removes the out-of-band request.

**Migrate to explicit inputs.** Receive the workspace via tool parameters,
resource URIs, or configuration:

```go
type Input struct {
    Roots []string `json:"roots" jsonschema:"description=Workspace directories to operate on"`
}

srv.Tool("scan").Handler(func(ctx context.Context, in Input) (Result, error) {
    // Before: roots, _ := sess.ListRoots(ctx)
    // After: the client passes roots as an explicit, declared parameter.
    return scan(in.Roots)
})
```

## Logging — `Session.Log` (and `Debug`/`Info`/`Notice`/`Warning`/`Error`/`Critical`/`Alert`/`Emergency`)

Server→client log notifications route diagnostics through the transport. The
stateless model removes them.

**Migrate to stderr + OpenTelemetry.** Write logs to stderr (stdio transport
keeps stderr free for exactly this) and/or emit them through the OTel middleware:

```go
// Before: sess.Info("scan", "starting")
// After: structured logging to stderr, spans/metrics via middleware.
slog.InfoContext(ctx, "starting", "tool", "scan")
```

`Session.SetLogLevel` / `Session.LogLevel` are **not** deprecated — in the modern
stateless model the client's desired log level travels in each request's `_meta`
(`io.modelcontextprotocol/logLevel`) and is applied per request.

## Timeline

| Milestone            | Behavior                                                        |
| -------------------- | -------------------------------------------------------------- |
| Now (v1)             | Deprecated, fully functional. Compiler/tooling warnings only.  |
| +12 months           | Eligible for removal in a future major (v2).                   |

The stateless request path itself stays opt-in (`WithStreamableStateless`) in
v1; it is not the default and does not change existing servers.
