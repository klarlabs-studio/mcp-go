# mcp-go Spec Revisions Roadmap

Plan to bring mcp-go current across **every** MCP spec revision, from the pinned
`2024-11-05` baseline through the `2026-07-28` release candidate.

**Status of the world (July 2026):**

| Revision | Role | mcp-go today |
|---|---|---|
| `2024-11-05` | original | pinned & hard-returned (no negotiation) |
| `2025-03-26` | Streamable HTTP, annotations, OAuth 2.1, batching | partial (annotations ✅, transport ❌) |
| `2025-06-18` | elicitation, structured output, resource links | partial (features ✅, wiring ❌) |
| `2025-11-25` | **current published** — tasks, icons, sampling-with-tools | partial (tasks unwired) |
| `2026-07-28` | RC — stateless rewrite | none |

The official `modelcontextprotocol/go-sdk` negotiates **all five** revisions and
ships a `2026-07-28` beta. Closing the version gap is the strategic objective.

---

## Guiding strategy

1. **Version negotiation is the backbone.** Every phase gates its behavior on a
   negotiated protocol version. Build the negotiation layer first (Phase 0) so
   later features can be advertised/enabled per revision instead of hard-coded.
2. **Fix the foundation before adding surface.** The audit found features already
   written but dead: `completion/complete`, `logging/setLevel`,
   `resources/templates/list`, and all `tasks/*` are **not wired into the
   dispatcher** (`mcp.go:672`); no transport injects a `Session` into the request
   context, so sampling/elicitation/channels are **unreachable**; the bundled
   HTTP server is old HTTP+SSE with `?clientId=` while the client already speaks
   Streamable HTTP. Shipping new spec features on top of this is building on sand.
3. **Certify one revision per release.** Each phase ends with a conformance test
   suite proving the negotiated revision is fully honored, then bumps the default.
4. **The stateless `2026-07-28` rewrite is a major version (v2).** Everything
   before it is additive/back-compatible (v1.x).

**Release mapping (proposed):**

| Phase | Revision certified | Release | Breaking |
|---|---|---|---|
| 0 — Foundation | (still 2024-11-05) | v1.22.0 | no |
| 1 | 2025-03-26 | v1.23.0 | no (additive) |
| 2 | 2025-06-18 | v1.24.0 | batching reversal (guarded) |
| 3 | 2025-11-25 | v1.25.0 | validation-error channel |
| 4 | 2026-07-28 RC | **v2.0.0** | yes — stateless rewrite |

---

## Phase 0 — Foundation (v1.22.0, no new spec)

Pure correctness. Make what's already built actually reachable, and stand up the
negotiation machinery. Highest ROI, lowest risk.

- [ ] **Version negotiation layer.** Add `protocol.SupportedVersions` (ordered).
  Parse the client's `protocolVersion` in `handleInitialize` (`mcp.go:746`) —
  today it ignores `req.Params` entirely. Negotiate: echo the client's version if
  supported, else the newest common one; return
  `UnsupportedProtocolVersion` when none overlap. Stop hard-returning
  `protocol.MCPVersion` (`server/server.go:278`).
- [ ] **Capture client capabilities at initialize.** Parse `capabilities` from
  initialize params into `ClientCapabilities` (`server/session.go:37`) — currently
  never populated, which is why elicitation/sampling gating never fires.
- [ ] **Inject a `Session` into the request context** from stdio/http/ws
  transports (wire `ContextWithSession`, re-exported at `mcp.go:211` but never
  called). Unblocks sampling, elicitation, channels, logging notifications.
- [ ] **Wire the dead methods** into `methodHandlers()` (`mcp.go:672`):
  `completion/complete` (`server.HandleCompletion`), `logging/setLevel`
  (`server/logging.go`), `resources/templates/list` (`Server.ResourceTemplates`),
  `notifications/initialized` (accept & no-op).
- [ ] **Advertise the `completions` capability** in the initialize response — it's
  declared (`server/server.go`) but never advertised (`mcp.go:751`).
