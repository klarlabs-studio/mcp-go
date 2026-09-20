# Agent identity and Enterprise-Managed Auth

mcp-go **advertises** identity metadata so gateways and clients can locate the
right authorization servers. It does **not** validate tokens, run OAuth/OIDC
flows, or enforce DPoP. Token acquisition and enforcement stay at the gateway
(or your own middleware), matching the library's long-standing auth stance.

## What you can advertise today

Use discovery options when serving Streamable HTTP:

```go
discovery := mcp.NewServerDiscovery(manifest,
    mcp.WithDiscoveryOAuthMetadata(mcp.OAuthMetadata{
        AuthorizationServers:      []string{"https://auth.example.com"},
        ProtectedResourceMetadata: "https://mcp.example.com/.well-known/oauth-protected-resource",
        ResourceIndicator:         "https://mcp.example.com/mcp",
        ScopesSupported:           []string{"mcp:tools"},
        OIDCConfiguration:         "https://auth.example.com/.well-known/openid-configuration",
    }),
    mcp.WithDiscoveryAuthExtensions(mcp.AuthExtensions{
        DPoP:                       true, // RFC 9449
        TokenExchange:              true, // RFC 8693
        IDJAG:                      "https://auth.example.com/oauth/id-jag", // EMA / SEP-990
        WorkloadIdentityFederation: "https://sts.example.com",               // SEP-1933-style
    }),
)

mcp.ServeHTTP(ctx, srv, ":8080",
    mcp.WithDiscovery(discovery),
    mcp.WithServerCard(mcp.NewServerCardFromDiscovery(discovery)),
)
```

`WithDiscoveryAuthExtensions` adds an `authentication.extensions` object on
`/.well-known/mcp`. RFC 9728 Protected Resource Metadata continues to be served
at `/.well-known/oauth-protected-resource` when OAuth metadata is configured.

## Gateway patterns (out of library)

| Concern | Spec / SEP | Where it lives |
|---|---|---|
| Bearer / API key enforcement | — | Gateway or `WithAuthorizeFn` |
| DPoP proof validation | RFC 9449 | Gateway / AS |
| Token exchange | RFC 8693 | Authorization server |
| ID-JAG (EMA) | SEP-990 | Enterprise AS + gateway |
| Workload Identity Federation | SEP-1933 / WIMSE | Platform identity |

Clients that understand these hints can choose the right grant; the MCP process
still sees only the identity your gateway injects (for example via
`WithRequestContextFn` after mTLS or header validation).

## Server Cards

SEP-2127 Server Cards are connection metadata only. Do **not** put credentials
or internal topology in a card. Prefer:

```go
card := mcp.NewServerCardFromDiscovery(discovery,
    mcp.WithServerCardName("com.example/weather"),
)
mcp.ServeHTTP(ctx, srv, ":8080", mcp.WithServerCard(card), mcp.WithDiscovery(discovery))
```

Served at `GET /mcp/server-card`, with AI Catalog entries at
`/.well-known/ai-catalog.json` and `/.well-known/mcp/catalog.json`.
