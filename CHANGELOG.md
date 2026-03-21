# Changelog

All notable changes to this project will be documented in this file.

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
