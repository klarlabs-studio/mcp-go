# mcp-go Spec Revisions Roadmap

Plan to bring mcp-go current across **every** MCP spec revision, from the pinned
`2024-11-05` baseline through the `2026-07-28` release candidate.

**Status of the world (August 2026):**

| Revision | Role | mcp-go today |
|---|---|---|
| `2024-11-05` | original | negotiated via initialize |
| `2025-03-26` | Streamable HTTP, annotations | certified |
| `2025-06-18` | elicitation, structured output, resource links | certified |
| `2025-11-25` | tasks, icons, sampling-with-tools | **initialize default (`MCPVersion`)** |
| `2026-07-28` | **current published** — stateless rewrite | dual-era: `server/discover` + per-request `_meta`; `ModernVersion` in `SupportedVersions`; initialize does not speak this revision |

The official spec released `2026-07-28` on that date. mcp-go serves it on the
modern path and advertises it from `server/discover`. The initialize handshake
stops at `2025-11-25` because 2026-07-28 retires initialize.

Phases 0–4 of this document shipped across v1.22–v1.26. Checkboxes below are
historical planning notes; treat the status table as authoritative.

### Remaining (evaluated)

There is **no `/v2` Go module path**. Phase 4 shipped on v1 (stateless default in v1.24.0; spec-alignment in v1.26.0). A major-version import rewrite is not a planned release.

| Item | Verdict |
|---|---|
| OAuth token enforcement, `iss` validation, DCR, CIMD | **Out of library.** Auth stays advertise-only; enforcement belongs at the gateway. RFC 9728 `/.well-known/oauth-protected-resource` is served when discovery OAuth metadata is configured. |
| JSON-RPC batching | **Closed — will not implement.** Optional in 2025-03-26, removed in 2025-06-18. Never batching is conformant. |
| `x-mcp-header` → `Mcp-Param-*` (SEP-2243) | **In library.** Servers emit via `jsonschema:"header=Region"`; Streamable HTTP clients mirror arguments into `Mcp-Param-*` after `ListTools` (invalid annotations excluded); servers validate header↔body on HTTP tools/call. |
| Unsolicited task handles (SEP-2663) | **In library, modern path.** A `TaskSupportRequired` tool returns a flat `CreateTaskResult` (`resultType: "task"`) on a plain `tools/call` when the client declares `io.modelcontextprotocol/tasks`. The retired `task` field is ignored (optional tools run synchronously). Missing the extension is `-32021`. Legacy callers keep the 2025-11-25 nested `{task: …}` opt-in. |
| `tasks/result` / `notifications/elicitation/complete` | **Gated off for modern** (`-32601`). Clients poll `tasks/get` (inlined `result`/`error`) and retry the original method under MRTR. Legacy keeps both methods. |
| `tasks/update` `inputResponses` | **In library.** A task that needs elicitation/sampling/roots pauses at `input_required` with a keyed `inputRequests` map. `tasks/update` accepts map (or array) `inputResponses`, ignores unknown keys, and replays the handler. TTL refresh via `ttl`/`ttlMs` is still accepted. Modern ack is empty. |
| `notifications/tasks` | **In library.** `subscriptions/listen` accepts `notifications.taskIds` (requires the tasks extension). Status changes push `notifications/tasks` carrying the same DetailedTask as `tasks/get`. |
| OpenAPI tool descriptors | **Not in library.** Typed Go handlers remain the registration surface. |
| Skills extension (SEP-2640) | **In library, experimental.** `Skill(path).FromDir` / `FromArchive` / `SkillsFromDir` / `SkillTemplate` serve `skill://` resources and `skill://index.json` (`skill-md`, `archive`, `mcp-resource-template`). Advertise `io.modelcontextprotocol/skills`. API may change until the SEP is Final. |
| Roots / Sampling / Logging / HTTP+SSE / `includeContext` thisServer\|allServers | **Deprecated, kept** for the 12-month window. |
| Client modern default | **In library.** `client.New` defaults to `ModernVersion` + per-request `_meta`; `Connect` prefers `server/discover` with `Initialize` fallback. |

