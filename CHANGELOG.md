# Changelog

All notable changes to this project will be documented in this file.

## [Unreleased]

## [1.28.1](https://github.com/klarlabs-studio/mcp-go/compare/v1.28.0...v1.28.1) - 2026-09-26

### Fixed

- **Tool results without a structured payload no longer send
  `"structuredContent": null`.** `StructuredResult.MarshalJSON` omitted a nil
  payload (v1.24.1), but the tools/call dispatch does not marshal the struct:
  `buildToolCallResponse` copies its fields into a response map and wrote the
  field unconditionally, so every error result on the wire still carried the
  null that strict clients reject (reported downstream as roady #92). The
  dispatch now omits a nil payload and keeps an explicit empty map as `{}`,
  and a test asserts on the dispatch output rather than the struct.

## [1.28.0](https://github.com/klarlabs-studio/mcp-go/compare/v1.27.0...v1.28.0) - 2026-09-20

### Added

- **SEP-2127 Server Cards (experimental).** `NewServerCard` /
  `NewServerCardFromDiscovery` build static connection metadata. `WithServerCard`
  serves `GET /mcp/server-card` (`application/mcp-server-card+json`, CORS + ETag)
  and AI Catalog entries at `/.well-known/ai-catalog.json` and
  `/.well-known/mcp/catalog.json`.
- **Progressive tool discovery helpers.** `ToolBuilder.Group` / `Tags` /
  `DeferSchema`; `tools/list` accepts `group`, `tags` (AND), and
  `detail` (`names`|`full`). Best-effort ahead of a Core Primitives SEP.
- **Streamable HTTP over stdio (pragmatic).** `ServeStdioHTTP` /
  `transport.NewStdioHTTP` speak NDJSON `HTTPFrame` envelopes (request /
  response / notification) preserving `Mcp-Method` until HTTP/2-over-stdio
  lands in the Transports WG.
- **Webhook notification delivery.** `WebhookNotifier` POSTs JSON-RPC
  notifications to a client URL; `MultiNotifier` fans out to multiple
  senders (compose with session/SSE notifiers).
- **Agent-identity advertise fields.** `WithDiscoveryAuthExtensions` adds
  DPoP / token-exchange / ID-JAG / WIF hints on discovery. Docs:
  `docs/agent-identity.md`. Enforcement remains gateway-side.
- **Warden provenance-skip for CI.** `ci.yml` runs `warden-verify` first; when
  the commit already carries `refs/notes/warden`, the expensive go-ci reusable
  workflow is skipped and required check names are reported green via the
  Checks API. Arm locally with `make hooks` so validated pushes do not re-burn
  Actions minutes.
- **SEP-2243 `x-mcp-header` / `Mcp-Param-*`.** Mark tool params with
  `jsonschema:"header=Region"` (emits `"x-mcp-header"`). Streamable HTTP
  clients cache schemas from `ListTools`, exclude invalid annotations, and
  mirror arguments into `Mcp-Param-*` on `tools/call`. HTTP servers validate
  header↔body matches (`-32020` on mismatch).
- **Skills archive + template index entries (SEP-2640, experimental).**
  `Skill(path).FromArchive(file)` serves `.tar.gz`/`.zip` with
  `type:"archive"`; `SkillTemplate(uri, desc)` registers
  `type:"mcp-resource-template"` plus an MCP resource template.
- **`client.Connect`.** Prefers `server/discover`, falls back to `Initialize`
  on MethodNotFound.

### Changed

- **Client defaults to modern (`2026-07-28`).** `client.New` attaches
  per-request `_meta` without a prior Discover. Use
  `WithProtocolVersion(protocol.MCPVersion)` for initialize-era-only peers.
- **Default CacheableResult TTL is 60s private** (was immediately stale /
  `ttlMs: 0`). Override with `WithResultCache`; pass `ttlMs: 0` for the old
  behavior.
- **Deprecation docs** steer new code to Connect/Discover and provider APIs;
  `examples/typed-client` uses `Connect`.

## [1.27.0](https://github.com/klarlabs-studio/mcp-go/compare/v1.26.0...v1.27.0) - 2026-08-15

### Added

- **Skills extension (SEP-2640) — experimental.** `Server.Skill(path).FromDir(dir)`
  and `SkillsFromDir` register Agent Skills as `skill://` resources, generate
  `skill://index.json`, and advertise `io.modelcontextprotocol/skills` when
  any skill is registered. `initialize` and `server/discover` both carry
  `capabilities.extensions`. Client helpers: `ListSkills`, `ReadSkillURI`.
  The upstream SEP is still Draft; treat this API as unstable until it lands.

## [1.26.0](https://github.com/klarlabs-studio/mcp-go/compare/v1.25.0...v1.26.0) - 2026-08-14

### Changed — MCP spec alignment

- **`ServeHTTP` defaults to Streamable HTTP** (stateless 2026-07-28 model).
  The retired POST `/mcp` + GET `/mcp/sse?clientId=` split is opt-in via
  `WithLegacyHTTP()`. Session-negotiated Streamable HTTP remains
  `WithStreamableStateful()`. `transport.NewHTTP` without options is still
  the legacy split so low-level tests and custom muxes are unchanged.
- **`resources/list` no longer includes URI templates.** Templates are only
  advertised on `resources/templates/list`, matching the spec split between
  `Resource.uri` and `ResourceTemplate.uriTemplate`.
- **Unknown tools and prompts return `-32602` Invalid params** rather than
  the non-schema `-32001` Not found. Resource-not-found stays `-32001` on
  the legacy path and is remapped to `-32602` for modern callers.
- **Blob-only `resources/read` contents omit empty `text`.** The spec is
  text XOR blob on a content item.
- **Icons serialize per negotiated protocol version.** 2025-11-25 listings
  emit `uri`/`size`; 2026-07-28 emits `src`/`sizes`/`theme`. Both eras no
  longer appear on the same object.
- **`2026-07-28` is in `SupportedVersions`** as `protocol.ModernVersion`
  (published spec, not a draft). `initialize` still negotiates only
  initialize-era revisions (`InitializeVersions`); requesting `2026-07-28`
  via initialize falls back to `2025-11-25` because that revision has no
  handshake. `DraftVersion` remains as a deprecated alias.
- **Client default protocol version is `2025-11-25`.** HTTP requests send
  `MCP-Protocol-Version`, `Mcp-Method`, and `Mcp-Name`. `Client.Discover`
  speaks `server/discover`. After Discover, subsequent calls attach modern
  `_meta` (protocolVersion, clientInfo, clientCapabilities). Icon parsing
  accepts modern `src`/`sizes`/`theme`.
- **Modern list/read/discover results always carry `ttlMs` and `cacheScope`**
  (CacheableResult). Defaults are `ttlMs: 0` (immediately stale) and
  `cacheScope: "private"`; `WithResultCache` overrides them.
- **Modern results identify the server** in
  `_meta[io.modelcontextprotocol/serverInfo]`.
- **Modern requests omit `notifications/message` unless `_meta` includes
  `io.modelcontextprotocol/logLevel`** (SEP-2575 MUST).

### Added

- **Cursor pagination** on `tools/list`, `resources/list`, `prompts/list`,
  and `resources/templates/list` (page size 100, `nextCursor`, invalid
  cursor → `-32602`).
- **`MCP-Protocol-Version` enforcement** on Streamable HTTP POSTs for
  revisions ≥ 2025-06-18 (missing or unsupported → HTTP 400).
  `initialize` and `server/discover` remain the version-probe exceptions.
- **`completion/complete` `context`** (previously-resolved arguments,
  MCP 2025-06-18) is parsed and attached via `CompletionContextFromContext`.
- **`notifications/elicitation/complete`** unblocks
  `Session.WaitElicitationComplete` for URL-mode elicitation.
- **RFC 9728 `/.well-known/oauth-protected-resource`** is served (advertise
  only, no token validation) when discovery OAuth metadata is configured.
- **Unsolicited task handles** on the modern path: a `TaskSupportRequired`
  tool returns a flat `CreateTaskResult` (`resultType: "task"`) from a plain
  `tools/call` (SEP-2663) when the client declares `io.modelcontextprotocol/tasks`.
  The retired per-request `task` field is ignored. Missing the extension is
  `-32021`. `tasks/result` and `notifications/elicitation/complete` are `-32601`
  for modern callers; `tasks/get` inlines the terminal `result`/`error`.
- **Task-level MRTR.** A background task that needs elicitation, sampling, or
  roots pauses at `input_required` with a keyed `inputRequests` map.
  `tasks/update` accepts `inputResponses` (map or array), ignores unknown keys,
  and replays the handler. `ttl` / `ttlMs` still refresh the deadline.
- **`notifications/tasks`.** `subscriptions/listen` accepts
  `notifications.taskIds` (tasks extension required). Status changes are
  pushed to those streams with the same DetailedTask as `tasks/get`.
- **No `/v2` module path.** Phase 4 stays on v1.

### Fixed

- **Background task elicitation no longer races `tools/call`.** A
  `TaskSupportRequired` tool that elicited immediately could rewrite the
  CreateTaskResult into an `input_required` response (and trip the race
  detector). The task now runs with a forked session/broker; `tools/call`
  keeps the task handle.

## [1.25.0](https://github.com/klarlabs-studio/mcp-go/compare/v1.24.1...v1.25.0) - 2026-08-14

### Fixed

- **`jsonschema` descriptions were truncated at the first comma.** The struct
  tag is comma-separated, which collides with prose:
  `description=Maximum results to return (default 10, capped at 50)` split into
  two parts, the second matched no directive, and it was dropped — advertising
  `"Maximum results to return (default 10"`, cut mid-parenthesis.

  Found in a real MCP server where 13 of 23 tags were losing text, including
  one advertising `"What sort of document this is: runbook"` while six other
  kinds were valid. For an MCP tool this text is the contract with the model,
  and neither the server author nor the model can tell it was cut — the schema
  is well-formed, just wrong.

  A comma-separated part matching no known directive is now treated as a
  continuation of the preceding description and rejoined with its comma.
  Escaping would also have worked but silently changes what every existing tag
  means; this way well-formed tags parse exactly as before and
  previously-truncated ones keep their text. `required` still works wherever it
  appears, including after a description containing commas.

  Known edge, pinned by tests rather than papered over: a description *ending*
  in something that looks like a directive (`...,default=x`) parses as one.
  That ambiguity is inherent to splitting on commas.

### Added

- **`minimum`, `maximum`, `default` and `enum` in `jsonschema` tags are now
  emitted.** They sat behind a TODO — accepted and discarded — while `Schema`
  already had the fields to hold them, so a field carrying them advertised no
  constraint at all.

  `enum` takes `|` as its separator, since `,` delimits directives. Values are
  typed, so `default=4` on an int encodes as `4` rather than `"4"`, which a
  strict validator rejects against `type: integer`.

## [1.24.1](https://github.com/klarlabs-studio/mcp-go/compare/v1.24.0...v1.24.1) - 2026-08-10

### Fixed

- **`StructuredResult` emitted `"structuredContent": null` when a handler
  returned no structured payload.** The field had no `omitempty`, so a nil map
  encoded as `null`.

  The spec makes `structuredContent` optional and requires it to match the
  tool's `outputSchema` when present. `null` matches no object schema, so a
  strict client rejects the *entire* result during validation — including the
  text content that would have explained what happened.

  The damage is worst on error results, which is exactly when that text
  matters. A handler returning `IsError` with a helpful message had the
  message discarded and replaced by a schema-validation failure, leaving the
  caller unable to distinguish "the operation failed" from "the response could
  not be encoded". For a tool that mutates state, that is the difference
  between knowing a write applied and guessing — a client that retries on
  error could double-apply it. Reported downstream as
  felixgeelhaar/roady#92.

  A struct tag alone cannot fix this: without `omitempty` a nil map encodes as
  `null`, and with it an empty-but-present map is dropped too, so a tool whose
  schema permits `{}` loses the ability to say so. `StructuredResult` now has
  a `MarshalJSON` that omits a nil payload and preserves an empty one.

## [1.24.0](https://github.com/klarlabs-studio/mcp-go/compare/v1.23.0...v1.24.0) - 2026-07-11

Makes the stateless (MCP 2026-07-28) model the **default** for the Streamable
HTTP transport. Shipped as a minor (not v2.0.0) by deliberate decision: the only
consumers are the maintainer's own fleet — all of which serve over **stdio**
(unaffected by this HTTP-transport change) and upgrade in lockstep — and there
are no external consumers, so the `/v2` module-path migration is intentionally
avoided.

### Changed — ⚠️ behavior change (Streamable HTTP)

- **`WithStreamable()` now defaults to stateless.** The Streamable HTTP transport
  enabled via `WithStreamable()` uses the 2026-07-28 stateless model: it **drops
  the `Mcp-Session-Id` lifecycle** (none minted on initialize, none required on
  POSTs) and **hard-requires the `Mcp-Method` routing header** (absent → `-32020`).
  Previously `WithStreamable()` was session-negotiated (2025-03-26).
  - **Migration:** to keep the session-negotiated behavior, switch
    `WithStreamable()` → **`WithStreamableStateful()`** (new). `stdio` servers are
    unaffected — this only touches the Streamable HTTP transport.

### Added

- **`WithStreamableStateful()`** — opt into the legacy session-negotiated
  (2025-03-26) Streamable HTTP model (mints/requires `Mcp-Session-Id`, serves the
  GET SSE stream + DELETE, validates `Mcp-Method` when present). The explicit
  opt-out from the new stateless default.
- **`WithStreamableStateless()`** is retained as an explicit spelling of the new
  default (`== WithStreamable`).

## [1.23.0](https://github.com/klarlabs-studio/mcp-go/compare/v1.22.0...v1.23.0) - 2026-07-11

Completes the 2026-07-28 stateless surface (Phase 4) on **v1** — additive and
backward-compatible. Modern behavior stays gated behind the
`WithStreamableStateless` opt-in and the per-request `_meta`; existing v1 servers
are unaffected. Making `Stateless` the default and tagging v2.0.0 is deferred.

### Added

- **Retired lifecycle methods on the modern path** — a modern (2026-07-28) caller
  invoking `initialize`, `notifications/initialized`, `ping`, `logging/setLevel`,
  `resources/subscribe`/`unsubscribe`, or `notifications/roots/list_changed` now
  gets `MethodNotFound` (`retiredInModern`); `server/discover` + per-request
  `_meta` + `subscriptions/listen` replace them. Legacy (`<=2025-11-25`) callers
  never enter the modern path and keep them as the back-compat probe.

### Deprecated

- **Server-initiated sampling, roots, and logging** now carry Go `// Deprecated:`
  markers so `gopls`/`staticcheck`/pkg.go.dev flag their use:
  `Session.CreateMessage`/`CreateMessageWithTools`, `Session.ListRoots`, and the
  `Session.Log`/`Debug`/…/`Emergency` cluster. They stay fully functional for the
  12-month window (nothing removed). `SetLogLevel`/`LogLevel` are retained — the
  modern log level travels in `_meta`. See `docs/deprecations.md` for the
  migrations (provider APIs / tool params / stderr + OpenTelemetry).

## [1.22.0](https://github.com/klarlabs-studio/mcp-go/compare/v1.21.0...v1.22.0) - 2026-07-10

### Added — 2026-07-28 stateless foundation (Phase 4, experimental)

First increment of the modern, stateless MCP revision (RC, SEP-2575). This lays
the foundation; the full stateless request path (per-request `_meta`, MRTR,
`subscriptions/listen`, routing headers) is built incrementally and `2026-07-28`
is deliberately NOT yet in `SupportedVersions`.

- **`server/discover`** (SEP-2575) — the stateless replacement for the
  `initialize` handshake. Returns `{ resultType:"complete", supportedVersions,
  capabilities (incl. the extensions map), serverInfo, instructions }` in one
  cacheable request. mcp-go is dual-era: it keeps `initialize` for legacy clients.
- **Extensions capability map** (SEP-2133) — `capabilities.extensions` advertises
  reverse-DNS extension ids: `io.modelcontextprotocol/ui` (MCP Apps, always) and
  `io.modelcontextprotocol/tasks` (when a tool opts into task augmentation).
- **Modern error codes** — `-32020` HeaderMismatch, `-32021`
  MissingRequiredClientCapability, `-32022` UnsupportedProtocolVersion, with
  `protocol.NewUnsupportedProtocolVersion` / `NewMissingRequiredClientCapability`.
- **Reserved `_meta` key + resultType constants** — `protocol.MetaKey*`
  (protocolVersion/clientInfo/clientCapabilities/logLevel/subscriptionId/
  related-task), `protocol.ResultTypeComplete`/`InputRequired`, `protocol.DraftVersion`.
- **Stateless per-request `_meta` handling.** A request carrying
  `io.modelcontextprotocol/protocolVersion` in `_meta` is served on the modern
  path: required fields (protocolVersion/clientInfo/clientCapabilities) are
  enforced (`-32602` if missing), the version is checked (`-32022` if
  unsupported; `server/discover` exempt), a request-scoped session is built from
  the declared capabilities (so sampling/elicitation gating works with no
  connection state), and the result is stamped `resultType:"complete"`. A
  request without the modern `_meta` is served unchanged under legacy semantics
  (dual-era).
- **MRTR — Multi Round-Trip Requests** (SEP-2575) — the stateless replacement
  for every server-initiated request. A modern tool handler that calls sampling,
  elicitation, or `roots/list` without a supplied response no longer fails with
  `ErrNoRequestSender`: the call is recorded and the request returns
  `resultType:"input_required"` with the `inputRequests` it needs (`InputRequest`
  ID/kind/payload). The client fulfills them and retries the same call carrying
  `io.modelcontextprotocol/inputResponses` in `_meta`; the handler is replayed
  and its input calls resolve from those responses (correlated by stable
  `ir-N` IDs). `requestState` is echoed back for client-side correlation. The
  legacy (session + RequestSender) path is unchanged. New public types
  `InputRequest`, `InputResponse`, `InputRequiredResult`, sentinel
  `ErrInputRequired`, and `InputKind{Sampling,Elicitation,Roots}`.
- **`subscriptions/listen`** (SEP; MCP 2026-07-28) — the stateless subscription
  method that replaces the GET SSE stream + `resources/subscribe`/`unsubscribe`.
  A client opts into notification types and resource `uris`; the server registers
  the URIs on the request-scoped session's SubscriptionManager and returns a
  `subscriptionId` (correlates the `io.modelcontextprotocol/subscriptionId` tag on
  subsequent notifications). No session → `-32602`. On Streamable HTTP the method
  is served as a **long-lived POST-response SSE stream**: the handler runs once to
  register + return the `subscriptionId`, then the response stays open forwarding
  notifications pushed via `HTTP.NotifySubscription` (each tagged with the
  `subscriptionId` in `_meta`) until the client disconnects, reusing the standing-
  stream registry for backpressure/cleanup.
- **Streamable HTTP routing headers** — `Mcp-Method` / `Mcp-Name` on the
  streamable POST path are validated against the JSON-RPC body (method, and the
  name/uri target for `tools/call`/`prompts/get`/`resources/read`); a mismatch
  returns `-32020` HeaderMismatch (`protocol.NewHeaderMismatch`). Validation is
  applied when the headers are present by default; `WithStreamableStateless()`
  additionally **hard-requires** `Mcp-Method` (absent → `-32020`) and **drops the
  `Mcp-Session-Id` lifecycle** (no minting, no per-request requirement) — the
  modern (MCP 2026-07-28) streamable model, opt-in until v2.
- **W3C Trace Context propagation** — a modern request carrying
  `io.modelcontextprotocol/traceparent` / `tracestate` / `baggage` in `_meta` has
  its distributed-trace position extracted (via the OTel `TraceContext`/`Baggage`
  propagators) so the server span joins the client's trace. The OTel middleware
  parents the top-level span onto the incoming remote span; `applyModern`
  propagates it to handler-level spans. `WithOTelPropagator` option added.
- **`tasks/update` + modern `tasks/list` retirement** — `tasks/update` refreshes
  a non-terminal task's `ttl` (null clears the deadline) so a slow task is not
  evicted before completion (`Server.UpdateAugTask`); `tasks/list` is served for
  legacy sessions but returns `MethodNotFound` for modern (2026-07-28) requests,
  per the tasks extension favoring direct task handles over listing.
- **Deterministic `tools/list` ordering** — tools are sorted by name, so
  `tools/list` returns a stable order across calls (was Go map-iteration order).
- **Full JSON Schema 2020-12** for `inputSchema`/`outputSchema` — the `schema`
  package now supports `$ref`/`$defs` (auto-emitted to break recursive types),
  `oneOf`/`anyOf`/`allOf`, and `if`/`then`/`else`: generated, marshaled, and
  enforced by the validator (`$ref` resolved against root `$defs`; unresolvable
  refs treated leniently). Legacy non-recursive output is unchanged.
- **Modern Icon fields** (SEP-973 evolution) — the `Icon` type gains additive
  modern fields `src` / `sizes` / `theme` alongside the legacy `uri` / `mimeType`
  / `size` (legacy JSON output is unchanged). `NewIcon(src)` + `WithMimeType` /
  `WithSizes` / `WithTheme` builders and `Normalize()` (fills each era's empty
  fields from the other) ease dual-era construction.
- **CacheableResult** (SEP-2549) — `WithResultCache(ttlMs, scope)` stamps
  `ttlMs`/`cacheScope` on cacheable results (`tools/list`, `prompts/list`,
  `resources/list`, `resources/read`, `resources/templates/list`) for modern
  clients; legacy responses are unaffected.
- **Modern error renumbering** — resource-not-found is `-32602` on the modern
  path (vs `-32001` on legacy), per the retirement of the `-32002` not-found code.
- **Deprecation posture** (SEP-2577) — sampling, roots, and logging are
  documented as deprecated in 2026-07-28 (12-month window; still fully
  functional) with their migration paths (provider APIs / tool params / stderr
  + OpenTelemetry). (Formalized with Go `// Deprecated:` markers in 1.23.0.)

### Certified — 2025-11-25 negotiable (Phase 3 complete)

`protocol.SupportedVersions` now includes `2025-11-25` and the default
(`protocol.MCPVersion`) advances to it. The server negotiates and honors all
four revisions; the conformance harness runs the full method set against each.
Completing the revision:

- **Input validation as tool execution errors** (SEP-1303) — invalid tool input
  is now returned as an `isError` result (via the new `ToolInputError`) instead
  of a `-32602` protocol error, so the model can self-correct. Applies to plain
  and task-augmented calls.
- **URL-mode elicitation** (SEP-1036) — `ElicitRequest` gains `mode`/`url`/
  `elicitationId`; new `Elicitor.ElicitURL`, `mcp.ElicitModeForm`/`ElicitModeURL`,
  the `elicitation.url` client capability (empty elicitation object = form only),
  and the `-32042` `URLElicitationRequired` error (`protocol.NewURLElicitationRequired`).
- **Elicitation enums & defaults** (SEP-1330 / SEP-1034) — expressible directly
  in the freeform `requestedSchema` (`oneOf`/`anyOf` with `const`+`title`,
  per-primitive `default`); no API change needed.
- `Implementation.description` (already present) confirmed advertised.

### Added — task-augmented requests (Phase 3, 2025-11-25, SEP-1686)

Full spec-conformant Tasks: a `tools/call` carrying a `task` field is accepted
immediately with a `CreateTaskResult`, runs in the background, and its outcome is
retrieved by polling. New over the wire:

- **Augmented `tools/call`** — `params.task: { ttl }` → returns `{ task: { taskId,
  status:"working", createdAt, lastUpdatedAt, ttl, pollInterval } }` and executes
  asynchronously.
- **`tasks/get`** (poll status), **`tasks/result`** (block until terminal, return
  exactly what the plain call would, with `io.modelcontextprotocol/related-task`
  meta), **`tasks/cancel`** (best-effort stop; `-32602` on already-terminal),
  **`tasks/list`** (cursor pagination).
- **`.TaskSupport(mcp.TaskSupportOptional|Required|Forbidden)`** builder →
  advertised as `execution.taskSupport` in `tools/list`; a task on a
  forbidden/unset tool, or a plain call on a required-task tool, is `-32601`.
- **`tasks` capability** auto-advertised (`{list, cancel, requests:{tools:{call}}}`)
  when any tool opts in. Task IDs are cryptographically random; the registry is
  bounded and TTL-evicting.

This is a distinct, spec-conformant implementation; the legacy `TaskManager`
(pre-spec `tasks/create` model) is left untouched.

### Certified — 2025-03-26 and 2025-06-18 negotiable (Phases 1–2)

`protocol.SupportedVersions` now lists `2024-11-05`, `2025-03-26`, and
`2025-06-18`; the default (`protocol.MCPVersion`) advances to `2025-06-18`. The
server negotiates and honors all three, and the conformance harness runs its
full method set against each (version-aware `initialize` echo).

- **Top-level `title`** (2025-06-18) on tools, resources, resource templates, and
  prompts — advertised as a sibling of `name` in every list response. New
  `.Title()` builder on resources and prompts (tools already had one via
  annotations); `Title` field on `ResourceInfo`/`ResourceTemplateInfo`/
  `PromptInfo`.
- `ProgressNotification.message` (2025-03-26) — already present, now covered.
- **JSON-RPC batching** (added 2025-03-26, removed 2025-06-18) is intentionally
  not supported: it was optional in 03-26 and gone by 06-18, so never batching is
  conformant across the supported range.

### Added — spec-revisions features (Phases 1–3, additive)

Feature work spanning the 2025-03-26 → 2025-11-25 revisions. These are additive
and land ahead of the formal per-revision certification (the negotiated default
stays 2024-11-05 until each revision's remaining wire-level work — batching
gating, `MCP-Protocol-Version` header enforcement, `tasks/*`, URL elicitation —
is complete and conformance-gated).

- **Streamable HTTP server** (2025-03-26). Opt in with `mcp.WithStreamable()` /
  `transport.WithStreamable()`: a single `/mcp` endpoint that accepts POST
  (JSON or SSE-framed reply, negotiated via `Accept`), GET (a standing
  server→client SSE stream keyed by `Mcp-Session-Id`), and DELETE (session
  teardown). `Mcp-Session-Id` is minted on `initialize` and required/echoed
  thereafter. The legacy HTTP+SSE endpoints remain the default, unchanged.
- **Audio & resource_link content, embedded resources** (2025-03-26/2025-06-18).
  `NewAudioContent`, `NewResourceLink`, `NewEmbeddedResource` on the ContentBlock
  union; they flow through tool results with no dispatcher change.
- **Icons metadata** (2025-11-25, SEP-973). `.Icons(...)` builder on tools,
  resources, and prompts; advertised in `tools/list`, `resources/list`,
  `resources/templates/list`, and `prompts/list`.
- **Sampling with tools** (2025-11-25, SEP-1577). `CreateMessageRequest` gains
  `Tools`/`ToolChoice`; `CreateMessageResult` gains `ToolCalls`; new
  `Session.CreateMessageWithTools`. New `SamplingTool`/`SamplingToolChoice`/
  `SamplingToolCall` types.
- **JSON Schema 2020-12 dialect** (2025-11-25, SEP-1613). Generated schemas
  carry the `$schema: …/2020-12/schema` marker; `schema.Dialect2020_12` constant.
- **OAuth/OIDC discovery metadata** (2025-06-18/2025-11-25, advertise-only). The
  `/.well-known/mcp` document can publish RFC 9728 protected-resource metadata
  (`authorizationServers`, `protectedResourceMetadata`, `resourceIndicator`,
  `scopesSupported`) and an `oidcConfiguration` pointer via
  `WithDiscoveryOAuthMetadata`. The library still performs no token handling.

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
- **`ContentBlock` content-block union.** `Content` is now the single canonical
  content-block union (aliased as `ContentBlock`), extended to cover `audio`,
  `resource_link`, and embedded `resource` blocks alongside text/image, with
  `NewAudioContent`, `NewResourceLink`, and `NewEmbeddedResource` constructors
  and optional content `Annotations`. Additive — text/image blocks serialize
  unchanged. The standalone prompt `TextContent`/`ImageContent` types remain for
  compatibility. This is the groundwork audio (Phase 1) and resource_link
  (Phase 2) build on.

### Per-revision conformance harness

- `mcp_conformance_test.go` drives a fully featured reference server through
  every method a revision defines and asserts the response shape; cases carry a
  `minVersion` so later phases extend the same gate.

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
