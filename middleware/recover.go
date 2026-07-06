package middleware

import (
	"context"
	"log"
	"runtime/debug"

	"go.klarlabs.de/mcp/protocol"
)

// PanicHandler is called when a panic is recovered.
type PanicHandler func(ctx context.Context, req *protocol.Request, panicVal any) (*protocol.Response, error)

// Recover returns middleware that catches panics and converts them to internal errors.
// The panic value is included in the error message for debugging.
func Recover() Middleware {
	return RecoverWithHandler(defaultPanicHandler)
}

// RecoverWithHandler returns middleware that catches panics and calls the provided handler.
// This allows for custom panic handling such as logging or alerting.
func RecoverWithHandler(handler PanicHandler) Middleware {
	return func(next HandlerFunc) HandlerFunc {
		return func(ctx context.Context, req *protocol.Request) (resp *protocol.Response, err error) {
			defer func() {
				if r := recover(); r != nil {
					resp, err = handler(ctx, req, r)
				}
			}()
			return next(ctx, req)
		}
	}
}

// defaultPanicHandler logs the panic (with stack) server-side and returns a
// GENERIC internal error to the peer.
//
// The panic value is deliberately NOT included in the response: it routinely
// embeds internal paths, state, or secret-adjacent values, and the peer may be
// untrusted. Detail goes to the standard logger (stderr — never stdout, which
// would corrupt stdio framing). Use RecoverWithHandler for custom reporting.
func defaultPanicHandler(_ context.Context, req *protocol.Request, panicVal any) (*protocol.Response, error) {
	method := ""
	if req != nil {
		method = req.Method
	}
	log.Printf("mcp: recovered panic handling %q: %v\n%s", method, panicVal, debug.Stack())
	return nil, protocol.NewInternalError("internal error")
}