- [ ] **Content block refactor.** Replace the three duplicated `Content` structs
  (`server/sampling.go:24`, `server/prompt.go:9`, `server/tool.go:16`) with one
  `ContentBlock` union. Prerequisite for audio/resource_link/embedded in later
  phases. Keep text+image behavior identical.
- [ ] **Conformance harness.** Table-driven suite that drives a server through the
  full `2024-11-05` method set over each transport; becomes the per-revision gate.

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
- [ ] **JSON-RPC batching** — accept request arrays (added this revision;
  **removed again in 2025-06-18**, so guard it on negotiated version).
- [ ] **Tool annotations** — already implemented (`server/annotations.go`); add
  conformance coverage.
- [ ] **`ProgressNotification.message`** field.
- [ ] **OAuth 2.1 posture** — decision point (see Cross-cutting): document the
  gateway-terminated stance and expose the AS-metadata `.well-known` hook, since
  in-library auth is out of scope by design.
- [ ] Bump negotiated default to `2025-03-26`.

---

## Phase 2 — Certify 2025-06-18 (v1.24.0)

The last published stable before `2025-11-25`. Mostly already built — this phase
is about wiring, headers, and the batching reversal.

- [ ] **Reject batching** when the negotiated version is `2025-06-18` (reversal of
  Phase 1). Version-gated in the wire decoder.
- [ ] **Enforce `MCP-Protocol-Version` header** on all post-initialize HTTP
  requests; reject/deprecate missing header per spec.
- [ ] **Resource links** — `ResourceLink` content block (`type:"resource_link"`)
  in tool results (union type from Phase 0).
- [ ] **Structured output** — already implemented (`OutputSchema`,
  `StructuredResult`); certify.
- [ ] **`title` fields** — expose top-level `title` on tools/resources/prompts
  (currently only on tool annotations); `name` stays the programmatic id.
- [ ] **`_meta` on more types** (already on tools; extend to resources/prompts).
- [ ] **Completion `context`** field (previously-resolved argument variables).
- [ ] **Auth metadata** — Protected Resource Metadata (RFC 9728) advertisement +
  RFC 8707 Resource Indicator awareness in the discovery doc (advertise-only;
  enforcement stays at the gateway).
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

## Phase 4 — 2026-07-28 Release Candidate (v2.0.0) — stateless rewrite

The largest transition since launch: a ground-up stateless redesign. Ship behind
an explicit opt-in (`StreamableHTTPOptions.Stateless`, mirroring go-sdk) so v1
clients keep working, then make it the default in v2.

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
- [~] Move **Tasks** to the `io.modelcontextprotocol/tasks` extension: polling
  `tasks/get`, new `tasks/update`, **remove `tasks/list`**, allow unsolicited task
  handles. (tasks/update added; tasks/list gated off for modern; extension
  already advertised. Unsolicited task handles remain.)

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
- [x] Make `Stateless` the default — shipped in **v1.24.0** (NOT v2.0.0, by
  decision). `WithStreamable()` now defaults to the stateless (2026-07-28) model
  (drops `Mcp-Session-Id`, hard-requires `Mcp-Method`); `WithStreamableStateful()`
  is the opt-out into the legacy session-negotiated (2025-03-26) path. This is a
  behavior change to the streamable HTTP default, released as a minor because the
  only consumers are the maintainer's own fleet (all stdio — unaffected) and they
  upgrade in lockstep; there are no external consumers. The `/v2` module-path tax
  is intentionally avoided. See CHANGELOG [1.24.0].

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
Phase 1  2025-03-26 ........ Streamable HTTP, audio, batching, OAuth doc  v1.23.0
Phase 2  2025-06-18 ........ resource links, headers, batching-off        v1.24.0
Phase 3  2025-11-25 ........ tasks-wired, icons, sampling-tools  [CURRENT] v1.25.0
Phase 4  2026-07-28 ........ stateless rewrite, MRTR, extensions          v2.0.0
```

Phases 0–3 are additive and safe to ship incrementally. Phase 4 is the v2 break;
gate it behind `Stateless` opt-in until the spec is final (July 28, 2026).
