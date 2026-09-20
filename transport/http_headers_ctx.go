package transport

import (
	"context"
	"net/http"
)

type httpHeadersKey struct{}

// ContextWithHTTPHeaders attaches the inbound HTTP request headers so handlers
// can validate SEP-2243 Mcp-Param-* mirrors against tools/call arguments.
func ContextWithHTTPHeaders(ctx context.Context, headers http.Header) context.Context {
	if ctx == nil || headers == nil {
		return ctx
	}
	return context.WithValue(ctx, httpHeadersKey{}, headers.Clone())
}

// HTTPHeadersFromContext returns headers previously attached with
// ContextWithHTTPHeaders, or nil when absent (stdio/WebSocket).
func HTTPHeadersFromContext(ctx context.Context) http.Header {
	if ctx == nil {
		return nil
	}
	h, _ := ctx.Value(httpHeadersKey{}).(http.Header)
	return h
}

type requestHeadersKey struct{}

// ContextWithRequestHeaders attaches outbound per-request headers for the HTTP
// client transport (used for Mcp-Param-* on tools/call).
func ContextWithRequestHeaders(ctx context.Context, headers http.Header) context.Context {
	if ctx == nil || headers == nil || len(headers) == 0 {
		return ctx
	}
	return context.WithValue(ctx, requestHeadersKey{}, headers.Clone())
}

// RequestHeadersFromContext returns outbound headers for the current HTTP
// client Send, or nil when none were attached.
func RequestHeadersFromContext(ctx context.Context) http.Header {
	if ctx == nil {
		return nil
	}
	h, _ := ctx.Value(requestHeadersKey{}).(http.Header)
	return h
}
