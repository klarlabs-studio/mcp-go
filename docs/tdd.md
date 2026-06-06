# Technical Design Document (TDD)

**Product:** mcp-go
**Version:** 0.1.0 (MVP)
**Author:** Felix Geelhaar
**Date:** 2025-12-25
**Status:** Draft

---

## 1. Overview

This document describes the technical architecture and implementation details for mcp-go, a Go framework for building Model Context Protocol (MCP) servers with Gin-like developer experience.

### 1.1 Goals

- Provide typed handlers with automatic JSON Schema generation
- Implement Gin-style middleware chain for cross-cutting concerns
- Support pluggable transports (stdio, HTTP+SSE)
- Deliver production-ready defaults (panic recovery, graceful shutdown)
- Achieve MCP spec compliance

### 1.2 Non-Goals

- Client SDK (future scope)
- WebSocket transport (post-MVP)
- Streaming tool responses (post-MVP)
- Code generation tooling

### 1.3 Design Principles

| Principle | Description |
|-----------|-------------|
| **Idiomatic Go** | Follow Go conventions, use stdlib patterns, accept interfaces return structs |
| **Domain-Driven Design** | Clear bounded contexts, ubiquitous language, separation of concerns |
| **Test-Driven Development** | Red-Green-Refactor cycle, tests as documentation, high coverage |
| **Explicit over Magic** | No hidden behavior, clear data flow, obvious configuration |
| **Composition over Inheritance** | Small interfaces, functional options, middleware chains |

---

## 2. Domain-Driven Design

### 2.1 Ubiquitous Language

| Term | Definition |
|------|------------|
| **Server** | The MCP server instance that handles client connections |
| **Tool** | A callable function exposed to MCP clients |
| **Resource** | Read-only data accessible via URI templates |
| **Prompt** | A reusable prompt template with parameters |
| **Transport** | Communication layer (stdio, HTTP+SSE) |
| **Handler** | Function that processes requests and returns responses |
| **Middleware** | Cross-cutting concern that wraps handlers |
| **Schema** | JSON Schema describing input/output structure |

### 2.2 Bounded Contexts

```
┌─────────────────────────────────────────────────────────────────────┐
│                        MCP-GO Framework                              │
├─────────────────────────────────────────────────────────────────────┤
│                                                                      │
│  ┌──────────────────┐  ┌──────────────────┐  ┌──────────────────┐   │
│  │   Server Core    │  │    Protocol      │  │    Transport     │   │
│  │   (Aggregate)    │  │   (Infra)        │  │    (Infra)       │   │
│  │                  │  │                  │  │                  │   │
│  │  - Server        │  │  - JSON-RPC      │  │  - stdio         │   │
│  │  - Tool          │  │  - Messages      │  │  - HTTP+SSE      │   │
│  │  - Resource      │  │  - Errors        │  │  - Transport IF  │   │
│  │  - Prompt        │  │                  │  │                  │   │
│  └────────┬─────────┘  └────────┬─────────┘  └────────┬─────────┘   │
│           │                     │                     │              │
│  ┌────────┴─────────────────────┴─────────────────────┴─────────┐   │
│  │                     Middleware Layer                          │   │
│  │  (Cross-cutting concerns: logging, recovery, auth, timeout)  │   │
│  └──────────────────────────────────────────────────────────────┘   │
│                                                                      │
│  ┌──────────────────┐  ┌──────────────────┐                         │
│  │     Schema       │  │   Observability  │                         │
│  │   (Supporting)   │  │   (Supporting)   │                         │
│  │                  │  │                  │                         │
│  │  - Generation    │  │  - Logging       │                         │
│  │  - Validation    │  │  - Metrics       │                         │
│  │  - Reflection    │  │  - Tracing       │                         │
│  └──────────────────┘  └──────────────────┘                         │
│                                                                      │
└─────────────────────────────────────────────────────────────────────┘
```

### 2.3 Package Structure (DDD-Aligned)

