# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

**mcp-go** is a Go framework for building Model Context Protocol (MCP) servers. The goal is to provide Gin-like developer experience for MCP, enabling Go developers to expose tools, resources, and prompts with strong typing, middleware support, and production-ready defaults.

## Build Commands

```bash
# Run all tests
go test ./...

# Run tests with race detection
go test -race ./...

# Run tests with coverage
go test -cover ./...

# Run specific package tests
go test -v ./server/...

# Run specific test
go test -run TestServer_Tool ./server

# Build example
go build ./examples/basic

# Format code
gofmt -w .

# Lint (when golangci-lint is installed)
golangci-lint run
```

## Architecture

```
mcp-go/
├── mcp.go              # Public API facade - main entry point
├── mcp_test.go         # Integration tests for the public API
├── go.mod              # Module definition
│
├── protocol/           # MCP protocol layer (JSON-RPC 2.0)
│   ├── errors.go       # MCP error types and constructors
│   ├── messages.go     # Request/Response types
│   ├── constants.go    # Protocol version and method names
│   └── context.go      # Request metadata context
│
├── server/             # Core server implementation
│   ├── server.go       # Server aggregate root
│   ├── tool.go         # Tool and ToolBuilder
│   ├── resource.go     # Resource and ResourceBuilder
│   ├── prompt.go       # Prompt and PromptBuilder
│   ├── annotations.go  # Tool/Resource/Prompt annotations
│   ├── progress.go     # Progress reporting for streaming
│   ├── session.go      # Bidirectional session management
│   ├── sampling.go     # Sampling types (LLM completion requests)
│   ├── roots.go        # Roots types (workspace awareness)
│   ├── logging.go      # Logging types (server→client logs)
│   ├── cancellation.go # Request cancellation management
│   └── subscriptions.go # Resource subscription management
│
├── schema/             # JSON Schema generation
│   └── schema.go       # Struct to JSON Schema with validation
│
├── middleware/         # Request middleware
│   ├── chain.go        # Middleware chain composition
│   ├── recover.go      # Panic recovery
│   ├── requestid.go    # Request ID injection
│   ├── timeout.go      # Request timeout
│   ├── logging.go      # Structured logging
│   ├── auth.go         # Authentication (API key, Bearer)
│   ├── ratelimit.go    # Rate limiting
│   └── sizelimit.go    # Request size limits
│
├── transport/          # Transport implementations
│   ├── transport.go    # Transport interface
│   ├── stdio.go        # stdio transport for CLI tools
│   ├── http.go         # HTTP + SSE transport
│   ├── websocket.go    # WebSocket transport
│   ├── cors.go         # CORS middleware
│   └── shutdown.go     # Graceful shutdown manager
│
├── client/             # MCP client SDK
│   └── client.go       # Client for consuming MCP servers
│
├── testutil/           # Testing utilities
│   └── testutil.go     # Helpers for testing MCP servers
│
└── examples/           # Example servers
    ├── basic/          # Basic stdio server
    ├── http/           # HTTP server example
    ├── middleware/     # Middleware usage example
    ├── resources/      # Resources example
    └── prompts/        # Prompts example
```

## Key Patterns

### Typed Handlers
Handlers accept typed structs and return typed results:
```go
type SearchInput struct {
    Query string `json:"query" jsonschema:"required"`
}

srv.Tool("search").
    Description("Search for items").
    Handler(func(input SearchInput) ([]Result, error) {
        return results, nil
    })
```

### Context Support
Handlers can optionally receive context:
```go
srv.Tool("fetch").Handler(func(ctx context.Context, input Input) (Result, error) {
    // Use ctx for cancellation, deadlines, etc.
})
```

### Middleware Chain
Gin-style middleware wrapping:
```go
type Middleware func(next HandlerFunc) HandlerFunc
```

## TDD Workflow

Follow Red-Green-Refactor:
1. Write failing test (`test: add failing test for X`)
2. Implement minimal code to pass (`feat: implement X`)
3. Refactor if needed (`refactor: clean up X`)

Use table-driven tests:
```go
func TestX(t *testing.T) {
    tests := []struct {
        name string
        input any
        want any
        wantErr bool
    }{...}

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {...})
    }
}
```

## Known Failure Modes

