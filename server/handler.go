package server

import (
	"context"

	"go.klarlabs.de/mcp/protocol"
)

// HandlerFunc is the signature for request handlers.
type HandlerFunc func(ctx context.Context, req *protocol.Request) (*protocol.Response, error)

// Middleware wraps a handler with additional behavior.
type Middleware func(next HandlerFunc) HandlerFunc

// Chain composes middleware in order, executing first middleware first.
func Chain(middlewares ...Middleware) Middleware {
	return func(final HandlerFunc) HandlerFunc {
		for i := len(middlewares) - 1; i >= 0; i-- {
			final = middlewares[i](final)
		}
		return final
	}
}