```
go.klarlabs.de/mcp/
│
├── mcp.go                    # Public API facade (exports)
│
├── server/                   # CORE DOMAIN - Server Aggregate
│   ├── server.go             # Server aggregate root
│   ├── server_test.go
│   ├── options.go            # Functional options pattern
│   ├── options_test.go
│   ├── tool.go               # Tool entity
│   ├── tool_test.go
│   ├── resource.go           # Resource entity
│   ├── resource_test.go
│   ├── prompt.go             # Prompt entity
│   ├── prompt_test.go
│   ├── handler.go            # Handler interfaces
│   └── handler_test.go
│
├── protocol/                 # INFRASTRUCTURE - MCP Protocol
│   ├── jsonrpc.go            # JSON-RPC 2.0 implementation
│   ├── jsonrpc_test.go
│   ├── messages.go           # MCP message types
│   ├── messages_test.go
│   ├── errors.go             # MCP error codes
│   ├── errors_test.go
│   └── constants.go          # Protocol constants
│
├── transport/                # INFRASTRUCTURE - Transport Layer
│   ├── transport.go          # Transport interface
│   ├── stdio.go              # stdio transport
│   ├── stdio_test.go
│   ├── http.go               # HTTP+SSE transport
│   └── http_test.go
│
├── middleware/               # APPLICATION - Cross-cutting Concerns
│   ├── middleware.go         # Middleware types
│   ├── middleware_test.go
│   ├── chain.go              # Middleware chaining
│   ├── chain_test.go
│   ├── recover.go            # Panic recovery
│   ├── recover_test.go
│   ├── requestid.go          # Request ID injection
│   ├── requestid_test.go
│   ├── timeout.go            # Request timeout
│   ├── timeout_test.go
│   └── logging.go            # Structured logging
│   └── logging_test.go
│
├── schema/                   # SUPPORTING - JSON Schema
│   ├── schema.go             # Schema generation
│   ├── schema_test.go
│   ├── reflect.go            # Reflection utilities
│   ├── reflect_test.go
│   ├── validate.go           # Input validation
│   └── validate_test.go
│
├── internal/                 # PRIVATE - Internal utilities
│   ├── context/              # Context helpers
│   │   ├── context.go
│   │   └── context_test.go
│   └── sync/                 # Concurrency utilities
│       ├── pool.go
│       └── pool_test.go
│
├── examples/                 # Example implementations
│   ├── basic/
│   │   └── main.go
│   ├── tools/
│   │   └── main.go
│   └── middleware/
│       └── main.go
│
└── testutil/                 # Test utilities (exported for users)
    ├── server.go             # Test server helpers
    └── transport.go          # Mock transports
```

---

## 3. Test-Driven Development Workflow

### 3.1 TDD Cycle

```
┌─────────────────────────────────────────────────────────────────┐
│                    RED-GREEN-REFACTOR CYCLE                      │
├─────────────────────────────────────────────────────────────────┤
│                                                                  │
│   ┌─────────┐    ┌─────────┐    ┌───────────┐                   │
│   │   RED   │ → │  GREEN  │ → │ REFACTOR  │ → (repeat)         │
│   │         │    │         │    │           │                   │
│   │ Write   │    │ Write   │    │ Improve   │                   │
│   │ failing │    │ minimal │    │ code      │                   │
│   │ test    │    │ code    │    │ quality   │                   │
│   └─────────┘    └─────────┘    └───────────┘                   │
│                                                                  │
│   Commit:        Commit:        Commit:                          │
│   test: add      feat: impl     refactor: clean                  │
│   failing test   feature        implementation                   │
│                                                                  │
└─────────────────────────────────────────────────────────────────┘
```

### 3.2 Test Structure

Follow table-driven tests (idiomatic Go):

```go
func TestTool_Execute(t *testing.T) {
    tests := []struct {
        name    string
        input   any
        want    any
        wantErr error
    }{
        {
            name:  "valid input returns result",
            input: SearchInput{Query: "test"},
            want:  &SearchResult{Count: 1},
        },
        {
            name:    "empty query returns error",
            input:   SearchInput{Query: ""},
            wantErr: ErrInvalidParams("query required"),
        },
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            got, err := tool.Execute(context.Background(), tt.input)

            if tt.wantErr != nil {
                if !errors.Is(err, tt.wantErr) {
                    t.Errorf("Execute() error = %v, wantErr %v", err, tt.wantErr)
                }
                return
            }

            if err != nil {
                t.Fatalf("Execute() unexpected error: %v", err)
            }

            if diff := cmp.Diff(tt.want, got); diff != "" {
                t.Errorf("Execute() mismatch (-want +got):\n%s", diff)
            }
        })
    }
}
```

### 3.3 Test Categories

