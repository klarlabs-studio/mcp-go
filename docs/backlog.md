
## 2026 Roadmap Alignment

Foundation work for aligning mcp-go with MCP core 2026 roadmap priorities including horizontal scaling, discovery, and enterprise readiness.

---

## Spec Revisions Alignment (2024-11-05 → 2026-07-28)

Bring mcp-go current across every MCP spec revision. Backbone: protocol version negotiation. Phases 0–4 shipped on **v1** (no `/v2` module path): version negotiation, Streamable HTTP, RFC 9728 advertise-only auth metadata, tasks/icons/sampling-tools, then the 2026-07-28 stateless rewrite (`server/discover`, MRTR, `subscriptions/listen`, routing headers, CacheableResult, extensions). Auth stays out-of-library by design. Preserve differentiators: MCP Apps, WebSocket, gRPC, middleware suite. Detailed plan in docs/revisions-roadmap.md.

---

---

## Spec extensions after 2026-07-28

Extensions and SEPs adopted on v1 once the 2026-07-28 revision shipped: Skills, Server Cards, progressive tool discovery, Streamable HTTP over stdio, webhooks and EMA, `Mcp-Param-*` headers, and the client defaulting to the modern protocol (v1.27.0–v1.28.0). Several stay experimental until their SEP or working group settles; those are blocked tasks in roady.

---

## Maintenance and correctness

Open correctness and test-infrastructure work that is not a spec revision: whether to advertise the `tasks` capability only from 2025-11-25, and running the e2e compliance tests on the real dispatcher.
