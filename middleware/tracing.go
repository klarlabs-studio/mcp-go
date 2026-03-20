package middleware

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"strings"

	"github.com/felixgeelhaar/mcp-go/protocol"
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
			if existing := CorrelationIDFromContext(ctx); existing == "" {
				for _, header := range headerNames {
					if val := getHeaderFromContext(ctx, header); val != "" {
						ctx = ContextWithCorrelationID(ctx, val)
						break
					}
				}
			}

			if existing := TraceIDFromContext(ctx); existing == "" {
				for _, header := range headerNames {
					if val := getHeaderFromContext(ctx, header); val != "" {
						ctx = ContextWithTracing(ctx, val)
						break
					}
				}
			}

			return next(ctx, req)
		}
	}
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
		if strings.EqualFold(normalizedKey, CorrelationIDHeader) {
			correlationID = val
		}
		if strings.EqualFold(normalizedKey, TraceIDHeader) {
			traceID = val
		}
	}
	return
}