| Category | Location | Purpose | Tools |
|----------|----------|---------|-------|
| **Unit** | `*_test.go` alongside code | Test single units in isolation | `testing`, `testify/assert` (optional) |
| **Integration** | `*_integration_test.go` | Test component interactions | `testing`, build tags |
| **E2E** | `e2e/` directory | Full protocol compliance | Custom test harness |

### 3.4 Test Commands

```bash
# Run all tests
go test ./...

# Run with race detection
go test -race ./...

# Run specific package
go test ./server/...

# Run specific test
go test -run TestServer_NewServer ./server

# Run with coverage
go test -cover -coverprofile=coverage.out ./...
go tool cover -html=coverage.out

# Run integration tests
go test -tags=integration ./...

# Run benchmarks
go test -bench=. -benchmem ./...
```

### 3.5 Commit Convention (TDD)

```
# RED phase
test: add failing test for Tool.Execute with empty query

# GREEN phase
feat(server): implement Tool.Execute validation

# REFACTOR phase
refactor(server): extract validation logic to separate function

# General pattern
<type>(<scope>): <description>

Types: feat, fix, refactor, test, docs, chore
```

---

## 4. Idiomatic Go Patterns

### 4.1 Accept Interfaces, Return Structs

```go
// Good: Accept interface
func NewServer(transport Transport, opts ...Option) *Server

// Good: Return concrete type
func (s *Server) Tool(name string) *ToolBuilder

// Interface defined by consumer
type Transport interface {
    Serve(ctx context.Context, handler Handler) error
    Close() error
}
```

### 4.2 Functional Options Pattern

```go
type Option func(*serverOptions)

type serverOptions struct {
    logger     Logger
    middleware []Middleware
    timeout    time.Duration
}

func WithLogger(l Logger) Option {
    return func(o *serverOptions) {
        o.logger = l
    }
}

func WithTimeout(d time.Duration) Option {
    return func(o *serverOptions) {
        o.timeout = d
    }
}

func NewServer(info ServerInfo, opts ...Option) *Server {
    options := defaultOptions()
    for _, opt := range opts {
        opt(&options)
    }
    // ...
}
```

### 4.3 Error Handling

```go
// Define sentinel errors
var (
    ErrToolNotFound     = errors.New("mcp: tool not found")
    ErrResourceNotFound = errors.New("mcp: resource not found")
)

// Wrap errors with context
func (s *Server) executeTool(name string, input any) (any, error) {
    tool, ok := s.tools[name]
    if !ok {
        return nil, fmt.Errorf("execute tool %q: %w", name, ErrToolNotFound)
    }

    result, err := tool.Execute(ctx, input)
    if err != nil {
        return nil, fmt.Errorf("execute tool %q: %w", name, err)
    }

    return result, nil
}

// Check errors with errors.Is/As
if errors.Is(err, ErrToolNotFound) {
    // handle not found
}
```

### 4.4 Context Propagation

```go
// Always pass context as first parameter
func (t *Tool) Execute(ctx context.Context, input any) (any, error)

// Use context for cancellation
func (s *Server) Shutdown(ctx context.Context) error {
    done := make(chan struct{})
    go func() {
        s.waitForInflight()
        close(done)
    }()

    select {
    case <-done:
        return nil
    case <-ctx.Done():
        return ctx.Err()
    }
}

// Context values via typed keys
type contextKey string

const (
    requestIDKey contextKey = "requestID"
    loggerKey    contextKey = "logger"
)

func RequestIDFromContext(ctx context.Context) string {
    id, _ := ctx.Value(requestIDKey).(string)
    return id
}
```

### 4.5 Small Interfaces

```go
// Single-method interfaces (Go idiom)
type Handler interface {
    Handle(ctx context.Context, req *Request) (*Response, error)
}

type Validator interface {
    Validate() error
}

type Closer interface {
    Close() error
}

// Compose when needed
type Transport interface {
    Handler
    Closer
}
```

### 4.6 Constructor Pattern

```go
// NewX constructor returns pointer
func NewServer(info ServerInfo, opts ...Option) *Server {
    s := &Server{
        info:      info,
        tools:     make(map[string]*Tool),
        resources: make(map[string]*Resource),
        prompts:   make(map[string]*Prompt),
    }

    for _, opt := range opts {
        opt(s)
    }

    return s
}

// MustX variant panics on error (for initialization)
func MustNewServer(info ServerInfo, opts ...Option) *Server {
    s, err := NewServerWithValidation(info, opts...)
    if err != nil {
        panic(fmt.Sprintf("mcp: %v", err))
    }
    return s
}
```