Phase 1–3 checkboxes below that are still unmarked were implemented in v1.22–v1.25 (Streamable HTTP, audio, progress `message`, sampling-with-tools, icons, JSON Schema 2020-12, `Implementation.description`, input validation as `isError`). Treat the table above as the live remainder.

---

## Guiding strategy

1. **Version negotiation is the backbone.** Every phase gates its behavior on a
   negotiated protocol version. Build the negotiation layer first (Phase 0) so
   later features can be advertised/enabled per revision instead of hard-coded.
2. **Fix the foundation before adding surface.** Dead methods, session injection,
   and Streamable HTTP were the original blockers; they shipped in v1.22–v1.24.
   Spec-alignment work after that is wire-format and default-behavior correctness.
3. **Certify one revision per release.** Each phase ends with a conformance test
   suite proving the negotiated revision is fully honored, then bumps the default.
4. **The stateless `2026-07-28` rewrite stays on v1.** Dual-era (initialize through
   `2025-11-25`, modern via `server/discover` + `_meta`). No `/v2` module path.

**Release mapping (proposed):**

| Phase | Revision certified | Release | Breaking |
|---|---|---|---|
| 0 — Foundation | (still 2024-11-05) | v1.22.0 | no |
| 1 | 2025-03-26 | v1.23.0 | no (additive) |
| 2 | 2025-06-18 | v1.24.0 | batching reversal (guarded) |
| 3 | 2025-11-25 | v1.25.0 | validation-error channel |
| 4 | 2026-07-28 | **v1.26.0** | ServeHTTP defaults to stateless Streamable HTTP; no module-path break |

---

## Phase 0 — Foundation (v1.22.0, no new spec)

Pure correctness. Make what's already built actually reachable, and stand up the
negotiation machinery. Highest ROI, lowest risk. **Shipped.**

- [x] **Version negotiation layer.** `protocol.SupportedVersions` /
  `InitializeVersions`. `handleInitialize` parses `protocolVersion` and
  negotiates via `NegotiateVersion`.
- [x] **Capture client capabilities at initialize.** Parsed into
  `ClientCapabilities` on the session.
- [x] **Inject a `Session` into the request context** from stdio/http/ws
  transports (`ContextWithSession`).
- [x] **Wire the dead methods** into `methodHandlers()`: `completion/complete`,
  `logging/setLevel`, `resources/templates/list`, `notifications/initialized`.
- [x] **Advertise the `completions` capability** in the initialize response.
- [x] **Content block refactor.** Shared `Content` / `ContentBlock` union
  (text, image, audio, resource_link).
- [x] **Conformance harness.** `mcp_conformance_test.go` / `mcp_revisions_test.go`.

---

## Phase 1 — Certify 2025-03-26 (v1.23.0)

Adds the modern transport and the annotation/content surface. Additive.

- [ ] **Streamable HTTP server** (single endpoint, `POST` that can upgrade to SSE,
  `Mcp-Session-Id` session header, server-initiated GET SSE stream). Replace the
  `?clientId=` query-param correlation in `transport/http.go` (keep old HTTP+SSE
  behind an option for back-compat). The **client already speaks this**
  (`client/http.go:271`) — this closes the server/client mismatch.
- [ ] **`Mcp-Session-Id`** minting/echo on the server; session store already
  exists (`transport/store.go`).
- [ ] **Audio content** (`type:"audio"`, base64 `data` + `mimeType`) via the new
  `ContentBlock` union.
- [ ] **JSON-RPC batching** — **will not implement.** Optional in this revision
  and removed in 2025-06-18; never batching is conformant.
- [ ] **Tool annotations** — already implemented (`server/annotations.go`); add
  conformance coverage.
- [ ] **`ProgressNotification.message`** field.
- [x] **OAuth 2.1 posture** — decision point (see Cross-cutting): document the
  gateway-terminated stance and expose the AS-metadata `.well-known` hook, since
  in-library auth is out of scope by design. RFC 9728
  `/.well-known/oauth-protected-resource` is served when discovery OAuth
  metadata is configured (advertise-only).
