package middleware

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"strings"

	"go.klarlabs.de/mcp/protocol"
)

const correlationIDKey contextKey = "correlationID"

var (
	CorrelationIDHeader = "X-Correlation-ID"
	TraceIDHeader       = "X-Trace-ID"
)

func Tracing() Middleware {
	return TracingWithHeaders(CorrelationIDHeader, TraceIDHeader)
}

func TracingWithHeaders(correlationHeader, traceHeader string) Middleware {
	return func(next HandlerFunc) HandlerFunc {
		return func(ctx context.Context, req *protocol.Request) (*protocol.Response, error) {
			var correlationID, traceID string

			if existing := CorrelationIDFromContext(ctx); existing != "" {
				correlationID = existing
			} else {
				correlationID = generateTracingID()
			}

			traceID = generateTracingID()

			ctx = ContextWithCorrelationID(ctx, correlationID)
			ctx = ContextWithTracing(ctx, traceID)

			return next(ctx, req)
		}
	}
}

func TracingFromHeaders(headerNames ...string) Middleware {
	return func(next HandlerFunc) HandlerFunc {
		return func(ctx context.Context, req *protocol.Request) (*protocol.Response, error) {
			// Inbound ids are attacker-controlled: they flow into logs and
			// downstream correlation. Adopt only well-formed values so a crafted
			// header cannot inject newlines/control characters (log forging) or
			// bloat the id. Malformed headers are ignored, not adopted.
			if existing := CorrelationIDFromContext(ctx); existing == "" {
				for _, header := range headerNames {
					if id, ok := sanitizeTracingID(getHeaderFromContext(ctx, header)); ok {
						ctx = ContextWithCorrelationID(ctx, id)
						break
					}
				}
			}

			if existing := TraceIDFromContext(ctx); existing == "" {
				for _, header := range headerNames {
					if id, ok := sanitizeTracingID(getHeaderFromContext(ctx, header)); ok {
						ctx = ContextWithTracing(ctx, id)
						break
					}
				}
			}

			return next(ctx, req)
		}
	}
}

// maxTracingIDLen caps the length of an adopted inbound trace/correlation id.
const maxTracingIDLen = 128

// sanitizeTracingID validates an inbound (client-supplied) trace/correlation
// id. It accepts a non-empty value of at most maxTracingIDLen characters drawn
// from an unreserved set (alphanumerics and '-', '_', '.', ':') so the id is
// safe to embed in structured logs and propagate. It reports ok=false for
// empty, over-long, or otherwise malformed values, which callers treat as
// "no id supplied".
func sanitizeTracingID(s string) (string, bool) {
	if s == "" || len(s) > maxTracingIDLen {
		return "", false
	}
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z',
			r >= 'A' && r <= 'Z',
			r >= '0' && r <= '9',
			r == '-', r == '_', r == '.', r == ':':
		default:
			return "", false
		}
	}
	return s, true
}

func ContextWithTracing(ctx context.Context, traceID string) context.Context {
	return context.WithValue(ctx, tracingKey{}, traceID)
}

func TraceIDFromContext(ctx context.Context) string {
	if id, ok := ctx.Value(tracingKey{}).(string); ok {
		return id
	}
	return ""
}

type tracingKey struct{}

func CorrelationIDFromContext(ctx context.Context) string {
	if id, ok := ctx.Value(correlationIDKey).(string); ok {
		return id
	}
	return ""
}

func ContextWithCorrelationID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, correlationIDKey, id)
}

func getHeaderFromContext(ctx context.Context, headerName string) string {
	if val, ok := ctx.Value(headerContextKey(headerName)).(string); ok {
		return val
	}
	return ""
}

func ContextWithHeader(ctx context.Context, headerName, headerValue string) context.Context {
	return context.WithValue(ctx, headerContextKey(headerName), headerValue)
}

type headerContextKey string

func generateTracingID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func FormatTracingHeaders(ctx context.Context) map[string]string {
	headers := make(map[string]string)

	if corrID := CorrelationIDFromContext(ctx); corrID != "" {
		headers[CorrelationIDHeader] = corrID
	}

	if traceID := TraceIDFromContext(ctx); traceID != "" {
		headers[TraceIDHeader] = traceID
	}

	return headers
}

func ParseTracingHeaders(headers map[string]string) (correlationID, traceID string) {
	for key, val := range headers {
		normalizedKey := strings.ToLower(strings.TrimSpace(key))
		// These ids are inbound and untrusted; drop malformed values rather than
		// return them for logging/propagation.
		if strings.EqualFold(normalizedKey, CorrelationIDHeader) {
			if id, ok := sanitizeTracingID(val); ok {
				correlationID = id
			}
		}
		if strings.EqualFold(normalizedKey, TraceIDHeader) {
			if id, ok := sanitizeTracingID(val); ok {
				traceID = id
			}
		}
	}
	return
}