### 4.7 Zero Values Are Useful

```go
// Design types so zero value is valid
type ServerInfo struct {
    Name    string // defaults to empty, could be validated
    Version string // defaults to empty
}

// Capabilities zero value means "no capabilities" (valid state)
type Capabilities struct {
    Tools     bool
    Resources bool
    Prompts   bool
}
```

---

## 5. Core Components

### 5.1 Server Aggregate

```go
// Server is the aggregate root for MCP server functionality.
type Server struct {
    mu sync.RWMutex

    info       ServerInfo
    tools      map[string]*Tool
    resources  map[string]*Resource
    prompts    map[string]*Prompt
    middleware []Middleware

    // Injected dependencies
    logger    Logger
    transport Transport
}

// ServerInfo contains server metadata exposed to clients.
type ServerInfo struct {
    Name         string
    Version      string
    Capabilities Capabilities
}

// Capabilities declares what features the server supports.
type Capabilities struct {
    Tools     bool
    Resources bool
    Prompts   bool
}
```

### 5.2 Tool Entity

```go
// Tool represents a callable function exposed via MCP.
type Tool struct {
    name        string
    description string
    tags        []string
    inputType   reflect.Type    // captured at registration
    inputSchema *schema.Schema  // pre-computed
    handler     reflect.Value   // handler function
}

// ToolBuilder provides fluent API for tool construction.
type ToolBuilder struct {
    tool   *Tool
    server *Server
    err    error  // captures first error
}

// Usage:
// srv.Tool("search").
//     Description("Search incidents").
//     Tags("search", "incidents").
//     Handler(searchHandler)
```

### 5.3 Middleware Chain

```go
// HandlerFunc is the signature for request handlers.
type HandlerFunc func(ctx context.Context, req *Request) (*Response, error)

// Middleware wraps a handler with additional behavior.
type Middleware func(next HandlerFunc) HandlerFunc

// Chain composes middleware in order.
func Chain(middlewares ...Middleware) Middleware {
    return func(final HandlerFunc) HandlerFunc {
        for i := len(middlewares) - 1; i >= 0; i-- {
            final = middlewares[i](final)
        }
        return final
    }
}
```

---

## 6. Protocol Implementation

### 6.1 JSON-RPC 2.0

```go
// Request represents a JSON-RPC 2.0 request.
type Request struct {
    JSONRPC string          `json:"jsonrpc"`
    ID      json.RawMessage `json:"id,omitempty"`
    Method  string          `json:"method"`
    Params  json.RawMessage `json:"params,omitempty"`
}

// Response represents a JSON-RPC 2.0 response.
type Response struct {
    JSONRPC string         `json:"jsonrpc"`
    ID      json.RawMessage `json:"id,omitempty"`
    Result  any            `json:"result,omitempty"`
    Error   *Error         `json:"error,omitempty"`
}

// Error represents a JSON-RPC 2.0 error.
type Error struct {
    Code    int    `json:"code"`
    Message string `json:"message"`
    Data    any    `json:"data,omitempty"`
}
```

### 6.2 MCP Error Codes

```go
// Standard JSON-RPC errors
const (
    CodeParseError     = -32700
    CodeInvalidRequest = -32600
    CodeMethodNotFound = -32601
    CodeInvalidParams  = -32602
    CodeInternalError  = -32603
)

// MCP-specific errors
const (
    CodeNotFound     = -32001
    CodeUnauthorized = -32002
    CodeRateLimited  = -32003
)

// Error constructors
func NewParseError(msg string) *Error
func NewInvalidParams(msg string) *Error
func NewNotFound(msg string) *Error
func NewInternalError(msg string) *Error
```

---

## 7. Transport Layer

### 7.1 Transport Interface

```go
// Transport defines the communication layer interface.
type Transport interface {
    // Serve starts the transport, blocking until ctx is cancelled.
    Serve(ctx context.Context, handler Handler) error

    // Addr returns the transport's address (e.g., ":8080" or "stdio").
    Addr() string
}

// Handler processes incoming requests.
type Handler interface {
    HandleRequest(ctx context.Context, req *protocol.Request) (*protocol.Response, error)
}
```

