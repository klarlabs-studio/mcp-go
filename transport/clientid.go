package transport

import "context"

type clientIDKey struct{}

// ContextWithClientID attaches the connection's client id to the context so
// request handlers can correlate a request (e.g. resources/subscribe) with the
// client's server-push stream.
func ContextWithClientID(ctx context.Context, clientID string) context.Context {
	return context.WithValue(ctx, clientIDKey{}, clientID)
}

// ClientIDFromContext returns the client id attached by the transport, or ""
// when the transport does not track clients.
func ClientIDFromContext(ctx context.Context) string {
	if id, ok := ctx.Value(clientIDKey{}).(string); ok {
		return id
	}
	return ""
}
