package middleware

import (
	"context"
	"fmt"

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

// defaultPanicHandler converts a panic value to an internal error.
func defaultPanicHandler(_ context.Context, _ *protocol.Request, panicVal any) (*protocol.Response, error) {
	var msg string
	switch v := panicVal.(type) {
	case error:
		msg = fmt.Sprintf("panic: %v", v)
	case string:
		msg = fmt.Sprintf("panic: %s", v)
	default:
		msg = fmt.Sprintf("panic: %v", v)
	}
	return nil, protocol.NewInternalError(msg)
}
