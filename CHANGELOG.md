# Changelog

All notable changes to this project will be documented in this file.

## [1.12.0](https://github.com/felixgeelhaar/mcp-go/compare/v1.11.2...v1.12.0)

### Features

#### BearerAuth shorthand for shared-secret deployments (#87, #88)
- Added `mcp.BearerAuth(tokens map[string]string, opts ...AuthOption) Middleware`
- Single-call entry point for the most common bearer-auth case: reject calls that don't present a shared secret
- Map values become `Identity.ID` + `Identity.Name` surfaced via `IdentityFromContext`
- Handshake methods (`initialize`, `notifications/initialized`, `ping`) exempted automatically
- The full `Auth` + `BearerTokenAuthenticator` + `StaticTokens` primitives remain in place for scope-aware authz, multi-tenant identity, and per-token metadata

#### TLS configuration for HTTP, gRPC, and WebSocket transports (#86, #89)
- Added `mcp.WithTLSConfig(*tls.Config) HTTPOption` — switches the HTTP transport to `ServeTLS`
- Added `mcp.WithWebSocketTLSConfig(*tls.Config) WebSocketOption` — switches WebSocket to `ListenAndServeTLS`
- Added `mcp.WithGRPCTLSConfig(*tls.Config) GRPCOption` — wraps `grpc.Creds(credentials.NewTLS(cfg))` and composes with `WithServerOptions`
- `*tls.Config` is the only surface — operators bring their own cert loading + rotation strategy (`LoadX509KeyPair`, autocert, SPIFFE workload API, etc.)
- Set `ClientCAs` + `ClientAuth` on the config for mTLS — common in service-mesh + regulated single-binary deployments where ops doesn't want a separate TLS-terminating proxy

## [1.9.0](https://github.com/felixgeelhaar/mcp-go/compare/v1.8.0...v1.9.0)

### Features

#### Structured Content in Tool Responses (#57)
- Added `OutputSchema()` builder method for tools to declare typed output schemas
- Added `StructuredResult` type for returning both text content blocks and structured data
- Tools with `outputSchema` advertise it in `tools/list` responses
- Backward compatible: existing string-returning handlers continue to work unchanged

#### Dynamic Tool Registration with List Changed (#58)
- Added `RemoveTool(name)`, `RemoveResource(uriTemplate)`, `RemovePrompt(name)` methods to `Server`
- Capabilities now advertise `listChanged: true` for tools, resources, and prompts
- Enables runtime tool set management — add/remove tools and notify clients via `session.NotifyToolListChanged()`

#### Elicitation Protocol for Interactive User Prompts (#59)
- Added `elicitation/create` method for server-to-client structured input requests
- New types: `ElicitRequest`, `ElicitResult`, `Elicitor`
- Context helper `ElicitFromContext(ctx)` available in tool handlers when client supports elicitation
- Supports accept, decline, and cancel actions with JSON Schema-defined input forms

#### MCP Channels for Server-Initiated Push Messages (#60)
- Added `notifications/channel/message` for server-to-client push messaging
- New types: `ChannelMessage`, `ChannelSender`
- Context helper `ChannelFromContext(ctx)` available in tool handlers when client supports channels
- Convenience method `SendText(channel, text)` for simple text messages
- Eliminates polling — servers can proactively push DOM changes, network events, navigation alerts

## [1.8.0](https://github.com/felixgeelhaar/mcp-go/compare/v1.7.0...v1.8.0)

### Breaking Changes
- Go 1.25 is now the minimum required version (previously 1.23)

### CI & Infrastructure
- Fixed invalid GitHub Actions SHAs across all workflows (CI, release, pages)
- Upgraded all actions to Node 24 runtime (checkout v6.0.2, setup-go v6.3.0, golangci-lint-action v9.2.0)
- Replaced VerdictSec with nox for security scanning
- Added `.githooks/pre-commit` hook covering vet, lint, build, and test (`make hooks` to install)

### Fixes
- Increased stdio test timeout for CI cold cache compilation
- Updated gonum to v0.17.0 for security patches
- Updated fortify to v1.2.1 for security patches

## [1.7.0](https://github.com/felixgeelhaar/mcp-go/compare/v1.6.3...v1.7.0)

### Features

#### SessionStore for Horizontal Scaling
- Added `SessionStore` interface for session persistence across server restarts
- Built-in `InMemorySessionStore` for single-instance deployments
- Redis-backed `SessionStore` example with TTL support
- HTTP transport integration with `WithSessionStore()` option

#### Server Discovery
- Added `/.well-known/mcp` endpoint for MCP server discovery
- `ServerDiscovery` type with protocol, capabilities, and auth info
- HTTP transport and `mcp.go` integration via `WithDiscovery()` option

#### Tasks Primitive
- `TaskManager` for async/long-running task execution
- Support for `tasks/create`, `tasks/get`, `tasks/list`, `tasks/cancel`
- Async execution with proper context cancellation
- `Server.RegisterTask()` for task registration

#### Enterprise Middleware
- `Audit()` middleware for request/response audit logging
- `Tracing()` middleware with correlation and trace ID propagation
- `OAuth2()` authenticator with JWT validation
- Scope-based authorization

### Dependencies
- Updated OpenTelemetry SDK to v1.42.0
- Updated google.golang.org/grpc to v1.79.3
- Updated gonum to v0.17.0
- Updated fortify to v1.2.1

## Release 1.6.3
