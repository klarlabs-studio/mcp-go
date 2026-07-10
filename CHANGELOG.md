# Changelog

All notable changes to this project will be documented in this file.

## [Unreleased]

### Added — spec-revisions foundation (Phase 0)

First slice of the spec-revisions roadmap (`docs/revisions-roadmap.md`), which
brings mcp-go current across every MCP revision from `2024-11-05` to the
`2026-07-28` release candidate. This slice lays the backbone and fixes wiring:

- **Protocol version negotiation.** `protocol.SupportedVersions`,
  `protocol.IsSupportedVersion`, and `protocol.NegotiateVersion` replace the
  hard-pinned version. `initialize` now parses the client's `protocolVersion`
  and echoes it back when supported (negotiating down to the server's preferred
  version otherwise) — previously the request was ignored entirely. New spec
  revisions are enabled by appending to `SupportedVersions` as each roadmap
  phase is certified.
- **Client capabilities captured at initialize.** `initialize` now records the
  client's advertised capabilities on the session (via the new
  `(*server.Session).SetClientCapabilitiesJSON`), so feature gating for
  sampling/elicitation has the data it needs.
- **Dead methods wired into the dispatcher.** `completion/complete`,
  `logging/setLevel`, `resources/templates/list`, and
  `notifications/initialized` are now dispatched. Their handlers already
  existed but were unreachable and returned `-32601 MethodNotFound`.
- **Capability advertisement.** `completions` now auto-advertises when a
  completion handler is registered; a new opt-in `Capabilities.Logging` flag
  advertises the `logging` capability.
- **Session injection (stdio + websocket).** Both transports now attach a
  per-connection `server.Session` to every request context, so features that
  need one — logging notifications, channels, resource-updated — are reachable.
  Previously `SessionFromContext(ctx)` was always nil and these silently no-op'd.
  (HTTP session injection lands in Phase 1 with the Streamable HTTP transport.)
- **Graceful degradation for server→client requests.** `Session.CreateMessage`,
  `Session.ListRoots`, and `Elicitor.Elicit` now return the new sentinel
  `server.ErrNoRequestSender` when the transport has no bidirectional request
  sender, instead of panicking on a nil sender. One-way features are unaffected.

### Removed (BREAKING)

#### In-library authentication removed — auth is out of scope
mcp-go never handles tokens, OAuth flows, or credentials. All in-library auth
has been deleted:

- Deleted `middleware/auth.go` and every symbol it exported: `Auth`,
  `Authenticator`, `AuthOption`, `Identity`, `APIKeyAuthenticator`,
  `BearerTokenAuthenticator`, `StaticAPIKeys`, `StaticTokens`,
  `ChainAuthenticators`, `OAuth2Authenticator`, `JWTValidator`,
  `IdentityFromContext`, `ContextWithIdentity`, and the `WithAuth*` options.
- Removed the top-level re-exports in `mcp.go` (including `mcp.BearerAuth`,
  `mcp.Identity`, `mcp.IdentityFromContext`, `mcp.ContextWithIdentity`, and the
  `mcp.Auth*` family).
- Removed `client.WithBearerToken`.

**Migration:**
- **Client:** inject auth via the caller-supplied `http.Client` transport.
  Replace `client.WithBearerToken(tok)` with a custom `http.RoundTripper` set on
  `mcp.WithHTTPClient(&http.Client{Transport: myAuthTransport})`. For API keys,
  bearer tokens, or mTLS, configure them on that transport.
- **Server:** terminate auth at the transport/proxy layer (API gateway, mTLS) or
  in your own middleware; mcp-go ships none. To vary behaviour by caller in a
  filter predicate, attach your own value to the request context (e.g. via
  `transport.WithRequestContextFn` for mTLS peer certs) and read it back — there
  is no longer an `Identity` type.

### Changed

#### Client and server now share transport framing (no duplication)
- Added `transport.NewlineFramer` (newline-delimited JSON) and
  `transport.SSEWriter` / `transport.SSEReader` (Server-Sent Events) as the
  single framing primitives.
