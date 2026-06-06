package middleware

import (
	"context"
	"crypto/rand"
	"encoding/hex"

	"go.klarlabs.de/mcp/protocol"
)

// contextKey is a private type for context keys to avoid collisions.
type contextKey string

const requestIDKey contextKey = "requestID"

// RequestID returns middleware that injects a unique request ID into the context.
// If a request ID already exists in the context, it is preserved.
func RequestID() Middleware {
	return RequestIDWithGenerator(generateID)
}

// RequestIDWithGenerator returns middleware that uses a custom ID generator.
func RequestIDWithGenerator(generator func() string) Middleware {
	return func(next HandlerFunc) HandlerFunc {
		return func(ctx context.Context, req *protocol.Request) (*protocol.Response, error) {
			// Check if request ID already exists
			if existing := RequestIDFromContext(ctx); existing != "" {
				return next(ctx, req)
			}

			// Generate and inject new request ID
			id := generator()
			ctx = ContextWithRequestID(ctx, id)
			return next(ctx, req)
		}
	}
}

// RequestIDFromContext returns the request ID from the context, or empty string if not set.
func RequestIDFromContext(ctx context.Context) string {
	id, _ := ctx.Value(requestIDKey).(string)
	return id
}

// ContextWithRequestID returns a new context with the request ID set.
func ContextWithRequestID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, requestIDKey, id)
}

// generateID generates a random request ID.
// Uses crypto/rand for better uniqueness than time-based IDs.
func generateID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