- [ ] Bump negotiated default to `2025-03-26`.

---

## Phase 2 — Certify 2025-06-18 (v1.24.0)

The last published stable before `2025-11-25`. Mostly already built — this phase
is about wiring, headers, and the batching reversal.

- [ ] **Reject batching** — N/A: this library never accepted request arrays.
- [x] **Enforce `MCP-Protocol-Version` header** on all post-initialize HTTP
  requests; reject/deprecate missing header per spec.
- [x] **Resource links** — `ResourceLink` content block (`type:"resource_link"`)
  in tool results (union type from Phase 0).
- [x] **Structured output** — already implemented (`OutputSchema`,
  `StructuredResult`); certify.
- [x] **`title` fields** — expose top-level `title` on tools/resources/prompts
  (currently only on tool annotations); `name` stays the programmatic id.
- [x] **`_meta` on more types** (already on tools; extend to resources/prompts).
- [x] **Completion `context`** field (previously-resolved argument variables).
- [x] **Auth metadata** — Protected Resource Metadata (RFC 9728) advertisement +
  RFC 8707 Resource Indicator awareness in the discovery doc (advertise-only;
  enforcement stays at the gateway). The RFC 9728 document is served at
  `/.well-known/oauth-protected-resource` when OAuth metadata is configured.
- [ ] Bump negotiated default to `2025-06-18`.

---

## Phase 3 — Certify 2025-11-25 (v1.25.0) — reach the current published spec

- [ ] **Wire Tasks to JSON-RPC.** `TaskManager` exists (`server/tasks.go`) but is
  **not dispatched**. Add `tasks/*` to `methodHandlers()`. Align method names to
  the spec (`tasks/list`, `tasks/get`, `tasks/result`) — the current code uses
  `tasks/create`/`tasks/get`. Note `tasks/list` exists here but is **removed in
  2026-07-28**, so keep it version-gated.
- [ ] **Sampling-with-tools** — add `tools`/`toolChoice` to
  `sampling/createMessage` (a `CreateMessageWithTools` path). Missing today.
- [ ] **Icons** metadata array on tools/resources/resource-templates/prompts.
- [ ] **URL-mode elicitation** — `elicitationId`, out-of-band URL flow,
  `notifications/elicitation/complete` (all **removed in 2026-07-28**; gate them).
- [ ] **JSON Schema 2020-12 as default dialect** in `schema/schema.go`.
- [ ] **Input-validation errors as Tool Execution Errors**, not Protocol Errors
  (so the model can self-correct) — change the dispatch error path (`tool.go:213`).
- [ ] **Elicitation enum/default rework** (titled/untitled, single/multi-select;
  defaults for all primitives).
- [ ] **OIDC Discovery 1.0** + incremental scope consent advertisement in the
  auth metadata.
- [ ] `Implementation.description` field.
- [ ] Bump negotiated default to `2025-11-25`. **mcp-go is now current.**

---

## Phase 4 — 2026-07-28 (v1) — stateless rewrite

The largest transition since launch: a ground-up stateless redesign. Shipped on
**v1** behind Streamable HTTP (`WithStreamable()` / ServeHTTP default). There is
no `/v2` module path.

**Lifecycle / methods**
- [x] Implement **`server/discover`** (advertise supported versions, capabilities,
  identity) — replaces `initialize`. Keep `initialize` as a back-compat probe.
  (`handleServerDiscover` advertises versions + the extensions map + identity; it
  is exempt from the modern version check so a client uses it to learn versions.
  `initialize` still serves legacy callers.)
- [x] **Stateless request model** — every request self-describes via `_meta`:
  `io.modelcontextprotocol/protocolVersion`, `/clientInfo`, `/clientCapabilities`,
  `/logLevel`. Remove reliance on init-time state. (`parseModernMeta`/`applyModern`
  require the three fields, build a request-scoped session from the declared
  capabilities, and set the per-request log level — no connection state.)