- The stdio client (`client.StdioTransport`) and stdio server
  (`transport.Stdio`) now both frame messages via `transport.NewlineFramer`,
  eliminating their duplicate `bufio.Scanner` + `json.Marshal`+`\n` framers and
  unifying the 16MB read-buffer limit.
- The SSE server emitter (`transport.HTTP`) and SSE client reader
  (`client.HTTPTransport.Stream`) now share the `transport.SSEWriter` /
  `transport.SSEReader` grammar, removing the duplicated `data: ` framing.

### Added

#### Top-level client API surface
- Added `mcp.NewClient(url, ...mcp.ClientOption)` and `mcp.WithHTTPClient(*http.Client)`
  for constructing an HTTP/SSE client. The injected `http.Client` is the only
  auth hook — mcp-go never handles tokens or credentials.
- Added `mcp.NewStdioClient(cmd, args...)` for CLI-based MCP servers.
- Added `mcp.Call[In, Out](ctx, client, name, in)` and
  `mcp.NewClientTool[In, Out](client, name)` — the typed, recommended client API.
- Added `(*client.Client).CallRaw(ctx, name, json.RawMessage)` and the `mcp.Tool`
  interface (`mcp.NewDynamicTool`) as the dynamic/untyped escape hatch. NOT
  recommended — prefer the typed API.
- Added `(*Server).ListTools()` introspection alias of `Tools()`.

### Changed (BREAKING)

#### `client.Tool` flips from a metadata struct to an interface
- **BREAKING:** `client.Tool` is no longer the tool-metadata struct — it is now
  the dynamic escape-hatch **interface** (formerly `DynamicTool`). The metadata
  struct that `ListTools` returns is now named `client.ToolInfo`. This is a hard
  semantic change, not just a rename: code that did `t.Name` / `t.Description` /
  `t.InputSchema` on a `client.Tool` **value** no longer compiles, because `Tool`
  is now an interface type. `DynamicTool` remains as a deprecated alias of the
  new interface.

**Migration:**
- Anywhere you held a `client.Tool` for its metadata fields (e.g. the elements of
  the `ListTools` result, or `t.Name`), change the type to `client.ToolInfo`.
  Field access (`t.Name`, `t.Description`, `t.InputSchema`) is unchanged once the
  type is `ToolInfo`.
- Code using the old `DynamicTool` interface can keep compiling via the alias, or
  switch to `client.Tool`.

### Changed

#### Input schema validation is now on by default
- Tool input is validated against the generated JSON Schema (required / minimum /
  maximum / enum) **before** the handler runs, so invalid-per-schema input is
  rejected with an `InvalidParams` error and never reaches business logic.
- Added `(*ToolBuilder).SkipValidation()` as the opt-out for tools that need to
  accept inputs the generated schema would reject.
- `(*ToolBuilder).ValidateInput()` is now a no-op (validation is the default) and
  is deprecated. Existing calls keep compiling and keep validation enabled.

**Migration:** No action needed for tools whose handlers already expect
schema-valid input. If a tool deliberately accepts inputs that violate its
generated schema, add `.SkipValidation()` to its builder chain.

## [1.21.0] - 2026-07-06

Security & correctness hardening from a full deep review. The theme is
**secure-by-default**: the framework's production defaults were unsafe and some
of the safety knobs were broken. All defaults now fail safe, with explicit
opt-outs. Behavior-compatible for well-behaved callers; a minor bump.

### Fixed — middleware & panic safety

- **`Server.Use()` was a silent no-op.** The serve path only read middleware
  from `WithMiddleware`; middleware registered via the fluent `Use()` API —
  including `Recover`/`SizeLimit` — was never applied, and `server.Middleware`
  was even a distinct type from `middleware.Middleware`, so the standard
  middleware couldn't be passed to it. Unified the types and wired `Use()` into
  the chain.
- **Panic recovery is now on by default.** A handler panic previously unwound
  the stdio/WebSocket read loop and crashed the whole server process (all
  sessions). `Recover` is now forced outermost by default; a caller's own
  Recover still runs inner.