### 7.2 stdio Transport

```go
// StdioTransport implements MCP over stdin/stdout.
type StdioTransport struct {
    in     io.Reader
    out    io.Writer
    errOut io.Writer
}

func NewStdioTransport() *StdioTransport {
    return &StdioTransport{
        in:     os.Stdin,
        out:    os.Stdout,
        errOut: os.Stderr,
    }
}

func (t *StdioTransport) Serve(ctx context.Context, h Handler) error {
    scanner := bufio.NewScanner(t.in)
    for scanner.Scan() {
        select {
        case <-ctx.Done():
            return ctx.Err()
        default:
        }

        line := scanner.Bytes()
        go t.handleLine(ctx, h, line)
    }
    return scanner.Err()
}
```

### 7.3 HTTP+SSE Transport

```go
// HTTPTransport implements MCP over HTTP with SSE.
type HTTPTransport struct {
    addr   string
    server *http.Server
}

func NewHTTPTransport(addr string) *HTTPTransport {
    return &HTTPTransport{addr: addr}
}

func (t *HTTPTransport) Serve(ctx context.Context, h Handler) error {
    mux := http.NewServeMux()
    mux.HandleFunc("/mcp", t.handleMCP(h))
    mux.HandleFunc("/mcp/sse", t.handleSSE(h))
    mux.HandleFunc("/health", t.handleHealth())

    t.server = &http.Server{
        Addr:    t.addr,
        Handler: mux,
    }

    errCh := make(chan error, 1)
    go func() {
        errCh <- t.server.ListenAndServe()
    }()

    select {
    case <-ctx.Done():
        return t.server.Shutdown(context.Background())
    case err := <-errCh:
        return err
    }
}
```

---

## 8. Schema Generation

### 8.1 Schema Interface

```go
// Schema represents a JSON Schema.
type Schema struct {
    Type        string             `json:"type"`
    Properties  map[string]*Schema `json:"properties,omitempty"`
    Required    []string           `json:"required,omitempty"`
    Description string             `json:"description,omitempty"`
    Default     any                `json:"default,omitempty"`
    Enum        []any              `json:"enum,omitempty"`
    Minimum     *float64           `json:"minimum,omitempty"`
    Maximum     *float64           `json:"maximum,omitempty"`
    Items       *Schema            `json:"items,omitempty"`
}

// Generate creates a JSON Schema from a Go type.
func Generate(v any) (*Schema, error)

// Validate checks input against a schema.
func (s *Schema) Validate(input any) error
```

### 8.2 Struct Tags

```go
type SearchInput struct {
    // Required field with description
    Query string `json:"query" jsonschema:"required" jsonschema_description:"Search query string"`

    // Optional with default and constraints
    Limit int `json:"limit" jsonschema:"default=10,minimum=1,maximum=100"`

    // Optional field
    Tags []string `json:"tags,omitempty"`

    // Enum constraint
    SortBy string `json:"sortBy" jsonschema:"enum=relevance|date|name"`
}
```

---

## 9. Middleware

### 9.1 Built-in Middleware

```go
// Recover catches panics and converts to internal errors.
func Recover() Middleware {
    return func(next HandlerFunc) HandlerFunc {
        return func(ctx context.Context, req *Request) (resp *Response, err error) {
            defer func() {
                if r := recover(); r != nil {
                    err = protocol.NewInternalError(fmt.Sprintf("panic: %v", r))
                }
            }()
            return next(ctx, req)
        }
    }
}

// RequestID injects a unique request ID into context.
func RequestID() Middleware {
    return func(next HandlerFunc) HandlerFunc {
        return func(ctx context.Context, req *Request) (*Response, error) {
            id := uuid.New().String()
            ctx = context.WithValue(ctx, requestIDKey, id)
            return next(ctx, req)
        }
    }
}

// Timeout enforces a request deadline.
func Timeout(d time.Duration) Middleware {
    return func(next HandlerFunc) HandlerFunc {
        return func(ctx context.Context, req *Request) (*Response, error) {
            ctx, cancel := context.WithTimeout(ctx, d)
            defer cancel()
            return next(ctx, req)
        }
    }
}

// Logger logs request/response with structured fields.
func Logger(log Logger) Middleware {
    return func(next HandlerFunc) HandlerFunc {
        return func(ctx context.Context, req *Request) (*Response, error) {
            start := time.Now()

            resp, err := next(ctx, req)

            log.Info("request",
                Field("method", req.Method),
                Field("duration", time.Since(start)),
                Field("error", err),
            )

            return resp, err
        }
    }
}
```

