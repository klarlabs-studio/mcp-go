package middleware

import (
	"context"
	"time"

	"go.klarlabs.de/mcp/protocol"
)

// Timeout returns middleware that enforces a request deadline.
//
// The handler runs on its own goroutine and is raced against the deadline. If
// it does not finish in time, the request returns a timeout error immediately
// (rather than blocking until the handler happens to return). The handler's
// context is canceled, so a cooperative handler stops promptly; a handler that
// ignores its context keeps running on the detached goroutine until it returns
// — the buffered result channel ensures that goroutine does not leak.
func Timeout(d time.Duration) Middleware {
	return func(next HandlerFunc) HandlerFunc {
		return func(ctx context.Context, req *protocol.Request) (*protocol.Response, error) {
			ctx, cancel := context.WithTimeout(ctx, d)
			defer cancel()

			type result struct {
				resp *protocol.Response
				err  error
			}
			done := make(chan result, 1) // buffered so the goroutine never blocks
			go func() {
				resp, err := next(ctx, req)
				done <- result{resp, err}
			}()

			select {
			case r := <-done:
				return r.resp, r.err
			case <-ctx.Done():
				// Deadline hit (or parent canceled) before the handler
				// returned. Surface the context error so callers can
				// distinguish DeadlineExceeded from Canceled.
				return nil, ctx.Err()
			}
		}
	}
}
