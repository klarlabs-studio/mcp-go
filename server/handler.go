package server

import "go.klarlabs.de/mcp/middleware"

// HandlerFunc is the signature for request handlers.
//
// It is an alias of middleware.HandlerFunc so the server's Use()-registered
// middleware and the framework's middleware package (Recover, Timeout, …) share
// a single type. Previously the server defined its own parallel HandlerFunc /
// Middleware / Chain, which were incompatible with middleware.* — so middleware
// passed to Server.Use could not even be the standard ones, and the field was
// never applied.
type HandlerFunc = middleware.HandlerFunc

// Middleware wraps a handler with additional behavior. Alias of
// middleware.Middleware (see HandlerFunc).
type Middleware = middleware.Middleware

// Chain composes middleware in order, executing the first middleware first.
func Chain(m ...Middleware) Middleware {
	return middleware.Chain(m...)
}