---

## 10. Implementation Roadmap

### Phase 1: Core Foundation (TDD)

Each feature follows Red-Green-Refactor:

| Step | Test First | Implement | Refactor |
|------|------------|-----------|----------|
| 1 | `TestNewServer` | `server.go` | Extract options |
| 2 | `TestServer_Tool` | `tool.go` | Builder pattern |
| 3 | `TestTool_Handler` | Handler validation | Type safety |
| 4 | `TestSchema_Generate` | `schema/schema.go` | Reflection |
| 5 | `TestStdioTransport` | `transport/stdio.go` | Error handling |

### Phase 2: Resources & Prompts

| Step | Test First | Implement | Refactor |
|------|------------|-----------|----------|
| 1 | `TestServer_Resource` | `resource.go` | URI matching |
| 2 | `TestServer_Prompt` | `prompt.go` | Arguments |
| 3 | `TestHTTPTransport` | `transport/http.go` | SSE |

### Phase 3: Middleware & Polish

| Step | Test First | Implement | Refactor |
|------|------------|-----------|----------|
| 1 | `TestMiddleware_Chain` | `middleware/chain.go` | Composition |
| 2 | `TestRecover` | `middleware/recover.go` | Panic capture |
| 3 | `TestTimeout` | `middleware/timeout.go` | Context |
| 4 | `TestRequestID` | `middleware/requestid.go` | UUID |

### Phase 4: Compliance & Release

| Step | Activity |
|------|----------|
| 1 | MCP compliance test suite |
| 2 | Performance benchmarks |
| 3 | Documentation |
| 4 | Examples |
| 5 | v0.1.0 release |

---

## 11. Quality Gates

### 11.1 Code Quality

```bash
# All must pass before merge
go test ./...                    # Tests pass
go test -race ./...              # No race conditions
golangci-lint run                # Linting clean
go test -cover ./... | grep -v "100.0%" # Coverage targets met
```

### 11.2 Coverage Targets

| Package | Target |
|---------|--------|
| `server/` | 90%+ |
| `protocol/` | 95%+ |
| `transport/` | 85%+ |
| `middleware/` | 90%+ |
| `schema/` | 90%+ |

### 11.3 Linting Rules

```yaml
# .golangci.yml
linters:
  enable:
    - errcheck
    - govet
    - staticcheck
    - unused
    - gosimple
    - ineffassign
    - gofmt
    - goimports
    - misspell
    - unconvert
    - gocritic
    - revive
```

---

## 12. Dependencies

### 12.1 Production Dependencies

| Dependency | Purpose | Justification |
|------------|---------|---------------|
| Standard library | Core | Zero external dependencies for core |

### 12.2 Development Dependencies

| Dependency | Purpose |
|------------|---------|
| `github.com/google/go-cmp` | Test comparison |
| `github.com/google/uuid` | Request IDs |
| `golang.org/x/tools` | Static analysis |

---

## 13. Security Considerations

### 13.1 Input Validation

- Strict JSON decoding (`DisallowUnknownFields`)
- Schema validation before handler execution
- Request size limits
- No eval or dynamic code execution

### 13.2 Resource Protection

- Context timeouts on all operations
- Graceful shutdown with drain period
- Rate limiting middleware (v0.2)

---

## 14. Open Questions

1. **Schema library**: Build custom or use `invopop/jsonschema`?
   - *Recommendation*: Start custom for minimal dependencies, evaluate later

2. **Generics depth**: How far to push generic type safety?
   - *Recommendation*: Use generics for handlers, keep API simple

3. **Streaming**: Include streaming responses in MVP?
   - *Recommendation*: Defer to v0.2

---

## 15. References

- [MCP Specification](https://modelcontextprotocol.io/specification)
- [JSON-RPC 2.0 Specification](https://www.jsonrpc.org/specification)
- [JSON Schema](https://json-schema.org/)
- [Effective Go](https://go.dev/doc/effective_go)
- [Go Code Review Comments](https://go.dev/wiki/CodeReviewComments)
- [Uber Go Style Guide](https://github.com/uber-go/guide/blob/master/style.md)
