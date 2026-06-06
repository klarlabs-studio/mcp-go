// Package middleware provides middleware utilities for MCP request handling.
package middleware

import (
	"context"

	"go.klarlabs.de/mcp/protocol"
)

// HandlerFunc is the signature for request handlers.
type HandlerFunc func(ctx context.Context, req *protocol.Request) (*protocol.Response, error)

// Middleware wraps a handler with additional behavior.
type Middleware func(next HandlerFunc) HandlerFunc

// Chain composes multiple middleware into a single middleware.
// Middleware are applied in order, so Chain(m1, m2, m3) results in
// m1 wrapping m2 wrapping m3 wrapping the final handler.
func Chain(middlewares ...Middleware) Middleware {
	return func(final HandlerFunc) HandlerFunc {
		// Apply middleware in reverse order so they execute in order
		for i := len(middlewares) - 1; i >= 0; i-- {
			final = middlewares[i](final)
		}
		return final
	}
}

// MiddlewareChain provides a fluent API for building middleware chains.
type MiddlewareChain struct {
	middlewares []Middleware
}

// Use creates a new middleware chain starting with the given middleware.
func Use(middlewares ...Middleware) *MiddlewareChain {
	return &MiddlewareChain{
		middlewares: middlewares,
	}
}

// Append adds middleware to the chain and returns the updated chain.
func (c *MiddlewareChain) Append(middlewares ...Middleware) *MiddlewareChain {
	c.middlewares = append(c.middlewares, middlewares...)
	return c
}

// Then applies the middleware chain to a handler and returns the wrapped handler.
func (c *MiddlewareChain) Then(handler HandlerFunc) HandlerFunc {
	return Chain(c.middlewares...)(handler)
}

// ThenFunc applies the middleware chain to a handler function and returns the wrapped handler.
func (c *MiddlewareChain) ThenFunc(fn func(ctx context.Context, req *protocol.Request) (*protocol.Response, error)) HandlerFunc {
	return c.Then(HandlerFunc(fn))
}