- The default panic handler no longer leaks the panic value (internal
  paths/state) to the peer — it logs detail server-side and returns a generic
  `internal error`.
- `Timeout` middleware now actually enforces the deadline (it previously ran the
  handler synchronously, so a non-cooperative handler ran to completion).

### Fixed — protocol

- Depth-limit untrusted JSON before decoding, preventing a deeply-nested payload
  from causing a fatal, unrecoverable stack overflow.
- `Response` now always emits a spec-correct `id` (`null` when undeterminable)
  and exactly one of `result`/`error`.
- gRPC request ids are JSON-escaped (no malformed-JSON injection); numeric
  `progressToken`s are accepted (string or integer).

### Fixed — dispatch

- Overlapping resource URIs now dispatch deterministically (most-specific wins,
  sorted iteration) instead of a random map-order handler — the authorization
  decision no longer depends on iteration order.
- Duplicate tool/resource/prompt registration is rejected (surfaced via
  `Server.Err()`) instead of silently shadowing.
- Added a `ContainedPath` helper for file-style resources.

### Security — transports (secure-by-default, with opt-outs)

- HTTP/SSE/WebSocket now validate the `Origin` header against an allowlist and
  reject cross-origin requests (mitigates DNS-rebinding / cross-origin
  exfiltration). New `WithAllowedOrigins` / `WithInsecureAllowAllOrigins` and
  WebSocket equivalents; SSE no longer hardcodes `Access-Control-Allow-Origin: *`.
- SSE session ids can no longer be hijacked or collided (server refuses to
  overwrite a live channel; `crypto/rand`-minted when absent); the caller auth
  hook now runs on the SSE path.
- Request bodies (`http.MaxBytesReader`), WebSocket reads (`SetReadLimit`), and
  concurrent connections are now bounded by default (413/503 on exceed). HTTP
  correctly returns `202` for notifications.

### Security — framing, client & lifecycle

- An over-sized transport frame is now skipped and the read loop survives
  (previously it permanently wedged the transport); all stdio writes serialize
  through one framer (no interleave race).
- The client bounds responses from an untrusted server (`io.LimitReader`) and
  refuses cross-host redirects (no custom-auth-header leak); the session store is
  bounded and returns copies.
- The task registry is bounded with TTL/eviction and `CancelTask` actually
  cancels the running goroutine; `notifications/cancelled` is wired; subscription
  counts are capped per client; internal handler errors are sanitized before
  reaching the peer.

## [1.13.0](https://github.com/klarlabs-studio/mcp-go/compare/v1.12.0...v1.13.0)

### Features

#### Identity-aware list filtering for tools, resources, and prompts (#90, #91)
- Added `mcp.WithToolFilter(func(ctx, name) bool) ServeOption` — predicate gates `tools/list` visibility AND `tools/call` execution, so the filter is the authoritative contract rather than a display layer
- Added `mcp.WithResourceFilter(func(ctx, uri, name) bool) ServeOption` — gates `resources/list` + `resources/read`
- Added `mcp.WithPromptFilter(func(ctx, name) bool) ServeOption` — gates `prompts/list` + `prompts/get`
- Predicates receive the request context — pair with `IdentityFromContext` for identity-aware authz (e.g. admin-only tools hidden from read-only clients)
- Filters apply during typed list construction, so they're immune to the schema-coupling problem of post-response middleware approaches (response-map walking breaks silently when the response shape evolves)
- Added `(*server.Resource).URITemplate()` and `(*server.Resource).Name()` accessors so resource filter predicates can be implemented without poking at unexported fields

## [1.12.0](https://github.com/klarlabs-studio/mcp-go/compare/v1.11.2...v1.12.0)

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

## [1.9.0](https://github.com/klarlabs-studio/mcp-go/compare/v1.8.0...v1.9.0)

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

## [1.8.0](https://github.com/klarlabs-studio/mcp-go/compare/v1.7.0...v1.8.0)

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

## [1.7.0](https://github.com/klarlabs-studio/mcp-go/compare/v1.6.3...v1.7.0)

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