- [x] **Remove** `initialize`/`notifications/initialized`, `ping`,
  `logging/setLevel`, `notifications/roots/list_changed`,
  `resources/subscribe`/`unsubscribe`, the GET stream (all gated to this version).
  (`retiredInModern` returns MethodNotFound for these on the modern path; legacy
  callers keep them. The GET stream is dropped by `WithStreamableStateless`.)
- [x] **`subscriptions/listen`** — single long-lived POST-response stream
  replacing the GET endpoint + subscribe/unsubscribe; clients opt into notif
  types; tag with `io.modelcontextprotocol/subscriptionId`. (Protocol method +
  server-side registration + `subscriptionId` + the long-lived POST-response SSE
  stream with `subscriptionId`-tagged notifications all landed.)

**Multi Round-Trip Requests (MRTR)** — replaces all server-initiated requests
- [x] Every result carries required `resultType` (`"complete"` | `"input_required"`).
- [x] Replace server-initiated `roots/list`, `sampling/createMessage`,
  `elicitation/create` with `InputRequiredResult` + client retry carrying
  `inputResponses`; correlate via `requestState`. (Replay/continuation model:
  broker fulfills input calls from client-supplied responses or records them as
  pending `input_required`; handler is re-run each round.)

**Transport**
- [x] **Drop `Mcp-Session-Id`** (sessions removed from the protocol layer).
  (`WithStreamableStateless()` drops the session-id lifecycle on the POST path.)
- [x] **Remove SSE resumability** (`Last-Event-ID`, event IDs). (Vacuous —
  mcp-go never emitted SSE event IDs nor honored `Last-Event-ID`, so there is no
  resumability to remove; the modern stream carries only `subscriptionId`-tagged
  frames.)
- [x] **Required routing headers** `Mcp-Method`, `Mcp-Name` on Streamable HTTP POST.
  (Validated-when-present by default; `WithStreamableStateless()` hard-requires
  `Mcp-Method` → `-32020` on absence/mismatch.)
- [x] **`CacheableResult`** — `ttlMs` + `cacheScope` on `tools/list`,
  `prompts/list`, `resources/list`, `resources/read`, `resources/templates/list`.
  (`WithResultCache(ttlMs, scope)` configures the hint; `applyCacheHint` stamps it
  onto the five `cacheableMethods` results for modern callers only.)
- [x] **W3C Trace Context** in `_meta` (`traceparent`/`tracestate`/`baggage`) —
  ties into existing OTel middleware.
- [x] Deterministic ordering of `tools/list`.

**Extensions framework (SEP-2133)**
- [x] Add the **`extensions` capability map** (reverse-DNS ids) to client/server
  capabilities. (`extensionsMap` advertises the reverse-DNS ids the server
  supports — `io.modelcontextprotocol/ui` today — under `capabilities.extensions`
  in `server/discover`.)
- [x] Re-express **MCP Apps** through the extensions framework. RESOLVED: the MCP
  Apps extension identifier is `io.modelcontextprotocol/ui` (NOT `/apps` — the
  feature is "MCP Apps" but the negotiated id is `/ui`, per the ext-apps spec
  2026-01-26). mcp-go already advertises it via `capabilities.extensions`
  (`ExtensionUI`) and associates tools via `_meta.ui.resourceUri` (+ the flat
  `_meta["ui/resourceUri"]`, which the spec deprecates for removal before GA — we
  keep emitting it for host compat). Already conformant.
- [x] Move **Tasks** to the `io.modelcontextprotocol/tasks` extension: polling
  `tasks/get`, new `tasks/update`, **remove `tasks/list`**, allow unsolicited task
  handles. (tasks/list gated off for modern; extension advertised and required
  on modern `tasks/*`. `TaskSupportRequired` returns a flat `CreateTaskResult`.
  `tasks/result` is MethodNotFound. `tasks/get` inlines the terminal result.
  `tasks/update` fulfills `inputResponses` and replays the handler from
  `input_required`. `notifications/tasks` is pushed to `subscriptions/listen`
  streams that opted in via `taskIds`.)

