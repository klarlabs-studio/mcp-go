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

**Notifications:**
- `notifications/progress` - Progress for long-running tools
- `notifications/cancelled` - Request cancellation
- `notifications/message` - Log messages (server → client)
- `notifications/resources/updated` - Resource change notification
- `notifications/resources/list_changed` - Resource list changed
- `notifications/tools/list_changed` - Tool list changed
- `notifications/prompts/list_changed` - Prompt list changed
- `notifications/roots/list_changed` - Roots changed (client → server)

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