- **Worktree subagents spawn from an old base.** `Agent(isolation: "worktree")` branches from `main`/the last release tag, **not** the current working branch. Every parallel worktree agent must run `git merge --ff-only <target-branch>` as its first step, and be scoped to **disjoint files** so integration stays cherry-pick/fast-forward-clean. Do the shared-dispatcher (`mcp.go`) / version-integration work yourself, sequentially — never fan it out.
- **`main` is protected (`strict` + 4 required checks: Lint/Build/Test/Security), but CI only runs on PRs targeting `main`.** Stacked PRs that target intermediate branches can never satisfy the gate. To land a stack: retarget each PR to `main` bottom-up, then merge. `enforce_admins` is off, so `gh pr merge --admin` is the intended path once the work is verified green locally — but the harness safety classifier will block the bypass until the user explicitly authorizes it.
- **Stale LSP diagnostics leak from `.claude/worktrees/` copies** into the main-checkout view (phantom undefined symbols / unused imports). Don't trust them mid-parallel-run — confirm with a real `go build` / `go test`.
- **Stale Dependabot "fixed in vX" alerts are a downgrade trap (fleet).** When a consumer's mcp-go bump PR already pulls a transitive dep (`golang.org/x/crypto`, `x/net`, …) *past* the alert's fix version, the alert on `main` is stale and clears once the bump merges. `go get <pkg>@<fix-version>` to "fix" it then *downgrades* the dep — and can drag `go.klarlabs.de/mcp` itself back a version via MVS. Merge the bump; never `go get` a lower pin against a stale alert.
- **npm trusted publishing (OIDC): no `registry-url`, npm ≥ 11.5.1 — and it's usually the npm-account config, not the workflow (fleet).** `actions/setup-node`'s `registry-url` writes an `.npmrc` that conflicts with OIDC auth → `ENEEDAUTH`/404. The correct GitHub Actions setup is: `setup-node` **without** `registry-url`, npm ≥ 11.5.1 (force it — Node's bundled npm may be older; `npm 12.x` had OIDC trouble, pin `npm@^11.5.1`), `permissions: id-token: write`, `npm publish --provenance`, **no** `NODE_AUTH_TOKEN`. A `404` on publish = trusted-publisher config *mismatch* (org/repo/workflow-file/environment must match exactly); `ENEEDAUTH` = npm found *no effective* trusted-publisher config → fell back to a token that isn't there. Trusted publishing must be configured on npmjs for **every** package (per-platform scoped packages too), which the workflow can't verify. Once the workflow matches npm's docs and still fails, **stop iterating on it** — the gap is the npm-account config; hand back the diagnostic rather than cutting more release tags (the warden saga burned 7 binaries-only tags this way).
- **`--admin` merge authorization is per-batch, not standing.** User-authorizing an admin bypass for one set of PRs does NOT let the safety classifier admin-merge later *new* agent-authored PRs — those need fresh authorization or a normal `--auto` merge once CI passes.
- **Local `go.mod` is NOT authoritative for fleet-version work.** Local checkouts drift behind origin (a prior session may have already bumped the whole fleet). Before assessing "what version is X on" or planning a bump, `git fetch origin` and read `origin/main:go.mod` — not the working-tree `go.mod`. Fleet-bump subagents MUST branch from `origin/main` (step 0: `git fetch origin && git reset --hard origin/main`); a stale base makes their PR **CONFLICTING** (hit briefkasten/scout/nox during the v1.24 sweep).
- **Use isolated `git worktree`s for fleet ops when a background process may be active.** A concurrent process (another session / `/loop` / script) running `git checkout` inside a shared checkout silently discards uncommitted work — it wiped local briefkasten/scout rebases mid-sweep. A worktree has its own HEAD/index and is immune. The mnemos agent recovered exactly this way.
- **`git push --force-with-lease` is classifier-blocked.** To replace a conflicting/stale PR branch, don't force-push over it — close the PR with `gh pr close <n> --delete-branch`, then push a **fresh-named** branch (non-force) and open a new PR. (Same pattern the coverctl/briefkasten/scout/nox redos used.)

## Coverage Targets

Coverage is enforced via `coverctl check`. See `.coverctl.yaml` for thresholds.

| Package | Current | Target |
|---------|---------|--------|
| server | 88.6% | 80%+ |
| schema | 84.4% | 80%+ |
| client | 81.6% | 80%+ |
| transport | 75.3% | 75%+ |
| middleware | 74.7% | 70%+ |
| testutil | 74.4% | 70%+ |
| protocol | 69.7% | 45%+ |

## MCP Methods Implemented

**Server Methods (Client → Server):**
- `initialize` - Server initialization handshake
- `tools/list` - List available tools
- `tools/call` - Execute a tool
- `resources/list` - List available resources
- `resources/read` - Read resource content
- `resources/subscribe` - Subscribe to resource updates
- `resources/unsubscribe` - Unsubscribe from resource updates
- `prompts/list` - List available prompts
- `prompts/get` - Get prompt with arguments
- `logging/setLevel` - Set minimum log level
- `ping` - Health check

**Client Methods (Server → Client):**
- `sampling/createMessage` - Server requests LLM completion
- `roots/list` - Server requests workspace roots
- `elicitation/create` - Server requests structured input from user

**Notifications:**
- `notifications/progress` - Progress for long-running tools
- `notifications/cancelled` - Request cancellation
- `notifications/message` - Log messages (server → client)
- `notifications/resources/updated` - Resource change notification
- `notifications/resources/list_changed` - Resource list changed
- `notifications/tools/list_changed` - Tool list changed
- `notifications/prompts/list_changed` - Prompt list changed
- `notifications/roots/list_changed` - Roots changed (client → server)
- `notifications/channel/message` - Server pushes channel message to client

## v1.0 Features (Complete)

**Core:**
- [x] Server core with info/capabilities
- [x] Tool registration with builder pattern and annotations
- [x] Resource registration with URI templates
- [x] Prompt registration with arguments
- [x] Typed handler validation
- [x] JSON Schema generation with runtime validation

**Transports:**
- [x] stdio transport for CLI tools
- [x] HTTP + SSE transport
- [x] WebSocket transport
- [x] Graceful shutdown with connection draining

**Middleware:**
- [x] Middleware chain execution
- [x] Recover (panic recovery)
- [x] RequestID (request tracing)
- [x] Timeout (request deadlines)
- [x] Logging (structured logging)
- [x] Auth (API key, Bearer token)
- [x] RateLimit (request throttling)
- [x] SizeLimit (request size limits)
- [x] CORS (cross-origin support)

**Client SDK:**
- [x] Full client for consuming MCP servers
- [x] Typed tool calls and resource reads
- [x] Connection management

**Testing:**
- [x] testutil package for testing MCP servers

## v1.1 Features (Complete)

**Bidirectional Communication:**
- [x] Session management for server-to-client requests
- [x] Sampling - Server requests LLM completions from client
- [x] Roots - Workspace awareness (`roots/list`)
- [x] Logging notifications - Server-to-client log messages
- [x] Cancellation - Cancel in-progress requests
- [x] Resource subscriptions - Subscribe to resource changes
- [x] List change notifications for tools, resources, prompts

## v1.2 Features (Complete)

**Completion Support:**
- [x] `completion/complete` method for autocomplete
- [x] Prompt completion handlers with fluent API
- [x] Resource completion handlers with fluent API
- [x] Max 100 values enforced per MCP spec

**Resource Templates:**
- [x] `resources/templates/list` method
- [x] ResourceTemplateInfo type for template metadata

**Observability:**
- [x] OpenTelemetry middleware for tracing and metrics
- [x] Request spans with method and service attributes
- [x] Request count, duration, and error metrics
- [x] Helper functions: SpanFromContext, AddSpanEvent, SetSpanAttribute

## v1.9 Features (Complete)

**Structured Content:**
- [x] `OutputSchema()` builder method for typed output schemas
- [x] `StructuredResult` type with content + structuredContent
- [x] `outputSchema` advertised in `tools/list` responses

**Dynamic Registration:**
- [x] `RemoveTool()`, `RemoveResource()`, `RemovePrompt()` methods
- [x] `listChanged: true` capability advertisement

**Elicitation:**
- [x] `elicitation/create` method (server → client)
- [x] `ElicitRequest`, `ElicitResult` types
- [x] `ElicitFromContext(ctx)` context helper
- [x] Client capability negotiation

**Channels:**
- [x] `notifications/channel/message` notification
- [x] `ChannelMessage`, `ChannelSender` types
- [x] `ChannelFromContext(ctx)` context helper
- [x] `SendText()` convenience method