**Auth / errors / deprecations**
- [~] `iss` validation (RFC 9207); `application_type` in DCR; Client ID Metadata
  Documents over DCR. RESOLVED as OUT OF SCOPE: in-library auth was deliberately
  removed (see Cross-cutting "Auth stance") — enforcement (iss validation, DCR)
  belongs at the gateway, and mcp-go stays advertise-only. Not implemented by
  design; revisit only if the auth stance changes.
- [x] Error renumbering: resource-not-found `-32002` → `-32602`;
  `HeaderMismatch` `-32001`→`-32020`, etc.; adopt the `-32020..-32099` MCP range.
  (`modernizeError` maps resource-not-found to `-32602` for modern callers while
  legacy keeps `-32001` — covered by `TestModern_ResourceNotFoundRenumbered`. The
  modern MCP-specific codes already live in the reserved range: `HeaderMismatch`
  `-32020`, `MissingRequiredClientCapability` `-32021`, `UnsupportedProtocolVersion`
  `-32022`, `URLElicitationRequired` `-32042`. mcp-go emits no other legacy
  `-3200x` code from a handler — auth/rate-limit are HTTP/gateway-terminated in
  the modern model, not JSON-RPC-renumbered by the spec — so the sweep is complete.)
- [x] **Deprecate (keep working 12 mo)** Roots, Sampling, Logging; document the
  migrations (tool params / provider APIs / stderr+OTel). (`Session.CreateMessage`/
  `CreateMessageWithTools`, `Session.ListRoots`, and the `Session.Log`/`Debug`/…/
  `Emergency` cluster carry Go `// Deprecated:` markers; all stay functional. See
  `docs/deprecations.md` for the migrations. `SetLogLevel`/`LogLevel` are retained
  — the modern log level travels in `_meta`.)
- [x] Loosen `inputSchema`/`outputSchema` to full JSON Schema 2020-12 (`$ref`,
  `oneOf`/`anyOf`, conditionals).
- [x] Make `Stateless` the default — shipped in **v1.24.0**. `WithStreamable()`
  uses the 2026-07-28 model (drops `Mcp-Session-Id`, hard-requires `Mcp-Method`);
  `WithStreamableStateful()` is the opt-out into the legacy session-negotiated
  (2025-03-26) path. Behavior change to the Streamable HTTP default, released as
  a minor because the only consumers are the maintainer's own fleet (all stdio —
  unaffected) and they upgrade in lockstep. See CHANGELOG [1.24.0].

---

## Cross-cutting decisions

- **Auth stance.** In-library auth was deliberately removed ("out of scope").
  Recommendation: keep enforcement out, but ship **advertise-only** OAuth/OIDC
  metadata (Protected Resource Metadata, `.well-known`) so mcp-go servers are
  discoverable by spec-compliant clients while auth terminates at the gateway.
  Enterprise-Managed Auth (ID-JAG, SEP-990) stays a documented gateway pattern.
- **Content block union** (Phase 0) is a prerequisite shared by Phases 1–2.
- **Version-gating helper.** A single `negotiatedVersion(ctx)` accessor that
  handlers consult, so batching (on@03-26/off@06-18), `tasks/list`
  (on@11-25/off@07-28), and URL elicitation (on@11-25/off@07-28) toggle cleanly.
- **Differentiators to preserve** through the churn: MCP Apps, WebSocket, gRPC,
  and the batteries-included middleware suite — none are in go-sdk.

---

## Sequencing summary

```
Phase 0  Foundation ........ wire dead methods, sessions, negotiation   v1.22.0
Phase 1  2025-03-26 ........ Streamable HTTP, audio, OAuth doc            v1.23.0
Phase 2  2025-06-18 ........ resource links, headers                      v1.24.0
Phase 3  2025-11-25 ........ tasks-wired, icons, sampling-tools           v1.25.0
Phase 4  2026-07-28 ........ stateless rewrite, MRTR, extensions          v1.26.0
```

Phases 0–4 shipped on v1. Dual-era: initialize through `2025-11-25`; modern via
`server/discover` + per-request `_meta`.
