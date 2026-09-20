# Deprecations

mcp-go tracks the MCP specification. The 2026-07-28 stateless revision retires
three server-initiated features. They remain **fully functional for a 12-month
window** so existing servers keep working — nothing is removed in v1. This page
documents each deprecation and its migration.

Deprecated symbols carry a Go `// Deprecated:` marker, so `gopls`, `staticcheck`,
and pkg.go.dev flag their use.

## Prefer modern clients

New clients default to the published stateless revision (`2026-07-28`). Prefer:

```go
c := client.New(tr)
info, err := c.Connect(ctx) // Discover, with Initialize fallback
```

`Initialize` remains for initialize-era peers (`2025-11-25` and earlier). Pass
`client.WithProtocolVersion(protocol.MCPVersion)` when you only speak the
legacy handshake.

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

For mid-call user input on a stateless server, use Multi Round-Trip Requests
(MRTR) via `mcp.ElicitFromContext` / `input_required` rather than sampling.

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
(`io.modelcontextprotocol/logLevel`) and is applied per request. A modern
request that omits that field does not receive `notifications/message`.

## Example that still exercises deprecated APIs

`examples/session` intentionally calls sampling, roots, and logging so the
legacy path stays covered. New servers should follow the migrations above; see
also `examples/typed-client` for a Discover/Connect-first HTTP client.

## Timeline

| Milestone            | Behavior                                                        |
| -------------------- | --------------------------------------------------------------- |
| Now (v1)             | Deprecated, fully functional. Compiler/tooling warnings only.   |
| +12 months (~2027-07)| Eligible for removal in a future major (v2).                    |

The stateless request path is the **default for `ServeHTTP`** (`WithStreamable`,
MCP 2026-07-28). `stdio` servers and `WithStreamableStateful()` /
`WithLegacyHTTP()` are unaffected. Deprecated sampling/roots/logging APIs stay
functional for the 12-month window.
