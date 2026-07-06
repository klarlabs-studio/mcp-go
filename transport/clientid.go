package transport

import (
	"context"
	"crypto/rand"
	"encoding/hex"
)

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

// newSessionID mints an unguessable server-side session identifier for an SSE
// connection. The id is the map key for the client's push channel, so it must
// not be attacker-controlled: a caller-supplied clientId is advisory only. We
// use crypto/rand rather than a timestamp so an attacker cannot predict or
// collide with another client's id and hijack its stream. rand.Read never
// fails on supported platforms; if it does, we surface the error and refuse
// the connection rather than fall back to a guessable id.
func newSessionID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(b[:]), nil
}
